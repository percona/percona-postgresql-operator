// Copyright 2021 - 2024 Crunchy Data Solutions, Inc.
//
// SPDX-License-Identifier: Apache-2.0

package pgbackrest

import (
	"strings"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/percona/percona-postgresql-operator/v3/internal/postgres"
	"github.com/percona/percona-postgresql-operator/v3/percona/version"
	"github.com/percona/percona-postgresql-operator/v3/pkg/apis/upstream.pgv2.percona.com/v1beta1"
)

// K8SPG-911: the restore Job reuses this, so the escaping is asserted on its own.
// PostgreSQL consumes the "%%" and the backslashes, leaving literal "%f" and "%p"
// for pg_tde_restore_encrypt to substitute in the command it runs.
func TestWALEncryptRestoreCommand(t *testing.T) {
	assert.Equal(t, WALEncryptRestoreCommand(""),
		`pg_tde_restore_encrypt %f %p "pgbackrest --stanza=db archive-get %%f \"%%p\""`)

	assert.Equal(t, WALEncryptRestoreCommand(" --repo=99"),
		`pg_tde_restore_encrypt %f %p "pgbackrest --stanza=db archive-get %%f \"%%p\" --repo=99"`)
}

func TestPostgreSQLParameters(t *testing.T) {
	t.Run("latest CR version", func(t *testing.T) {
		cluster := new(v1beta1.PostgresCluster)
		parameters := new(postgres.Parameters)

		if cluster.Labels == nil {
			cluster.Labels = make(map[string]string)
		}
		cluster.Labels["pgv2.percona.com/version"] = version.Version()

		PostgreSQL(cluster, parameters, true)
		assert.DeepEqual(t, parameters.Mandatory.AsMap(), map[string]string{
			"archive_mode":    "on",
			"archive_command": `pgbackrest --stanza=db archive-push "%p"`,
			"restore_command": `/opt/crunchy/bin/restore-command-wrapper.sh pgbackrest --stanza=db archive-get %f "%p"`,
		})

		assert.DeepEqual(t, parameters.Default.AsMap(), map[string]string{
			"archive_timeout": "60s",
		})

		dynamic := map[string]any{
			"postgresql": map[string]any{
				"parameters": map[string]any{
					"restore_command": "/bin/true",
				},
			},
		}
		if cluster.Spec.Patroni == nil {
			cluster.Spec.Patroni = &v1beta1.PatroniSpec{}
		}
		cluster.Spec.Patroni.DynamicConfiguration = dynamic

		PostgreSQL(cluster, parameters, true)
		assert.DeepEqual(t, parameters.Mandatory.AsMap(), map[string]string{
			"archive_mode":    "on",
			"archive_command": `pgbackrest --stanza=db archive-push "%p"`,
			"restore_command": "/bin/true",
		})

		cluster.Spec.Standby = &v1beta1.PostgresStandbySpec{
			Enabled:  true,
			RepoName: "repo99",
		}
		cluster.Spec.Patroni.DynamicConfiguration = nil

		PostgreSQL(cluster, parameters, true)
		assert.DeepEqual(t, parameters.Mandatory.AsMap(), map[string]string{
			"archive_mode":    "on",
			"archive_command": `pgbackrest --stanza=db archive-push "%p"`,
			"restore_command": `/opt/crunchy/bin/restore-command-wrapper.sh pgbackrest --stanza=db archive-get %f "%p" --repo=99`,
		})

		cluster.Spec.Standby = nil
		cluster.Spec.Patroni.DynamicConfiguration = nil
		cluster.Spec.Backups.TrackLatestRestorableTime = new(true)

		PostgreSQL(cluster, parameters, true)
		assert.DeepEqual(t, parameters.Mandatory.AsMap(), map[string]string{
			"archive_mode": "on",
			"archive_command": strings.Join([]string{
				`pgbackrest --stanza=db archive-push "%p" `,
				`&& timestamp=$(pg_waldump -r Transaction "%p" | `,
				`grep -oP "COMMIT \K[^;]+" | `,
				`sed -E "s/([0-9]{4}-[0-9]{2}-[0-9]{2}) ([0-9]{2}:[0-9]{2}:[0-9]{2}\.[0-9]{6}) (UTC|[\\+\\-][0-9]{2})/\1T\2\3/" | `,
				`sed "s/UTC/Z/" | `,
				"tail -n 1 | ",
				`grep -E "^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}\.[0-9]{6}(Z|[\+\-][0-9]{2})$"); `,
				"if [ ! -z ${timestamp} ]; then echo ${timestamp} > /pgdata/latest_commit_timestamp.txt; fi",
			}, ""),
			"restore_command":        `/opt/crunchy/bin/restore-command-wrapper.sh pgbackrest --stanza=db archive-get %f "%p"`,
			"track_commit_timestamp": "true",
		})
	})

	t.Run("backups disabled", func(t *testing.T) {
		cluster := new(v1beta1.PostgresCluster)
		parameters := new(postgres.Parameters)

		if cluster.Labels == nil {
			cluster.Labels = make(map[string]string)
		}
		cluster.Labels["pgv2.percona.com/version"] = version.Version()

		// No restore_command override: the key is omitted entirely, since a
		// pgBackRest command would fail with no repository configured.
		PostgreSQL(cluster, parameters, false)
		assert.DeepEqual(t, parameters.Mandatory.AsMap(), map[string]string{
			"archive_mode":    "on",
			"archive_command": "true",
		})

		// An explicit user override is still respected even with backups disabled.
		dynamic := map[string]any{
			"postgresql": map[string]any{
				"parameters": map[string]any{
					"restore_command": "/bin/true",
				},
			},
		}
		if cluster.Spec.Patroni == nil {
			cluster.Spec.Patroni = &v1beta1.PatroniSpec{}
		}
		cluster.Spec.Patroni.DynamicConfiguration = dynamic

		PostgreSQL(cluster, parameters, false)
		assert.DeepEqual(t, parameters.Mandatory.AsMap(), map[string]string{
			"archive_mode":    "on",
			"archive_command": "true",
			"restore_command": "/bin/true",
		})

		// A standby cluster following an external repo needs restore_command
		// regardless of this cluster's own backups.enabled setting.
		cluster.Spec.Standby = &v1beta1.PostgresStandbySpec{
			Enabled:  true,
			RepoName: "repo99",
		}
		cluster.Spec.Patroni.DynamicConfiguration = nil

		PostgreSQL(cluster, parameters, false)
		assert.DeepEqual(t, parameters.Mandatory.AsMap(), map[string]string{
			"archive_mode":    "on",
			"archive_command": "true",
			"restore_command": `/opt/crunchy/bin/restore-command-wrapper.sh pgbackrest --stanza=db archive-get %f "%p" --repo=99`,
		})
	})

	t.Run("2.8.0< version", func(t *testing.T) {
		cluster := new(v1beta1.PostgresCluster)
		parameters := new(postgres.Parameters)

		if cluster.Labels == nil {
			cluster.Labels = make(map[string]string)
		}
		cluster.Labels["pgv2.percona.com/version"] = "2.7.0"

		PostgreSQL(cluster, parameters, true)
		assert.DeepEqual(t, parameters.Mandatory.AsMap(), map[string]string{
			"archive_mode": "on",
			"archive_command": strings.Join([]string{
				`pgbackrest --stanza=db archive-push "%p" `,
				`&& timestamp=$(pg_waldump -r Transaction "%p" | `,
				`grep -oP "COMMIT \K[^;]+" | `,
				`sed -E "s/([0-9]{4}-[0-9]{2}-[0-9]{2}) ([0-9]{2}:[0-9]{2}:[0-9]{2}\.[0-9]{6}) (UTC|[\\+\\-][0-9]{2})/\1T\2\3/" | `,
				`sed "s/UTC/Z/" | `,
				"tail -n 1 | ",
				`grep -E "^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}\.[0-9]{6}(Z|[\+\-][0-9]{2})$"); `,
				"if [ ! -z ${timestamp} ]; then echo ${timestamp} > /pgdata/latest_commit_timestamp.txt; fi",
			}, ""),
			"restore_command":        `pgbackrest --stanza=db archive-get %f "%p"`,
			"track_commit_timestamp": "true",
		})

		assert.DeepEqual(t, parameters.Default.AsMap(), map[string]string{
			"archive_timeout": "60s",
		})

		dynamic := map[string]any{
			"postgresql": map[string]any{
				"parameters": map[string]any{
					"restore_command": "/bin/true",
				},
			},
		}
		if cluster.Spec.Patroni == nil {
			cluster.Spec.Patroni = &v1beta1.PatroniSpec{}
		}
		cluster.Spec.Patroni.DynamicConfiguration = dynamic

		PostgreSQL(cluster, parameters, true)
		assert.DeepEqual(t, parameters.Mandatory.AsMap(), map[string]string{
			"archive_mode": "on",
			"archive_command": strings.Join([]string{
				`pgbackrest --stanza=db archive-push "%p" `,
				`&& timestamp=$(pg_waldump -r Transaction "%p" | `,
				`grep -oP "COMMIT \K[^;]+" | `,
				`sed -E "s/([0-9]{4}-[0-9]{2}-[0-9]{2}) ([0-9]{2}:[0-9]{2}:[0-9]{2}\.[0-9]{6}) (UTC|[\\+\\-][0-9]{2})/\1T\2\3/" | `,
				`sed "s/UTC/Z/" | `,
				"tail -n 1 | ",
				`grep -E "^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}\.[0-9]{6}(Z|[\+\-][0-9]{2})$"); `,
				"if [ ! -z ${timestamp} ]; then echo ${timestamp} > /pgdata/latest_commit_timestamp.txt; fi",
			}, ""),
			"restore_command":        "/bin/true",
			"track_commit_timestamp": "true",
		})

		cluster.Spec.Standby = &v1beta1.PostgresStandbySpec{
			Enabled:  true,
			RepoName: "repo99",
		}
		cluster.Spec.Patroni.DynamicConfiguration = nil

		PostgreSQL(cluster, parameters, true)
		assert.DeepEqual(t, parameters.Mandatory.AsMap(), map[string]string{
			"archive_mode": "on",
			"archive_command": strings.Join([]string{
				`pgbackrest --stanza=db archive-push "%p" `,
				`&& timestamp=$(pg_waldump -r Transaction "%p" | `,
				`grep -oP "COMMIT \K[^;]+" | `,
				`sed -E "s/([0-9]{4}-[0-9]{2}-[0-9]{2}) ([0-9]{2}:[0-9]{2}:[0-9]{2}\.[0-9]{6}) (UTC|[\\+\\-][0-9]{2})/\1T\2\3/" | `,
				`sed "s/UTC/Z/" | `,
				"tail -n 1 | ",
				`grep -E "^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}\.[0-9]{6}(Z|[\+\-][0-9]{2})$"); `,
				"if [ ! -z ${timestamp} ]; then echo ${timestamp} > /pgdata/latest_commit_timestamp.txt; fi",
			}, ""),
			"restore_command":        `pgbackrest --stanza=db archive-get %f "%p" --repo=99`,
			"track_commit_timestamp": "true",
		})

		cluster.Spec.Standby = nil
		cluster.Spec.Patroni.DynamicConfiguration = nil
		cluster.Spec.Backups.TrackLatestRestorableTime = new(true)

		PostgreSQL(cluster, parameters, true)
		assert.DeepEqual(t, parameters.Mandatory.AsMap(), map[string]string{
			"archive_mode": "on",
			"archive_command": strings.Join([]string{
				`pgbackrest --stanza=db archive-push "%p" `,
				`&& timestamp=$(pg_waldump -r Transaction "%p" | `,
				`grep -oP "COMMIT \K[^;]+" | `,
				`sed -E "s/([0-9]{4}-[0-9]{2}-[0-9]{2}) ([0-9]{2}:[0-9]{2}:[0-9]{2}\.[0-9]{6}) (UTC|[\\+\\-][0-9]{2})/\1T\2\3/" | `,
				`sed "s/UTC/Z/" | `,
				"tail -n 1 | ",
				`grep -E "^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}\.[0-9]{6}(Z|[\+\-][0-9]{2})$"); `,
				"if [ ! -z ${timestamp} ]; then echo ${timestamp} > /pgdata/latest_commit_timestamp.txt; fi",
			}, ""),
			"restore_command":        `pgbackrest --stanza=db archive-get %f "%p"`,
			"track_commit_timestamp": "true",
		})
	})

	// K8SPG-911
	t.Run("WAL encryption", func(t *testing.T) {
		cluster := new(v1beta1.PostgresCluster)
		parameters := new(postgres.Parameters)

		cluster.Labels = map[string]string{"pgv2.percona.com/version": version.Version()}
		cluster.Spec.Extensions.PGTDE.Enabled = true
		cluster.Spec.Extensions.PGTDE.WALEncryption = true

		PostgreSQL(cluster, parameters, true)
		assert.DeepEqual(t, parameters.Mandatory.AsMap(), map[string]string{
			"archive_mode":    "on",
			"archive_command": `pg_tde_archive_decrypt %f %p "pgbackrest --stanza=db archive-push %%p"`,
			"restore_command": `/opt/crunchy/bin/restore-command-wrapper.sh ` +
				`pg_tde_restore_encrypt %f %p "pgbackrest --stanza=db archive-get %%f \"%%p\""`,
		})

		// The repository option belongs to pgBackRest, so it stays inside the
		// command pg_tde_restore_encrypt runs.
		cluster.Spec.Standby = &v1beta1.PostgresStandbySpec{
			Enabled:  true,
			RepoName: "repo99",
		}

		PostgreSQL(cluster, parameters, true)
		assert.DeepEqual(t, parameters.Mandatory.AsMap(), map[string]string{
			"archive_mode":    "on",
			"archive_command": `pg_tde_archive_decrypt %f %p "pgbackrest --stanza=db archive-push %%p"`,
			"restore_command": `/opt/crunchy/bin/restore-command-wrapper.sh ` +
				`pg_tde_restore_encrypt %f %p "pgbackrest --stanza=db archive-get %%f \"%%p\" --repo=99"`,
		})

		// A user-supplied restore_command is opaque, so it is neither wrapped nor
		// rewritten -- the repository option can only be appended.
		cluster.Spec.Patroni = &v1beta1.PatroniSpec{
			DynamicConfiguration: map[string]any{
				"postgresql": map[string]any{
					"parameters": map[string]any{
						"restore_command": "/bin/true",
					},
				},
			},
		}

		PostgreSQL(cluster, parameters, true)
		assert.Equal(t, parameters.Mandatory.Value("restore_command"), "/bin/true --repo=99")

		// The commit timestamp is read from the segment Postgres hands us, which
		// is still encrypted, so it takes the pg_tde build of pg_waldump pointed
		// at the keyring the startup script links into the data volume.
		cluster.Spec.Standby = nil
		cluster.Spec.Patroni.DynamicConfiguration = nil
		cluster.Spec.Backups.TrackLatestRestorableTime = new(true)

		PostgreSQL(cluster, parameters, true)
		assert.DeepEqual(t, parameters.Mandatory.AsMap(), map[string]string{
			"archive_mode": "on",
			"archive_command": strings.Join([]string{
				`pg_tde_archive_decrypt %f %p "pgbackrest --stanza=db archive-push %%p" `,
				`&& timestamp=$(pg_tde_waldump -k $PGDATA/pg_tde -r Transaction "%p" | `,
				`grep -oP "COMMIT \K[^;]+" | `,
				`sed -E "s/([0-9]{4}-[0-9]{2}-[0-9]{2}) ([0-9]{2}:[0-9]{2}:[0-9]{2}\.[0-9]{6}) (UTC|[\\+\\-][0-9]{2})/\1T\2\3/" | `,
				`sed "s/UTC/Z/" | `,
				"tail -n 1 | ",
				`grep -E "^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}\.[0-9]{6}(Z|[\+\-][0-9]{2})$"); `,
				"if [ ! -z ${timestamp} ]; then echo ${timestamp} > /pgdata/latest_commit_timestamp.txt; fi",
			}, ""),
			"restore_command": `/opt/crunchy/bin/restore-command-wrapper.sh ` +
				`pg_tde_restore_encrypt %f %p "pgbackrest --stanza=db archive-get %%f \"%%p\""`,
			"track_commit_timestamp": "true",
		})
	})
}
