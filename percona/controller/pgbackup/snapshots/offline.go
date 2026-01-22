package snapshots

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/percona/percona-postgresql-operator/v2/internal/controller/runtime"
	"github.com/percona/percona-postgresql-operator/v2/internal/logging"
	"github.com/percona/percona-postgresql-operator/v2/internal/naming"
	pNaming "github.com/percona/percona-postgresql-operator/v2/percona/naming"
	perconaPG "github.com/percona/percona-postgresql-operator/v2/percona/postgres"
	v2 "github.com/percona/percona-postgresql-operator/v2/pkg/apis/pgv2.percona.com/v2"
)

const (
	annotationBackupTarget = pNaming.PrefixPerconaPGV2 + "backup-target"

	waitTimeout   = 5 * time.Minute
	retryInterval = 3 * time.Second
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
	targetPod, err := e.getBackupTargetPod(ctx)
	if err != nil {
		return "", errors.Wrap(err, "failed to get backup target pod")
	}

	if err := e.fenceInstance(ctx, targetPod); err != nil {
		return "", errors.Wrap(err, "failed to fence instance")
	}

	targetPVC, err := e.getTargetPVC(ctx, targetPod)
	if err != nil {
		return "", errors.Wrap(err, "failed to get target PVC")
	}
	return targetPVC, nil
}

func (e *offlineExec) complete(ctx context.Context) error {
	targetPod, err := e.getBackupTargetPod(ctx)
	if err != nil {
		return errors.Wrap(err, "failed to get backup target pod")
	}

	if err := e.unfenceInstance(ctx, targetPod); err != nil {
		return errors.Wrap(err, "failed to unfence instance")
	}
	return nil
}

func (e *offlineExec) getBackupTargetPod(ctx context.Context) (*corev1.Pod, error) {
	// If we already determined it before, use the same pod.
	if podName, ok := e.backup.GetAnnotations()[annotationBackupTarget]; ok && podName != "" {
		pod := &corev1.Pod{}
		if err := e.cl.Get(ctx, client.ObjectKey{Namespace: e.cluster.GetNamespace(), Name: podName}, pod); err != nil {
			return nil, errors.Wrap(err, "failed to get backup target pod")
		}
		return pod, nil
	}

	log := logging.FromContext(ctx)

	// TODO: single node clusters do not have replicas.
	// We should allow using a primary pod as the backup target.
	// Since this is unsafe, we should let the user explicitly opt-in for this behavior.
	replicas, err := perconaPG.GetReplicaPods(ctx, e.cl, e.cluster)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get replica pods")
	}
	if len(replicas) == 0 {
		return nil, errors.New("no replica pods found")
	}
	targetPod := replicas[0]

	if err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		bcp := e.backup.DeepCopy()
		annots := bcp.GetAnnotations()
		if annots == nil {
			annots = make(map[string]string)
		}
		annots[annotationBackupTarget] = targetPod.GetName()
		bcp.SetAnnotations(annots)
		return e.cl.Update(ctx, bcp)
	}); err != nil {
		return nil, errors.Wrap(err, "failed to update backup annotations")
	}

	log.Info("Selected backup target pod", "pod", targetPod.GetName())
	return targetPod.DeepCopy(), nil
}

func (e *offlineExec) fenceInstance(ctx context.Context, instancePod *corev1.Pod) error {
	// TODO: should we perform a checkpoint? Should it be configurable? What if it takes too long?
	cmd := []string{"touch", "/pgdata/sleep-forever"}
	if err := e.podExec(ctx, instancePod.GetNamespace(), instancePod.GetName(), naming.ContainerDatabase, nil, io.Discard, nil, cmd...); err != nil {
		return fmt.Errorf("failed to run pod exec: %w", err)
	}

	// Re-create the pod and wait for database container to be unready.
	if err := e.cl.Delete(ctx, instancePod); err != nil {
		return fmt.Errorf("failed to delete pod: %w", err)
	}

	log := logging.FromContext(ctx)

	if err := wait.PollUntilContextTimeout(ctx, retryInterval, waitTimeout, false, func(ctx context.Context) (bool, error) {
		pod := &corev1.Pod{}
		if err := e.cl.Get(ctx, client.ObjectKeyFromObject(instancePod), pod); err != nil {
			return false, client.IgnoreNotFound(err)
		}

		if pod.Status.Phase != corev1.PodRunning {
			return false, nil
		}

		databaseReady := false
		allOthersReady := true
		for _, containerStatus := range pod.Status.ContainerStatuses {
			if containerStatus.Name == naming.ContainerDatabase {
				databaseReady = containerStatus.Ready
				continue
			}
			if !containerStatus.Ready {
				allOthersReady = false
			}
		}

		return allOthersReady && !databaseReady, nil
	}); err != nil {
		return errors.Wrap(err, "failed to wait for pod to be unready")
	}

	log.Info("Instance fenced", "pod", instancePod.GetName())
	return nil
}

func (e *offlineExec) unfenceInstance(ctx context.Context, instancePod *corev1.Pod) error {
	cmd := []string{"rm", "-f", "/pgdata/sleep-forever"}
	if err := e.podExec(ctx, instancePod.GetNamespace(), instancePod.GetName(), naming.ContainerDatabase, nil, io.Discard, nil, cmd...); err != nil {
		return fmt.Errorf("failed to run pod exec: %w", err)
	}

	log := logging.FromContext(ctx)

	// wait for database container to be ready
	if err := wait.PollUntilContextTimeout(ctx, retryInterval, waitTimeout, false, func(ctx context.Context) (bool, error) {
		pod := &corev1.Pod{}
		if err := e.cl.Get(ctx, client.ObjectKeyFromObject(instancePod), pod); err != nil {
			return false, client.IgnoreNotFound(err)
		}

		if pod.Status.Phase != corev1.PodRunning {
			return false, nil
		}

		// ensure all containers are ready.
		for _, containerStatus := range pod.Status.ContainerStatuses {
			if !containerStatus.Ready {
				return false, nil
			}
		}

		return true, nil
	}); err != nil {
		return errors.Wrap(err, "failed to wait for pod to be ready")
	}

	log.Info("Instance unfenced", "pod", instancePod.GetName())
	return nil
}

func (e *offlineExec) getTargetPVC(ctx context.Context, targetPod *corev1.Pod) (string, error) {
	instanceName := targetPod.GetLabels()[naming.LabelInstance]
	if instanceName == "" {
		return "", errors.New("cannot determine instance name from pod labels")
	}

	pvcs := &corev1.PersistentVolumeClaimList{}
	if err := e.cl.List(ctx, pvcs, &client.ListOptions{
		Namespace: targetPod.GetNamespace(),
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
