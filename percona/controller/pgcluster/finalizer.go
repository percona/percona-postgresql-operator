package pgcluster

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"

	"github.com/percona/percona-postgresql-operator/internal/logging"
	"github.com/percona/percona-postgresql-operator/internal/naming"
	v2 "github.com/percona/percona-postgresql-operator/pkg/apis/pgv2.percona.com/v2"
)

type finalizerFunc func(context.Context, *v2.PerconaPGCluster) error

func (r *PGClusterReconciler) deletePVC(ctx context.Context, cr *v2.PerconaPGCluster) error {
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

func (r *PGClusterReconciler) deleteUserSecrets(ctx context.Context, cr *v2.PerconaPGCluster) error {
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

func (r *PGClusterReconciler) deleteTLSSecrets(ctx context.Context, cr *v2.PerconaPGCluster) error {
	log := logging.FromContext(ctx)

	crunchyCluster, err := cr.ToCrunchy(ctx, nil, r.Client.Scheme())
	if err != nil {
		return errors.Wrap(err, "to crunchy")
	}

	secretsMeta := []metav1.ObjectMeta{
		naming.PGBackRestSecret(crunchyCluster),
		naming.ClusterPGBouncer(crunchyCluster),
	}
	if cr.Spec.Secrets.CustomRootCATLSSecret == nil {
		secretsMeta = append(secretsMeta, metav1.ObjectMeta{Namespace: crunchyCluster.Namespace, Name: naming.RootCertSecret})
		secretsMeta = append(secretsMeta, naming.PostgresRootCASecret(crunchyCluster))
	}
	if cr.Spec.Secrets.CustomTLSSecret == nil {
		secretsMeta = append(secretsMeta, naming.PostgresTLSSecret(crunchyCluster))
	}
	if cr.Spec.Secrets.CustomReplicationClientTLSSecret == nil {
		secretsMeta = append(secretsMeta, naming.ReplicationClientCertSecret(crunchyCluster))
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

		for _, s := range secretList.Items {
			secretsMeta = append(secretsMeta, s.ObjectMeta)
		}
	}

	for _, secret := range secretsMeta {
		log.Info("Deleting secret", "name", secret.Name)
		if err := r.Client.Delete(ctx, &corev1.Secret{ObjectMeta: secret}); client.IgnoreNotFound(err) != nil {
			return errors.Wrapf(err, "delete secret %s", secret.Name)
		}
	}

	return nil
}

func (r *PGClusterReconciler) deletePVCAndSecrets(ctx context.Context, cr *v2.PerconaPGCluster) error {
	if err := r.deletePVC(ctx, cr); err != nil {
		return err
	}

	if err := r.deleteUserSecrets(ctx, cr); err != nil {
		return err
	}

	return nil
}

func (r *PGClusterReconciler) stopExternalWatchers(ctx context.Context, cr *v2.PerconaPGCluster) error {
	log := logging.FromContext(ctx)
	log.Info("Stopping external watchers", "cluster", cr.Name, "namespace", cr.Namespace)

	select {
	case r.StopExternalWatchers <- event.DeleteEvent{Object: cr}:
		log.Info("External watchers are stopped", "cluster", cr.Name, "namespace", cr.Namespace)
	default:
		log.Info("External watchers are already stopped", "cluster", cr.Name, "namespace", cr.Namespace)
	}

	for _, watcherName := range r.Watchers.Names() {
		r.Watchers.Remove(watcherName)
	}

	return nil
}

func (r *PGClusterReconciler) deleteBackups(ctx context.Context, cr *v2.PerconaPGCluster) error {
	log := logging.FromContext(ctx)

	podList := &corev1.PodList{}
	err := r.Client.List(ctx, podList, &client.ListOptions{
		Namespace: cr.Namespace,
		LabelSelector: labels.SelectorFromSet(map[string]string{
			"app.kubernetes.io/instance": cr.Name,
		}),
	})
	if err != nil {
		return err
	}

	if len(podList.Items) == 0 {
		return errors.New("no pods found")
	}

	log.Info("Deleting backups from all the repos configured")

	pod := podList.Items[0]

	var stdout, stderr bytes.Buffer
	cmd := "pgbackrest --stanza=db --log-level-console=info stop; pgbackrest --stanza=db --log-level-console=info --repo=%s stanza-delete"

	for _, repo := range cr.Spec.Backups.PGBackRest.Repos {
		c := fmt.Sprintf(cmd, strings.TrimPrefix(repo.Name, "repo"))
		if err := r.PodExec(ctx, cr.Namespace, pod.Name, "database", nil, &stdout, &stderr, c); err != nil {
			return errors.Wrapf(err, "delete backups, stderr: %s", stderr.String())
		}
		log.Info("Deleted backups from repo", "repo", repo.Name)
	}

	pbList := new(v2.PerconaPGBackupList)
	err = r.Client.List(ctx, pbList, &client.ListOptions{
		Namespace: cr.Namespace,
	})
	if err != nil {
		return errors.Wrap(err, "failed to list backup jobs")
	}

	log.Info("Deleting all PGBackup objects")
	for _, pgBackup := range pbList.Items {
		if err := r.Client.Delete(ctx, &pgBackup); err != nil {
			return errors.Wrapf(err, "delete backup %s/%s", pgBackup.Name, pgBackup.Namespace)
		}
		log.Info("Deleted PGBackup", "name", pgBackup.Name)
	}

	return nil
}

func (r *PGClusterReconciler) runFinalizers(ctx context.Context, cr *v2.PerconaPGCluster) error {
	if err := r.runFinalizer(ctx, cr, v2.FinalizerDeletePVC, r.deletePVCAndSecrets); err != nil {
		return errors.Wrapf(err, "run finalizer %s", v2.FinalizerDeletePVC)
	}

	if err := r.runFinalizer(ctx, cr, v2.FinalizerDeleteSSL, r.deleteTLSSecrets); err != nil {
		return errors.Wrapf(err, "run finalizer %s", v2.FinalizerDeleteSSL)
	}

	if err := r.runFinalizer(ctx, cr, v2.FinalizerStopWatchers, r.stopExternalWatchers); err != nil {
		return errors.Wrapf(err, "run finalizer %s", v2.FinalizerStopWatchers)
	}

	if err := r.runFinalizer(ctx, cr, v2.FinalizerDeleteBackups, r.deleteBackups); err != nil {
		return errors.Wrapf(err, "run finalizer %s", v2.FinalizerStopWatchers)
	}

	return nil
}

func (r *PGClusterReconciler) runFinalizer(ctx context.Context, cr *v2.PerconaPGCluster, finalizer string, f finalizerFunc) error {
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
		log.Info("Removing finalizer", "name", finalizer)
		if err := r.Client.Patch(ctx, cr, client.MergeFrom(orig)); err != nil {
			return errors.Wrap(err, "remove finalizers")
		}
	}

	return nil
}
