package pgcluster

import (
	"context"
	// #nosec G501
	"crypto/md5"
	"fmt"
	"reflect"
	"time"

	"github.com/pkg/errors"
	"go.opentelemetry.io/otel/trace"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
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

	"github.com/percona/percona-postgresql-operator/internal/logging"
	"github.com/percona/percona-postgresql-operator/internal/naming"
	perconaController "github.com/percona/percona-postgresql-operator/percona/controller"
	"github.com/percona/percona-postgresql-operator/percona/k8s"
	"github.com/percona/percona-postgresql-operator/percona/pmm"
	v2 "github.com/percona/percona-postgresql-operator/pkg/apis/pgv2.percona.com/v2"
	"github.com/percona/percona-postgresql-operator/pkg/apis/postgres-operator.crunchydata.com/v1beta1"
)

const (
	// ControllerName is the name of the PerconaPGCluster controller
	PGClusterControllerName = "perconapgcluster-controller"
)

// Reconciler holds resources for the PerconaPGCluster reconciler
type PGClusterReconciler struct {
	Client            client.Client
	Owner             client.FieldOwner
	Recorder          record.EventRecorder
	Tracer            trace.Tracer
	Platform          string
	KubeVersion       string
	CrunchyController controller.Controller
}

// SetupWithManager adds the PerconaPGCluster controller to the provided runtime manager
func (r *PGClusterReconciler) SetupWithManager(mgr manager.Manager) error {
	if err := r.CrunchyController.Watch(source.Kind(mgr.GetCache(), &corev1.Secret{}), r.watchSecrets()); err != nil {
		return errors.Wrap(err, "unable to watch secrets")
	}
	if err := r.CrunchyController.Watch(source.Kind(mgr.GetCache(), &batchv1.Job{}), r.watchBackupJobs()); err != nil {
		return errors.Wrap(err, "unable to watch jobs")
	}

	return builder.ControllerManagedBy(mgr).
		For(&v2.PerconaPGCluster{}).
		Owns(&v1beta1.PostgresCluster{}).
		Watches(&corev1.Service{}, r.watchServices()).
		Watches(&corev1.Secret{}, r.watchSecrets()).
		Watches(&batchv1.Job{}, r.watchBackupJobs()).
		Complete(r)
}

func (r *PGClusterReconciler) watchServices() handler.Funcs {
	return handler.Funcs{
		UpdateFunc: func(ctx context.Context, e event.UpdateEvent, q workqueue.RateLimitingInterface) {
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

func (r *PGClusterReconciler) watchBackupJobs() handler.Funcs {
	return handler.Funcs{
		UpdateFunc: func(ctx context.Context, e event.UpdateEvent, q workqueue.RateLimitingInterface) {
			labels := e.ObjectNew.GetLabels()
			crName := labels[naming.LabelCluster]
			repoName := labels[naming.LabelPGBackRestRepo]

			if len(crName) != 0 && len(repoName) != 0 &&
				!reflect.DeepEqual(e.ObjectNew.(*batchv1.Job).Status, e.ObjectOld.(*batchv1.Job).Status) {
				q.Add(reconcile.Request{NamespacedName: client.ObjectKey{
					Namespace: e.ObjectNew.GetNamespace(),
					Name:      crName,
				}})
			}
		},
	}
}

func (r *PGClusterReconciler) watchSecrets() handler.Funcs {
	return handler.Funcs{
		UpdateFunc: func(ctx context.Context, e event.UpdateEvent, q workqueue.RateLimitingInterface) {
			labels := e.ObjectNew.GetLabels()
			crName := labels[naming.LabelCluster]

			if len(crName) != 0 &&
				!reflect.DeepEqual(e.ObjectNew.(*corev1.Secret).Data, e.ObjectOld.(*corev1.Secret).Data) {
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
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=create;list;update

func (r *PGClusterReconciler) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	log := logging.FromContext(ctx)

	cr := &v2.PerconaPGCluster{}
	if err := r.Client.Get(ctx, request.NamespacedName, cr); err != nil {
		// NotFound cannot be fixed by requeuing so ignore it. During background
		// deletion, we receive delete events from cluster's dependents after
		// cluster is deleted.
		if err = client.IgnoreNotFound(err); err != nil {
			log.Error(err, "unable to fetch PerconaPGCluster")
		}
		return ctrl.Result{}, err
	}

	cr.Default()

	postgresCluster := &v1beta1.PostgresCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cr.Name,
			Namespace: cr.Namespace,
		},
	}

	if cr.DeletionTimestamp != nil {
		// We're deleting PostgresCluster explicitly to let Crunchy controller run its finalizers and not mess with us.
		if err := r.Client.Delete(ctx, postgresCluster); client.IgnoreNotFound(err) != nil {
			return ctrl.Result{RequeueAfter: 5 * time.Second}, errors.Wrap(err, "delete postgres cluster")
		}

		if err := r.Client.Get(ctx, client.ObjectKeyFromObject(postgresCluster), postgresCluster); err == nil {
			log.Info("Waiting for PostgresCluster to be deleted")
			return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
		}

		if err := r.runFinalizers(ctx, cr); err != nil {
			return reconcile.Result{RequeueAfter: 5 * time.Second}, err
		}

		return reconcile.Result{}, nil
	}

	if err := r.reconcileVersion(ctx, cr); err != nil {
		return reconcile.Result{}, errors.Wrap(err, "reconcile version")
	}

	if err := r.reconcileBackupJobs(ctx, cr); err != nil {
		return reconcile.Result{}, errors.Wrap(err, "reconcile backup jobs")
	}

	if err := r.addPMMSidecar(ctx, cr); err != nil {
		return reconcile.Result{}, errors.Wrap(err, "failed to add pmm sidecar")
	}

	if err := r.handleMonitorUserPassChange(ctx, cr); err != nil {
		return reconcile.Result{}, err
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

	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, postgresCluster, func() error {
		var err error
		postgresCluster, err = cr.ToCrunchy(ctx, postgresCluster, r.Client.Scheme())

		return err
	})
	if err != nil {
		return ctrl.Result{}, errors.Wrap(err, "update/create PostgresCluster")
	}

	if err := r.Client.Get(ctx, client.ObjectKeyFromObject(postgresCluster), postgresCluster); err != nil {
		return ctrl.Result{}, errors.Wrap(err, "get PostgresCluster")
	}

	err = r.updateStatus(ctx, cr, &postgresCluster.Status)

	return ctrl.Result{}, err
}

func (r *PGClusterReconciler) addPMMSidecar(ctx context.Context, cr *v2.PerconaPGCluster) error {
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
		set.Metadata.Annotations[v2.AnnotationPMMSecretHash] = pmmSecretHash

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
		set.Metadata.Annotations[v2.AnnotationMonitorUserSecretHash] = currentHash
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
		Namespace: cr.Namespace}); err != nil {
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
