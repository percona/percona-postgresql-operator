package pgcluster

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/percona/percona-postgresql-operator/v2/internal/logicalreplica"
	pNaming "github.com/percona/percona-postgresql-operator/v2/percona/naming"
	v2 "github.com/percona/percona-postgresql-operator/v2/pkg/apis/pgv2.percona.com/v2"
	crunchyv1beta1 "github.com/percona/percona-postgresql-operator/v2/pkg/apis/upstream.pgv2.percona.com/v1beta1"
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

	config := logicalReplicaPostgresConfig(cr, 3, false, "pgaudit,pg_stat_monitor")

	// One apply worker and one origin per database, and max_worker_processes has
	// to be strictly greater than the number of databases.
	assert.Contains(t, config, "max_replication_slots = 3")
	assert.Contains(t, config, "max_logical_replication_workers = 3")
	assert.Contains(t, config, "max_worker_processes = 11")

	assert.Contains(t, config, "port = 5432")
	assert.Contains(t, config, "ssl = on")
	assert.Contains(t, config, "ssl_cert_file = '/pgconf/tls/tls.crt'")
	assert.Contains(t, config, "unix_socket_directories = '/tmp/postgres'")

	t.Run("carries the primary's preloaded libraries", func(t *testing.T) {
		// Without this, every database carrying pgaudit or pg_stat_monitor
		// rejects statements with "must be loaded via
		// shared_preload_libraries", including the DDL that
		// pg_createsubscriber runs. Restated from the running primary so that
		// it holds even for parameters Patroni passes on the command line
		// rather than writing to postgresql.conf.
		assert.Contains(t, config, "shared_preload_libraries = 'pgaudit,pg_stat_monitor'")

		// Nothing is emitted when the primary has none.
		assert.NotContains(t,
			logicalReplicaPostgresConfig(cr, 3, false, ""),
			"shared_preload_libraries")
	})

	t.Run("inherits whatever else the primary's file holds", func(t *testing.T) {
		// The pg-tde key command and custom GUCs come along this way. It is
		// include_if_exists because the file is only there once pg_basebackup
		// has run.
		include := strings.Index(config, "include_if_exists '/pgdata/pg17/postgresql.conf'")
		require.NotEqual(t, -1, include, "config does not include the primary's:\n%s", config)

		// Overrides only win if they come after the include.
		assert.Less(t, include, strings.Index(config, "archive_mode = off"))
		assert.Less(t, include, strings.Index(config, "max_replication_slots"))
		assert.Less(t, include, strings.Index(config, "shared_preload_libraries"))
	})

	t.Run("never archives", func(t *testing.T) {
		// pg_basebackup copies the primary's data directory, so without this the
		// replica inherits the pgBackRest archive_command and pushes WAL from a
		// diverged timeline into the source cluster's stanza.
		assert.Contains(t, config, "archive_mode = off")
		assert.Contains(t, config, "archive_command = ''")
	})

	t.Run("read-only when asked", func(t *testing.T) {
		assert.NotContains(t, config, "default_transaction_read_only")
		assert.Contains(t,
			logicalReplicaPostgresConfig(cr, 3, true, "pgaudit"),
			"default_transaction_read_only = on")
	})

	t.Run("the conversion config is writable", func(t *testing.T) {
		// pg_createsubscriber creates subscriptions, advances origins and drops
		// publications on the target, so this one must not be read-only.
		boot := logicalReplicaBootstrapConfig(cr, 3, "pgaudit,pg_stat_monitor")

		assert.NotContains(t, boot, "default_transaction_read_only")
		assert.Contains(t, boot, "archive_mode = off")
		// pg_createsubscriber issues DROP PUBLICATION and CREATE SUBSCRIPTION
		// against every converted database, so it needs the preloaded
		// libraries just as much as the running replica does.
		assert.Contains(t, boot, "shared_preload_libraries = 'pgaudit,pg_stat_monitor'")
		// pg_createsubscriber checks these on the target before it starts.
		assert.Contains(t, boot, "max_replication_slots = 3")
		assert.Contains(t, boot, "max_logical_replication_workers = 3")
		assert.Contains(t, boot, "max_worker_processes = 11")
	})
}

func TestLogicalReplicaBootstrapScript(t *testing.T) {
	databases := []string{"cluster1", "reporting"}
	script := logicalReplicaBootstrapScript(
		"/pgdata/pg17", "cluster1-primary.pg.svc", 5432, databases, "analytics")

	// commandAt locates a command by its own line, so that a mention of it in a
	// comment does not count as the invocation.
	commandAt := func(t *testing.T, name string) int {
		t.Helper()
		i := strings.Index(script, "\n"+name)
		require.NotEqual(t, -1, i, "%s is never invoked", name)
		return i
	}

	t.Run("seeds a physical standby before converting it", func(t *testing.T) {
		// pg_createsubscriber only converts an existing physical standby, and
		// --write-recovery-conf is what makes the seeded directory into one.
		assert.Less(t, commandAt(t, "pg_basebackup"), commandAt(t, "createsubscriber --dry-run"))
		assert.Contains(t, script, "--write-recovery-conf")
	})

	t.Run("validates before doing anything irreversible", func(t *testing.T) {
		// Once pg_createsubscriber promotes the target the data directory is
		// neither a standby nor a subscriber, and the only way forward is to
		// seed it again. The dry run runs the same prerequisite checks without
		// promoting, so a settings or connectivity problem costs a retry
		// instead of a re-seed.
		dry := commandAt(t, "createsubscriber --dry-run")
		real := commandAt(t, "createsubscriber\n")

		assert.Less(t, dry, real, "the dry run must come first")

		// Both go through one definition, so they cannot drift apart.
		assert.Equal(t, 1, strings.Count(script, "pg_createsubscriber \\"))
		assert.Contains(t, script, `"$@"`)
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

	t.Run("the apply worker can authenticate after the job is gone", func(t *testing.T) {
		// pg_createsubscriber stores the publisher connection string verbatim in
		// pg_subscription, and the apply worker reuses it from inside the
		// replica's postmaster, which has none of the Job's environment. Without
		// a credential reachable from the connection string itself the worker
		// fails with "no password supplied".
		assert.Contains(t, script, "passfile=/pgdata/.pgpass")

		// The file has to be written, outside PGDATA so pg_basebackup and
		// pg_resetwal leave it alone, and unreadable to anyone else or libpq
		// silently ignores it.
		assert.Contains(t, script, "umask 0077")
		assert.Contains(t, script, "> '/pgdata/.pgpass'")
		assert.Contains(t, script, "chmod 0600 '/pgdata/.pgpass'")

		// The password itself must not be baked into the connection string, or
		// it would end up in pg_subscription and in pg_dump output.
		assert.NotContains(t, script, "password=")

		assert.Less(t, commandAt(t, "printf"), commandAt(t, "createsubscriber --dry-run"),
			"passfile must be written before the conversion")
	})

	t.Run("converts with the operator's config, not the primary's", func(t *testing.T) {
		// pg_createsubscriber promotes the target before it resets the system
		// identifier. Left on the primary's inherited config, the target would
		// archive WAL into the source cluster's pgBackRest stanza during that
		// window.
		assert.Contains(t, script, "--config-file=/etc/logical-replica/bootstrap.conf")
	})
}

func TestLogicalReplicaPassFile(t *testing.T) {
	// Beside the data directory, never inside it: pg_basebackup demands an
	// empty target and pg_createsubscriber runs pg_resetwal over what it finds.
	assert.Equal(t, "/pgdata/.pgpass", logicalReplicaPassFile("/pgdata/pg17"))
	assert.Equal(t, "/pgdata/.pgpass", logicalReplicaPassFile("/pgdata/pg18"))
}

func TestParseSubscriptionHealth(t *testing.T) {
	for _, tt := range []struct {
		name        string
		stdout      string
		wantEnabled bool
		wantRunning bool
		wantErr     bool
	}{
		{name: "enabled and running", stdout: " t | t", wantEnabled: true, wantRunning: true},
		{name: "enabled but no worker", stdout: " t | f", wantEnabled: true},
		{name: "disabled", stdout: " f | f"},
		{name: "subscription is gone", stdout: ""},
		{name: "only whitespace", stdout: "   \n "},
		{name: "unexpected shape", stdout: "t", wantErr: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			enabled, running, err := parseSubscriptionHealth(tt.stdout)

			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantEnabled, enabled)
			assert.Equal(t, tt.wantRunning, running)
		})
	}
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

func TestGenerateLogicalReplicaBootstrapJob(t *testing.T) {
	ctx := context.Background()

	cr := testLogicalReplicaCluster()
	cr.Spec.PostgresVersion = 17
	// Pinning the image keeps k8s.InitImage from having to look up the
	// operator Pod, which does not exist under the fake client.
	cr.Spec.InitContainer = &crunchyv1beta1.InitContainerSpec{Image: "operator:test"}
	cr.Spec.InstanceSets = v2.PGInstanceSets{{
		Name:     "instance1",
		Replicas: new(int32(1)),
		DataVolumeClaimSpec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
		},
	}}

	cl, err := buildFakeClient(ctx, cr)
	require.NoError(t, err)
	r := &PGClusterReconciler{Client: cl}

	spec := &v2.LogicalReplicaSpec{Name: "analytics"}
	status := &v2.LogicalReplicaStatus{Name: "analytics", Databases: []string{"cluster1"}}

	job, err := r.generateLogicalReplicaBootstrapJob(ctx, cr, spec, status)
	require.NoError(t, err)

	podSpec := job.Spec.Template.Spec

	t.Run("injects the operator init container", func(t *testing.T) {
		// K8SPG-708: the same container instance pods get, so the bootstrap Job
		// has the shared scripts available under /opt/crunchy/bin.
		require.Len(t, podSpec.InitContainers, 1)

		init := podSpec.InitContainers[0]
		assert.Equal(t, "database-init", init.Name)
		assert.Equal(t, "operator:test", init.Image)
		assert.Equal(t, []string{"/usr/local/bin/init-entrypoint.sh"}, init.Command)

		require.Len(t, init.VolumeMounts, 1)
		assert.Equal(t, pNaming.CrunchyBinVolumeName, init.VolumeMounts[0].Name)
		assert.Equal(t, pNaming.CrunchyBinVolumePath, init.VolumeMounts[0].MountPath)
	})

	t.Run("the bootstrap container can reach the scripts", func(t *testing.T) {
		require.Len(t, podSpec.Containers, 1)

		var mount *corev1.VolumeMount
		for i := range podSpec.Containers[0].VolumeMounts {
			if podSpec.Containers[0].VolumeMounts[i].Name == pNaming.CrunchyBinVolumeName {
				mount = &podSpec.Containers[0].VolumeMounts[i]
			}
		}
		require.NotNil(t, mount, "bootstrap container does not mount the scripts volume")
		assert.Equal(t, pNaming.CrunchyBinVolumePath, mount.MountPath)
	})

	t.Run("the shared volume exists on the pod", func(t *testing.T) {
		var volume *corev1.Volume
		for i := range podSpec.Volumes {
			if podSpec.Volumes[i].Name == pNaming.CrunchyBinVolumeName {
				volume = &podSpec.Volumes[i]
			}
		}
		require.NotNil(t, volume, "scripts volume is missing from the pod")
		assert.NotNil(t, volume.EmptyDir)
	})

	t.Run("never retries", func(t *testing.T) {
		// A half-seeded data directory cannot be re-used: the script refuses to
		// run over it, so a retry would only burn time.
		require.NotNil(t, job.Spec.BackoffLimit)
		assert.Equal(t, int32(0), *job.Spec.BackoffLimit)
		assert.Equal(t, corev1.RestartPolicyNever, podSpec.RestartPolicy)
	})
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
