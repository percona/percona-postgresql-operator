package pgbackup

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	coordinationv1 "k8s.io/api/coordination/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/percona/percona-postgresql-operator/v2/internal/naming"
	pNaming "github.com/percona/percona-postgresql-operator/v2/percona/naming"
	v2 "github.com/percona/percona-postgresql-operator/v2/pkg/apis/pgv2.percona.com/v2"
	"github.com/percona/percona-postgresql-operator/v2/pkg/apis/upstream.pgv2.percona.com/v1beta1"
)

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
				HolderIdentity: ptr.To("other-backup|uid-other"),
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
				HolderIdentity: ptr.To("other-backup|uid-other"),
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
				HolderIdentity: ptr.To("deleted-backup|uid-deleted"),
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
				HolderIdentity: ptr.To("completed-backup|uid-completed"),
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
				HolderIdentity: ptr.To("failed-backup|uid-failed"),
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
				HolderIdentity: ptr.To("backup-1|uid-xyz"),
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
				HolderIdentity: ptr.To("backup-1|uid-1"),
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
				HolderIdentity: ptr.To("backup-1|uid-1"),
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
				HolderIdentity: ptr.To("backup-1|uid-1"),
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
				HolderIdentity: ptr.To("backup-1|uid-1"),
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
				HolderIdentity: ptr.To("other-backup|uid-other"),
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
