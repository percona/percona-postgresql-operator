package pgrestore

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/percona/percona-postgresql-operator/percona/testutils"
	pgv2 "github.com/percona/percona-postgresql-operator/pkg/apis/pgv2.percona.com/v2"
	"github.com/percona/percona-postgresql-operator/pkg/apis/postgres-operator.crunchydata.com/v1beta1"
)

func TestDisableRestore(t *testing.T) {
	tests := []struct {
		name       string
		backupSpec pgv2.Backups
	}{
		{
			"spec.backups.enabled is false",
			pgv2.Backups{
				Enabled:    ptr.To(false),
				PGBackRest: pgv2.PGBackRestArchive{},
			},
		},
		{
			"spec.backups.pgbackrest.restore is nil",
			pgv2.Backups{
				Enabled:    ptr.To(true),
				PGBackRest: pgv2.PGBackRestArchive{},
			},
		},
		{
			"spec.backups.pgbackrest.restore is empty",
			pgv2.Backups{
				Enabled: ptr.To(true),
				PGBackRest: pgv2.PGBackRestArchive{
					Restore: &v1beta1.PGBackRestRestore{
						PostgresClusterDataSource: &v1beta1.PostgresClusterDataSource{},
					},
				},
			},
		},
		{
			"spec.backups.pgbackrest.restore is defined",
			pgv2.Backups{
				Enabled: ptr.To(true),
				PGBackRest: pgv2.PGBackRestArchive{
					Restore: &v1beta1.PGBackRestRestore{
						Enabled: ptr.To(true),
						PostgresClusterDataSource: &v1beta1.PostgresClusterDataSource{
							RepoName:  "repo1",
							Resources: corev1.ResourceRequirements{},
						},
					},
				},
			},
		},
		{
			"spec.backups.pgbackrest.restore is defined, repoName is empty",
			pgv2.Backups{
				Enabled: ptr.To(true),
				PGBackRest: pgv2.PGBackRestArchive{
					Restore: &v1beta1.PGBackRestRestore{
						Enabled: ptr.To(true),
						PostgresClusterDataSource: &v1beta1.PostgresClusterDataSource{
							RepoName:  "",
							Resources: corev1.ResourceRequirements{},
						},
					},
				},
			},
		},
	}

	ctx := context.Background()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pgc := &pgv2.PerconaPGCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "test-ns",
				},
				Spec: pgv2.PerconaPGClusterSpec{
					Backups: tt.backupSpec,
				},
			}

			pgr := &pgv2.PerconaPGRestore{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-restore",
					Namespace: "test-ns",
				},
				Spec: pgv2.PerconaPGRestoreSpec{
					PGCluster: "test-cluster",
					RepoName:  "repo1",
				},
				Status: pgv2.PerconaPGRestoreStatus{
					State: pgv2.RestoreRunning,
				},
			}

			cl := testutils.BuildFakeClient(pgc, pgr)
			err := disableRestore(ctx, cl, pgc, pgr)
			require.NoError(t, err)

			updatedPG := &pgv2.PerconaPGCluster{}
			err = cl.Get(ctx, client.ObjectKeyFromObject(pgc), updatedPG)
			require.NoError(t, err)

			require.NotNil(t, updatedPG.Spec.Backups.PGBackRest.Restore)
			assert.Equal(t, pgc.Spec.Backups.PGBackRest.Restore.RepoName, updatedPG.Spec.Backups.PGBackRest.Restore.RepoName)
			assert.False(t, *updatedPG.Spec.Backups.PGBackRest.Restore.Enabled, "spec.backups.pgbackrest.restore.enabled should be false")

		})
	}

}
