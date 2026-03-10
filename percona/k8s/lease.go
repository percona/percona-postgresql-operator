package k8s

import (
	"context"
	"time"

	"github.com/pkg/errors"
	coordinationv1 "k8s.io/api/coordination/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

var ErrLeaseAlreadyHeld = errors.New("lease held by another holder")

// IsHolderStaleFunc determines whether the current lease holder is stale
// and can be evicted. Called with the current holder's identity.
type IsHolderStaleFunc func(ctx context.Context, currentHolder string) (bool, error)

// +kubebuilder:rbac:groups=coordination.k8s.io,resources=leases,verbs=get;list

func GetLease(ctx context.Context, client client.Client, leaseName, namespace string) (*coordinationv1.Lease, error) {
	lease := &coordinationv1.Lease{}
	if err := client.Get(ctx, types.NamespacedName{Name: leaseName, Namespace: namespace}, lease); err != nil {
		return nil, err
	}
	return lease, nil
}

// +kubebuilder:rbac:groups=coordination.k8s.io,resources=leases,verbs=create;update

// AcquireLease attempts to acquire a named lease for the given holder.
// If isHolderStale is non-nil and the lease is held by a different holder,
// the callback is invoked to determine whether the current holder is stale.
// A stale holder is evicted atomically and the lease is granted to the new holder.
func AcquireLease(ctx context.Context, cl client.Client, leaseName, holder, namespace string, checkStale IsHolderStaleFunc) error {
	lease := &coordinationv1.Lease{
		ObjectMeta: metav1.ObjectMeta{
			Name:      leaseName,
			Namespace: namespace,
		},
	}

	_, err := controllerutil.CreateOrUpdate(ctx, cl, lease, func() error {
		if lease.Spec.HolderIdentity != nil {
			if *lease.Spec.HolderIdentity == holder {
				return nil // already held
			}

			if checkStale == nil {
				return ErrLeaseAlreadyHeld
			}

			stale, err := checkStale(ctx, *lease.Spec.HolderIdentity)
			if err != nil {
				return errors.Wrap(err, "failed to check if lease holder is stale")
			}
			if !stale {
				return ErrLeaseAlreadyHeld
			}
		}

		lease.Spec.HolderIdentity = &holder
		lease.Spec.AcquireTime = &metav1.MicroTime{Time: time.Now()}
		return nil
	})
	return err
}

// +kubebuilder:rbac:groups=coordination.k8s.io,resources=leases,verbs=get;delete

// ReleaseLease deletes the lease only if it is still held by the given holder.
// UID and ResourceVersion preconditions prevent accidentally deleting a lease
// that was re-created by a concurrent acquirer between the Get and Delete.
func ReleaseLease(ctx context.Context, cl client.Client, leaseName, holder, namespace string) error {
	lease, err := GetLease(ctx, cl, leaseName, namespace)
	if k8serrors.IsNotFound(err) {
		return nil
	} else if err != nil {
		return err
	}

	if holderID := lease.Spec.HolderIdentity; holderID != nil && *holderID != holder {
		return ErrLeaseAlreadyHeld
	}

	if err := cl.Delete(ctx, lease, &client.DeleteOptions{
		Preconditions: &metav1.Preconditions{
			UID:             &lease.UID,
			ResourceVersion: &lease.ResourceVersion,
		},
	}); err != nil {
		if k8serrors.IsNotFound(err) || k8serrors.IsConflict(err) {
			return nil
		}
		return errors.Wrap(err, "delete lease")
	}
	return nil
}
