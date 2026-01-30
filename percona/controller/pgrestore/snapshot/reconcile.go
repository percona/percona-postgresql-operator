package snapshot

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	volumesnapshotv1 "github.com/kubernetes-csi/external-snapshotter/client/v8/apis/volumesnapshot/v1"
	"github.com/pkg/errors"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/percona/percona-postgresql-operator/v2/internal/feature"
	"github.com/percona/percona-postgresql-operator/v2/internal/logging"
	"github.com/percona/percona-postgresql-operator/v2/internal/naming"
	"github.com/percona/percona-postgresql-operator/v2/percona/controller"
	restoreutils "github.com/percona/percona-postgresql-operator/v2/percona/controller/pgrestore/utils"
	pNaming "github.com/percona/percona-postgresql-operator/v2/percona/naming"
	perconaPG "github.com/percona/percona-postgresql-operator/v2/percona/postgres"
	v2 "github.com/percona/percona-postgresql-operator/v2/pkg/apis/pgv2.percona.com/v2"
	"github.com/percona/percona-postgresql-operator/v2/pkg/apis/postgres-operator.crunchydata.com/v1beta1"
	crunchyv1beta1 "github.com/percona/percona-postgresql-operator/v2/pkg/apis/postgres-operator.crunchydata.com/v1beta1"
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
	if ok, err := r.suspendAllInstances(ctx); err != nil {
		return reconcile.Result{}, errors.Wrap(err, "suspend all instances")
	} else if !ok {
		r.log.Info("Waiting for all instances to be suspended")
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

	// Restore PVCs
	for _, pvc := range clusterPVCs {
		if ok, err := r.restorePVCFromSnapshot(ctx, &pvc, volumeSnapshotName); err != nil {
			return reconcile.Result{}, errors.Wrap(err, "restore PVC from snapshot")
		} else if !ok {
			r.log.Info("Waiting for PVC to restored", "pvc", pvc.GetName())
			return reconcile.Result{RequeueAfter: time.Second * 5}, nil
		}
	}

	// Restore PITR (if needed)
	if ok, err := r.restorePITR(ctx); err != nil {
		return reconcile.Result{}, errors.Wrap(err, "restore PITR")
	} else if !ok {
		r.log.Info("Waiting for PiTR to complete")
		return reconcile.Result{RequeueAfter: time.Second * 5}, nil
	}

	// Re-create (if needed) Patroni leader Endpoints so the cluster can be bootstrapped from the new data.
	if err := r.reconcileLeaderEndpoints(ctx); err != nil {
		return reconcile.Result{}, errors.Wrap(err, "delete leader endpoints")
	}

	// Start the cluster
	if ok, err := r.unsuspendAllInstances(ctx); err != nil {
	} else if !ok && !r.isPITRInProgress() {
		r.log.Info("Waiting for all instances to be unsuspended")
		return reconcile.Result{RequeueAfter: time.Second * 5}, nil
	}

	if err := r.restore.UpdateStatus(ctx, r.cl, func(restore *v2.PerconaPGRestore) {
		restore.Status.State = v2.RestoreSucceeded
		restore.Status.CompletedAt = ptr.To(metav1.Now())
	}); err != nil {
		return reconcile.Result{}, errors.Wrap(err, "update restore status")
	}

	r.log.Info("Snapshot restore complete")
	return reconcile.Result{}, nil
}

func (r *snapshotRestorer) reconcileLeaderEndpoints(ctx context.Context) error {
	postgresCluster := &crunchyv1beta1.PostgresCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      r.cluster.Name,
			Namespace: r.cluster.Namespace,
		},
	}

	leaderEp := &corev1.Endpoints{ObjectMeta: naming.PatroniLeaderEndpoints(postgresCluster)}
	if err := r.cl.Get(ctx, client.ObjectKeyFromObject(leaderEp), leaderEp); err != nil {
		return client.IgnoreNotFound(err)
	}

	if len(leaderEp.Subsets) > 0 {
		return nil
	}

	if err := r.cl.Delete(ctx, leaderEp); client.IgnoreNotFound(err) != nil {
		return errors.Wrap(err, "delete leader endpoints")
	}
	return nil
}

// listPVCs returns the list of PostgreSQL data PVCs that need to be restored.
//
// Instead of listing existing PVCs directly, this function derives the PVC names
// from the cluster instance statefulsets. This is necessary because during restore,
// PVCs are deleted and recreated. Listing live PVCs would miss PVCs that are
// currently being recreated and lead to an inconsistent state.
//
// The function returns PVC objects with only metadata populated,
// which is sufficient for getting the job done.
func (r *snapshotRestorer) listPVCs(ctx context.Context) ([]corev1.PersistentVolumeClaim, error) {
	instances := &appsv1.StatefulSetList{}
	if err := r.cl.List(ctx, instances, &client.ListOptions{
		Namespace: r.cluster.GetNamespace(),
		LabelSelector: labels.SelectorFromSet(map[string]string{
			naming.LabelCluster: r.cluster.Name,
			naming.LabelData:    naming.DataPostgres,
		}),
	}); err != nil {
		return nil, errors.Wrap(err, "list instances")
	}

	result := []corev1.PersistentVolumeClaim{}
	for _, instance := range instances.Items {
		objectMeta := naming.InstancePostgresDataVolume(&instance)
		objectMeta.SetLabels(map[string]string{
			naming.LabelInstanceSet: instance.Labels[naming.LabelInstanceSet], // needed for createPVCFromSnapshot
		})
		result = append(result, corev1.PersistentVolumeClaim{
			ObjectMeta: objectMeta,
		})
	}

	// sort to ensure consistent ordering
	slices.SortStableFunc(result, func(a, b corev1.PersistentVolumeClaim) int {
		return strings.Compare(a.GetName(), b.GetName())
	})
	return result, nil
}

// restorePVCFromSnapshot restores a PVC from a snapshot.
// pvc is a partial object derived from the cluster instance statefulsets (see listPVCs method).
func (r *snapshotRestorer) restorePVCFromSnapshot(
	ctx context.Context,
	pvc *corev1.PersistentVolumeClaim,
	snapshotName string,
) (bool, error) {
	observedPVC := &corev1.PersistentVolumeClaim{}
	err := r.cl.Get(ctx, client.ObjectKeyFromObject(pvc), observedPVC)

	if k8serrors.IsNotFound(err) {
		// PVC doesn't exist, create it from the snapshot
		if err := r.createPVCFromSnapshot(ctx, pvc, snapshotName); err != nil {
			return false, errors.Wrap(err, "create PVC with snapshot")
		}
		return false, nil
	} else if err != nil {
		return false, errors.Wrap(err, "get observed PVC")
	}

	// Check if the PVC is already using the snapshot
	if val, ok := observedPVC.GetAnnotations()[pNaming.AnnotationSnapshotRestore]; ok && val == r.restore.GetName() {
		return true, nil
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

func (r *snapshotRestorer) createPVCFromSnapshot(ctx context.Context, pvc *corev1.PersistentVolumeClaim, snapshotName string) error {
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
		Kind:     pNaming.KindVolumeSnapshot,
		Name:     snapshotName,
	}
	newPVC := &corev1.PersistentVolumeClaim{
		ObjectMeta: pvc.ObjectMeta,
		Spec:       *volumeClaimSpec,
	}

	newPVC.SetAnnotations(map[string]string{
		pNaming.AnnotationSnapshotRestore: r.restore.GetName(),
	})
	return r.cl.Create(ctx, newPVC)
}

func (r *snapshotRestorer) suspendAllInstances(ctx context.Context) (bool, error) {
	instances := &appsv1.StatefulSetList{}
	if err := r.cl.List(ctx, instances, &client.ListOptions{
		Namespace: r.cluster.GetNamespace(),
		LabelSelector: labels.SelectorFromSet(map[string]string{
			naming.LabelCluster: r.cluster.Name,
			naming.LabelData:    naming.DataPostgres,
		}),
	}); err != nil {
		return false, errors.Wrap(err, "list instances")
	}

	allSuspended := true
	for _, instance := range instances.Items {
		if suspended, err := perconaPG.SuspendInstance(ctx, r.cl, client.ObjectKeyFromObject(&instance)); err != nil {
			return false, errors.Wrap(err, "suspend instance")
		} else if !suspended {
			allSuspended = false
		}
	}
	return allSuspended, nil
}

func (r *snapshotRestorer) unsuspendAllInstances(ctx context.Context) (bool, error) {
	instances := &appsv1.StatefulSetList{}
	if err := r.cl.List(ctx, instances, &client.ListOptions{
		Namespace: r.cluster.GetNamespace(),
		LabelSelector: labels.SelectorFromSet(map[string]string{
			naming.LabelCluster: r.cluster.Name,
			naming.LabelData:    naming.DataPostgres,
		}),
	}); err != nil {
		return false, errors.Wrap(err, "list instances")
	}

	allUnsuspended := true
	for _, instance := range instances.Items {
		if unsuspended, err := perconaPG.UnsuspendInstance(ctx, r.cl, client.ObjectKeyFromObject(&instance)); err != nil {
			return false, errors.Wrap(err, "unsuspend instance")
		} else if !unsuspended {
			allUnsuspended = false
		}
	}
	return allUnsuspended, nil
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

func (r *snapshotRestorer) finalizeSnapshotRestore(_ client.Client, _ *v2.PerconaPGRestore) func(ctx context.Context, restore *v2.PerconaPGRestore) error {
	return func(ctx context.Context, restore *v2.PerconaPGRestore) error {
		if err := r.enableWALArchiveRecovery(ctx); err != nil {
			return errors.Wrap(err, "enable WAL archive recovery")
		}
		// Resume the cluster if it was paused during restore
		if _, err := r.unsuspendAllInstances(ctx); err != nil {
			return errors.Wrap(err, "unsuspend all instances")
		}
		return nil
	}
}

func (r *snapshotRestorer) restorePITR(ctx context.Context) (bool, error) {
	if r.restore.Spec.RepoName == nil {
		// PiTR is not needed, no need to recover WAL archives to maintain snapshot consistency.
		if err := r.disableWALArchiveRecovery(ctx); err != nil {
			return false, errors.Wrap(err, "disable WAL archive recovery")
		}
		return true, nil
	}

	pgbackrestRestore := restoreutils.NewPGBackRestRestore(r.cl, r.cluster, r.restore)
	status, _, err := pgbackrestRestore.ObserveStatus(ctx)
	if err != nil {
		return false, errors.Wrap(err, "observe PITR status")
	}

	switch status {
	case v2.RestoreStarting:
		return false, pgbackrestRestore.Start(ctx)
	case v2.RestoreRunning:
		return false, nil
	case v2.RestoreSucceeded:
		return true, pgbackrestRestore.DisableRestore(ctx)
	case v2.RestoreFailed:
		if err := r.restore.UpdateStatus(ctx, r.cl, func(restore *v2.PerconaPGRestore) {
			restore.Status.State = v2.RestoreFailed
		}); err != nil {
			return false, errors.Wrap(err, "update restore status")
		}
		return true, nil
	}
	return false, nil
}

func (r *snapshotRestorer) isPITRInProgress() bool {
	return r.cluster.GetAnnotations()[naming.PGBackRestRestore] != ""
}

func (r *snapshotRestorer) disableWALArchiveRecovery(ctx context.Context) error {
	orig := r.cluster.DeepCopy()
	for i := range r.cluster.Spec.InstanceSets {
		if r.cluster.Spec.InstanceSets[i].Metadata == nil {
			r.cluster.Spec.InstanceSets[i].Metadata = &v1beta1.Metadata{}
		}
		if r.cluster.Spec.InstanceSets[i].Metadata.Annotations == nil {
			r.cluster.Spec.InstanceSets[i].Metadata.Annotations = make(map[string]string)
		}
		r.cluster.Spec.InstanceSets[i].Metadata.Annotations[naming.DisableWALArchiveRecoveryAnnotation] = "true"
	}
	if err := r.cl.Patch(ctx, r.cluster.DeepCopy(), client.MergeFrom(orig)); err != nil {
		return errors.Wrap(err, "patch cluster")
	}
	return nil
}

func (r *snapshotRestorer) enableWALArchiveRecovery(ctx context.Context) error {
	orig := r.cluster.DeepCopy()
	for i := range r.cluster.Spec.InstanceSets {
		if r.cluster.Spec.InstanceSets[i].Metadata == nil {
			continue
		}
		if r.cluster.Spec.InstanceSets[i].Metadata.Annotations == nil {
			continue
		}
		delete(r.cluster.Spec.InstanceSets[i].Metadata.Annotations, naming.DisableWALArchiveRecoveryAnnotation)
	}
	if err := r.cl.Patch(ctx, r.cluster.DeepCopy(), client.MergeFrom(orig)); err != nil {
		return errors.Wrap(err, "patch cluster")
	}
	return nil
}
