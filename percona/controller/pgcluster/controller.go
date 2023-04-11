package pgcluster

import (
	"context"
	"fmt"

	"github.com/pkg/errors"
	"go.opentelemetry.io/otel/trace"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	"github.com/percona/percona-postgresql-operator/internal/logging"
	"github.com/percona/percona-postgresql-operator/internal/naming"
	"github.com/percona/percona-postgresql-operator/percona/pmm"
	"github.com/percona/percona-postgresql-operator/pkg/apis/pg.percona.com/v2beta1"
	"github.com/percona/percona-postgresql-operator/pkg/apis/postgres-operator.crunchydata.com/v1beta1"
)

const (
	// ControllerName is the name of the PerconaPGCluster controller
	PGClusterControllerName = "perconapgcluster-controller"
)

// Reconciler holds resources for the PerconaPGCluster reconciler
type PGClusterReconciler struct {
	Client      client.Client
	Owner       client.FieldOwner
	Recorder    record.EventRecorder
	Tracer      trace.Tracer
	Platform    string
	KubeVersion string
}

// SetupWithManager adds the PerconaPGCluster controller to the provided runtime manager
func (r *PGClusterReconciler) SetupWithManager(mgr manager.Manager) error {
	return builder.ControllerManagedBy(mgr).
		For(&v2beta1.PerconaPGCluster{}).
		Owns(&v1beta1.PostgresCluster{}).
		Watches(&source.Kind{Type: &corev1.Service{}}, r.watchServices()).
		Complete(r)
}

func (r *PGClusterReconciler) watchServices() handler.Funcs {
	return handler.Funcs{
		UpdateFunc: func(e event.UpdateEvent, q workqueue.RateLimitingInterface) {
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

// +kubebuilder:rbac:groups=pg.percona.com,resources=perconapgclusters,verbs=get;list;watch
// +kubebuilder:rbac:groups=pg.percona.com,resources=perconapgclusters/status,verbs=patch;update
// +kubebuilder:rbac:groups=postgres-operator.crunchydata.com,resources=postgresclusters,verbs=get;list;create;update;patch;watch
// +kubebuilder:rbac:groups=apps,resources=replicasets,verbs=create;delete;get;list;patch;watch

func (r *PGClusterReconciler) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	log := logging.FromContext(ctx)

	cr := &v2beta1.PerconaPGCluster{}
	if err := r.Client.Get(ctx, request.NamespacedName, cr); err != nil {
		// NotFound cannot be fixed by requeuing so ignore it. During background
		// deletion, we receive delete events from cluster's dependents after
		// cluster is deleted.
		if err = client.IgnoreNotFound(err); err != nil {
			log.Error(err, "unable to fetch PerconaPGCluster")
		}
		return ctrl.Result{}, err
	}

	if err := r.reconcileVersion(ctx, cr); err != nil {
		return reconcile.Result{}, errors.Wrap(err, "reconcile version")
	}

	postgresCluster := &v1beta1.PostgresCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cr.Name,
			Namespace: cr.Namespace,
		},
	}

	if err := controllerutil.SetControllerReference(cr, postgresCluster, r.Client.Scheme()); err != nil {
		return ctrl.Result{}, err
	}

	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, postgresCluster, func() error {
		postgresCluster.Default()

		annotations := make(map[string]string)
		for k, v := range cr.Annotations {
			switch k {
			case v2beta1.AnnotationPGBackrestBackup:
				annotations[naming.PGBackRestBackup] = v
			case v2beta1.AnnotationPGBackRestRestore:
				annotations[naming.PGBackRestRestore] = v
			case corev1.LastAppliedConfigAnnotation:
				continue
			default:
				annotations[k] = v
			}
		}
		postgresCluster.Annotations = annotations
		postgresCluster.Labels = cr.Labels

		postgresCluster.Spec.Image = cr.Spec.Image
		postgresCluster.Spec.ImagePullPolicy = cr.Spec.ImagePullPolicy
		postgresCluster.Spec.ImagePullSecrets = cr.Spec.ImagePullSecrets

		postgresCluster.Spec.PostgresVersion = cr.Spec.PostgresVersion
		postgresCluster.Spec.Port = cr.Spec.Port
		postgresCluster.Spec.OpenShift = cr.Spec.OpenShift
		postgresCluster.Spec.Paused = cr.Spec.Paused
		postgresCluster.Spec.Shutdown = cr.Spec.Shutdown
		postgresCluster.Spec.Standby = cr.Spec.Standby
		postgresCluster.Spec.Service = cr.Spec.Expose.ToCrunchy()

		postgresCluster.Spec.CustomReplicationClientTLSSecret = cr.Spec.Secrets.CustomReplicationClientTLSSecret
		postgresCluster.Spec.CustomTLSSecret = cr.Spec.Secrets.CustomTLSSecret

		postgresCluster.Spec.Backups = cr.Spec.Backups
		postgresCluster.Spec.DataSource = cr.Spec.DataSource
		postgresCluster.Spec.DatabaseInitSQL = cr.Spec.DatabaseInitSQL
		postgresCluster.Spec.Patroni = cr.Spec.Patroni

		if err := r.addPMMSidecar(ctx, cr); err != nil {
			return errors.Wrap(err, "failed to add pmm sidecar")
		}

		postgresCluster.Spec.InstanceSets = cr.Spec.InstanceSets.ToCrunchy()
		postgresCluster.Spec.Users = cr.Spec.Users
		postgresCluster.Spec.Proxy = cr.Spec.Proxy.ToCrunchy()

		return nil
	})
	if err != nil {
		return ctrl.Result{}, errors.Wrap(err, "update/create PostgresCluster")
	}

	if err := r.Client.Get(ctx, client.ObjectKeyFromObject(postgresCluster), postgresCluster); err != nil {
		return ctrl.Result{}, errors.Wrap(err, "get PostgresCluster")
	}

	host, err := r.getHost(ctx, cr)
	if err != nil {
		return ctrl.Result{}, errors.Wrap(err, "get app host")
	}

	cr.Status = v2beta1.PerconaPGClusterStatus{
		State:                 r.getState(&postgresCluster.Status),
		Host:                  host,
		PostgresClusterStatus: postgresCluster.Status,
	}

	if err := r.Client.Status().Update(ctx, cr); err != nil {
		log.Error(err, "failed to update status")
	}

	return ctrl.Result{}, err
}

func (r *PGClusterReconciler) getHost(ctx context.Context, cr *v2beta1.PerconaPGCluster) (string, error) {
	svcName := cr.Name + "-pgbouncer"

	if cr.Spec.Proxy.PGBouncer.ServiceExpose == nil || cr.Spec.Proxy.PGBouncer.ServiceExpose.Type != string(corev1.ServiceTypeLoadBalancer) {
		return svcName + "." + cr.Namespace + ".svc", nil
	}

	svc := &corev1.Service{}
	err := r.Client.Get(ctx, types.NamespacedName{Namespace: cr.Namespace, Name: svcName}, svc)
	if err != nil {
		return "", errors.Wrapf(err, "get %s service", svcName)
	}

	var host string
	for _, i := range svc.Status.LoadBalancer.Ingress {
		host = i.IP
		if len(i.Hostname) > 0 {
			host = i.Hostname
		}
	}

	return host, nil
}

func (r *PGClusterReconciler) getState(status *v1beta1.PostgresClusterStatus) v2beta1.AppState {

	if status.PGBackRest != nil && status.PGBackRest.RepoHost != nil && !status.PGBackRest.RepoHost.Ready {
		return v2beta1.AppStateInit
	}

	if status.Proxy.PGBouncer.ReadyReplicas != status.Proxy.PGBouncer.Replicas {
		return v2beta1.AppStateInit
	}

	for _, is := range status.InstanceSets {
		if is.Replicas != is.ReadyReplicas {
			return v2beta1.AppStateInit
		}
	}

	return v2beta1.AppStateReady
}

func (r *PGClusterReconciler) addPMMSidecar(ctx context.Context, cr *v2beta1.PerconaPGCluster) error {
	if cr.Spec.PMM == nil || !cr.Spec.PMM.Enabled {
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

	if v, ok := pmmSecret.Data[pmm.SecretKey]; !ok || len(v) == 0 {
		log.Info(fmt.Sprintf("Can't enable PMM: %s key doesn't exist in %s secret or empty", pmm.SecretKey, cr.Spec.PMM.Secret))
		return nil
	}

	for i := 0; i < len(cr.Spec.InstanceSets); i++ {
		set := &cr.Spec.InstanceSets[i]
		set.Sidecars = append(set.Sidecars, pmm.SidecarContainer(cr))
	}

	cr.Spec.Users = append(cr.Spec.Users, v1beta1.PostgresUserSpec{
		Name:    pmm.MonitoringUser,
		Options: "SUPERUSER",
	})

	return nil
}
