package pgcluster

import (
	"context"

	"github.com/pkg/errors"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/percona/percona-postgresql-operator/v3/internal/controller/postgrescluster"
	"github.com/percona/percona-postgresql-operator/v3/internal/logging"
	"github.com/percona/percona-postgresql-operator/v3/internal/naming"
	pNaming "github.com/percona/percona-postgresql-operator/v3/percona/naming"
	v2 "github.com/percona/percona-postgresql-operator/v3/pkg/apis/pgv2.percona.com/v2"
)

type suspension struct {
	Needed bool

	// Invalidate communicates that logical replication can not continue
	// User needs to fix it by re-seeding the replica
	Invalidate bool

	Reason  string
	Message string
}

// shouldSuspendLogicalReplicas reports the need of suspending logical replicas
// and the reason for suspension
func (r *PGClusterReconciler) shouldSuspendLogicalReplicas(ctx context.Context, cr *v2.PerconaPGCluster) (suspension, error) {
	s := suspension{}

	if cr.IsPaused() {
		s.Needed = true
		s.Reason = v2.LogicalReplicaReasonClusterPaused
		s.Message = "the cluster is paused"
	}

	// Set by PGBackRestRestore.Start before anything is torn down and cleared by
	// DisableRestore on every terminal outcome. The earliest signal there is, and
	// both edges write this CR, so the controller is woken for free.
	if enabled := cr.Spec.Backups.PGBackRest.Restore; enabled != nil &&
		enabled.Enabled != nil && *enabled.Enabled {
		s.Needed = true
		s.Reason = v2.LogicalReplicaReasonSourceRestoring
		s.Message = "the cluster is being restored in place"
	}
	if cr.GetAnnotations()[naming.PGBackRestRestore] != "" {
		s.Needed = true
		s.Reason = v2.LogicalReplicaReasonSourceRestoring
		s.Message = "the cluster is being restored in place"
	}

	// Raised by prepareForRestore as it deletes the instance runners. Nothing
	// removes it when a restore fails, which is right: a half-restored data
	// directory invalidates a replica just as thoroughly as a finished one.
	if meta.IsStatusConditionTrue(cr.Status.Conditions, postgrescluster.ConditionPGBackRestRestoreProgressing) {
		s.Invalidate = true
		s.Reason = v2.LogicalReplicaReasonSourceRestored
		s.Message = "logical replica invalidated by a restore of the cluster"
	}

	// The only signal that covers a snapshot restore with no point-in-time
	// recovery: that path never calls PGBackRestRestore.Start, and a volume
	// snapshot of PGDATA carries pg_replslot with it, so the health check would
	// not find the slots missing either.
	restores := &v2.PerconaPGRestoreList{}
	if err := r.Client.List(ctx, restores, client.InNamespace(cr.Namespace)); err != nil {
		return s, errors.Wrap(err, "list restores")
	}

	for i := range restores.Items {
		pgRestore := &restores.Items[i]
		if pgRestore.Spec.PGCluster != cr.Name || pgRestore.DeletionTimestamp != nil {
			continue
		}

		// A restore that has not started yet is deliberately not counted: it has
		// touched nothing, and one left behind in that state would keep the
		// replicas down for good. No window is missed - a pgBackRest restore sets
		// the two signals above in the same pass that moves it out of this state.
		switch pgRestore.Status.State {
		case v2.RestoreRunning:
			s.Needed = true
			s.Invalidate = true
			s.Reason = v2.LogicalReplicaReasonSourceRestored
			s.Message = "logical replica invalidated by a restore of the cluster"
		case v2.RestoreStarting:
			s.Needed = true
			s.Reason = v2.LogicalReplicaReasonSourceRestoring
			s.Message = "the cluster is being restored in place"
		default:
		}
	}

	return s, nil
}

// suspendLogicalReplicas stops every logical replica
func (r *PGClusterReconciler) suspendLogicalReplicas(ctx context.Context, cr *v2.PerconaPGCluster, s suspension) error {
	log := logging.FromContext(ctx).WithName("LogicalReplication")

	// Driven by the status rather than the spec: a replica removed from the
	// spec mid-restore still has a running StatefulSet, and its status entry is
	// the only record of the objects that have to be dropped on the primary
	// once there is one again.
	statuses := make([]v2.LogicalReplicaStatus, 0, len(cr.Status.LogicalReplicas))
	for i := range cr.Status.LogicalReplicas {
		status := cr.Status.LogicalReplicas[i].DeepCopy()

		if err := r.scaleLogicalReplica(ctx, cr, status.Name, 0); err != nil {
			return errors.Wrapf(err, "stop logical replica %q", status.Name)
		}

		switch {
		case status.SeededAt != nil:
			status.State = v2.LogicalReplicaStateSuspended
			status.Reason = s.Reason
			status.Message = s.Message

			if s.Invalidate && status.InvalidatedAt == nil {
				log.Info("logical replica invalidated by a restore of the cluster", "logicalReplica", status.Name)
				status.InvalidatedAt = new(metav1.Now())
			}

		default:
			// The bootstrap Job has a backoff limit of zero, so one interrupted by
			// the restore can never succeed, and the half-written data directory it
			// leaves behind is what the next attempt refuses to seed over. Throw
			// both away so the replica bootstraps from scratch.
			if err := r.discardLogicalReplicaBootstrap(ctx, cr, status.Name); err != nil {
				return errors.Wrapf(err, "discard bootstrap of logical replica %q", status.Name)
			}

			status.State = v2.LogicalReplicaStateBootstrapping
			status.Message = "the bootstrap was canceled because the logical replica is suspended"
			status.Databases = nil
		}

		statuses = append(statuses, *status)
	}

	readiness := metav1.Condition{
		Type:    pNaming.ConditionReadyForLogicalReplication,
		Status:  metav1.ConditionFalse,
		Reason:  s.Reason,
		Message: s.Message,
	}

	return r.updateLogicalReplicaStatus(ctx, cr, statuses, &readiness)
}

// scaleLogicalReplica sets the replica count of a logical replica's StatefulSet.
// Zero stops a replica while keeping the StatefulSet and the data volume, so
// nothing has to be rebuilt to start it again.
func (r *PGClusterReconciler) scaleLogicalReplica(
	ctx context.Context, cr *v2.PerconaPGCluster, replica string, replicas int32,
) error {
	sts := &appsv1.StatefulSet{}
	key := client.ObjectKey{Name: logicalReplicaObjectName(cr, replica), Namespace: cr.Namespace}

	return errors.Wrap(retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		if err := r.Client.Get(ctx, key, sts); err != nil {
			return client.IgnoreNotFound(err)
		}
		if sts.Spec.Replicas != nil && *sts.Spec.Replicas == replicas {
			return nil
		}

		orig := sts.DeepCopy()
		sts.Spec.Replicas = &replicas

		return r.Client.Patch(ctx, sts, client.MergeFrom(orig))
	}), "scale statefulset")
}

// discardLogicalReplicaBootstrap throws away a bootstrap that was interrupted,
// along with whatever it managed to write to the data volume.
func (r *PGClusterReconciler) discardLogicalReplicaBootstrap(
	ctx context.Context, cr *v2.PerconaPGCluster, replica string,
) error {
	job := &batchv1.Job{}
	key := client.ObjectKey{Name: logicalReplicaJobName(cr, replica), Namespace: cr.Namespace}
	switch err := r.Client.Get(ctx, key, job); {
	case apierrors.IsNotFound(err):
		// Nothing ever ran, so the data volume is pristine and worth keeping.
		return nil
	case err != nil:
		return errors.Wrap(err, "get bootstrap job")
	}

	if err := r.deleteLogicalReplicaJob(ctx, cr, replica); err != nil {
		return err
	}

	// A StatefulSet means the volume holds a replica rather than a partial copy,
	// whatever the status says. Deleting the claim would destroy it.
	sts, err := r.logicalReplicaStatefulSet(ctx, cr, replica)
	if err != nil || sts != nil {
		return err
	}

	pvc := &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{
		Name:      logicalReplicaPVCName(cr, replica),
		Namespace: cr.Namespace,
	}}
	if err := r.Client.Delete(ctx, pvc); client.IgnoreNotFound(err) != nil {
		return errors.Wrap(err, "delete data volume")
	}

	return nil
}
