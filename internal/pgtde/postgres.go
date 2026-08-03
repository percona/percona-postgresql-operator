package pgtde

import (
	"context"
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/types"

	"github.com/percona/percona-postgresql-operator/v2/internal/logging"
	"github.com/percona/percona-postgresql-operator/v2/internal/naming"
	"github.com/percona/percona-postgresql-operator/v2/internal/postgres"
	crunchyv1beta1 "github.com/percona/percona-postgresql-operator/v2/pkg/apis/upstream.pgv2.percona.com/v1beta1"
)

// enableInPostgreSQL installs pg_tde extension in every database.
func enableInPostgreSQL(ctx context.Context, exec postgres.Executor) error {
	log := logging.FromContext(ctx)

	stdout, stderr, err := exec.ExecInAllDatabases(ctx,
		strings.Join([]string{
			`SET client_min_messages = WARNING;`,
			`CREATE EXTENSION IF NOT EXISTS pg_tde;`,
			`ALTER EXTENSION pg_tde UPDATE;`,
		}, "\n"),
		map[string]string{
			"ON_ERROR_STOP": "on", // Abort when any one command fails.
			"QUIET":         "on", // Do not print successful commands to stdout.
		})

	log.V(1).Info("enabled pg_tde", "stdout", stdout, "stderr", stderr)

	return err
}

func disableInPostgreSQL(ctx context.Context, exec postgres.Executor) error {
	log := logging.FromContext(ctx)

	stdout, stderr, err := exec.ExecInAllDatabases(ctx,
		strings.Join([]string{
			`SET client_min_messages = WARNING;`,
			`DROP EXTENSION IF EXISTS pg_tde;`,
		}, "\n"),
		map[string]string{
			"ON_ERROR_STOP": "on", // Abort when any one command fails.
			"QUIET":         "on", // Do not print successful commands to stdout.
		})

	log.V(1).Info("disabled pg_tde", "stdout", stdout, "stderr", stderr)

	return err
}

// ReconcileExtension installs or drops the pg_tde extension according to the spec.
func ReconcileExtension(ctx context.Context, exec postgres.Executor, cluster *crunchyv1beta1.PostgresCluster) error {
	if !cluster.Spec.Extensions.PGTDE.Enabled {
		return disableInPostgreSQL(ctx, exec)
	}

	return enableInPostgreSQL(ctx, exec)
}

func PostgreSQLParameters(outParameters *postgres.Parameters) {
	outParameters.Mandatory.AppendToList("shared_preload_libraries", "pg_tde")
	outParameters.Mandatory.Add("pg_tde.wal_encrypt", "off")
}

func createGlobalKey(ctx context.Context, exec postgres.Executor, clusterID types.UID, providerName string) error {
	log := logging.FromContext(ctx)

	globalKey := fmt.Sprintf("%s-%s", naming.PGTDEGlobalKey, clusterID)

	stdout, stderr, err := exec.Exec(ctx,
		strings.NewReader(strings.Join([]string{
			// Quiet NOTICE messages from IF NOT EXISTS statements.
			// - https://www.postgresql.org/docs/current/runtime-config-client.html
			`SET client_min_messages = WARNING;`,
			`SELECT pg_tde_create_key_using_global_key_provider(:'global_key', :'provider_name');`,
		}, "\n")),
		map[string]string{
			"ON_ERROR_STOP": "on", // Abort when any one statement fails.
			"QUIET":         "on", // Do not print successful statements to stdout.
			"provider_name": providerName,
			"global_key":    globalKey,
		}, nil)

	if err != nil {
		log.Info("failed to create global key", "globalKey", globalKey, "stdout", stdout, "stderr", stderr)
	} else {
		log.Info("created global key", "globalKey", globalKey, "stdout", stdout, "stderr", stderr)
	}

	return err
}

func setDefaultKey(ctx context.Context, exec postgres.Executor, clusterID types.UID, providerName string) error {
	log := logging.FromContext(ctx)

	globalKey := fmt.Sprintf("%s-%s", naming.PGTDEGlobalKey, clusterID)

	stdout, stderr, err := exec.Exec(ctx,
		strings.NewReader(strings.Join([]string{
			// Quiet NOTICE messages from IF NOT EXISTS statements.
			// - https://www.postgresql.org/docs/current/runtime-config-client.html
			`SET client_min_messages = WARNING;`,
			`SELECT pg_tde_set_default_key_using_global_key_provider(:'global_key', :'provider_name');`,
		}, "\n")),
		map[string]string{
			"ON_ERROR_STOP": "on", // Abort when any one statement fails.
			"QUIET":         "on", // Do not print successful statements to stdout.
			"provider_name": providerName,
			"global_key":    globalKey,
		}, nil)

	if err != nil {
		log.Info("failed to set global key", "globalKey", globalKey, "stdout", stdout, "stderr", stderr)
	} else {
		log.Info("set global key", "globalKey", globalKey, "stdout", stdout, "stderr", stderr)
	}

	return err
}
