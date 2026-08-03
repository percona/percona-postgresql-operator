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

type fileProvider struct {
	file *crunchyv1beta1.PGTDEFileSpec
}

func NewFileProvider(file *crunchyv1beta1.PGTDEFileSpec) KeyProvider {
	return &fileProvider{
		file: file,
	}
}

func (p *fileProvider) Reconcile(ctx context.Context, exec postgres.Executor, cluster *crunchyv1beta1.PostgresCluster, path CredentialPath) error {
	log := logging.FromContext(ctx)

	keyPath := path.FileProvider.KeyPath

	if cluster.Status.PGTDERevision != "" {
		return changeFileProvider(ctx, exec, keyPath)
	}

	if addErr := addFileProvider(ctx, exec, keyPath); addErr != nil {
		log.V(1).Info("could not add pg_tde file provider, rewriting the existing one", "error", addErr.Error())

		if err := changeFileProvider(ctx, exec, keyPath); err != nil {
			return errors.Wrap(addErr, "add file provider")
		}
	}

	createErr := createGlobalKey(ctx, exec, cluster.UID, naming.PGTDEFileProvider)

	if err := setDefaultKey(ctx, exec, cluster.UID, naming.PGTDEFileProvider); err != nil {
		if createErr != nil {
			return errors.Wrap(createErr, "create global key")
		}
		return errors.Wrap(err, "set default key")
	}

	return nil
}

func (p *fileProvider) GetCredentialPath() CredentialPath {
	return CredentialPath{
		FileProvider: &FileProviderCredentialPath{
			KeyPath: naming.PGTDEMountPath + "/" + p.file.KeySecret.Key,
		},
	}
}

func (p *fileProvider) GetStagedCredentialPath() CredentialPath {
	return CredentialPath{
		FileProvider: &FileProviderCredentialPath{
			KeyPath: "/pgdata/tde-new-key",
		},
	}
}

func (p *fileProvider) GetRevision(path CredentialPath) (string, error) {
	return util.SafeHash32(func(hasher io.Writer) error {
		_, err := fmt.Fprintf(hasher, "%q", path.FileProvider.KeyPath)
		return err
	})
}

func addFileProvider(ctx context.Context, exec postgres.Executor, keyPath string) error {
	log := logging.FromContext(ctx)

	stdout, stderr, err := exec.Exec(ctx,
		strings.NewReader(strings.Join([]string{
			// Quiet NOTICE messages from IF NOT EXISTS statements.
			// - https://www.postgresql.org/docs/current/runtime-config-client.html
			`SET client_min_messages = WARNING;`,
			`SELECT pg_tde_add_global_key_provider_file(:'provider_name', :'key_path');`,
		}, "\n")),
		map[string]string{
			"ON_ERROR_STOP": "on", // Abort when any one statement fails.
			"QUIET":         "on", // Do not print successful statements to stdout.
			"provider_name": naming.PGTDEFileProvider,
			"key_path":      keyPath,
		}, nil)

	if err != nil {
		log.Info("failed to add pg_tde fileprovider", "stdout", stdout, "stderr", stderr)
	} else {
		log.Info("added pg_tde file provider", "stdout", stdout, "stderr", stderr)
	}

	return err
}

func changeFileProvider(ctx context.Context, exec postgres.Executor, keyPath string) error {
	log := logging.FromContext(ctx)

	stdout, stderr, err := exec.Exec(ctx,
		strings.NewReader(strings.Join([]string{
			// Quiet NOTICE messages from IF NOT EXISTS statements.
			// - https://www.postgresql.org/docs/current/runtime-config-client.html
			`SET client_min_messages = WARNING;`,
			`SELECT pg_tde_change_global_key_provider_file(:'provider_name', :'key_path');`,
		}, "\n")),
		map[string]string{
			"ON_ERROR_STOP": "on", // Abort when any one statement fails.
			"QUIET":         "on", // Do not print successful statements to stdout.
			"provider_name": naming.PGTDEFileProvider,
			"key_path":      keyPath,
		}, nil)

	if err != nil {
		log.Info("failed to change pg_tde fileprovider", "stdout", stdout, "stderr", stderr)
	} else {
		log.Info("changed pg_tde file provider", "stdout", stdout, "stderr", stderr)
	}

	return err
}

func (p *fileProvider) StageCredentials(
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

	keyPath := paths.FileProvider.KeyPath
	key, err := secretValue(ctx, k8sclient, namespace, p.file.KeySecret)
	if err != nil {
		return errors.Wrap(err, "token secret")
	}
	files := []stagedFile{{path: keyPath, data: key}}

	for _, pod := range pods {
		for _, file := range files {
			if err := writeTempFile(ctx, exec, pod, container, file.path, file.data); err != nil {
				return errors.Wrapf(err, "pod %s", pod.Name)
			}
		}
	}

	return nil
}

func (p *fileProvider) CleanupStagedCredentials(ctx context.Context, exec runtime.PodExecutor, pods []*corev1.Pod, container string, paths CredentialPath) error {
	var err error

	for _, pod := range pods {
		keyPath := paths.FileProvider.KeyPath

		if e := removeTempFiles(ctx, exec, pod, container, keyPath); e != nil && err == nil {
			err = errors.Wrapf(e, "pod %s", pod.Name)
		}
	}

	return err
}
