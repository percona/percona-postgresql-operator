package v2

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/percona/percona-postgresql-operator/v3/percona/version"
	crunchyv1beta1 "github.com/percona/percona-postgresql-operator/v3/pkg/apis/upstream.pgv2.percona.com/v1beta1"
)

func logicalReplicaCR(replicas ...LogicalReplicaSpec) *PerconaPGCluster {
	return &PerconaPGCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "test-cluster", Namespace: "test-namespace"},
		Spec: PerconaPGClusterSpec{
			CRVersion:       version.Version(),
			PostgresVersion: 17,
			InstanceSets: PGInstanceSets{{
				Name:     "instance1",
				Replicas: new(int32(1)),
				DataVolumeClaimSpec: corev1.PersistentVolumeClaimSpec{
					AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
				},
			}},
			Backups: Backups{
				PGBackRest: PGBackRestArchive{
					Repos: []crunchyv1beta1.PGBackRestRepo{{Name: "repo1"}},
				},
			},
			LogicalReplicas: replicas,
		},
	}
}

func TestLogicalReplicasEnabled(t *testing.T) {
	t.Run("disabled when the section is empty", func(t *testing.T) {
		assert.False(t, logicalReplicaCR().LogicalReplicasEnabled())
	})

	t.Run("enabled when a replica is configured", func(t *testing.T) {
		cr := logicalReplicaCR(LogicalReplicaSpec{Name: "analytics"})
		assert.True(t, cr.LogicalReplicasEnabled())
	})

	t.Run("disabled for CRVersion < 3.1.0", func(t *testing.T) {
		cr := logicalReplicaCR(LogicalReplicaSpec{Name: "analytics"})
		cr.Spec.CRVersion = "3.0.0"
		assert.False(t, cr.LogicalReplicasEnabled())
	})
}

func TestBootstrapMethodOrDefault(t *testing.T) {
	t.Run("unset seeds from pgBackRest", func(t *testing.T) {
		// The CRD defaults this, so an empty value only reaches here from a spec
		// the API server has not seen: a unit test, or an object stored before
		// the field existed. Neither may quietly switch method.
		spec := &LogicalReplicaSpec{Name: "analytics"}
		assert.Equal(t, LogicalReplicaBootstrapMethodPGBackRest, spec.BootstrapMethodOrDefault())
	})

	t.Run("an explicit method is kept", func(t *testing.T) {
		for _, method := range []LogicalReplicaBootstrapMethod{
			LogicalReplicaBootstrapMethodPGBackRest,
			LogicalReplicaBootstrapMethodPGBaseBackup,
		} {
			spec := &LogicalReplicaSpec{Name: "analytics", BootstrapMethod: method}
			assert.Equal(t, method, spec.BootstrapMethodOrDefault())
		}
	})
}

func TestValidateLogicalReplicas(t *testing.T) {
	t.Run("empty is valid", func(t *testing.T) {
		require.NoError(t, logicalReplicaCR().ValidateLogicalReplicas())
	})

	t.Run("distinct names are valid", func(t *testing.T) {
		cr := logicalReplicaCR(
			LogicalReplicaSpec{Name: "analytics"},
			LogicalReplicaSpec{Name: "reporting"},
		)
		require.NoError(t, cr.ValidateLogicalReplicas())
	})

	t.Run("rejects duplicate names", func(t *testing.T) {
		cr := logicalReplicaCR(
			LogicalReplicaSpec{Name: "analytics"},
			LogicalReplicaSpec{Name: "analytics"},
		)
		err := cr.ValidateLogicalReplicas()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "duplicate spec.logicalReplicas name")
	})

	t.Run("rejects a name that collides with an instance set", func(t *testing.T) {
		// Both would want the same StatefulSet name.
		cr := logicalReplicaCR(LogicalReplicaSpec{Name: "instance1"})
		err := cr.ValidateLogicalReplicas()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "conflicts with an instance set")
	})

	t.Run("rejects duplicate databases within a replica", func(t *testing.T) {
		cr := logicalReplicaCR(LogicalReplicaSpec{
			Name:      "analytics",
			Databases: []crunchyv1beta1.PostgresIdentifier{"db1", "db1"},
		})
		err := cr.ValidateLogicalReplicas()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "duplicate database")
	})

	t.Run("reached through Validate", func(t *testing.T) {
		cr := logicalReplicaCR(
			LogicalReplicaSpec{Name: "analytics"},
			LogicalReplicaSpec{Name: "analytics"},
		)
		cr.Default()
		require.Error(t, cr.Validate())
	})
}

func TestLogicalReplicasToCrunchy(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, AddToScheme(scheme))
	require.NoError(t, crunchyv1beta1.AddToScheme(scheme))

	t.Run("no logicalrepl user without logical replicas", func(t *testing.T) {
		cr := logicalReplicaCR()
		cr.Default()

		actual, err := cr.ToCrunchy(context.Background(), nil, scheme)
		require.NoError(t, err)

		assert.Empty(t, actual.Spec.LogicalReplicas)
		for _, user := range actual.Spec.Users {
			assert.NotEqual(t, PostgresIdentifierOf(UserLogicalReplication), user.Name)
		}
	})

	t.Run("injects the reserved superuser and mirrors the names", func(t *testing.T) {
		cr := logicalReplicaCR(
			LogicalReplicaSpec{Name: "analytics"},
			LogicalReplicaSpec{Name: "reporting"},
		)
		cr.Default()

		actual, err := cr.ToCrunchy(context.Background(), nil, scheme)
		require.NoError(t, err)

		require.Len(t, actual.Spec.LogicalReplicas, 2)
		assert.Equal(t, "analytics", actual.Spec.LogicalReplicas[0].Name)
		assert.Equal(t, "reporting", actual.Spec.LogicalReplicas[1].Name)

		// pg_createsubscriber creates a publication FOR ALL TABLES, which needs
		// a superuser, and the replica streams from the primary as this role,
		// which needs REPLICATION.
		var found *crunchyv1beta1.PostgresUserSpec
		for i := range actual.Spec.Users {
			if actual.Spec.Users[i].Name == PostgresIdentifierOf(UserLogicalReplication) {
				found = &actual.Spec.Users[i]
			}
		}
		require.NotNil(t, found, "logicalrepl user was not injected")
		assert.Equal(t, "SUPERUSER REPLICATION", found.Options)
		require.NotNil(t, found.Password)
	})

	t.Run("a user-declared logicalrepl is ignored", func(t *testing.T) {
		cr := logicalReplicaCR(LogicalReplicaSpec{Name: "analytics"})
		cr.Spec.Users = []crunchyv1beta1.PostgresUserSpec{{
			Name:    PostgresIdentifierOf(UserLogicalReplication),
			Options: "NOSUPERUSER",
		}}
		cr.Default()

		actual, err := cr.ToCrunchy(context.Background(), nil, scheme)
		require.NoError(t, err)

		count := 0
		for _, user := range actual.Spec.Users {
			if user.Name == PostgresIdentifierOf(UserLogicalReplication) {
				count++
				assert.Equal(t, "SUPERUSER REPLICATION", user.Options)
			}
		}
		assert.Equal(t, 1, count)
	})
}

// PostgresIdentifierOf is a readability helper for the tests above.
func PostgresIdentifierOf(s string) crunchyv1beta1.PostgresIdentifier {
	return crunchyv1beta1.PostgresIdentifier(s)
}
