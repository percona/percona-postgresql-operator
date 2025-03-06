package pgcluster

import (
	"bytes"
	"context"
	"crypto/md5"
	"fmt"
	"io"
	"reflect"
	"slices"
	"strings"
	"time"

	gover "github.com/hashicorp/go-version"
	"github.com/pkg/errors"
	"go.opentelemetry.io/otel/trace"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	"github.com/percona/percona-postgresql-operator/internal/controller/runtime"
	"github.com/percona/percona-postgresql-operator/internal/logging"
	"github.com/percona/percona-postgresql-operator/internal/naming"
	"github.com/percona/percona-postgresql-operator/internal/postgres"
	"github.com/percona/percona-postgresql-operator/percona/clientcmd"
	perconaController "github.com/percona/percona-postgresql-operator/percona/controller"
	"github.com/percona/percona-postgresql-operator/percona/extensions"
	"github.com/percona/percona-postgresql-operator/percona/k8s"
	pNaming "github.com/percona/percona-postgresql-operator/percona/naming"
	"github.com/percona/percona-postgresql-operator/percona/pmm"
	perconaPG "github.com/percona/percona-postgresql-operator/percona/postgres"
	"github.com/percona/percona-postgresql-operator/percona/utils/registry"
	"github.com/percona/percona-postgresql-operator/percona/watcher"
	v2 "github.com/percona/percona-postgresql-operator/pkg/apis/pgv2.percona.com/v2"
	"github.com/percona/percona-postgresql-operator/pkg/apis/postgres-operator.crunchydata.com/v1beta1"
)

const (
	// ControllerName is the name of the PerconaPGCluster controller
	PGClusterControllerName = "perconapgcluster-controller"
)

// Reconciler holds resources for the PerconaPGCluster reconciler
type PGClusterReconciler struct {
	Client  client.Client
	Owner   client.FieldOwner
	PodExec func(
		ctx context.Context, namespace, pod, container string,
		stdin io.Reader, stdout, stderr io.Writer, command ...string,
	) error
	Recorder             record.EventRecorder
	Tracer               trace.Tracer
	Platform             string
	KubeVersion          string
	CrunchyController    controller.Controller
	IsOpenShift          bool
	Cron                 CronRegistry
	Watchers             *registry.Registry
	ExternalChan         chan event.GenericEvent
	StopExternalWatchers chan event.DeleteEvent
}

// SetupWithManager adds the PerconaPGCluster controller to the provided runtime manager
func (r *PGClusterReconciler) SetupWithManager(mgr manager.Manager) error {
	if r.PodExec == nil {
		var err error
		r.PodExec, err = runtime.NewPodExecutor(mgr.GetConfig())
		if err != nil {
			return err
		}
	}

	if err := r.CrunchyController.Watch(source.Kind(mgr.GetCache(), &corev1.Secret{}, r.watchSecrets())); err != nil {
		return errors.Wrap(err, "unable to watch secrets")
	}
	if err := r.CrunchyController.Watch(source.Kind(mgr.GetCache(), &batchv1.Job{}, r.watchBackupJobs())); err != nil {
		return errors.Wrap(err, "unable to watch jobs")
	}
	if err := r.CrunchyController.Watch(source.Kind(mgr.GetCache(), &v2.PerconaPGBackup{}, r.watchPGBackups())); err != nil {
		return errors.Wrap(err, "unable to watch pg-backups")
	}

	return builder.ControllerManagedBy(mgr).
		For(&v2.PerconaPGCluster{}).
		Owns(&v1beta1.PostgresCluster{}).
		WatchesRawSource(source.Kind(mgr.GetCache(), &corev1.Service{}, r.watchServices())).
		WatchesRawSource(source.Kind(mgr.GetCache(), &corev1.Secret{}, r.watchSecrets())).
		WatchesRawSource(source.Kind(mgr.GetCache(), &batchv1.Job{}, r.watchBackupJobs())).
		WatchesRawSource(source.Kind(mgr.GetCache(), &v2.PerconaPGBackup{}, r.watchPGBackups())).
		Complete(r)
}

func (r *PGClusterReconciler) watchServices() handler.TypedFuncs[*corev1.Service, reconcile.Request] {
	return handler.TypedFuncs[*corev1.Service, reconcile.Request]{
		UpdateFunc: func(ctx context.Context, e event.TypedUpdateEvent[*corev1.Service], q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
			labels := e.ObjectNew.GetLabels()
			crName := labels[naming.LabelCluster]

			if e.ObjectNew.GetName() == crName+"-pgbouncer" {
				q.Add(reconcile.Request{NamespacedName: client.ObjectKey{
					Namespace: e.ObjectNew.GetNamespace(),
					Name:      crName,
				}})
			}
		},
	}
}

func (r *PGClusterReconciler) watchBackupJobs() handler.TypedFuncs[*batchv1.Job, reconcile.Request] {
	return handler.TypedFuncs[*batchv1.Job, reconcile.Request]{
		UpdateFunc: func(ctx context.Context, e event.TypedUpdateEvent[*batchv1.Job], q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
			labels := e.ObjectNew.GetLabels()
			crName := labels[naming.LabelCluster]
			repoName := labels[naming.LabelPGBackRestRepo]

			if len(crName) != 0 && len(repoName) != 0 &&
				!reflect.DeepEqual(e.ObjectNew.Status, e.ObjectOld.Status) {
				q.Add(reconcile.Request{NamespacedName: client.ObjectKey{
					Namespace: e.ObjectNew.GetNamespace(),
					Name:      crName,
				}})
			}
		},
	}
}

func (r *PGClusterReconciler) watchPGBackups() handler.TypedFuncs[*v2.PerconaPGBackup, reconcile.Request] {
	return handler.TypedFuncs[*v2.PerconaPGBackup, reconcile.Request]{
		UpdateFunc: func(ctx context.Context, e event.TypedUpdateEvent[*v2.PerconaPGBackup], q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
			pgBackup := e.ObjectNew
			q.Add(reconcile.Request{NamespacedName: client.ObjectKey{
				Namespace: pgBackup.GetNamespace(),
				Name:      pgBackup.Spec.PGCluster,
			}})
		},
		DeleteFunc: func(ctx context.Context, e event.TypedDeleteEvent[*v2.PerconaPGBackup], q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
			pgBackup := e.Object
			q.Add(reconcile.Request{NamespacedName: client.ObjectKey{
				Namespace: pgBackup.GetNamespace(),
				Name:      pgBackup.Spec.PGCluster,
			}})
		},
	}
}

func (r *PGClusterReconciler) watchSecrets() handler.TypedFuncs[*corev1.Secret, reconcile.Request] {
	return handler.TypedFuncs[*corev1.Secret, reconcile.Request]{
		UpdateFunc: func(ctx context.Context, e event.TypedUpdateEvent[*corev1.Secret], q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
			labels := e.ObjectNew.GetLabels()
			crName := labels[naming.LabelCluster]

			if len(crName) != 0 &&
				!reflect.DeepEqual(e.ObjectNew.Data, e.ObjectOld.Data) {
				q.Add(reconcile.Request{NamespacedName: client.ObjectKey{
					Namespace: e.ObjectNew.GetNamespace(),
					Name:      crName,
				}})
			}
		},
	}
}

// +kubebuilder:rbac:groups=pgv2.percona.com,resources=perconapgclusters,verbs=get;list;watch
// +kubebuilder:rbac:groups=pgv2.percona.com,resources=perconapgclusters/status,verbs=patch;update
// +kubebuilder:rbac:groups=postgres-operator.crunchydata.com,resources=postgresclusters,verbs=get;list;create;update;patch;delete;watch
// +kubebuilder:rbac:groups=apps,resources=replicasets,verbs=create;delete;get;list;patch;watch
// +kubebuilder:rbac:groups=pgv2.percona.com,resources=perconapgclusters/finalizers,verbs=update
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=create;list;update
// +kubebuilder:rbac:groups="",resources="pods",verbs=create;delete

func (r *PGClusterReconciler) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	log := logging.FromContext(ctx).WithValues("cluster", request.Name, "namespace", request.Namespace)

	cr := &v2.PerconaPGCluster{}
	if err := r.Client.Get(ctx, request.NamespacedName, cr); err != nil {
		// NotFound cannot be fixed by requeuing so ignore it. During background
		// deletion, we receive delete events from cluster's dependents after
		// cluster is deleted.
		if err = client.IgnoreNotFound(err); err != nil {
			log.Error(err, "unable to fetch PerconaPGCluster")
		}
		return ctrl.Result{}, errors.Wrap(err, "get PerconaPGCluster")
	}

	cr.Default()

	if cr.Spec.OpenShift == nil {
		cr.Spec.OpenShift = &r.IsOpenShift
	}

	postgresCluster := &v1beta1.PostgresCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cr.Name,
			Namespace: cr.Namespace,
		},
	}

	if cr.DeletionTimestamp != nil {
		log.Info("Deleting PerconaPGCluster", "deletionTimestamp", cr.DeletionTimestamp)

		if err := r.runFinalizers(ctx, cr); err != nil {
			return reconcile.Result{RequeueAfter: 5 * time.Second}, errors.Wrap(err, "run finalizers")
		}

		// We're deleting PostgresCluster explicitly to let Crunchy controller run its finalizers and not mess with us.
		if err := r.Client.Delete(ctx, postgresCluster); client.IgnoreNotFound(err) != nil {
			return ctrl.Result{RequeueAfter: 5 * time.Second}, errors.Wrap(err, "delete postgres cluster")
		}

		if err := r.Client.Get(ctx, client.ObjectKeyFromObject(postgresCluster), postgresCluster); err == nil {
			log.Info("Waiting for PostgresCluster to be deleted")
			return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
		}

		return reconcile.Result{}, nil
	}

	if err := r.ensureFinalizers(ctx, cr); err != nil {
		return reconcile.Result{}, errors.Wrap(err, "ensure finalizers")
	}

	if err := r.reconcilePatroniVersionCheck(ctx, cr); err != nil {
		if errors.Is(err, errPatroniVersionCheckWait) {
			return reconcile.Result{
				RequeueAfter: 5 * time.Second,
			}, nil
		}
		return reconcile.Result{}, errors.Wrap(err, "check patroni version")
	}

	if err := r.reconcileTLS(ctx, cr); err != nil {
		return reconcile.Result{}, errors.Wrap(err, "reconcile TLS")
	}

	if err := r.reconcileExternalWatchers(ctx, cr); err != nil {
		return reconcile.Result{}, errors.Wrap(err, "start external watchers")
	}

	if err := r.reconcileVersion(ctx, cr); err != nil {
		return reconcile.Result{}, errors.Wrap(err, "reconcile version")
	}

	if err := r.reconcileBackups(ctx, cr); err != nil {
		return reconcile.Result{}, errors.Wrap(err, "reconcile backups")
	}

	if err := r.createBootstrapRestoreObject(ctx, cr); err != nil {
		return reconcile.Result{}, errors.Wrap(err, "reconcile restore")
	}

	if err := r.reconcilePMM(ctx, cr); err != nil {
		return reconcile.Result{}, errors.Wrap(err, "failed to add pmm sidecar")
	}

	if err := r.handleMonitorUserPassChange(ctx, cr); err != nil {
		return reconcile.Result{}, errors.Wrap(err, "failed to handle monitor user password change")
	}

	if err := r.reconcileCustomExtensions(ctx, cr); err != nil {
		return reconcile.Result{}, errors.Wrap(err, "reconcile custom extensions")
	}

	if err := r.reconcileScheduledBackups(ctx, cr); err != nil {
		return reconcile.Result{}, errors.Wrap(err, "reconcile scheduled backups")
	}

	if cr.Spec.Pause != nil && *cr.Spec.Pause {
		backupRunning, err := isBackupRunning(ctx, r.Client, cr)
		if err != nil {
			return reconcile.Result{}, errors.Wrap(err, "is backup running")
		}
		if backupRunning {
			*cr.Spec.Pause = false
			log.Info("Backup is running. Can't pause cluster", "cluster", cr.Name)
		}
	}

	var opRes controllerutil.OperationResult
	if err := retry.OnError(retry.DefaultRetry, func(err error) bool { return err != nil }, func() error {
		var err error
		opRes, err = controllerutil.CreateOrUpdate(ctx, r.Client, postgresCluster, func() error {
			var err error
			postgresCluster, err = cr.ToCrunchy(ctx, postgresCluster, r.Client.Scheme())

			return err
		})
		return err
	}); err != nil {
		return ctrl.Result{}, errors.Wrap(err, "update/create PostgresCluster")
	}

	// postgresCluster will not be available immediately after creation.
	// We should wait some time, it's better to continue on the next reconcile
	if opRes == controllerutil.OperationResultCreated {
		return ctrl.Result{RequeueAfter: 1 * time.Second}, nil
	}

	if err := r.Client.Get(ctx, client.ObjectKeyFromObject(postgresCluster), postgresCluster); err != nil {
		if k8serrors.IsNotFound(err) {
			return ctrl.Result{RequeueAfter: 1 * time.Second}, nil
		}
		return ctrl.Result{}, errors.Wrap(err, "get PostgresCluster")
	}

	if err := r.updateStatus(ctx, cr, &postgresCluster.Status); err != nil {
		return ctrl.Result{}, errors.Wrap(err, "update status")
	}

	return ctrl.Result{}, nil
}

var errPatroniVersionCheckWait = errors.New("waiting for pod to initialize")

func (r *PGClusterReconciler) reconcilePatroniVersionCheck(ctx context.Context, cr *v2.PerconaPGCluster) error {
	if cr.Annotations == nil {
		cr.Annotations = make(map[string]string)
	}

	getImageIDFromPod := func(pod *corev1.Pod, containerName string) string {
		idx := slices.IndexFunc(pod.Status.ContainerStatuses, func(s corev1.ContainerStatus) bool {
			return s.Name == containerName
		})
		if idx == -1 {
			return ""
		}
		return pod.Status.ContainerStatuses[idx].ImageID
	}

	pods := new(corev1.PodList)
	instances, err := naming.AsSelector(naming.ClusterInstances(cr.Name))
	if err != nil {
		return err
	}
	if err = r.Client.List(ctx, pods, client.InNamespace(cr.Namespace), client.MatchingLabelsSelector{Selector: instances}); err != nil {
		return err
	}

	// Collecting all image IDs from instance pods. Under normal conditions, this slice will contain a single image ID, as all pods typically use the same image.
	// During an image update, it may contain multiple different image IDs as the update progresses.
	imageIDs := []string{}
	for _, pod := range pods.Items {
		imageID := getImageIDFromPod(&pod, naming.ContainerDatabase)
		if imageID != "" && !slices.Contains(imageIDs, imageID) {
			imageIDs = append(imageIDs, imageID)
		}
	}

	// If the imageIDs slice contains the imageID from the status, we skip checking the Patroni version.
	// This ensures that the Patroni version is only checked after all pods have been updated.
	if slices.Contains(imageIDs, cr.Status.Postgres.ImageID) && cr.Status.PatroniVersion != "" {
		cr.Annotations[pNaming.AnnotationPatroniVersion] = cr.Status.PatroniVersion
		return nil
	}

	meta := metav1.ObjectMeta{
		Name:      cr.Name + "-patroni-version-check",
		Namespace: cr.Namespace,
	}

	p := &corev1.Pod{
		ObjectMeta: meta,
	}

	err = r.Client.Get(ctx, client.ObjectKeyFromObject(p), p)
	if client.IgnoreNotFound(err) != nil {
		return errors.Wrap(err, "failed to get patroni version check pod")
	}
	if k8serrors.IsNotFound(err) {
		if len(cr.Spec.InstanceSets) == 0 {
			return errors.New(".spec.instances is a required value") // shouldn't happen as the value is required in the crd.yaml
		}
		p = &corev1.Pod{
			ObjectMeta: meta,
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name:  pNaming.ContainerPatroniVersionCheck,
						Image: cr.Spec.Image,
						Command: []string{
							"bash",
						},
						Args: []string{
							"-c", "sleep 300",
						},
						Resources: cr.Spec.InstanceSets[0].Resources,
					},
				},
				SecurityContext:               cr.Spec.InstanceSets[0].SecurityContext,
				TerminationGracePeriodSeconds: ptr.To(int64(5)),
			},
		}

		if err := controllerutil.SetControllerReference(cr, p, r.Client.Scheme()); err != nil {
			return errors.Wrap(err, "set controller reference")
		}
		if err := r.Client.Create(ctx, p); client.IgnoreAlreadyExists(err) != nil {
			return errors.Wrap(err, "failed to create pod to check patroni version")
		}

		return errPatroniVersionCheckWait
	}

	if p.Status.Phase != corev1.PodRunning {
		return errPatroniVersionCheckWait
	}

	var stdout, stderr bytes.Buffer
	execCli, err := clientcmd.NewClient()
	if err != nil {
		return errors.Wrap(err, "failed to create exec client")
	}
	b := wait.Backoff{
		Duration: 5 * time.Second,
		Factor:   1.0,
		Steps:    12,
		Cap:      time.Minute,
	}
	if err := retry.OnError(b, func(err error) bool { return err != nil && strings.Contains(err.Error(), "container not found") }, func() error {
		return execCli.Exec(ctx, p, pNaming.ContainerPatroniVersionCheck, nil, &stdout, &stderr, "patronictl", "version")
	}); err != nil {
		return errors.Wrap(err, "exec")
	}

	patroniVersion := strings.TrimSpace(strings.TrimPrefix(stdout.String(), "patronictl version "))

	if _, err := gover.NewVersion(patroniVersion); err != nil {
		return errors.Wrap(err, "failed to validate patroni version")
	}

	orig := cr.DeepCopy()

	cr.Status.PatroniVersion = patroniVersion
	cr.Status.Postgres.Version = cr.Spec.PostgresVersion
	cr.Status.Postgres.ImageID = getImageIDFromPod(p, pNaming.ContainerPatroniVersionCheck)

	if err := r.Client.Status().Patch(ctx, cr.DeepCopy(), client.MergeFrom(orig)); err != nil {
		return errors.Wrap(err, "failed to patch patroni version")
	}

	if err := r.Client.Delete(ctx, p); err != nil {
		return errors.Wrap(err, "failed to delete patroni version check pod")
	}
	cr.Annotations[pNaming.AnnotationPatroniVersion] = patroniVersion

	return nil
}

func (r *PGClusterReconciler) reconcileTLS(ctx context.Context, cr *v2.PerconaPGCluster) error {
	if err := r.validateTLS(ctx, cr); err != nil {
		return errors.Wrap(err, "validate TLS")
	}
	if err := r.reconcileOldCACert(ctx, cr); err != nil {
		return errors.Wrap(err, "reconcile old CA")
	}
	return nil
}

func (r *PGClusterReconciler) validateTLS(ctx context.Context, cr *v2.PerconaPGCluster) error {
	validateSecretProjection := func(p *corev1.SecretProjection, requiredPaths ...string) error {
		if p == nil {
			return nil
		}

		if p.Name == "" {
			return errors.New("secret name is not specified")
		}

		secret := new(corev1.Secret)
		nn := types.NamespacedName{Name: p.Name, Namespace: cr.Namespace}
		if err := r.Client.Get(ctx, nn, secret); err != nil {
			return errors.Wrapf(err, "failed to get secret %s", nn.Name)
		}

		pathMap := make(map[string]struct{})
		for _, item := range p.Items {
			if _, ok := secret.Data[item.Key]; !ok {
				return errors.Errorf("key %s doesn't exist in secret %s", item.Key, secret.Name)
			}
			pathMap[item.Path] = struct{}{}
		}

		for _, path := range requiredPaths {
			if _, ok := pathMap[path]; !ok {
				if _, ok := secret.Data[path]; !ok {
					return errors.Errorf("required path %s was not found both in secret %s and in the .items section", path, secret.Name)
				}
			}
		}

		return nil
	}

	if err := validateSecretProjection(cr.Spec.Secrets.CustomRootCATLSSecret, "root.crt", "root.key"); err != nil {
		return errors.Wrap(err, "failed to validate .spec.customRootCATLSSecret")
	}

	certPaths := []string{"tls.key", "tls.crt"}
	if cr.Spec.Secrets.CustomRootCATLSSecret == nil {
		certPaths = append(certPaths, "ca.crt")
	}
	if err := validateSecretProjection(cr.Spec.Secrets.CustomTLSSecret, certPaths...); err != nil {
		return errors.Wrap(err, "failed to validate .spec.customTLSSecret")
	}
	if err := validateSecretProjection(cr.Spec.Secrets.CustomReplicationClientTLSSecret, certPaths...); err != nil {
		return errors.Wrap(err, "failed to validate .spec.customReplicationTLSSecret")
	}
	return nil
}

func (r *PGClusterReconciler) reconcileOldCACert(ctx context.Context, cr *v2.PerconaPGCluster) error {
	if cr.Spec.Secrets.CustomRootCATLSSecret != nil {
		return nil
	}

	oldCASecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      naming.RootCertSecret,
			Namespace: cr.Namespace,
		},
	}
	err := r.Client.Get(ctx, client.ObjectKeyFromObject(oldCASecret), oldCASecret)
	if client.IgnoreNotFound(err) != nil {
		return errors.Wrap(err, "failed to get old ca secret")
	}

	if cr.CompareVersion("2.5.0") < 0 {
		if k8serrors.IsNotFound(err) {
			// K8SPG-555: We should create an empty secret with old name, so that crunchy part can populate it
			// instead of creating secrets unique to the cluster
			// TODO: remove when 2.4.0 will become unsupported

			if err := r.Client.Create(ctx, oldCASecret); err != nil {
				return errors.Wrap(err, "failed to create ca secret")
			}
		}
		return nil
	}
	if k8serrors.IsNotFound(err) {
		return nil
	}

	// K8SPG-555: Previously we used a single CA secret for all clusters in a namespace.
	// We should copy the contents of the old CA secret, if it exists, to the new one, which is unique for each cluster.
	// TODO: remove when 2.4.0 will become unsupported
	newCASecret := &corev1.Secret{
		ObjectMeta: naming.PostgresRootCASecret(
			&v1beta1.PostgresCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      cr.Name,
					Namespace: cr.Namespace,
				},
			}),
	}
	err = r.Client.Get(ctx, client.ObjectKeyFromObject(newCASecret), new(corev1.Secret))
	if client.IgnoreNotFound(err) != nil {
		return errors.Wrap(err, "failed to get new ca secret")
	}

	if k8serrors.IsNotFound(err) {
		err := r.Client.Get(ctx, types.NamespacedName{
			Name:      cr.Name,
			Namespace: cr.Namespace,
		}, new(v1beta1.PostgresCluster))
		if client.IgnoreNotFound(err) != nil {
			return errors.Wrap(err, "failed to get crunchy cluster")
		}
		// If the cluster is new, we should not copy the old CA secret.
		// We should create an empty secret instead, so that crunchy part can populate it.
		if !k8serrors.IsNotFound(err) {
			newCASecret.Data = oldCASecret.Data
		}

		if cr.CompareVersion("2.6.0") >= 0 && cr.Spec.Metadata != nil {
			newCASecret.Annotations = cr.Spec.Metadata.Annotations
			newCASecret.Labels = cr.Spec.Metadata.Labels
		}

		if err := r.Client.Create(ctx, newCASecret); err != nil {
			return errors.Wrap(err, "failed to create updated CA secret")
		}
	}
	return nil
}

func (r *PGClusterReconciler) reconcilePMM(ctx context.Context, cr *v2.PerconaPGCluster) error {
	if !cr.PMMEnabled() {
		return nil
	}

	log := logging.FromContext(ctx)

	if cr.Spec.PMM.Secret == "" {
		log.Info(fmt.Sprintf("Can't enable PMM: `.spec.pmm.secret` is empty in %s", cr.Name))
		return nil
	}

	pmmSecret := new(corev1.Secret)
	if err := r.Client.Get(ctx, types.NamespacedName{
		Name:      cr.Spec.PMM.Secret,
		Namespace: cr.Namespace,
	}, pmmSecret); err != nil {
		if k8serrors.IsNotFound(err) {
			log.Info(fmt.Sprintf("Can't enable PMM: %s secret doesn't exist", cr.Spec.PMM.Secret))
			return nil
		}
		return errors.Wrap(err, "failed to get pmm secret")
	}

	if pmmSecret.Labels == nil {
		pmmSecret.Labels = make(map[string]string)
	}

	_, pmmSecretLabelOK := pmmSecret.Labels[v2.LabelPMMSecret]
	_, clusterLabelOK := pmmSecret.Labels[naming.LabelCluster]
	if !pmmSecretLabelOK || !clusterLabelOK {
		orig := pmmSecret.DeepCopy()
		pmmSecret.Labels[v2.LabelPMMSecret] = "true"
		pmmSecret.Labels[naming.LabelCluster] = cr.Name
		if err := r.Client.Patch(ctx, pmmSecret, client.MergeFrom(orig)); err != nil {
			return errors.Wrap(err, "label PMM secret")
		}
	}

	if v, ok := pmmSecret.Data[pmm.SecretKey]; !ok || len(v) == 0 {
		log.Info(fmt.Sprintf("Can't enable PMM: %s key doesn't exist in %s secret or empty", pmm.SecretKey, cr.Spec.PMM.Secret))
		return nil
	}

	pmmSecretHash, err := k8s.ObjectHash(pmmSecret)
	if err != nil {
		return errors.Wrap(err, "hash PMM secret")
	}

	for i := 0; i < len(cr.Spec.InstanceSets); i++ {
		set := &cr.Spec.InstanceSets[i]

		if set.Metadata == nil {
			set.Metadata = &v1beta1.Metadata{}
		}
		if set.Metadata.Annotations == nil {
			set.Metadata.Annotations = make(map[string]string)
		}
		set.Metadata.Annotations[pNaming.AnnotationPMMSecretHash] = pmmSecretHash

		set.Sidecars = append(set.Sidecars, pmm.SidecarContainer(cr))
	}

	return nil
}

func (r *PGClusterReconciler) handleMonitorUserPassChange(ctx context.Context, cr *v2.PerconaPGCluster) error {
	if !cr.PMMEnabled() {
		return nil
	}

	log := logging.FromContext(ctx)

	secret := new(corev1.Secret)
	n := cr.Name + "-" + naming.RolePostgresUser + "-" + v2.UserMonitoring

	if err := r.Client.Get(ctx, types.NamespacedName{
		Name:      n,
		Namespace: cr.Namespace,
	}, secret); err != nil {
		if k8serrors.IsNotFound(err) {
			log.V(1).Info(fmt.Sprintf("Secret %s not found", n))
			return nil
		}
		return errors.Wrap(err, "failed to get monitor user secret")
	}

	secretString := fmt.Sprintln(secret.Data)
	// #nosec G401
	currentHash := fmt.Sprintf("%x", md5.Sum([]byte(secretString)))

	for i := 0; i < len(cr.Spec.InstanceSets); i++ {
		set := &cr.Spec.InstanceSets[i]

		if set.Metadata == nil {
			set.Metadata = &v1beta1.Metadata{}
		}
		if set.Metadata.Annotations == nil {
			set.Metadata.Annotations = make(map[string]string)
		}

		// If the currentHash is the same  is the on the STS, restart will not  happen
		set.Metadata.Annotations[pNaming.AnnotationMonitorUserSecretHash] = currentHash
	}

	return nil
}

func (r *PGClusterReconciler) reconcileCustomExtensions(ctx context.Context, cr *v2.PerconaPGCluster) error {
	if cr.Spec.Extensions.Storage.Secret == nil {
		return nil
	}

	if len(cr.Spec.Extensions.Image) == 0 && len(cr.Spec.Extensions.Custom) > 0 {
		return errors.New("you need to set spec.extensions.image to install custom extensions")
	}

	extensionKeys := make([]string, 0)
	extensionNames := make([]string, 0)

	for _, extension := range cr.Spec.Extensions.Custom {
		key := extensions.GetExtensionKey(cr.Spec.PostgresVersion, extension.Name, extension.Version)
		extensionKeys = append(extensionKeys, key)
		extensionNames = append(extensionNames, extension.Name)
	}

	if cr.CompareVersion("2.6.0") >= 0 {
		var removedExtension []string
		installedExtensions := cr.Status.InstalledCustomExtensions
		crExtensions := make(map[string]struct{})
		for _, ext := range extensionNames {
			crExtensions[ext] = struct{}{}
		}

		// Check for missing entries in crExtensions
		for _, ext := range installedExtensions {
			// If an object exists in installedExtensions but not in crExtensions, the extension should be deleted.
			if _, ok := crExtensions[ext]; !ok {
				removedExtension = append(removedExtension, ext)
			}
		}

		if len(removedExtension) > 0 {
			disable := func(ctx context.Context, exec postgres.Executor) error {
				return errors.WithStack(disableCustomExtensionsInDB(ctx, exec, removedExtension))
			}

			primary, err := perconaPG.GetPrimaryPod(ctx, r.Client, cr)
			if err != nil {
				return errors.New("primary pod not found")
			}

			err = disable(ctx, func(ctx context.Context, stdin io.Reader, stdout, stderr io.Writer, command ...string) error {
				return r.PodExec(ctx, primary.Namespace, primary.Name, naming.ContainerDatabase, stdin, stdout, stderr, command...)
			})
			if err != nil {
				return errors.Wrap(err, "deletion extension from installed")
			}
		}
	}

	for i := 0; i < len(cr.Spec.InstanceSets); i++ {
		set := &cr.Spec.InstanceSets[i]
		set.InitContainers = append(set.InitContainers, extensions.ExtensionRelocatorContainer(
			cr, cr.Spec.Image, cr.Spec.ImagePullPolicy, cr.Spec.PostgresVersion,
		))
		set.InitContainers = append(set.InitContainers, extensions.ExtensionInstallerContainer(
			cr,
			cr.Spec.PostgresVersion,
			&cr.Spec.Extensions,
			strings.Join(extensionKeys, ","),
			cr.Spec.OpenShift,
		))
		set.VolumeMounts = append(set.VolumeMounts, extensions.ExtensionVolumeMounts(cr.Spec.PostgresVersion)...)
	}
	return nil
}

func disableCustomExtensionsInDB(ctx context.Context, exec postgres.Executor, customExtensionsForDeletion []string) error {
	log := logging.FromContext(ctx)

	for _, extensionName := range customExtensionsForDeletion {
		sqlCommand := fmt.Sprintf(
			`SET client_min_messages = WARNING; DROP EXTENSION IF EXISTS %s;`,
			extensionName,
		)
		_, _, err := exec.ExecInAllDatabases(ctx,
			sqlCommand,
			map[string]string{
				"ON_ERROR_STOP": "on", // Abort when any one command fails.
				"QUIET":         "on", // Do not print successful commands to stdout.
			},
		)

		log.V(1).Info("extension was disabled ", "extensionName", extensionName)

		return errors.Wrap(err, "custom extension deletion")
	}

	return nil
}

func isBackupRunning(ctx context.Context, cl client.Reader, cr *v2.PerconaPGCluster) (bool, error) {
	jobList := &batchv1.JobList{}
	selector := labels.SelectorFromSet(map[string]string{
		naming.LabelCluster: cr.Name,
	})
	err := cl.List(ctx, jobList, client.InNamespace(cr.Namespace), client.MatchingLabelsSelector{Selector: selector}, client.HasLabels{naming.LabelPGBackRestBackup})
	if err != nil {
		return false, errors.Wrap(err, "get backup jobs")
	}

	for _, job := range jobList.Items {
		job := job
		if perconaController.JobFailed(&job) || perconaController.JobCompleted(&job) {
			continue
		}
		return true, nil
	}

	backupList := &v2.PerconaPGBackupList{}
	if err := cl.List(ctx, backupList, &client.ListOptions{
		Namespace: cr.Namespace,
	}); err != nil {
		return false, errors.Wrap(err, "list backups")
	}

	for _, backup := range backupList.Items {
		if backup.Spec.PGCluster != cr.Name {
			continue
		}
		if backup.Status.State == v2.BackupStarting || backup.Status.State == v2.BackupRunning {
			return true, nil
		}
	}

	return false, nil
}

func (r *PGClusterReconciler) reconcileExternalWatchers(ctx context.Context, cr *v2.PerconaPGCluster) error {
	if err := r.startExternalWatchers(ctx, cr); err != nil {
		return errors.Wrap(err, "start external watchers")
	}

	r.stopExternalWatcher(ctx, cr)

	return nil
}

func (r *PGClusterReconciler) startExternalWatchers(ctx context.Context, cr *v2.PerconaPGCluster) error {
	if !*cr.Spec.Backups.TrackLatestRestorableTime {
		return nil
	}

	log := logging.FromContext(ctx)

	watcherName, watcherFunc := watcher.GetWALWatcher(cr)
	if r.Watchers.IsExist(watcherName) {
		return nil
	}

	if err := r.Watchers.Add(watcherName, watcherFunc); err != nil {
		return errors.Wrap(err, "add WAL watcher")
	}

	log.Info("Starting WAL watcher", "name", watcherName)

	go watcherFunc(ctx, r.Client, r.ExternalChan, r.StopExternalWatchers, cr)

	return nil
}

func (r *PGClusterReconciler) stopExternalWatcher(ctx context.Context, cr *v2.PerconaPGCluster) {
	log := logging.FromContext(ctx)
	if *cr.Spec.Backups.TrackLatestRestorableTime {
		return
	}

	watcherName, _ := watcher.GetWALWatcher(cr)
	if !r.Watchers.IsExist(watcherName) {
		return
	}

	select {
	case r.StopExternalWatchers <- event.DeleteEvent{Object: cr}:
		log.Info("External watcher is stopped", "cluster", cr.Name, "namespace", cr.Namespace, "watcher", watcherName)
	default:
		log.Info("External watcher is already stopped", "cluster", cr.Name, "namespace", cr.Namespace, "watcher", watcherName)
	}

	r.Watchers.Remove(watcherName)
}

func (r *PGClusterReconciler) ensureFinalizers(ctx context.Context, cr *v2.PerconaPGCluster) error {
	if !slices.Contains(cr.Finalizers, pNaming.FinalizerStopWatchersDeprecated) && slices.Contains(cr.Finalizers, pNaming.FinalizerStopWatchers) {
		return nil
	}

	if *cr.Spec.Backups.TrackLatestRestorableTime {
		orig := cr.DeepCopy()
		cr.Finalizers = slices.DeleteFunc(cr.Finalizers, func(f string) bool {
			return f == pNaming.FinalizerStopWatchersDeprecated
		})
		if !slices.Contains(cr.Finalizers, pNaming.FinalizerStopWatchers) {
			cr.Finalizers = append(cr.Finalizers, pNaming.FinalizerStopWatchers)
		}
		if err := r.Client.Patch(ctx, cr.DeepCopy(), client.MergeFrom(orig)); err != nil {
			return errors.Wrap(err, "patch finalizers")
		}
	}

	return nil
}
