package pgrestore

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/percona/percona-postgresql-operator/v3/internal/naming"
	pNaming "github.com/percona/percona-postgresql-operator/v3/percona/naming"
	v2 "github.com/percona/percona-postgresql-operator/v3/pkg/apis/pgv2.percona.com/v2"
	"github.com/percona/percona-postgresql-operator/v3/pkg/apis/upstream.pgv2.percona.com/v1beta1"
)

func TestReconcileSkipsUnmanagedCluster(t *testing.T) {
	ctx := t.Context()

	tests := []struct {
		name          string
		unmanaged     bool
		expectedState v2.PGRestoreState
	}{
		{
			name:          "unmanaged cluster",
			unmanaged:     true,
			expectedState: v2.RestoreNew,
		},
		{
			name:          "managed cluster",
			unmanaged:     false,
			expectedState: v2.RestoreStarting,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cluster, err := readDefaultCR("test-cluster", "test-namespace")
			require.NoError(t, err)
			cluster.Spec.Unmanaged = new(tt.unmanaged)

			restore := &v2.PerconaPGRestore{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-restore",
					Namespace: cluster.Namespace,
				},
				Spec: v2.PerconaPGRestoreSpec{
					PGCluster: cluster.Name,
					RepoName:  new("repo1"),
				},
				Status: v2.PerconaPGRestoreStatus{State: v2.RestoreNew},
			}

			cl, err := buildFakeClient(ctx, cluster, restore)
			require.NoError(t, err)

			r := &PGRestoreReconciler{Client: cl}
			_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(restore)})
			require.NoError(t, err)

			updated := new(v2.PerconaPGRestore)
			require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(restore), updated))
			assert.Equal(t, tt.expectedState, updated.Status.State)

			updatedCluster := new(v2.PerconaPGCluster)
			require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(cluster), updatedCluster))

			if tt.unmanaged {
				assert.Empty(t, updated.Finalizers)
				assert.NotContains(t, updatedCluster.Annotations, naming.PGBackRestRestore)
				assert.Nil(t, updatedCluster.Spec.Backups.PGBackRest.Restore)
			} else {
				assert.Contains(t, updated.Finalizers, pNaming.FinalizerDeleteRestore)
				assert.Equal(t, restore.Name, updatedCluster.Annotations[naming.PGBackRestRestore])
				require.NotNil(t, updatedCluster.Spec.Backups.PGBackRest.Restore)
				assert.True(t, ptr.Deref(updatedCluster.Spec.Backups.PGBackRest.Restore.Enabled, false))
			}
		})
	}
}

func TestReconcileRunsFinalizersForUnmanagedCluster(t *testing.T) {
	ctx := t.Context()

	cluster, err := readDefaultCR("test-cluster", "test-namespace")
	require.NoError(t, err)
	cluster.Spec.Unmanaged = new(true)

	restore := &v2.PerconaPGRestore{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test-restore",
			Namespace:  cluster.Namespace,
			Finalizers: []string{pNaming.FinalizerDeleteRestore},
		},
		Spec: v2.PerconaPGRestoreSpec{
			PGCluster: cluster.Name,
			RepoName:  new("repo1"),
		},
		Status: v2.PerconaPGRestoreStatus{State: v2.RestoreRunning},
	}

	cl, err := buildFakeClient(ctx, cluster, restore)
	require.NoError(t, err)
	require.NoError(t, cl.Delete(ctx, restore))

	r := &PGRestoreReconciler{Client: cl}
	_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(restore)})
	require.NoError(t, err)

	err = cl.Get(ctx, client.ObjectKeyFromObject(restore), new(v2.PerconaPGRestore))
	assert.True(t, k8serrors.IsNotFound(err), "restore should be deleted after its finalizers ran")

	updatedCluster := new(v2.PerconaPGCluster)
	require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(cluster), updatedCluster))
	require.NotNil(t, updatedCluster.Spec.Backups.PGBackRest.Restore)
	assert.False(t, ptr.Deref(updatedCluster.Spec.Backups.PGBackRest.Restore.Enabled, false))
}

func buildFakeClient(ctx context.Context, cr *v2.PerconaPGCluster, objs ...client.Object) (client.Client, error) {
	s := scheme.Scheme

	if err := v1beta1.AddToScheme(s); err != nil {
		return nil, err
	}
	if err := v2.AddToScheme(s); err != nil {
		return nil, err
	}

	objs = append(objs, cr)
	cr.Default()
	postgresCluster, err := cr.ToCrunchy(ctx, nil, s)
	if err != nil {
		return nil, err
	}
	objs = append(objs, postgresCluster)

	return fake.NewClientBuilder().WithScheme(s).WithObjects(objs...).WithStatusSubresource(objs...).Build(), nil
}

func readDefaultCR(name, namespace string) (*v2.PerconaPGCluster, error) {
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "deploy", "cr.yaml"))
	if err != nil {
		return nil, err
	}

	cr := &v2.PerconaPGCluster{}

	if err := yaml.Unmarshal(data, cr); err != nil {
		return nil, err
	}

	cr.Name = name
	cr.Namespace = namespace
	cr.Status.Postgres.Version = cr.Spec.PostgresVersion
	return cr, nil
}
