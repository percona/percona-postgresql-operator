package k8s

import (
	"context"
	"time"

	"github.com/pkg/errors"
	coordv1 "k8s.io/api/coordination/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

var ErrLeaseAlreadyHeld = errors.New("lease held by another holder")

// +kubebuilder:rbac:groups=coordination.k8s.io,resources=leases,verbs=get;list
func GetLease(ctx context.Context, client client.Client, leaseName, namespace string) (*coordv1.Lease, error) {
	lease := &coordv1.Lease{}
	if err := client.Get(ctx, types.NamespacedName{Name: leaseName, Namespace: namespace}, lease); err != nil {
		return nil, err
	}
	return lease, nil
}

// +kubebuilder:rbac:groups=coordination.k8s.io,resources=leases,verbs=create;update
func AcquireLease(ctx context.Context, client client.Client, leaseName, holder, namespace string) error {
	lease := &coordv1.Lease{
		ObjectMeta: metav1.ObjectMeta{
			Name:      leaseName,
			Namespace: namespace,
		},
	}

	_, err := controllerutil.CreateOrUpdate(ctx, client, lease, func() error {
		if lease.Spec.HolderIdentity != nil && *lease.Spec.HolderIdentity != holder {
			return errors.Wrap(ErrLeaseAlreadyHeld, "lease already held by another holder")
		}
		lease.Spec.HolderIdentity = &holder
		lease.Spec.AcquireTime = &metav1.MicroTime{Time: time.Now()}
		return nil
	})
	return err
}

// +kubebuilder:rbac:groups=coordination.k8s.io,resources=leases,verbs=get;delete
func ReleaseLease(ctx context.Context, client client.Client, leaseName, holder, namespace string) error {
	lease, err := GetLease(ctx, client, leaseName, namespace)
	if k8serrors.IsNotFound(err) {
		return nil
	} else if err != nil {
		return err
	}

	if holderID := lease.Spec.HolderIdentity; holderID != nil && *holderID != holder {
		return errors.New("lease held by another holder")
	}

	if err := client.Delete(ctx, lease); err != nil {
		return errors.Wrap(err, "delete lease")
	}
	return nil
}
