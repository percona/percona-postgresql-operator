package pgtde

import (
	"context"
	"fmt"
	"strings"

	"github.com/percona/percona-postgresql-operator/v2/internal/logging"
	"github.com/percona/percona-postgresql-operator/v2/internal/naming"
	"github.com/percona/percona-postgresql-operator/v2/internal/postgres"
	crunchyv1beta1 "github.com/percona/percona-postgresql-operator/v2/pkg/apis/postgres-operator.crunchydata.com/v1beta1"
)

// EnableInPostgreSQL installs pgvector triggers into every database.
func EnableInPostgreSQL(ctx context.Context, exec postgres.Executor) error {
	log := logging.FromContext(ctx)

	stdout, stderr, err := exec.ExecInAllDatabases(ctx,
		`SET client_min_messages = WARNING; CREATE EXTENSION IF NOT EXISTS pg_tde; ALTER EXTENSION pg_tde UPDATE;`,
		map[string]string{
			"ON_ERROR_STOP": "on", // Abort when any one command fails.
			"QUIET":         "on", // Do not print successful commands to stdout.
		})

	log.V(1).Info("enabled pg_tde", "stdout", stdout, "stderr", stderr)

	return err
}

func DisableInPostgreSQL(ctx context.Context, exec postgres.Executor) error {
	log := logging.FromContext(ctx)

	stdout, stderr, err := exec.ExecInAllDatabases(ctx,
		`SET client_min_messages = WARNING; DROP EXTENSION IF EXISTS pg_tde;`,
		map[string]string{
			"ON_ERROR_STOP": "on", // Abort when any one command fails.
			"QUIET":         "on", // Do not print successful commands to stdout.
		})

	log.V(1).Info("disabled pg_tde", "stdout", stdout, "stderr", stderr)

	return err
}

func PostgreSQLParameters(outParameters *postgres.Parameters) {
	outParameters.Mandatory.AppendToList("shared_preload_libraries", "pg_tde")
}

func AddVaultProvider(ctx context.Context, exec postgres.Executor, vault *crunchyv1beta1.PGTDEVaultSpec) error {
	log := logging.FromContext(ctx)

	caSecretPath := "NULL"
	if vault.CASecret.Key != "" {
		caSecretPath = fmt.Sprintf("'%s'", naming.PGTDEMountPath+"/"+vault.CASecret.Key)
	}

	stdout, stderr, err := exec.Exec(ctx,
		strings.NewReader(strings.Join([]string{
			// Quiet NOTICE messages from IF NOT EXISTS statements.
			// - https://www.postgresql.org/docs/current/runtime-config-client.html
			`SET client_min_messages = WARNING;`,
			fmt.Sprintf(`SELECT pg_tde_add_global_key_provider_vault_v2(
			    '%s', '%s', '%s', '%s', %s
			);`,
				naming.PGTDEVaultProvider,
				vault.Host,
				vault.MountPath,
				naming.PGTDEMountPath+"/"+vault.TokenSecret.Key,
				caSecretPath,
			),
			fmt.Sprintf("SELECT pg_tde_create_key_using_global_key_provider('%s', '%s');",
				naming.PGTDEGlobalKey,
				naming.PGTDEVaultProvider,
			),
			fmt.Sprintf("SELECT pg_tde_set_default_key_using_global_key_provider('%s', '%s');",
				naming.PGTDEGlobalKey,
				naming.PGTDEVaultProvider,
			),
		}, "\n")),
		map[string]string{
			"ON_ERROR_STOP": "on", // Abort when any one statement fails.
			"QUIET":         "on", // Do not print successful statements to stdout.
		}, nil)

	if err != nil {
		log.Info("failed to add pg_tde vault provider", "stdout", stdout, "stderr", stderr)
	} else {
		log.Info("added pg_tde vault provider", "stdout", stdout, "stderr", stderr)
	}

	return err
}

func ChangeVaultProvider(ctx context.Context, exec postgres.Executor, vault *crunchyv1beta1.PGTDEVaultSpec) error {
	log := logging.FromContext(ctx)

	stdout, stderr, err := exec.Exec(ctx,
		strings.NewReader(strings.Join([]string{
			// Quiet NOTICE messages from IF NOT EXISTS statements.
			// - https://www.postgresql.org/docs/current/runtime-config-client.html
			`SET client_min_messages = WARNING;`,
			fmt.Sprintf(`SELECT pg_tde_change_global_key_provider_vault_v2(
			    'vault-provider', '%s', '%s', '%s', '%s'
			);`,
				vault.Host,
				vault.MountPath,
				naming.PGTDEMountPath+"/"+vault.TokenSecret.Key,
				naming.PGTDEMountPath+"/"+vault.CASecret.Key,
			),
		}, "\n")),
		map[string]string{
			"ON_ERROR_STOP": "on", // Abort when any one statement fails.
			"QUIET":         "on", // Do not print successful statements to stdout.
		}, nil)

	if err != nil {
		log.Info("failed to change pg_tde vault provider", "stdout", stdout, "stderr", stderr)
	} else {
		log.Info("changed pg_tde vault provider", "stdout", stdout, "stderr", stderr)
	}

	return err
}
