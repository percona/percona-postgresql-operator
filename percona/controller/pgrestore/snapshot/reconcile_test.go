package snapshot

import (
	"path"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	v2 "github.com/percona/percona-postgresql-operator/v2/pkg/apis/pgv2.percona.com/v2"
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
