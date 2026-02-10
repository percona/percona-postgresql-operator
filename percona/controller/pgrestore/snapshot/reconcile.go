package snapshot

import (
	"context"
	"fmt"
	"io"
	"path"
	"strings"
	"time"

	volumesnapshotv1 "github.com/kubernetes-csi/external-snapshotter/client/v8/apis/volumesnapshot/v1"
	"github.com/pkg/errors"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/percona/percona-postgresql-operator/v2/internal/controller/runtime"
	"github.com/percona/percona-postgresql-operator/v2/internal/feature"
	"github.com/percona/percona-postgresql-operator/v2/internal/logging"
	"github.com/percona/percona-postgresql-operator/v2/internal/naming"
	"github.com/percona/percona-postgresql-operator/v2/percona/controller"
	restoreutils "github.com/percona/percona-postgresql-operator/v2/percona/controller/pgrestore/utils"
	pNaming "github.com/percona/percona-postgresql-operator/v2/percona/naming"
	perconaPG "github.com/percona/percona-postgresql-operator/v2/percona/postgres"
	v2 "github.com/percona/percona-postgresql-operator/v2/pkg/apis/pgv2.percona.com/v2"
	crunchyv1beta1 "github.com/percona/percona-postgresql-operator/v2/pkg/apis/postgres-operator.crunchydata.com/v1beta1"
)

type snapshotRestorer struct {
	cl      client.Client
	log     logging.Logger
	cluster *v2.PerconaPGCluster
	backup  *v2.PerconaPGBackup
	restore *v2.PerconaPGRestore
	podExec runtime.PodExecutor
}

func newSnapshotRestorer(
	cl client.Client,
	log logging.Logger,
	cluster *v2.PerconaPGCluster,
	backup *v2.PerconaPGBackup,
	restore *v2.PerconaPGRestore,
	exec runtime.PodExecutor,
) *snapshotRestorer {
	return &snapshotRestorer{
		cl:      cl,
		log:     log,
		cluster: cluster,
		backup:  backup,
		restore: restore,
		podExec: exec,
	}
}

func Reconcile(
	ctx context.Context,
	c client.Client,
	exec runtime.PodExecutor,
	pg *v2.PerconaPGCluster,
	restore *v2.PerconaPGRestore,
) (reconcile.Result, error) {
	log := logging.FromContext(ctx).WithName("SnapshotRestorer")

	if !feature.Enabled(ctx, feature.BackupSnapshots) {
		log.Info(fmt.Sprintf("Feature gate '%s' is not enabled, skipping snapshot restore", feature.BackupSnapshots))
		return reconcile.Result{}, nil
	}

	backup := &v2.PerconaPGBackup{}
	if err := c.Get(ctx, types.NamespacedName{Name: restore.Spec.VolumeSnapshotBackupName, Namespace: pg.Namespace}, backup); err != nil {
		return reconcile.Result{}, errors.Wrap(err, "get backup")
	}

	r := newSnapshotRestorer(c, log, pg, backup, restore, exec)

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
	if ok, err := r.suspendAllInstances(ctx); err != nil {
		return reconcile.Result{}, errors.Wrap(err, "shutdown cluster")
	} else if !ok {
		r.log.Info("Waiting for instances to be suspended")
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
	instances := &appsv1.StatefulSetList{}
	if err := r.cl.List(ctx, instances, &client.ListOptions{
		Namespace: r.cluster.GetNamespace(),
		LabelSelector: labels.SelectorFromSet(map[string]string{
			naming.LabelCluster: r.cluster.Name,
			naming.LabelData:    naming.DataPostgres,
		}),
	}); err != nil {
		return reconcile.Result{}, errors.Wrap(err, "list instances")
	}

	if ok, err := r.reconcileInstances(ctx, instances); err != nil {
		return reconcile.Result{}, errors.Wrap(err, "reconcile instances")
	} else if !ok {
		r.log.Info("Waiting for instances PVCs to be reconciled")
		return reconcile.Result{RequeueAfter: time.Second * 5}, nil
	}

	// Prepare PVCs
	if ok, err := r.runPrepareJob(ctx, instances); err != nil {
		return reconcile.Result{}, errors.Wrap(err, "run prepare job")
	} else if !ok {
		r.log.Info("Preparing PVCs")
		return reconcile.Result{RequeueAfter: time.Second * 5}, nil
	}
	if err := r.reconcilePrepareJobAnnotation(ctx); err != nil {
		return reconcile.Result{}, errors.Wrap(err, "reconcile prepare job annotation")
	}

	// Run PITR if needed
	if ok, err := r.restorePITR(ctx); err != nil {
		return reconcile.Result{}, errors.Wrap(err, "restore PITR")
	} else if !ok {
		r.log.Info("Waiting for PITR to complete")
		return reconcile.Result{RequeueAfter: time.Second * 5}, nil
	}

	// Recreate DCS so that cluster can be bootstrapped with new data.
	if err := r.reconcileLeaderEndpoints(ctx); err != nil {
		return reconcile.Result{}, errors.Wrap(err, "reconcile leader endpoints")
	}

	if ok, err := r.unsuspendAllInstances(ctx); err != nil {
		return reconcile.Result{}, errors.Wrap(err, "resume cluster")
	} else if !ok && !r.isPITRInProgress() {
		r.log.Info("Waiting for instances to be unsuspended")
		return reconcile.Result{RequeueAfter: time.Second * 5}, nil
	}

	if err := r.restore.UpdateStatus(ctx, r.cl, func(restore *v2.PerconaPGRestore) {
		restore.Status.State = v2.RestoreSucceeded
		restore.Status.CompletedAt = &metav1.Time{Time: time.Now()}
	}); err != nil {
		return reconcile.Result{}, errors.Wrap(err, "update restore status")
	}

	r.log.Info("Snapshot restore is complete")
	return reconcile.Result{}, nil
}

func (r *snapshotRestorer) reconcileInstances(ctx context.Context, instances *appsv1.StatefulSetList) (bool, error) {
	done := true
	for _, instance := range instances.Items {
		if ok, err := r.reconcileInstance(ctx, &instance); err != nil {
			return false, errors.Wrap(err, "reconcile instance")
		} else if !ok {
			done = false
		}
	}
	return done, nil
}

func (r *snapshotRestorer) reconcileInstance(ctx context.Context, instance *appsv1.StatefulSet) (bool, error) {
	dataOk, err := r.reconcileDataVolume(ctx, instance)
	if err != nil {
		return false, errors.Wrap(err, "reconcile data volume")
	}

	walOk, err := r.reconcileWALVolume(ctx, instance)
	if err != nil {
		return false, errors.Wrap(err, "reconcile WAL volume")
	}

	tablespaceOk, err := r.reconcileTablespaceVolumes(ctx, instance)
	if err != nil {
		return false, errors.Wrap(err, "reconcile tablespace volumes")
	}

	return dataOk && walOk && tablespaceOk, nil
}

func (r *snapshotRestorer) reconcileDataVolume(
	ctx context.Context,
	instance *appsv1.StatefulSet,
) (bool, error) {
	if r.backup.Status.Snapshot == nil || r.backup.Status.Snapshot.DataVolume == nil || r.backup.Status.Snapshot.DataVolume.SnapshotName == "" {
		return false, errors.New("data volume snapshot not known")
	}

	pvc := &corev1.PersistentVolumeClaim{ObjectMeta: naming.InstancePostgresDataVolume(instance)}
	snapshotName := r.backup.Status.Snapshot.DataVolume.SnapshotName
	return r.reconcileInstancePVC(ctx, pvc, instance, snapshotName)
}

func (r *snapshotRestorer) reconcileWALVolume(
	ctx context.Context,
	instance *appsv1.StatefulSet,
) (bool, error) {
	if r.backup.Status.Snapshot == nil || r.backup.Status.Snapshot.WALVolume == nil || r.backup.Status.Snapshot.WALVolume.SnapshotName == "" {
		return true, nil
	}

	pvc := &corev1.PersistentVolumeClaim{ObjectMeta: naming.InstancePostgresWALVolume(instance)}
	snapshotName := r.backup.Status.Snapshot.WALVolume.SnapshotName
	return r.reconcileInstancePVC(ctx, pvc, instance, snapshotName)
}

func (r *snapshotRestorer) reconcileTablespaceVolumes(ctx context.Context, instance *appsv1.StatefulSet) (bool, error) {
	if r.backup.Status.Snapshot == nil || r.backup.Status.Snapshot.TablespaceVolumes == nil || len(r.backup.Status.Snapshot.TablespaceVolumes) == 0 {
		return true, nil
	}

	done := true
	for tsName, info := range r.backup.Status.Snapshot.TablespaceVolumes {
		pvc := &corev1.PersistentVolumeClaim{ObjectMeta: naming.InstanceTablespaceDataVolume(instance, tsName)}
		snapshotName := info.SnapshotName
		ok, err := r.reconcileInstancePVC(ctx, pvc, instance, snapshotName)
		if err != nil {
			return false, errors.Wrap(err, "reconcile tablespace PVC")
		}
		if !ok {
			done = false
		}
	}
	return done, nil
}

func (r *snapshotRestorer) reconcileInstancePVC(
	ctx context.Context,
	pvc *corev1.PersistentVolumeClaim,
	instance *appsv1.StatefulSet,
	snapshotName string,
) (bool, error) {
	observedPVC := &corev1.PersistentVolumeClaim{}
	err := r.cl.Get(ctx, client.ObjectKeyFromObject(pvc), observedPVC)
	if k8serrors.IsNotFound(err) {
		if err := r.createPVCFromSnapshot(ctx, pvc, instance, snapshotName); err != nil {
			return false, errors.Wrap(err, "create PVC from data source")
		}
		return true, nil
	} else if err != nil {
		return false, errors.Wrap(err, "get observed PVC")
	}

	if observedPVC.GetAnnotations()[pNaming.AnnotationSnapshotRestore] == r.restore.GetName() {
		return true, nil
	}

	if !observedPVC.GetDeletionTimestamp().IsZero() {
		return false, nil
	}

	// Delete it so we can recreate
	if err := r.cl.Delete(ctx, observedPVC); err != nil {
		return false, errors.Wrap(err, "delete PVC")
	}
	return false, nil
}

func (r *snapshotRestorer) createPVCFromSnapshot(
	ctx context.Context,
	pvc *corev1.PersistentVolumeClaim,
	instance *appsv1.StatefulSet,
	snapshotName string,
) error {
	instanceSetName := instance.GetLabels()[naming.LabelInstanceSet]
	if instanceSetName == "" {
		return errors.New("instance set name is not known")
	}

	dataSource := &corev1.TypedLocalObjectReference{
		APIGroup: ptr.To(volumesnapshotv1.GroupName),
		Kind:     pNaming.KindVolumeSnapshot,
		Name:     snapshotName,
	}
	spec, err := r.pvcSpecFromDataSource(instanceSetName, dataSource)
	if err != nil {
		return errors.Wrap(err, "get PVC spec from data source")
	}
	pvc.Spec = spec
	pvc.SetAnnotations(map[string]string{
		pNaming.AnnotationSnapshotRestore: r.restore.GetName(),
	})
	if err := r.cl.Create(ctx, pvc); err != nil {
		return errors.Wrap(err, "create PVC")
	}
	return nil
}

func (r *snapshotRestorer) pvcSpecFromDataSource(instanceSetName string, dataSource *corev1.TypedLocalObjectReference) (corev1.PersistentVolumeClaimSpec, error) {
	var instanceSetSpec *v2.PGInstanceSetSpec
	for _, instanceSet := range r.cluster.Spec.InstanceSets {
		if instanceSet.Name == instanceSetName {
			instanceSetSpec = &instanceSet
			break
		}
	}
	if instanceSetSpec == nil {
		return corev1.PersistentVolumeClaimSpec{}, errors.New("instance set not found")
	}

	dataVolSpec := instanceSetSpec.DataVolumeClaimSpec
	dataVolSpec.DataSource = dataSource
	return dataVolSpec, nil
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
		if done, err := r.unsuspendAllInstances(ctx); err != nil {
			return errors.Wrap(err, "resume cluster")
		} else if !done {
			return controller.ErrFinalizerPending
		}

		if err := r.cleanupSkipRecoveryFile(ctx); err != nil {
			return errors.Wrap(err, "cleanup")
		}
		return nil
	}
}

func (r *snapshotRestorer) restorePITR(ctx context.Context) (bool, error) {
	if r.restore.Spec.RepoName == nil {
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
	_, ok := r.cluster.GetAnnotations()[naming.PGBackRestRestore]
	return ok
}

func (r *snapshotRestorer) reconcilePrepareJobAnnotation(ctx context.Context) error {
	if _, ok := r.restore.GetAnnotations()[pNaming.AnnotationPVCsPreparedAt]; ok {
		return nil
	}

	orig := r.restore.DeepCopy()
	annotations := r.restore.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string)
	}
	annotations[pNaming.AnnotationPVCsPreparedAt] = time.Now().Format(time.RFC3339)
	r.restore.SetAnnotations(annotations)
	if err := r.cl.Patch(ctx, r.restore.DeepCopy(), client.MergeFrom(orig)); err != nil {
		return errors.Wrap(err, "patch restore annotations")
	}
	return nil
}

// prepares PVCs before starting the cluster.
func (r *snapshotRestorer) runPrepareJob(ctx context.Context, instances *appsv1.StatefulSetList) (bool, error) {
	jobName := r.restore.GetName() + "-prepare"
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName,
			Namespace: r.cluster.GetNamespace(),
		},
	}

	// PVC already prepared, delete and return.
	if _, ok := r.restore.GetAnnotations()[pNaming.AnnotationPVCsPreparedAt]; ok {
		return true, client.IgnoreNotFound(r.cl.Delete(ctx, job,
			client.PropagationPolicy(metav1.DeletePropagationForeground)))
	}

	err := r.cl.Get(ctx, client.ObjectKeyFromObject(job), job)
	if k8serrors.IsNotFound(err) {
		generatePrepareJob(job, instances, r.cluster, r.restore)
		if err := controllerutil.SetControllerReference(r.restore, job, r.cl.Scheme()); err != nil {
			return false, errors.Wrap(err, "set controller reference")
		}
		if err := r.cl.Create(ctx, job); err != nil {
			return false, errors.Wrap(err, "create prepare job")
		}
		return false, nil
	} else if err != nil {
		return false, errors.Wrap(err, "get prepare job")
	}

	if !job.Status.CompletionTime.IsZero() && job.Status.Succeeded > 0 {
		return true, nil
	}

	if job.Status.Failed > 0 {
		if err := r.restore.UpdateStatus(ctx, r.cl, func(restore *v2.PerconaPGRestore) {
			restore.Status.State = v2.RestoreFailed
		}); err != nil {
			return false, errors.Wrap(err, "update restore status")
		}
		return true, nil
	}
	return false, nil
}

func generatePrepareJob(
	job *batchv1.Job,
	instances *appsv1.StatefulSetList,
	cluster *v2.PerconaPGCluster,
	restore *v2.PerconaPGRestore,
) {
	volumes := []corev1.Volume{}
	volumeMounts := []corev1.VolumeMount{}

	for _, instance := range instances.Items {
		volName := instance.GetName() + "-pgdata"
		volumes = append(volumes, corev1.Volume{
			Name: volName,
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: naming.InstancePostgresDataVolume(&instance).Name,
				},
			},
		})
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      volName,
			MountPath: path.Join(instance.GetName(), "pgdata"),
		})
	}

	scriptParts := []string{"set -e"}
	for _, mount := range volumeMounts {
		if restore.Spec.RepoName == nil || restore.Spec.VolumeSnapshotBackupName == "" { // no PITR
			// PVCs are not needed, signal the restore_command to skip WAL recovery in order
			// to maintain consistency with the snapshot data.
			dataDir := path.Join(mount.MountPath, fmt.Sprintf("pg%d", cluster.Spec.PostgresVersion))
			signalFile := path.Join(dataDir, "skip-wal-recovery")
			scriptParts = append(scriptParts, fmt.Sprintf("touch %q", signalFile))
		} else {
			//  PITR is needed, clear local WAL files since they may belong to a different timeline.
			// PITR restore job will fetch the required WAL files from the repo.
			walDir := path.Join(mount.MountPath, fmt.Sprintf("pg%d_wal", cluster.Spec.PostgresVersion))
			scriptParts = append(scriptParts, fmt.Sprintf("find %q -mindepth 1 -delete", walDir))
		}
	}
	script := strings.Join(scriptParts, "\n")

	container := corev1.Container{
		Name:  "snapshot-prepare",
		Image: cluster.Spec.Image,
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("100m"),
				corev1.ResourceMemory: resource.MustParse("100Mi"),
			},
		},
		VolumeMounts: volumeMounts,
		Command:      []string{"bash", "-c", script},
	}
	job.Spec = batchv1.JobSpec{
		Template: corev1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					naming.DefaultContainerAnnotation: "prepare",
				},
			},
			Spec: corev1.PodSpec{
				Containers:    []corev1.Container{container},
				Volumes:       volumes,
				RestartPolicy: corev1.RestartPolicyNever,
			},
		},
	}

}

// We create a $PGDATA/skip-wal-recovery file during the snapshot restore when no PITR is specified.
// This method will cleanup this file after the restore is completed.
func (r *snapshotRestorer) cleanupSkipRecoveryFile(ctx context.Context) error {
	if r.restore.Spec.RepoName != nil {
		return nil
	}

	pods := &corev1.PodList{}
	if err := r.cl.List(ctx, pods, &client.ListOptions{
		Namespace: r.cluster.GetNamespace(),
		LabelSelector: labels.SelectorFromSet(map[string]string{
			naming.LabelCluster: r.cluster.Name,
			naming.LabelData:    naming.DataPostgres,
		}),
	}); err != nil {
		return errors.Wrap(err, "list pods")
	}

	rmScript := `rm -f "${PGDATA}/skip-wal-recovery"`
	for _, pod := range pods.Items {
		if err := r.podExec(ctx, r.cluster.GetNamespace(), pod.GetName(), naming.ContainerDatabase, nil, io.Discard, nil, "sh", "-c", rmScript); err != nil {
			return err
		}
	}

	return nil
}
