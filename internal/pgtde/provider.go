package pgtde

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/percona/percona-postgresql-operator/v2/internal/controller/runtime"
	"github.com/percona/percona-postgresql-operator/v2/internal/postgres"
	crunchyv1beta1 "github.com/percona/percona-postgresql-operator/v2/pkg/apis/upstream.pgv2.percona.com/v1beta1"
)

type KeyProvider interface {
	Reconcile(context.Context, postgres.Executor, *crunchyv1beta1.PostgresCluster, CredentialPath) error
	GetRevision(CredentialPath) (string, error)
	GetCredentialPath() CredentialPath
	GetStagedCredentialPath() CredentialPath
	StageCredentials(context.Context, client.Client, runtime.PodExecutor, string, []*corev1.Pod, string, CredentialPath) error
	CleanupStagedCredentials(context.Context, runtime.PodExecutor, []*corev1.Pod, string, CredentialPath) error
}

type VaultProviderCredentialPath struct {
	TokenPath string
	CAPath    string
}

type FileProviderCredentialPath struct {
	KeyPath string
}

type CredentialPath struct {
	VaultProvider *VaultProviderCredentialPath
	FileProvider  *FileProviderCredentialPath
}

func NewProviderForCluster(cluster *crunchyv1beta1.PostgresCluster) KeyProvider {
	switch {
	case cluster.Spec.Extensions.PGTDE.Vault != nil:
		return NewVaultProvider(cluster.Spec.Extensions.PGTDE.Vault)
	case cluster.Spec.Extensions.PGTDE.File != nil:
		return NewFileProvider(cluster.Spec.Extensions.PGTDE.File)
	default:
		return nil
	}
}
