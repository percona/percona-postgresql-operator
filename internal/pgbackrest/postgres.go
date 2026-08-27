// Copyright 2021 - 2024 Crunchy Data Solutions, Inc.
//
// SPDX-License-Identifier: Apache-2.0

package pgbackrest

import (
	"strings"

	"github.com/percona/percona-postgresql-operator/v3/internal/postgres"
	"github.com/percona/percona-postgresql-operator/v3/pkg/apis/upstream.pgv2.percona.com/v1beta1"
)

// PostgreSQL populates outParameters with any settings needed to run pgBackRest.
func PostgreSQL(
	inCluster *v1beta1.PostgresCluster,
	outParameters *postgres.Parameters,
	backupsEnabled bool,
) {
	if outParameters.Mandatory == nil {
		outParameters.Mandatory = postgres.NewParameterSet()
	}
	if outParameters.Default == nil {
		outParameters.Default = postgres.NewParameterSet()
	}

	walEncryption := inCluster.Spec.Extensions.PGTDE.WALEncryption

	// Send WAL files to all configured repositories when not in recovery.
	// - https://pgbackrest.org/user-guide.html#quickstart/configure-archiving
	// - https://pgbackrest.org/command.html#command-archive-push
	// - https://www.postgresql.org/docs/current/runtime-config-wal.html
	archive := `pgbackrest --stanza=` + DefaultStanzaName + ` archive-push "%p"`

	// K8SPG-911: pg_tde keeps WAL segments encrypted on disk, which pgBackRest cannot
	// read. pg_tde_archive_decrypt writes a decrypted copy of the segment and runs the
	// command it is given against that copy. The "%%" leaves a literal "%p" for
	// pg_tde_archive_decrypt to replace with the path of that copy, rather than having
	// PostgreSQL expand it to the encrypted original.
	if walEncryption {
		archive = `pg_tde_archive_decrypt %f %p "pgbackrest --stanza=` +
			DefaultStanzaName + ` archive-push %%p"`
	}

	// K8SPG-518
	if inCluster.CompareVersion("2.8.0") >= 0 {
		if trackRestorableTime := inCluster.Spec.Backups.TrackLatestRestorableTime; trackRestorableTime != nil && *trackRestorableTime {
			updateCommandRestorableTime(&archive, walEncryption)
			// K8SPG-518: This parameter is required to ensure that the commit timestamp is
			// included in the WAL file. This is necessary for the WAL watcher to
			// function correctly.
			outParameters.Mandatory.Add("track_commit_timestamp", "true")
		}
	} else {
		updateCommandRestorableTime(&archive, walEncryption)
	}

	outParameters.Mandatory.Add("archive_mode", "on")

	if backupsEnabled {
		outParameters.Mandatory.Add("archive_command", archive)
	} else {
		// If backups are disabled, keep archive_mode on (to avoid a Postgres restart)
		// and throw away WAL.
		outParameters.Mandatory.Add("archive_command", `true`)
	}

	if inCluster.CompareVersion("2.8.0") < 0 {
		// K8SPG-518: This parameter is required to ensure that the commit timestamp is
		// included in the WAL file. This is necessary for the WAL watcher to
		// function correctly.
		outParameters.Mandatory.Add("track_commit_timestamp", "true")
	}

	// archive_timeout is used to determine at what point a WAL file is switched,
	// if the WAL archive has not reached its full size in # of transactions
	// (16MB). This has ramifications for log shipping, i.e. it ensures a WAL file
	// is shipped to an archive every X seconds to help reduce the risk of data
	// loss in a disaster recovery scenario. For standby servers that are not
	// connected using streaming replication, this also ensures that new data is
	// available at least once a minute.
	//
	// PostgreSQL documentation considers an archive_timeout of 60 seconds to be
	// reasonable. There are cases where you may want to set archive_timeout to 0,
	// for example, when the remote archive (pgBackRest repo) is unavailable; this
	// is to prevent WAL accumulation on your primary.
	// - https://www.postgresql.org/docs/current/runtime-config-wal.html#GUC-ARCHIVE-TIMEOUT
	outParameters.Default.Add("archive_timeout", "60s")

	// Fetch WAL files from any configured repository during recovery.
	// - https://pgbackrest.org/command.html#command-archive-get
	// - https://www.postgresql.org/docs/current/runtime-config-wal.html
	//
	// The repository option belongs to pgBackRest, so it goes on the pgBackRest
	// command rather than on whatever is wrapping it.
	archiveGet := func(repoOption string) string {
		if walEncryption {
			return WALEncryptRestoreCommand(repoOption)
		}
		return `pgbackrest --stanza=` + DefaultStanzaName + ` archive-get %f "%p"` + repoOption
	}

	// The wrapper script only decides whether to skip WAL recovery before exec'ing
	// what it is given, so it has to stay outermost.
	wrap := func(command string) string {
		if inCluster.CompareVersion("2.9.0") >= 0 {
			command = "/opt/crunchy/bin/restore-command-wrapper.sh " + command
		}
		return command
	}

	restore := wrap(archiveGet(""))
	restoreOverridden := false
	if inCluster.Spec.Patroni != nil && inCluster.Spec.Patroni.DynamicConfiguration != nil {
		postgresql, ok := inCluster.Spec.Patroni.DynamicConfiguration["postgresql"].(map[string]any)
		if ok {
			params, ok := postgresql["parameters"].(map[string]any)
			if ok {
				restore_command, ok := params["restore_command"].(string)
				if ok {
					restore = restore_command
					restoreOverridden = true
				}
			}
		}
	}

	// If backups are disabled, there is no pgBackRest repository to restore WAL
	// from. Unlike archive_command, restore_command can't be replaced with a
	// no-op placeholder: Postgres treats a zero exit status as "the file was
	// placed", so a placeholder would make it think recovery succeeded when it
	// didn't. Leave restore_command unset so Postgres relies on streaming
	// replication and local WAL only -- unless the user explicitly configured
	// their own restore_command.
	if backupsEnabled || restoreOverridden {
		outParameters.Mandatory.Add("restore_command", restore)
	}

	if inCluster.Spec.Standby != nil && inCluster.Spec.Standby.Enabled && inCluster.Spec.Standby.RepoName != "" {

		// Fetch WAL files from the designated repository. The repository name
		// is validated by the Kubernetes API, so it does not need to be quoted
		// nor escaped.
		repoName := inCluster.Spec.Standby.RepoName
		repoOption := " --repo=" + strings.TrimPrefix(repoName, "repo")

		// A user-supplied restore_command is opaque to us, so it can only be
		// appended to.
		if restoreOverridden {
			restore += repoOption
		} else {
			restore = wrap(archiveGet(repoOption))
		}
		outParameters.Mandatory.Add("restore_command", restore)
	}
}

// WALEncryptRestoreCommand returns a restore_command that fetches a WAL segment
// from the pgBackRest repository and encrypts it into place. Any repoOption is
// passed to pgBackRest, so it belongs inside the command being wrapped.
//
// K8SPG-911: archive_command decrypts before pushing, so the repository holds
// plaintext WAL. pg_tde_restore_encrypt fetches a segment into a temporary file
// and encrypts it into place. As with archive_command, the "%%" escapes are there
// for pg_tde_restore_encrypt rather than for PostgreSQL.
func WALEncryptRestoreCommand(repoOption string) string {
	return `pg_tde_restore_encrypt %f %p "pgbackrest --stanza=` + DefaultStanzaName +
		` archive-get %%f \"%%p\"` + repoOption + `"`
}

func updateCommandRestorableTime(archive *string, walEncryption bool) {
	fixTimezone := `sed -E "s/([0-9]{4}-[0-9]{2}-[0-9]{2}) ([0-9]{2}:[0-9]{2}:[0-9]{2}\.[0-9]{6}) (UTC|[\\+\\-][0-9]{2})/\1T\2\3/" | sed "s/UTC/Z/"`
	extractCommitTime := `grep -oP "COMMIT \K[^;]+" | ` + fixTimezone + ``
	validateCommitTime := `grep -E "^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}\.[0-9]{6}(Z|[\+\-][0-9]{2})$"`

	// K8SPG-911: this reads the segment PostgreSQL substitutes into "%p", which is
	// still the encrypted original, so it needs the pg_tde build of pg_waldump.
	waldump := "pg_waldump"
	if walEncryption {
		waldump = "pg_tde_waldump -k $PGDATA/pg_tde"
	}

	*archive += ` && timestamp=$(` + waldump + ` -r Transaction "%p" | ` + extractCommitTime + ` | tail -n 1 | ` + validateCommitTime + `);`
	*archive += ` if [ ! -z ${timestamp} ]; then echo ${timestamp} > /pgdata/latest_commit_timestamp.txt; fi`
}
