package pgtde

import (
	"bytes"
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/percona/percona-postgresql-operator/v2/internal/controller/runtime"
	"github.com/percona/percona-postgresql-operator/v2/pkg/apis/upstream.pgv2.percona.com/v1beta1"
)

func secretValue(
	ctx context.Context,
	k8sClient client.Reader,
	namespace string,
	secretRef v1beta1.PGTDESecretObjectReference,
) ([]byte, error) {
	secret := &corev1.Secret{}
	if err := k8sClient.Get(ctx, client.ObjectKey{
		Namespace: namespace,
		Name:      secretRef.Name,
	}, secret); err != nil {
		return nil, errors.Wrapf(err, "get secret %q", secretRef.Name)
	}

	data, ok := secret.Data[secretRef.Key]
	if !ok {
		return nil, errors.Errorf("key %q not found in secret %q", secretRef.Key, secretRef.Name)
	}

	return data, nil
}

func writeTempFile(
	ctx context.Context,
	podExec runtime.PodExecutor,
	pod *corev1.Pod,
	container string,
	destPath string,
	data []byte,
) error {
	// umask makes the file unreadable by other users from the moment it is
	// created; chmod after the write would leave a window where it is not.
	// The byte count is echoed back so a short write is not mistaken for a
	// complete one: pg_tde would then authenticate with a truncated token.
	var stdout, stderr bytes.Buffer
	err := podExec(ctx, pod.Namespace, pod.Name, container,
		bytes.NewReader(data), &stdout, &stderr,
		"bash", "-ceu", fmt.Sprintf("umask 077; cat > %s; wc -c < %s", destPath, destPath))
	if err != nil {
		return errors.Wrapf(err, "write %s: %s", destPath, stderr.String())
	}

	written, err := strconv.Atoi(strings.TrimSpace(stdout.String()))
	if err != nil {
		return errors.Wrapf(err, "check size of %s", destPath)
	}
	if written != len(data) {
		return errors.Errorf("wrote %d of %d bytes to %s", written, len(data), destPath)
	}

	return nil
}

func removeTempFiles(
	ctx context.Context,
	podExec runtime.PodExecutor,
	pod *corev1.Pod,
	container string,
	paths ...string,
) error {
	var stdout, stderr bytes.Buffer
	err := podExec(ctx, pod.Namespace, pod.Name, container,
		nil, &stdout, &stderr,
		"bash", "-ceu", fmt.Sprintf("rm -f %s", strings.Join(paths, " ")))
	if err != nil {
		return errors.Wrapf(err, "remove %s: %s", strings.Join(paths, ", "), stderr.String())
	}
	return nil
}
