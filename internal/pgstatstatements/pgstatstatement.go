package pgstatstatements

import (
	"context"

	"github.com/percona/percona-postgresql-operator/v3/internal/logging"
	"github.com/percona/percona-postgresql-operator/v3/internal/postgres"
	"github.com/percona/percona-postgresql-operator/v3/pkg/apis/upstream.pgv2.percona.com/v1beta1"
)

func EnableInPostgreSQL(ctx context.Context, exec postgres.Executor) error {
	log := logging.FromContext(ctx)

	stdout, stderr, err := exec.ExecInAllDatabases(ctx,
		`SET client_min_messages = WARNING; CREATE EXTENSION IF NOT EXISTS pg_stat_statements;`,
		map[string]string{
			"ON_ERROR_STOP": "on", // Abort when any one command fails.
			"QUIET":         "on", // Do not print successful commands to stdout.
		})

	log.V(1).Info("enabled pg_stat_statements", "stdout", stdout, "stderr", stderr)

	return err
}

func DisableInPostgreSQL(ctx context.Context, exec postgres.Executor) error {
	log := logging.FromContext(ctx)

	stdout, stderr, err := exec.ExecInAllDatabases(ctx,
		`SET client_min_messages = WARNING; DROP EXTENSION IF EXISTS pg_stat_statements;`,
		map[string]string{
			"ON_ERROR_STOP": "on", // Abort when any one command fails.
			"QUIET":         "on", // Do not print successful commands to stdout.
		})

	log.V(1).Info("disabled pg_stat_statements", "stdout", stdout, "stderr", stderr)

	return err
}

func PostgreSQLParameters(cluster *v1beta1.PostgresCluster, outParameters *postgres.Parameters) {
	outParameters.Mandatory.AppendToList("shared_preload_libraries", "pg_stat_statements")

	// K8SPG-1198
	if cluster.CompareVersion("3.2.0") >= 0 {
		outParameters.Default.Add("pg_stat_statements.track", "top")
		return
	}
	outParameters.Mandatory.Add("pg_stat_statements.track", "all")
}
