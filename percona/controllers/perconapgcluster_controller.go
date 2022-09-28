package controllers

import (
	"context"

	"go.opentelemetry.io/otel/trace"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/crunchydata/postgres-operator/internal/logging"
	"github.com/crunchydata/postgres-operator/pkg/apis/pg.percona.com/v2beta1"
	"github.com/crunchydata/postgres-operator/pkg/apis/postgres-operator.crunchydata.com/v1beta1"
)

const (
	// ControllerName is the name of the PostgresCluster controller
	ControllerName = "perconapgcluster-controller"
)

// Reconciler holds resources for the PostgresCluster reconciler
type Reconciler struct {
	Client   client.Client
	Owner    client.FieldOwner
	Recorder record.EventRecorder
	Tracer   trace.Tracer
}

// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch
// +kubebuilder:rbac:groups=pg.percona.com,resources=perconapgclusters,verbs=get;list;watch
// +kubebuilder:rbac:groups=pg.percona.com,resources=perconapgclusters/status,verbs=patch
func (r *Reconciler) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
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
		postgresCluster.Spec.InstanceSets = perconaPGCluster.Spec.InstanceSets.ToCrunchy()
		postgresCluster.Spec.Users = perconaPGCluster.Spec.Users
		postgresCluster.Spec.Proxy = perconaPGCluster.Spec.Proxy.ToCrunchy()

		return nil
	})

	return reconcile.Result{}, err
}

// SetupWithManager adds the PerconaPGCluster controller to the provided runtime manager
func (r *Reconciler) SetupWithManager(mgr manager.Manager) error {
	return builder.ControllerManagedBy(mgr).For(&v2beta1.PerconaPGCluster{}).Complete(r)
}
