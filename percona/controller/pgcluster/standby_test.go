package pgcluster

import (
	"context"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/percona/percona-postgresql-operator/v2/internal/controller/postgrescluster"
	pNaming "github.com/percona/percona-postgresql-operator/v2/percona/naming"
	v2 "github.com/percona/percona-postgresql-operator/v2/pkg/apis/pgv2.percona.com/v2"
	crunchyv1beta1 "github.com/percona/percona-postgresql-operator/v2/pkg/apis/postgres-operator.crunchydata.com/v1beta1"
)

func TestReconcileStandbyLag(t *testing.T) {
	sourceCluster, err := readDefaultCR("source-cluster", "default")
	require.NoError(t, err)
	sourceCluster.Default()

	standbyCluster, err := readDefaultCR("standby-cluster", "default")
	require.NoError(t, err)
	standbyCluster.Default()
	standbyCluster.Spec.Standby = &v2.StandbySpec{
		PostgresStandbySpec: &crunchyv1beta1.PostgresStandbySpec{
			Enabled:  true,
			RepoName: "repo1",
		},
		MaxAcceptableLag: ptr.To(resource.MustParse("1Mi")),
	}
	standbyCluster.Status.State = v2.AppStateReady
	standbyCluster.SetAnnotations(map[string]string{
		pNaming.AnnotationReplicationMainSite: sourceCluster.GetNamespace() + "/" + sourceCluster.GetName(),
	})

	mockPodExec := func(wantLagBytes int64) func(context.Context, string, string, string, io.Reader, io.Writer, io.Writer, ...string) error {
		return func(ctx context.Context, namespace, pod, container string,
			stdin io.Reader, stdout, stderr io.Writer, command ...string) error {

			if stdin == nil {
				return nil
			}

			sqlB, err := io.ReadAll(stdin)
			if err != nil {
				return err
			}

			query := strings.TrimSpace(string(sqlB))
			if strings.Contains(query, "pg_current_wal_lsn") {
				fmt.Fprint(stdout, "0/12345678\n")
			} else if strings.Contains(query, "pg_wal_lsn_diff") {
				fmt.Fprintf(stdout, "%d\n", wantLagBytes)
			}
			return nil
		}
	}

	newReconciler := func(
		sourceCluster *v2.PerconaPGCluster,
		standbyCluster *v2.PerconaPGCluster,
		sourcePrimary *corev1.Pod,
		standbyPrimary *corev1.Pod,
	) *PGClusterReconciler {
		cl, err := buildFakeClient(t.Context(), sourceCluster, standbyCluster, sourcePrimary, standbyPrimary)
		require.NoError(t, err)

		return &PGClusterReconciler{
			Client: cl,
		}
	}

	t.Run("CRVersion < 2.9.0", func(t *testing.T) {
		cluster := standbyCluster.DeepCopy()
		cluster.Spec.CRVersion = "2.8.0"

		r := newReconciler(
			sourceCluster,
			cluster,
			primaryPodForCluster(sourceCluster),
			primaryPodForCluster(cluster),
		)
		r.PodExec = mockPodExec(0)

		err := r.reconcileStandbyLag(t.Context(), cluster)
		require.NoError(t, err)

		cond := meta.FindStatusCondition(cluster.Status.Conditions, postgrescluster.ConditionStandbyLagging)
		assert.Nil(t, cond)
		assert.Nil(t, cluster.Status.Standby)
	})

	t.Run("Standby not enabled", func(t *testing.T) {
		cluster := standbyCluster.DeepCopy()
		cluster.Spec.Standby.Enabled = false

		r := newReconciler(
			sourceCluster,
			cluster,
			primaryPodForCluster(sourceCluster),
			primaryPodForCluster(cluster),
		)
		r.PodExec = mockPodExec(0)

		err := r.reconcileStandbyLag(t.Context(), cluster)
		require.NoError(t, err)

		cond := meta.FindStatusCondition(cluster.Status.Conditions, postgrescluster.ConditionStandbyLagging)
		assert.Nil(t, cond)
		assert.Nil(t, cluster.Status.Standby)
	})

	t.Run("main site not found", func(t *testing.T) {
		cluster := standbyCluster.DeepCopy()
		cluster.SetAnnotations(map[string]string{})

		r := newReconciler(
			sourceCluster,
			cluster,
			primaryPodForCluster(sourceCluster),
			primaryPodForCluster(cluster),
		)
		r.PodExec = mockPodExec(0)

		err := r.reconcileStandbyLag(t.Context(), cluster)
		require.NoError(t, err)

		cond := meta.FindStatusCondition(cluster.Status.Conditions, postgrescluster.ConditionStandbyLagging)
		assert.NotNil(t, cond)
		assert.Equal(t, metav1.ConditionUnknown, cond.Status)
		assert.Equal(t, "MainSiteNotFound", cond.Reason)
	})

	t.Run("lag not detected", func(t *testing.T) {
		cluster := standbyCluster.DeepCopy()
		r := newReconciler(
			sourceCluster,
			cluster,
			primaryPodForCluster(sourceCluster),
			primaryPodForCluster(cluster),
		)
		r.PodExec = mockPodExec(0)

		err = r.reconcileStandbyLag(t.Context(), cluster)
		require.NoError(t, err)

		cond := meta.FindStatusCondition(cluster.Status.Conditions, postgrescluster.ConditionStandbyLagging)
		assert.NotNil(t, cond)
		assert.Equal(t, metav1.ConditionFalse, cond.Status)

		assert.NotNil(t, cluster.Status.Standby)
		assert.NotNil(t, cluster.Status.Standby.LagLastComputedAt)
		assert.Equal(t, int64(0), cluster.Status.Standby.LagBytes)
	})

	t.Run("lag below threshold", func(t *testing.T) {
		cluster := standbyCluster.DeepCopy()
		r := newReconciler(
			sourceCluster,
			cluster,
			primaryPodForCluster(sourceCluster),
			primaryPodForCluster(standbyCluster),
		)
		r.PodExec = mockPodExec(1024)

		err = r.reconcileStandbyLag(t.Context(), cluster)
		require.NoError(t, err)

		cond := meta.FindStatusCondition(cluster.Status.Conditions, postgrescluster.ConditionStandbyLagging)
		assert.NotNil(t, cond)
		assert.Equal(t, metav1.ConditionFalse, cond.Status)
		assert.Equal(t, int64(1024), cluster.Status.Standby.LagBytes)
	})

	t.Run("lag above threshold", func(t *testing.T) {
		cluster := standbyCluster.DeepCopy()
		now := time.Now()
		now = now.Add(-defaultReplicationLagDetectionInterval)
		cluster.Status.Standby = &v2.StandbyStatus{
			LagLastComputedAt: ptr.To(metav1.Time{Time: now}),
		}
		r := newReconciler(
			sourceCluster,
			cluster,
			primaryPodForCluster(sourceCluster),
			primaryPodForCluster(cluster),
		)
		r.PodExec = mockPodExec(3 * 1024 * 1024)

		err = r.reconcileStandbyLag(t.Context(), cluster)
		require.NoError(t, err)

		cond := meta.FindStatusCondition(cluster.Status.Conditions, postgrescluster.ConditionStandbyLagging)
		assert.NotNil(t, cond)
		assert.Equal(t, metav1.ConditionTrue, cond.Status)
	})
}

func primaryPodForCluster(cluster *v2.PerconaPGCluster) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cluster.Name + "-primary-xyz-0",
			Namespace: cluster.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/instance":             cluster.Name,
				"postgres-operator.crunchydata.com/role": "primary",
			},
		},
	}
}

func TestGetStandbyMainSite(t *testing.T) {
	// Prepare mocks objects
	sourceCluster, err := readDefaultCR("source-cluster", "default")
	require.NoError(t, err)
	sourceCluster.Default()
	sourceCluster.Spec.Backups.PGBackRest.Repos = []crunchyv1beta1.PGBackRestRepo{
		{
			Name: "repo1",
			S3: &crunchyv1beta1.RepoS3{
				Bucket:   "some-bucket",
				Endpoint: "some-endpoint",
				Region:   "some-region",
			},
		},
	}

	standbyCluster, err := readDefaultCR("standby-cluster", "default")
	require.NoError(t, err)
	standbyCluster.Default()
	standbyCluster.Spec.Backups.PGBackRest.Repos = []crunchyv1beta1.PGBackRestRepo{
		{
			Name: "repo1",
			S3: &crunchyv1beta1.RepoS3{
				Bucket:   "some-bucket",
				Endpoint: "some-endpoint",
				Region:   "some-region",
			},
		},
	}
	standbyCluster.Spec.Standby = &v2.StandbySpec{
		PostgresStandbySpec: &crunchyv1beta1.PostgresStandbySpec{
			Enabled:  true,
			RepoName: "repo1",
		},
		MaxAcceptableLag: ptr.To(resource.MustParse("1Mi")),
	}

	t.Run("standby not enabled", func(t *testing.T) {
		standby := standbyCluster.DeepCopy()
		standby.Spec.Standby.Enabled = false

		cl, err := buildFakeClient(t.Context(), standbyCluster, []client.Object{sourceCluster}...)
		require.NoError(t, err)

		r := &PGClusterReconciler{
			Client: cl,
		}

		_, err = r.getStandbyMainSite(t.Context(), standby)
		assert.Error(t, err)
	})

	t.Run("standby repo not specified", func(t *testing.T) {
		standby := standbyCluster.DeepCopy()
		standby.Spec.Standby.RepoName = ""

		cl, err := buildFakeClient(t.Context(), standbyCluster, []client.Object{sourceCluster}...)
		require.NoError(t, err)

		r := &PGClusterReconciler{
			Client: cl,
		}

		_, err = r.getStandbyMainSite(t.Context(), standby)
		assert.Error(t, err)
	})

	t.Run("source cluster with different repo configuration", func(t *testing.T) {
		source := sourceCluster.DeepCopy()
		standby := standbyCluster.DeepCopy()

		source.Spec.Backups.PGBackRest.Repos = []crunchyv1beta1.PGBackRestRepo{
			{
				Name: "repo1",
				S3: &crunchyv1beta1.RepoS3{
					Bucket:   "different-bucket",
					Endpoint: "some-endpoint",
					Region:   "some-region",
				},
			},
		}

		cl, err := buildFakeClient(t.Context(), standby, []client.Object{source}...)
		require.NoError(t, err)

		r := &PGClusterReconciler{
			Client: cl,
		}

		mainSite, err := r.getStandbyMainSite(t.Context(), standby)
		require.NoError(t, err)
		assert.Nil(t, mainSite)
	})

	t.Run("source cluster in same namespace", func(t *testing.T) {
		source := sourceCluster.DeepCopy()
		standby := standbyCluster.DeepCopy()

		cl, err := buildFakeClient(t.Context(), standby, []client.Object{source}...)
		require.NoError(t, err)

		r := &PGClusterReconciler{
			Client: cl,
		}

		mainSite, err := r.getStandbyMainSite(t.Context(), standby)
		require.NoError(t, err)
		assert.NotNil(t, mainSite)
		assert.Equal(t, source.GetNamespace(), mainSite.GetNamespace())
		assert.Equal(t, source.GetName(), mainSite.GetName())
	})

	t.Run("source cluster in different namespace", func(t *testing.T) {
		source := sourceCluster.DeepCopy()
		standby := standbyCluster.DeepCopy()
		source.Namespace = "different-namespace"

		cl, err := buildFakeClient(t.Context(), standby, []client.Object{source}...)
		require.NoError(t, err)

		r := &PGClusterReconciler{
			Client: cl,
		}

		mainSite, err := r.getStandbyMainSite(t.Context(), standby)
		require.NoError(t, err)
		assert.NotNil(t, mainSite)
		assert.Equal(t, "different-namespace", mainSite.GetNamespace())
		assert.Equal(t, source.GetName(), mainSite.GetName())
	})

	t.Run("non-cluster-wide mode", func(t *testing.T) {
		source := sourceCluster.DeepCopy()
		standby := standbyCluster.DeepCopy()

		source.Namespace = "different-namespace"

		cl, err := buildFakeClient(t.Context(), standby, []client.Object{source}...)
		require.NoError(t, err)

		r := &PGClusterReconciler{
			Client:         cl,
			WatchNamespace: []string{"default"},
		}

		mainSite, err := r.getStandbyMainSite(t.Context(), standby)
		require.NoError(t, err)
		assert.Nil(t, mainSite)
	})
}
