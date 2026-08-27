package setuser

import (
	"context"

	"github.com/percona/percona-postgresql-operator/v3/internal/logging"
	"github.com/percona/percona-postgresql-operator/v3/internal/postgres"
)

// EnableInPostgreSQL installs set_user in every database.
func EnableInPostgreSQL(ctx context.Context, exec postgres.Executor) error {
	log := logging.FromContext(ctx)

	stdout, stderr, err := exec.ExecInAllDatabases(ctx,
		`SET client_min_messages = WARNING; CREATE EXTENSION IF NOT EXISTS set_user; ALTER EXTENSION set_user UPDATE;`,
		map[string]string{
			"ON_ERROR_STOP": "on",
			"QUIET":         "on",
		})

	log.V(1).Info("enabled set_user", "stdout", stdout, "stderr", stderr)

	return err
}

func DisableInPostgreSQL(ctx context.Context, exec postgres.Executor) error {
	log := logging.FromContext(ctx)

	stdout, stderr, err := exec.ExecInAllDatabases(ctx,
		`SET client_min_messages = WARNING; DROP EXTENSION IF EXISTS set_user;`,
		map[string]string{
			"ON_ERROR_STOP": "on",
			"QUIET":         "on",
		})

	log.V(1).Info("disabled set_user", "stdout", stdout, "stderr", stderr)

	return err
}

func PostgreSQLParameters(outParameters *postgres.Parameters) {
	outParameters.Mandatory.AppendToList("shared_preload_libraries", "set_user")
}
