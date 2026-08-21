package pgbackup

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	batchv1 "k8s.io/api/batch/v1"
	coordinationv1 "k8s.io/api/coordination/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/percona/percona-postgresql-operator/v2/internal/feature"
	"github.com/percona/percona-postgresql-operator/v2/internal/naming"
	pNaming "github.com/percona/percona-postgresql-operator/v2/percona/naming"
	"github.com/percona/percona-postgresql-operator/v2/percona/pgbackrest"
	v2 "github.com/percona/percona-postgresql-operator/v2/pkg/apis/pgv2.percona.com/v2"
	"github.com/percona/percona-postgresql-operator/v2/pkg/apis/upstream.pgv2.percona.com/v1beta1"
)

func TestReconcileFailsBackupWhenClusterIsUnavailable(t *testing.T) {
	gate := feature.NewGate()
	require.NoError(t, gate.SetFromMap(map[string]bool{feature.BackupSnapshots: true}))
	ctx := feature.NewContext(t.Context(), gate)

	tests := []struct {
		name                   string
		deleteBackupsFinalizer bool
		snapshot               bool
		expectedError          string
	}{
		{
			name:          "cluster without delete-backups finalizer is deleted",
			expectedError: "PerconaPGCluster test-cluster is not found",
		},
		{
			name:                   "cluster with delete-backups finalizer is terminating",
			deleteBackupsFinalizer: true,
			expectedError:          "PerconaPGCluster test-cluster is being deleted",
		},
		{
			name:          "cluster without delete-backups finalizer is deleted during snapshot",
			snapshot:      true,
			expectedError: "PerconaPGCluster test-cluster is not found",
		},
		{
			name:                   "cluster with delete-backups finalizer is terminating during snapshot",
			deleteBackupsFinalizer: true,
			snapshot:               true,
			expectedError:          "PerconaPGCluster test-cluster is being deleted",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cluster, err := readDefaultCR("test-cluster", "test-namespace")
			require.NoError(t, err)
			if tt.deleteBackupsFinalizer {
				cluster.Finalizers = []string{pNaming.FinalizerDeleteBackups}
			}
			if tt.snapshot {
				cluster.Spec.Backups.VolumeSnapshots = &v2.VolumeSnapshots{
					Mode:      v2.VolumeSnapshotModeOffline,
					ClassName: "snapshot-class",
				}
			}

			backup := &v2.PerconaPGBackup{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-backup",
					Namespace:  cluster.Namespace,
					Finalizers: []string{pNaming.FinalizerDeleteBackup},
				},
				Spec: v2.PerconaPGBackupSpec{
					PGCluster: cluster.Name,
					RepoName:  new("repo1"),
				},
				Status: v2.PerconaPGBackupStatus{
					State:   v2.BackupRunning,
					JobName: "test-backup-job",
				},
			}
			if tt.snapshot {
				backup.Spec.Method = new(v2.BackupMethodVolumeSnapshot)
				backup.UID = "snapshot-uid"
				backup.Finalizers = []string{pNaming.FinalizerSnapshotInProgress}
				backup.Status.Conditions = []metav1.Condition{{
					Type:   v2.ConditionBackupLeaseAcquired,
					Status: metav1.ConditionTrue,
				}}
			}

			objects := []client.Object{backup}
			if !tt.snapshot {
				objects = append(objects, &batchv1.Job{ObjectMeta: metav1.ObjectMeta{
					Name:       backup.Status.JobName,
					Namespace:  backup.Namespace,
					Finalizers: []string{pNaming.FinalizerKeepJob},
				}})
			}
			if tt.snapshot {
				holder := backupLeaseHolder(backup)
				objects = append(objects, &coordinationv1.Lease{
					ObjectMeta: metav1.ObjectMeta{
						Name:      backupLeaseName(cluster.Name),
						Namespace: cluster.Namespace,
						UID:       "lease-uid",
					},
					Spec: coordinationv1.LeaseSpec{HolderIdentity: &holder},
				})
			}

			cl, err := buildFakeClient(ctx, cluster, objects...)
			require.NoError(t, err)
			require.NoError(t, cl.Delete(ctx, cluster))

			r := &PGBackupReconciler{Client: cl}
			_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(backup)})
			require.NoError(t, err)

			updated := new(v2.PerconaPGBackup)
			require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(backup), updated))
			assert.Equal(t, v2.BackupFailed, updated.Status.State)
			assert.Equal(t, tt.expectedError, updated.Status.Error)

			if tt.snapshot {
				assert.NotContains(t, updated.Finalizers, pNaming.FinalizerSnapshotInProgress)
				assert.False(t, meta.IsStatusConditionTrue(updated.Status.Conditions, v2.ConditionBackupLeaseAcquired))

				err = cl.Get(ctx, client.ObjectKey{
					Name:      backupLeaseName(cluster.Name),
					Namespace: cluster.Namespace,
				}, new(coordinationv1.Lease))
				assert.True(t, k8serrors.IsNotFound(err))
			} else {
				assert.Contains(t, updated.Finalizers, pNaming.FinalizerDeleteBackup)

				err = cl.Get(ctx, client.ObjectKey{
					Name: backup.Status.JobName, Namespace: backup.Namespace,
				}, new(batchv1.Job))
				assert.True(t, k8serrors.IsNotFound(err))

				_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(backup)})
				require.NoError(t, err)
				require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(backup), updated))
				assert.NotContains(t, updated.Finalizers, pNaming.FinalizerDeleteBackup)
			}
		})
	}
}

func TestReconcileDeletesStartingBackupJobWhenClusterIsDeleted(t *testing.T) {
	ctx := t.Context()
	tests := []struct {
		name                   string
		deleteBackupsFinalizer bool
		expectedError          string
	}{
		{
			name:          "cluster without delete-backups finalizer is deleted",
			expectedError: "PerconaPGCluster test-cluster is not found",
		},
		{
			name:                   "cluster with delete-backups finalizer is terminating",
			deleteBackupsFinalizer: true,
			expectedError:          "PerconaPGCluster test-cluster is being deleted",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cluster, err := readDefaultCR("test-cluster", "test-namespace")
			require.NoError(t, err)
			if tt.deleteBackupsFinalizer {
				cluster.Finalizers = []string{pNaming.FinalizerDeleteBackups}
			}

			backup := &v2.PerconaPGBackup{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-backup",
					Namespace:  cluster.Namespace,
					Finalizers: []string{pNaming.FinalizerDeleteBackup},
				},
				Spec: v2.PerconaPGBackupSpec{
					PGCluster: cluster.Name,
					RepoName:  new("repo1"),
				},
				Status: v2.PerconaPGBackupStatus{State: v2.BackupStarting},
			}
			job := &batchv1.Job{ObjectMeta: metav1.ObjectMeta{
				Name:      "test-cluster-backup-xff6",
				Namespace: backup.Namespace,
				Labels: naming.PGBackRestBackupJobLabels(
					cluster.Name, *backup.Spec.RepoName, naming.BackupManual,
				),
				Annotations: map[string]string{
					naming.PGBackRestBackup: backup.Name,
				},
				Finalizers: []string{pNaming.FinalizerKeepJob},
			}}

			cl, err := buildFakeClient(ctx, cluster, backup, job)
			require.NoError(t, err)
			require.NoError(t, cl.Delete(ctx, cluster))

			r := &PGBackupReconciler{Client: cl}
			request := reconcile.Request{NamespacedName: client.ObjectKeyFromObject(backup)}
			_, err = r.Reconcile(ctx, request)
			require.NoError(t, err)

			updated := new(v2.PerconaPGBackup)
			require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(backup), updated))
			assert.Equal(t, v2.BackupFailed, updated.Status.State)
			assert.Equal(t, tt.expectedError, updated.Status.Error)
			assert.Empty(t, updated.Status.JobName)
			assert.Contains(t, updated.Finalizers, pNaming.FinalizerDeleteBackup)

			err = cl.Get(ctx, client.ObjectKeyFromObject(job), new(batchv1.Job))
			assert.True(t, k8serrors.IsNotFound(err))

			_, err = r.Reconcile(ctx, request)
			require.NoError(t, err)
			require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(backup), updated))
			assert.NotContains(t, updated.Finalizers, pNaming.FinalizerDeleteBackup)
		})
	}
}

func TestReconcileDeletesJobWhenStartingBackupIsDeleted(t *testing.T) {
	ctx := t.Context()
	cluster, err := readDefaultCR("test-cluster", "test-namespace")
	require.NoError(t, err)

	backup := &v2.PerconaPGBackup{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test-backup",
			Namespace:  cluster.Namespace,
			Finalizers: []string{pNaming.FinalizerDeleteBackup},
		},
		Spec: v2.PerconaPGBackupSpec{
			PGCluster: cluster.Name,
			RepoName:  new("repo1"),
		},
		Status: v2.PerconaPGBackupStatus{State: v2.BackupStarting},
	}
	cluster.Annotations[naming.PGBackRestBackup] = backup.Name
	cluster.Annotations[pNaming.AnnotationBackupInProgress] = backup.Name

	job := &batchv1.Job{ObjectMeta: metav1.ObjectMeta{
		Name:      "test-cluster-backup-xff6",
		Namespace: backup.Namespace,
		Labels: naming.PGBackRestBackupJobLabels(
			cluster.Name, *backup.Spec.RepoName, naming.BackupManual,
		),
		Annotations: map[string]string{
			naming.PGBackRestBackup: backup.Name,
		},
		Finalizers: []string{pNaming.FinalizerKeepJob},
	}}

	cl, err := buildFakeClient(ctx, cluster, backup, job)
	require.NoError(t, err)
	require.NoError(t, cl.Delete(ctx, backup))

	r := &PGBackupReconciler{Client: cl}
	request := reconcile.Request{NamespacedName: client.ObjectKeyFromObject(backup)}
	_, err = r.Reconcile(ctx, request)
	require.NoError(t, err)

	updated := new(v2.PerconaPGBackup)
	require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(backup), updated))
	assert.False(t, updated.DeletionTimestamp.IsZero())
	assert.Contains(t, updated.Finalizers, pNaming.FinalizerDeleteBackup)
	assert.Equal(t, job.Name, updated.Status.JobName)

	currentJob := new(batchv1.Job)
	require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(job), currentJob))
	assert.True(t, currentJob.DeletionTimestamp.IsZero())
	assert.Contains(t, currentJob.Finalizers, pNaming.FinalizerKeepJob)

	// The next reconciliation waits for Crunchy to observe the annotation cleanup,
	// so the Job is left untouched.
	_, err = r.Reconcile(ctx, request)
	require.NoError(t, err)
	require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(job), currentJob))
	assert.True(t, currentJob.DeletionTimestamp.IsZero())
	assert.Contains(t, currentJob.Finalizers, pNaming.FinalizerKeepJob)

	currentCluster := new(v2.PerconaPGCluster)
	require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(cluster), currentCluster))
	delete(currentCluster.Annotations, naming.PGBackRestBackup)
	delete(currentCluster.Annotations, pNaming.AnnotationBackupInProgress)
	require.NoError(t, cl.Update(ctx, currentCluster))

	crunchyCluster := new(v1beta1.PostgresCluster)
	require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(cluster), crunchyCluster))
	delete(crunchyCluster.Annotations, pNaming.ToCrunchyAnnotation(naming.PGBackRestBackup))
	delete(crunchyCluster.Annotations, pNaming.ToCrunchyAnnotation(pNaming.AnnotationBackupInProgress))
	require.NoError(t, cl.Update(ctx, crunchyCluster))

	_, err = r.Reconcile(ctx, request)
	require.NoError(t, err)
	err = cl.Get(ctx, client.ObjectKeyFromObject(job), new(batchv1.Job))
	assert.True(t, k8serrors.IsNotFound(err))
}

func TestReconcileNotUpdatingOldBackup(t *testing.T) {
	ctx := t.Context()
	cluster, err := readDefaultCR("test-cluster", "test-namespace")
	require.NoError(t, err)

	now := metav1.NewTime(time.Now().Truncate(time.Microsecond))
	latestCompletedAt := metav1.NewTime(now.Add(time.Hour))
	latestRestorableTime := metav1.NewTime(now.Add(30 * time.Minute))
	newLatestRestorableTime := metav1.NewTime(now.Add(45 * time.Minute))
	oldBackup := &v2.PerconaPGBackup{
		ObjectMeta: metav1.ObjectMeta{Name: "old-backup", Namespace: cluster.Namespace},
		Spec: v2.PerconaPGBackupSpec{
			PGCluster: cluster.Name,
			RepoName:  new("repo1"),
		},
		Status: v2.PerconaPGBackupStatus{
			State:                v2.BackupSucceeded,
			CompletedAt:          &now,
			LatestRestorableTime: v2.PITRestoreDateTime{Time: &latestRestorableTime},
		},
	}
	latestBackup := &v2.PerconaPGBackup{
		ObjectMeta: metav1.ObjectMeta{Name: "latest-backup", Namespace: cluster.Namespace},
		Spec: v2.PerconaPGBackupSpec{
			PGCluster: cluster.Name,
			RepoName:  new("repo1"),
		},
		Status: v2.PerconaPGBackupStatus{
			State:       v2.BackupSucceeded,
			CompletedAt: &latestCompletedAt,
		},
	}

	cl, err := buildFakeClient(ctx, cluster, oldBackup, latestBackup)
	require.NoError(t, err)
	timestampRequested := false
	r := &PGBackupReconciler{
		Client: cl,
		LatestCommitGetter: func(context.Context, client.Client, *v2.PerconaPGCluster, *v2.PerconaPGBackup) (*metav1.Time, error) {
			timestampRequested = true
			return &newLatestRestorableTime, nil
		},
	}

	_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(oldBackup)})
	require.NoError(t, err)

	updated := new(v2.PerconaPGBackup)
	require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(oldBackup), updated))
	assert.False(t, timestampRequested)
	require.NotNil(t, updated.Status.LatestRestorableTime.Time)
	assert.True(t, updated.Status.LatestRestorableTime.Equal(&latestRestorableTime))
}

func TestFailIfClusterIsNotReady(t *testing.T) {
	ctx := context.Background()

	cr, err := readDefaultCR("ready-for-backup", "ready-for-backup")
	if err != nil {
		t.Fatal(err)
	}
	cr.Annotations[pNaming.AnnotationBackupInProgress] = "some-backup"
	cr.Annotations[naming.PGBackRestBackup] = "some-backup"

	tests := []struct {
		name          string
		cr            *v2.PerconaPGCluster
		pgBackup      *v2.PerconaPGBackup
		expectedState v2.PGBackupState
	}{
		{
			name: "no condition",
			cr:   cr.DeepCopy(),
			pgBackup: &v2.PerconaPGBackup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "some-backup",
					Namespace: cr.Namespace,
				},
				Status: v2.PerconaPGBackupStatus{
					State: v2.BackupStarting,
				},
			},
			expectedState: v2.BackupStarting,
		},
		{
			name: "true condition",
			cr: func() *v2.PerconaPGCluster {
				cr := cr.DeepCopy()
				cr.Status.Conditions = append(cr.Status.Conditions, metav1.Condition{
					Type:               pNaming.ConditionClusterIsReadyForBackup,
					Status:             metav1.ConditionTrue,
					LastTransitionTime: metav1.Now(),
				})
				return cr
			}(),
			pgBackup: &v2.PerconaPGBackup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "some-backup",
					Namespace: cr.Namespace,
				},
				Status: v2.PerconaPGBackupStatus{
					State: v2.BackupStarting,
				},
			},
			expectedState: v2.BackupStarting,
		},
		{
			name: "false condition with current time",
			cr: func() *v2.PerconaPGCluster {
				cr := cr.DeepCopy()
				cr.Status.Conditions = append(cr.Status.Conditions, metav1.Condition{
					Type:               pNaming.ConditionClusterIsReadyForBackup,
					Status:             metav1.ConditionFalse,
					LastTransitionTime: metav1.Now(),
				})
				return cr
			}(),
			pgBackup: &v2.PerconaPGBackup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "some-backup",
					Namespace: cr.Namespace,
				},
				Status: v2.PerconaPGBackupStatus{
					State: v2.BackupStarting,
				},
			},
			expectedState: v2.BackupStarting,
		},
		{
			name: "false condition with old time",
			cr: func() *v2.PerconaPGCluster {
				cr := cr.DeepCopy()
				cr.Status.Conditions = append(cr.Status.Conditions, metav1.Condition{
					Type:               pNaming.ConditionClusterIsReadyForBackup,
					Status:             metav1.ConditionFalse,
					LastTransitionTime: metav1.Time{Time: time.Now().Add(-time.Hour)},
				})
				return cr
			}(),
			pgBackup: &v2.PerconaPGBackup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "some-backup",
					Namespace: cr.Namespace,
				},
				Status: v2.PerconaPGBackupStatus{
					State: v2.BackupStarting,
				},
			},
			expectedState: v2.BackupFailed,
		},
		{
			name: "false condition with old time without annotations",
			cr: func() *v2.PerconaPGCluster {
				cr := cr.DeepCopy()
				cr.Annotations = make(map[string]string)
				cr.Status.Conditions = append(cr.Status.Conditions, metav1.Condition{
					Type:               pNaming.ConditionClusterIsReadyForBackup,
					Status:             metav1.ConditionFalse,
					LastTransitionTime: metav1.Time{Time: time.Now().Add(-time.Hour)},
				})
				return cr
			}(),
			pgBackup: &v2.PerconaPGBackup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "some-backup",
					Namespace: cr.Namespace,
				},
				Status: v2.PerconaPGBackupStatus{
					State: v2.BackupStarting,
				},
			},
			expectedState: v2.BackupStarting,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cl, err := buildFakeClient(ctx, tt.cr, tt.pgBackup)
			if err != nil {
				t.Fatal(err)
			}

			crunchyCluster := new(v1beta1.PostgresCluster)
			if err := cl.Get(ctx, client.ObjectKeyFromObject(tt.cr), crunchyCluster); err != nil {
				t.Fatal(err)
			}
			if err := failIfClusterIsNotReady(ctx, cl, tt.cr, tt.pgBackup); err != nil {
				t.Fatal(err)
			}
			bcp := new(v2.PerconaPGBackup)
			if err := cl.Get(ctx, client.ObjectKeyFromObject(tt.pgBackup), bcp); err != nil {
				t.Fatal(err)
			}
			if bcp.Status.State != tt.expectedState {
				t.Fatalf("expected state: %s; got: %s", tt.expectedState, bcp.Status.State)
			}
		})
	}
}

func TestTryAcquireLease(t *testing.T) {
	ctx := t.Context()
	ns := "test-ns"
	clusterName := "my-cluster"

	s := scheme.Scheme
	require.NoError(t, corev1.AddToScheme(s))
	require.NoError(t, v2.AddToScheme(s))
	require.NoError(t, coordinationv1.AddToScheme(s))

	t.Run("acquires lease when no lease exists", func(t *testing.T) {
		backup := &v2.PerconaPGBackup{
			ObjectMeta: metav1.ObjectMeta{Name: "backup-1", Namespace: ns, UID: "uid-1"},
			Spec:       v2.PerconaPGBackupSpec{PGCluster: clusterName},
		}
		cl := fake.NewClientBuilder().
			WithScheme(s).
			WithObjects(backup).
			WithStatusSubresource(backup).
			Build()

		r := &PGBackupReconciler{Client: cl}
		acquired, err := r.tryAcquireLease(ctx, backup)
		require.NoError(t, err)
		assert.True(t, acquired)

		lease := &coordinationv1.Lease{}
		require.NoError(t, cl.Get(ctx, client.ObjectKey{
			Name:      "pg-" + clusterName + "-backup-lock",
			Namespace: ns,
		}, lease))
		require.NotNil(t, lease.Spec.HolderIdentity)
		assert.Equal(t, "backup-1|uid-1", *lease.Spec.HolderIdentity)

		require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(backup), backup))
		assert.True(t, meta.IsStatusConditionTrue(backup.Status.Conditions, v2.ConditionBackupLeaseAcquired))
	})

	t.Run("returns false when lease is held by another active backup", func(t *testing.T) {
		otherBackup := &v2.PerconaPGBackup{
			ObjectMeta: metav1.ObjectMeta{Name: "other-backup", Namespace: ns, UID: "uid-other"},
			Spec:       v2.PerconaPGBackupSpec{PGCluster: clusterName},
			Status: v2.PerconaPGBackupStatus{
				State: v2.BackupRunning,
			},
		}
		existingLease := &coordinationv1.Lease{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pg-" + clusterName + "-backup-lock",
				Namespace: ns,
			},
			Spec: coordinationv1.LeaseSpec{
				HolderIdentity: new("other-backup|uid-other"),
			},
		}
		backup := &v2.PerconaPGBackup{
			ObjectMeta: metav1.ObjectMeta{Name: "backup-1", Namespace: ns, UID: "uid-1"},
			Spec:       v2.PerconaPGBackupSpec{PGCluster: clusterName},
		}
		cl := fake.NewClientBuilder().
			WithScheme(s).
			WithObjects(backup, otherBackup, existingLease).
			WithStatusSubresource(backup).
			Build()

		r := &PGBackupReconciler{Client: cl}
		acquired, err := r.tryAcquireLease(ctx, backup)
		require.NoError(t, err)
		assert.False(t, acquired)

		require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(backup), backup))
		assert.True(t, meta.IsStatusConditionFalse(backup.Status.Conditions, v2.ConditionBackupLeaseAcquired))
	})

	t.Run("returns false when backup has finalizers", func(t *testing.T) {
		otherBackup := &v2.PerconaPGBackup{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "other-backup",
				Namespace:  ns,
				UID:        "uid-other",
				Finalizers: []string{pNaming.FinalizerSnapshotInProgress},
			},
			Spec: v2.PerconaPGBackupSpec{PGCluster: clusterName},
			Status: v2.PerconaPGBackupStatus{
				State: v2.BackupFailed,
			},
		}
		existingLease := &coordinationv1.Lease{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pg-" + clusterName + "-backup-lock",
				Namespace: ns,
			},
			Spec: coordinationv1.LeaseSpec{
				HolderIdentity: new("other-backup|uid-other"),
			},
		}
		backup := &v2.PerconaPGBackup{
			ObjectMeta: metav1.ObjectMeta{Name: "backup-1", Namespace: ns, UID: "uid-1"},
			Spec:       v2.PerconaPGBackupSpec{PGCluster: clusterName},
		}
		cl := fake.NewClientBuilder().
			WithScheme(s).
			WithObjects(backup, otherBackup, existingLease).
			WithStatusSubresource(backup).
			Build()

		r := &PGBackupReconciler{Client: cl}
		acquired, err := r.tryAcquireLease(ctx, backup)
		require.NoError(t, err)
		assert.False(t, acquired)

		require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(backup), backup))
		assert.True(t, meta.IsStatusConditionFalse(backup.Status.Conditions, v2.ConditionBackupLeaseAcquired))
	})

	t.Run("acquires stale lease when holder backup is not found", func(t *testing.T) {
		existingLease := &coordinationv1.Lease{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pg-" + clusterName + "-backup-lock",
				Namespace: ns,
			},
			Spec: coordinationv1.LeaseSpec{
				HolderIdentity: new("deleted-backup|uid-deleted"),
			},
		}
		backup := &v2.PerconaPGBackup{
			ObjectMeta: metav1.ObjectMeta{Name: "backup-1", Namespace: ns, UID: "uid-1"},
			Spec:       v2.PerconaPGBackupSpec{PGCluster: clusterName},
		}
		cl := fake.NewClientBuilder().
			WithScheme(s).
			WithObjects(backup, existingLease).
			WithStatusSubresource(backup).
			Build()

		r := &PGBackupReconciler{Client: cl}
		acquired, err := r.tryAcquireLease(ctx, backup)
		require.NoError(t, err)
		assert.True(t, acquired)

		lease := &coordinationv1.Lease{}
		require.NoError(t, cl.Get(ctx, client.ObjectKey{
			Name:      "pg-" + clusterName + "-backup-lock",
			Namespace: ns,
		}, lease))
		require.NotNil(t, lease.Spec.HolderIdentity)
		assert.Equal(t, "backup-1|uid-1", *lease.Spec.HolderIdentity)

		require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(backup), backup))
		assert.True(t, meta.IsStatusConditionTrue(backup.Status.Conditions, v2.ConditionBackupLeaseAcquired))
	})

	t.Run("acquires stale lease when holder backup has succeeded", func(t *testing.T) {
		completedBackup := &v2.PerconaPGBackup{
			ObjectMeta: metav1.ObjectMeta{Name: "completed-backup", Namespace: ns, UID: "uid-completed"},
			Spec:       v2.PerconaPGBackupSpec{PGCluster: clusterName},
			Status: v2.PerconaPGBackupStatus{
				State: v2.BackupSucceeded,
			},
		}
		existingLease := &coordinationv1.Lease{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pg-" + clusterName + "-backup-lock",
				Namespace: ns,
			},
			Spec: coordinationv1.LeaseSpec{
				HolderIdentity: new("completed-backup|uid-completed"),
			},
		}
		backup := &v2.PerconaPGBackup{
			ObjectMeta: metav1.ObjectMeta{Name: "backup-1", Namespace: ns, UID: "uid-1"},
			Spec:       v2.PerconaPGBackupSpec{PGCluster: clusterName},
		}
		cl := fake.NewClientBuilder().
			WithScheme(s).
			WithObjects(backup, completedBackup, existingLease).
			WithStatusSubresource(backup, completedBackup).
			Build()

		r := &PGBackupReconciler{Client: cl}
		acquired, err := r.tryAcquireLease(ctx, backup)
		require.NoError(t, err)
		assert.True(t, acquired)

		require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(backup), backup))
		assert.True(t, meta.IsStatusConditionTrue(backup.Status.Conditions, v2.ConditionBackupLeaseAcquired))
	})

	t.Run("acquires stale lease when holder backup has failed", func(t *testing.T) {
		failedBackup := &v2.PerconaPGBackup{
			ObjectMeta: metav1.ObjectMeta{Name: "failed-backup", Namespace: ns, UID: "uid-failed"},
			Spec:       v2.PerconaPGBackupSpec{PGCluster: clusterName},
			Status: v2.PerconaPGBackupStatus{
				State: v2.BackupFailed,
			},
		}
		existingLease := &coordinationv1.Lease{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pg-" + clusterName + "-backup-lock",
				Namespace: ns,
			},
			Spec: coordinationv1.LeaseSpec{
				HolderIdentity: new("failed-backup|uid-failed"),
			},
		}
		backup := &v2.PerconaPGBackup{
			ObjectMeta: metav1.ObjectMeta{Name: "backup-1", Namespace: ns, UID: "uid-1"},
			Spec:       v2.PerconaPGBackupSpec{PGCluster: clusterName},
		}
		cl := fake.NewClientBuilder().
			WithScheme(s).
			WithObjects(backup, failedBackup, existingLease).
			WithStatusSubresource(backup, failedBackup).
			Build()

		r := &PGBackupReconciler{Client: cl}
		acquired, err := r.tryAcquireLease(ctx, backup)
		require.NoError(t, err)
		assert.True(t, acquired)

		require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(backup), backup))
		assert.True(t, meta.IsStatusConditionTrue(backup.Status.Conditions, v2.ConditionBackupLeaseAcquired))
	})

	t.Run("acquires stale lease when holder backup is found but has a different UID", func(t *testing.T) {
		existingBackup := &v2.PerconaPGBackup{
			ObjectMeta: metav1.ObjectMeta{Name: "backup-1", Namespace: ns, UID: "uid-abc"},
			Spec:       v2.PerconaPGBackupSpec{PGCluster: clusterName},
			Status:     v2.PerconaPGBackupStatus{},
		}
		existingLease := &coordinationv1.Lease{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pg-" + clusterName + "-backup-lock",
				Namespace: ns,
			},
			Spec: coordinationv1.LeaseSpec{
				HolderIdentity: new("backup-1|uid-xyz"),
			},
		}
		backup := &v2.PerconaPGBackup{
			ObjectMeta: metav1.ObjectMeta{Name: "backup-2", Namespace: ns, UID: "uid-1"},
			Spec:       v2.PerconaPGBackupSpec{PGCluster: clusterName},
		}
		cl := fake.NewClientBuilder().
			WithScheme(s).
			WithObjects(backup, existingBackup, existingLease).
			WithStatusSubresource(backup).
			Build()

		r := &PGBackupReconciler{Client: cl}
		acquired, err := r.tryAcquireLease(ctx, backup)
		require.NoError(t, err)
		assert.True(t, acquired)

		require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(backup), backup))
		assert.True(t, meta.IsStatusConditionTrue(backup.Status.Conditions, v2.ConditionBackupLeaseAcquired))
	})

	t.Run("returns true when lease is already held by self", func(t *testing.T) {
		backup := &v2.PerconaPGBackup{
			ObjectMeta: metav1.ObjectMeta{Name: "backup-1", Namespace: ns, UID: "uid-1"},
			Spec:       v2.PerconaPGBackupSpec{PGCluster: clusterName},
		}
		existingLease := &coordinationv1.Lease{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pg-" + clusterName + "-backup-lock",
				Namespace: ns,
			},
			Spec: coordinationv1.LeaseSpec{
				HolderIdentity: new("backup-1|uid-1"),
			},
		}
		cl := fake.NewClientBuilder().
			WithScheme(s).
			WithObjects(backup, existingLease).
			WithStatusSubresource(backup).
			Build()

		r := &PGBackupReconciler{Client: cl}
		acquired, err := r.tryAcquireLease(ctx, backup)
		require.NoError(t, err)
		assert.True(t, acquired)

		require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(backup), backup))
		assert.True(t, meta.IsStatusConditionTrue(backup.Status.Conditions, v2.ConditionBackupLeaseAcquired))
	})
}

func TestReleaseLeaseIfNeeded(t *testing.T) {
	ctx := t.Context()
	ns := "test-ns"
	clusterName := "my-cluster"

	s := scheme.Scheme
	require.NoError(t, corev1.AddToScheme(s))
	require.NoError(t, v2.AddToScheme(s))
	require.NoError(t, coordinationv1.AddToScheme(s))

	leaseName := "pg-" + clusterName + "-backup-lock"

	t.Run("no-op when lease condition is not present", func(t *testing.T) {
		backup := &v2.PerconaPGBackup{
			ObjectMeta: metav1.ObjectMeta{Name: "backup-1", Namespace: ns, UID: "uid-1"},
			Spec:       v2.PerconaPGBackupSpec{PGCluster: clusterName},
		}
		existingLease := &coordinationv1.Lease{
			ObjectMeta: metav1.ObjectMeta{Name: leaseName, Namespace: ns},
			Spec: coordinationv1.LeaseSpec{
				HolderIdentity: new("backup-1|uid-1"),
			},
		}
		cl := fake.NewClientBuilder().
			WithScheme(s).
			WithObjects(backup, existingLease).
			WithStatusSubresource(backup).
			Build()

		r := &PGBackupReconciler{Client: cl}
		require.NoError(t, r.releaseLeaseIfNeeded(ctx, backup))

		lease := &coordinationv1.Lease{}
		require.NoError(t, cl.Get(ctx, client.ObjectKey{Name: leaseName, Namespace: ns}, lease))
		assert.NotNil(t, lease.Spec.HolderIdentity, "lease should not be deleted")
	})

	t.Run("no-op when lease condition is false", func(t *testing.T) {
		backup := &v2.PerconaPGBackup{
			ObjectMeta: metav1.ObjectMeta{Name: "backup-1", Namespace: ns, UID: "uid-1"},
			Spec:       v2.PerconaPGBackupSpec{PGCluster: clusterName},
			Status: v2.PerconaPGBackupStatus{
				Conditions: []metav1.Condition{
					{
						Type:               v2.ConditionBackupLeaseAcquired,
						Status:             metav1.ConditionFalse,
						LastTransitionTime: metav1.Now(),
					},
				},
			},
		}
		existingLease := &coordinationv1.Lease{
			ObjectMeta: metav1.ObjectMeta{Name: leaseName, Namespace: ns},
			Spec: coordinationv1.LeaseSpec{
				HolderIdentity: new("backup-1|uid-1"),
			},
		}
		cl := fake.NewClientBuilder().
			WithScheme(s).
			WithObjects(backup, existingLease).
			WithStatusSubresource(backup).
			Build()

		r := &PGBackupReconciler{Client: cl}
		require.NoError(t, r.releaseLeaseIfNeeded(ctx, backup))

		lease := &coordinationv1.Lease{}
		require.NoError(t, cl.Get(ctx, client.ObjectKey{Name: leaseName, Namespace: ns}, lease))
		assert.NotNil(t, lease.Spec.HolderIdentity, "lease should not be deleted")
	})

	t.Run("releases lease and removes condition when condition is true", func(t *testing.T) {
		backup := &v2.PerconaPGBackup{
			ObjectMeta: metav1.ObjectMeta{Name: "backup-1", Namespace: ns, UID: "uid-1"},
			Spec:       v2.PerconaPGBackupSpec{PGCluster: clusterName},
			Status: v2.PerconaPGBackupStatus{
				Conditions: []metav1.Condition{
					{
						Type:               v2.ConditionBackupLeaseAcquired,
						Status:             metav1.ConditionTrue,
						LastTransitionTime: metav1.Now(),
					},
				},
			},
		}
		existingLease := &coordinationv1.Lease{
			ObjectMeta: metav1.ObjectMeta{Name: leaseName, Namespace: ns},
			Spec: coordinationv1.LeaseSpec{
				HolderIdentity: new("backup-1|uid-1"),
			},
		}
		cl := fake.NewClientBuilder().
			WithScheme(s).
			WithObjects(backup, existingLease).
			WithStatusSubresource(backup).
			Build()

		r := &PGBackupReconciler{Client: cl}
		require.NoError(t, r.releaseLeaseIfNeeded(ctx, backup))

		lease := &coordinationv1.Lease{}
		err := cl.Get(ctx, client.ObjectKey{Name: leaseName, Namespace: ns}, lease)
		assert.True(t, k8serrors.IsNotFound(err), "lease should be deleted")

		require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(backup), backup))
		assert.False(t, meta.IsStatusConditionPresentAndEqual(backup.Status.Conditions, v2.ConditionBackupLeaseAcquired, metav1.ConditionTrue),
			"condition should be removed")
	})

	t.Run("succeeds when lease does not exist", func(t *testing.T) {
		backup := &v2.PerconaPGBackup{
			ObjectMeta: metav1.ObjectMeta{Name: "backup-1", Namespace: ns, UID: "uid-1"},
			Spec:       v2.PerconaPGBackupSpec{PGCluster: clusterName},
			Status: v2.PerconaPGBackupStatus{
				Conditions: []metav1.Condition{
					{
						Type:               v2.ConditionBackupLeaseAcquired,
						Status:             metav1.ConditionTrue,
						LastTransitionTime: metav1.Now(),
					},
				},
			},
		}
		cl := fake.NewClientBuilder().
			WithScheme(s).
			WithObjects(backup).
			WithStatusSubresource(backup).
			Build()

		r := &PGBackupReconciler{Client: cl}
		require.NoError(t, r.releaseLeaseIfNeeded(ctx, backup))

		require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(backup), backup))
		assert.False(t, meta.IsStatusConditionPresentAndEqual(backup.Status.Conditions, v2.ConditionBackupLeaseAcquired, metav1.ConditionTrue),
			"condition should be removed")
	})

	t.Run("returns error when lease is held by another backup", func(t *testing.T) {
		backup := &v2.PerconaPGBackup{
			ObjectMeta: metav1.ObjectMeta{Name: "backup-1", Namespace: ns, UID: "uid-1"},
			Spec:       v2.PerconaPGBackupSpec{PGCluster: clusterName},
			Status: v2.PerconaPGBackupStatus{
				Conditions: []metav1.Condition{
					{
						Type:               v2.ConditionBackupLeaseAcquired,
						Status:             metav1.ConditionTrue,
						LastTransitionTime: metav1.Now(),
					},
				},
			},
		}
		existingLease := &coordinationv1.Lease{
			ObjectMeta: metav1.ObjectMeta{Name: leaseName, Namespace: ns},
			Spec: coordinationv1.LeaseSpec{
				HolderIdentity: new("other-backup|uid-other"),
			},
		}
		cl := fake.NewClientBuilder().
			WithScheme(s).
			WithObjects(backup, existingLease).
			WithStatusSubresource(backup).
			Build()

		r := &PGBackupReconciler{Client: cl}
		err := r.releaseLeaseIfNeeded(ctx, backup)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to release lease")
	})
}

func TestStartBackup(t *testing.T) {
	ctx := t.Context()
	ns := "test-ns"
	clusterName := "my-cluster"

	s := scheme.Scheme
	require.NoError(t, corev1.AddToScheme(s))
	require.NoError(t, v2.AddToScheme(s))

	newCluster := func() *v2.PerconaPGCluster {
		return &v2.PerconaPGCluster{
			ObjectMeta: metav1.ObjectMeta{Name: clusterName, Namespace: ns},
			Spec: v2.PerconaPGClusterSpec{
				CRVersion:       "2.9.0",
				PostgresVersion: 17,
			},
		}
	}

	newBackup := func() *v2.PerconaPGBackup {
		repo := "repo1"
		return &v2.PerconaPGBackup{
			ObjectMeta: metav1.ObjectMeta{Name: "backup-1", Namespace: ns},
			Spec: v2.PerconaPGBackupSpec{
				PGCluster: clusterName,
				RepoName:  &repo,
			},
		}
	}

	t.Run("marks the cluster for backup", func(t *testing.T) {
		cluster, backup := newCluster(), newBackup()
		cl := fake.NewClientBuilder().WithScheme(s).WithObjects(cluster, backup).Build()

		require.NoError(t, startBackup(ctx, cl, backup))

		updated := &v2.PerconaPGCluster{}
		require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(cluster), updated))

		assert.Equal(t, backup.Name, updated.Annotations[naming.PGBackRestBackup])
		assert.Equal(t, backup.Name, updated.Annotations[pNaming.AnnotationBackupInProgress])
		require.NotNil(t, updated.Spec.Backups.PGBackRest.Manual)
		assert.Equal(t, "repo1", updated.Spec.Backups.PGBackRest.Manual.RepoName)
	})

	t.Run("does not persist defaults", func(t *testing.T) {
		cluster, backup := newCluster(), newBackup()
		cl := fake.NewClientBuilder().WithScheme(s).WithObjects(cluster, backup).Build()

		defaulted := cluster.DeepCopy()
		defaulted.Default()
		require.NotNil(t, defaulted.Spec.Extensions.BuiltIn.PGStatMonitor) // nolint:staticcheck
		require.NotNil(t, defaulted.Spec.Extensions.PGStatMonitor.Enabled)
		require.NotNil(t, defaulted.Spec.AutoCreateUserSchema)

		require.NoError(t, startBackup(ctx, cl, backup))

		updated := &v2.PerconaPGCluster{}
		require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(cluster), updated))

		assert.Nil(t, updated.Spec.Extensions.BuiltIn.PGStatMonitor, // nolint:staticcheck
			"a backup must not write the deprecated builtin extension fields")
		assert.Nil(t, updated.Spec.Extensions.PGStatMonitor.Enabled,
			"a backup must not decide which extensions the user enabled")
		assert.Nil(t, updated.Spec.AutoCreateUserSchema,
			"a backup must not fill in unrelated spec defaults")
	})

	t.Run("refuses when another backup is running", func(t *testing.T) {
		cluster, backup := newCluster(), newBackup()
		cluster.Annotations = map[string]string{
			pNaming.AnnotationBackupInProgress: "other-backup",
		}
		cl := fake.NewClientBuilder().WithScheme(s).WithObjects(cluster, backup).Build()

		err := startBackup(ctx, cl, backup)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "other-backup")
	})
}

func TestFindBackupInPGBackrestInfo(t *testing.T) {
	tests := []struct {
		name         string
		infoOutput   pgbackrest.InfoOutput
		pgBackup     *v2.PerconaPGBackup
		expectFound  bool
		expectStanza string
		expectLabel  string
		expectType   v2.PGBackupType
		expectSize   int64
	}{
		{
			name: "matches backup by backup-name annotation",
			infoOutput: pgbackrest.InfoOutput{
				{
					Name: "db",
					Backup: []pgbackrest.InfoBackup{
						{
							Label: "20250722-120000F",
							Type:  v2.PGBackupTypeFull,
							Info: struct {
								Delta int64 `json:"delta,omitempty"`
							}{Delta: 1234567},
							Annotation: map[string]string{
								v2.PGBackrestAnnotationBackupName: "my-backup",
							},
						},
					},
				},
			},
			pgBackup: &v2.PerconaPGBackup{
				ObjectMeta: metav1.ObjectMeta{
					Name: "my-backup",
					Annotations: map[string]string{
						pNaming.AnnotationPGBackrestBackupJobType: string(naming.BackupManual),
					},
				},
				Status: v2.PerconaPGBackupStatus{
					JobName: "backup-job-1",
				},
			},
			expectFound:  true,
			expectStanza: "db",
			expectLabel:  "20250722-120000F",
			expectType:   v2.PGBackupTypeFull,
			expectSize:   1234567,
		},
		{
			name: "matches backup by job type annotation for manual backup",
			infoOutput: pgbackrest.InfoOutput{
				{
					Name: "db",
					Backup: []pgbackrest.InfoBackup{
						{
							Label: "20250722-130000F",
							Type:  v2.PGBackupTypeDifferential,
							Info: struct {
								Delta int64 `json:"delta,omitempty"`
							}{Delta: 9876543},
							Annotation: map[string]string{
								v2.PGBackrestAnnotationJobType:    string(naming.BackupManual),
								v2.PGBackrestAnnotationBackupName: "my-manual-backup",
							},
						},
					},
				},
			},
			pgBackup: &v2.PerconaPGBackup{
				ObjectMeta: metav1.ObjectMeta{
					Name: "my-manual-backup",
					Annotations: map[string]string{
						pNaming.AnnotationPGBackrestBackupJobType: string(naming.BackupManual),
					},
				},
				Status: v2.PerconaPGBackupStatus{
					JobName: "",
				},
			},
			expectFound:  true,
			expectStanza: "db",
			expectLabel:  "20250722-130000F",
			expectType:   v2.PGBackupTypeDifferential,
			expectSize:   9876543,
		},
		{
			name: "skips backup with different job name annotation",
			infoOutput: pgbackrest.InfoOutput{
				{
					Name: "db",
					Backup: []pgbackrest.InfoBackup{
						{
							Label: "20250722-140000F",
							Type:  v2.PGBackupTypeFull,
							Info: struct {
								Delta int64 `json:"delta,omitempty"`
							}{Delta: 111},
							Annotation: map[string]string{
								v2.PGBackrestAnnotationJobName:    "other-job",
								v2.PGBackrestAnnotationBackupName: "my-backup",
							},
						},
					},
				},
			},
			pgBackup: &v2.PerconaPGBackup{
				ObjectMeta: metav1.ObjectMeta{
					Name: "my-backup",
					Annotations: map[string]string{
						pNaming.AnnotationPGBackrestBackupJobType: string(naming.BackupManual),
					},
				},
				Status: v2.PerconaPGBackupStatus{
					JobName: "my-job",
				},
			},
			expectFound: false,
		},
		{
			name: "skips backup with no annotations",
			infoOutput: pgbackrest.InfoOutput{
				{
					Name: "db",
					Backup: []pgbackrest.InfoBackup{
						{
							Label: "20250722-150000F",
							Type:  v2.PGBackupTypeFull,
							Info: struct {
								Delta int64 `json:"delta,omitempty"`
							}{Delta: 222},
						},
					},
				},
			},
			pgBackup: &v2.PerconaPGBackup{
				ObjectMeta: metav1.ObjectMeta{
					Name: "my-backup",
					Annotations: map[string]string{
						pNaming.AnnotationPGBackrestBackupJobType: string(naming.BackupManual),
					},
				},
				Status: v2.PerconaPGBackupStatus{
					JobName: "my-job",
				},
			},
			expectFound: false,
		},
		{
			name:       "empty info output returns not found",
			infoOutput: pgbackrest.InfoOutput{},
			pgBackup: &v2.PerconaPGBackup{
				ObjectMeta: metav1.ObjectMeta{
					Name: "my-backup",
				},
				Status: v2.PerconaPGBackupStatus{
					JobName: "my-job",
				},
			},
			expectFound: false,
		},
		{
			name: "matches backup already annotated with same job name",
			infoOutput: pgbackrest.InfoOutput{
				{
					Name: "db",
					Backup: []pgbackrest.InfoBackup{
						{
							Label: "20250722-160000F",
							Type:  v2.PGBackupTypeIncremental,
							Info: struct {
								Delta int64 `json:"delta,omitempty"`
							}{Delta: 555},
							Annotation: map[string]string{
								v2.PGBackrestAnnotationJobName:    "my-job",
								v2.PGBackrestAnnotationBackupName: "my-backup",
								v2.PGBackrestAnnotationJobType:    string(naming.BackupManual),
							},
						},
					},
				},
			},
			pgBackup: &v2.PerconaPGBackup{
				ObjectMeta: metav1.ObjectMeta{
					Name: "my-backup",
					Annotations: map[string]string{
						pNaming.AnnotationPGBackrestBackupJobType: string(naming.BackupManual),
					},
				},
				Status: v2.PerconaPGBackupStatus{
					JobName: "my-job",
				},
			},
			expectFound:  true,
			expectStanza: "db",
			expectLabel:  "20250722-160000F",
			expectType:   v2.PGBackupTypeIncremental,
			expectSize:   555,
		},
		{
			name: "matches replica-create backup by job type",
			infoOutput: pgbackrest.InfoOutput{
				{
					Name: "db",
					Backup: []pgbackrest.InfoBackup{
						{
							Label: "20250722-170000F",
							Type:  v2.PGBackupTypeFull,
							Info: struct {
								Delta int64 `json:"delta,omitempty"`
							}{Delta: 999},
							Annotation: map[string]string{
								v2.PGBackrestAnnotationJobType:    string(naming.BackupReplicaCreate),
								v2.PGBackrestAnnotationBackupName: "replica-backup",
							},
						},
					},
				},
			},
			pgBackup: &v2.PerconaPGBackup{
				ObjectMeta: metav1.ObjectMeta{
					Name: "replica-backup",
					Annotations: map[string]string{
						pNaming.AnnotationPGBackrestBackupJobType: string(naming.BackupReplicaCreate),
					},
				},
				Status: v2.PerconaPGBackupStatus{
					JobName: "",
				},
			},
			expectFound:  true,
			expectStanza: "db",
			expectLabel:  "20250722-170000F",
			expectType:   v2.PGBackupTypeFull,
			expectSize:   999,
		},
		{
			name: "selects correct backup from multiple stanzas",
			infoOutput: pgbackrest.InfoOutput{
				{
					Name: "stanza1",
					Backup: []pgbackrest.InfoBackup{
						{
							Label: "20250722-100000F",
							Type:  v2.PGBackupTypeFull,
							Info: struct {
								Delta int64 `json:"delta,omitempty"`
							}{Delta: 100},
							Annotation: map[string]string{
								v2.PGBackrestAnnotationBackupName: "other-backup",
							},
						},
					},
				},
				{
					Name: "stanza2",
					Backup: []pgbackrest.InfoBackup{
						{
							Label: "20250722-110000F",
							Type:  v2.PGBackupTypeDifferential,
							Info: struct {
								Delta int64 `json:"delta,omitempty"`
							}{Delta: 200},
							Annotation: map[string]string{
								v2.PGBackrestAnnotationBackupName: "target-backup",
								v2.PGBackrestAnnotationJobType:    string(naming.BackupManual),
							},
						},
					},
				},
			},
			pgBackup: &v2.PerconaPGBackup{
				ObjectMeta: metav1.ObjectMeta{
					Name: "target-backup",
					Annotations: map[string]string{
						pNaming.AnnotationPGBackrestBackupJobType: string(naming.BackupManual),
					},
				},
				Status: v2.PerconaPGBackupStatus{
					JobName: "",
				},
			},
			expectFound:  true,
			expectStanza: "stanza2",
			expectLabel:  "20250722-110000F",
			expectType:   v2.PGBackupTypeDifferential,
			expectSize:   200,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stanza, backup, found := findBackupInPGBackrestInfo(tt.infoOutput, tt.pgBackup)

			assert.Equal(t, tt.expectFound, found, "unexpected found value")

			if !tt.expectFound {
				assert.Empty(t, stanza)
				assert.Empty(t, backup.Label)
				return
			}

			assert.Equal(t, tt.expectStanza, stanza, "stanza name mismatch")
			// These fields map directly to status: BackupName, BackupType, Size
			assert.Equal(t, tt.expectLabel, backup.Label, "backup.Label populates status.BackupName")
			assert.Equal(t, tt.expectType, backup.Type, "backup.Type populates status.BackupType")
			assert.Equal(t, tt.expectSize, backup.Info.Delta, "backup.Info.Delta populates status.Size")
		})
	}
}

func TestReconcileSkipsUnmanagedCluster(t *testing.T) {
	ctx := t.Context()

	tests := []struct {
		name          string
		unmanaged     bool
		expectedState v2.PGBackupState
	}{
		{
			name:          "unmanaged cluster",
			unmanaged:     true,
			expectedState: v2.BackupNew,
		},
		{
			name:          "managed cluster",
			unmanaged:     false,
			expectedState: v2.BackupStarting,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cluster, err := readDefaultCR("test-cluster", "test-namespace")
			require.NoError(t, err)
			cluster.Spec.Unmanaged = new(tt.unmanaged)

			backup := &v2.PerconaPGBackup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-backup",
					Namespace: cluster.Namespace,
				},
				Spec: v2.PerconaPGBackupSpec{
					PGCluster: cluster.Name,
					RepoName:  new("repo1"),
				},
				Status: v2.PerconaPGBackupStatus{State: v2.BackupNew},
			}

			cl, err := buildFakeClient(ctx, cluster, backup)
			require.NoError(t, err)

			r := &PGBackupReconciler{Client: cl}
			_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(backup)})
			require.NoError(t, err)

			updated := new(v2.PerconaPGBackup)
			require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(backup), updated))
			assert.Equal(t, tt.expectedState, updated.Status.State)

			updatedCluster := new(v2.PerconaPGCluster)
			require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(cluster), updatedCluster))

			leaseErr := cl.Get(ctx, client.ObjectKey{
				Name:      backupLeaseName(cluster.Name),
				Namespace: cluster.Namespace,
			}, new(coordinationv1.Lease))

			if tt.unmanaged {
				assert.Empty(t, updated.Finalizers)
				assert.Empty(t, updated.Status.Conditions)
				assert.NotContains(t, updatedCluster.Annotations, pNaming.AnnotationBackupInProgress)
				assert.NotContains(t, updatedCluster.Annotations, naming.PGBackRestBackup)
				assert.True(t, k8serrors.IsNotFound(leaseErr))
			} else {
				assert.Contains(t, updated.Finalizers, pNaming.FinalizerDeleteBackup)
				assert.True(t, meta.IsStatusConditionTrue(updated.Status.Conditions, v2.ConditionBackupLeaseAcquired))
				assert.Equal(t, backup.Name, updatedCluster.Annotations[pNaming.AnnotationBackupInProgress])
				assert.Equal(t, backup.Name, updatedCluster.Annotations[naming.PGBackRestBackup])
				assert.NoError(t, leaseErr)
			}
		})
	}
}

func TestReconcileRunsFinalizersForUnmanagedCluster(t *testing.T) {
	ctx := t.Context()

	cluster, err := readDefaultCR("test-cluster", "test-namespace")
	require.NoError(t, err)
	cluster.Spec.Unmanaged = new(true)

	backup := &v2.PerconaPGBackup{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test-backup",
			Namespace:  cluster.Namespace,
			Finalizers: []string{pNaming.FinalizerDeleteBackup},
		},
		Spec: v2.PerconaPGBackupSpec{
			PGCluster: cluster.Name,
			RepoName:  new("repo1"),
		},
		Status: v2.PerconaPGBackupStatus{State: v2.BackupRunning},
	}

	cl, err := buildFakeClient(ctx, cluster, backup)
	require.NoError(t, err)
	require.NoError(t, cl.Delete(ctx, backup))

	r := &PGBackupReconciler{Client: cl}
	_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(backup)})
	require.NoError(t, err)

	err = cl.Get(ctx, client.ObjectKeyFromObject(backup), new(v2.PerconaPGBackup))
	assert.True(t, k8serrors.IsNotFound(err))
}
