package pgtde

import (
	"context"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"

	"github.com/percona/percona-postgresql-operator/v2/internal/logging"
	"github.com/percona/percona-postgresql-operator/v2/internal/naming"
	"github.com/percona/percona-postgresql-operator/v2/internal/postgres"
	crunchyv1beta1 "github.com/percona/percona-postgresql-operator/v2/pkg/apis/upstream.pgv2.percona.com/v1beta1"
)

const (
	// TempTokenPath is where the new vault token is written inside the pod
	// during a vault provider change (before the volume is updated).
	// Stored under /pgdata so it survives pod restarts (persistent volume).
	TempTokenPath = "/pgdata/tde-new-token" // nolint:gosec
	// TempCAPath is where the new CA certificate is written inside the pod
	// during a vault provider change (before the volume is updated).
	// Stored under /pgdata so it survives pod restarts (persistent volume).
	TempCAPath = "/pgdata/tde-new-ca.crt"
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

func ReconcileExtension(ctx context.Context, exec postgres.Executor, record record.EventRecorder, cluster *crunchyv1beta1.PostgresCluster) error {
	if !cluster.Spec.Extensions.PGTDE.Enabled {
		err := disableInPostgreSQL(ctx, exec)
		if err != nil {
			record.Event(cluster, corev1.EventTypeWarning, "pgTdeEnabled", "Unable to disable pg_tde")
			return err
		}

		meta.SetStatusCondition(&cluster.Status.Conditions, metav1.Condition{
			Type:               crunchyv1beta1.PGTDEEnabled,
			Status:             metav1.ConditionFalse,
			Reason:             "Disabled",
			Message:            "pg_tde is disabled in PerconaPGCluster",
			ObservedGeneration: cluster.GetGeneration(),
		})

		return nil
	}

	err := enableInPostgreSQL(ctx, exec)
	if err != nil {
		record.Event(cluster, corev1.EventTypeWarning, "pgTdeDisabled", "Unable to install pg_tde")
		return err
	}

	meta.SetStatusCondition(&cluster.Status.Conditions, metav1.Condition{
		Type:               crunchyv1beta1.PGTDEEnabled,
		Status:             metav1.ConditionTrue,
		Reason:             "Enabled",
		Message:            "pg_tde is enabled in PerconaPGCluster",
		ObservedGeneration: cluster.GetGeneration(),
	})

	return nil
}

func PostgreSQLParameters(outParameters *postgres.Parameters) {
	outParameters.Mandatory.AppendToList("shared_preload_libraries", "pg_tde")
	outParameters.Mandatory.Add("pg_tde.wal_encrypt", "off")
}

// VaultCredentialPaths returns the standard volume mount paths for the vault
// token and CA certificate based on the vault spec's secret key names.
func VaultCredentialPaths(vault *crunchyv1beta1.PGTDEVaultSpec) (tokenPath, caPath string) {
	tokenPath = naming.PGTDEMountPath + "/" + vault.TokenSecret.Key
	if vault.CASecret.Key != "" {
		caPath = naming.PGTDEMountPath + "/" + vault.CASecret.Key
	}
	return tokenPath, caPath
}

// TempVaultCredentialPaths returns the temporary file paths used during a vault
// provider change, before the pod volume is updated with new credentials.
func TempVaultCredentialPaths(vault *crunchyv1beta1.PGTDEVaultSpec) (tokenPath, caPath string) {
	tokenPath = TempTokenPath
	if vault.CASecret.Name != "" && vault.CASecret.Key != "" {
		caPath = TempCAPath
	}
	return tokenPath, caPath
}

func addVaultProvider(ctx context.Context, exec postgres.Executor, vault *crunchyv1beta1.PGTDEVaultSpec, tokenPath, caPath string) error {
	log := logging.FromContext(ctx)

	stdout, stderr, err := exec.Exec(ctx,
		strings.NewReader(strings.Join([]string{
			// Quiet NOTICE messages from IF NOT EXISTS statements.
			// - https://www.postgresql.org/docs/current/runtime-config-client.html
			`SET client_min_messages = WARNING;`,
			`SELECT pg_tde_add_global_key_provider_vault_v2(
			    :'provider_name', :'vault_host', :'vault_mount_path', :'token_path', NULLIF(:'ca_path', '')
			);`,
		}, "\n")),
		map[string]string{
			"ON_ERROR_STOP":    "on", // Abort when any one statement fails.
			"QUIET":            "on", // Do not print successful statements to stdout.
			"provider_name":    naming.PGTDEVaultProvider,
			"vault_host":       vault.Host,
			"vault_mount_path": vault.MountPath,
			"token_path":       tokenPath,
			"ca_path":          caPath,
		}, nil)

	if err != nil {
		log.Info("failed to add pg_tde vault provider", "stdout", stdout, "stderr", stderr)
	} else {
		log.Info("added pg_tde vault provider", "stdout", stdout, "stderr", stderr)
	}

	return err
}

func createGlobalKey(ctx context.Context, exec postgres.Executor, clusterID types.UID) error {
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
			"provider_name": naming.PGTDEVaultProvider,
			"global_key":    globalKey,
		}, nil)

	if err != nil {
		log.Info("failed to create global key", "globalKey", globalKey, "stdout", stdout, "stderr", stderr)
	} else {
		log.Info("created global key", "globalKey", globalKey, "stdout", stdout, "stderr", stderr)
	}

	return err
}

func setDefaultKey(ctx context.Context, exec postgres.Executor, clusterID types.UID) error {
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
			"provider_name": naming.PGTDEVaultProvider,
			"global_key":    globalKey,
		}, nil)

	if err != nil {
		log.Info("failed to set global key", "globalKey", globalKey, "stdout", stdout, "stderr", stderr)
	} else {
		log.Info("set global key", "globalKey", globalKey, "stdout", stdout, "stderr", stderr)
	}

	return err
}

func changeVaultProvider(ctx context.Context, exec postgres.Executor, vault *crunchyv1beta1.PGTDEVaultSpec, tokenPath, caPath string) error {
	log := logging.FromContext(ctx)

	stdout, stderr, err := exec.Exec(ctx,
		strings.NewReader(strings.Join([]string{
			// Quiet NOTICE messages from IF NOT EXISTS statements.
			// - https://www.postgresql.org/docs/current/runtime-config-client.html
			`SET client_min_messages = WARNING;`,
			`SELECT pg_tde_change_global_key_provider_vault_v2(
			    :'provider_name', :'vault_host', :'vault_mount_path', :'token_path', NULLIF(:'ca_path', '')
			);`,
		}, "\n")),
		map[string]string{
			"ON_ERROR_STOP":    "on", // Abort when any one statement fails.
			"QUIET":            "on", // Do not print successful statements to stdout.
			"provider_name":    naming.PGTDEVaultProvider,
			"vault_host":       vault.Host,
			"vault_mount_path": vault.MountPath,
			"token_path":       tokenPath,
			"ca_path":          caPath,
		}, nil)

	if err != nil {
		log.Info("failed to change pg_tde vault provider", "stdout", stdout, "stderr", stderr)
	} else {
		log.Info("changed pg_tde vault provider", "stdout", stdout, "stderr", stderr)
	}

	return err
}

// ReconcileVaultProvider configures or updates the pg_tde vault key provider.
// tokenPath and caPath are the file paths inside the pod where the vault
// credentials can be read. For initial setup these are the standard volume
// mount paths; for provider changes they may be temporary file paths.
//
// The provider and the global key may already exist even on the initial setup
// path: a cluster that is deleted and recreated with its PVCs retained, or one
// where pg_tde was disabled and re-enabled, starts with an empty PGTDERevision
// but a populated pg_tde state. Rather than interpreting the error text to
// recognize those cases, each step recovers from a failure by driving the state
// towards the spec and lets the following step decide whether that worked.
func ReconcileVaultProvider(ctx context.Context, exec postgres.Executor, cluster *crunchyv1beta1.PostgresCluster, tokenPath, caPath string) error {
	log := logging.FromContext(ctx)
	vault := cluster.Spec.Extensions.PGTDE.Vault

	if cluster.Status.PGTDERevision != "" {
		return changeVaultProvider(ctx, exec, vault, tokenPath, caPath)
	}

	if addErr := addVaultProvider(ctx, exec, vault, tokenPath, caPath); addErr != nil {
		// The provider probably exists already. Its configuration belongs to
		// whatever created it, which may be an older incarnation of this
		// cluster pointing at a different Vault; overwrite it so it matches
		// the spec instead of assuming it already does.
		log.V(1).Info("could not add pg_tde vault provider, rewriting the existing one",
			"error", addErr.Error())

		if err := changeVaultProvider(ctx, exec, vault, tokenPath, caPath); err != nil {
			// Neither statement worked, so the provider is not usable. The
			// failure to add it is the more useful of the two to report.
			return addErr
		}
	}

	// Creating the key fails when it already exists, which is expected for a
	// recreated cluster. Setting it as the default is the real test of whether
	// the provider and the key are usable, so defer to that result.
	createErr := createGlobalKey(ctx, exec, cluster.UID)

	if err := setDefaultKey(ctx, exec, cluster.UID); err != nil {
		if createErr != nil {
			return createErr
		}
		return err
	}

	return nil
}
