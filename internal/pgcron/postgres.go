package pgcron

import (
	"context"
	"strings"

	"github.com/percona/percona-postgresql-operator/v2/internal/logging"
	"github.com/percona/percona-postgresql-operator/v2/internal/postgres"
)

// EnableInPostgreSQL installs pg_cron in the postgres database.
func EnableInPostgreSQL(ctx context.Context, exec postgres.Executor) error {
	log := logging.FromContext(ctx)

	stdout, stderr, err := exec.Exec(ctx,
		strings.NewReader(`SET client_min_messages = WARNING; CREATE EXTENSION IF NOT EXISTS pg_cron; ALTER EXTENSION pg_cron UPDATE;`),
		map[string]string{
			"ON_ERROR_STOP": "on",
			"QUIET":         "on",
		},
		[]string{"--dbname=postgres"})

	log.V(1).Info("enabled pg_cron", "stdout", stdout, "stderr", stderr)

	return err
}

func DisableInPostgreSQL(ctx context.Context, exec postgres.Executor) error {
	log := logging.FromContext(ctx)

	stdout, stderr, err := exec.Exec(ctx,
		strings.NewReader(`SET client_min_messages = WARNING; DROP EXTENSION IF EXISTS pg_cron;`),
		map[string]string{
			"ON_ERROR_STOP": "on",
			"QUIET":         "on",
		},
		[]string{"--dbname=postgres"})

	log.V(1).Info("disabled pg_cron", "stdout", stdout, "stderr", stderr)

	return err
}

func PostgreSQLParameters(outParameters *postgres.Parameters) {
	outParameters.Mandatory.AppendToList("shared_preload_libraries", "pg_cron")
}
