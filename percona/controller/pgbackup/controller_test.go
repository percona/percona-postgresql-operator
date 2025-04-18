package pgbackup

import (
	"context"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/percona/percona-postgresql-operator/internal/naming"
	pNaming "github.com/percona/percona-postgresql-operator/percona/naming"
	v2 "github.com/percona/percona-postgresql-operator/pkg/apis/pgv2.percona.com/v2"
	"github.com/percona/percona-postgresql-operator/pkg/apis/postgres-operator.crunchydata.com/v1beta1"
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
