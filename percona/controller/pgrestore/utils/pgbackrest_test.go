package utils

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	v2 "github.com/percona/percona-postgresql-operator/v3/pkg/apis/pgv2.percona.com/v2"
	"github.com/percona/percona-postgresql-operator/v3/pkg/apis/upstream.pgv2.percona.com/v1beta1"
)

func TestPGBackRestRestoreStartTolerations(t *testing.T) {
	namespace := "test-ns"
	name := "test-cluster"
	repoName := "repo1"

	jobTolerations := []corev1.Toleration{{
		Key:      "backup-jobs",
		Operator: corev1.TolerationOpExists,
		Effect:   corev1.TaintEffectNoSchedule,
	}}
	restoreTolerations := []corev1.Toleration{{
		Key:      "restore",
		Operator: corev1.TolerationOpExists,
		Effect:   corev1.TaintEffectNoSchedule,
	}}

	t.Run("defaults to backup job tolerations", func(t *testing.T) {
		pgCluster := &v2.PerconaPGCluster{
			TypeMeta:   metav1.TypeMeta{APIVersion: v2.GroupVersion.String(), Kind: "PerconaPGCluster"},
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
			Spec: v2.PerconaPGClusterSpec{
				Backups: v2.Backups{PGBackRest: v2.PGBackRestArchive{
					Jobs: &v1beta1.BackupJobs{Tolerations: jobTolerations},
				}},
			},
		}
		postgresCluster := &v1beta1.PostgresCluster{
			TypeMeta:   metav1.TypeMeta{APIVersion: v1beta1.GroupVersion.String(), Kind: "PostgresCluster"},
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		}
		pgRestore := &v2.PerconaPGRestore{
			ObjectMeta: metav1.ObjectMeta{Name: "restore", Namespace: namespace},
			Spec: v2.PerconaPGRestoreSpec{
				PGCluster: name,
				RepoName:  new(repoName),
			},
		}

		cl := buildRestoreTestClient(t, pgCluster, postgresCluster)
		require.NoError(t, NewPGBackRestRestore(cl, pgCluster, pgRestore).Start(t.Context()))

		updated := &v2.PerconaPGCluster{}
		require.NoError(t, cl.Get(t.Context(), types.NamespacedName{Name: name, Namespace: namespace}, updated))
		require.Equal(t, jobTolerations, updated.Spec.Backups.PGBackRest.Restore.Tolerations)
	})

	t.Run("preserves restore tolerations", func(t *testing.T) {
		pgCluster := &v2.PerconaPGCluster{
			TypeMeta:   metav1.TypeMeta{APIVersion: v2.GroupVersion.String(), Kind: "PerconaPGCluster"},
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
			Spec: v2.PerconaPGClusterSpec{
				Backups: v2.Backups{PGBackRest: v2.PGBackRestArchive{
					Jobs: &v1beta1.BackupJobs{Tolerations: jobTolerations},
					Restore: &v1beta1.PGBackRestRestore{
						PostgresClusterDataSource: &v1beta1.PostgresClusterDataSource{
							Tolerations: restoreTolerations,
						},
					},
				}},
			},
		}
		postgresCluster := &v1beta1.PostgresCluster{
			TypeMeta:   metav1.TypeMeta{APIVersion: v1beta1.GroupVersion.String(), Kind: "PostgresCluster"},
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		}
		pgRestore := &v2.PerconaPGRestore{
			ObjectMeta: metav1.ObjectMeta{Name: "restore", Namespace: namespace},
			Spec: v2.PerconaPGRestoreSpec{
				PGCluster: name,
				RepoName:  new(repoName),
			},
		}

		cl := buildRestoreTestClient(t, pgCluster, postgresCluster)
		require.NoError(t, NewPGBackRestRestore(cl, pgCluster, pgRestore).Start(t.Context()))

		updated := &v2.PerconaPGCluster{}
		require.NoError(t, cl.Get(t.Context(), types.NamespacedName{Name: name, Namespace: namespace}, updated))
		require.Equal(t, restoreTolerations, updated.Spec.Backups.PGBackRest.Restore.Tolerations)
	})
}

func buildRestoreTestClient(t *testing.T, pgCluster *v2.PerconaPGCluster,
	postgresCluster *v1beta1.PostgresCluster,
) client.Client {
	t.Helper()

	scheme := runtime.NewScheme()
	require.NoError(t, v2.AddToScheme(scheme))
	require.NoError(t, v1beta1.AddToScheme(scheme))

	return fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(pgCluster, postgresCluster).
		WithStatusSubresource(postgresCluster).
		Build()
}
