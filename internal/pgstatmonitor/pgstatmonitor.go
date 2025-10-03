package pgstatmonitor

import (
	"context"
	"strings"

	"github.com/percona/percona-postgresql-operator/v2/internal/logging"
	"github.com/percona/percona-postgresql-operator/v2/internal/postgres"
)

func EnableInPostgreSQL(ctx context.Context, exec postgres.Executor) error {
	log := logging.FromContext(ctx)

	stdout, stderr, err := exec.ExecInAllDatabases(ctx,
		`SET client_min_messages = WARNING; CREATE EXTENSION IF NOT EXISTS pg_stat_monitor; ALTER EXTENSION pg_stat_monitor UPDATE;`,
		map[string]string{
			"ON_ERROR_STOP": "on", // Abort when any one command fails.
			"QUIET":         "on", // Do not print successful commands to stdout.
		})

	log.V(1).Info("enabled pg_stat_monitor", "stdout", stdout, "stderr", stderr)

	return err
}

func DisableInPostgreSQL(ctx context.Context, exec postgres.Executor) error {
	log := logging.FromContext(ctx)

	stdout, stderr, err := exec.ExecInAllDatabases(ctx,
		`SET client_min_messages = WARNING; DROP EXTENSION IF EXISTS pg_stat_monitor;`,
		map[string]string{
			"ON_ERROR_STOP": "on", // Abort when any one command fails.
			"QUIET":         "on", // Do not print successful commands to stdout.
		})

	log.V(1).Info("disabled pg_stat_monitor", "stdout", stdout, "stderr", stderr)

	return err
}

func PostgreSQLParameters(outParameters *postgres.Parameters) {

	shared := outParameters.Mandatory.Value("shared_preload_libraries")
	outParameters.Mandatory.Add("shared_preload_libraries",
		strings.TrimPrefix(shared+",pg_stat_monitor", ","))
	outParameters.Mandatory.Add("pg_stat_monitor.pgsm_query_max_len", "2048")
}
