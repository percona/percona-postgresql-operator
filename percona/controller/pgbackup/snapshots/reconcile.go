package snapshots

import (
	"context"
	"fmt"
	"time"

	volumesnapshotv1 "github.com/kubernetes-csi/external-snapshotter/client/v8/apis/volumesnapshot/v1"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/util/retry"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/percona/percona-postgresql-operator/v2/internal/controller/runtime"
	"github.com/percona/percona-postgresql-operator/v2/internal/feature"
	"github.com/percona/percona-postgresql-operator/v2/internal/logging"
	"github.com/percona/percona-postgresql-operator/v2/internal/naming"
	pNaming "github.com/percona/percona-postgresql-operator/v2/percona/naming"
	v2 "github.com/percona/percona-postgresql-operator/v2/pkg/apis/pgv2.percona.com/v2"
)

const (
	annotationBackupTarget = pNaming.PrefixPerconaPGV2 + "backup-target"

	defaultSnapshotErrorDeadline = 5 * time.Minute
)

type snapshotExecutor interface {
	// Prepare the cluster for performing a snapshot.
	// Returns the name of the instance whose PVCs will be snapshotted.
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
	podExec runtime.PodExecutor,
	cluster *v2.PerconaPGCluster,
	backup *v2.PerconaPGBackup,
) (snapshotExecutor, error) {
	switch mode := cluster.Spec.Backups.VolumeSnapshots.Mode; mode {
	case v2.VolumeSnapshotModeOffline:
		return newOfflineExec(cl, podExec, cluster, backup), nil
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
		return reconcile.Result{}, errors.New("PerconaPGBackup or PerconaPGCluster is nil or not found")
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
	if !pgCluster.Spec.Backups.IsVolumeSnapshotsEnabled() {
		if updErr := pgBackup.UpdateStatus(ctx, cl, func(bcp *v2.PerconaPGBackup) {
			bcp.Status.State = v2.BackupFailed
			bcp.Status.Error = "Volume snapshots are not enabled for this cluster"
		}); updErr != nil {
			return reconcile.Result{}, fmt.Errorf("failed to update backup status: %w", updErr)
		}
		return reconcile.Result{}, nil
	}

	exec, err := newSnapshotExec(cl, podExec, pgCluster, pgBackup)
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
		bcp.Status.Snapshot = &v2.SnapshotStatus{}
	}); updErr != nil {
		return reconcile.Result{}, fmt.Errorf("failed to update backup status: %w", updErr)
	}
	r.log.Info("Snapshot is running")
	return reconcile.Result{}, nil
}

// +kubebuilder:rbac:groups=snapshot.storage.k8s.io,resources=volumesnapshots,verbs=get;list;watch;create
func (r *snapshotReconciler) reconcileRunning(ctx context.Context) (reconcile.Result, error) {
	dataPVC, walPVC, tablespacePVCs, err := r.getTargetPVCs(ctx)
	if err != nil {
		return reconcile.Result{}, fmt.Errorf("failed to get target PVCs: %w", err)
	}

	dataOk, err := r.reconcileDataSnapshot(ctx, dataPVC)
	if err != nil {
		return reconcile.Result{}, fmt.Errorf("failed to reconcile data snapshot: %w", err)
	}

	walOk, err := r.reconcileWALSnapshot(ctx, walPVC)
	if err != nil {
		return reconcile.Result{}, fmt.Errorf("failed to reconcile WAL snapshot: %w", err)
	}

	tablespaceOk, err := r.reconcileTablespaceSnapshot(ctx, tablespacePVCs)
	if err != nil {
		return reconcile.Result{}, fmt.Errorf("failed to reconcile tablespace snapshot: %w", err)
	}

	if !dataOk || !walOk || !tablespaceOk {
		return reconcile.Result{RequeueAfter: time.Second * 5}, nil
	}

	if err := r.complete(ctx); err != nil {
		return reconcile.Result{}, fmt.Errorf("failed to complete snapshot: %w", err)
	}

	if err := r.backup.UpdateStatus(ctx, r.cl, func(bcp *v2.PerconaPGBackup) {
		bcp.Status.State = v2.BackupSucceeded
		bcp.Status.CompletedAt = ptr.To(metav1.Now())
	}); err != nil {
		return reconcile.Result{}, fmt.Errorf("failed to update backup status: %w", err)
	}
	return reconcile.Result{}, nil
}

func (r *snapshotReconciler) reconcileSnapshot(ctx context.Context, volumeSnapshot *volumesnapshotv1.VolumeSnapshot) (bool, error) {
	created, err := r.ensureSnapshot(ctx, volumeSnapshot)
	if err != nil {
		return false, fmt.Errorf("failed to ensure snapshot: %w", err)
	}

	log := r.log.WithValues("snapshot", volumeSnapshot.GetName())
	if created {
		log.Info("Volume snapshot created successfully")
		return false, nil // return back later to observe the status
	}

	if err := r.cl.Get(ctx, client.ObjectKeyFromObject(volumeSnapshot), volumeSnapshot); err != nil {
		return false, fmt.Errorf("failed to get volume snapshot: %w", err)
	}

	switch {
	// no status reported
	case volumeSnapshot.Status == nil:
		return false, nil

	// snapshot is complete and ready to be restored.
	case ptr.Deref(volumeSnapshot.Status.ReadyToUse, false):
		log.Info("Snapshot is complete and ready to be used")
		return true, nil

	// error occurred while creating the snapshot.
	case volumeSnapshot.Status.Error != nil:
		// Some errors can be transient, so we should wait for a while before giving up.
		message := ptr.Deref(volumeSnapshot.Status.Error.Message, "")
		if !shouldFailSnapshot(volumeSnapshot) {
			r.log.Info("Snapshot is in error state, but within deadline. Retrying.", "message", message)
			return false, nil
		}

		err := errors.New(message)

		log.Error(err, "Volume snapshot failed")
		return false, err

	default:
		return false, nil
	}
}

func (r *snapshotReconciler) generateSnapshotIntent(
	snapshotRole,
	sourcePVC string) (*volumesnapshotv1.VolumeSnapshot, error) {
	name := r.backup.GetName() + "-" + snapshotRole
	namespace := r.backup.GetNamespace()
	volumeSnapshot := &volumesnapshotv1.VolumeSnapshot{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: volumesnapshotv1.VolumeSnapshotSpec{
			VolumeSnapshotClassName: ptr.To(r.cluster.Spec.Backups.VolumeSnapshots.ClassName),
			Source: volumesnapshotv1.VolumeSnapshotSource{
				PersistentVolumeClaimName: &sourcePVC,
			},
		},
	}
	if err := controllerutil.SetControllerReference(r.backup, volumeSnapshot, r.cl.Scheme()); err != nil {
		return nil, fmt.Errorf("failed to set owner reference on volume snapshot: %w", err)
	}
	return volumeSnapshot, nil
}

func (r *snapshotReconciler) reconcileDataSnapshot(ctx context.Context, targetPVC string) (bool, error) {
	volumeSnapshot, err := r.generateSnapshotIntent(naming.RolePostgresData, targetPVC)
	if err != nil {
		return false, fmt.Errorf("failed to generate snapshot intent: %w", err)
	}

	ok, err := r.reconcileSnapshot(ctx, volumeSnapshot)
	if err != nil {
		return false, fmt.Errorf("failed to reconcile snapshot: %w", err)
	}

	if err := r.backup.UpdateStatus(ctx, r.cl, func(bcp *v2.PerconaPGBackup) {
		bcp.Status.Snapshot.DataVolumeSnapshotRef = ptr.To(volumeSnapshot.GetName())
	}); err != nil {
		return false, fmt.Errorf("failed to update backup status: %w", err)
	}
	return ok, nil
}

func (r *snapshotReconciler) reconcileWALSnapshot(ctx context.Context, targetPVC string) (bool, error) {
	if targetPVC == "" {
		return true, nil
	}

	volumeSnapshot, err := r.generateSnapshotIntent(naming.RolePostgresWAL, targetPVC)
	if err != nil {
		return false, fmt.Errorf("failed to generate snapshot intent: %w", err)
	}

	ok, err := r.reconcileSnapshot(ctx, volumeSnapshot)
	if err != nil {
		return false, fmt.Errorf("failed to reconcile snapshot: %w", err)
	}
	if err := r.backup.UpdateStatus(ctx, r.cl, func(bcp *v2.PerconaPGBackup) {
		bcp.Status.Snapshot.WALVolumeSnapshotRef = ptr.To(volumeSnapshot.GetName())
	}); err != nil {
		return false, fmt.Errorf("failed to update backup status: %w", err)
	}
	return ok, nil
}

func (r *snapshotReconciler) reconcileTablespaceSnapshot(ctx context.Context, targetPVCs map[string]string) (bool, error) {
	if len(targetPVCs) == 0 {
		return true, nil
	}

	done := true
	for tsName, targetPVC := range targetPVCs {
		role := tsName + "-" + naming.RoleTablespace
		volumeSnapshot, err := r.generateSnapshotIntent(role, targetPVC)
		if err != nil {
			return false, fmt.Errorf("failed to generate snapshot intent: %w", err)
		}

		ok, err := r.reconcileSnapshot(ctx, volumeSnapshot)
		if err != nil {
			return false, fmt.Errorf("failed to reconcile snapshot: %w", err)
		}

		if err := r.backup.UpdateStatus(ctx, r.cl, func(bcp *v2.PerconaPGBackup) {
			if bcp.Status.Snapshot.TablespaceVolumeSnapshotRefs == nil {
				bcp.Status.Snapshot.TablespaceVolumeSnapshotRefs = make(map[string]string)
			}
			bcp.Status.Snapshot.TablespaceVolumeSnapshotRefs[tsName] = volumeSnapshot.GetName()
		}); err != nil {
			return false, fmt.Errorf("failed to update backup status: %w", err)
		}
		if !ok {
			done = false
		}
	}
	return done, nil
}

func shouldFailSnapshot(volumeSnapshot *volumesnapshotv1.VolumeSnapshot) bool {
	if volumeSnapshot.Status == nil || volumeSnapshot.Status.Error == nil || volumeSnapshot.Status.Error.Time.IsZero() {
		return false
	}
	errAt := volumeSnapshot.Status.Error.Time
	return !errAt.IsZero() && time.Now().After(errAt.Add(defaultSnapshotErrorDeadline))
}

func (r *snapshotReconciler) ensureSnapshot(ctx context.Context, volumeSnapshot *volumesnapshotv1.VolumeSnapshot) (bool, error) {
	if err := r.cl.Create(ctx, volumeSnapshot); err != nil {
		return false, client.IgnoreAlreadyExists(err)
	}
	return true, nil
}

func (r *snapshotReconciler) getTargetPVCs(ctx context.Context) (string, string, map[string]string, error) {
	targetInstance := r.backup.GetAnnotations()[annotationBackupTarget]
	if targetInstance == "" {
		return "", "", nil, fmt.Errorf("backup target instance is not found")
	}

	dataPVC := ""
	var dataVolumes corev1.PersistentVolumeClaimList
	if err := r.cl.List(ctx, &dataVolumes, &client.ListOptions{
		Namespace: r.cluster.GetNamespace(),
		LabelSelector: labels.SelectorFromSet(map[string]string{
			naming.LabelInstance: targetInstance,
			naming.LabelRole:     naming.RolePostgresData,
		}),
	}); err != nil {
		return "", "", nil, fmt.Errorf("failed to list data volumes: %w", err)
	}
	if len(dataVolumes.Items) == 1 {
		dataPVC = dataVolumes.Items[0].GetName()
	} else {
		return "", "", nil, fmt.Errorf("unexpected number of data volumes: %d", len(dataVolumes.Items))
	}

	walPVC := ""
	var walVolumes corev1.PersistentVolumeClaimList
	if err := r.cl.List(ctx, &walVolumes, &client.ListOptions{
		Namespace: r.cluster.GetNamespace(),
		LabelSelector: labels.SelectorFromSet(map[string]string{
			naming.LabelInstance: targetInstance,
			naming.LabelRole:     naming.RolePostgresWAL,
		}),
	}); err != nil {
		return "", "", nil, fmt.Errorf("failed to list WAL volumes: %w", err)
	}
	if len(walVolumes.Items) == 1 {
		walPVC = walVolumes.Items[0].GetName()
	}

	tablespacePVCs := make(map[string]string)
	var tablespaceVolumes corev1.PersistentVolumeClaimList
	if err := r.cl.List(ctx, &tablespaceVolumes, &client.ListOptions{
		Namespace: r.cluster.GetNamespace(),
		LabelSelector: labels.SelectorFromSet(map[string]string{
			naming.LabelInstance: targetInstance,
			naming.LabelRole:     naming.RoleTablespace,
		}),
	}); err != nil {
		return "", "", nil, fmt.Errorf("failed to list tablespace volumes: %w", err)
	}

	for _, vol := range tablespaceVolumes.Items {
		name := vol.GetLabels()[naming.LabelData]
		tablespacePVCs[name] = vol.GetName()
	}

	return dataPVC, walPVC, tablespacePVCs, nil
}

func (r *snapshotReconciler) prepare(ctx context.Context) error {
	// finalizer already present, prepare already completed
	if controllerutil.ContainsFinalizer(r.backup, pNaming.FinalizerSnapshotInProgress) {
		return nil
	}

	// prepare the cluster
	targetInstance, err := r.exec.prepare(ctx)
	if err != nil {
		return fmt.Errorf("failed to prepare for snapshot: %w", err)
	}

	// Store the backup target instance for later retrieval.
	orig := r.backup.DeepCopy()
	annotations := r.backup.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string)
	}
	annotations[annotationBackupTarget] = targetInstance
	r.backup.SetAnnotations(annotations)
	if err := r.cl.Patch(ctx, r.backup.DeepCopy(), client.MergeFrom(orig)); err != nil {
		return fmt.Errorf("failed to patch backup annotations: %w", err)
	}

	// add finalizer
	if err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		bcp := r.backup.DeepCopy()
		if err := r.cl.Get(ctx, client.ObjectKeyFromObject(bcp), bcp); err != nil {
			return err
		}
		orig := bcp.DeepCopy()
		controllerutil.AddFinalizer(bcp, pNaming.FinalizerSnapshotInProgress)
		return r.cl.Patch(ctx, bcp, client.MergeFrom(orig))
	}); err != nil {
		return fmt.Errorf("failed to add backup finalizer: %w", err)
	}
	r.log.Info("Prepared for snapshot")
	return nil
}

func (r *snapshotReconciler) complete(ctx context.Context) error {
	// already finalized
	if !controllerutil.ContainsFinalizer(r.backup, pNaming.FinalizerSnapshotInProgress) {
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
		controllerutil.RemoveFinalizer(bcp, pNaming.FinalizerSnapshotInProgress)
		return r.cl.Patch(ctx, bcp, client.MergeFrom(orig))
	}); err != nil {
		return fmt.Errorf("failed to remove finalizer: %w", err)
	}
	return nil
}
