package pgcluster

import (
	"context"

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/percona/percona-postgresql-operator/internal/logging"
	"github.com/percona/percona-postgresql-operator/pkg/apis/pg.percona.com/v2beta1"
)

type finalizerFunc func(context.Context, *v2beta1.PerconaPGCluster) error

func (r *PGClusterReconciler) deletePVC(ctx context.Context, cr *v2beta1.PerconaPGCluster) error {
	log := logging.FromContext(ctx)

	pvcList := corev1.PersistentVolumeClaimList{}

	err := r.Client.List(ctx, &pvcList, &client.ListOptions{
		Namespace: cr.Namespace,
		LabelSelector: labels.SelectorFromSet(map[string]string{
			"postgres-operator.crunchydata.com/cluster": cr.Name,
		}),
	})
	if err != nil {
		return errors.Wrap(err, "get PVC list")
	}

	for i, pvc := range pvcList.Items {
		log.Info("Deleting PVC", "name", pvc.Name)
		if err := r.Client.Delete(ctx, &pvcList.Items[i]); err != nil {
			return errors.Wrapf(err, "delete PVC %s", pvc.Name)
		}

	}

	return nil
}

func (r *PGClusterReconciler) runFinalizers(ctx context.Context, cr *v2beta1.PerconaPGCluster) error {
	if err := r.runFinalizer(ctx, cr, v2beta1.FinalizerDeletePVC, r.deletePVC); err != nil {
		return errors.Wrapf(err, "run finalizer %s", v2beta1.FinalizerDeletePVC)
	}

	return nil
}

func (r *PGClusterReconciler) runFinalizer(ctx context.Context, cr *v2beta1.PerconaPGCluster, finalizer string, f finalizerFunc) error {
	if !controllerutil.ContainsFinalizer(cr, finalizer) {
		return nil
	}

	log := logging.FromContext(ctx)
	log.Info("Running finalizer", "name", finalizer)

	orig := cr.DeepCopy()

	if err := f(ctx, cr); err != nil {
		return errors.Wrapf(err, "run finalizer %s", v2beta1.FinalizerDeletePVC)
	}

	if controllerutil.RemoveFinalizer(cr, v2beta1.FinalizerDeletePVC) {
		if err := r.Client.Patch(ctx, cr, client.MergeFrom(orig)); err != nil {
			return errors.Wrap(err, "remove finalizers")
		}
	}

	return nil
}
