package watcher

import (
	"bytes"
	"context"
	"strings"
	"time"

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"

	"github.com/percona/percona-postgresql-operator/internal/logging"
	"github.com/percona/percona-postgresql-operator/percona/clientcmd"
	pgv2 "github.com/percona/percona-postgresql-operator/pkg/apis/pgv2.percona.com/v2"
)

const (
	LatestCommitTimestampFile = "/pgwal/latest_commit_timestamp.txt"
)

var LatestTimestampFileNotFound = errors.Errorf("%s not found in container", LatestCommitTimestampFile)

type WALWatcher func(context.Context, client.Client, chan event.GenericEvent, chan event.DeleteEvent, *pgv2.PerconaPGCluster)

func GetWALWatcher(cr *pgv2.PerconaPGCluster) (string, WALWatcher) {
	return cr.Namespace + "-" + cr.Name + "-wal-watcher", WatchCommitTimestamps
}

func WatchCommitTimestamps(ctx context.Context, cli client.Client, eventChan chan event.GenericEvent, stopChan chan event.DeleteEvent, cr *pgv2.PerconaPGCluster) {
	log := logging.FromContext(ctx)

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
			ts, err := GetLatestCommitTimestamp(ctx, cli, execCli, cr)
			if err != nil {
				if errors.Is(err, LatestTimestampFileNotFound) {
					log.V(1).Info("Latest commit timestamp file not found", "file", LatestCommitTimestampFile)
				} else {
					log.Error(err, "get latest commit timestamp")
				}
				continue
			}

			latestBackup, err := getLatestBackup(ctx, cli, cr)
			if err != nil {
				log.Error(err, "get latest backup")
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

	latest := &pgv2.PerconaPGBackup{}
	for _, backup := range backupList.Items {
		backup := backup
		if latest.Status.CompletedAt == nil || backup.Status.CompletedAt.After(latest.Status.CompletedAt.Time) {
			latest = &backup
		}
	}

	if latest.Status.CompletedAt == nil {
		return nil, errors.New("no completed backups found")
	}

	return latest, nil
}

func GetLatestCommitTimestamp(ctx context.Context, cli client.Client, execCli *clientcmd.Client, cr *pgv2.PerconaPGCluster) (*metav1.Time, error) {
	log := logging.FromContext(ctx)

	primary, err := getPrimaryPod(ctx, cli, cr)
	if err != nil {
		return nil, errors.Wrap(err, "get primary pod")
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

	return &commitTsMeta, nil
}

func getPrimaryPod(ctx context.Context, cli client.Client, cr *pgv2.PerconaPGCluster) (*corev1.Pod, error) {
	podList := &corev1.PodList{}
	err := cli.List(ctx, podList, &client.ListOptions{
		Namespace: cr.Namespace,
		LabelSelector: labels.SelectorFromSet(map[string]string{
			"app.kubernetes.io/instance":             cr.Name,
			"postgres-operator.crunchydata.com/role": "master",
		}),
	})
	if err != nil {
		return nil, err
	}

	if len(podList.Items) == 0 {
		return nil, errors.New("no primary pod found")
	}

	if len(podList.Items) > 1 {
		return nil, errors.New("multiple primary pods found")
	}

	return &podList.Items[0], nil
}
