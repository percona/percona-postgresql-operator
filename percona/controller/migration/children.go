package migration

import (
	"context"

	"github.com/pkg/errors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/percona/percona-postgresql-operator/v2/internal/logging"
	"github.com/percona/percona-postgresql-operator/v2/internal/naming"
	"github.com/percona/percona-postgresql-operator/v2/pkg/apis/upstream.pgv2.percona.com/v1beta1"
)

// childKinds is the set of resource kinds created by the upstream
// PostgresCluster reconciler and therefore potentially owned by a legacy
// PostgresCluster object.
var childKinds = []schema.GroupVersionKind{
	{Group: "apps", Version: "v1", Kind: "StatefulSet"},
	{Group: "apps", Version: "v1", Kind: "Deployment"},
	{Group: "", Version: "v1", Kind: "Service"},
	{Group: "", Version: "v1", Kind: "Secret"},
	{Group: "", Version: "v1", Kind: "ConfigMap"},
	{Group: "", Version: "v1", Kind: "PersistentVolumeClaim"},
	{Group: "", Version: "v1", Kind: "ServiceAccount"},
	{Group: "", Version: "v1", Kind: "Endpoints"},
	{Group: "rbac.authorization.k8s.io", Version: "v1", Kind: "Role"},
	{Group: "rbac.authorization.k8s.io", Version: "v1", Kind: "RoleBinding"},
	{Group: "batch", Version: "v1", Kind: "Job"},
	{Group: "batch", Version: "v1", Kind: "CronJob"},
	{Group: "policy", Version: "v1", Kind: "PodDisruptionBudget"},
	// Optional — only patched when the CRD is installed.
	{Group: "cert-manager.io", Version: "v1", Kind: "Certificate"},
	{Group: "cert-manager.io", Version: "v1", Kind: "Issuer"},
	{Group: "snapshot.storage.k8s.io", Version: "v1", Kind: "VolumeSnapshot"},
}

// reparentChildren walks every childKind looking for objects labeled as part
// of the cluster and owned by the legacy PostgresCluster. Matching objects
// have their ownerReferences rewritten to point at newPC. The return value is
// the number of children that could not be re-parented (typically zero; >0
// indicates the caller should requeue).
func (r *Reconciler) reparentChildren(
	ctx context.Context, legacy *unstructured.Unstructured, newPC *v1beta1.PostgresCluster,
) (int, error) {
	log := logging.FromContext(ctx)
	mapper := r.Client.RESTMapper()

	legacyUID := legacy.GetUID()
	newOwner := ownerReferenceFor(newPC)

	selector := client.MatchingLabels{naming.LabelCluster: legacy.GetName()}
	namespace := client.InNamespace(legacy.GetNamespace())

	remaining := 0
	for _, gvk := range childKinds {
		if _, err := mapper.RESTMapping(gvk.GroupKind(), gvk.Version); err != nil {
			if meta.IsNoMatchError(err) {
				continue
			}
			return 0, errors.Wrapf(err, "discover %s", gvk)
		}

		list := &unstructured.UnstructuredList{}
		list.SetGroupVersionKind(gvk)
		if err := r.Client.List(ctx, list, namespace, selector); err != nil {
			return 0, errors.Wrapf(err, "list %s", gvk.Kind)
		}

		for i := range list.Items {
			child := &list.Items[i]
			if !ownedBy(child, legacyUID) {
				continue
			}

			if err := r.replaceOwnerReference(ctx, child, legacyUID, newOwner); err != nil {
				if apierrors.IsConflict(err) {
					remaining++
					continue
				}
				return 0, errors.Wrapf(err, "re-parent %s %s/%s",
					gvk.Kind, child.GetNamespace(), child.GetName())
			}
			log.Info("re-parented child to new PostgresCluster",
				"kind", gvk.Kind, "name", child.GetName())
		}
	}

	return remaining, nil
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

// replaceOwnerReference patches obj, removing any ownerReference whose UID is
// legacyUID and (if not already present) adding newOwner. The patch is a JSON
// merge patch of the entire ownerReferences array, which is the only sound
// way to remove individual entries.
func (r *Reconciler) replaceOwnerReference(
	ctx context.Context, obj *unstructured.Unstructured, legacyUID types.UID, newOwner metav1.OwnerReference,
) error {
	base := obj.DeepCopy()

	refs := obj.GetOwnerReferences()
	filtered := make([]metav1.OwnerReference, 0, len(refs))
	for _, ref := range refs {
		if ref.UID == legacyUID {
			continue
		}
		filtered = append(filtered, ref)
	}

	alreadyHasNewOwner := false
	for _, ref := range filtered {
		if ref.UID == newOwner.UID {
			alreadyHasNewOwner = true
			break
		}
	}
	if !alreadyHasNewOwner {
		filtered = append(filtered, newOwner)
	}

	obj.SetOwnerReferences(filtered)
	return r.Client.Patch(ctx, obj, client.MergeFrom(base))
}
