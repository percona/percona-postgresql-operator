package snapshots

import (
	"context"
	"errors"
	"fmt"
	"time"

	volumesnapshotv1 "github.com/kubernetes-csi/external-snapshotter/client/v4/apis/volumesnapshot/v1"
	"github.com/percona/percona-postgresql-operator/v2/internal/feature"
	"github.com/percona/percona-postgresql-operator/v2/internal/logging"
	v2 "github.com/percona/percona-postgresql-operator/v2/pkg/apis/pgv2.percona.com/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type snapshotExecutor interface {
	// Prepare the cluster for performing a snapshot.
	// Returns the name of the PVC that will be snapshotted.
	prepare(ctx context.Context, pgCluster *v2.PerconaPGCluster) (string, error)

	// Complete the snapshot.
	complete(ctx context.Context, pgCluster *v2.PerconaPGCluster) error
}

// Reconcile backup snapshot
func Reconcile(
	ctx context.Context,
	cl client.Client,
	pgBackup *v2.PerconaPGBackup,
	pgCluster *v2.PerconaPGCluster,
) (reconcile.Result, error) {

	log := logging.FromContext(ctx).
		WithName("SnapshotReconciler").
		WithValues("backup", pgBackup.Name, "cluster", pgCluster.Name)

	if !feature.Enabled(ctx, feature.VolumeSnapshots) {
		log.Info(fmt.Sprintf("Feature gate '%s' is not enabled, skipping snapshot reconciliation", feature.BackupSnapshots))
		return reconcile.Result{}, nil
	}

	// TODO: implement executor
	var exec snapshotExecutor

	switch pgBackup.Status.State {
	case v2.BackupNew:
		return handleStateNew(ctx, log, cl, pgBackup, pgCluster)
	case v2.BackupStarting:
		return handleStateStarting(ctx, log, cl, exec, pgBackup, pgCluster)
	case v2.BackupRunning:
		return handleStateRunning(ctx, log, exec, cl, pgBackup, pgCluster)
	case v2.BackupFailed:
		log.Info("Backup failed")
	case v2.BackupSucceeded:
		log.Info("Backup succeeded")
	}
	return reconcile.Result{}, nil
}

// +kubebuilder:rbac:groups=snapshot.storage.k8s.io,resources=volumesnapshotclasses,verbs=get;list;watch
func handleStateNew(
	ctx context.Context,
	log logging.Logger,
	cl client.Client,
	backup *v2.PerconaPGBackup,
	pgCluster *v2.PerconaPGCluster,
) (reconcile.Result, error) {
	// Ensure that the volume snapshot class exists.
	className := pgCluster.Spec.Backups.VolumeSnapshots.ClassName
	if className == "" {
		return reconcile.Result{}, errors.New("volume snapshot class name is not set")
	}
	volumeSnapshotClass := &volumesnapshotv1.VolumeSnapshotClass{}
	if err := cl.Get(ctx, client.ObjectKey{Name: className}, volumeSnapshotClass); err != nil {
		stsErr := fmt.Errorf("failed to get volume snapshot class: %w", err)
		backup.Status.State = v2.BackupFailed
		backup.Status.Error = stsErr.Error()
		return reconcile.Result{}, stsErr
	}

	if pgCluster.Status.State != v2.AppStateReady {
		log.Info("Waiting for cluster to be ready before creating snapshot")
		return reconcile.Result{RequeueAfter: time.Second * 5}, nil
	}

	backup.Status.State = v2.BackupStarting
	if err := cl.Status().Update(ctx, backup); err != nil {
		return reconcile.Result{}, fmt.Errorf("failed to update backup status: %w", err)
	}
	log.Info("Backup is starting")
	return reconcile.Result{}, nil
}

func handleStateStarting(
	ctx context.Context,
	log logging.Logger,
	cl client.Client,
	exec snapshotExecutor,
	backup *v2.PerconaPGBackup,
	pgCluster *v2.PerconaPGCluster) (reconcile.Result, error) {

	pvcTarget, err := exec.prepare(ctx, pgCluster)
	if err != nil {
		return reconcile.Result{}, fmt.Errorf("failed to prepare for snapshot: %w", err)
	}

	backup.Status.State = v2.BackupRunning
	backup.Status.Snapshot = &v2.SnapshotStatus{
		TargetPVCName: pvcTarget,
	}

	if err := cl.Status().Update(ctx, backup); err != nil {
		return reconcile.Result{}, fmt.Errorf("failed to update backup status: %w", err)
	}
	log.Info("Creating snapshot")
	return reconcile.Result{}, nil
}

// +kubebuilder:rbac:groups=snapshot.storage.k8s.io,resources=volumesnapshots,verbs=get;list;watch;create
func handleStateRunning(
	ctx context.Context,
	log logging.Logger,
	exec snapshotExecutor,
	cl client.Client,
	backup *v2.PerconaPGBackup,
	pgCluster *v2.PerconaPGCluster,
) (reconcile.Result, error) {
	volumeSnapshot := &volumesnapshotv1.VolumeSnapshot{
		ObjectMeta: metav1.ObjectMeta{
			Name:      backup.GetName(),
			Namespace: backup.GetNamespace(),
		},
		Spec: volumesnapshotv1.VolumeSnapshotSpec{
			VolumeSnapshotClassName: ptr.To(pgCluster.Spec.Backups.VolumeSnapshots.ClassName),
			Source: volumesnapshotv1.VolumeSnapshotSource{
				PersistentVolumeClaimName: &backup.Status.Snapshot.TargetPVCName,
			},
		},
	}

	if err := cl.Create(ctx, volumeSnapshot); client.IgnoreAlreadyExists(err) != nil {
		return reconcile.Result{}, fmt.Errorf("failed to create volume snapshot: %w", err)
	}

	if err := cl.Get(ctx, client.ObjectKeyFromObject(volumeSnapshot), volumeSnapshot); err != nil {
		return reconcile.Result{}, fmt.Errorf("failed to get volume snapshot: %w", err)
	}

	if backup.Status.Snapshot.PVCName == "" {
		backup.Status.Snapshot.PVCName = volumeSnapshot.GetName()
		if err := cl.Status().Update(ctx, backup); err != nil {
			return reconcile.Result{}, fmt.Errorf("failed to update backup status: %w", err)
		}
	}

	switch {
	// snapshot is complete and ready to be restored.
	case ptr.Deref(volumeSnapshot.Status.ReadyToUse, false):
		if err := exec.complete(ctx, pgCluster); err != nil {
			return reconcile.Result{}, fmt.Errorf("failed to complete snapshot: %w", err)
		}
		log.Info("Snapshot is complete and ready to be used")

		backup.Status.State = v2.BackupSucceeded
		backup.Status.CompletedAt = ptr.To(metav1.Now())
	// error occurred while creating the snapshot.
	case volumeSnapshot.Status.Error != nil:
		message := volumeSnapshot.Status.Error.Message
		return reconcile.Result{}, fmt.Errorf("failed to create volume snapshot: %s", ptr.Deref(message, ""))
	}

	// snapshot is still being created.
	// TODO: controller should watch the snapshot for changes rather that periodically requeue.
	return reconcile.Result{RequeueAfter: time.Second * 5}, nil
}
