package pgcluster

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/percona/percona-postgresql-operator/v3/internal/logicalreplica"
	"github.com/percona/percona-postgresql-operator/v3/internal/naming"
	"github.com/percona/percona-postgresql-operator/v3/internal/pgbackrest"
	pNaming "github.com/percona/percona-postgresql-operator/v3/percona/naming"
	v2 "github.com/percona/percona-postgresql-operator/v3/pkg/apis/pgv2.percona.com/v2"
	crunchyv1beta1 "github.com/percona/percona-postgresql-operator/v3/pkg/apis/upstream.pgv2.percona.com/v1beta1"
)

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
	cc := &crunchyv1beta1.PostgresCluster{}
	cc.Spec.PostgresVersion = 17

	config := logicalReplicaPostgresConfig(cr, cc, 3, false /* read only */)

	// One apply worker and one origin per database, and max_worker_processes has
	// to be strictly greater than the number of databases.
	assert.Contains(t, config, "max_replication_slots = 3")
	assert.Contains(t, config, "max_logical_replication_workers = 3")
	assert.Contains(t, config, "max_worker_processes = 11")

	assert.Contains(t, config, "port = 5432")
	assert.Contains(t, config, "ssl = on")
	assert.Contains(t, config, "ssl_cert_file = '/pgconf/tls/tls.crt'")
	assert.Contains(t, config, "unix_socket_directories = '/tmp/postgres'")

	t.Run("inherits whatever the primary's file holds", func(t *testing.T) {
		// shared_preload_libraries above all: without it every database carrying
		// pgaudit or pg_stat_monitor rejects statements with "must be loaded via
		// shared_preload_libraries", including the DDL that pg_createsubscriber
		// runs. The pg-tde key command and custom GUCs come along the same way,
		// which is why the file is inherited rather than restated.
		//
		// It is include_if_exists because the file is only there once the
		// restore has run.
		include := strings.Index(config, "include_if_exists '/pgdata/pg17/postgresql.conf'")
		require.NotEqual(t, -1, include, "config does not include the primary's:\n%s", config)

		assert.NotContains(t, config, "shared_preload_libraries",
			"restating this would drop whatever the primary preloads")

		// Overrides only win if they come after the include.
		assert.Less(t, include, strings.Index(config, "archive_mode = off"))
		assert.Less(t, include, strings.Index(config, "max_replication_slots"))
	})

	t.Run("never archives", func(t *testing.T) {
		// The restore brings back the primary's data directory, so without this
		// the replica inherits the pgBackRest archive_command and pushes WAL
		// from a diverged timeline into the source cluster's stanza.
		assert.Contains(t, config, "archive_mode = off")
		assert.Contains(t, config, "archive_command = ''")
	})

	t.Run("the conversion config is writable", func(t *testing.T) {
		// pg_createsubscriber creates subscriptions, advances origins and drops
		// publications on the target, so this one must not be read-only.
		assert.NotContains(t, config, "default_transaction_read_only")
	})

	t.Run("read-only when asked", func(t *testing.T) {
		assert.Contains(t,
			logicalReplicaPostgresConfig(cr, cc, 3, true /* read only */),
			"default_transaction_read_only = on")
	})
}

func TestLogicalReplicaBootstrapScript(t *testing.T) {
	databases := []string{"cluster1", "reporting"}
	script := logicalReplicaBootstrapScript(
		"/pgdata/pg17", "cluster1-primary.pg.svc", 5432, databases, "analytics",
		"echo \"restoring the data directory from pgBackRest\"\n"+
			"pgbackrest restore --stanza=db --pg1-path=/pgdata/pg17 --repo=1 --type=standby")

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
		// "--type=standby" is what makes the restored directory into one.
		assert.Less(t, commandAt(t, "pgbackrest restore"), commandAt(t, "createsubscriber --dry-run"))
		assert.Contains(t, script, "--type=standby")

		// pgBackRest leaves a restore_command but cannot know where the primary
		// is. Without this the target replays only what has been archived, and
		// the conversion waits out its whole --recovery-timeout for the segment
		// holding the LSN it recovers to.
		assert.Contains(t, script, "primary_conninfo")
		assert.Less(t, commandAt(t, "pgbackrest restore"), strings.Index(script, "primary_conninfo"))
	})

	t.Run("never starts Postgres before the conversion", func(t *testing.T) {
		// pg_createsubscriber starts the target, replays it to the LSN it reads
		// from the publisher and stops it again. Finishing recovery here first
		// would end recovery and promote, and the conversion refuses a target
		// that is no longer a standby.
		assert.NotContains(t, script, "pg_is_in_recovery")
		assert.Less(t, commandAt(t, "createsubscriber\n"), commandAt(t, "pg_ctl start"))
	})

	t.Run("disables the new subscriptions on their first apply error", func(t *testing.T) {
		// Set here or never: the option cannot be given to pg_createsubscriber,
		// and the replica's own postmaster starts applying as soon as it comes
		// up. Left unset, an apply error is retried forever behind a
		// subscription that still reports itself enabled.
		for _, db := range databases {
			assert.Contains(t, script,
				`--command='`+logicalreplica.DisableOnErrorSQL("analytics", db)+`'`)
		}

		// Between a start and a matching clean stop, both of which the
		// conversion has to have finished with first.
		start := commandAt(t, "pg_ctl start")
		stop := commandAt(t, "pg_ctl stop")
		assert.Less(t, start, commandAt(t, "psql "))
		assert.Less(t, commandAt(t, "psql "), stop)

		// The target is a promoted standalone primary by now. On the config it
		// inherited from the primary it would archive WAL from a diverged
		// timeline into the source cluster's stanza.
		assert.Contains(t, script[start:stop],
			"-c config_file=/etc/logical-replica/bootstrap.conf")

		// No apply worker may run before the ALTERs above, and nothing outside
		// this container may connect: the Job's pod carries the labels the
		// replica's Service selects on.
		assert.Contains(t, script[start:stop], "-c max_logical_replication_workers=0")
		assert.Contains(t, script[start:stop], "-c listen_addresses=''")
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

	t.Run("the connection string carries no credential", func(t *testing.T) {
		// pg_createsubscriber stores the publisher connection string verbatim in
		// pg_subscription, where anyone who can read the catalog sees it and
		// pg_dump copies it out. The password reaches libpq through PGPASSWORD,
		// which logicalReplicaEnvironment puts on both the Job and the
		// StatefulSet the apply worker later runs in.
		assert.NotContains(t, script, "password=")
		assert.NotContains(t, script, "passfile")
		assert.NotContains(t, script, ".pgpass")
	})

	t.Run("the connection string is built before anything connects", func(t *testing.T) {
		assert.Less(t, commandAt(t, "PUBLISHER_CONNINFO="),
			commandAt(t, "createsubscriber --dry-run"))
	})

	t.Run("converts with the operator's config, not the primary's", func(t *testing.T) {
		// pg_createsubscriber promotes the target before it resets the system
		// identifier. Left on the primary's inherited config, the target would
		// archive WAL into the source cluster's pgBackRest stanza during that
		// window.
		assert.Contains(t, script, "--config-file='/etc/logical-replica/bootstrap.conf'")
	})
}

func TestLogicalReplicaRestoreOptions(t *testing.T) {
	cc := &crunchyv1beta1.PostgresCluster{}
	cc.Spec.PostgresVersion = 17

	t.Run("needs a repository", func(t *testing.T) {
		_, err := logicalReplicaRestoreOptions(cc, "/pgdata/pg17")
		require.Error(t, err)
	})

	cc.Spec.Backups.PGBackRest.Repos = []crunchyv1beta1.PGBackRestRepo{{Name: "repo2"}}

	t.Run("restores a standby from the cluster's own repository", func(t *testing.T) {
		opts, err := logicalReplicaRestoreOptions(cc, "/pgdata/pg17")
		require.NoError(t, err)

		assert.Contains(t, opts, "--stanza=db")
		assert.Contains(t, opts, "--pg1-path=/pgdata/pg17")
		assert.Contains(t, opts, "--repo=2")

		// pg_createsubscriber converts a target that is still in recovery, so
		// the restore must not be one that promotes.
		assert.Contains(t, opts, "--type=standby")

		// Nothing to remap when the instances keep WAL on the data volume too.
		assert.NotContains(t, strings.Join(opts, " "), "--link-map")
	})

	t.Run("brings pg_wal back inside the data directory", func(t *testing.T) {
		// A logical replica has no separate WAL volume, so pg_wal is a link in
		// the backup that has nowhere to point on this pod.
		cc.Spec.InstanceSets = []crunchyv1beta1.PostgresInstanceSetSpec{{
			Name:               "instance1",
			WALVolumeClaimSpec: &corev1.PersistentVolumeClaimSpec{},
		}}

		opts, err := logicalReplicaRestoreOptions(cc, "/pgdata/pg17")
		require.NoError(t, err)

		assert.Contains(t, opts, "--link-map=pg_wal=/pgdata/pg17/pg_wal")
	})
}

func TestLogicalReplicaSeedCommand(t *testing.T) {
	withRepo := func() *crunchyv1beta1.PostgresCluster {
		cc := &crunchyv1beta1.PostgresCluster{}
		cc.Spec.PostgresVersion = 17
		cc.Spec.Backups.PGBackRest.Repos = []crunchyv1beta1.PGBackRestRepo{{Name: "repo1"}}
		return cc
	}

	t.Run("defaults to pgbackrest", func(t *testing.T) {
		// The field carries a CRD default, but a spec the API server has not
		// defaulted must not silently pick the other method.
		unset, err := logicalReplicaSeedCommand(withRepo(), &v2.LogicalReplicaSpec{}, "/pgdata/pg17")
		require.NoError(t, err)

		explicit, err := logicalReplicaSeedCommand(withRepo(), &v2.LogicalReplicaSpec{
			BootstrapMethod: v2.LogicalReplicaBootstrapMethodPGBackRest,
		}, "/pgdata/pg17")
		require.NoError(t, err)

		assert.Equal(t, explicit, unset)
		assert.Contains(t, unset, "pgbackrest restore")
	})

	t.Run("pgbackrest restores a standby", func(t *testing.T) {
		seed, err := logicalReplicaSeedCommand(withRepo(), &v2.LogicalReplicaSpec{
			BootstrapMethod: v2.LogicalReplicaBootstrapMethodPGBackRest,
		}, "/pgdata/pg17")
		require.NoError(t, err)

		assert.Contains(t, seed, "pgbackrest restore")
		assert.Contains(t, seed, "--type=standby")
		assert.NotContains(t, seed, "pg_basebackup")
	})

	t.Run("pgbackrest needs a repository", func(t *testing.T) {
		cc := &crunchyv1beta1.PostgresCluster{}
		cc.Spec.PostgresVersion = 17

		_, err := logicalReplicaSeedCommand(cc, &v2.LogicalReplicaSpec{
			BootstrapMethod: v2.LogicalReplicaBootstrapMethodPGBackRest,
		}, "/pgdata/pg17")
		require.Error(t, err)

		// The message has to name the way out, because this is the only error a
		// user hits by turning backups off.
		assert.Contains(t, err.Error(), "pg_basebackup")
	})

	t.Run("pg_basebackup needs no repository", func(t *testing.T) {
		// The whole point of the method: a cluster with backups disabled has no
		// repository at all, and used to be unable to host a logical replica.
		cc := &crunchyv1beta1.PostgresCluster{}
		cc.Spec.PostgresVersion = 17

		seed, err := logicalReplicaSeedCommand(cc, &v2.LogicalReplicaSpec{
			BootstrapMethod: v2.LogicalReplicaBootstrapMethodPGBaseBackup,
		}, "/pgdata/pg17")
		require.NoError(t, err)

		assert.Contains(t, seed, "pg_basebackup")
		assert.NotContains(t, seed, "pgbackrest")
	})

	t.Run("pg_basebackup leaves a standby behind", func(t *testing.T) {
		seed, err := logicalReplicaSeedCommand(withRepo(), &v2.LogicalReplicaSpec{
			BootstrapMethod: v2.LogicalReplicaBootstrapMethodPGBaseBackup,
		}, "/pgdata/pg17")
		require.NoError(t, err)

		// pg_createsubscriber converts nothing but a standby, and this is what
		// "pgbackrest restore --type=standby" writes on the other path.
		assert.Contains(t, seed, "touch '/pgdata/pg17/standby.signal'")

		// --write-recovery-conf would write the signal file too, but it derives
		// primary_conninfo from libpq, which has resolved PGPASSWORD by then.
		// The password would land in postgresql.auto.conf in plain text and
		// outlive the Job.
		assert.NotContains(t, seed, "--write-recovery-conf")
		assert.NotContains(t, seed, "-R")
	})

	t.Run("pg_basebackup reuses the one connection string", func(t *testing.T) {
		seed, err := logicalReplicaSeedCommand(withRepo(), &v2.LogicalReplicaSpec{
			BootstrapMethod: v2.LogicalReplicaBootstrapMethodPGBaseBackup,
		}, "/pgdata/pg17")
		require.NoError(t, err)

		assert.Contains(t, seed, `--dbname="${PUBLISHER_CONNINFO}"`)
		assert.Contains(t, seed, "--pgdata='/pgdata/pg17'")

		// Nothing here can prompt, and nothing can stall: there is no terminal,
		// and a spread checkpoint would hold the backup for checkpoint_timeout.
		assert.Contains(t, seed, "--no-password")
		assert.Contains(t, seed, "--checkpoint=fast")

		// No restore_command on this path, so the WAL written during the backup
		// has to ship over the second connection.
		assert.Contains(t, seed, "--wal-method=stream")
	})

	t.Run("pg_basebackup leaves nothing behind on the primary", func(t *testing.T) {
		seed, err := logicalReplicaSeedCommand(withRepo(), &v2.LogicalReplicaSpec{
			BootstrapMethod: v2.LogicalReplicaBootstrapMethodPGBaseBackup,
		}, "/pgdata/pg17")
		require.NoError(t, err)

		// Without --slot, "--wal-method=stream" uses a temporary slot the primary
		// drops with the connection. A named one would survive a failed Job and
		// pin WAL on the primary for good.
		assert.NotContains(t, seed, "--slot")
		assert.NotContains(t, seed, "--create-slot")
	})

	t.Run("pg_basebackup keeps pg_wal on the data volume", func(t *testing.T) {
		// A logical replica has one volume. The server sends pg_wal as an empty
		// directory even when the primary keeps it elsewhere, so this needs no
		// counterpart to the pgBackRest --link-map.
		cc := withRepo()
		cc.Spec.InstanceSets = []crunchyv1beta1.PostgresInstanceSetSpec{{
			Name:               "instance1",
			WALVolumeClaimSpec: &corev1.PersistentVolumeClaimSpec{},
		}}

		seed, err := logicalReplicaSeedCommand(cc, &v2.LogicalReplicaSpec{
			BootstrapMethod: v2.LogicalReplicaBootstrapMethodPGBaseBackup,
		}, "/pgdata/pg17")
		require.NoError(t, err)

		assert.NotContains(t, seed, "--waldir")
		assert.NotContains(t, seed, "--link-map")
	})

	t.Run("pg_tde needs its own basebackup", func(t *testing.T) {
		// Plain pg_basebackup does not understand what pg_tde encrypts, which is
		// why Patroni is told the same thing for creating a physical replica.
		cc := withRepo()
		cc.Spec.Extensions.PGTDE.Enabled = true

		seed, err := logicalReplicaSeedCommand(cc, &v2.LogicalReplicaSpec{
			BootstrapMethod: v2.LogicalReplicaBootstrapMethodPGBaseBackup,
		}, "/pgdata/pg17")
		require.NoError(t, err)

		assert.Contains(t, seed, "pg_tde_basebackup")
		assert.NotContains(t, seed, "\npg_basebackup")
	})

	t.Run("rejects a method it does not know", func(t *testing.T) {
		_, err := logicalReplicaSeedCommand(withRepo(), &v2.LogicalReplicaSpec{
			BootstrapMethod: "pg_dump",
		}, "/pgdata/pg17")

		require.Error(t, err)
		assert.Contains(t, err.Error(), "pg_dump")
	})
}

func TestLogicalReplicaBootstrapScriptWithBaseBackup(t *testing.T) {
	cc := &crunchyv1beta1.PostgresCluster{}
	cc.Spec.PostgresVersion = 17

	seed, err := logicalReplicaSeedCommand(cc, &v2.LogicalReplicaSpec{
		BootstrapMethod: v2.LogicalReplicaBootstrapMethodPGBaseBackup,
	}, "/pgdata/pg17")
	require.NoError(t, err)

	script := logicalReplicaBootstrapScript(
		"/pgdata/pg17", "cluster1-primary.pg.svc", 5432, []string{"cluster1"}, "analytics", seed)

	at := func(t *testing.T, needle string) int {
		t.Helper()
		i := strings.Index(script, needle)
		require.NotEqual(t, -1, i, "%s is missing from the script", needle)
		return i
	}

	t.Run("seeds, marks the standby, then converts", func(t *testing.T) {
		assert.Less(t, at(t, "\npg_basebackup"), at(t, "touch '/pgdata/pg17/standby.signal'"))
		assert.Less(t, at(t, "touch '/pgdata/pg17/standby.signal'"), at(t, "primary_conninfo"))
		assert.Less(t, at(t, "primary_conninfo"), at(t, "\ncreatesubscriber --dry-run"))
	})

	t.Run("the connection string is set before the seed connects", func(t *testing.T) {
		// pg_basebackup authenticates as the same role over the same conninfo,
		// and would expand it to the empty string otherwise.
		assert.Less(t, at(t, "\nPUBLISHER_CONNINFO="), at(t, "\npg_basebackup"))
	})

	t.Run("the password never reaches the data directory", func(t *testing.T) {
		// primary_conninfo lands in postgresql.auto.conf, so anything the
		// conninfo carries outlives the Job. pg_basebackup authenticates from
		// PGPASSWORD instead.
		assert.NotContains(t, script, "password=")
		assert.NotContains(t, script, "passfile")
	})
}

func TestLogicalReplicaCapacity(t *testing.T) {
	basebackup := &v2.LogicalReplicaSpec{
		BootstrapMethod: v2.LogicalReplicaBootstrapMethodPGBaseBackup,
	}

	t.Run("pgbackrest needs one of each per database", func(t *testing.T) {
		slots, senders := logicalReplicaCapacity(&v2.LogicalReplicaSpec{}, 3)

		assert.Equal(t, 3, slots)
		assert.Equal(t, 3, senders)
	})

	t.Run("pg_basebackup needs two WAL senders at once", func(t *testing.T) {
		// One for the base backup, one for the WAL stream. A single-database
		// replica would otherwise pass this check with one sender free and die
		// mid-seed, after a full copy of the cluster.
		slots, senders := logicalReplicaCapacity(basebackup, 1)

		assert.Equal(t, 1, slots)
		assert.Equal(t, 2, senders)
	})

	t.Run("the seed and the subscriptions never overlap", func(t *testing.T) {
		// The primary drops the temporary slot and both senders well before
		// pg_createsubscriber creates the per-database logical slots, so this is
		// a floor rather than something to add on top.
		slots, senders := logicalReplicaCapacity(basebackup, 3)

		assert.Equal(t, 3, slots)
		assert.Equal(t, 3, senders)
	})
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

func TestGenerateLogicalReplicaBootstrapJob(t *testing.T) {
	ctx := context.Background()

	cr := testLogicalReplicaCluster()
	cr.Spec.PostgresVersion = 17
	cr.Spec.InstanceSets = v2.PGInstanceSets{{
		Name:     "instance1",
		Replicas: new(int32(1)),
		DataVolumeClaimSpec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
		},
	}}

	crunchyCR := &crunchyv1beta1.PostgresCluster{
		ObjectMeta: metav1.ObjectMeta{Name: cr.Name, Namespace: cr.Namespace},
	}
	crunchyCR.Spec.PostgresVersion = 17
	// Pinning the image keeps k8s.InitImage from having to look up the
	// operator Pod, which does not exist under the fake client.
	crunchyCR.Spec.InitContainer = &crunchyv1beta1.InitContainerSpec{Image: "operator:test"}
	// The Job restores from the cluster's own repository.
	crunchyCR.Spec.Backups.PGBackRest.Repos = []crunchyv1beta1.PGBackRestRepo{{Name: "repo1"}}

	cl, err := buildFakeClient(ctx, cr)
	require.NoError(t, err)
	r := &PGClusterReconciler{Client: cl}

	spec := &v2.LogicalReplicaSpec{Name: "analytics"}
	status := &v2.LogicalReplicaStatus{Name: "analytics", Databases: []string{"cluster1"}}

	job, err := r.generateLogicalReplicaBootstrapJob(ctx, cr, crunchyCR, spec, status)
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

	t.Run("carries the replication password", func(t *testing.T) {
		require.Len(t, podSpec.Containers, 1)

		// The conninfo the script builds holds no credential of its own, so
		// pg_createsubscriber and pg_basebackup reach the primary on this alone.
		assertLogicalReplicaPassword(t, cr, podSpec.Containers[0].Env)
	})
}

// assertLogicalReplicaPassword checks that env resolves PGPASSWORD from the
// logicalrepl user Secret. Both the bootstrap Job and the replica's StatefulSet
// depend on it: see logicalReplicaEnvironment.
func assertLogicalReplicaPassword(t *testing.T, cr *v2.PerconaPGCluster, env []corev1.EnvVar) {
	t.Helper()

	var found *corev1.EnvVar
	for i := range env {
		if env[i].Name == "PGPASSWORD" {
			found = &env[i]
		}
	}
	require.NotNil(t, found, "PGPASSWORD is missing")

	require.NotNil(t, found.ValueFrom)
	require.NotNil(t, found.ValueFrom.SecretKeyRef)
	assert.Equal(t, logicalReplicaUserSecretName(cr), found.ValueFrom.SecretKeyRef.Name)
	assert.Equal(t, "password", found.ValueFrom.SecretKeyRef.Key)

	// Never the literal: it would be readable in the Pod spec by anyone who can
	// list Pods, whether or not they can read the Secret.
	assert.Empty(t, found.Value)
}

func TestReconcileLogicalReplicaStatefulSet(t *testing.T) {
	ctx := context.Background()

	cr := testLogicalReplicaCluster()
	crunchyCR := &crunchyv1beta1.PostgresCluster{
		ObjectMeta: metav1.ObjectMeta{Name: cr.Name, Namespace: cr.Namespace},
	}
	crunchyCR.Spec.PostgresVersion = 17

	cl, err := buildFakeClient(ctx, cr)
	require.NoError(t, err)
	r := &PGClusterReconciler{Client: cl}

	spec := &v2.LogicalReplicaSpec{Name: "analytics"}
	require.NoError(t, r.reconcileLogicalReplicaStatefulSet(ctx, cr, crunchyCR, spec))

	sts := &appsv1.StatefulSet{}
	require.NoError(t, cl.Get(ctx, client.ObjectKey{
		Name: logicalReplicaObjectName(cr, spec.Name), Namespace: cr.Namespace,
	}, sts))

	require.Len(t, sts.Spec.Template.Spec.Containers, 1)

	// The load-bearing one. pg_createsubscriber stored a conninfo with no
	// credential in pg_subscription, and the apply worker runs in this
	// postmaster and resolves the password out of its environment. Without this
	// the replica bootstraps and then never replicates.
	assertLogicalReplicaPassword(t, cr, sts.Spec.Template.Spec.Containers[0].Env)
}

func TestGenerateLogicalReplicaBootstrapJobPGBackRestConfig(t *testing.T) {
	ctx := context.Background()

	// hasPGBackRestConfig reports whether the Job both projects the pgBackRest
	// configuration and mounts it. The two travel together: a mount with no
	// volume makes the Pod invalid, and a volume with no mount hands the
	// repository credentials to a container with no use for them.
	hasPGBackRestConfig := func(t *testing.T, job *batchv1.Job) (volume, mount bool) {
		t.Helper()
		name := pgbackrest.ConfigVolumeMount().Name

		podSpec := job.Spec.Template.Spec
		for i := range podSpec.Volumes {
			if podSpec.Volumes[i].Name == name {
				volume = true
			}
		}
		require.Len(t, podSpec.Containers, 1)
		for _, m := range podSpec.Containers[0].VolumeMounts {
			if m.Name == name {
				mount = true
			}
		}
		return volume, mount
	}

	generate := func(t *testing.T, repos int, method v2.LogicalReplicaBootstrapMethod) *batchv1.Job {
		t.Helper()

		cr := testLogicalReplicaCluster()
		cr.Spec.PostgresVersion = 17

		crunchyCR := &crunchyv1beta1.PostgresCluster{
			ObjectMeta: metav1.ObjectMeta{Name: cr.Name, Namespace: cr.Namespace},
		}
		crunchyCR.Spec.PostgresVersion = 17
		crunchyCR.Spec.InitContainer = &crunchyv1beta1.InitContainerSpec{Image: "operator:test"}
		for i := range repos {
			crunchyCR.Spec.Backups.PGBackRest.Repos = append(
				crunchyCR.Spec.Backups.PGBackRest.Repos,
				crunchyv1beta1.PGBackRestRepo{Name: "repo" + strconv.Itoa(i+1)})
		}

		cl, err := buildFakeClient(ctx, cr)
		require.NoError(t, err)
		r := &PGClusterReconciler{Client: cl}

		job, err := r.generateLogicalReplicaBootstrapJob(ctx, cr, crunchyCR,
			&v2.LogicalReplicaSpec{Name: "analytics", BootstrapMethod: method},
			&v2.LogicalReplicaStatus{Name: "analytics", Databases: []string{"cluster1"}})
		require.NoError(t, err)

		return job
	}

	t.Run("projected when the cluster has a repository", func(t *testing.T) {
		volume, mount := hasPGBackRestConfig(t, generate(t, 1, v2.LogicalReplicaBootstrapMethodPGBackRest))

		assert.True(t, volume)
		assert.True(t, mount)
	})

	t.Run("projected for pg_basebackup too", func(t *testing.T) {
		// This follows the repository rather than the method: the seed brings the
		// primary's postgresql.conf along, and the pgBackRest restore_command in
		// it is a useful fallback for WAL the primary has already recycled.
		volume, mount := hasPGBackRestConfig(t, generate(t, 1, v2.LogicalReplicaBootstrapMethodPGBaseBackup))

		assert.True(t, volume)
		assert.True(t, mount)
	})

	t.Run("absent when the cluster keeps no backups", func(t *testing.T) {
		// The ConfigMap does not exist, and the projection is not optional, so
		// leaving it in would hang the Pod unschedulable rather than fail it.
		volume, mount := hasPGBackRestConfig(t, generate(t, 0, v2.LogicalReplicaBootstrapMethodPGBaseBackup))

		assert.False(t, volume)
		assert.False(t, mount)
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

// logicalReplicaUserSecret is the secret the bootstrap Job reads its password
// from, which observePrimaryReadiness waits for.
func logicalReplicaUserSecret(cr *v2.PerconaPGCluster) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      logicalReplicaUserSecretName(cr),
			Namespace: cr.Namespace,
		},
		Data: map[string][]byte{"password": []byte("secret")},
	}
}

func TestObservePrimaryReadiness(t *testing.T) {
	newCluster := func() *v2.PerconaPGCluster {
		cr, err := readDefaultCR("cluster1", "pg")
		require.NoError(t, err)
		cr.Default()
		cr.Spec.CRVersion = "3.1.0"
		cr.Status.State = v2.AppStateReady
		return cr
	}

	// Records whether the primary was queried at all: the cheap checks must
	// short-circuit before paying for an exec.
	type execRecorder struct {
		called bool
		query  string
	}

	newReconciler := func(t *testing.T, cr *v2.PerconaPGCluster, rec *execRecorder,
		stdout string, execErr error, objs ...client.Object,
	) *PGClusterReconciler {
		cl, err := buildFakeClient(t.Context(), cr, objs...)
		require.NoError(t, err)

		return &PGClusterReconciler{
			Client: cl,
			PodExec: func(_ context.Context, _, _, _ string,
				stdin io.Reader, out, _ io.Writer, _ ...string,
			) error {
				rec.called = true
				if stdin != nil {
					sql, err := io.ReadAll(stdin)
					require.NoError(t, err)
					rec.query = string(sql)
				}
				if execErr != nil {
					return execErr
				}
				_, err := fmt.Fprintln(out, stdout)
				return err
			},
		}
	}

	t.Run("no primary pod", func(t *testing.T) {
		cr := newCluster()
		rec := new(execRecorder)
		r := newReconciler(t, cr, rec, "", nil, logicalReplicaUserSecret(cr))

		cond := r.observePrimaryReadiness(t.Context(), cr)

		assert.Equal(t, metav1.ConditionFalse, cond.Status)
		assert.Equal(t, "PrimaryPodNotFound", cond.Reason)
		assert.False(t, rec.called, "must not exec without a primary")
	})

	t.Run("replication secret missing", func(t *testing.T) {
		cr := newCluster()
		rec := new(execRecorder)
		r := newReconciler(t, cr, rec, "", nil, primaryPodForCluster(cr))

		cond := r.observePrimaryReadiness(t.Context(), cr)

		assert.Equal(t, metav1.ConditionFalse, cond.Status)
		assert.Equal(t, "ReplicationSecretMissing", cond.Reason)
		assert.Contains(t, cond.Message, logicalReplicaUserSecretName(cr))
		assert.False(t, rec.called, "must not exec before the secret exists")
	})

	t.Run("secret present but empty", func(t *testing.T) {
		cr := newCluster()
		secret := logicalReplicaUserSecret(cr)
		secret.Data = nil
		rec := new(execRecorder)
		r := newReconciler(t, cr, rec, "", nil, primaryPodForCluster(cr), secret)

		cond := r.observePrimaryReadiness(t.Context(), cr)

		assert.Equal(t, "ReplicationSecretMissing", cond.Reason)
		assert.False(t, rec.called)
	})

	t.Run("patroni reports a pending restart", func(t *testing.T) {
		cr := newCluster()
		pod := primaryPodForCluster(cr)
		// The annotation Patroni writes, and the signal handlePatroniRestarts
		// acts on to bounce the primary.
		pod.Annotations = map[string]string{"status": `{"role":"primary","pending_restart":true}`}

		rec := new(execRecorder)
		r := newReconciler(t, cr, rec, "", nil, pod, logicalReplicaUserSecret(cr))

		cond := r.observePrimaryReadiness(t.Context(), cr)

		assert.Equal(t, metav1.ConditionFalse, cond.Status)
		assert.Equal(t, "RestartPending", cond.Reason)
		assert.False(t, rec.called, "a pending restart is decided before the query")
	})

	t.Run("primary is ready", func(t *testing.T) {
		cr := newCluster()
		rec := new(execRecorder)
		r := newReconciler(t, cr, rec, "", nil,
			primaryPodForCluster(cr), logicalReplicaUserSecret(cr))

		cond := r.observePrimaryReadiness(t.Context(), cr)

		assert.Equal(t, metav1.ConditionTrue, cond.Status)
		assert.Equal(t, "PrimaryReady", cond.Reason)
		assert.Equal(t, pNaming.ConditionReadyForLogicalReplication, cond.Type)
		// Left for meta.SetStatusCondition to stamp, so a reason that moves on
		// its own does not look like a transition.
		assert.True(t, cond.LastTransitionTime.IsZero())

		assert.True(t, rec.called)
		assert.Equal(t, logicalreplica.PrimaryReadinessQuery(), rec.query)
	})

	t.Run("one unmet prerequisite", func(t *testing.T) {
		cr := newCluster()
		rec := new(execRecorder)
		r := newReconciler(t, cr, rec, "ReplicationHBAMissing", nil,
			primaryPodForCluster(cr), logicalReplicaUserSecret(cr))

		cond := r.observePrimaryReadiness(t.Context(), cr)

		assert.Equal(t, metav1.ConditionFalse, cond.Status)
		assert.Equal(t, "ReplicationHBAMissing", cond.Reason)
		assert.Equal(t,
			logicalreplica.PrimaryReadinessMessage("ReplicationHBAMissing"),
			cond.Message)
	})

	t.Run("several unmet prerequisites", func(t *testing.T) {
		cr := newCluster()
		rec := new(execRecorder)
		r := newReconciler(t, cr, rec, "RestartPending,ReplicationHBAMissing", nil,
			primaryPodForCluster(cr), logicalReplicaUserSecret(cr))

		cond := r.observePrimaryReadiness(t.Context(), cr)

		// The reason is the first, but the message has to name all of them: the
		// user fixes them together.
		assert.Equal(t, "RestartPending", cond.Reason)
		assert.Contains(t, cond.Message,
			logicalreplica.PrimaryReadinessMessage("RestartPending"))
		assert.Contains(t, cond.Message,
			logicalreplica.PrimaryReadinessMessage("ReplicationHBAMissing"))
	})

	t.Run("primary cannot be queried", func(t *testing.T) {
		cr := newCluster()
		rec := new(execRecorder)
		r := newReconciler(t, cr, rec, "", errors.New("connection refused"),
			primaryPodForCluster(cr), logicalReplicaUserSecret(cr))

		cond := r.observePrimaryReadiness(t.Context(), cr)

		// Unknown rather than False: the operator does not know, and a bootstrap
		// still must not start.
		assert.Equal(t, metav1.ConditionUnknown, cond.Status)
		assert.Equal(t, "PrimaryUnreachable", cond.Reason)
		assert.Contains(t, cond.Message, "connection refused")
	})
}

func TestReconcileLogicalReplicaGatesBootstrap(t *testing.T) {
	cr, err := readDefaultCR("cluster1", "pg")
	require.NoError(t, err)
	cr.Default()
	cr.Spec.CRVersion = "3.1.0"
	cr.Status.State = v2.AppStateReady

	spec := &v2.LogicalReplicaSpec{Name: "analytics"}

	cl, err := buildFakeClient(t.Context(), cr)
	require.NoError(t, err)
	r := &PGClusterReconciler{Client: cl}

	crunchyCR := &crunchyv1beta1.PostgresCluster{}
	crunchyCR.Spec.PostgresVersion = 17

	status, err := r.reconcileLogicalReplica(t.Context(), cr, crunchyCR, spec, false)
	require.NoError(t, err)

	assert.Equal(t, v2.LogicalReplicaStateBootstrapping, status.State)
	assert.Equal(t, v2.LogicalReplicaReasonPrimaryNotReady, status.Reason)
	assert.Contains(t, status.Message, pNaming.ConditionReadyForLogicalReplication)

	// The gate sits ahead of everything, so nothing at all was created - not the
	// Job, and not the volume or config it would need. Resolving the databases
	// would have queried the primary, and PodExec is nil here: that this did not
	// panic is itself the assertion.
	unwanted := map[string]client.Object{
		logicalReplicaJobName(cr, spec.Name):       &batchv1.Job{},
		logicalReplicaPVCName(cr, spec.Name):       &corev1.PersistentVolumeClaim{},
		logicalReplicaConfigMapName(cr, spec.Name): &corev1.ConfigMap{},
	}
	for name, obj := range unwanted {
		err := cl.Get(t.Context(), client.ObjectKey{Name: name, Namespace: cr.Namespace}, obj)
		assert.True(t, apierrors.IsNotFound(err), "%s should not exist: %v", name, err)
	}
}

// TestReconcileLogicalReplicaWaitsForDatabases covers the way a cluster that has
// no databases yet used to be reported. The list is frozen for the lifetime of
// the replica, so resolving it too early is permanent: an empty list left the
// replica broken, and a database named in the spec that had still to be created
// was seeded anyway and failed the one bootstrap attempt the Job gets.
func TestReconcileLogicalReplicaWaitsForDatabases(t *testing.T) {
	newCluster := func(t *testing.T) *v2.PerconaPGCluster {
		cr, err := readDefaultCR("cluster1", "pg")
		require.NoError(t, err)
		cr.Default()
		cr.Spec.CRVersion = "3.1.0"
		cr.Status.State = v2.AppStateReady
		cr.Spec.Port = new(int32(5432))
		return cr
	}

	// The databases the primary reports, whatever it is asked. Both queries this
	// covers read one name per line.
	primaryWith := func(databases ...string) func(context.Context, string, string, string,
		io.Reader, io.Writer, io.Writer, ...string) error {
		return func(_ context.Context, _, _, _ string,
			_ io.Reader, out, _ io.Writer, _ ...string,
		) error {
			for _, database := range databases {
				if _, err := fmt.Fprintln(out, database); err != nil {
					return err
				}
			}
			return nil
		}
	}

	// Nothing may be created while a replica is waiting: the bootstrap Job gets
	// one attempt, and running it against the wrong set of databases spends it.
	assertNothingCreated := func(t *testing.T, cl client.Client, cr *v2.PerconaPGCluster, replica string) {
		t.Helper()
		for name, obj := range map[string]client.Object{
			logicalReplicaJobName(cr, replica):       &batchv1.Job{},
			logicalReplicaPVCName(cr, replica):       &corev1.PersistentVolumeClaim{},
			logicalReplicaConfigMapName(cr, replica): &corev1.ConfigMap{},
		} {
			err := cl.Get(t.Context(), client.ObjectKey{Name: name, Namespace: cr.Namespace}, obj)
			assert.True(t, apierrors.IsNotFound(err), "%s should not exist: %v", name, err)
		}
	}

	t.Run("waits for the operator to create the databases", func(t *testing.T) {
		cr := newCluster(t)
		spec := &v2.LogicalReplicaSpec{Name: "analytics"}

		cl, err := buildFakeClient(t.Context(), cr, primaryPodForCluster(cr))
		require.NoError(t, err)

		// No databaseRevision: the PostgresCluster controller has not finished a
		// create pass, so the list on the primary may be empty or half of what it
		// is about to be. PodExec is nil here - that this did not panic is the
		// assertion that the primary was not queried at all.
		crunchyCR := &crunchyv1beta1.PostgresCluster{}
		crunchyCR.Spec.PostgresVersion = 17

		r := &PGClusterReconciler{Client: cl}
		status, err := r.reconcileLogicalReplica(t.Context(), cr, crunchyCR, spec, true)
		require.NoError(t, err)

		assert.Equal(t, v2.LogicalReplicaStateBootstrapping, status.State)
		assert.Equal(t, v2.LogicalReplicaReasonWaitingForDatabases, status.Reason)
		assert.Empty(t, status.Databases)
		assertNothingCreated(t, cl, cr, spec.Name)
	})

	t.Run("waits when the cluster has no databases of its own", func(t *testing.T) {
		cr := newCluster(t)
		spec := &v2.LogicalReplicaSpec{Name: "analytics"}

		cl, err := buildFakeClient(t.Context(), cr, primaryPodForCluster(cr))
		require.NoError(t, err)

		crunchyCR := &crunchyv1beta1.PostgresCluster{}
		crunchyCR.Spec.PostgresVersion = 17
		crunchyCR.Status.DatabaseRevision = "abc123"

		r := &PGClusterReconciler{Client: cl}
		// The query already filters out templates and "postgres", so a cluster
		// with no databases of its own answers with nothing.
		r.PodExec = primaryWith()

		status, err := r.reconcileLogicalReplica(t.Context(), cr, crunchyCR, spec, true)
		require.NoError(t, err)

		// Bootstrapping, not broken: a database created at any point makes this
		// replica viable, and nothing about it needs to be rebuilt first.
		assert.Equal(t, v2.LogicalReplicaStateBootstrapping, status.State)
		assert.Equal(t, v2.LogicalReplicaReasonWaitingForDatabases, status.Reason)
		assert.Contains(t, status.Message, "spec.logicalReplicas[].databases")
		assert.Empty(t, status.Databases)
		assertNothingCreated(t, cl, cr, spec.Name)
	})

	t.Run("waits for a database named in the spec that does not exist", func(t *testing.T) {
		cr := newCluster(t)
		spec := &v2.LogicalReplicaSpec{
			Name:      "analytics",
			Databases: []crunchyv1beta1.PostgresIdentifier{"shop", "warehouse"},
		}

		cl, err := buildFakeClient(t.Context(), cr, primaryPodForCluster(cr))
		require.NoError(t, err)

		crunchyCR := &crunchyv1beta1.PostgresCluster{}
		crunchyCR.Spec.PostgresVersion = 17
		crunchyCR.Status.DatabaseRevision = "abc123"

		r := &PGClusterReconciler{Client: cl}
		r.PodExec = primaryWith("postgres", "template1", "shop")

		status, err := r.reconcileLogicalReplica(t.Context(), cr, crunchyCR, spec, true)
		require.NoError(t, err)

		assert.Equal(t, v2.LogicalReplicaStateBootstrapping, status.State)
		assert.Equal(t, v2.LogicalReplicaReasonWaitingForDatabases, status.Reason)
		assert.Contains(t, status.Message, "warehouse")
		assert.NotContains(t, status.Message, "shop,")

		// Nothing is frozen while one of them is missing: seeding the half that
		// exists would produce a replica that can never cover the other.
		assert.Empty(t, status.Databases)
		assertNothingCreated(t, cl, cr, spec.Name)
	})

	t.Run("freezes the list once every database exists", func(t *testing.T) {
		cr := newCluster(t)
		spec := &v2.LogicalReplicaSpec{
			Name:      "analytics",
			Databases: []crunchyv1beta1.PostgresIdentifier{"shop", "warehouse"},
		}

		cl, err := buildFakeClient(t.Context(), cr, primaryPodForCluster(cr))
		require.NoError(t, err)

		crunchyCR := &crunchyv1beta1.PostgresCluster{}
		crunchyCR.Spec.PostgresVersion = 17
		crunchyCR.Status.DatabaseRevision = "abc123"

		r := &PGClusterReconciler{Client: cl}
		r.PodExec = primaryWith("postgres", "template1", "shop", "warehouse")

		status, err := r.reconcileLogicalReplica(t.Context(), cr, crunchyCR, spec, true)
		require.NoError(t, err)

		assert.Equal(t, v2.LogicalReplicaStateBootstrapping, status.State)
		assert.Empty(t, status.Reason)
		assert.Equal(t, []string{"shop", "warehouse"}, status.Databases)

		// The resolved list is persisted before anything acts on it, so this pass
		// still stops short of the Job.
		assertNothingCreated(t, cl, cr, spec.Name)
	})
}

func TestReconcileLogicalReplicasRequeue(t *testing.T) {
	newCluster := func(state v2.AppState, replicas ...v2.LogicalReplicaSpec) *v2.PerconaPGCluster {
		cr, err := readDefaultCR("cluster1", "pg")
		require.NoError(t, err)
		cr.Default()
		cr.Spec.CRVersion = "3.1.0"
		cr.Spec.LogicalReplicas = replicas
		cr.Status.State = state
		return cr
	}

	newReconciler := func(t *testing.T, cr *v2.PerconaPGCluster) *PGClusterReconciler {
		cl, err := buildFakeClient(t.Context(), cr)
		require.NoError(t, err)
		return &PGClusterReconciler{Client: cl}
	}

	crunchyCR := &crunchyv1beta1.PostgresCluster{}

	t.Run("no requeue when the feature is unused", func(t *testing.T) {
		cr := newCluster(v2.AppStateReady)

		requeue, err := newReconciler(t, cr).reconcileLogicalReplicas(t.Context(), cr, crunchyCR)

		require.NoError(t, err)
		assert.False(t, requeue)
	})

	t.Run("no requeue while the cluster is not ready", func(t *testing.T) {
		// The state is written by updateStatus just before this runs, so the
		// cluster becoming ready wakes the controller on its own. Polling here
		// would spin on a paused cluster forever.
		cr := newCluster(v2.AppStatePaused, v2.LogicalReplicaSpec{Name: "analytics"})

		requeue, err := newReconciler(t, cr).reconcileLogicalReplicas(t.Context(), cr, crunchyCR)

		require.NoError(t, err)
		assert.False(t, requeue)
	})

	t.Run("requeues a replica that is not ready", func(t *testing.T) {
		// No primary pod, so the gate stays shut. Nothing this controller
		// watches will change to reopen it.
		cr := newCluster(v2.AppStateReady, v2.LogicalReplicaSpec{Name: "analytics"})
		r := newReconciler(t, cr)

		requeue, err := r.reconcileLogicalReplicas(t.Context(), cr, crunchyCR)

		require.NoError(t, err)
		assert.True(t, requeue)

		updated := new(v2.PerconaPGCluster)
		require.NoError(t, r.Client.Get(t.Context(),
			client.ObjectKey{Name: cr.Name, Namespace: cr.Namespace}, updated))

		require.Len(t, updated.Status.LogicalReplicas, 1)
		assert.Equal(t, v2.LogicalReplicaReasonPrimaryNotReady, updated.Status.LogicalReplicas[0].Reason)

		cond := meta.FindStatusCondition(updated.Status.Conditions,
			pNaming.ConditionReadyForLogicalReplication)
		require.NotNil(t, cond)
		assert.Equal(t, "PrimaryPodNotFound", cond.Reason)
	})
}

// TestBootstrapIsNotRepeated covers the way a completed bootstrap used to be
// forgotten. The Job is deleted as soon as it completes, so status.seededAt is
// the only remaining record that the replica was seeded; anything that failed
// after it was set - the pod not accepting connections yet, most easily - used to
// discard it and seed the replica a second time.
func TestBootstrapIsNotRepeated(t *testing.T) {
	cr, err := readDefaultCR("cluster1", "pg")
	require.NoError(t, err)
	cr.Default()
	cr.Spec.CRVersion = "3.1.0"
	cr.Status.State = v2.AppStateReady
	// Not set by deploy/cr.yaml, and the rendered replica config needs it.
	cr.Spec.Port = new(int32(5432))

	spec := &v2.LogicalReplicaSpec{Name: "analytics"}

	crunchyCR := &crunchyv1beta1.PostgresCluster{
		ObjectMeta: metav1.ObjectMeta{Name: cr.Name, Namespace: cr.Namespace},
	}
	crunchyCR.Spec.PostgresVersion = 17
	crunchyCR.Spec.Backups.PGBackRest.Repos = []crunchyv1beta1.PGBackRestRepo{{Name: "repo1"}}

	completedJob := func() *batchv1.Job {
		return &batchv1.Job{
			ObjectMeta: metav1.ObjectMeta{
				Name:      logicalReplicaJobName(cr, spec.Name),
				Namespace: cr.Namespace,
			},
			Status: batchv1.JobStatus{Conditions: []batchv1.JobCondition{
				{Type: batchv1.JobComplete, Status: corev1.ConditionTrue},
			}},
		}
	}

	t.Run("the statefulset is created in the same pass as the bootstrap", func(t *testing.T) {
		crWithDatabases := cr.DeepCopy()
		crWithDatabases.Status.LogicalReplicas = []v2.LogicalReplicaStatus{{
			Name:      spec.Name,
			State:     v2.LogicalReplicaStateBootstrapping,
			Databases: []string{"cluster1"},
		}}

		cl, err := buildFakeClient(t.Context(), crWithDatabases, completedJob(),
			primaryPodForCluster(crWithDatabases))
		require.NoError(t, err)

		r := &PGClusterReconciler{Client: cl}
		r.PodExec = func(_ context.Context, _, _, _ string,
			_ io.Reader, out, _ io.Writer, _ ...string,
		) error {
			// The slot the bootstrap created is on the primary. The health check
			// then goes looking for the replica's own pod, which the fake client
			// has none of, and settles on LogicalReplicaPodNotFound.
			_, err := fmt.Fprintln(out, "1")
			return err
		}

		status, err := r.reconcileLogicalReplica(t.Context(), crWithDatabases, crunchyCR, spec, true)
		require.NoError(t, err)

		// Nothing waits for another pass: the replica is seeded, the objects
		// that run it exist, and the only thing left is the pod coming up.
		require.NotNil(t, status.SeededAt)
		assert.Equal(t, v2.LogicalReplicaStateBootstrapping, status.State)
		assert.Equal(t, v2.LogicalReplicaReasonPodNotFound, status.Reason)

		sts := &appsv1.StatefulSet{}
		require.NoError(t, cl.Get(t.Context(), client.ObjectKey{
			Name: logicalReplicaObjectName(cr, spec.Name), Namespace: cr.Namespace}, sts))
		assert.Equal(t, int32(1), *sts.Spec.Replicas)

		require.NoError(t, cl.Get(t.Context(), client.ObjectKey{
			Name: logicalReplicaObjectName(cr, spec.Name), Namespace: cr.Namespace}, new(corev1.Service)))

		// seededAt is now the only record that the replica was seeded: the Job
		// is deleted the moment it completes. The subtest below is what keeps a
		// later failure in this same pass from discarding it.
		err = cl.Get(t.Context(), client.ObjectKey{
			Name: logicalReplicaJobName(cr, spec.Name), Namespace: cr.Namespace}, new(batchv1.Job))
		assert.True(t, apierrors.IsNotFound(err), "the bootstrap job must be deleted: %v", err)
	})

	// The reported symptom: the health check cannot reach the replica yet, the
	// replica is recorded broken, and the next pass bootstraps it again.
	t.Run("a health check failure does not forget the bootstrap", func(t *testing.T) {
		// Bootstrapped, and the pod is not queryable yet, so the health check
		// fails. The recorded status must not lose seededAt.
		bootstrapped := cr.DeepCopy()
		bootstrapped.Status.LogicalReplicas = []v2.LogicalReplicaStatus{{
			Name:      spec.Name,
			State:     v2.LogicalReplicaStateBootstrapping,
			Reason:    v2.LogicalReplicaReasonPodNotFound,
			Databases: []string{"cluster1"},
			SeededAt:  new(metav1.Now()),
		}}
		bootstrapped.Spec.LogicalReplicas = v2.LogicalReplicas{*spec}

		cl, err := buildFakeClient(t.Context(), bootstrapped,
			primaryPodForCluster(bootstrapped), logicalReplicaUserSecret(bootstrapped))
		require.NoError(t, err)

		r := &PGClusterReconciler{Client: cl}
		r.PodExec = func(_ context.Context, _, _, _ string,
			stdin io.Reader, out, _ io.Writer, _ ...string,
		) error {
			sql, err := io.ReadAll(stdin)
			require.NoError(t, err)
			// The readiness probe query succeeds; the slot count on the primary
			// does not.
			if strings.Contains(string(sql), "pg_hba_file_rules") {
				_, err = fmt.Fprintln(out, "")
				return err
			}
			return errors.New("FATAL: the database system is starting up")
		}

		_, err = r.reconcileLogicalReplicas(t.Context(), bootstrapped, crunchyCR)
		require.NoError(t, err)

		updated := new(v2.PerconaPGCluster)
		require.NoError(t, cl.Get(t.Context(),
			client.ObjectKey{Name: cr.Name, Namespace: cr.Namespace}, updated))

		require.Len(t, updated.Status.LogicalReplicas, 1)
		recorded := updated.Status.LogicalReplicas[0]
		assert.Equal(t, v2.LogicalReplicaStateBroken, recorded.State)
		assert.NotNil(t, recorded.SeededAt, "seededAt must survive a failed pass")
		assert.Equal(t, []string{"cluster1"}, recorded.Databases,
			"the frozen database list must survive a failed pass")
	})

	t.Run("an existing statefulset is adopted rather than seeded again", func(t *testing.T) {
		// seededAt lost by any means at all: the StatefulSet is the more
		// trustworthy record, because it only exists after a bootstrap.
		crWithDatabases := cr.DeepCopy()
		crWithDatabases.Status.LogicalReplicas = []v2.LogicalReplicaStatus{{
			Name:      spec.Name,
			State:     v2.LogicalReplicaStateBootstrapping,
			Databases: []string{"cluster1"},
		}}

		// Truncated because metav1.Time round-trips at second granularity.
		created := metav1.NewTime(time.Now().Add(-time.Hour).Truncate(time.Second))
		sts := &appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{
			Name:              logicalReplicaObjectName(cr, spec.Name),
			Namespace:         cr.Namespace,
			CreationTimestamp: created,
		}}

		cl, err := buildFakeClient(t.Context(), crWithDatabases, sts)
		require.NoError(t, err)
		r := &PGClusterReconciler{Client: cl}

		status := logicalReplicaStatusFor(crWithDatabases, spec.Name)
		bootstrapped, err := r.reconcileLogicalReplicaBootstrap(
			t.Context(), crWithDatabases, crunchyCR, spec, status)

		require.NoError(t, err)
		assert.True(t, bootstrapped)
		require.NotNil(t, status.SeededAt)
		// Taken from the StatefulSet, not stamped as now.
		assert.True(t, status.SeededAt.Equal(&created),
			"expected %v, got %v", created, status.SeededAt)

		job := &batchv1.Job{}
		err = cl.Get(t.Context(), client.ObjectKey{
			Name: logicalReplicaJobName(cr, spec.Name), Namespace: cr.Namespace}, job)
		assert.True(t, apierrors.IsNotFound(err), "no bootstrap job may be created: %v", err)
	})
}

func TestLogicalReplicaPodRequiresReady(t *testing.T) {
	cr, err := readDefaultCR("cluster1", "pg")
	require.NoError(t, err)
	cr.Default()

	pod := func(ready corev1.ConditionStatus) *corev1.Pod {
		return &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "cluster1-lr-analytics-0",
				Namespace: cr.Namespace,
				Labels: map[string]string{
					naming.LabelCluster:         cr.Name,
					pNaming.LabelLogicalReplica: "analytics",
				},
			},
			Status: corev1.PodStatus{
				Phase:      corev1.PodRunning,
				Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: ready}},
			},
		}
	}

	// Running but not ready is the state a replica pod is in for the first few
	// seconds. Returning it made psql fail and the replica look broken.
	cl, err := buildFakeClient(t.Context(), cr.DeepCopy(), pod(corev1.ConditionFalse))
	require.NoError(t, err)
	_, err = (&PGClusterReconciler{Client: cl}).logicalReplicaPod(t.Context(), cr, "analytics")
	require.Error(t, err)

	cl, err = buildFakeClient(t.Context(), cr.DeepCopy(), pod(corev1.ConditionTrue))
	require.NoError(t, err)
	found, err := (&PGClusterReconciler{Client: cl}).logicalReplicaPod(t.Context(), cr, "analytics")
	require.NoError(t, err)
	assert.Equal(t, "cluster1-lr-analytics-0", found.Name)
}

func TestUpdateLogicalReplicaStatus(t *testing.T) {
	cr, err := readDefaultCR("cluster1", "pg")
	require.NoError(t, err)
	cr.Default()

	cl, err := buildFakeClient(t.Context(), cr)
	require.NoError(t, err)
	r := &PGClusterReconciler{Client: cl}

	read := func() *v2.PerconaPGCluster {
		out := new(v2.PerconaPGCluster)
		require.NoError(t, cl.Get(t.Context(),
			client.ObjectKey{Name: cr.Name, Namespace: cr.Namespace}, out))
		return out
	}

	statuses := []v2.LogicalReplicaStatus{{Name: "analytics", State: v2.LogicalReplicaStateBootstrapping}}

	t.Run("writes the statuses and the condition together", func(t *testing.T) {
		require.NoError(t, r.updateLogicalReplicaStatus(t.Context(), cr, statuses, &metav1.Condition{
			Type:    pNaming.ConditionReadyForLogicalReplication,
			Status:  metav1.ConditionFalse,
			Reason:  "ReplicationHBAMissing",
			Message: "first",
		}))

		out := read()
		require.Len(t, out.Status.LogicalReplicas, 1)

		cond := meta.FindStatusCondition(out.Status.Conditions,
			pNaming.ConditionReadyForLogicalReplication)
		require.NotNil(t, cond)
		assert.Equal(t, metav1.ConditionFalse, cond.Status)
		assert.Equal(t, out.Generation, cond.ObservedGeneration)
	})

	t.Run("a message that changes on its own is not a transition", func(t *testing.T) {
		before := meta.FindStatusCondition(read().Status.Conditions,
			pNaming.ConditionReadyForLogicalReplication).LastTransitionTime
		require.False(t, before.IsZero())

		require.NoError(t, r.updateLogicalReplicaStatus(t.Context(), cr, statuses, &metav1.Condition{
			Type:    pNaming.ConditionReadyForLogicalReplication,
			Status:  metav1.ConditionFalse,
			Reason:  "RestartPending",
			Message: "second",
		}))

		cond := meta.FindStatusCondition(read().Status.Conditions,
			pNaming.ConditionReadyForLogicalReplication)
		assert.Equal(t, "RestartPending", cond.Reason)
		assert.Equal(t, "second", cond.Message)
		assert.True(t, before.Equal(&cond.LastTransitionTime))
	})

	t.Run("the condition goes with the last replica", func(t *testing.T) {
		require.NoError(t, r.updateLogicalReplicaStatus(t.Context(), cr, nil, nil))

		out := read()
		assert.Empty(t, out.Status.LogicalReplicas)
		assert.Nil(t, meta.FindStatusCondition(out.Status.Conditions,
			pNaming.ConditionReadyForLogicalReplication))
	})
}
