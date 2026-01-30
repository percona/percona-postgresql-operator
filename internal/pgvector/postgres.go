package pgvector

import (
	"context"

	"github.com/percona/percona-postgresql-operator/v2/internal/logging"
	"github.com/percona/percona-postgresql-operator/v2/internal/postgres"
)

// EnableInPostgreSQL installs pgvector triggers into every database.
func EnableInPostgreSQL(ctx context.Context, exec postgres.Executor) error {
	log := logging.FromContext(ctx)

	stdout, stderr, err := exec.ExecInAllDatabases(ctx,
		// Quiet the NOTICE from IF EXISTS, and create pgvector extension.
		// - https://www.postgresql.org/docs/current/runtime-config-client.html
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
		// Quiet the NOTICE from IF EXISTS, and drop pgvector extension.
		// - https://www.postgresql.org/docs/current/runtime-config-client.html
		`SET client_min_messages = WARNING; DROP EXTENSION IF EXISTS vector;`,
		map[string]string{
			"ON_ERROR_STOP": "on", // Abort when any one command fails.
			"QUIET":         "on", // Do not print successful commands to stdout.
		})

	log.V(1).Info("disabled pgvector", "stdout", stdout, "stderr", stderr)

	return err
}

// PostgreSQLParameters sets the parameters required by pgvector.
func PostgreSQLParameters(outParameters *postgres.Parameters) {}
