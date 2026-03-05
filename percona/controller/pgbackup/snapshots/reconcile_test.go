package snapshots

import (
	"context"
	"testing"
	"time"

	volumesnapshotv1 "github.com/kubernetes-csi/external-snapshotter/client/v8/apis/volumesnapshot/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/percona/percona-postgresql-operator/v2/internal/logging"
	"github.com/percona/percona-postgresql-operator/v2/internal/naming"
	pNaming "github.com/percona/percona-postgresql-operator/v2/percona/naming"
	v2 "github.com/percona/percona-postgresql-operator/v2/pkg/apis/pgv2.percona.com/v2"
)

func TestShouldFailSnapshot(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name           string
		volumeSnapshot *volumesnapshotv1.VolumeSnapshot
		wantFail       bool
	}{
		{
			name: "Status.Error.Time is zero",
			volumeSnapshot: &volumesnapshotv1.VolumeSnapshot{
				Status: &volumesnapshotv1.VolumeSnapshotStatus{
					Error: &volumesnapshotv1.VolumeSnapshotError{
						Time: ptr.To(metav1.Time{}),
					},
				},
			},
			wantFail: false,
		},
		{
			name: "error within deadline",
			volumeSnapshot: &volumesnapshotv1.VolumeSnapshot{
				Status: &volumesnapshotv1.VolumeSnapshotStatus{
					Error: &volumesnapshotv1.VolumeSnapshotError{
						Time: ptr.To(metav1.NewTime(now.Add(-1 * time.Minute))), // 1mins ago, within deadline
					},
				},
			},
			wantFail: false,
		},
		{
			name: "error past deadline",
			volumeSnapshot: &volumesnapshotv1.VolumeSnapshot{
				Status: &volumesnapshotv1.VolumeSnapshotStatus{
					Error: &volumesnapshotv1.VolumeSnapshotError{
						Time: ptr.To(metav1.NewTime(now.Add(-10 * time.Minute))), // 10 minutes ago (past 5min deadline)
					},
				},
			},
			wantFail: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.wantFail, shouldFailSnapshot(tt.volumeSnapshot))
		})
	}
}

func TestReconcileNew(t *testing.T) {
	ctx := context.Background()
	ns := "test-ns"
	backupName := "my-backup"
	clusterName := "my-cluster"

	s := scheme.Scheme
	require.NoError(t, corev1.AddToScheme(s))
	require.NoError(t, v2.AddToScheme(s))
	require.NoError(t, volumesnapshotv1.AddToScheme(s))

	noopExec := &mockSnapshotExecutor{}
	t.Run("transitions to BackupStarting when cluster is ready", func(t *testing.T) {
		cluster := &v2.PerconaPGCluster{
			ObjectMeta: metav1.ObjectMeta{Name: clusterName, Namespace: ns},
			Status: v2.PerconaPGClusterStatus{
				State: v2.AppStateReady,
			},
		}
		backup := &v2.PerconaPGBackup{
			ObjectMeta: metav1.ObjectMeta{Name: backupName, Namespace: ns, UID: "backup-uid"},
			Spec:       v2.PerconaPGBackupSpec{PGCluster: clusterName},
		}
		cl := fake.NewClientBuilder().
			WithScheme(s).
			WithObjects(backup.DeepCopy(), cluster).
			WithStatusSubresource(backup).
			Build()

		r := newSnapshotReconciler(cl, logging.Discard(), cluster, backup, noopExec)
		result, err := r.reconcileNew(ctx)
		require.NoError(t, err)
		assert.Zero(t, result.RequeueAfter, "should not requeue")

		updated := &v2.PerconaPGBackup{}
		require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(backup), updated))
		assert.Equal(t, v2.BackupStarting, updated.Status.State)
		assert.Equal(t, v2.PGBackupTypeSnapshot, updated.Status.BackupType)
	})
}

func TestReconcileDataSnapshot(t *testing.T) {
	ctx := context.Background()
	ns := "test-ns"
	backupName := "my-backup"
	clusterName := "my-cluster"
	pvcName := "data-pvc"
	snapshotClassName := "test-snapshotclass"

	s := scheme.Scheme
	require.NoError(t, corev1.AddToScheme(s))
	require.NoError(t, v2.AddToScheme(s))
	require.NoError(t, volumesnapshotv1.AddToScheme(s))

	cluster := &v2.PerconaPGCluster{
		ObjectMeta: metav1.ObjectMeta{Name: clusterName, Namespace: ns},
		Spec: v2.PerconaPGClusterSpec{
			Backups: v2.Backups{
				VolumeSnapshots: &v2.VolumeSnapshots{
					Mode:      v2.VolumeSnapshotModeOffline,
					ClassName: snapshotClassName,
				},
			},
		},
	}

	backup := &v2.PerconaPGBackup{
		ObjectMeta: metav1.ObjectMeta{Name: backupName, Namespace: ns, UID: "backup-uid"},
		Spec:       v2.PerconaPGBackupSpec{PGCluster: clusterName},
		Status: v2.PerconaPGBackupStatus{
			Snapshot: &v2.SnapshotStatus{},
		},
	}

	noopExec := &mockSnapshotExecutor{}

	t.Run("creates VolumeSnapshot and updates backup status", func(t *testing.T) {
		cl := fake.NewClientBuilder().
			WithScheme(s).
			WithObjects(backup.DeepCopy(), cluster).
			WithStatusSubresource(backup).
			Build()

		r := newSnapshotReconciler(cl, logging.Discard(), cluster, backup, noopExec)
		ok, err := r.reconcileDataSnapshot(ctx, pvcName)
		require.NoError(t, err)
		assert.False(t, ok, "snapshot not ready yet")

		vsName := backupName + "-" + naming.RolePostgresData
		vs := &volumesnapshotv1.VolumeSnapshot{}
		require.NoError(t, cl.Get(ctx, client.ObjectKey{Namespace: ns, Name: vsName}, vs))
		assert.Equal(t, snapshotClassName, ptr.Deref(vs.Spec.VolumeSnapshotClassName, ""))
		assert.Equal(t, pvcName, ptr.Deref(vs.Spec.Source.PersistentVolumeClaimName, ""))

		updated := &v2.PerconaPGBackup{}
		require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(backup), updated))
		require.NotNil(t, updated.Status.Snapshot)
		assert.Equal(t, vsName, *updated.Status.Snapshot.DataVolumeSnapshotRef)
	})

	t.Run("returns true when existing VolumeSnapshot is ReadyToUse", func(t *testing.T) {
		vsName := backupName + "-" + naming.RolePostgresData
		existingVS := &volumesnapshotv1.VolumeSnapshot{
			ObjectMeta: metav1.ObjectMeta{Name: vsName, Namespace: ns},
			Spec: volumesnapshotv1.VolumeSnapshotSpec{
				VolumeSnapshotClassName: ptr.To(snapshotClassName),
				Source: volumesnapshotv1.VolumeSnapshotSource{
					PersistentVolumeClaimName: ptr.To(pvcName),
				},
			},
			Status: &volumesnapshotv1.VolumeSnapshotStatus{
				ReadyToUse: ptr.To(true),
			},
		}

		cl := fake.NewClientBuilder().
			WithScheme(s).
			WithObjects(backup.DeepCopy(), cluster, existingVS).
			WithStatusSubresource(backup, existingVS).
			Build()

		r := newSnapshotReconciler(cl, logging.Discard(), cluster, backup, noopExec)
		ok, err := r.reconcileDataSnapshot(ctx, pvcName)
		require.NoError(t, err)
		assert.True(t, ok, "snapshot ready")
	})
}

func TestReconcileWALSnapshot(t *testing.T) {
	ctx := context.Background()
	ns := "test-ns"
	backupName := "my-backup"
	clusterName := "my-cluster"
	walPVCName := "wal-pvc"
	snapshotClassName := "test-snapshotclass"

	s := scheme.Scheme
	require.NoError(t, corev1.AddToScheme(s))
	require.NoError(t, v2.AddToScheme(s))
	require.NoError(t, volumesnapshotv1.AddToScheme(s))

	cluster := &v2.PerconaPGCluster{
		ObjectMeta: metav1.ObjectMeta{Name: clusterName, Namespace: ns},
		Spec: v2.PerconaPGClusterSpec{
			Backups: v2.Backups{
				VolumeSnapshots: &v2.VolumeSnapshots{
					Mode:      v2.VolumeSnapshotModeOffline,
					ClassName: snapshotClassName,
				},
			},
		},
	}

	noopExec := &mockSnapshotExecutor{}

	t.Run("returns true when target PVC is empty", func(t *testing.T) {
		backup := &v2.PerconaPGBackup{
			ObjectMeta: metav1.ObjectMeta{Name: backupName, Namespace: ns, UID: "backup-uid"},
			Spec:       v2.PerconaPGBackupSpec{PGCluster: clusterName},
			Status: v2.PerconaPGBackupStatus{
				Snapshot: &v2.SnapshotStatus{},
			},
		}
		cl := fake.NewClientBuilder().
			WithScheme(s).
			WithObjects(backup.DeepCopy(), cluster).
			WithStatusSubresource(backup).
			Build()

		r := newSnapshotReconciler(cl, logging.Discard(), cluster, backup, noopExec)
		ok, err := r.reconcileWALSnapshot(ctx, "")
		require.NoError(t, err)
		assert.True(t, ok, "no WAL volume to snapshot")
	})

	t.Run("creates VolumeSnapshot and updates backup status", func(t *testing.T) {
		backup := &v2.PerconaPGBackup{
			ObjectMeta: metav1.ObjectMeta{Name: backupName, Namespace: ns, UID: "backup-uid"},
			Spec:       v2.PerconaPGBackupSpec{PGCluster: clusterName},
			Status: v2.PerconaPGBackupStatus{
				Snapshot: &v2.SnapshotStatus{},
			},
		}
		cl := fake.NewClientBuilder().
			WithScheme(s).
			WithObjects(backup.DeepCopy(), cluster).
			WithStatusSubresource(backup).
			Build()

		r := newSnapshotReconciler(cl, logging.Discard(), cluster, backup, noopExec)
		ok, err := r.reconcileWALSnapshot(ctx, walPVCName)
		require.NoError(t, err)
		assert.False(t, ok, "snapshot not ready yet")

		vsName := backupName + "-" + naming.RolePostgresWAL
		vs := &volumesnapshotv1.VolumeSnapshot{}
		require.NoError(t, cl.Get(ctx, client.ObjectKey{Namespace: ns, Name: vsName}, vs))
		assert.Equal(t, snapshotClassName, ptr.Deref(vs.Spec.VolumeSnapshotClassName, ""))
		assert.Equal(t, walPVCName, ptr.Deref(vs.Spec.Source.PersistentVolumeClaimName, ""))

		updated := &v2.PerconaPGBackup{}
		require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(backup), updated))
		require.NotNil(t, updated.Status.Snapshot)
		assert.Equal(t, vsName, *updated.Status.Snapshot.WALVolumeSnapshotRef)
	})

	t.Run("returns true when existing VolumeSnapshot is ReadyToUse", func(t *testing.T) {
		vsName := backupName + "-" + naming.RolePostgresWAL
		existingVS := &volumesnapshotv1.VolumeSnapshot{
			ObjectMeta: metav1.ObjectMeta{Name: vsName, Namespace: ns},
			Spec: volumesnapshotv1.VolumeSnapshotSpec{
				VolumeSnapshotClassName: ptr.To(snapshotClassName),
				Source: volumesnapshotv1.VolumeSnapshotSource{
					PersistentVolumeClaimName: ptr.To(walPVCName),
				},
			},
			Status: &volumesnapshotv1.VolumeSnapshotStatus{
				ReadyToUse: ptr.To(true),
			},
		}
		backup := &v2.PerconaPGBackup{
			ObjectMeta: metav1.ObjectMeta{Name: backupName, Namespace: ns, UID: "backup-uid"},
			Spec:       v2.PerconaPGBackupSpec{PGCluster: clusterName},
			Status: v2.PerconaPGBackupStatus{
				Snapshot: &v2.SnapshotStatus{},
			},
		}
		cl := fake.NewClientBuilder().
			WithScheme(s).
			WithObjects(backup.DeepCopy(), cluster, existingVS).
			WithStatusSubresource(backup, existingVS).
			Build()

		r := newSnapshotReconciler(cl, logging.Discard(), cluster, backup, noopExec)
		ok, err := r.reconcileWALSnapshot(ctx, walPVCName)
		require.NoError(t, err)
		assert.True(t, ok, "snapshot ready")
	})
}

func TestReconcileTablespaceSnapshot(t *testing.T) {
	ctx := context.Background()
	ns := "test-ns"
	backupName := "my-backup"
	clusterName := "my-cluster"
	snapshotClassName := "test-snapshotclass"
	ts1Name, ts2Name := "ts1", "ts2"
	ts1PVC, ts2PVC := "pvc-ts1", "pvc-ts2"

	s := scheme.Scheme
	require.NoError(t, corev1.AddToScheme(s))
	require.NoError(t, v2.AddToScheme(s))
	require.NoError(t, volumesnapshotv1.AddToScheme(s))

	cluster := &v2.PerconaPGCluster{
		ObjectMeta: metav1.ObjectMeta{Name: clusterName, Namespace: ns},
		Spec: v2.PerconaPGClusterSpec{
			Backups: v2.Backups{
				VolumeSnapshots: &v2.VolumeSnapshots{
					Mode:      v2.VolumeSnapshotModeOffline,
					ClassName: snapshotClassName,
				},
			},
		},
	}

	noopExec := &mockSnapshotExecutor{}

	t.Run("returns true when TablespaceVolumes is empty", func(t *testing.T) {
		backup := &v2.PerconaPGBackup{
			ObjectMeta: metav1.ObjectMeta{Name: backupName, Namespace: ns, UID: "backup-uid"},
			Spec:       v2.PerconaPGBackupSpec{PGCluster: clusterName},
			Status: v2.PerconaPGBackupStatus{
				Snapshot: &v2.SnapshotStatus{},
			},
		}
		cl := fake.NewClientBuilder().
			WithScheme(s).
			WithObjects(backup.DeepCopy(), cluster).
			WithStatusSubresource(backup).
			Build()

		r := newSnapshotReconciler(cl, logging.Discard(), cluster, backup, noopExec)
		ok, err := r.reconcileTablespaceSnapshot(ctx, nil)
		require.NoError(t, err)
		assert.True(t, ok, "no tablespace volumes to snapshot")
	})

	t.Run("creates VolumeSnapshots and updates backup status", func(t *testing.T) {
		backup := &v2.PerconaPGBackup{
			ObjectMeta: metav1.ObjectMeta{Name: backupName, Namespace: ns, UID: "backup-uid"},
			Spec:       v2.PerconaPGBackupSpec{PGCluster: clusterName},
			Status: v2.PerconaPGBackupStatus{
				Snapshot: &v2.SnapshotStatus{},
			},
		}
		cl := fake.NewClientBuilder().
			WithScheme(s).
			WithObjects(backup.DeepCopy(), cluster).
			WithStatusSubresource(backup).
			Build()

		r := newSnapshotReconciler(cl, logging.Discard(), cluster, backup, noopExec)
		ok, err := r.reconcileTablespaceSnapshot(ctx, map[string]string{
			ts1Name: ts1PVC,
			ts2Name: ts2PVC,
		})
		require.NoError(t, err)
		assert.False(t, ok, "snapshots not ready yet")

		for _, tc := range []struct {
			tsName string
			pvc    string
		}{
			{ts1Name, ts1PVC},
			{ts2Name, ts2PVC},
		} {
			vsName := backupName + "-" + tc.tsName + "-" + naming.RoleTablespace
			vs := &volumesnapshotv1.VolumeSnapshot{}
			require.NoError(t, cl.Get(ctx, client.ObjectKey{Namespace: ns, Name: vsName}, vs))
			assert.Equal(t, snapshotClassName, ptr.Deref(vs.Spec.VolumeSnapshotClassName, ""))
			assert.Equal(t, tc.pvc, ptr.Deref(vs.Spec.Source.PersistentVolumeClaimName, ""))
		}

		updated := &v2.PerconaPGBackup{}
		require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(backup), updated))
		require.NotNil(t, updated.Status.Snapshot)
		assert.Equal(t, backupName+"-"+ts1Name+"-"+naming.RoleTablespace, updated.Status.Snapshot.TablespaceVolumeSnapshotRefs[ts1Name])
		assert.Equal(t, backupName+"-"+ts2Name+"-"+naming.RoleTablespace, updated.Status.Snapshot.TablespaceVolumeSnapshotRefs[ts2Name])
	})

	t.Run("returns true when all existing VolumeSnapshots are ReadyToUse", func(t *testing.T) {
		vs1Name := backupName + "-" + ts1Name + "-" + naming.RoleTablespace
		vs2Name := backupName + "-" + ts2Name + "-" + naming.RoleTablespace
		existingVS1 := &volumesnapshotv1.VolumeSnapshot{
			ObjectMeta: metav1.ObjectMeta{Name: vs1Name, Namespace: ns},
			Spec: volumesnapshotv1.VolumeSnapshotSpec{
				VolumeSnapshotClassName: ptr.To(snapshotClassName),
				Source: volumesnapshotv1.VolumeSnapshotSource{
					PersistentVolumeClaimName: ptr.To(ts1PVC),
				},
			},
			Status: &volumesnapshotv1.VolumeSnapshotStatus{ReadyToUse: ptr.To(true)},
		}
		existingVS2 := &volumesnapshotv1.VolumeSnapshot{
			ObjectMeta: metav1.ObjectMeta{Name: vs2Name, Namespace: ns},
			Spec: volumesnapshotv1.VolumeSnapshotSpec{
				VolumeSnapshotClassName: ptr.To(snapshotClassName),
				Source: volumesnapshotv1.VolumeSnapshotSource{
					PersistentVolumeClaimName: ptr.To(ts2PVC),
				},
			},
			Status: &volumesnapshotv1.VolumeSnapshotStatus{ReadyToUse: ptr.To(true)},
		}
		backup := &v2.PerconaPGBackup{
			ObjectMeta: metav1.ObjectMeta{Name: backupName, Namespace: ns, UID: "backup-uid"},
			Spec:       v2.PerconaPGBackupSpec{PGCluster: clusterName},
			Status: v2.PerconaPGBackupStatus{
				Snapshot: &v2.SnapshotStatus{},
			},
		}
		cl := fake.NewClientBuilder().
			WithScheme(s).
			WithObjects(backup.DeepCopy(), cluster, existingVS1, existingVS2).
			WithStatusSubresource(backup, existingVS1, existingVS2).
			Build()

		r := newSnapshotReconciler(cl, logging.Discard(), cluster, backup, noopExec)
		ok, err := r.reconcileTablespaceSnapshot(ctx, map[string]string{
			ts1Name: ts1PVC,
			ts2Name: ts2PVC,
		})
		require.NoError(t, err)
		assert.True(t, ok, "all tablespace snapshots ready")
	})
}

func TestReconcileRunning(t *testing.T) {
	ctx := context.Background()
	ns := "test-ns"
	backupName := "my-backup"
	clusterName := "my-cluster"
	snapshotClassName := "test-snapshotclass"
	targetInstance := "instance-0"

	s := scheme.Scheme
	require.NoError(t, corev1.AddToScheme(s))
	require.NoError(t, v2.AddToScheme(s))
	require.NoError(t, volumesnapshotv1.AddToScheme(s))

	cluster := &v2.PerconaPGCluster{
		ObjectMeta: metav1.ObjectMeta{Name: clusterName, Namespace: ns},
		Spec: v2.PerconaPGClusterSpec{
			Backups: v2.Backups{
				VolumeSnapshots: &v2.VolumeSnapshots{
					Mode:      v2.VolumeSnapshotModeOffline,
					ClassName: snapshotClassName,
				},
			},
		},
	}

	dataPVC := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "data-pvc",
			Namespace: ns,
			Labels: map[string]string{
				naming.LabelInstance: targetInstance,
				naming.LabelRole:     naming.RolePostgresData,
			},
		},
	}

	walPVC := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "wal-pvc",
			Namespace: ns,
			Labels: map[string]string{
				naming.LabelInstance: targetInstance,
				naming.LabelRole:     naming.RolePostgresWAL,
			},
		},
	}

	noopExec := &mockSnapshotExecutor{}
	t.Run("creates snapshots and requeues when not ready", func(t *testing.T) {
		backup := &v2.PerconaPGBackup{
			ObjectMeta: metav1.ObjectMeta{
				Name:        backupName,
				Namespace:   ns,
				UID:         "backup-uid",
				Annotations: map[string]string{annotationBackupTarget: targetInstance},
			},
			Spec: v2.PerconaPGBackupSpec{PGCluster: clusterName},
			Status: v2.PerconaPGBackupStatus{
				State:    v2.BackupRunning,
				Snapshot: &v2.SnapshotStatus{},
			},
		}

		cl := fake.NewClientBuilder().
			WithScheme(s).
			WithObjects(backup.DeepCopy(), cluster, dataPVC, walPVC).
			WithStatusSubresource(backup).
			Build()

		r := newSnapshotReconciler(cl, logging.Discard(), cluster, backup, noopExec)
		result, err := r.reconcileRunning(ctx)
		require.NoError(t, err)
		assert.Equal(t, time.Second*5, result.RequeueAfter, "should requeue while snapshots are pending")

		dataVSName := backupName + "-" + naming.RolePostgresData
		dataVS := &volumesnapshotv1.VolumeSnapshot{}
		require.NoError(t, cl.Get(ctx, client.ObjectKey{Namespace: ns, Name: dataVSName}, dataVS))
		assert.Equal(t, "data-pvc", ptr.Deref(dataVS.Spec.Source.PersistentVolumeClaimName, ""))

		walVSName := backupName + "-" + naming.RolePostgresWAL
		walVS := &volumesnapshotv1.VolumeSnapshot{}
		require.NoError(t, cl.Get(ctx, client.ObjectKey{Namespace: ns, Name: walVSName}, walVS))
		assert.Equal(t, "wal-pvc", ptr.Deref(walVS.Spec.Source.PersistentVolumeClaimName, ""))
	})

	t.Run("succeeds when all snapshots are ready", func(t *testing.T) {
		backup := &v2.PerconaPGBackup{
			ObjectMeta: metav1.ObjectMeta{
				Name:        backupName,
				Namespace:   ns,
				UID:         "backup-uid",
				Annotations: map[string]string{annotationBackupTarget: targetInstance},
				Finalizers:  []string{pNaming.FinalizerSnapshotInProgress},
			},
			Spec: v2.PerconaPGBackupSpec{PGCluster: clusterName},
			Status: v2.PerconaPGBackupStatus{
				State:    v2.BackupRunning,
				Snapshot: &v2.SnapshotStatus{},
			},
		}

		dataVS := &volumesnapshotv1.VolumeSnapshot{
			ObjectMeta: metav1.ObjectMeta{
				Name:      backupName + "-" + naming.RolePostgresData,
				Namespace: ns,
			},
			Spec: volumesnapshotv1.VolumeSnapshotSpec{
				VolumeSnapshotClassName: ptr.To(snapshotClassName),
				Source: volumesnapshotv1.VolumeSnapshotSource{
					PersistentVolumeClaimName: ptr.To("data-pvc"),
				},
			},
			Status: &volumesnapshotv1.VolumeSnapshotStatus{ReadyToUse: ptr.To(true)},
		}
		walVS := &volumesnapshotv1.VolumeSnapshot{
			ObjectMeta: metav1.ObjectMeta{
				Name:      backupName + "-" + naming.RolePostgresWAL,
				Namespace: ns,
			},
			Spec: volumesnapshotv1.VolumeSnapshotSpec{
				VolumeSnapshotClassName: ptr.To(snapshotClassName),
				Source: volumesnapshotv1.VolumeSnapshotSource{
					PersistentVolumeClaimName: ptr.To("wal-pvc"),
				},
			},
			Status: &volumesnapshotv1.VolumeSnapshotStatus{ReadyToUse: ptr.To(true)},
		}

		cl := fake.NewClientBuilder().
			WithScheme(s).
			WithObjects(backup.DeepCopy(), cluster, dataPVC, walPVC, dataVS, walVS).
			WithStatusSubresource(backup, dataVS, walVS).
			Build()

		r := newSnapshotReconciler(cl, logging.Discard(), cluster, backup, noopExec)
		result, err := r.reconcileRunning(ctx)
		require.NoError(t, err)
		assert.Zero(t, result.RequeueAfter, "should not requeue on success")

		updated := &v2.PerconaPGBackup{}
		require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(backup), updated))
		assert.Equal(t, v2.BackupSucceeded, updated.Status.State)
		assert.NotNil(t, updated.Status.CompletedAt)
		assert.NotContains(t, updated.Finalizers, pNaming.FinalizerSnapshotInProgress,
			"finalizer should be removed after completion")
	})

	t.Run("fails backup when volume snapshot error exceeds deadline", func(t *testing.T) {
		backup := &v2.PerconaPGBackup{
			ObjectMeta: metav1.ObjectMeta{
				Name:        backupName,
				Namespace:   ns,
				UID:         "backup-uid",
				Annotations: map[string]string{annotationBackupTarget: targetInstance},
			},
			Spec: v2.PerconaPGBackupSpec{PGCluster: clusterName},
			Status: v2.PerconaPGBackupStatus{
				State:    v2.BackupRunning,
				Snapshot: &v2.SnapshotStatus{},
			},
		}

		failedDataVS := &volumesnapshotv1.VolumeSnapshot{
			ObjectMeta: metav1.ObjectMeta{
				Name:      backupName + "-" + naming.RolePostgresData,
				Namespace: ns,
			},
			Spec: volumesnapshotv1.VolumeSnapshotSpec{
				VolumeSnapshotClassName: ptr.To(snapshotClassName),
				Source: volumesnapshotv1.VolumeSnapshotSource{
					PersistentVolumeClaimName: ptr.To("data-pvc"),
				},
			},
			Status: &volumesnapshotv1.VolumeSnapshotStatus{
				Error: &volumesnapshotv1.VolumeSnapshotError{
					Time:    ptr.To(metav1.NewTime(time.Now().Add(-10 * time.Minute))),
					Message: ptr.To("disk full"),
				},
			},
		}

		cl := fake.NewClientBuilder().
			WithScheme(s).
			WithObjects(backup.DeepCopy(), cluster, dataPVC, walPVC, failedDataVS).
			WithStatusSubresource(backup, failedDataVS).
			Build()

		r := newSnapshotReconciler(cl, logging.Discard(), cluster, backup, noopExec)
		_, err := r.reconcileRunning(ctx)
		require.Error(t, err)
		assert.ErrorIs(t, err, errVolumeSnapshotFailed)

		updated := &v2.PerconaPGBackup{}
		require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(backup), updated))
		assert.Equal(t, v2.BackupFailed, updated.Status.State)
		assert.Contains(t, updated.Status.Error, "one or more snapshots failed")
	})
}

func TestGenerateSnapshotIntent(t *testing.T) {
	ns := "test-ns"
	backupName := "my-backup"
	clusterName := "my-cluster"
	snapshotClassName := "test-snapshotclass"

	s := scheme.Scheme
	require.NoError(t, corev1.AddToScheme(s))
	require.NoError(t, v2.AddToScheme(s))
	require.NoError(t, volumesnapshotv1.AddToScheme(s))

	cluster := &v2.PerconaPGCluster{
		ObjectMeta: metav1.ObjectMeta{Name: clusterName, Namespace: ns},
		Spec: v2.PerconaPGClusterSpec{
			Backups: v2.Backups{
				VolumeSnapshots: &v2.VolumeSnapshots{
					Mode:      v2.VolumeSnapshotModeOffline,
					ClassName: snapshotClassName,
				},
			},
		},
	}

	backup := &v2.PerconaPGBackup{
		ObjectMeta: metav1.ObjectMeta{Name: backupName, Namespace: ns, UID: "backup-uid"},
		Spec:       v2.PerconaPGBackupSpec{PGCluster: clusterName},
	}

	cl := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(backup, cluster).
		Build()

	r := newSnapshotReconciler(cl, logging.Discard(), cluster, backup, &mockSnapshotExecutor{})

	tests := []struct {
		name         string
		snapshotRole string
		sourcePVC    string
		wantName     string
	}{
		{
			name:         "data volume",
			snapshotRole: naming.RolePostgresData,
			sourcePVC:    "data-pvc",
			wantName:     backupName + "-" + naming.RolePostgresData,
		},
		{
			name:         "WAL volume",
			snapshotRole: naming.RolePostgresWAL,
			sourcePVC:    "wal-pvc",
			wantName:     backupName + "-" + naming.RolePostgresWAL,
		},
		{
			name:         "tablespace volume",
			snapshotRole: "ts1-" + naming.RoleTablespace,
			sourcePVC:    "pvc-ts1",
			wantName:     backupName + "-" + "ts1-" + naming.RoleTablespace,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vs, err := r.generateSnapshotIntent(tt.snapshotRole, tt.sourcePVC)
			require.NoError(t, err)
			require.NotNil(t, vs)

			assert.Equal(t, tt.wantName, vs.Name)
			assert.Equal(t, ns, vs.Namespace)
			assert.Equal(t, snapshotClassName, ptr.Deref(vs.Spec.VolumeSnapshotClassName, ""))
			assert.Equal(t, tt.sourcePVC, ptr.Deref(vs.Spec.Source.PersistentVolumeClaimName, ""))

			// Owner reference should be set to the backup
			require.NotEmpty(t, vs.OwnerReferences, "expected owner reference to be set")
			assert.Equal(t, backupName, vs.OwnerReferences[0].Name)
			assert.Equal(t, "pgv2.percona.com/v2", vs.OwnerReferences[0].APIVersion)
			assert.Equal(t, "PerconaPGBackup", vs.OwnerReferences[0].Kind)
		})
	}
}

// mockSnapshotExecutor is a no-op snapshotExecutor for tests.
type mockSnapshotExecutor struct{}

func (m *mockSnapshotExecutor) prepare(ctx context.Context) (string, error) { return "instance-0", nil }
func (m *mockSnapshotExecutor) finalize(ctx context.Context) error          { return nil }
