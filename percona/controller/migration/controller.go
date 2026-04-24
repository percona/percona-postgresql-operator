// Package migration re-parents legacy postgres-operator.crunchydata.com/v1beta1
// PostgresCluster objects to the renamed upstream.pgv2.percona.com/v1beta1
// group so that existing clusters can continue serving traffic after the
// operator upgrade that renamed the CRDs.
package migration

import (
	"context"
	"time"

	"github.com/pkg/errors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/percona/percona-postgresql-operator/v2/internal/logging"
	"github.com/percona/percona-postgresql-operator/v2/pkg/apis/upstream.pgv2.percona.com/v1beta1"
)

const (
	ControllerName = "legacy-postgrescluster-migration-controller"

	// MigrationFinalizer is set on the legacy PostgresCluster while we are
	// re-parenting its children. It blocks the Kubernetes GC from cascading
	// the delete to child objects mid-migration.
	MigrationFinalizer = "migration.percona.com/legacy-postgrescluster"

	requeueAfter = 5 * time.Second
)

// LegacyGVK is the GroupVersionKind of the pre-rename PostgresCluster.
var LegacyGVK = schema.GroupVersionKind{
	Group:   "postgres-operator.crunchydata.com",
	Version: "v1beta1",
	Kind:    "PostgresCluster",
}

// Reconciler re-parents children of a legacy PostgresCluster to the new-group
// PostgresCluster and then deletes the legacy object.
type Reconciler struct {
	Client client.Client
}

// SetupWithManager registers the controller with the manager. When the legacy
// CRD is not installed in the cluster (fresh installs or already-migrated
// clusters), registration is skipped and reconciliation becomes a no-op.
func (r *Reconciler) SetupWithManager(mgr manager.Manager) error {
	log := logging.FromContext(context.Background()).WithName(ControllerName)

	if _, err := mgr.GetRESTMapper().RESTMapping(LegacyGVK.GroupKind(), LegacyGVK.Version); err != nil {
		if meta.IsNoMatchError(err) {
			log.Info("legacy PostgresCluster CRD not found; migration controller will not start",
				"gvk", LegacyGVK)
			return nil
		}
		return errors.Wrap(err, "discover legacy PostgresCluster GVK")
	}

	legacy := &unstructured.Unstructured{}
	legacy.SetGroupVersionKind(LegacyGVK)

	return builder.ControllerManagedBy(mgr).
		Named(ControllerName).
		For(legacy).
		Complete(r)
}

// +kubebuilder:rbac:groups=postgres-operator.crunchydata.com,resources=postgresclusters,verbs=get;list;watch;update;patch;delete
// +kubebuilder:rbac:groups=postgres-operator.crunchydata.com,resources=postgresclusters/finalizers,verbs=update
// +kubebuilder:rbac:groups=upstream.pgv2.percona.com,resources=postgresclusters,verbs=get;list;watch

func (r *Reconciler) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	log := logging.FromContext(ctx).WithValues("cluster", request.Name, "namespace", request.Namespace)

	legacy := &unstructured.Unstructured{}
	legacy.SetGroupVersionKind(LegacyGVK)
	if err := r.Client.Get(ctx, request.NamespacedName, legacy); err != nil {
		return reconcile.Result{}, client.IgnoreNotFound(err)
	}

	newPC := &v1beta1.PostgresCluster{}
	err := r.Client.Get(ctx, request.NamespacedName, newPC)
	switch {
	case apierrors.IsNotFound(err):
		log.Info("new-group PostgresCluster not present yet; waiting for PerconaPGCluster reconciler to create it")
		return reconcile.Result{RequeueAfter: requeueAfter}, nil
	case err != nil:
		return reconcile.Result{}, errors.Wrap(err, "get new PostgresCluster")
	}

	if legacy.GetDeletionTimestamp().IsZero() {
		if added, err := r.ensureMigrationFinalizer(ctx, legacy); err != nil {
			return reconcile.Result{}, errors.Wrap(err, "ensure migration finalizer")
		} else if added {
			return reconcile.Result{Requeue: true}, nil
		}
	}

	remaining, err := r.reparentChildren(ctx, legacy, newPC)
	if err != nil {
		return reconcile.Result{}, errors.Wrap(err, "reparent children")
	}
	if remaining > 0 {
		log.Info("children still owned by legacy PostgresCluster; requeueing", "remaining", remaining)
		return reconcile.Result{RequeueAfter: requeueAfter}, nil
	}

	if err := r.removeFinalizers(ctx, legacy); err != nil {
		return reconcile.Result{}, errors.Wrap(err, "strip finalizers from legacy PostgresCluster")
	}

	orphan := metav1.DeletePropagationOrphan
	if err := r.Client.Delete(ctx, legacy, &client.DeleteOptions{PropagationPolicy: &orphan}); err != nil {
		return reconcile.Result{}, errors.Wrap(err, "delete legacy PostgresCluster")
	}

	log.Info("legacy PostgresCluster migrated to new API group", "newUID", newPC.UID)
	return reconcile.Result{}, nil
}

// ensureMigrationFinalizer adds MigrationFinalizer to the legacy object if it
// is not already present. Returns true if it was added.
func (r *Reconciler) ensureMigrationFinalizer(ctx context.Context, legacy *unstructured.Unstructured) (bool, error) {
	for _, f := range legacy.GetFinalizers() {
		if f == MigrationFinalizer {
			return false, nil
		}
	}

	patch := client.MergeFrom(legacy.DeepCopy())
	legacy.SetFinalizers(append(legacy.GetFinalizers(), MigrationFinalizer))
	return true, r.Client.Patch(ctx, legacy, patch)
}

// removeFinalizers clears every finalizer from the legacy object. The legacy
// upstream finalizer (naming.Finalizer) will otherwise never be removed
// because the old operator is gone; this controller takes responsibility for
// its cleanup.
func (r *Reconciler) removeFinalizers(ctx context.Context, legacy *unstructured.Unstructured) error {
	if len(legacy.GetFinalizers()) == 0 {
		return nil
	}
	patch := client.MergeFrom(legacy.DeepCopy())
	legacy.SetFinalizers(nil)
	return r.Client.Patch(ctx, legacy, patch)
}

// ownerReferenceFor returns a fresh controller OwnerReference pointing at the
// new-group PostgresCluster.
func ownerReferenceFor(newPC *v1beta1.PostgresCluster) metav1.OwnerReference {
	tru := true
	gvk := v1beta1.GroupVersion.WithKind("PostgresCluster")
	return metav1.OwnerReference{
		APIVersion:         gvk.GroupVersion().String(),
		Kind:               gvk.Kind,
		Name:               newPC.Name,
		UID:                newPC.UID,
		Controller:         &tru,
		BlockOwnerDeletion: &tru,
	}
}

