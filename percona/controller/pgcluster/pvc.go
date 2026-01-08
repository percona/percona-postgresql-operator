package pgcluster

import (
	"context"

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v2 "github.com/percona/percona-postgresql-operator/v2/pkg/apis/pgv2.percona.com/v2"
	"github.com/percona/percona-postgresql-operator/v2/pkg/apis/postgres-operator.crunchydata.com/v1beta1"
)

func (r *PGClusterReconciler) reconcilePVCs(ctx context.Context, cr *v2.PerconaPGCluster) error {
	if cr.CompareVersion("2.9.0") < 0 {
		return nil
	}
	if cr.Spec.Proxy != nil && cr.Spec.Proxy.PGBouncer != nil && len(cr.Spec.Proxy.PGBouncer.SidecarPVCs) > 0 {
		// PGBouncer is a deployment. Operator should manually create a PVC
		if err := ensureSidecarPVCs(ctx, r.Client, cr.Namespace, cr.Spec.Proxy.PGBouncer.SidecarPVCs); err != nil {
			return errors.Wrap(err, "failed to create pgbouncer sidecar pvcs")
		}
	}

	return nil
}

func ensureSidecarPVCs(
	ctx context.Context,
	cl client.Client,
	namespace string,
	pvcs []v1beta1.SidecarPVC,
) error {
	for _, sidecarPVC := range pvcs {
		pvc := new(corev1.PersistentVolumeClaim)
		pvc.Name = sidecarPVC.Name
		pvc.Namespace = namespace
		pvc.Spec = sidecarPVC.Spec

		err := cl.Get(ctx, client.ObjectKeyFromObject(pvc), &corev1.PersistentVolumeClaim{})
		if err == nil {
			// already exists
			continue
		}

		if !k8serrors.IsNotFound(err) {
			return errors.Wrapf(err, "get %s", client.ObjectKeyFromObject(pvc).String())
		}

		if err := cl.Create(ctx, pvc); err != nil {
			return errors.Wrapf(err, "create PVC %s", client.ObjectKeyFromObject(pvc).String())
		}
	}

	return nil
}
