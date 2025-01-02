package watcher

import (
	"bytes"
	"context"
	"strings"
	"time"

	"github.com/pkg/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"

	"github.com/percona/percona-postgresql-operator/internal/logging"
	"github.com/percona/percona-postgresql-operator/percona/clientcmd"
	"github.com/percona/percona-postgresql-operator/percona/pgbackrest"
	"github.com/percona/percona-postgresql-operator/percona/postgres"
	pgv2 "github.com/percona/percona-postgresql-operator/pkg/apis/pgv2.percona.com/v2"
)

const (
	LatestCommitTimestampFile = "/pgdata/latest_commit_timestamp.txt"
)

var (
	LatestTimestampFileNotFound        = errors.Errorf("%s not found in container", LatestCommitTimestampFile)
	PrimaryPodNotFound                 = errors.New("primary pod not found")
	LatestTimestampIsBeforeBackupStart = errors.New("latest commit timestamp is before backup start timestamp")
)

type WALWatcher func(context.Context, client.Client, chan event.GenericEvent, chan event.DeleteEvent, *pgv2.PerconaPGCluster)

func GetWALWatcher(cr *pgv2.PerconaPGCluster) (string, WALWatcher) {
	return cr.Namespace + "-" + cr.Name + "-wal-watcher", WatchCommitTimestamps
}

func WatchCommitTimestamps(ctx context.Context, cli client.Client, eventChan chan event.GenericEvent, stopChan chan event.DeleteEvent, cr *pgv2.PerconaPGCluster) {
	log := logging.FromContext(ctx).WithName("WALWatcher")

	log.Info("Watching commit timestamps")

	execCli, err := clientcmd.NewClient()
	if err != nil {
		log.Error(err, "create exec client")
		return
	}

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			log.V(1).Info("Running WAL watcher")

			localCr := cr.DeepCopy()
			err := cli.Get(ctx, client.ObjectKeyFromObject(cr), localCr)
			if err != nil {
				log.Error(err, "get cluster")
				return
			}

			latestBackup, err := getLatestBackup(ctx, cli, cr)
			if err != nil {
				if localCr.Status.State != pgv2.AppStateInit || (!errors.Is(err, errRunningBackup) && !errors.Is(err, errNoBackups)) {
					log.Error(err, "get latest backup")
				}

				continue
			}

			ts, err := GetLatestCommitTimestamp(ctx, cli, execCli, cr, latestBackup)
			if err != nil {
				switch {
				case errors.Is(err, PrimaryPodNotFound) && localCr.Status.State != pgv2.AppStateReady:
					log.V(1).Info("Primary pod not found, skipping WAL watcher")
				case errors.Is(err, LatestTimestampFileNotFound):
					log.V(1).Info("Latest commit timestamp file not found", "file", LatestCommitTimestampFile)
				case errors.Is(err, LatestTimestampIsBeforeBackupStart):
					log.V(1).Info("Latest commit timestamp is before backup start timestamp")
				default:
					log.Error(err, "get latest commit timestamp")
				}
				continue
			}

			latestRestorableTime := latestBackup.Status.LatestRestorableTime
			log.Info("Latest commit timestamp", "timestamp", ts, "latestRestorableTime", latestRestorableTime.Time)
			if latestRestorableTime.Time == nil || latestRestorableTime.UTC().Before(ts.Time) {
				log.Info("Triggering PGBackup reconcile",
					"latestBackup", latestBackup.Name,
					"latestRestorableTime", latestRestorableTime.Time,
					"latestCommitTimestamp", ts,
				)
				eventChan <- event.GenericEvent{
					Object: latestBackup,
				}
			}
		case event := <-stopChan:
			if event.Object.GetName() == cr.Name && event.Object.GetNamespace() == cr.Namespace {
				log.Info("Stopping WAL watcher", "cluster", event.Object.GetName(), "namespace", event.Object.GetNamespace())
				return
			}
		}
	}
}

var (
	errRunningBackup = errors.New("backups are running")
	errNoBackups     = errors.New("no backups found")
)

func getLatestBackup(ctx context.Context, cli client.Client, cr *pgv2.PerconaPGCluster) (*pgv2.PerconaPGBackup, error) {
	backupList := &pgv2.PerconaPGBackupList{}
	err := cli.List(ctx, backupList, &client.ListOptions{
		Namespace: cr.Namespace,
		FieldSelector: fields.SelectorFromSet(map[string]string{
			"spec.pgCluster": cr.Name,
			"status.state":   string(pgv2.BackupSucceeded),
		}),
	})
	if err != nil {
		return nil, err
	}

	if len(backupList.Items) == 0 {
		return nil, errNoBackups
	}

	latest := &pgv2.PerconaPGBackup{}
	runningBackupExists := false
	for _, backup := range backupList.Items {
		backup := backup
		if latest.Status.CompletedAt == nil || backup.Status.CompletedAt.After(latest.Status.CompletedAt.Time) {
			latest = &backup
		} else if backup.Status.State == pgv2.BackupStarting || backup.Status.State == pgv2.BackupRunning {
			runningBackupExists = true
		}
	}

	if latest.Status.CompletedAt == nil {
		if runningBackupExists {
			return nil, errRunningBackup
		}
		return nil, errors.New("no completed backups found")
	}

	return latest, nil
}

func GetLatestCommitTimestamp(ctx context.Context, cli client.Client, execCli *clientcmd.Client, cr *pgv2.PerconaPGCluster, backup *pgv2.PerconaPGBackup) (*metav1.Time, error) {
	log := logging.FromContext(ctx)

	primary, err := perconaPG.GetPrimaryPod(ctx, cli, cr)
	if err != nil {
		return nil, PrimaryPodNotFound
	}

	log.V(1).Info("Getting latest commit timestamp from primary pod", "pod", primary.Name)

	stdout := bytes.Buffer{}
	stderr := bytes.Buffer{}
	if err := execCli.Exec(ctx, primary, "database", nil, &stdout, &stderr, "cat", LatestCommitTimestampFile); err != nil {
		if strings.Contains(stderr.String(), "No such file or directory") {
			return nil, LatestTimestampFileNotFound
		}
		return nil, errors.Wrapf(err, "get latest commit timestamp from container, stderr: %s", stderr.String())
	}

	commitTs, err := time.Parse("2006-01-02T15:04:05.000000Z07", strings.TrimSpace(stdout.String()))
	if err != nil {
		return nil, errors.Wrap(err, "parse commit timestamp")
	}

	commitTsMeta := metav1.NewTime(commitTs.UTC())

	backupStartTs, err := getBackupStartTimestamp(ctx, cli, cr, backup)
	if err != nil {
		return nil, errors.Wrap(err, "get backup start timestamp")
	}

	if commitTsMeta.Time.Before(backupStartTs) {
		return nil, LatestTimestampIsBeforeBackupStart
	}

	return &commitTsMeta, nil
}

func getBackupStartTimestamp(ctx context.Context, cli client.Client, cr *pgv2.PerconaPGCluster, backup *pgv2.PerconaPGBackup) (time.Time, error) {
	primary, err := perconaPG.GetPrimaryPod(ctx, cli, cr)
	if err != nil {
		return time.Time{}, PrimaryPodNotFound
	}

	pgbackrestInfo, err := pgbackrest.GetInfo(ctx, primary, backup.Spec.RepoName)
	if err != nil {
		return time.Time{}, errors.Wrap(err, "get pgbackrest info")
	}

	for _, info := range pgbackrestInfo {
		for _, b := range info.Backup {
			if b.Annotation[pgv2.PGBackrestAnnotationJobName] == backup.Status.JobName {
				return time.Unix(b.Timestamp.Start, 0), nil
			}
		}
	}

	return time.Time{}, errors.New("backup not found in pgbackrest info")
}
