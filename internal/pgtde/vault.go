package pgtde

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/percona/percona-postgresql-operator/v2/internal/controller/runtime"
	"github.com/percona/percona-postgresql-operator/v2/internal/logging"
	"github.com/percona/percona-postgresql-operator/v2/internal/naming"
	"github.com/percona/percona-postgresql-operator/v2/internal/postgres"
	"github.com/percona/percona-postgresql-operator/v2/internal/util"
	crunchyv1beta1 "github.com/percona/percona-postgresql-operator/v2/pkg/apis/upstream.pgv2.percona.com/v1beta1"
)

type vaultProvider struct {
	vault *crunchyv1beta1.PGTDEVaultSpec
}

func NewVaultProvider(vault *crunchyv1beta1.PGTDEVaultSpec) KeyProvider {
	return &vaultProvider{
		vault: vault,
	}
}

func (p *vaultProvider) Reconcile(ctx context.Context, exec postgres.Executor, cluster *crunchyv1beta1.PostgresCluster, paths CredentialPath) error {
	log := logging.FromContext(ctx)
	vault := cluster.Spec.Extensions.PGTDE.Vault
	caPath := paths.VaultProvider.CAPath
	tokenPath := paths.VaultProvider.TokenPath

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
	createErr := createGlobalKey(ctx, exec, cluster.UID, naming.PGTDEVaultProvider)

	if err := setDefaultKey(ctx, exec, cluster.UID, naming.PGTDEVaultProvider); err != nil {
		if createErr != nil {
			return errors.Wrap(createErr, "create global key")
		}
		return errors.Wrap(err, "set default key")
	}

	return nil
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

func (p *vaultProvider) GetCredentialPath() CredentialPath {
	path := CredentialPath{
		VaultProvider: &VaultProviderCredentialPath{
			TokenPath: naming.PGTDEMountPath + "/" + p.vault.TokenSecret.Key,
		},
	}

	if p.vault.HasCA() {
		path.VaultProvider.CAPath = naming.PGTDEMountPath + "/" + p.vault.CASecret.Key
	}

	return path
}

const (
	tempTokenPath = "/pgdata/tde-new-token" // nolint:gosec
	tempCAPath    = "/pgdata/tde-new-ca.crt"
)

func (p *vaultProvider) GetStagedCredentialPath() CredentialPath {
	path := CredentialPath{
		VaultProvider: &VaultProviderCredentialPath{
			TokenPath: tempTokenPath,
		},
	}

	if p.vault.HasCA() {
		path.VaultProvider.CAPath = tempCAPath
	}

	return path
}

func (p *vaultProvider) GetRevision(path CredentialPath) (string, error) {
	return util.SafeHash32(func(hasher io.Writer) error {
		_, err := fmt.Fprintf(hasher, "%q%q%q%q%q%q%q%q",
			p.vault.Host, p.vault.MountPath,
			p.vault.TokenSecret.Name, p.vault.TokenSecret.Key,
			p.vault.CASecret.Name, p.vault.CASecret.Key,
			path.VaultProvider.TokenPath, path.VaultProvider.CAPath)
		return err
	})
}

func (p *vaultProvider) StageCredentials(
	ctx context.Context,
	k8sclient client.Client,
	exec runtime.PodExecutor,
	namespace string,
	pods []*corev1.Pod,
	container string, // nolint:unparam
	paths CredentialPath,
) error {
	type stagedFile struct {
		path string
		data []byte
	}

	tokenPath := paths.VaultProvider.TokenPath
	token, err := secretValue(ctx, k8sclient, namespace, p.vault.TokenSecret)
	if err != nil {
		return errors.Wrap(err, "token secret")
	}
	files := []stagedFile{{path: tokenPath, data: token}}

	if p.vault.HasCA() {
		caPath := paths.VaultProvider.CAPath
		ca, err := secretValue(ctx, k8sclient, namespace, p.vault.CASecret)
		if err != nil {
			return errors.Wrap(err, "CA secret")
		}
		files = append(files, stagedFile{path: caPath, data: ca})
	}

	for _, pod := range pods {
		for _, file := range files {
			if err := writeTempFile(ctx, exec, pod, container, file.path, file.data); err != nil {
				return errors.Wrapf(err, "pod %s", pod.Name)
			}
		}
	}

	return nil
}

func (p *vaultProvider) CleanupStagedCredentials(ctx context.Context, exec runtime.PodExecutor, pods []*corev1.Pod, container string, paths CredentialPath) error {
	var err error

	for _, pod := range pods {
		tokenPath := paths.VaultProvider.TokenPath
		caPath := paths.VaultProvider.CAPath

		if e := removeTempFiles(ctx, exec, pod, container, tokenPath, caPath); e != nil && err == nil {
			err = errors.Wrapf(e, "pod %s", pod.Name)
		}
	}

	return err
}
