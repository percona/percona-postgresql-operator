package pgcluster

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/percona/percona-postgresql-operator/v2/internal/logicalreplica"
	v2 "github.com/percona/percona-postgresql-operator/v2/pkg/apis/pgv2.percona.com/v2"
)

// K8SPG-784

func testLogicalReplicaCluster() *v2.PerconaPGCluster {
	return &v2.PerconaPGCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "cluster1", Namespace: "pg"},
		Spec: v2.PerconaPGClusterSpec{
			CRVersion:       "3.1.0",
			PostgresVersion: 17,
			Port:            new(int32(5432)),
		},
	}
}

func TestLogicalReplicaObjectNames(t *testing.T) {
	cr := testLogicalReplicaCluster()

	assert.Equal(t, "cluster1-lr-analytics", logicalReplicaObjectName(cr, "analytics"))
	assert.Equal(t, "cluster1-lr-analytics-pgdata", logicalReplicaPVCName(cr, "analytics"))
	assert.Equal(t, "cluster1-lr-analytics-bootstrap", logicalReplicaJobName(cr, "analytics"))
	assert.Equal(t, "cluster1-lr-analytics-config", logicalReplicaConfigMapName(cr, "analytics"))
}

func TestLogicalReplicaPostgresConfig(t *testing.T) {
	cr := testLogicalReplicaCluster()

	config := logicalReplicaPostgresConfig(cr, 3)

	// One apply worker and one origin per database, and max_worker_processes has
	// to be strictly greater than the number of databases.
	assert.Contains(t, config, "max_replication_slots = 3")
	assert.Contains(t, config, "max_logical_replication_workers = 3")
	assert.Contains(t, config, "max_worker_processes = 11")

	assert.Contains(t, config, "port = 5432")
	assert.Contains(t, config, "ssl = on")
	assert.Contains(t, config, "ssl_cert_file = '/pgconf/tls/tls.crt'")
	assert.Contains(t, config, "unix_socket_directories = '/tmp/postgres'")
}

func TestLogicalReplicaBootstrapScript(t *testing.T) {
	databases := []string{"cluster1", "reporting"}
	script := logicalReplicaBootstrapScript(
		"/pgdata/pg17", "cluster1-primary.pg.svc", 5432, databases, "analytics")

	t.Run("seeds a physical standby before converting it", func(t *testing.T) {
		// pg_createsubscriber only converts an existing physical standby, and
		// --write-recovery-conf is what makes the seeded directory into one.
		basebackup := strings.Index(script, "pg_basebackup")
		convert := strings.Index(script, "pg_createsubscriber")

		require.NotEqual(t, -1, basebackup)
		require.NotEqual(t, -1, convert)
		assert.Less(t, basebackup, convert)
		assert.Contains(t, script, "--write-recovery-conf")
	})

	t.Run("refuses to overwrite an existing data directory", func(t *testing.T) {
		assert.Contains(t, script, "PG_VERSION")
		assert.Contains(t, script, "refusing to seed over it")
	})

	t.Run("names every replication object explicitly", func(t *testing.T) {
		// --all cannot be combined with --replication-slot, and the operator has
		// to own the slot names so it can drop them again later.
		assert.NotContains(t, script, "--all")

		for _, db := range databases {
			assert.Contains(t, script, "--database='"+db+"'")
			assert.Contains(t, script, "--publication="+logicalreplica.PublicationName("analytics", db))
			assert.Contains(t, script, "--subscription="+logicalreplica.SubscriptionName("analytics", db))
			assert.Contains(t, script, "--replication-slot="+logicalreplica.SlotName("analytics", db))
		}
	})

	t.Run("connects to the primary over verified TLS", func(t *testing.T) {
		assert.Contains(t, script, "user="+logicalreplica.ReplicationUser)
		assert.Contains(t, script, "sslmode=verify-ca")
		assert.Contains(t, script, "host=cluster1-primary.pg.svc")
	})

	t.Run("fails on the first error", func(t *testing.T) {
		assert.Contains(t, script, "set -euo pipefail")
	})
}

func TestShellQuote(t *testing.T) {
	assert.Equal(t, `'plain'`, shellQuote("plain"))
	assert.Equal(t, `'it'\''s'`, shellQuote("it's"))
	assert.Equal(t, `'; rm -rf /'`, shellQuote("; rm -rf /"))
}

func TestQuoteLiteral(t *testing.T) {
	assert.Equal(t, `'plain'`, quoteLiteral("plain"))
	assert.Equal(t, `'it''s'`, quoteLiteral("it's"))
}

func TestLogicalReplicaStatusFor(t *testing.T) {
	cr := testLogicalReplicaCluster()
	cr.Status.LogicalReplicas = []v2.LogicalReplicaStatus{{
		Name:      "analytics",
		State:     v2.LogicalReplicaStateBroken,
		Reason:    v2.LogicalReplicaReasonSourceSlotMissing,
		Message:   "stale",
		Databases: []string{"cluster1"},
	}}

	t.Run("keeps the resolved databases and clears the verdict", func(t *testing.T) {
		// The database list is frozen once resolved, because the slot,
		// publication and subscription names all derive from it.
		status := logicalReplicaStatusFor(cr, "analytics")

		assert.Equal(t, []string{"cluster1"}, status.Databases)
		assert.Empty(t, status.Reason)
		assert.Empty(t, status.Message)
	})

	t.Run("does not alias the recorded status", func(t *testing.T) {
		status := logicalReplicaStatusFor(cr, "analytics")
		status.Databases[0] = "mutated"

		assert.Equal(t, "cluster1", cr.Status.LogicalReplicas[0].Databases[0])
	})

	t.Run("fresh status for an unknown replica", func(t *testing.T) {
		status := logicalReplicaStatusFor(cr, "reporting")

		assert.Equal(t, "reporting", status.Name)
		assert.Equal(t, v2.LogicalReplicaStateBootstrapping, status.State)
		assert.Empty(t, status.Databases)
	})
}
