package pgtde

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/pkg/errors"
	"gotest.tools/v3/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/percona/percona-postgresql-operator/v2/internal/naming"
	"github.com/percona/percona-postgresql-operator/v2/pkg/apis/upstream.pgv2.percona.com/v1beta1"
)

func TestSecretValue(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Namespace: "ns1", Name: "vault-secret"},
		Data:       map[string][]byte{"token": []byte("hvs.sometoken")},
	}
	k8s := fake.NewClientBuilder().WithObjects(secret).Build()

	t.Run("Reads", func(t *testing.T) {
		data, err := secretValue(ctx, k8s, "ns1",
			v1beta1.PGTDESecretObjectReference{Name: "vault-secret", Key: "token"})
		assert.NilError(t, err)
		assert.Equal(t, string(data), "hvs.sometoken")
	})

	t.Run("MissingSecret", func(t *testing.T) {
		_, err := secretValue(ctx, k8s, "ns1",
			v1beta1.PGTDESecretObjectReference{Name: "nope", Key: "token"})
		assert.ErrorContains(t, err, `get secret "nope"`)
	})

	t.Run("MissingKey", func(t *testing.T) {
		_, err := secretValue(ctx, k8s, "ns1",
			v1beta1.PGTDESecretObjectReference{Name: "vault-secret", Key: "nope"})
		assert.ErrorContains(t, err, `key "nope" not found in secret "vault-secret"`)
	})

	// The Secrets live beside the cluster, so reading one from another namespace
	// would hand a different tenant's key to pg_tde.
	t.Run("OtherNamespace", func(t *testing.T) {
		_, err := secretValue(ctx, k8s, "ns2",
			v1beta1.PGTDESecretObjectReference{Name: "vault-secret", Key: "token"})
		assert.ErrorContains(t, err, `get secret "vault-secret"`)
	})
}

func TestWriteTempFile(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Namespace: "ns1", Name: "pgc1-instance1-abcd-0"},
	}
	destPath := "/pgdata/tde-new-token"

	t.Run("PipesDataAndSetsMode", func(t *testing.T) {
		var calls []execCall

		assert.NilError(t, writeTempFile(ctx, execRecorder(&calls, nil), pod,
			naming.ContainerDatabase, destPath, []byte("hvs.sometoken")))

		assert.Equal(t, len(calls), 1)
		assert.Equal(t, calls[0].namespace, "ns1")
		assert.Equal(t, calls[0].pod, pod.Name)
		assert.Equal(t, calls[0].container, naming.ContainerDatabase)
		assert.Equal(t, calls[0].stdin, "hvs.sometoken",
			"the secret value should be piped in, not interpolated into the command")
		assert.DeepEqual(t, calls[0].command[:2], []string{"bash", "-ceu"})
		assert.Assert(t, strings.Contains(calls[0].command[2], destPath))
		assert.Assert(t, strings.Contains(calls[0].command[2], "umask 077"),
			"the token file must never exist in a world readable state")
	})

	t.Run("ShortWrite", func(t *testing.T) {
		// The container reports fewer bytes on disk than were sent.
		err := writeTempFile(ctx,
			func(ctx context.Context, namespace, pod, container string,
				stdin io.Reader, stdout, stderr io.Writer, command ...string,
			) error {
				_, _ = io.WriteString(stdout, "4\n")
				return nil
			},
			pod, naming.ContainerDatabase, destPath, []byte("hvs.sometoken"))

		assert.ErrorContains(t, err, "wrote 4 of 13 bytes",
			"a truncated token must not be accepted as written")
	})

	t.Run("UnreadableSize", func(t *testing.T) {
		err := writeTempFile(ctx,
			func(ctx context.Context, namespace, pod, container string,
				stdin io.Reader, stdout, stderr io.Writer, command ...string,
			) error {
				_, _ = io.WriteString(stdout, "wc: /pgdata: Is a directory\n")
				return nil
			},
			pod, naming.ContainerDatabase, destPath, []byte("x"))

		assert.ErrorContains(t, err, "check size of "+destPath)
	})

	t.Run("ExecFails", func(t *testing.T) {
		var calls []execCall

		err := writeTempFile(ctx,
			execRecorder(&calls, func(execCall) error {
				return errors.New("no such file or directory")
			}),
			pod, naming.ContainerDatabase, destPath, []byte("x"))

		assert.ErrorContains(t, err, destPath)
		assert.ErrorContains(t, err, "no such file or directory")
	})
}

func TestRemoveTempFiles(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Namespace: "ns1", Name: "pgc1-instance1-abcd-0"},
	}

	t.Run("RemovesEveryPath", func(t *testing.T) {
		var calls []execCall

		assert.NilError(t, removeTempFiles(ctx, execRecorder(&calls, nil), pod,
			naming.ContainerDatabase, "/pgdata/tde-new-token", "/pgdata/tde-new-ca.crt"))

		assert.Equal(t, len(calls), 1, "one exec removes every path")
		assert.DeepEqual(t, calls[0].command[:2], []string{"bash", "-ceu"})
		assert.Equal(t, calls[0].command[2],
			"rm -f /pgdata/tde-new-token /pgdata/tde-new-ca.crt")
	})

	// rm -f exits zero for a path that is not there, so a file that was never
	// staged is not a failure.
	t.Run("ReportsFailure", func(t *testing.T) {
		var calls []execCall

		err := removeTempFiles(ctx,
			execRecorder(&calls, func(execCall) error {
				return errors.New("permission denied")
			}),
			pod, naming.ContainerDatabase, "/pgdata/tde-new-token")

		assert.ErrorContains(t, err, "remove /pgdata/tde-new-token")
		assert.ErrorContains(t, err, "permission denied")
	})
}
