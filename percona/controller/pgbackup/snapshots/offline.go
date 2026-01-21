package snapshots

import (
	"context"
	"strings"
	"time"

	"github.com/percona/percona-postgresql-operator/v2/internal/naming"
	perconaPG "github.com/percona/percona-postgresql-operator/v2/percona/postgres"
	v2 "github.com/percona/percona-postgresql-operator/v2/pkg/apis/pgv2.percona.com/v2"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type offlineExec struct {
	cl client.Client
}

func newOfflineExec(cl client.Client) *offlineExec {
	return &offlineExec{
		cl: cl,
	}
}

func (e *offlineExec) prepare(ctx context.Context, pgCluster *v2.PerconaPGCluster) (string, error) {
	// TODO: for single node clusters, we should use the primary,
	// but this is unsafe as it results in downtime during backup.
	// We should at least let the user explicilty opt-in for this behaviour.
	replicas, err := perconaPG.GetReplicaPods(ctx, e.cl, pgCluster)
	if err != nil {
		return "", errors.Wrap(err, "failed to get replica pods")
	}
	if len(replicas) == 0 {
		return "", errors.New("no replica pods found")
	}

	targetPod := replicas[0]
	annotations := targetPod.GetAnnotations()
	targetInstanceName := annotations[naming.LabelInstance]

	if err := e.suspendInstanceAndWait(ctx, targetInstanceName, pgCluster); err != nil {
		return "", errors.Wrap(err, "failed to suspend instance")
	}

	targetPVC, err := e.getTargetPVC(ctx, targetInstanceName, pgCluster.GetNamespace())
	if err != nil {
		return "", errors.Wrap(err, "failed to get target PVC")
	}

	return targetPVC, nil
}

func (e *offlineExec) complete(ctx context.Context, pgCluster *v2.PerconaPGCluster) error {
	if err := e.resumeSuspendedInstance(ctx, pgCluster); err != nil {
		return errors.Wrap(err, "failed to resume suspended instance")
	}
	return nil
}

func (e *offlineExec) suspendInstanceAndWait(ctx context.Context, instanceName string, pgCluster *v2.PerconaPGCluster) error {
	// suspend the instance
	if err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		pgCluster := &v2.PerconaPGCluster{}
		if err := e.cl.Get(ctx, client.ObjectKeyFromObject(pgCluster), pgCluster); err != nil {
			return errors.Wrap(err, "failed to get PGCluster")
		}
		annotations := pgCluster.GetAnnotations()
		if annotations == nil {
			annotations = make(map[string]string)
		}
		annotations[naming.SuspendedInstancesAnnotation] = instanceName
		pgCluster.SetAnnotations(annotations)
		return e.cl.Update(ctx, pgCluster)
	}); err != nil {
		return errors.Wrap(err, "failed to update PGCluster")
	}

	wCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	// wait for the instance to be suspended
	if err := wait.PollUntilContextTimeout(wCtx, 1*time.Second, 5*time.Minute, true, func(ctx context.Context) (bool, error) {
		pods := &corev1.PodList{}
		if err := e.cl.List(ctx, pods, &client.ListOptions{
			Namespace: pgCluster.GetNamespace(),
			LabelSelector: labels.SelectorFromSet(map[string]string{
				naming.LabelInstance: instanceName,
			}),
		}); err != nil {
			return false, errors.Wrap(err, "failed to list pods")
		}
		return len(pods.Items) == 0, nil
	}); err != nil {
		return errors.Wrap(err, "failed to wait for instance to suspend")
	}
	return nil
}

func (e *offlineExec) resumeSuspendedInstance(ctx context.Context, pgCluster *v2.PerconaPGCluster) error {
	suspendedInstancesVal, ok := pgCluster.GetAnnotations()[naming.SuspendedInstancesAnnotation]
	if !ok || suspendedInstancesVal == "" {
		return nil
	}

	if err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		annots := pgCluster.GetAnnotations()
		delete(annots, naming.SuspendedInstancesAnnotation)
		pgCluster.SetAnnotations(annots)
		return e.cl.Update(ctx, pgCluster)
	}); err != nil {
		return errors.Wrap(err, "failed to update PGCluster")
	}

	wCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	suspendedInstances := strings.Split(suspendedInstancesVal, ",")
	for _, instanceName := range suspendedInstances {
		if err := wait.PollUntilContextTimeout(wCtx, 1*time.Second, 5*time.Minute, true, func(ctx context.Context) (bool, error) {
			pods := &corev1.PodList{}
			if err := e.cl.List(ctx, pods, &client.ListOptions{
				Namespace: pgCluster.GetNamespace(),
				LabelSelector: labels.SelectorFromSet(map[string]string{
					naming.LabelInstance: instanceName,
				}),
			}); err != nil {
				return false, errors.Wrap(err, "failed to list pods")
			}
			return len(pods.Items) == 1, nil
		}); err != nil {
			return errors.Wrap(err, "failed to wait for instance to resume")
		}
	}
	return nil
}

func (e *offlineExec) getTargetPVC(ctx context.Context, instanceName, namespace string) (string, error) {
	pvcs := &corev1.PersistentVolumeClaimList{}
	if err := e.cl.List(ctx, pvcs, &client.ListOptions{
		Namespace: namespace,
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
	return pvcs.Items[0].GetName(), nil
}
