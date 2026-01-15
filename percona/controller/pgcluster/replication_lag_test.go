package pgcluster

import (
	"context"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/percona/percona-postgresql-operator/v2/internal/controller/postgrescluster"
	pNaming "github.com/percona/percona-postgresql-operator/v2/percona/naming"
	v2 "github.com/percona/percona-postgresql-operator/v2/pkg/apis/pgv2.percona.com/v2"
	crunchyv1beta1 "github.com/percona/percona-postgresql-operator/v2/pkg/apis/postgres-operator.crunchydata.com/v1beta1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

func TestReconcileReplicationLagStatus(t *testing.T) {
	sourceCluster, err := readDefaultCR("source-cluster", "default")
	require.NoError(t, err)
	sourceCluster.Default()
	sourcePrimary := primaryPodForCluster(sourceCluster)

	standbyCluster, err := readDefaultCR("standby-cluster", "default")
	require.NoError(t, err)
	standbyCluster.Default()
	standbyPrimary := primaryPodForCluster(standbyCluster)
	standbyCluster.Spec.Standby = &v2.StandbySpec{
		PostgresStandbySpec: &crunchyv1beta1.PostgresStandbySpec{
			Enabled:  true,
			RepoName: "repo1",
		},
		MaxAcceptableLag: ptr.To(resource.MustParse("1Mi")),
	}
	standbyCluster.Status.State = v2.AppStateReady

	cl, err := buildFakeClient(t.Context(), sourceCluster, standbyCluster, sourcePrimary, standbyPrimary)
	require.NoError(t, err)

	r := &PGClusterReconciler{
		Client: cl,
	}

	t.Run("main site not found", func(t *testing.T) {
		r.PodExec = mockPodExecForLag(0)

		err := r.reconcileReplicationLagStatus(t.Context(), standbyCluster)
		require.NoError(t, err)

		cond := meta.FindStatusCondition(standbyCluster.Status.Conditions, postgrescluster.ConditionReplicationLagDetected)
		assert.NotNil(t, cond)
		assert.Equal(t, metav1.ConditionUnknown, cond.Status)
		assert.Equal(t, "MainSiteNotFound", cond.Reason)
	})

	t.Run("lag not detected", func(t *testing.T) {
		standbyCluster.SetAnnotations(map[string]string{
			pNaming.AnnotationReplicationMainSite: sourceCluster.GetNamespace() + "/" + sourceCluster.GetName(),
		})
		err := r.Client.Update(t.Context(), standbyCluster)
		require.NoError(t, err)

		r.PodExec = mockPodExecForLag(0)

		err = r.reconcileReplicationLagStatus(t.Context(), standbyCluster)
		require.NoError(t, err)

		cond := meta.FindStatusCondition(standbyCluster.Status.Conditions, postgrescluster.ConditionReplicationLagDetected)
		assert.NotNil(t, cond)
		assert.Equal(t, metav1.ConditionFalse, cond.Status)
	})

	t.Run("lag below threshold", func(t *testing.T) {
		now := time.Now()
		now = now.Add(-defaultReplicationLagDetectionInterval)
		standbyCluster.Status.Standby = &v2.StandbyStatus{
			LagLastComputedAt: &metav1.Time{Time: now},
		}
		r.PodExec = mockPodExecForLag(1024)

		err = r.reconcileReplicationLagStatus(t.Context(), standbyCluster)
		require.NoError(t, err)

		cond := meta.FindStatusCondition(standbyCluster.Status.Conditions, postgrescluster.ConditionReplicationLagDetected)
		assert.NotNil(t, cond)
		assert.Equal(t, metav1.ConditionFalse, cond.Status)

		assert.Equal(t, int64(1024), standbyCluster.Status.Standby.LagBytes)
	})

	t.Run("lag above threshold", func(t *testing.T) {
		now := time.Now()
		now = now.Add(-defaultReplicationLagDetectionInterval)
		standbyCluster.Status.Standby = &v2.StandbyStatus{
			LagLastComputedAt: &metav1.Time{Time: now},
		}
		r.PodExec = mockPodExecForLag(3 * 1024 * 1024)

		err = r.reconcileReplicationLagStatus(t.Context(), standbyCluster)
		require.NoError(t, err)

		cond := meta.FindStatusCondition(standbyCluster.Status.Conditions, postgrescluster.ConditionReplicationLagDetected)
		assert.NotNil(t, cond)
		assert.Equal(t, metav1.ConditionTrue, cond.Status)

		assert.Equal(t, int64(3*1024*1024), standbyCluster.Status.Standby.LagBytes)
	})

	t.Run("cluster has caught up", func(t *testing.T) {
		now := time.Now()
		now = now.Add(-defaultReplicationLagDetectionInterval)
		standbyCluster.Status.Standby = &v2.StandbyStatus{
			LagLastComputedAt: &metav1.Time{Time: now},
		}
		r.PodExec = mockPodExecForLag(0)

		err = r.reconcileReplicationLagStatus(t.Context(), standbyCluster)
		require.NoError(t, err)

		cond := meta.FindStatusCondition(standbyCluster.Status.Conditions, postgrescluster.ConditionReplicationLagDetected)
		assert.NotNil(t, cond)
		assert.Equal(t, metav1.ConditionFalse, cond.Status)

		assert.Equal(t, int64(0), standbyCluster.Status.Standby.LagBytes)
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

func mockPodExecForLag(wantLagBytes int64) func(context.Context, string, string, string, io.Reader, io.Writer, io.Writer, ...string) error {
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
