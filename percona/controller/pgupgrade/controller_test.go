package pgupgrade

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	pgv2 "github.com/percona/percona-postgresql-operator/v3/pkg/apis/pgv2.percona.com/v2"
	crunchyv1beta1 "github.com/percona/percona-postgresql-operator/v3/pkg/apis/upstream.pgv2.percona.com/v1beta1"
)

func TestReconcileSkipsUnmanagedCluster(t *testing.T) {
	ctx := t.Context()

	tests := []struct {
		name      string
		unmanaged bool
	}{
		{
			name:      "unmanaged cluster",
			unmanaged: true,
		},
		{
			name:      "managed cluster",
			unmanaged: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cluster := &pgv2.PerconaPGCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "test-namespace",
				},
				Spec: pgv2.PerconaPGClusterSpec{
					PostgresVersion: 17,
					Unmanaged:       new(tt.unmanaged),
				},
			}

			upgrade := &pgv2.PerconaPGUpgrade{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-upgrade",
					Namespace: cluster.Namespace,
				},
				Spec: pgv2.PerconaPGUpgradeSpec{
					PostgresClusterName: cluster.Name,
					Image:               new("some-upgrade-image"),
					FromPostgresVersion: 17,
					ToPostgresVersion:   18,
					ToPostgresImage:     "some-postgres-image",
					ToPgBouncerImage:    "some-pgbouncer-image",
					ToPgBackRestImage:   "some-pgbackrest-image",
				},
			}

			cl, err := buildFakeClient(cluster, upgrade)
			require.NoError(t, err)

			r := &PGUpgradeReconciler{Client: cl}
			_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(upgrade)})
			require.NoError(t, err)

			pgUpgrade := new(crunchyv1beta1.PGUpgrade)
			err = cl.Get(ctx, client.ObjectKeyFromObject(upgrade), pgUpgrade)

			if tt.unmanaged {
				assert.True(t, k8serrors.IsNotFound(err), "PGUpgrade should not be created for an unmanaged cluster")
			} else {
				require.NoError(t, err)
				assert.Equal(t, cluster.Name, pgUpgrade.Spec.PostgresClusterName)
			}
		})
	}
}

func buildFakeClient(objs ...client.Object) (client.Client, error) {
	s := scheme.Scheme

	if err := crunchyv1beta1.AddToScheme(s); err != nil {
		return nil, err
	}
	if err := pgv2.AddToScheme(s); err != nil {
		return nil, err
	}

	return fake.NewClientBuilder().WithScheme(s).WithObjects(objs...).WithStatusSubresource(objs...).Build(), nil
}
