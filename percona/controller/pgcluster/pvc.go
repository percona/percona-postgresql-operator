package pgcluster

import (
	"context"

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/percona/percona-postgresql-operator/v2/internal/naming"
	v2 "github.com/percona/percona-postgresql-operator/v2/pkg/apis/pgv2.percona.com/v2"
	"github.com/percona/percona-postgresql-operator/v2/pkg/apis/postgres-operator.crunchydata.com/v1beta1"
)

func (r *PGClusterReconciler) reconcilePVCs(ctx context.Context, cr *v2.PerconaPGCluster) error {
	if cr.CompareVersion("2.9.0") < 0 {
		return nil
	}
	for _, set := range cr.Spec.InstanceSets {
		ls := naming.Merge(
			cr.Spec.Metadata.GetLabelsOrNil(),
			set.Metadata.GetLabelsOrNil(),
			naming.WithPerconaLabels(
				map[string]string{
					naming.LabelCluster:     cr.Name,
					naming.LabelInstanceSet: set.Name,
					naming.LabelData:        naming.DataPostgres,
				},
				cr.GetName(), "pg", cr.Spec.CRVersion),
		)
		if err := ensureSidecarPVCs(ctx, r.Client, cr, set.SidecarPVCs, ls); err != nil {
			return errors.Wrap(err, "failed to create instance set pvcs")
		}
	}
	if cr.Spec.Backups.PGBackRest.RepoHost != nil && len(cr.Spec.Backups.PGBackRest.RepoHost.SidecarPVCs) > 0 {
		ls := naming.Merge(
			cr.Spec.Metadata.GetLabelsOrNil(),
			cr.Spec.Backups.PGBackRest.Metadata.GetLabelsOrNil(),
			naming.WithPerconaLabels(
				naming.PGBackRestLabels(cr.GetName()),
				cr.GetName(), "", cr.Spec.CRVersion),
		)
		if err := ensureSidecarPVCs(ctx, r.Client, cr, cr.Spec.Backups.PGBackRest.RepoHost.SidecarPVCs, ls); err != nil {
			return errors.Wrap(err, "failed to create repo host sidecar pvcs")
		}
	}
	if cr.Spec.Proxy != nil && cr.Spec.Proxy.PGBouncer != nil && len(cr.Spec.Proxy.PGBouncer.SidecarPVCs) > 0 {
		ls := naming.Merge(
			cr.Spec.Metadata.GetLabelsOrNil(),
			cr.Spec.Proxy.PGBouncer.Metadata.GetLabelsOrNil(),
			naming.WithPerconaLabels(map[string]string{
				naming.LabelCluster: cr.Name,
				naming.LabelRole:    naming.RolePGBouncer,
			}, cr.Name, "pgbouncer", cr.Spec.CRVersion),
		)
		if err := ensureSidecarPVCs(ctx, r.Client, cr, cr.Spec.Proxy.PGBouncer.SidecarPVCs, ls); err != nil {
			return errors.Wrap(err, "failed to create pgbouncer sidecar pvcs")
		}
	}

	return nil
}

func ensureSidecarPVCs(
	ctx context.Context,
	cl client.Client,
	cr *v2.PerconaPGCluster,
	pvcs []v1beta1.SidecarPVC,
	ls map[string]string,
) error {
	for _, sidecarPVC := range pvcs {
		pvc := new(corev1.PersistentVolumeClaim)
		pvc.Name = sidecarPVC.Name
		pvc.Namespace = cr.Namespace
		pvc.Spec = sidecarPVC.Spec
		pvc.Labels = ls

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
