package pgcluster

import (
	"context"
	"fmt"

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/percona/percona-postgresql-operator/internal/logging"
	"github.com/percona/percona-postgresql-operator/internal/naming"
	"github.com/percona/percona-postgresql-operator/pkg/apis/pg.percona.com/v2beta1"
)

type finalizerFunc func(context.Context, *v2beta1.PerconaPGCluster) error

func (r *PGClusterReconciler) deletePVC(ctx context.Context, cr *v2beta1.PerconaPGCluster) error {
	log := logging.FromContext(ctx)

	pvcList := corev1.PersistentVolumeClaimList{}

	err := r.Client.List(ctx, &pvcList, &client.ListOptions{
		Namespace: cr.Namespace,
		LabelSelector: labels.SelectorFromSet(map[string]string{
			naming.LabelCluster: cr.Name,
		}),
	})
	if err != nil {
		return errors.Wrap(err, "get PVC list")
	}

	for i, pvc := range pvcList.Items {
		log.Info("Deleting PVC", "name", pvc.Name)
		if err := r.Client.Delete(ctx, &pvcList.Items[i]); client.IgnoreNotFound(err) != nil {
			return errors.Wrapf(err, "delete PVC %s", pvc.Name)
		}
	}

	return nil
}

func (r *PGClusterReconciler) deleteUserSecrets(ctx context.Context, cr *v2beta1.PerconaPGCluster) error {
	log := logging.FromContext(ctx)

	secretList := corev1.SecretList{}

	err := r.Client.List(ctx, &secretList, &client.ListOptions{
		Namespace: cr.Namespace,
		LabelSelector: labels.SelectorFromSet(map[string]string{
			naming.LabelCluster: cr.Name,
			naming.LabelRole:    naming.RolePostgresUser,
		}),
	})
	if err != nil {
		return errors.Wrap(err, "get secret list")
	}

	for i, secret := range secretList.Items {
		log.Info("Deleting secret", "name", secret.Name)
		if err := r.Client.Delete(ctx, &secretList.Items[i]); client.IgnoreNotFound(err) != nil {
			return errors.Wrapf(err, "delete secret %s", secret.Name)
		}
	}

	return nil
}

func (r *PGClusterReconciler) deleteTLSSecrets(ctx context.Context, cr *v2beta1.PerconaPGCluster) error {
	log := logging.FromContext(ctx)

	secrets := []corev1.Secret{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      cr.Name + "-pgbackrest",
				Namespace: cr.Namespace,
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      cr.Name + "-pgbouncer",
				Namespace: cr.Namespace,
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf(naming.ClusterCertSecret, cr.Name),
				Namespace: cr.Namespace,
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      naming.RootCertSecret,
				Namespace: cr.Namespace,
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      cr.Name + "-replication-cert",
				Namespace: cr.Namespace,
			},
		},
	}

	for _, instance := range cr.Spec.InstanceSets {
		secretList := corev1.SecretList{}
		err := r.Client.List(ctx, &secretList, &client.ListOptions{
			Namespace: cr.Namespace,
			LabelSelector: labels.SelectorFromSet(map[string]string{
				naming.LabelCluster:     cr.Name,
				naming.LabelInstanceSet: instance.Name,
			}),
		})
		if err != nil {
			return errors.Wrap(err, "get instance TLS secrets")
		}

		secrets = append(secrets, secretList.Items...)
	}

	for i, secret := range secrets {
		log.Info("Deleting secret", "name", secret.Name)
		if err := r.Client.Delete(ctx, &secrets[i]); client.IgnoreNotFound(err) != nil {
			return errors.Wrapf(err, "delete secret %s", secret.Name)
		}
	}

	return nil
}

func (r *PGClusterReconciler) deletePVCAndSecrets(ctx context.Context, cr *v2beta1.PerconaPGCluster) error {
	if err := r.deletePVC(ctx, cr); err != nil {
		return err
	}

	if err := r.deleteUserSecrets(ctx, cr); err != nil {
		return err
	}

	return nil
}

func (r *PGClusterReconciler) runFinalizers(ctx context.Context, cr *v2beta1.PerconaPGCluster) error {
	if err := r.runFinalizer(ctx, cr, v2beta1.FinalizerDeletePVC, r.deletePVCAndSecrets); err != nil {
		return errors.Wrapf(err, "run finalizer %s", v2beta1.FinalizerDeletePVC)
	}

	if err := r.runFinalizer(ctx, cr, v2beta1.FinalizerDeleteSSL, r.deleteTLSSecrets); err != nil {
		return errors.Wrapf(err, "run finalizer %s", v2beta1.FinalizerDeleteSSL)
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
		return errors.Wrapf(err, "run finalizer %s", finalizer)
	}

	if controllerutil.RemoveFinalizer(cr, finalizer) {
		if err := r.Client.Patch(ctx, cr, client.MergeFrom(orig)); err != nil {
			return errors.Wrap(err, "remove finalizers")
		}
	}

	return nil
}
