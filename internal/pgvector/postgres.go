package pgvector

import (
	"context"

	"github.com/percona/percona-postgresql-operator/internal/logging"
	"github.com/percona/percona-postgresql-operator/internal/postgres"
)

// EnableInPostgreSQL installs pgvector triggers into every database.
func EnableInPostgreSQL(ctx context.Context, exec postgres.Executor) error {
	log := logging.FromContext(ctx)

	stdout, stderr, err := exec.ExecInAllDatabases(ctx,
		// Quiet the NOTICE from IF EXISTS, and install the pgAudit event triggers.
		// - https://www.postgresql.org/docs/current/runtime-config-client.html
		// - https://github.com/pgaudit/pgaudit#settings
		`SET client_min_messages = WARNING; CREATE EXTENSION IF NOT EXISTS vector; ALTER EXTENSION vector UPDATE;`,
		map[string]string{
			"ON_ERROR_STOP": "on", // Abort when any one command fails.
			"QUIET":         "on", // Do not print successful commands to stdout.
		})

	log.V(1).Info("enabled pgvector", "stdout", stdout, "stderr", stderr)

	return err
}

func DisableInPostgreSQL(ctx context.Context, exec postgres.Executor) error {
	log := logging.FromContext(ctx)

	stdout, stderr, err := exec.ExecInAllDatabases(ctx,
		// Quiet the NOTICE from IF EXISTS, and install the pgAudit event triggers.
		// - https://www.postgresql.org/docs/current/runtime-config-client.html
		// - https://github.com/pgaudit/pgaudit#settings
		`SET client_min_messages = WARNING; DROP EXTENSION IF EXISTS vector;`,
		map[string]string{
			"ON_ERROR_STOP": "on", // Abort when any one command fails.
			"QUIET":         "on", // Do not print successful commands to stdout.
		})

	log.V(1).Info("disabled pgvector", "stdout", stdout, "stderr", stderr)

	return err
}

// PostgreSQLParameters sets the parameters required by pgAudit.
func PostgreSQLParameters(outParameters *postgres.Parameters) {}
