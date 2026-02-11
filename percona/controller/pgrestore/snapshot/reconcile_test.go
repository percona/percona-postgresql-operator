package snapshot

import (
	"context"
	"io"
	"path"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	volumesnapshotv1 "github.com/kubernetes-csi/external-snapshotter/client/v8/apis/volumesnapshot/v1"
	"github.com/percona/percona-postgresql-operator/v2/internal/controller/runtime"
	"github.com/percona/percona-postgresql-operator/v2/internal/logging"
	"github.com/percona/percona-postgresql-operator/v2/internal/naming"
	pNaming "github.com/percona/percona-postgresql-operator/v2/percona/naming"
	v2 "github.com/percona/percona-postgresql-operator/v2/pkg/apis/pgv2.percona.com/v2"
	crunchyv1beta1 "github.com/percona/percona-postgresql-operator/v2/pkg/apis/postgres-operator.crunchydata.com/v1beta1"
)

func TestGeneratePrepareJob(t *testing.T) {
	ns := "test-ns"
	clusterName := "my-cluster"
	postgresVersion := 15
	image := "postgres:15"

	cluster := &v2.PerconaPGCluster{
		ObjectMeta: metav1.ObjectMeta{Name: clusterName, Namespace: ns},
		Spec: v2.PerconaPGClusterSpec{
			PostgresVersion: postgresVersion,
			Image:           image,
		},
	}

	makeInstance := func(name string) appsv1.StatefulSet {
		return appsv1.StatefulSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: ns,
			},
		}
	}

	t.Run("single instance without PITR", func(t *testing.T) {
		job := &batchv1.Job{}
		instances := &appsv1.StatefulSetList{
			Items: []appsv1.StatefulSet{makeInstance("my-cluster-instance-0")},
		}
		restore := &v2.PerconaPGRestore{
			ObjectMeta: metav1.ObjectMeta{Name: "my-restore", Namespace: ns},
			Spec: v2.PerconaPGRestoreSpec{
				PGCluster:                clusterName,
				VolumeSnapshotBackupName: "my-backup",
				// RepoName nil and VolumeSnapshotBackupName set = no PITR
			},
		}

		generatePrepareJob(job, instances, cluster, restore)

		require.Len(t, job.Spec.Template.Spec.Containers, 1)
		container := job.Spec.Template.Spec.Containers[0]
		assert.Equal(t, "snapshot-prepare", container.Name)
		assert.Equal(t, image, container.Image)
		assert.Equal(t, []string{"bash", "-c"}, container.Command[:2])

		assert.Equal(t, resource.MustParse("50m"), container.Resources.Requests[corev1.ResourceCPU])
		assert.Equal(t, resource.MustParse("32Mi"), container.Resources.Requests[corev1.ResourceMemory])

		// No PITR: script should touch skip-wal-recovery files
		script := container.Command[2]
		dataDir := path.Join("my-cluster-instance-0", "pgdata", "pg15")
		assert.Contains(t, script, "touch")
		assert.Contains(t, script, path.Join(dataDir, "skip-wal-recovery"))

		// Volume and mount for instance
		require.Len(t, job.Spec.Template.Spec.Volumes, 1)
		assert.Equal(t, "my-cluster-instance-0-pgdata", job.Spec.Template.Spec.Volumes[0].Name)
		assert.Equal(t, "my-cluster-instance-0-pgdata", job.Spec.Template.Spec.Volumes[0].PersistentVolumeClaim.ClaimName)

		require.Len(t, container.VolumeMounts, 1)
		assert.Equal(t, "my-cluster-instance-0-pgdata", container.VolumeMounts[0].Name)
		assert.Equal(t, path.Join("my-cluster-instance-0", "pgdata"), container.VolumeMounts[0].MountPath)

		assert.Equal(t, corev1.RestartPolicyNever, job.Spec.Template.Spec.RestartPolicy)
	})

	t.Run("multiple instances without PITR", func(t *testing.T) {
		job := &batchv1.Job{}
		instances := &appsv1.StatefulSetList{
			Items: []appsv1.StatefulSet{
				makeInstance("my-cluster-instance-0"),
				makeInstance("my-cluster-instance-1"),
			},
		}
		restore := &v2.PerconaPGRestore{
			ObjectMeta: metav1.ObjectMeta{Name: "my-restore", Namespace: ns},
			Spec: v2.PerconaPGRestoreSpec{
				PGCluster:                clusterName,
				VolumeSnapshotBackupName: "my-backup",
			},
		}

		generatePrepareJob(job, instances, cluster, restore)

		container := job.Spec.Template.Spec.Containers[0]
		script := container.Command[2]

		// Both instances should have skip-wal-recovery
		assert.Contains(t, script, path.Join("my-cluster-instance-0", "pgdata", "pg15", "skip-wal-recovery"))
		assert.Contains(t, script, path.Join("my-cluster-instance-1", "pgdata", "pg15", "skip-wal-recovery"))

		require.Len(t, job.Spec.Template.Spec.Volumes, 2)
		assert.Equal(t, []string{"my-cluster-instance-0-pgdata", "my-cluster-instance-1-pgdata"},
			[]string{job.Spec.Template.Spec.Volumes[0].Name, job.Spec.Template.Spec.Volumes[1].Name})
	})

	t.Run("with PITR clears WAL directory", func(t *testing.T) {
		job := &batchv1.Job{}
		instances := &appsv1.StatefulSetList{
			Items: []appsv1.StatefulSet{makeInstance("my-cluster-instance-0")},
		}
		restore := &v2.PerconaPGRestore{
			ObjectMeta: metav1.ObjectMeta{Name: "my-restore", Namespace: ns},
			Spec: v2.PerconaPGRestoreSpec{
				PGCluster:                clusterName,
				RepoName:                 ptr.To("repo1"),
				VolumeSnapshotBackupName: "my-backup",
			},
		}

		generatePrepareJob(job, instances, cluster, restore)

		container := job.Spec.Template.Spec.Containers[0]
		script := container.Command[2]

		// PITR: script should find/delete WAL dir, not touch skip-wal-recovery
		walDir := path.Join("my-cluster-instance-0", "pgdata", "pg15_wal")
		assert.Contains(t, script, "find")
		assert.Contains(t, script, "-mindepth")
		assert.Contains(t, script, "-delete")
		assert.Contains(t, script, walDir)
		assert.NotContains(t, script, "skip-wal-recovery")
	})

	t.Run("script starts with set -e", func(t *testing.T) {
		job := &batchv1.Job{}
		instances := &appsv1.StatefulSetList{
			Items: []appsv1.StatefulSet{makeInstance("instance-0")},
		}
		restore := &v2.PerconaPGRestore{
			Spec: v2.PerconaPGRestoreSpec{
				PGCluster:                clusterName,
				VolumeSnapshotBackupName: "backup",
			},
		}

		generatePrepareJob(job, instances, cluster, restore)

		script := job.Spec.Template.Spec.Containers[0].Command[2]
		assert.True(t, strings.HasPrefix(script, "set -e\n"),
			"script should start with 'set -e' for error handling, got: %q", script[:50])
	})
}

// noopPodExecutor is a PodExecutor that does nothing, for tests that don't need exec.
var noopPodExecutor runtime.PodExecutor = func(
	_ context.Context, _, _, _ string, _ io.Reader, _, _ io.Writer, _ ...string,
) error {
	return nil
}

func TestReconcileDataVolume(t *testing.T) {
	ctx := context.Background()
	ns := "test-ns"
	clusterName := "my-cluster"
	backupName := "my-backup"
	restoreName := "my-restore"
	snapshotName := "my-backup-pgdata"
	instanceSetName := "00"
	instanceName := clusterName + "-" + instanceSetName + "-0"

	s := scheme.Scheme
	require.NoError(t, corev1.AddToScheme(s))
	require.NoError(t, v2.AddToScheme(s))
	require.NoError(t, volumesnapshotv1.AddToScheme(s))

	cluster := &v2.PerconaPGCluster{
		ObjectMeta: metav1.ObjectMeta{Name: clusterName, Namespace: ns},
		Spec: v2.PerconaPGClusterSpec{
			PostgresVersion: 15,
			InstanceSets: v2.PGInstanceSets{
				{
					Name: instanceSetName,
					DataVolumeClaimSpec: corev1.PersistentVolumeClaimSpec{
						AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
						Resources: corev1.VolumeResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceStorage: resource.MustParse("1Gi"),
							},
						},
					},
				},
			},
		},
	}

	backup := &v2.PerconaPGBackup{
		ObjectMeta: metav1.ObjectMeta{Name: backupName, Namespace: ns},
		Spec:       v2.PerconaPGBackupSpec{PGCluster: clusterName},
		Status: v2.PerconaPGBackupStatus{
			Snapshot: &v2.SnapshotStatus{
				DataVolumeSnapshotRef: ptr.To(snapshotName),
			},
		},
	}

	restore := &v2.PerconaPGRestore{
		ObjectMeta: metav1.ObjectMeta{Name: restoreName, Namespace: ns},
		Spec:       v2.PerconaPGRestoreSpec{PGCluster: clusterName, VolumeSnapshotBackupName: backupName},
	}

	instance := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: instanceName, Namespace: ns},
		Spec:       appsv1.StatefulSetSpec{ServiceName: clusterName + "-pods"},
	}
	instance.Labels = map[string]string{
		naming.LabelInstanceSet: instanceSetName,
		naming.LabelInstance:    instanceName,
	}

	t.Run("returns error when DataVolumeSnapshotRef is nil", func(t *testing.T) {
		backupNoSnapshot := backup.DeepCopy()
		backupNoSnapshot.Status.Snapshot = nil

		cl := fake.NewClientBuilder().
			WithScheme(s).
			WithObjects(cluster, backupNoSnapshot, restore).
			WithStatusSubresource(backupNoSnapshot).
			Build()

		r := newSnapshotRestorer(cl, logging.Discard(), cluster, backupNoSnapshot, restore, noopPodExecutor)
		ok, err := r.reconcileDataVolume(ctx, instance)
		require.Error(t, err)
		assert.False(t, ok)
		assert.Contains(t, err.Error(), "data volume snapshot not known")
	})

	t.Run("creates PVC with correct data source when not found", func(t *testing.T) {
		cl := fake.NewClientBuilder().
			WithScheme(s).
			WithObjects(cluster, backup, restore).
			WithStatusSubresource(backup).
			Build()

		r := newSnapshotRestorer(cl, logging.Discard(), cluster, backup, restore, noopPodExecutor)
		ok, err := r.reconcileDataVolume(ctx, instance)
		require.NoError(t, err)
		assert.True(t, ok)

		pvcName := instanceName + "-pgdata"
		pvc := &corev1.PersistentVolumeClaim{}
		require.NoError(t, cl.Get(ctx, client.ObjectKey{Namespace: ns, Name: pvcName}, pvc))

		// Verify data source points to the VolumeSnapshot
		require.NotNil(t, pvc.Spec.DataSource, "PVC should have DataSource")
		assert.Equal(t, snapshotName, pvc.Spec.DataSource.Name)
		assert.Equal(t, ptr.Deref(pvc.Spec.DataSource.APIGroup, ""), volumesnapshotv1.GroupName)
		assert.Equal(t, pNaming.KindVolumeSnapshot, pvc.Spec.DataSource.Kind)

		// Verify spec from instance set
		assert.Equal(t, []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce}, pvc.Spec.AccessModes)
		assert.Equal(t, resource.MustParse("1Gi"), pvc.Spec.Resources.Requests[corev1.ResourceStorage])

		// Verify restore annotation
		assert.Equal(t, restoreName, pvc.GetAnnotations()[pNaming.AnnotationSnapshotRestore])
	})

	t.Run("deletes PVC when restore annotation is not found", func(t *testing.T) {
		pvcName := instanceName + "-pgdata"
		existingPVC := &corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:        pvcName,
				Namespace:   ns,
				Annotations: map[string]string{}, // No AnnotationSnapshotRestore
			},
			Spec: corev1.PersistentVolumeClaimSpec{
				AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
				Resources: corev1.VolumeResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceStorage: resource.MustParse("500Mi"),
					},
				},
			},
		}

		cl := fake.NewClientBuilder().
			WithScheme(s).
			WithObjects(cluster, backup, restore, existingPVC).
			WithStatusSubresource(backup).
			Build()

		r := newSnapshotRestorer(cl, logging.Discard(), cluster, backup, restore, noopPodExecutor)
		ok, err := r.reconcileDataVolume(ctx, instance)
		require.NoError(t, err)
		assert.False(t, ok, "should return false to trigger requeue")

		// PVC should be deleted
		pvc := &corev1.PersistentVolumeClaim{}
		err = cl.Get(ctx, client.ObjectKey{Namespace: ns, Name: pvcName}, pvc)
		require.True(t, k8serrors.IsNotFound(err), "PVC should be deleted, got err: %v", err)
	})

	t.Run("deletes PVC when annotation points to different restore", func(t *testing.T) {
		pvcName := instanceName + "-pgdata"
		existingPVC := &corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      pvcName,
				Namespace: ns,
				Annotations: map[string]string{
					pNaming.AnnotationSnapshotRestore: "other-restore",
				},
			},
			Spec: corev1.PersistentVolumeClaimSpec{
				AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
				Resources: corev1.VolumeResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceStorage: resource.MustParse("2Gi"),
					},
				},
			},
		}

		cl := fake.NewClientBuilder().
			WithScheme(s).
			WithObjects(cluster, backup, restore, existingPVC).
			WithStatusSubresource(backup).
			Build()

		r := newSnapshotRestorer(cl, logging.Discard(), cluster, backup, restore, noopPodExecutor)
		ok, err := r.reconcileDataVolume(ctx, instance)
		require.NoError(t, err)
		assert.False(t, ok)

		// PVC should be deleted so it can be recreated for this restore
		pvc := &corev1.PersistentVolumeClaim{}
		err = cl.Get(ctx, client.ObjectKey{Namespace: ns, Name: pvcName}, pvc)
		require.True(t, k8serrors.IsNotFound(err), "PVC should be deleted, got err: %v", err)
	})

	t.Run("returns true when PVC already has restore annotation", func(t *testing.T) {
		pvcName := instanceName + "-pgdata"
		existingPVC := &corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      pvcName,
				Namespace: ns,
				Annotations: map[string]string{
					pNaming.AnnotationSnapshotRestore: restoreName,
				},
			},
			Spec: corev1.PersistentVolumeClaimSpec{
				AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
				Resources: corev1.VolumeResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceStorage: resource.MustParse("1Gi"),
					},
				},
			},
		}

		cl := fake.NewClientBuilder().
			WithScheme(s).
			WithObjects(cluster, backup, restore, existingPVC).
			WithStatusSubresource(backup).
			Build()

		r := newSnapshotRestorer(cl, logging.Discard(), cluster, backup, restore, noopPodExecutor)
		ok, err := r.reconcileDataVolume(ctx, instance)
		require.NoError(t, err)
		assert.True(t, ok)
	})
}

func TestReconcileWALVolume(t *testing.T) {
	ctx := context.Background()
	ns := "test-ns"
	clusterName := "my-cluster"
	backupName := "my-backup"
	restoreName := "my-restore"
	walSnapshotName := "my-backup-pgwal"
	instanceSetName := "00"
	instanceName := clusterName + "-" + instanceSetName + "-0"

	s := scheme.Scheme
	require.NoError(t, corev1.AddToScheme(s))
	require.NoError(t, v2.AddToScheme(s))
	require.NoError(t, volumesnapshotv1.AddToScheme(s))

	cluster := &v2.PerconaPGCluster{
		ObjectMeta: metav1.ObjectMeta{Name: clusterName, Namespace: ns},
		Spec: v2.PerconaPGClusterSpec{
			PostgresVersion: 15,
			InstanceSets: v2.PGInstanceSets{
				{
					Name: instanceSetName,
					DataVolumeClaimSpec: corev1.PersistentVolumeClaimSpec{
						AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
						Resources: corev1.VolumeResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceStorage: resource.MustParse("1Gi"),
							},
						},
					},
					WALVolumeClaimSpec: &corev1.PersistentVolumeClaimSpec{
						AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
						Resources: corev1.VolumeResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceStorage: resource.MustParse("512Mi"),
							},
						},
					},
				},
			},
		},
	}

	backup := &v2.PerconaPGBackup{
		ObjectMeta: metav1.ObjectMeta{Name: backupName, Namespace: ns},
		Spec:       v2.PerconaPGBackupSpec{PGCluster: clusterName},
		Status: v2.PerconaPGBackupStatus{
			Snapshot: &v2.SnapshotStatus{
				WALVolumeSnapshotRef: ptr.To(walSnapshotName),
			},
		},
	}

	restore := &v2.PerconaPGRestore{
		ObjectMeta: metav1.ObjectMeta{Name: restoreName, Namespace: ns},
		Spec:       v2.PerconaPGRestoreSpec{PGCluster: clusterName, VolumeSnapshotBackupName: backupName},
	}

	instance := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: instanceName, Namespace: ns},
		Spec:       appsv1.StatefulSetSpec{ServiceName: clusterName + "-pods"},
	}
	instance.Labels = map[string]string{
		naming.LabelInstanceSet: instanceSetName,
		naming.LabelInstance:    instanceName,
	}

	t.Run("returns true when WALVolumeSnapshotRef is nil", func(t *testing.T) {
		backupNoWAL := backup.DeepCopy()
		backupNoWAL.Status.Snapshot.WALVolumeSnapshotRef = nil

		cl := fake.NewClientBuilder().
			WithScheme(s).
			WithObjects(cluster, backupNoWAL, restore).
			WithStatusSubresource(backupNoWAL).
			Build()

		r := newSnapshotRestorer(cl, logging.Discard(), cluster, backupNoWAL, restore, noopPodExecutor)
		ok, err := r.reconcileWALVolume(ctx, instance)
		require.NoError(t, err)
		assert.True(t, ok, "no WAL volume to restore")
	})

	t.Run("creates PVC with correct data source when not found", func(t *testing.T) {
		cl := fake.NewClientBuilder().
			WithScheme(s).
			WithObjects(cluster, backup, restore).
			WithStatusSubresource(backup).
			Build()

		r := newSnapshotRestorer(cl, logging.Discard(), cluster, backup, restore, noopPodExecutor)
		ok, err := r.reconcileWALVolume(ctx, instance)
		require.NoError(t, err)
		assert.True(t, ok)

		pvcName := instanceName + "-pgwal"
		pvc := &corev1.PersistentVolumeClaim{}
		require.NoError(t, cl.Get(ctx, client.ObjectKey{Namespace: ns, Name: pvcName}, pvc))

		// Verify data source points to the WAL VolumeSnapshot
		require.NotNil(t, pvc.Spec.DataSource, "PVC should have DataSource")
		assert.Equal(t, walSnapshotName, pvc.Spec.DataSource.Name)
		assert.Equal(t, ptr.Deref(pvc.Spec.DataSource.APIGroup, ""), volumesnapshotv1.GroupName)
		assert.Equal(t, pNaming.KindVolumeSnapshot, pvc.Spec.DataSource.Kind)

		// Verify spec from WALVolumeClaimSpec (512Mi, not data's 1Gi)
		assert.Equal(t, []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce}, pvc.Spec.AccessModes)
		assert.Equal(t, resource.MustParse("512Mi"), pvc.Spec.Resources.Requests[corev1.ResourceStorage])

		// Verify restore annotation
		assert.Equal(t, restoreName, pvc.GetAnnotations()[pNaming.AnnotationSnapshotRestore])
	})

	t.Run("deletes PVC when restore annotation is not found", func(t *testing.T) {
		pvcName := instanceName + "-pgwal"
		existingPVC := &corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:        pvcName,
				Namespace:   ns,
				Annotations: map[string]string{},
			},
			Spec: corev1.PersistentVolumeClaimSpec{
				AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
				Resources: corev1.VolumeResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceStorage: resource.MustParse("256Mi"),
					},
				},
			},
		}

		cl := fake.NewClientBuilder().
			WithScheme(s).
			WithObjects(cluster, backup, restore, existingPVC).
			WithStatusSubresource(backup).
			Build()

		r := newSnapshotRestorer(cl, logging.Discard(), cluster, backup, restore, noopPodExecutor)
		ok, err := r.reconcileWALVolume(ctx, instance)
		require.NoError(t, err)
		assert.False(t, ok)

		pvc := &corev1.PersistentVolumeClaim{}
		err = cl.Get(ctx, client.ObjectKey{Namespace: ns, Name: pvcName}, pvc)
		require.True(t, k8serrors.IsNotFound(err), "PVC should be deleted, got err: %v", err)
	})

	t.Run("deletes PVC when annotation points to different restore", func(t *testing.T) {
		pvcName := instanceName + "-pgwal"
		existingPVC := &corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      pvcName,
				Namespace: ns,
				Annotations: map[string]string{
					pNaming.AnnotationSnapshotRestore: "other-restore",
				},
			},
			Spec: corev1.PersistentVolumeClaimSpec{
				AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
				Resources: corev1.VolumeResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceStorage: resource.MustParse("256Mi"),
					},
				},
			},
		}

		cl := fake.NewClientBuilder().
			WithScheme(s).
			WithObjects(cluster, backup, restore, existingPVC).
			WithStatusSubresource(backup).
			Build()

		r := newSnapshotRestorer(cl, logging.Discard(), cluster, backup, restore, noopPodExecutor)
		ok, err := r.reconcileWALVolume(ctx, instance)
		require.NoError(t, err)
		assert.False(t, ok)

		pvc := &corev1.PersistentVolumeClaim{}
		err = cl.Get(ctx, client.ObjectKey{Namespace: ns, Name: pvcName}, pvc)
		require.True(t, k8serrors.IsNotFound(err), "PVC should be deleted, got err: %v", err)
	})

	t.Run("returns true when PVC already has restore annotation", func(t *testing.T) {
		pvcName := instanceName + "-pgwal"
		existingPVC := &corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      pvcName,
				Namespace: ns,
				Annotations: map[string]string{
					pNaming.AnnotationSnapshotRestore: restoreName,
				},
			},
			Spec: corev1.PersistentVolumeClaimSpec{
				AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
				Resources: corev1.VolumeResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceStorage: resource.MustParse("512Mi"),
					},
				},
			},
		}

		cl := fake.NewClientBuilder().
			WithScheme(s).
			WithObjects(cluster, backup, restore, existingPVC).
			WithStatusSubresource(backup).
			Build()

		r := newSnapshotRestorer(cl, logging.Discard(), cluster, backup, restore, noopPodExecutor)
		ok, err := r.reconcileWALVolume(ctx, instance)
		require.NoError(t, err)
		assert.True(t, ok)
	})
}

func TestReconcileTablespaceVolumes(t *testing.T) {
	ctx := context.Background()
	ns := "test-ns"
	clusterName := "my-cluster"
	backupName := "my-backup"
	restoreName := "my-restore"
	ts1Name := "ts1"
	ts1SnapshotName := "my-backup-ts1-tablespace"
	instanceSetName := "00"
	instanceName := clusterName + "-" + instanceSetName + "-0"

	s := scheme.Scheme
	require.NoError(t, corev1.AddToScheme(s))
	require.NoError(t, v2.AddToScheme(s))
	require.NoError(t, volumesnapshotv1.AddToScheme(s))

	cluster := &v2.PerconaPGCluster{
		ObjectMeta: metav1.ObjectMeta{Name: clusterName, Namespace: ns},
		Spec: v2.PerconaPGClusterSpec{
			PostgresVersion: 15,
			InstanceSets: v2.PGInstanceSets{
				{
					Name: instanceSetName,
					DataVolumeClaimSpec: corev1.PersistentVolumeClaimSpec{
						AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
						Resources: corev1.VolumeResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceStorage: resource.MustParse("1Gi"),
							},
						},
					},
					TablespaceVolumes: []crunchyv1beta1.TablespaceVolume{
						{
							Name: ts1Name,
							DataVolumeClaimSpec: corev1.PersistentVolumeClaimSpec{
								AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
								Resources: corev1.VolumeResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceStorage: resource.MustParse("2Gi"),
									},
								},
							},
						},
					},
				},
			},
		},
	}

	backup := &v2.PerconaPGBackup{
		ObjectMeta: metav1.ObjectMeta{Name: backupName, Namespace: ns},
		Spec:       v2.PerconaPGBackupSpec{PGCluster: clusterName},
		Status: v2.PerconaPGBackupStatus{
			Snapshot: &v2.SnapshotStatus{
				TablespaceVolumeSnapshotRefs: map[string]string{ts1Name: ts1SnapshotName},
			},
		},
	}

	restore := &v2.PerconaPGRestore{
		ObjectMeta: metav1.ObjectMeta{Name: restoreName, Namespace: ns},
		Spec:       v2.PerconaPGRestoreSpec{PGCluster: clusterName, VolumeSnapshotBackupName: backupName},
	}

	instance := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: instanceName, Namespace: ns},
		Spec:       appsv1.StatefulSetSpec{ServiceName: clusterName + "-pods"},
	}
	instance.Labels = map[string]string{
		naming.LabelInstanceSet: instanceSetName,
		naming.LabelInstance:    instanceName,
	}

	t.Run("returns true when Snapshot is nil", func(t *testing.T) {
		backupNoSnapshot := backup.DeepCopy()
		backupNoSnapshot.Status.Snapshot = nil

		cl := fake.NewClientBuilder().
			WithScheme(s).
			WithObjects(cluster, backupNoSnapshot, restore).
			WithStatusSubresource(backupNoSnapshot).
			Build()

		r := newSnapshotRestorer(cl, logging.Discard(), cluster, backupNoSnapshot, restore, noopPodExecutor)
		ok, err := r.reconcileTablespaceVolumes(ctx, instance)
		require.NoError(t, err)
		assert.True(t, ok, "no tablespace volumes to restore")
	})

	t.Run("returns true when TablespaceVolumeSnapshotRefs is empty", func(t *testing.T) {
		backupEmpty := backup.DeepCopy()
		backupEmpty.Status.Snapshot = &v2.SnapshotStatus{TablespaceVolumeSnapshotRefs: map[string]string{}}

		cl := fake.NewClientBuilder().
			WithScheme(s).
			WithObjects(cluster, backupEmpty, restore).
			WithStatusSubresource(backupEmpty).
			Build()

		r := newSnapshotRestorer(cl, logging.Discard(), cluster, backupEmpty, restore, noopPodExecutor)
		ok, err := r.reconcileTablespaceVolumes(ctx, instance)
		require.NoError(t, err)
		assert.True(t, ok, "no tablespace volumes to restore")
	})

	t.Run("creates PVC with correct data source when not found", func(t *testing.T) {
		cl := fake.NewClientBuilder().
			WithScheme(s).
			WithObjects(cluster, backup, restore).
			WithStatusSubresource(backup).
			Build()

		r := newSnapshotRestorer(cl, logging.Discard(), cluster, backup, restore, noopPodExecutor)
		ok, err := r.reconcileTablespaceVolumes(ctx, instance)
		require.NoError(t, err)
		assert.True(t, ok)

		pvcName := instanceName + "-" + ts1Name + "-tablespace"
		pvc := &corev1.PersistentVolumeClaim{}
		require.NoError(t, cl.Get(ctx, client.ObjectKey{Namespace: ns, Name: pvcName}, pvc))

		// Verify data source points to the tablespace VolumeSnapshot
		require.NotNil(t, pvc.Spec.DataSource, "PVC should have DataSource")
		assert.Equal(t, ts1SnapshotName, pvc.Spec.DataSource.Name)
		assert.Equal(t, ptr.Deref(pvc.Spec.DataSource.APIGroup, ""), volumesnapshotv1.GroupName)
		assert.Equal(t, pNaming.KindVolumeSnapshot, pvc.Spec.DataSource.Kind)

		// Verify spec from TablespaceVolumes[ts1].DataVolumeClaimSpec (2Gi, not data's 1Gi)
		assert.Equal(t, []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce}, pvc.Spec.AccessModes)
		assert.Equal(t, resource.MustParse("2Gi"), pvc.Spec.Resources.Requests[corev1.ResourceStorage])

		// Verify restore annotation
		assert.Equal(t, restoreName, pvc.GetAnnotations()[pNaming.AnnotationSnapshotRestore])
	})

	t.Run("deletes PVC when restore annotation is not found", func(t *testing.T) {
		pvcName := instanceName + "-" + ts1Name + "-tablespace"
		existingPVC := &corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:        pvcName,
				Namespace:   ns,
				Annotations: map[string]string{},
			},
			Spec: corev1.PersistentVolumeClaimSpec{
				AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
				Resources: corev1.VolumeResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceStorage: resource.MustParse("2Gi"),
					},
				},
			},
		}

		cl := fake.NewClientBuilder().
			WithScheme(s).
			WithObjects(cluster, backup, restore, existingPVC).
			WithStatusSubresource(backup).
			Build()

		r := newSnapshotRestorer(cl, logging.Discard(), cluster, backup, restore, noopPodExecutor)
		ok, err := r.reconcileTablespaceVolumes(ctx, instance)
		require.NoError(t, err)
		assert.False(t, ok)

		pvc := &corev1.PersistentVolumeClaim{}
		err = cl.Get(ctx, client.ObjectKey{Namespace: ns, Name: pvcName}, pvc)
		require.True(t, k8serrors.IsNotFound(err), "PVC should be deleted, got err: %v", err)
	})

	t.Run("deletes PVC when annotation points to different restore", func(t *testing.T) {
		pvcName := instanceName + "-" + ts1Name + "-tablespace"
		existingPVC := &corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      pvcName,
				Namespace: ns,
				Annotations: map[string]string{
					pNaming.AnnotationSnapshotRestore: "other-restore",
				},
			},
			Spec: corev1.PersistentVolumeClaimSpec{
				AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
				Resources: corev1.VolumeResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceStorage: resource.MustParse("2Gi"),
					},
				},
			},
		}

		cl := fake.NewClientBuilder().
			WithScheme(s).
			WithObjects(cluster, backup, restore, existingPVC).
			WithStatusSubresource(backup).
			Build()

		r := newSnapshotRestorer(cl, logging.Discard(), cluster, backup, restore, noopPodExecutor)
		ok, err := r.reconcileTablespaceVolumes(ctx, instance)
		require.NoError(t, err)
		assert.False(t, ok)

		pvc := &corev1.PersistentVolumeClaim{}
		err = cl.Get(ctx, client.ObjectKey{Namespace: ns, Name: pvcName}, pvc)
		require.True(t, k8serrors.IsNotFound(err), "PVC should be deleted, got err: %v", err)
	})

	t.Run("returns true when all tablespace PVCs have correct restore annotation", func(t *testing.T) {
		pvcName := instanceName + "-" + ts1Name + "-tablespace"
		existingPVC := &corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      pvcName,
				Namespace: ns,
				Annotations: map[string]string{
					pNaming.AnnotationSnapshotRestore: restoreName,
				},
			},
			Spec: corev1.PersistentVolumeClaimSpec{
				AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
				Resources: corev1.VolumeResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceStorage: resource.MustParse("2Gi"),
					},
				},
			},
		}

		cl := fake.NewClientBuilder().
			WithScheme(s).
			WithObjects(cluster, backup, restore, existingPVC).
			WithStatusSubresource(backup).
			Build()

		r := newSnapshotRestorer(cl, logging.Discard(), cluster, backup, restore, noopPodExecutor)
		ok, err := r.reconcileTablespaceVolumes(ctx, instance)
		require.NoError(t, err)
		assert.True(t, ok)
	})
}
