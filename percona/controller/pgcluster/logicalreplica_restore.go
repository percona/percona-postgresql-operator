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

	"github.com/percona/percona-postgresql-operator/v2/internal/controller/postgrescluster"
	"github.com/percona/percona-postgresql-operator/v2/internal/logging"
	"github.com/percona/percona-postgresql-operator/v2/internal/naming"
	pNaming "github.com/percona/percona-postgresql-operator/v2/percona/naming"
	v2 "github.com/percona/percona-postgresql-operator/v2/pkg/apis/pgv2.percona.com/v2"
)

// sourceRestore is what a restore of the cluster means for its logical
// replicas.
type sourceRestore struct {
	// InFlight means a restore has been asked for and has not finished, so the
	// primary is about to go away or already has.
	InFlight bool

	// DataReplaced means the restore has passed the point where the cluster could
	// still be put back the way it was.
	DataReplaced bool
}

// observeSourceRestore reports what the restores of this cluster mean for its
// logical replicas.
//
// It never looks at the restore Job: both prepareForRestore and this operator's
// own PGBackRestRestore.Start blank status.pgbackrest.restore, so anything
// derived from it is ambiguous exactly while a restore is starting.
func (r *PGClusterReconciler) observeSourceRestore(
	ctx context.Context, cr *v2.PerconaPGCluster,
) (sourceRestore, error) {
	restore := sourceRestore{}

	// Set by PGBackRestRestore.Start before anything is torn down and cleared by
	// DisableRestore on every terminal outcome. The earliest signal there is, and
	// both edges write this CR, so the controller is woken for free.
	if enabled := cr.Spec.Backups.PGBackRest.Restore; enabled != nil &&
		enabled.Enabled != nil && *enabled.Enabled {
		restore.InFlight = true
	}
	if cr.GetAnnotations()[naming.PGBackRestRestore] != "" {
		restore.InFlight = true
	}

	// Raised by prepareForRestore as it deletes the instance runners. Nothing
	// removes it when a restore fails, which is right: a half-restored data
	// directory invalidates a replica just as thoroughly as a finished one.
	if meta.IsStatusConditionTrue(cr.Status.Conditions,
		postgrescluster.ConditionPGBackRestRestoreProgressing) {
		restore.DataReplaced = true
	}

	// The only signal that covers a snapshot restore with no point-in-time
	// recovery: that path never calls PGBackRestRestore.Start, and a volume
	// snapshot of PGDATA carries pg_replslot with it, so the health check would
	// not find the slots missing either.
	restores := &v2.PerconaPGRestoreList{}
	if err := r.Client.List(ctx, restores, client.InNamespace(cr.Namespace)); err != nil {
		return restore, errors.Wrap(err, "list restores")
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
			restore.InFlight = true
			restore.DataReplaced = true
		case v2.RestoreStarting:
			restore.InFlight = true
		default:
		}
	}

	return restore, nil
}

// suspendLogicalReplicas stops every logical replica for the duration of a
// restore of the cluster they replicate. See
// [v2.LogicalReplicaReasonSourceRestored] for why they cannot keep running.
//
// Whether they can be resumed afterwards is deliberately not decided here: a
// restore that fails before it touches the data directory leaves them perfectly
// valid. Nothing is destroyed and nothing is forgotten.
func (r *PGClusterReconciler) suspendLogicalReplicas(
	ctx context.Context, cr *v2.PerconaPGCluster, restore sourceRestore,
) error {
	log := logging.FromContext(ctx).WithName("LogicalReplication")

	// Driven by the status rather than the spec: a replica removed from the
	// spec mid-restore still has a running StatefulSet, and its status entry is
	// the only record of the objects that have to be dropped on the primary
	// once there is one again.
	statuses := make([]v2.LogicalReplicaStatus, 0, len(cr.Status.LogicalReplicas))
	for i := range cr.Status.LogicalReplicas {
		status := cr.Status.LogicalReplicas[i].DeepCopy()
		status.Reason = v2.LogicalReplicaReasonSourceRestoring

		if err := r.scaleLogicalReplica(ctx, cr, status.Name, 0); err != nil {
			return errors.Wrapf(err, "stop logical replica %q", status.Name)
		}

		switch {
		case status.SeededAt != nil:
			status.State = v2.LogicalReplicaStateSuspended
			status.Message = "the cluster is being restored in place"

			if restore.DataReplaced && status.InvalidatedAt == nil {
				log.Info("logical replica invalidated by a restore of the cluster",
					"logicalReplica", status.Name)
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
			status.Message = "the bootstrap was cancelled because the cluster is being restored in place"
			status.Databases = nil
		}

		statuses = append(statuses, *status)
	}

	// Synthesised rather than observed: observePrimaryReadiness execs on a
	// primary that a restore has taken away, and the answer is known anyway.
	readiness := metav1.Condition{
		Type:    pNaming.ConditionReadyForLogicalReplication,
		Status:  metav1.ConditionFalse,
		Reason:  v2.LogicalReplicaReasonSourceRestoring,
		Message: "the cluster is being restored in place; logical replicas are stopped",
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
