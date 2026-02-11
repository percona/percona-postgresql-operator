package snapshots

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/percona/percona-postgresql-operator/v2/internal/controller/runtime"
	"github.com/percona/percona-postgresql-operator/v2/internal/logging"
	"github.com/percona/percona-postgresql-operator/v2/internal/naming"
	"github.com/percona/percona-postgresql-operator/v2/internal/postgres"
	perconaPG "github.com/percona/percona-postgresql-operator/v2/percona/postgres"
	v2 "github.com/percona/percona-postgresql-operator/v2/pkg/apis/pgv2.percona.com/v2"
)

const (
	defaultCheckpointTimeoutSeconds int32 = 300 // 5mins
	waitTimeout                           = 5 * time.Minute
	retryInterval                         = 3 * time.Second
)

type offlineExec struct {
	cl            client.Client
	cluster       *v2.PerconaPGCluster
	backup        *v2.PerconaPGBackup
	podExec       runtime.PodExecutor
	offlineConfig *v2.OfflineSnapshotConfig
}

func newOfflineExec(cl client.Client, podExec runtime.PodExecutor, pgCluster *v2.PerconaPGCluster, pgBackup *v2.PerconaPGBackup) *offlineExec {
	return &offlineExec{
		cl:            cl,
		cluster:       pgCluster,
		backup:        pgBackup,
		podExec:       podExec,
		offlineConfig: pgCluster.Spec.Backups.VolumeSnapshots.OfflineConfig,
	}
}

func (e *offlineExec) prepare(ctx context.Context) (string, error) {
	targetInstance, err := e.getBackupTarget(ctx)
	if err != nil {
		return "", errors.Wrap(err, "failed to get backup target pod")
	}

	// TODO: should this be optional, since this can take a while on large datasets?
	if err := e.checkpoint(ctx, targetInstance); err != nil {
		return "", errors.Wrap(err, "failed to checkpoint instance")
	}

	if err := e.suspendInstance(ctx, targetInstance); err != nil {
		return "", errors.Wrap(err, "failed to suspend instance")
	}
	return targetInstance, nil
}

func (e *offlineExec) checkpoint(ctx context.Context, instanceName string) error {
	exec := func(_ context.Context, stdin io.Reader, stdout, stderr io.Writer, command ...string) error {
		return e.podExec(ctx, e.cluster.GetNamespace(), instanceName+"-0", naming.ContainerDatabase, stdin, stdout, stderr, command...)
	}

	timeoutSeconds := defaultCheckpointTimeoutSeconds
	if e.offlineConfig != nil && e.offlineConfig.CheckpointTimeoutSeconds != nil {
		timeoutSeconds = *e.offlineConfig.CheckpointTimeoutSeconds
	}

	stdout, stderr, err := postgres.Executor(exec).
		ExecInDatabasesFromQuery(ctx, `SELECT pg_catalog.current_database()`,
			`SET statement_timeout = :'timeout'; CHECKPOINT;`,
			map[string]string{
				"timeout":       fmt.Sprintf("%ds", timeoutSeconds),
				"ON_ERROR_STOP": "on", // Abort when any one statement fails.
				"QUIET":         "on", // Do not print successful statements to stdout.
			})
	if err != nil {
		return errors.Wrap(err, "failed to execute checkpoint")
	}

	if stderr != "" {
		return fmt.Errorf("checkpoint failed: %s", stderr)
	}

	log := logging.FromContext(ctx)
	log.Info("checkpoint executed", "stdout", stdout, "stderr", stderr)
	return nil
}

func (e *offlineExec) suspendInstance(ctx context.Context, instanceName string) error {
	// Suspend and wait
	instanceKey := client.ObjectKey{Namespace: e.cluster.GetNamespace(), Name: instanceName}
	if err := wait.PollUntilContextTimeout(ctx, retryInterval, waitTimeout, true, func(ctx context.Context) (bool, error) {
		return perconaPG.SuspendInstance(ctx, e.cl, instanceKey)
	}); err != nil {
		return errors.Wrap(err, "failed to wait for suspension")
	}
	return nil
}

func (e *offlineExec) resumeInstance(ctx context.Context, instanceName string) error {
	// unsuspend and wait
	instanceKey := client.ObjectKey{Namespace: e.cluster.GetNamespace(), Name: instanceName}
	if err := wait.PollUntilContextTimeout(ctx, retryInterval, waitTimeout, true, func(ctx context.Context) (bool, error) {
		return perconaPG.UnsuspendInstance(ctx, e.cl, instanceKey)
	}); err != nil {
		return errors.Wrap(err, "failed to wait for unsuspend")
	}
	return nil
}

func (e *offlineExec) finalize(ctx context.Context) error {
	targetInstance, err := e.getBackupTarget(ctx)
	if err != nil {
		return errors.Wrap(err, "failed to get backup target")
	}

	if err := e.resumeInstance(ctx, targetInstance); err != nil {
		return errors.Wrap(err, "failed to resume instance")
	}
	return nil
}

func (e *offlineExec) getBackupTarget(ctx context.Context) (string, error) {
	// If we already determined it before, use it.
	if name, ok := e.backup.GetAnnotations()[annotationBackupTarget]; ok && name != "" {
		return name, nil
	}

	log := logging.FromContext(ctx)

	// TODO: single node clusters do not have replicas.
	// We should allow using a primary pod as the backup target.
	// Since this is unsafe, we should let the user explicitly opt-in for this behavior.
	replicas, err := perconaPG.GetReplicaPods(ctx, e.cl, e.cluster)
	if err != nil {
		return "", errors.Wrap(err, "failed to get replica pods")
	}
	if len(replicas) == 0 {
		return "", errors.New("no replica pods found")
	}
	targetPod := replicas[0]
	instanceName := targetPod.GetLabels()[naming.LabelInstance]
	if instanceName == "" {
		return "", errors.New("cannot determine instance name from pod labels")
	}

	if err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		orig := e.backup.DeepCopy()
		bcp := e.backup.DeepCopy()
		annots := bcp.GetAnnotations()
		if annots == nil {
			annots = make(map[string]string)
		}
		annots[annotationBackupTarget] = instanceName
		bcp.SetAnnotations(annots)
		return e.cl.Patch(ctx, bcp, client.MergeFrom(orig))
	}); err != nil {
		return "", errors.Wrap(err, "failed to update backup annotations")
	}

	log.Info("Selected backup target", "instance", instanceName)
	return instanceName, nil
}
