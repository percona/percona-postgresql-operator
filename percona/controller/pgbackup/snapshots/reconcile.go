package snapshots

import (
	"context"
	"fmt"
	"time"

	volumesnapshotv1 "github.com/kubernetes-csi/external-snapshotter/client/v8/apis/volumesnapshot/v1"
	"github.com/pkg/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/percona/percona-postgresql-operator/v2/internal/controller/runtime"
	"github.com/percona/percona-postgresql-operator/v2/internal/feature"
	"github.com/percona/percona-postgresql-operator/v2/internal/logging"
	pNaming "github.com/percona/percona-postgresql-operator/v2/percona/naming"
	v2 "github.com/percona/percona-postgresql-operator/v2/pkg/apis/pgv2.percona.com/v2"
)

type snapshotExecutor interface {
	// Prepare the cluster for performing a snapshot.
	// Returns the name of the PVC that will be snapshotted.
	prepare(ctx context.Context) (string, error)
	// Complete the snapshot.
	finalize(ctx context.Context) error
}

type snapshotReconciler struct {
	cl      client.Client
	log     logging.Logger
	cluster *v2.PerconaPGCluster
	backup  *v2.PerconaPGBackup
	exec    snapshotExecutor
}

func newSnapshotReconciler(
	cl client.Client,
	log logging.Logger,
	cluster *v2.PerconaPGCluster,
	backup *v2.PerconaPGBackup,
	exec snapshotExecutor,
) *snapshotReconciler {
	return &snapshotReconciler{
		cl:      cl,
		log:     log,
		cluster: cluster,
		backup:  backup,
		exec:    exec,
	}
}

func newSnapshotExec(
	cl client.Client,
	cluster *v2.PerconaPGCluster,
	backup *v2.PerconaPGBackup,
) (snapshotExecutor, error) {
	switch mode := cluster.Spec.Backups.VolumeSnapshots.Mode; mode {
	case v2.VolumeSnapshotModeOffline:
		return newOfflineExec(cl, cluster, backup), nil
	default:
		return nil, fmt.Errorf("invalid or unsupported volume snapshot mode: %s", mode)
	}
}

// Reconcile backup snapshot
func Reconcile(
	ctx context.Context,
	cl client.Client,
	podExec runtime.PodExecutor,
	pgBackup *v2.PerconaPGBackup,
	pgCluster *v2.PerconaPGCluster,
) (reconcile.Result, error) {
	if pgBackup == nil || pgCluster == nil {
		return reconcile.Result{}, errors.New("pgBackup or pgCluster is nil or not found")
	}

	log := logging.FromContext(ctx).
		WithName("SnapshotReconciler").
		WithValues("backup", pgBackup.Name, "cluster", pgCluster.Name)

	// Do nothing if the feature is not enabled.
	if !feature.Enabled(ctx, feature.BackupSnapshots) {
		log.Info(fmt.Sprintf("Feature gate '%s' is not enabled, skipping snapshot reconciliation", feature.BackupSnapshots))
		return reconcile.Result{}, nil
	}

	// Check if volume snapshots are enabled for this cluster.
	if pgCluster.Spec.Backups.VolumeSnapshots == nil || !pgCluster.Spec.Backups.VolumeSnapshots.Enabled {
		if updErr := pgBackup.UpdateStatus(ctx, cl, func(bcp *v2.PerconaPGBackup) {
			bcp.Status.State = v2.BackupFailed
			bcp.Status.Error = "Volume snapshots are not enabled for this cluster"
		}); updErr != nil {
			return reconcile.Result{}, fmt.Errorf("failed to update backup status: %w", updErr)
		}
		return reconcile.Result{}, nil
	}

	exec, err := newSnapshotExec(cl, pgCluster, pgBackup)
	if err != nil {
		stsErr := fmt.Errorf("invalid or unsupported volume snapshot mode: %s", pgCluster.Spec.Backups.VolumeSnapshots.Mode)
		if updErr := pgBackup.UpdateStatus(ctx, cl, func(bcp *v2.PerconaPGBackup) {
			bcp.Status.State = v2.BackupFailed
			bcp.Status.Error = stsErr.Error()
		}); updErr != nil {
			return reconcile.Result{}, fmt.Errorf("failed to update backup status: %w", updErr)
		}
		return reconcile.Result{}, stsErr
	}

	r := newSnapshotReconciler(cl, log, pgCluster, pgBackup, exec)
	return r.reconcile(ctx)
}

func (r *snapshotReconciler) reconcile(ctx context.Context) (reconcile.Result, error) {
	if !r.backup.GetDeletionTimestamp().IsZero() {
		return reconcile.Result{}, r.complete(ctx)
	}

	switch r.backup.Status.State {
	case v2.BackupNew:
		return r.reconcileNew(ctx)
	case v2.BackupStarting:
		return r.reconcileStarting(ctx)
	case v2.BackupRunning:
		return r.reconcileRunning(ctx)
	case v2.BackupFailed, v2.BackupSucceeded:
		return reconcile.Result{}, r.complete(ctx)
	}
	return reconcile.Result{}, nil
}

func (r *snapshotReconciler) reconcileNew(ctx context.Context) (reconcile.Result, error) {
	if r.cluster.Status.State != v2.AppStateReady {
		r.log.Info("Waiting for cluster to be ready before creating snapshot")
		return reconcile.Result{RequeueAfter: time.Second * 5}, nil
	}

	if updErr := r.backup.UpdateStatus(ctx, r.cl, func(bcp *v2.PerconaPGBackup) {
		bcp.Status.State = v2.BackupStarting
	}); updErr != nil {
		return reconcile.Result{}, fmt.Errorf("failed to update backup status: %w", updErr)
	}
	r.log.Info("Snapshot is starting")
	return reconcile.Result{}, nil
}

func (r *snapshotReconciler) reconcileStarting(ctx context.Context) (reconcile.Result, error) {
	if err := r.prepare(ctx); err != nil {
		return reconcile.Result{}, err
	}

	if updErr := r.backup.UpdateStatus(ctx, r.cl, func(bcp *v2.PerconaPGBackup) {
		bcp.Status.State = v2.BackupRunning
	}); updErr != nil {
		return reconcile.Result{}, fmt.Errorf("failed to update backup status: %w", updErr)
	}
	r.log.Info("Snapshot is running")
	return reconcile.Result{}, nil
}

// +kubebuilder:rbac:groups=snapshot.storage.k8s.io,resources=volumesnapshots,verbs=get;list;watch;create
func (r *snapshotReconciler) reconcileRunning(ctx context.Context) (reconcile.Result, error) {
	volumeSnapshot := &volumesnapshotv1.VolumeSnapshot{
		ObjectMeta: metav1.ObjectMeta{
			Name:      r.backup.GetName(),
			Namespace: r.backup.GetNamespace(),
		},
		Spec: volumesnapshotv1.VolumeSnapshotSpec{
			VolumeSnapshotClassName: ptr.To(r.cluster.Spec.Backups.VolumeSnapshots.ClassName),
			Source: volumesnapshotv1.VolumeSnapshotSource{
				PersistentVolumeClaimName: &r.backup.Status.Snapshot.TargetPVCName,
			},
		},
	}
	if err := controllerutil.SetControllerReference(r.backup, volumeSnapshot, r.cl.Scheme()); err != nil {
		return reconcile.Result{}, fmt.Errorf("failed to set owner reference on volume snapshot: %w", err)
	}

	created, err := r.ensureSnapshot(ctx, volumeSnapshot)
	if err != nil {
		return reconcile.Result{}, fmt.Errorf("failed to ensure snapshot: %w", err)
	}
	if created {
		r.log.Info("Volume snapshot created successfully", "snapshot", volumeSnapshot.GetName())
		return reconcile.Result{}, nil // return back later to observe the status
	}

	if err := r.cl.Get(ctx, client.ObjectKeyFromObject(volumeSnapshot), volumeSnapshot); err != nil {
		return reconcile.Result{}, fmt.Errorf("failed to get volume snapshot: %w", err)
	}

	switch {
	// no status reported
	case volumeSnapshot.Status == nil:
		return reconcile.Result{}, nil

	// snapshot is complete and ready to be restored.
	case ptr.Deref(volumeSnapshot.Status.ReadyToUse, false):
		if err := r.exec.finalize(ctx); err != nil {
			return reconcile.Result{}, fmt.Errorf("failed to complete snapshot: %w", err)
		}

		if updErr := r.backup.UpdateStatus(ctx, r.cl, func(bcp *v2.PerconaPGBackup) {
			bcp.Status.State = v2.BackupSucceeded
			bcp.Status.CompletedAt = ptr.To(metav1.Now())
		}); updErr != nil {
			return reconcile.Result{}, fmt.Errorf("failed to update backup status: %w", updErr)
		}
		r.log.Info("Snapshot is complete and ready to be used")

	// error occurred while creating the snapshot.
	case volumeSnapshot.Status.Error != nil:
		message := ptr.Deref(volumeSnapshot.Status.Error.Message, "")
		stsErr := fmt.Errorf("volume snapshot failed: %s", message)
		r.log.Error(stsErr, "Volume snapshot failed")
		if updErr := r.backup.UpdateStatus(ctx, r.cl, func(bcp *v2.PerconaPGBackup) {
			bcp.Status.State = v2.BackupFailed
			bcp.Status.Error = stsErr.Error()
		}); updErr != nil {
			return reconcile.Result{}, fmt.Errorf("failed to update backup status: %w", updErr)
		}
	}

	return reconcile.Result{}, nil
}

func (r *snapshotReconciler) ensureSnapshot(ctx context.Context, volumeSnapshot *volumesnapshotv1.VolumeSnapshot) (bool, error) {
	if err := r.cl.Create(ctx, volumeSnapshot); err != nil {
		return false, client.IgnoreAlreadyExists(err)
	}

	if updErr := r.backup.UpdateStatus(ctx, r.cl, func(bcp *v2.PerconaPGBackup) {
		if bcp.Status.Snapshot == nil {
			bcp.Status.Snapshot = &v2.SnapshotStatus{}
		}
		bcp.Status.Snapshot.VolumeSnapshotName = volumeSnapshot.GetName()
	}); updErr != nil {
		return true, fmt.Errorf("failed to update volumeSnapshot name in backup status: %w", updErr)
	}
	return true, nil
}

func (r *snapshotReconciler) prepare(ctx context.Context) error {
	// finalizer already present, prepare already completed
	if controllerutil.ContainsFinalizer(r.backup, pNaming.FinalizerCompleteSnapshot) {
		return nil
	}

	// prepare the cluster
	pvcTarget, err := r.exec.prepare(ctx)
	if err != nil {
		return fmt.Errorf("failed to prepare for snapshot: %w", err)
	}

	// update snapshot status
	if err := r.backup.UpdateStatus(ctx, r.cl, func(bcp *v2.PerconaPGBackup) {
		if bcp.Status.Snapshot == nil {
			bcp.Status.Snapshot = &v2.SnapshotStatus{}
		}
		bcp.Status.Snapshot.TargetPVCName = pvcTarget
	}); err != nil {
		return fmt.Errorf("failed to update backup status: %w", err)
	}

	// add finalizer
	if err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		bcp := r.backup.DeepCopy()
		if err := r.cl.Get(ctx, client.ObjectKeyFromObject(bcp), bcp); err != nil {
			return err
		}
		orig := bcp.DeepCopy()
		controllerutil.AddFinalizer(bcp, pNaming.FinalizerCompleteSnapshot)
		return r.cl.Patch(ctx, bcp, client.MergeFrom(orig))
	}); err != nil {
		return fmt.Errorf("failed to add backup finalizer: %w", err)
	}
	r.log.Info("Prepared for snapshot")
	return nil
}

func (r *snapshotReconciler) complete(ctx context.Context) error {
	// already finalized
	if !controllerutil.ContainsFinalizer(r.backup, pNaming.FinalizerCompleteSnapshot) {
		return nil
	}

	// run finalize
	if err := r.exec.finalize(ctx); err != nil {
		return fmt.Errorf("finalize failed: %w", err)
	}

	// remove finalizer
	if err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		bcp := r.backup.DeepCopy()
		if err := r.cl.Get(ctx, client.ObjectKeyFromObject(bcp), bcp); err != nil {
			return err
		}
		orig := bcp.DeepCopy()
		controllerutil.RemoveFinalizer(bcp, pNaming.FinalizerCompleteSnapshot)
		return r.cl.Patch(ctx, bcp, client.MergeFrom(orig))
	}); err != nil {
		return fmt.Errorf("failed to add remove finalizer: %w", err)
	}
	return nil
}
