package k8s

import (
	"context"

	"github.com/pkg/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	pgv2 "github.com/percona/percona-postgresql-operator/v3/pkg/apis/pgv2.percona.com/v2"
)

var (
	ErrRunningBackup = errors.New("backups are running")
	ErrNoBackups     = errors.New("no backups found")
)

func GetLatestBackup(ctx context.Context, cli client.Client, cr *pgv2.PerconaPGCluster) (*pgv2.PerconaPGBackup, error) {
	backupList := &pgv2.PerconaPGBackupList{}
	err := cli.List(ctx, backupList, &client.ListOptions{
		Namespace: cr.Namespace,
		FieldSelector: fields.SelectorFromSet(map[string]string{
			"spec.pgCluster": cr.Name,
		}),
	})
	if err != nil {
		return nil, err
	}

	if len(backupList.Items) == 0 {
		return nil, ErrNoBackups
	}

	latest := &pgv2.PerconaPGBackup{}
	runningBackupExists := false
	for _, backup := range backupList.Items {
		if ptr.Deref(backup.Spec.Method, pgv2.BackupMethodPGBackrest) == pgv2.BackupMethodVolumeSnapshot {
			continue
		}

		switch backup.Status.State {
		case pgv2.BackupSucceeded:
			var completedAt *metav1.Time

			if backup.Status.CompletedAt != nil {
				completedAt = backup.Status.CompletedAt
			}
			if completedAt == nil {
				completedAt = &backup.CreationTimestamp
			}

			if latest.Status.CompletedAt == nil || completedAt.After(latest.Status.CompletedAt.Time) {
				latest = &backup
			}
		case pgv2.BackupFailed:
		default:
			runningBackupExists = true
		}
	}

	if latest.Status.CompletedAt == nil {
		if runningBackupExists {
			return nil, ErrRunningBackup
		}
		return nil, errors.New("no completed backups found")
	}

	return latest, nil
}
