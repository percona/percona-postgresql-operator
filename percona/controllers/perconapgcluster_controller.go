package controllers

import (
	"context"

	"github.com/pkg/errors"
	"go.opentelemetry.io/otel/trace"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

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
	Client   client.Client
	Owner    client.FieldOwner
	Recorder record.EventRecorder
	Tracer   trace.Tracer
}

// +kubebuilder:rbac:groups=pg.percona.com,resources=perconapgclusters,verbs=get;list;watch
// +kubebuilder:rbac:groups=pg.percona.com,resources=perconapgclusters/status,verbs=patch;update
// +kubebuilder:rbac:groups=postgres-operator.crunchydata.com,resources=postgresclusters,verbs=get;list;create;update;patch;watch

func (r *PGClusterReconciler) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	log := logging.FromContext(ctx)

	log.Info("Reconciling", "request", request)

	perconaPGCluster := &v2beta1.PerconaPGCluster{}
	if err := r.Client.Get(ctx, request.NamespacedName, perconaPGCluster); err != nil {
		// NotFound cannot be fixed by requeuing so ignore it. During background
		// deletion, we receive delete events from cluster's dependents after
		// cluster is deleted.
		if err = client.IgnoreNotFound(err); err != nil {
			log.Error(err, "unable to fetch PerconaPGCluster")
		}
		return reconcile.Result{}, err
	}

	log.Info("Reconciled", "request", request)

	postgresCluster := &v1beta1.PostgresCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      perconaPGCluster.Name,
			Namespace: perconaPGCluster.Namespace,
		},
	}

	if err := controllerutil.SetControllerReference(perconaPGCluster, postgresCluster, r.Client.Scheme()); err != nil {
		return reconcile.Result{}, err
	}

	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, postgresCluster, func() error {
		postgresCluster.Default()

		annotations := make(map[string]string)
		for k, v := range perconaPGCluster.Annotations {
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
		postgresCluster.Labels = perconaPGCluster.Labels

		postgresCluster.Spec.Image = perconaPGCluster.Spec.Image
		postgresCluster.Spec.ImagePullPolicy = perconaPGCluster.Spec.ImagePullPolicy
		postgresCluster.Spec.ImagePullSecrets = perconaPGCluster.Spec.ImagePullSecrets

		postgresCluster.Spec.PostgresVersion = perconaPGCluster.Spec.PostgresVersion
		postgresCluster.Spec.Port = perconaPGCluster.Spec.Port
		postgresCluster.Spec.OpenShift = perconaPGCluster.Spec.OpenShift
		postgresCluster.Spec.Paused = perconaPGCluster.Spec.Paused
		postgresCluster.Spec.Standby = perconaPGCluster.Spec.Standby
		postgresCluster.Spec.Service = perconaPGCluster.Spec.Expose.ToCrunchy()

		postgresCluster.Spec.CustomReplicationClientTLSSecret = perconaPGCluster.Spec.Secrets.CustomReplicationClientTLSSecret
		postgresCluster.Spec.CustomTLSSecret = perconaPGCluster.Spec.Secrets.CustomTLSSecret

		postgresCluster.Spec.Backups = perconaPGCluster.Spec.Backups
		postgresCluster.Spec.DataSource = perconaPGCluster.Spec.DataSource
		postgresCluster.Spec.DatabaseInitSQL = perconaPGCluster.Spec.DatabaseInitSQL
		postgresCluster.Spec.Patroni = perconaPGCluster.Spec.Patroni

		if perconaPGCluster.Spec.PMM != nil && perconaPGCluster.Spec.PMM.Enabled {
			// TODO: Check if PMM secret exists
			for i := 0; i < len(perconaPGCluster.Spec.InstanceSets); i++ {
				set := &perconaPGCluster.Spec.InstanceSets[i]
				set.Sidecars = append(set.Sidecars, pmm.SidecarContainer(perconaPGCluster))
			}
		}

		postgresCluster.Spec.InstanceSets = perconaPGCluster.Spec.InstanceSets.ToCrunchy()
		postgresCluster.Spec.Users = perconaPGCluster.Spec.Users
		postgresCluster.Spec.Proxy = perconaPGCluster.Spec.Proxy.ToCrunchy()

		return nil
	})

	if err := r.Client.Get(ctx, client.ObjectKeyFromObject(postgresCluster), postgresCluster); err != nil {
		return reconcile.Result{}, errors.Wrap(err, "get PostgresCluster")
	}

	perconaPGCluster.Status = v2beta1.PerconaPGClusterStatus{
		PostgresClusterStatus: postgresCluster.Status,
	}

	if err := r.Client.Status().Update(ctx, perconaPGCluster); err != nil {
		log.Error(err, "failed to update status")
	}

	return reconcile.Result{}, err
}

// SetupWithManager adds the PerconaPGCluster controller to the provided runtime manager
func (r *PGClusterReconciler) SetupWithManager(mgr manager.Manager) error {
	return builder.ControllerManagedBy(mgr).For(&v2beta1.PerconaPGCluster{}).Owns(&v1beta1.PostgresCluster{}).Complete(r)
}
