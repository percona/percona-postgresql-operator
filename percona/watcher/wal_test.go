package watcher

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/percona/percona-postgresql-operator/v2/percona/version"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/percona/percona-postgresql-operator/v2/percona/testutils"
	pgv2 "github.com/percona/percona-postgresql-operator/v2/pkg/apis/pgv2.percona.com/v2"
)

func mustParseTime(layout string, value string) time.Time {
	time, err := time.Parse(layout, value)
	if err != nil {
		panic(err)
	}
	return time
}

func TestGetLatestBackup(t *testing.T) {
	tests := []struct {
		name             string
		backups          []client.Object
		latestBackupName string
		expectedErr      error
	}{
		{
			name:             "no backups",
			backups:          []client.Object{},
			latestBackupName: "",
			expectedErr:      errNoBackups,
		},
		{
			name: "single backup",
			backups: []client.Object{
				&pgv2.PerconaPGBackup{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "backup1",
						Namespace: "test-ns",
						CreationTimestamp: metav1.Time{
							Time: mustParseTime(time.RFC3339, "2024-02-04T21:00:57Z"),
						},
					},
					Spec: pgv2.PerconaPGBackupSpec{
						PGCluster: "test-cluster",
					},
					Status: pgv2.PerconaPGBackupStatus{
						State: pgv2.BackupSucceeded,
						CompletedAt: &metav1.Time{
							Time: mustParseTime(time.RFC3339, "2024-02-04T21:23:38Z"),
						},
					},
				},
			},
			latestBackupName: "backup1",
			expectedErr:      nil,
		},
		{
			name: "multiple backups, same cluster",
			backups: []client.Object{
				&pgv2.PerconaPGBackup{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "backup1",
						Namespace: "test-ns",
						CreationTimestamp: metav1.Time{
							Time: mustParseTime(time.RFC3339, "2024-02-04T21:00:57Z"),
						},
					},
					Spec: pgv2.PerconaPGBackupSpec{
						PGCluster: "test-cluster",
					},
					Status: pgv2.PerconaPGBackupStatus{
						State: pgv2.BackupSucceeded,
						CompletedAt: &metav1.Time{
							Time: mustParseTime(time.RFC3339, "2024-02-04T21:23:38Z"),
						},
					},
				},
				&pgv2.PerconaPGBackup{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "backup2",
						Namespace: "test-ns",
						CreationTimestamp: metav1.Time{
							Time: mustParseTime(time.RFC3339, "2024-02-04T22:00:57Z"),
						},
					},
					Spec: pgv2.PerconaPGBackupSpec{
						PGCluster: "test-cluster",
					},
					Status: pgv2.PerconaPGBackupStatus{
						State: pgv2.BackupSucceeded,
						CompletedAt: &metav1.Time{
							Time: mustParseTime(time.RFC3339, "2024-02-04T22:24:12Z"),
						},
					},
				},
			},
			latestBackupName: "backup2",
			expectedErr:      nil,
		},
		{
			name: "multiple backups, different clusters",
			backups: []client.Object{
				&pgv2.PerconaPGBackup{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "backup1",
						Namespace: "test-ns",
						CreationTimestamp: metav1.Time{
							Time: mustParseTime(time.RFC3339, "2024-02-04T21:00:57Z"),
						},
					},
					Spec: pgv2.PerconaPGBackupSpec{
						PGCluster: "test-cluster",
					},
					Status: pgv2.PerconaPGBackupStatus{
						State: pgv2.BackupSucceeded,
						CompletedAt: &metav1.Time{
							Time: mustParseTime(time.RFC3339, "2024-02-04T21:23:38Z"),
						},
					},
				},
				&pgv2.PerconaPGBackup{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "backup2",
						Namespace: "test-ns",
						CreationTimestamp: metav1.Time{
							Time: mustParseTime(time.RFC3339, "2024-02-04T22:00:57Z"),
						},
					},
					Spec: pgv2.PerconaPGBackupSpec{
						PGCluster: "test-cluster",
					},
					Status: pgv2.PerconaPGBackupStatus{
						State: pgv2.BackupSucceeded,
						CompletedAt: &metav1.Time{
							Time: mustParseTime(time.RFC3339, "2024-02-04T22:24:12Z"),
						},
					},
				},
				&pgv2.PerconaPGBackup{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "backup-from-different-cluster",
						Namespace: "test-ns",
						CreationTimestamp: metav1.Time{
							Time: mustParseTime(time.RFC3339, "2024-02-04T22:10:57Z"),
						},
					},
					Spec: pgv2.PerconaPGBackupSpec{
						PGCluster: "different-cluster",
					},
					Status: pgv2.PerconaPGBackupStatus{
						State: pgv2.BackupSucceeded,
						CompletedAt: &metav1.Time{
							Time: mustParseTime(time.RFC3339, "2024-02-04T22:14:12Z"),
						},
					},
				},
			},
			latestBackupName: "backup2",
			expectedErr:      nil,
		},
		{
			name: "single running backup",
			backups: []client.Object{
				&pgv2.PerconaPGBackup{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "backup1",
						Namespace: "test-ns",
						CreationTimestamp: metav1.Time{
							Time: mustParseTime(time.RFC3339, "2024-02-04T21:00:57Z"),
						},
					},
					Spec: pgv2.PerconaPGBackupSpec{
						PGCluster: "test-cluster",
					},
					Status: pgv2.PerconaPGBackupStatus{
						State: pgv2.BackupRunning,
					},
				},
			},
			latestBackupName: "",
			expectedErr:      errRunningBackup,
		},
		{
			name: "running backup but a backup is already succeeded",
			backups: []client.Object{
				&pgv2.PerconaPGBackup{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "backup1",
						Namespace: "test-ns",
						CreationTimestamp: metav1.Time{
							Time: mustParseTime(time.RFC3339, "2024-02-04T21:00:57Z"),
						},
					},
					Spec: pgv2.PerconaPGBackupSpec{
						PGCluster: "test-cluster",
					},
					Status: pgv2.PerconaPGBackupStatus{
						State: pgv2.BackupSucceeded,
						CompletedAt: &metav1.Time{
							Time: mustParseTime(time.RFC3339, "2024-02-04T21:23:38Z"),
						},
					},
				},
				&pgv2.PerconaPGBackup{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "backup2",
						Namespace: "test-ns",
						CreationTimestamp: metav1.Time{
							Time: mustParseTime(time.RFC3339, "2024-02-04T22:00:57Z"),
						},
					},
					Spec: pgv2.PerconaPGBackupSpec{
						PGCluster: "test-cluster",
					},
					Status: pgv2.PerconaPGBackupStatus{
						State: pgv2.BackupRunning,
					},
				},
			},
			latestBackupName: "backup1",
			expectedErr:      nil,
		},
		{
			name: "K8SPG-772: multiple backups, some has no CompletedAt",
			backups: []client.Object{
				&pgv2.PerconaPGBackup{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "backup1",
						Namespace: "test-ns",
						CreationTimestamp: metav1.Time{
							Time: mustParseTime(time.RFC3339, "2024-02-04T21:00:57Z"),
						},
					},
					Spec: pgv2.PerconaPGBackupSpec{
						PGCluster: "test-cluster",
					},
					Status: pgv2.PerconaPGBackupStatus{
						State: pgv2.BackupSucceeded,
						CompletedAt: &metav1.Time{
							Time: mustParseTime(time.RFC3339, "2024-02-04T21:24:12Z"),
						},
					},
				},
				&pgv2.PerconaPGBackup{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "backup2",
						Namespace: "test-ns",
						CreationTimestamp: metav1.Time{
							Time: mustParseTime(time.RFC3339, "2024-02-04T22:00:57Z"),
						},
					},
					Spec: pgv2.PerconaPGBackupSpec{
						PGCluster: "test-cluster",
					},
					Status: pgv2.PerconaPGBackupStatus{
						State: pgv2.BackupSucceeded,
					},
				},
				&pgv2.PerconaPGBackup{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "backup3",
						Namespace: "test-ns",
						CreationTimestamp: metav1.Time{
							Time: mustParseTime(time.RFC3339, "2024-02-04T23:00:57Z"),
						},
					},
					Spec: pgv2.PerconaPGBackupSpec{
						PGCluster: "test-cluster",
					},
					Status: pgv2.PerconaPGBackupStatus{
						State: pgv2.BackupSucceeded,
						CompletedAt: &metav1.Time{
							Time: mustParseTime(time.RFC3339, "2024-02-04T23:24:12Z"),
						},
					},
				},
			},
			latestBackupName: "backup3",
			expectedErr:      nil,
		},
	}

	ctx := context.Background()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := testutils.BuildFakeClient(tt.backups...)

			cluster := &pgv2.PerconaPGCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "test-ns",
				},
			}

			latest, err := getLatestBackup(ctx, client, cluster)
			if tt.expectedErr != nil {
				require.EqualError(t, err, tt.expectedErr.Error())
				assert.Nil(t, latest)
			} else {
				require.NoError(t, err)
				assert.NotNil(t, latest)
				assert.Equal(t, latest.Name, tt.latestBackupName)
			}
		})
	}
}

func TestGetLatestCommitTimestamp(t *testing.T) {
	ctx := context.Background()

	tests := map[string]struct {
		pods        []client.Object
		backup      *pgv2.PerconaPGBackup
		cluster     *pgv2.PerconaPGCluster
		expectedErr error
	}{
		"primary pod not found due to invalid patroni version": {
			pods: []client.Object{},
			backup: &pgv2.PerconaPGBackup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "backup1",
					Namespace: "test-ns",
				},
				Spec: pgv2.PerconaPGBackupSpec{
					PGCluster: "test-cluster",
					RepoName:  "repo1",
				},
			},
			cluster: &pgv2.PerconaPGCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "test-ns",
				},
				Status: pgv2.PerconaPGClusterStatus{
					Patroni: pgv2.Patroni{
						Version: "error",
					},
				},
				Spec: pgv2.PerconaPGClusterSpec{
					CRVersion: version.Version(),
				},
			},
			expectedErr: errors.New("failed to get patroni version: Malformed version: error: primary pod not found"),
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			c := testutils.BuildFakeClient(tt.pods...)

			_, err := GetLatestCommitTimestamp(ctx, c, nil, tt.cluster, tt.backup)

			assert.EqualError(t, err, tt.expectedErr.Error())
		})
	}
}
