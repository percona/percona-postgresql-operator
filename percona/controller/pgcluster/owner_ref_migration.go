package pgcluster

import (
	"context"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/percona/percona-postgresql-operator/v3/internal/logging"
	"github.com/percona/percona-postgresql-operator/v3/internal/naming"
	v2 "github.com/percona/percona-postgresql-operator/v3/pkg/apis/pgv2.percona.com/v2"
	"github.com/percona/percona-postgresql-operator/v3/pkg/apis/upstream.pgv2.percona.com/v1beta1"
	"github.com/pkg/errors"
)

//+kubebuilder:rbac:groups="postgres-operator.crunchydata.com",resources="postgresclusters",verbs={get,list,watch}

var childKinds = []schema.GroupVersionKind{
	{Group: "apps", Version: "v1", Kind: "StatefulSet"},
	{Group: "apps", Version: "v1", Kind: "Deployment"},
	{Group: "", Version: "v1", Kind: "Service"},
	{Group: "", Version: "v1", Kind: "ConfigMap"},
	{Group: "", Version: "v1", Kind: "ServiceAccount"},
	{Group: "", Version: "v1", Kind: "Endpoints"},
	{Group: "rbac.authorization.k8s.io", Version: "v1", Kind: "Role"},
	{Group: "rbac.authorization.k8s.io", Version: "v1", Kind: "RoleBinding"},
	{Group: "batch", Version: "v1", Kind: "Job"},
	{Group: "batch", Version: "v1", Kind: "CronJob"},
	{Group: "policy", Version: "v1", Kind: "PodDisruptionBudget"},
	{Group: "cert-manager.io", Version: "v1", Kind: "Certificate"},
	{Group: "cert-manager.io", Version: "v1", Kind: "Issuer"},
}

// LegacyGVK is the GroupVersionKind of the pre-rename PostgresCluster.
var LegacyGVK = schema.GroupVersionKind{
	Group:   "postgres-operator.crunchydata.com",
	Version: "v1beta1",
	Kind:    "PostgresCluster",
}

var errOwnerRefMigrationInProgress = errors.New("owner ref migration is in progress")

func (r *PGClusterReconciler) reconcileOwnerRefMigrationStatus(ctx context.Context, cr *v2.PerconaPGCluster) error {
	log := logging.FromContext(ctx).WithName("APIGroupMigration")

	if c := meta.FindStatusCondition(cr.Status.Conditions, "APIGroupMigration"); c != nil && c.Status == metav1.ConditionTrue {
		return nil
	}

	mapper := r.Client.RESTMapper()
	if _, err := mapper.RESTMapping(LegacyGVK.GroupKind(), LegacyGVK.Version); err != nil {
		if meta.IsNoMatchError(err) {
			log.Info("legacy PostgresCluster CRD not found; no need to check references", "gvk", LegacyGVK)

			err := setStatusCondition(ctx, r.Client, cr, metav1.Condition{
				Type:               "APIGroupMigration",
				Status:             metav1.ConditionTrue,
				Reason:             "APIGroupMigrationNotNeeded",
				Message:            "Legacy API group is not installed",
				ObservedGeneration: cr.GetGeneration(),
			})
			if err != nil {
				return errors.Wrap(err, "set status condition to true")
			}

			return nil
		}
		return errors.Wrap(err, "discover legacy PostgresCluster GVK")
	}

	legacy := &unstructured.Unstructured{}
	legacy.SetGroupVersionKind(LegacyGVK)

	if err := r.Client.Get(ctx, types.NamespacedName{Name: cr.Name, Namespace: cr.Namespace}, legacy); err != nil {
		if apierrors.IsNotFound(err) {
			err := setStatusCondition(ctx, r.Client, cr, metav1.Condition{
				Type:               "APIGroupMigration",
				Status:             metav1.ConditionTrue,
				Reason:             "APIGroupMigrationNotNeeded",
				Message:            "Legacy PostgresCluster not found",
				ObservedGeneration: cr.GetGeneration(),
			})
			if err != nil {
				return errors.Wrap(err, "set status condition to true")
			}

			return nil
		}

		return errors.Wrap(err, "get legacy PostgresCluster")
	}

	err := setStatusCondition(ctx, r.Client, cr, metav1.Condition{
		Type:               "APIGroupMigration",
		Status:             metav1.ConditionFalse,
		Reason:             "APIGroupMigrationInProgress",
		Message:            "Waiting for owner references to be updated",
		ObservedGeneration: cr.GetGeneration(),
	})
	if err != nil {
		return errors.Wrap(err, "set status condition to false")
	}

	newPC := &v1beta1.PostgresCluster{}
	err = r.Client.Get(ctx, types.NamespacedName{Name: cr.Name, Namespace: cr.Namespace}, newPC)
	switch {
	case apierrors.IsNotFound(err):
		log.Info("new-group PostgresCluster not present yet; waiting for PerconaPGCluster reconciler to create it")
		return errOwnerRefMigrationInProgress
	case err != nil:
		return errors.Wrap(err, "get new PostgresCluster")
	}

	legacyUID := legacy.GetUID()

	selector := client.MatchingLabels{naming.LabelCluster: legacy.GetName()}
	namespace := client.InNamespace(legacy.GetNamespace())

	for _, gvk := range childKinds {
		if _, err := mapper.RESTMapping(gvk.GroupKind(), gvk.Version); err != nil {
			if meta.IsNoMatchError(err) {
				log.V(1).Info("Skipping kind: NoMatchError", "gvk", gvk)
				continue
			}
			return errors.Wrapf(err, "discover %s", gvk)
		}

		list := &unstructured.UnstructuredList{}
		list.SetGroupVersionKind(gvk)
		if err := r.Client.List(ctx, list, namespace, selector); err != nil {
			return errors.Wrapf(err, "list %s", gvk.Kind)
		}

		for i := range list.Items {
			child := &list.Items[i]

			if ownedBy(child, legacyUID) {
				log.Info("Child is still owned by legacy cluster", "child", child.GetName(), "kind", child.GetKind())
				return errOwnerRefMigrationInProgress
			}

			log.Info("Child is updated successfully", "child", child.GetName(), "kind", child.GetKind())
		}
	}

	err = setStatusCondition(ctx, r.Client, cr, metav1.Condition{
		Type:               "APIGroupMigration",
		Status:             metav1.ConditionTrue,
		Reason:             "APIGroupMigrationCompleted",
		Message:            "All owner references are updated",
		ObservedGeneration: cr.GetGeneration(),
	})
	if err != nil {
		return errors.Wrap(err, "set status condition to true")
	}

	return nil
}

// ownedBy reports whether the object has an ownerReference with the given UID.
func ownedBy(obj *unstructured.Unstructured, uid types.UID) bool {
	for _, ref := range obj.GetOwnerReferences() {
		if ref.UID == uid {
			return true
		}
	}
	return false
}

// setStatusCondition immediately sets condition and immediately updates object in K8s.
func setStatusCondition(ctx context.Context, cl client.Client, cr *v2.PerconaPGCluster, cond metav1.Condition) error {
	orig := cr.DeepCopy()

	meta.SetStatusCondition(&cr.Status.Conditions, cond)

	return cl.Status().Patch(ctx, cr, client.MergeFrom(orig))
}
