package snapshots

import (
	"context"
	"fmt"
	"io"
	"path"
	"time"

	"github.com/pkg/errors"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/percona/percona-postgresql-operator/v2/internal/controller/runtime"
	"github.com/percona/percona-postgresql-operator/v2/internal/logging"
	"github.com/percona/percona-postgresql-operator/v2/internal/naming"
	"github.com/percona/percona-postgresql-operator/v2/internal/postgres"
	pNaming "github.com/percona/percona-postgresql-operator/v2/percona/naming"
	perconaPG "github.com/percona/percona-postgresql-operator/v2/percona/postgres"
	v2 "github.com/percona/percona-postgresql-operator/v2/pkg/apis/pgv2.percona.com/v2"
	crunchyv1beta1 "github.com/percona/percona-postgresql-operator/v2/pkg/apis/postgres-operator.crunchydata.com/v1beta1"
)

const (
	annotationBackupTarget = pNaming.PrefixPerconaPGV2 + "backup-target"

	checkpointTimeoutSeconds = 30 // TODO: make this configurable
	waitTimeout              = 5 * time.Minute
	retryInterval            = 3 * time.Second

	snapshotSignalFile = "restored-from-snapshot"
)

type offlineExec struct {
	cl      client.Client
	cluster *v2.PerconaPGCluster
	backup  *v2.PerconaPGBackup
	podExec runtime.PodExecutor
}

func newOfflineExec(cl client.Client, podExec runtime.PodExecutor, pgCluster *v2.PerconaPGCluster, pgBackup *v2.PerconaPGBackup) *offlineExec {
	return &offlineExec{
		cl:      cl,
		cluster: pgCluster,
		backup:  pgBackup,
		podExec: podExec,
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

	if err := e.createSnapshotSignal(ctx, targetInstance); err != nil {
		return "", errors.Wrap(err, "failed to create snapshot signal")
	}

	if err := e.suspendInstance(ctx, targetInstance); err != nil {
		return "", errors.Wrap(err, "failed to suspend instance")
	}

	targetPVC, err := e.getTargetPVC(ctx, targetInstance)
	if err != nil {
		return "", errors.Wrap(err, "failed to get target PVC")
	}
	return targetPVC, nil
}

// After a snapshot restored and cluster resumed, Patroni will start the instance in recovery mode,
// so PostgreSQL invokes restore_command to fetch WAL from pgbackrest repo. We want the instance to remain at
// the snapshot-consistent state instead of advancing past it. We create this special file
// in PGDATA before taking the snapshot so it is included in the snapshot; when the instance
// starts after a restore, the restore_command wrapper checks for this file and, if present, exits without
// fetching WAL (so recovery stops at local WAL) and then removes the file.
func (e *offlineExec) createSnapshotSignal(ctx context.Context, instanceName string) error {
	postgresCluster := &crunchyv1beta1.PostgresCluster{}
	if err := e.cl.Get(ctx, client.ObjectKeyFromObject(e.cluster), postgresCluster); err != nil {
		return errors.Wrap(err, "failed to get postgres cluster")
	}

	snapshotSignalFile := path.Join(postgres.DataDirectory(postgresCluster), snapshotSignalFile)
	cmd := []string{"touch", snapshotSignalFile}
	err := e.podExec(ctx, e.cluster.GetNamespace(),
		instanceName+"-0", naming.ContainerDatabase, nil, io.Discard, io.Discard, cmd...)
	if err != nil {
		return errors.Wrap(err, "pod exec failed")
	}
	return nil
}

func (e *offlineExec) removeSnapshotSignal(ctx context.Context, instanceName string) error {
	postgresCluster := &crunchyv1beta1.PostgresCluster{}
	if err := e.cl.Get(ctx, client.ObjectKeyFromObject(e.cluster), postgresCluster); err != nil {
		return errors.Wrap(err, "failed to get postgres cluster")
	}

	snapshotSignalFile := path.Join(postgres.DataDirectory(postgresCluster), snapshotSignalFile)
	cmd := []string{"rm", "-f", snapshotSignalFile}
	err := e.podExec(ctx, e.cluster.GetNamespace(),
		instanceName+"-0", naming.ContainerDatabase, nil, io.Discard, io.Discard, cmd...)
	if err != nil {
		return errors.Wrap(err, "pod exec failed")
	}
	return nil
}

func (e *offlineExec) checkpoint(ctx context.Context, instanceName string) error {
	exec := func(_ context.Context, stdin io.Reader, stdout, stderr io.Writer, command ...string) error {
		return e.podExec(ctx, e.cluster.GetNamespace(), instanceName+"-0", naming.ContainerDatabase, stdin, stdout, stderr, command...)
	}

	stdout, stderr, err := postgres.Executor(exec).
		ExecInDatabasesFromQuery(ctx, `SELECT pg_catalog.current_database()`,
			`SET statement_timeout = :'timeout'; CHECKPOINT;`,
			map[string]string{
				"timeout":       fmt.Sprintf("%ds", checkpointTimeoutSeconds),
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
	sts := &appsv1.StatefulSet{}
	if err := e.cl.Get(ctx, client.ObjectKey{Namespace: e.cluster.GetNamespace(), Name: instanceName}, sts); err != nil {
		return errors.Wrap(err, "failed to get stateful set")
	}

	if err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		orig := sts.DeepCopy()
		annots := sts.GetAnnotations()
		if annots == nil {
			annots = make(map[string]string)
		}
		annots[pNaming.AnnotationInstanceSuspended] = ""
		sts.SetAnnotations(annots)
		return e.cl.Patch(ctx, sts, client.MergeFrom(orig))
	}); err != nil {
		return errors.Wrap(err, "failed to update stateful set annotations")
	}

	// wait for suspension
	if err := wait.PollUntilContextTimeout(ctx, retryInterval, waitTimeout, true, func(ctx context.Context) (bool, error) {
		if err := e.cl.Get(ctx, client.ObjectKey{
			Namespace: e.cluster.GetNamespace(),
			Name:      instanceName,
		}, sts); err != nil {
			return false, errors.Wrap(err, "failed to get stateful set")
		}
		return sts.Status.Replicas == 0 && sts.Status.ReadyReplicas == 0, nil
	}); err != nil {
		return errors.Wrap(err, "failed to wait for suspension")
	}
	return nil
}

func (e *offlineExec) resumeInstance(ctx context.Context, instanceName string) error {
	sts := &appsv1.StatefulSet{}
	if err := e.cl.Get(ctx, client.ObjectKey{Namespace: e.cluster.GetNamespace(), Name: instanceName}, sts); err != nil {
		return errors.Wrap(err, "failed to get stateful set")
	}

	if err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		orig := sts.DeepCopy()
		annots := sts.GetAnnotations()
		delete(annots, pNaming.AnnotationInstanceSuspended)
		sts.SetAnnotations(annots)
		return e.cl.Patch(ctx, sts, client.MergeFrom(orig))
	}); err != nil {
		return errors.Wrap(err, "failed to update stateful set annotations")
	}

	// wait for resume
	if err := wait.PollUntilContextTimeout(ctx, retryInterval, waitTimeout, true, func(ctx context.Context) (bool, error) {
		if err := e.cl.Get(ctx, client.ObjectKey{
			Namespace: e.cluster.GetNamespace(),
			Name:      instanceName,
		}, sts); err != nil {
			return false, errors.Wrap(err, "failed to get stateful set")
		}
		return sts.Status.Replicas > 0 && sts.Status.ReadyReplicas > 0, nil
	}); err != nil {
		return errors.Wrap(err, "failed to wait for suspension")
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

	if err := e.removeSnapshotSignal(ctx, targetInstance); err != nil {
		return errors.Wrap(err, "failed to remove snapshot signal")
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

func (e *offlineExec) getTargetPVC(ctx context.Context, instanceName string) (string, error) {
	pvcs := &corev1.PersistentVolumeClaimList{}
	if err := e.cl.List(ctx, pvcs, &client.ListOptions{
		Namespace: e.cluster.GetNamespace(),
		LabelSelector: labels.SelectorFromSet(map[string]string{
			naming.LabelInstance: instanceName,
			naming.LabelRole:     naming.RolePostgresData,
		}),
	}); err != nil {
		return "", errors.Wrap(err, "failed to list PVCs")
	}

	if len(pvcs.Items) == 0 {
		return "", errors.New("no PVC found")
	}

	log := logging.FromContext(ctx)

	if len(pvcs.Items) > 1 {
		log.V(1).Info("Multiple PVCs found, using the first one", "pvc", pvcs.Items[0].GetName())
	}
	return pvcs.Items[0].GetName(), nil
}
