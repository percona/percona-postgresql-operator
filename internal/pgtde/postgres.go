package pgtde

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/pkg/errors"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"

	"github.com/percona/percona-postgresql-operator/v3/internal/logging"
	"github.com/percona/percona-postgresql-operator/v3/internal/naming"
	"github.com/percona/percona-postgresql-operator/v3/internal/postgres"
	"github.com/percona/percona-postgresql-operator/v3/internal/util"
	crunchyv1beta1 "github.com/percona/percona-postgresql-operator/v3/pkg/apis/upstream.pgv2.percona.com/v1beta1"
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

// ReconcileExtension installs or drops the pg_tde extension according to the spec.
func ReconcileExtension(ctx context.Context, exec postgres.Executor, cluster *crunchyv1beta1.PostgresCluster) error {
	if !cluster.Spec.Extensions.PGTDE.Enabled {
		return disableInPostgreSQL(ctx, exec)
	}

	return enableInPostgreSQL(ctx, exec)
}

// ReportExtension records the outcome of a ReconcileExtension.
// The PGTDEEnabled condition decides whether pg_tde is in shared_preload_libraries
// and whether instance Pods carry the vault volume.
func ReportExtension(cluster *crunchyv1beta1.PostgresCluster, record record.EventRecorder, err error) {
	enabled := cluster.Spec.Extensions.PGTDE.Enabled

	if err != nil {
		// Leave the condition alone: a failed DROP means the extension is
		// still installed, and a failed CREATE means whatever was there
		// before still is.
		if enabled {
			record.Event(cluster, corev1.EventTypeWarning,
				"PGTDEInstallFailed", "Unable to install pg_tde")
		} else {
			record.Event(cluster, corev1.EventTypeWarning,
				"PGTDEDisableFailed", "Unable to disable pg_tde")
		}
		return
	}

	condition := metav1.Condition{
		Type:               crunchyv1beta1.PGTDEEnabled,
		Status:             metav1.ConditionTrue,
		Reason:             "Enabled",
		Message:            "pg_tde is enabled in PerconaPGCluster",
		ObservedGeneration: cluster.GetGeneration(),
	}
	if !enabled {
		condition.Status = metav1.ConditionFalse
		condition.Reason = "Disabled"
		condition.Message = "pg_tde is disabled in PerconaPGCluster"
	}
	meta.SetStatusCondition(&cluster.Status.Conditions, condition)
}

// readOnlyPSQLArgs make psql print a single value and nothing else, so the
// result of a one-row, one-column query is exactly "t" or "f".
var readOnlyPSQLArgs = []string{"--no-align", "--tuples-only"}

// ObserveExtension reports whether pg_tde is installed.
func ObserveExtension(ctx context.Context, exec postgres.Executor) (bool, error) {
	log := logging.FromContext(ctx)

	stdout, stderr, err := exec.Exec(ctx,
		strings.NewReader(`SELECT EXISTS (SELECT 1 FROM pg_catalog.pg_extension WHERE extname = 'pg_tde')`),
		map[string]string{
			"ON_ERROR_STOP": "on", // Abort when any one statement fails.
			"QUIET":         "on", // Do not print successful statements to stdout.
		}, readOnlyPSQLArgs)

	log.V(1).Info("observed pg_tde extension", "stdout", stdout, "stderr", stderr)

	if err != nil {
		return false, errors.Wrap(err, psqlStderr(stderr))
	}

	switch strings.TrimSpace(stdout) {
	case "t":
		return true, nil
	case "f":
		return false, nil
	}

	return false, errors.Errorf("unexpected result from pg_extension: %q", stdout)
}

// VerifyPrincipalKey asks pg_tde to fetch the principal key its key provider names.
func VerifyPrincipalKey(ctx context.Context, exec postgres.Executor) error {
	log := logging.FromContext(ctx)

	stdout, stderr, err := exec.Exec(ctx,
		strings.NewReader(`SELECT pg_tde_verify_default_key()`),
		map[string]string{
			"ON_ERROR_STOP": "on", // Abort when any one statement fails.
			"QUIET":         "on", // Do not print successful statements to stdout.
		}, readOnlyPSQLArgs)

	log.V(1).Info("verified pg_tde principal key", "stdout", stdout, "stderr", stderr)

	if err != nil {
		return errors.Wrap(err, psqlStderr(stderr))
	}

	return nil
}

func psqlStderr(stderr string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(stderr), "\n")
	if line == "" {
		return "psql failed"
	}
	return line
}

// ReportStandby records the pg_tde state of a cluster in recovery from what its
// standby leader reported.
func ReportStandby(
	cluster *crunchyv1beta1.PostgresCluster, installed bool, keyErr error) {
	pgTDE := cluster.Spec.Extensions.PGTDE

	extension := metav1.Condition{
		Type:               crunchyv1beta1.PGTDEEnabled,
		Status:             metav1.ConditionTrue,
		Reason:             "ReplicatedFromSource",
		Message:            "pg_tde arrived with the data replicated from the source cluster",
		ObservedGeneration: cluster.GetGeneration(),
	}

	switch {
	case installed && !pgTDE.Enabled:
		// Stay true. This condition is what holds pg_tde in
		// shared_preload_libraries and the vault credentials on the Pods, and a
		// cluster replaying encrypted data cannot read it without both.
		extension.Reason = "RecoveryCannotDrop"
		extension.Message = "pg_tde is disabled in PerconaPGCluster but the extension is still" +
			" installed; a cluster in recovery cannot drop it. Disable pg_tde on the source" +
			" cluster, or promote this one first."

	case !installed && pgTDE.Enabled:
		extension.Status = metav1.ConditionFalse
		extension.Reason = "RecoveryCannotInstall"
		extension.Message = "pg_tde is enabled in PerconaPGCluster but the extension is not" +
			" installed; a cluster in recovery cannot install it. Enable pg_tde on the source" +
			" cluster."

	case !installed && !pgTDE.Enabled:
		extension.Status = metav1.ConditionFalse
		extension.Reason = "Disabled"
		extension.Message = "pg_tde is disabled in PerconaPGCluster"
	}

	meta.SetStatusCondition(&cluster.Status.Conditions, extension)

	if !pgTDE.Enabled || pgTDE.Vault == nil || cluster.Status.PGTDERevision != "" {
		return
	}

	provider := metav1.Condition{
		Type:               crunchyv1beta1.PGTDEVaultProviderReady,
		Status:             metav1.ConditionTrue,
		Reason:             "ReplicatedFromSource",
		Message:            "pg_tde fetched its principal key using the replicated key provider",
		ObservedGeneration: cluster.GetGeneration(),
	}

	switch {
	case !installed:
		provider.Status = metav1.ConditionFalse
		provider.Reason = "ExtensionNotInstalled"
		provider.Message = "pg_tde is not installed, so it has no key provider"

	case keyErr != nil:
		provider.Status = metav1.ConditionFalse
		provider.Reason = "KeyUnavailable"
		provider.Message = "pg_tde could not fetch its principal key on the standby leader: " +
			keyErr.Error()
	}

	meta.SetStatusCondition(&cluster.Status.Conditions, provider)
}

func PostgreSQLParameters(cluster *crunchyv1beta1.PostgresCluster, outParameters *postgres.Parameters) {
	outParameters.Mandatory.AppendToList("shared_preload_libraries", "pg_tde")

	canEnableWALEncryption := meta.IsStatusConditionTrue(cluster.Status.Conditions, crunchyv1beta1.PGTDEEnabled) &&
		meta.IsStatusConditionTrue(cluster.Status.Conditions, crunchyv1beta1.PGTDEVaultProviderReady)

	if cluster.IsStandby() {
		canEnableWALEncryption = !meta.IsStatusConditionFalse(
			cluster.Status.Conditions, crunchyv1beta1.PGTDEEnabled)
	}

	if cluster.Spec.Extensions.PGTDE.WALEncryption && canEnableWALEncryption {
		outParameters.Mandatory.Add("pg_tde.wal_encrypt", "on")
	} else {
		outParameters.Mandatory.Add("pg_tde.wal_encrypt", "off")
	}
}

// VaultCredentialPaths returns the standard volume mount paths for the vault
// token and CA certificate based on the vault spec's secret key names.
func VaultCredentialPaths(vault *crunchyv1beta1.PGTDEVaultSpec) (tokenPath, caPath string) {
	tokenPath = naming.PGTDEMountPath + "/" + vault.TokenSecret.Key
	if vault.HasCA() {
		caPath = naming.PGTDEMountPath + "/" + vault.CASecret.Key
	}
	return tokenPath, caPath
}

// TempVaultCredentialPaths returns the temporary file paths used during a vault
// provider change, before the pod volume is updated with new credentials.
func TempVaultCredentialPaths(vault *crunchyv1beta1.PGTDEVaultSpec) (tokenPath, caPath string) {
	tokenPath = TempTokenPath
	if vault.HasCA() {
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
		log.V(1).Info("could not add pg_tde vault provider, rewriting the existing one", "error", addErr.Error())

		if err := changeVaultProvider(ctx, exec, vault, tokenPath, caPath); err != nil {
			// Neither statement worked, so the provider is not usable. The
			// failure to add it is the more useful of the two to report.
			return errors.Wrap(addErr, "add vault provider")
		}
	}

	// Creating the key fails when it already exists, which is expected for a
	// recreated cluster. Setting it as the default is the real test of whether
	// the provider and the key are usable, so defer to that result.
	createErr := createGlobalKey(ctx, exec, cluster.UID)

	if err := setDefaultKey(ctx, exec, cluster.UID); err != nil {
		if createErr != nil {
			return errors.Wrap(createErr, "create global key")
		}
		return errors.Wrap(err, "set default key")
	}

	return nil
}

// Phase is where a cluster stands in the two-phase vault credential change
// described on reconcilePGTDEProviders.
type Phase int

const (
	// InitialSetup means no key provider has been configured yet, so the
	// credentials in the spec are the only ones there have ever been.
	InitialSetup Phase = iota

	// Configured means the key provider names the credentials in the spec.
	Configured

	// StageCredentials means the spec names credentials the key provider
	// has not been pointed at yet. Phase 1 has to copy them onto the data
	// volumes and repoint the provider before the Pods may mount them.
	StageCredentials

	// Finalize means the key provider names the staged copies on the data
	// volumes. Phase 2 repoints it at the mount paths and removes them.
	Finalize
)

// vaultChange is the state of a vault credential change. reconcileInstance
// decides whether to hold the Pods' vault volume from it and
// reconcilePGTDEProviders decides which SQL to run; the two have to agree,
// because releasing the volume in a phase that still expects the old
// credentials mounted is what leaves pg_tde unable to fetch its key.
type vaultChange struct {
	Phase Phase

	// Paths inside the Pod. The standard pair are the projected Secret's mount
	// paths; the temp pair are the copies staged on the data volume.
	TokenPath, CAPath         string
	TempTokenPath, TempCAPath string

	// Revisions matching each pair of paths, to compare with
	// cluster.Status.PGTDERevision.
	StandardRevision, TempRevision string
}

// VaultChangeFor derives the change from the spec and the stored revision.
func VaultChangeFor(cluster *crunchyv1beta1.PostgresCluster) (vaultChange, error) {
	var change vaultChange
	var err error

	vault := cluster.Spec.Extensions.PGTDE.Vault
	change.TokenPath, change.CAPath = VaultCredentialPaths(vault)
	change.TempTokenPath, change.TempCAPath = TempVaultCredentialPaths(vault)

	if change.StandardRevision, err = VaultRevision(
		vault, change.TokenPath, change.CAPath); err != nil {
		return change, err
	}
	if change.TempRevision, err = VaultRevision(
		vault, change.TempTokenPath, change.TempCAPath); err != nil {
		return change, err
	}

	switch cluster.Status.PGTDERevision {
	case "":
		change.Phase = InitialSetup
	case change.StandardRevision:
		change.Phase = Configured
	case change.TempRevision:
		change.Phase = Finalize
	default:
		change.Phase = StageCredentials
	}

	return change, nil
}

// VaultRevision computes a hash of the vault configuration and credential
// paths for comparing with cluster.Status.PGTDERevision.
func VaultRevision(vault *crunchyv1beta1.PGTDEVaultSpec, tokenPath, caPath string) (string, error) {
	return util.SafeHash32(func(hasher io.Writer) error {
		_, err := fmt.Fprintf(hasher, "%q%q%q%q%q%q%q%q",
			vault.Host, vault.MountPath,
			vault.TokenSecret.Name, vault.TokenSecret.Key,
			vault.CASecret.Name, vault.CASecret.Key,
			tokenPath, caPath)
		return err
	})
}

// PreserveOldTDEVolume replaces the pg-tde volume and its mount on the database
// container with the ones from the StatefulSet as it exists in the cluster,
// adding them back when the new pod spec no longer has them. This prevents pods
// from restarting with new vault credentials before the vault provider change
// SQL has been executed, and from restarting with no credentials at all while
// the extension is still installed.
func PreserveOldTDEVolume(podSpec *corev1.PodSpec, existing *appsv1.StatefulSet) {
	var oldVolume *corev1.Volume
	for i := range existing.Spec.Template.Spec.Volumes {
		if existing.Spec.Template.Spec.Volumes[i].Name == naming.PGTDEVolume {
			oldVolume = &existing.Spec.Template.Spec.Volumes[i]
			break
		}
	}
	if oldVolume == nil {
		return
	}

	replaced := false
	for i := range podSpec.Volumes {
		if podSpec.Volumes[i].Name == naming.PGTDEVolume {
			podSpec.Volumes[i] = *oldVolume
			replaced = true
			break
		}
	}
	if !replaced {
		podSpec.Volumes = append(podSpec.Volumes, *oldVolume)
	}

	// The volume is only ever mounted into the database container.
	var oldMount *corev1.VolumeMount
	for _, container := range existing.Spec.Template.Spec.Containers {
		if container.Name == naming.ContainerDatabase {
			for i := range container.VolumeMounts {
				if container.VolumeMounts[i].Name == naming.PGTDEVolume {
					oldMount = &container.VolumeMounts[i]
					break
				}
			}
			break
		}
	}
	if oldMount == nil {
		return
	}

	for i := range podSpec.Containers {
		if podSpec.Containers[i].Name != naming.ContainerDatabase {
			continue
		}

		mounts := podSpec.Containers[i].VolumeMounts
		replaced := false
		for j := range mounts {
			if mounts[j].Name == naming.PGTDEVolume {
				mounts[j] = *oldMount
				replaced = true
				break
			}
		}
		if !replaced {
			podSpec.Containers[i].VolumeMounts = append(mounts, *oldMount)
		}
		break
	}
}
