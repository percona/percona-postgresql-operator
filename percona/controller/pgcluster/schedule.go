package pgcluster

import (
	"context"

	"github.com/go-logr/logr"
	"github.com/pkg/errors"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/percona/percona-postgresql-operator/internal/controller/postgrescluster"
	"github.com/percona/percona-postgresql-operator/internal/logging"
	"github.com/percona/percona-postgresql-operator/internal/naming"
	v2 "github.com/percona/percona-postgresql-operator/pkg/apis/pgv2.percona.com/v2"
	"github.com/percona/percona-postgresql-operator/pkg/apis/postgres-operator.crunchydata.com/v1beta1"
)

func (r *PGClusterReconciler) reconcileScheduledBackups(ctx context.Context, cr *v2.PerconaPGCluster) error {
	for i := range cr.Spec.Backups.PGBackRest.Repos {
		repo := cr.Spec.Backups.PGBackRest.Repos[i]
		for _, t := range []string{postgrescluster.Full, postgrescluster.Differential, postgrescluster.Incremental} {
			err := r.reconcileScheduledBackup(ctx, cr, &repo, t)
			if err != nil {
				return errors.Wrapf(err, "failed to reconcile scheduled %s backup for %s repo", t, repo.Name)
			}
		}
	}
	return nil
}

func (r *PGClusterReconciler) reconcileScheduledBackup(ctx context.Context, cr *v2.PerconaPGCluster, repo *v1beta1.PGBackRestRepo, backupType string) error {
	log := logging.FromContext(ctx)

	name := naming.PGBackRestCronJob(&v1beta1.PostgresCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cr.Name,
			Namespace: cr.Namespace,
		},
	}, backupType, repo.Name)

	if repo.BackupSchedules == nil {
		r.Cron.DeleteBackupJob(name.Name)
		return nil
	}

	schedule := ""
	switch backupType {
	case postgrescluster.Full:
		if repo.BackupSchedules.Full != nil {
			schedule = *repo.BackupSchedules.Full
		}
	case postgrescluster.Differential:
		if repo.BackupSchedules.Differential != nil {
			schedule = *repo.BackupSchedules.Differential
		}
	case postgrescluster.Incremental:
		if repo.BackupSchedules.Incremental != nil {
			schedule = *repo.BackupSchedules.Incremental
		}
	default:
		return errors.Errorf("invalid backup type %s", backupType)
	}

	if schedule == "" {
		r.Cron.DeleteBackupJob(name.Name)
		return nil
	}

	createBackupFunc := r.createScheduledBackupFunc(log, name.Name, backupType, repo.Name, cr.Namespace, cr.Name)

	if err := r.Cron.ApplyBackupJob(name.Name, schedule, createBackupFunc); err != nil {
		log.Error(err, "failed to create a cron for a scheduled backup job")
		return nil
	}

	return nil
}

func (r *PGClusterReconciler) createScheduledBackupFunc(log logr.Logger, backupName, backupType, repoName, namespace, clusterName string) func() {
	return func() {
		if err := r.createScheduledBackup(log, backupName, backupType, repoName, namespace, clusterName); err != nil {
			log.Error(err, "failed to create a scheduled pg-backup")
		}
	}
}

func (r *PGClusterReconciler) createScheduledBackup(log logr.Logger, backupName, backupType, repoName, namespace, clusterName string) error {
	ctx := context.Background()

	cr := &v2.PerconaPGCluster{}
	if err := r.Client.Get(ctx, types.NamespacedName{
		Namespace: namespace,
		Name:      clusterName,
	}, cr); err != nil {
		if k8serrors.IsNotFound(err) {
			log.Info("cluster is not found, deleting the job", "name", backupName, "cluster", cr.Name, "namespace", cr.Namespace)

			r.Cron.DeleteBackupJob(backupName)
			return nil
		}
		return err
	}

	pb := &v2.PerconaPGBackup{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: backupName + "-",
			Namespace:    namespace,
		},
		Spec: v2.PerconaPGBackupSpec{
			PGCluster: cr.Name,
			RepoName:  repoName,
			Options:   []string{"--type=" + backupType},
		},
	}

	err := r.Client.Create(ctx, pb)
	if err != nil {
		return errors.Wrapf(err, "failed to create PerconaPGBackup %s", backupName)
	}
	return nil
}
