package snapshot

import (
	"context"
	"fmt"
	"time"

	volumesnapshotv1 "github.com/kubernetes-csi/external-snapshotter/client/v8/apis/volumesnapshot/v1"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/yaml"

	"github.com/percona/percona-postgresql-operator/v2/internal/feature"
	"github.com/percona/percona-postgresql-operator/v2/internal/logging"
	"github.com/percona/percona-postgresql-operator/v2/internal/naming"
	"github.com/percona/percona-postgresql-operator/v2/percona/controller"
	pNaming "github.com/percona/percona-postgresql-operator/v2/percona/naming"
	v2 "github.com/percona/percona-postgresql-operator/v2/pkg/apis/pgv2.percona.com/v2"
)

type snapshotRestorer struct {
	cl      client.Client
	log     logging.Logger
	cluster *v2.PerconaPGCluster
	restore *v2.PerconaPGRestore
}

func newSnapshotRestorer(
	cl client.Client,
	log logging.Logger,
	cluster *v2.PerconaPGCluster,
	restore *v2.PerconaPGRestore,
) *snapshotRestorer {
	return &snapshotRestorer{
		cl:      cl,
		log:     log,
		cluster: cluster,
		restore: restore,
	}
}

func Reconcile(
	ctx context.Context,
	c client.Client,
	pg *v2.PerconaPGCluster,
	restore *v2.PerconaPGRestore,
) (reconcile.Result, error) {
	log := logging.FromContext(ctx).WithName("SnapshotRestorer")

	if !feature.Enabled(ctx, feature.BackupSnapshots) {
		log.Info(fmt.Sprintf("Feature gate '%s' is not enabled, skipping snapshot restore", feature.BackupSnapshots))
		return reconcile.Result{}, nil
	}

	r := newSnapshotRestorer(c, log, pg, restore)

	if !restore.GetDeletionTimestamp().IsZero() {
		if ok, err := r.runFinalizers(ctx); err != nil {
			return reconcile.Result{}, errors.Wrap(err, "run finalizers")
		} else if !ok {
			return reconcile.Result{RequeueAfter: time.Second * 5}, nil
		}
		return reconcile.Result{}, nil
	}

	switch restore.Status.State {
	case v2.RestoreNew:
		return r.reconcileNew(ctx)
	case v2.RestoreStarting:
		return r.reconcileStarting(ctx)
	case v2.RestoreRunning:
		return r.reconcileRunning(ctx)
	case v2.RestoreSucceeded, v2.RestoreFailed:
		ok, err := r.runFinalizers(ctx)
		if err != nil {
			return reconcile.Result{}, errors.Wrap(err, "run finalizers")
		}
		if !ok {
			return reconcile.Result{RequeueAfter: time.Second * 5}, nil
		}
		return reconcile.Result{}, nil
	}
	return reconcile.Result{}, nil
}

func (r *snapshotRestorer) reconcileNew(ctx context.Context) (reconcile.Result, error) {
	if restore := r.cluster.Spec.Backups.PGBackRest.Restore; restore != nil && *restore.Enabled {
		r.log.Info("Waiting for another restore to finish")
		return reconcile.Result{RequeueAfter: time.Second * 5}, nil
	}

	restores := &v2.PerconaPGRestoreList{}
	if err := r.cl.List(ctx, restores, client.InNamespace(r.cluster.Namespace)); err != nil {
		return reconcile.Result{}, errors.Wrap(err, "list restores")
	}
	for _, restore := range restores.Items {
		if restore.Spec.PGCluster != r.cluster.Name || restore.IsCompleted() || restore.GetName() == r.restore.GetName() {
			continue
		}
		r.log.Info("Waiting for another restore to finish")
		return reconcile.Result{RequeueAfter: time.Second * 5}, nil
	}

	if err := r.restore.UpdateStatus(ctx, r.cl, func(restore *v2.PerconaPGRestore) {
		restore.Status.State = v2.RestoreStarting
	}); err != nil {
		return reconcile.Result{}, errors.Wrap(err, "update restore status")
	}
	r.log.Info("Snapshot restore is starting")
	return reconcile.Result{}, nil
}

func (r *snapshotRestorer) reconcileStarting(ctx context.Context) (reconcile.Result, error) {
	// Check if specified volume snapshot exists
	volumeSnapshotName := r.restore.Spec.VolumeSnapshotName
	volumeSnapshot := &volumesnapshotv1.VolumeSnapshot{}
	if err := r.cl.Get(ctx, types.NamespacedName{Name: volumeSnapshotName, Namespace: r.cluster.Namespace}, volumeSnapshot); err != nil {
		if k8serrors.IsNotFound(err) {
			r.log.Info("Volume snapshot not found, failing restore")
			if err := r.restore.UpdateStatus(ctx, r.cl, func(restore *v2.PerconaPGRestore) {
				restore.Status.State = v2.RestoreFailed
			}); err != nil {
				return reconcile.Result{}, errors.Wrap(err, "update restore status")
			}
			return reconcile.Result{}, nil
		}
		return reconcile.Result{}, errors.Wrap(err, "get volume snapshot")
	}

	// pausing the cluster so the PVCs are unmounted and can be re-created.
	if ok, err := r.pauseCluster(ctx); err != nil {
		return reconcile.Result{}, errors.Wrap(err, "pause cluster")
	} else if !ok {
		r.log.Info("Waiting for cluster to be paused")
		return reconcile.Result{RequeueAfter: time.Second * 5}, nil
	}

	if err := r.ensureFinalizers(ctx); err != nil {
		return reconcile.Result{}, errors.Wrap(err, "ensure finalizers")
	}

	if err := r.restore.UpdateStatus(ctx, r.cl, func(restore *v2.PerconaPGRestore) {
		restore.Status.State = v2.RestoreRunning
	}); err != nil {
		return reconcile.Result{}, errors.Wrap(err, "update restore status")
	}

	r.log.Info("Snapshot restore is running")
	return reconcile.Result{}, nil
}

func (r *snapshotRestorer) reconcileRunning(ctx context.Context) (reconcile.Result, error) {
	volumeSnapshotName := r.restore.Spec.VolumeSnapshotName
	clusterPVCs, err := r.listPVCs(ctx)
	if err != nil {
		return reconcile.Result{}, errors.Wrap(err, "list PVCs")
	}

	for _, pvc := range clusterPVCs.Items {
		if ok, err := r.replacePVC(ctx, &pvc, volumeSnapshotName); err != nil {
			return reconcile.Result{}, errors.Wrap(err, "replace PVC")
		} else if !ok {
			r.log.Info("Waiting for PVC to be replaced", "pvc", pvc.GetName())
			return reconcile.Result{RequeueAfter: time.Second * 5}, nil
		}
	}

	// Start the cluster
	if ok, err := r.resumeCluster(ctx); err != nil {
		return reconcile.Result{}, errors.Wrap(err, "resume cluster")
	} else if !ok {
		r.log.Info("Waiting for cluster to be ready")
		return reconcile.Result{RequeueAfter: time.Second * 5}, nil
	}

	// TODO: Implement PiTR

	if err := r.restore.UpdateStatus(ctx, r.cl, func(restore *v2.PerconaPGRestore) {
		restore.Status.State = v2.RestoreSucceeded
		restore.Status.CompletedAt = ptr.To(metav1.Now())
	}); err != nil {
		return reconcile.Result{}, errors.Wrap(err, "update restore status")
	}

	r.log.Info("Snapshot restore complete")
	return reconcile.Result{}, nil
}

func (r *snapshotRestorer) pvcStateConfigMapName(restoreName string) metav1.ObjectMeta {
	return metav1.ObjectMeta{
		Namespace: r.cluster.Namespace,
		Name:      fmt.Sprintf("pvc-state-%s-%s", r.cluster.Name, restoreName),
	}
}

// listPVCs retrieves the list of PVCs that need to be restored from snapshots.
//
// This function maintains a ConfigMap that stores the PVC specifications before
// they are deleted and recreated. This serves two critical purposes:
//
//  1. If the operator restarts during a restore operation, the
//     ConfigMap preserves the list of PVCs that still need to be processed,
//     allowing the restore to continue seamlessly.
//  2. During restore, PVCs are deleted and recreated from
//     snapshots. Once deleted, the original PVC specifications are lost. By storing
//     the state externally, the operator can identidy which PVCs need to be tracked.
func (r *snapshotRestorer) listPVCs(
	ctx context.Context,
) (*corev1.PersistentVolumeClaimList, error) {
	cm := &corev1.ConfigMap{
		ObjectMeta: r.pvcStateConfigMapName(r.restore.Name),
	}

	result := &corev1.PersistentVolumeClaimList{}

	cmKey := "pvcs.yaml"
	err := r.cl.Get(ctx, client.ObjectKeyFromObject(cm), cm)

	switch {
	// ConfigMap was found.
	case err == nil:
		data := cm.Data[cmKey]
		if err := yaml.Unmarshal([]byte(data), result); err != nil {
			return nil, errors.Wrap(err, "unmarshal PVC state")
		}
		return result, nil

	// ConfigMap was not found, list the PVCs and create the ConfigMap.
	case k8serrors.IsNotFound(err):
		if err := r.cl.List(ctx, result, &client.ListOptions{
			Namespace: r.cluster.GetNamespace(),
			LabelSelector: labels.SelectorFromSet(map[string]string{
				naming.LabelCluster: r.cluster.GetName(),
				naming.LabelRole:    naming.RolePostgresData,
			}),
		}); err != nil {
			return nil, errors.Wrap(err, "list instance PVCs")
		}
		data, err := yaml.Marshal(result)
		if err != nil {
			return nil, errors.Wrap(err, "marshal PVC state")
		}
		cm.Data = map[string]string{cmKey: string(data)}
		// TODO: add a finalizer on this configmap?
		return result, r.cl.Create(ctx, cm)
	}
	return nil, err
}

func (r *snapshotRestorer) replacePVC(
	ctx context.Context,
	pvc *corev1.PersistentVolumeClaim,
	snapshotName string,
) (bool, error) {
	observedPVC := &corev1.PersistentVolumeClaim{}
	err := r.cl.Get(ctx, client.ObjectKeyFromObject(pvc), observedPVC)

	if k8serrors.IsNotFound(err) {
		// PVC doesn't exist, create it from the snapshot
		if err := r.createPVCWithSnapshot(ctx, pvc, snapshotName); err != nil {
			return false, errors.Wrap(err, "create PVC with snapshot")
		}
		return false, nil
	} else if err != nil {
		return false, errors.Wrap(err, "get observed PVC")
	}

	// Check if the PVC is already using the snapshot
	if dataSource := observedPVC.Spec.DataSource; dataSource != nil {
		if dataSource.Kind == "VolumeSnapshot" &&
			ptr.Deref(dataSource.APIGroup, "") == volumesnapshotv1.GroupName &&
			dataSource.Name == snapshotName {
			return true, nil
		}
	}

	// If deleting, wait for it to be deleted before recreating
	if !observedPVC.GetDeletionTimestamp().IsZero() {
		return false, nil
	}

	// Delete the existing PVC so we can recreate it from the snapshot
	if err := r.cl.Delete(ctx, observedPVC); err != nil {
		return false, errors.Wrap(err, "delete PVC")
	}
	return false, nil
}

func (r *snapshotRestorer) createPVCWithSnapshot(ctx context.Context, pvc *corev1.PersistentVolumeClaim, snapshotName string) error {
	instanceName := pvc.GetLabels()[naming.LabelInstanceSet]
	if instanceName == "" {
		return errors.New("instance not known for PVC")
	}
	var volumeClaimSpec *corev1.PersistentVolumeClaimSpec
	for _, instanceSet := range r.cluster.Spec.InstanceSets {
		if instanceSet.Name == instanceName {
			volumeClaimSpec = &instanceSet.DataVolumeClaimSpec
			break
		}
	}
	if volumeClaimSpec == nil {
		return fmt.Errorf("instance set '%s' either not found or has no data volume claim spec", instanceName)
	}
	volumeClaimSpec.DataSource = &corev1.TypedLocalObjectReference{
		APIGroup: ptr.To(volumesnapshotv1.GroupName),
		Kind:     "VolumeSnapshot",
		Name:     snapshotName,
	}
	newPVC := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pvc.GetName(),
			Namespace: pvc.GetNamespace(),
		},
		Spec: *volumeClaimSpec,
	}
	return r.cl.Create(ctx, newPVC)
}

func (r *snapshotRestorer) pauseCluster(ctx context.Context) (bool, error) {
	// Check if already paused
	if r.cluster.Spec.Pause != nil && *r.cluster.Spec.Pause {
		return r.cluster.Status.State == v2.AppStatePaused, nil
	}

	// Pause the cluster
	if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		orig := r.cluster.DeepCopy()
		updated := orig.DeepCopy()
		if err := r.cl.Get(ctx, client.ObjectKeyFromObject(updated), updated); err != nil {
			return err
		}
		updated.Spec.Pause = ptr.To(true)
		return r.cl.Patch(ctx, updated, client.MergeFrom(orig))
	}); err != nil {
		return false, err
	}
	return false, nil
}

func (r *snapshotRestorer) resumeCluster(ctx context.Context) (bool, error) {
	// Check if already resumed
	if r.cluster.Spec.Pause == nil || !*r.cluster.Spec.Pause {
		return r.cluster.Status.State == v2.AppStateReady, nil
	}

	// Resume the cluster
	if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		orig := r.cluster.DeepCopy()
		updated := orig.DeepCopy()
		if err := r.cl.Get(ctx, client.ObjectKeyFromObject(updated), updated); err != nil {
			return err
		}
		updated.Spec.Pause = nil
		return r.cl.Patch(ctx, updated, client.MergeFrom(orig))
	}); err != nil {
		return false, err
	}
	return false, nil
}

func (r *snapshotRestorer) ensureFinalizers(ctx context.Context) error {
	orig := r.restore.DeepCopy()

	finalizers := []string{pNaming.FinalizerSnapshotRestore}
	finalizersChanged := false
	for _, f := range finalizers {
		if controllerutil.AddFinalizer(r.restore, f) {
			finalizersChanged = true
		}
	}
	if !finalizersChanged {
		return nil
	}

	if err := r.cl.Patch(ctx, r.restore.DeepCopy(), client.MergeFrom(orig)); err != nil {
		return errors.Wrap(err, "patch finalizers")
	}
	return nil
}

func (r *snapshotRestorer) runFinalizers(ctx context.Context) (bool, error) {
	finalizers := map[string]controller.FinalizerFunc[*v2.PerconaPGRestore]{
		pNaming.FinalizerSnapshotRestore: r.finalizeSnapshotRestore(r.cl, r.restore),
	}

	finished := true
	for finalizer, f := range finalizers {
		done, err := controller.RunFinalizer(ctx, r.cl, r.restore, finalizer, f)
		if err != nil {
			return false, errors.Wrapf(err, "run finalizer %s", finalizer)
		}
		if !done {
			finished = false
		}
	}
	return finished, nil
}

func (r *snapshotRestorer) finalizeSnapshotRestore(c client.Client, _ *v2.PerconaPGRestore) func(ctx context.Context, restore *v2.PerconaPGRestore) error {
	return func(ctx context.Context, restore *v2.PerconaPGRestore) error {
		// Resume the cluster if it was paused during restore
		if _, err := r.resumeCluster(ctx); err != nil {
			return errors.Wrap(err, "resume cluster")
		}

		// Always clean up the PVC state ConfigMap regardless of restore success or failure
		cm := &corev1.ConfigMap{
			ObjectMeta: r.pvcStateConfigMapName(restore.Name),
		}
		if err := c.Delete(ctx, cm); client.IgnoreNotFound(err) != nil {
			return errors.Wrap(err, "delete PVC state configmap")
		}
		return nil
	}
}
