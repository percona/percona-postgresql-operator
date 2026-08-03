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
	"github.com/percona/percona-postgresql-operator/v2/internal/postgres"
	crunchyv1beta1 "github.com/percona/percona-postgresql-operator/v2/pkg/apis/upstream.pgv2.percona.com/v1beta1"
)

// testFileSpec is the file key provider configuration shared by the tests below.
func testFileSpec() *crunchyv1beta1.PGTDEFileSpec {
	return &crunchyv1beta1.PGTDEFileSpec{
		KeySecret: crunchyv1beta1.PGTDESecretObjectReference{
			Name: "key-secret",
			Key:  "key-file",
		},
	}
}

func TestFileCredentialPaths(t *testing.T) {
	t.Parallel()

	provider := NewFileProvider(testFileSpec())

	mounted := provider.GetCredentialPath()
	assert.Assert(t, mounted.FileProvider != nil)
	assert.Assert(t, mounted.VaultProvider == nil,
		"a file provider must not describe vault paths")
	assert.Equal(t, mounted.FileProvider.KeyPath, naming.PGTDEMountPath+"/key-file",
		"the provider should read the key from the projected Secret")

	// The staged copy lives on the data volume so it survives the restart that
	// mounts the new Secret.
	staged := provider.GetStagedCredentialPath()
	assert.Assert(t, staged.FileProvider != nil)
	assert.Assert(t, strings.HasPrefix(staged.FileProvider.KeyPath, "/pgdata/"))
	assert.Assert(t, staged.FileProvider.KeyPath != mounted.FileProvider.KeyPath)
}

func TestFileRevision(t *testing.T) {
	t.Parallel()

	provider := NewFileProvider(testFileSpec())

	base, err := provider.GetRevision(provider.GetCredentialPath())
	assert.NilError(t, err)
	assert.Assert(t, base != "")

	t.Run("Deterministic", func(t *testing.T) {
		other := NewFileProvider(testFileSpec())
		again, err := other.GetRevision(other.GetCredentialPath())
		assert.NilError(t, err)
		assert.Equal(t, base, again, "same input should hash the same")
	})

	t.Run("StagedPathDiffers", func(t *testing.T) {
		staged, err := provider.GetRevision(provider.GetStagedCredentialPath())
		assert.NilError(t, err)
		assert.Assert(t, staged != base,
			"the staged revision must differ from the standard revision; the "+
				"two-phase provider change relies on telling them apart")
	})

	// The key name is part of the path pg_tde is pointed at, so changing it has
	// to move the cluster through a provider change.
	t.Run("KeyName", func(t *testing.T) {
		changed := testFileSpec()
		changed.KeySecret.Key = "other-key-file"

		other := NewFileProvider(changed)
		rev, err := other.GetRevision(other.GetCredentialPath())
		assert.NilError(t, err)
		assert.Assert(t, rev != base, "changing the key name should change the revision")
	})
}

func TestAddFileProvider(t *testing.T) {
	t.Run("names the key path", func(t *testing.T) {
		expected := errors.New("whoops")
		exec := func(
			_ context.Context, stdin io.Reader, stdout, stderr io.Writer, command ...string,
		) error {
			assert.Assert(t, stdout != nil, "should capture stdout")
			assert.Assert(t, stderr != nil, "should capture stderr")

			b, err := io.ReadAll(stdin)
			assert.NilError(t, err)

			assert.Assert(t, strings.Contains(string(b), "pg_tde_add_global_key_provider_file"))

			joined := strings.Join(command, " ")
			assert.Assert(t, strings.Contains(joined, "--set=provider_name="+naming.PGTDEFileProvider))
			assert.Assert(t, strings.Contains(joined, "--set=key_path="+naming.PGTDEMountPath+"/key-file"))

			return expected
		}

		keyPath := NewFileProvider(testFileSpec()).GetCredentialPath().FileProvider.KeyPath
		assert.Equal(t, expected, addFileProvider(t.Context(), exec, keyPath))
	})

	t.Run("does not interpret stderr", func(t *testing.T) {
		exec := func(
			_ context.Context, stdin io.Reader, stdout, stderr io.Writer, command ...string,
		) error {
			// psql exits zero, so the statement succeeded no matter what a
			// NOTICE or a localized message on stderr happens to say.
			_, _ = stderr.Write([]byte("ERROR: already exists"))
			return nil
		}

		assert.NilError(t, addFileProvider(t.Context(), exec, "/pgconf/tde/key-file"))
	})
}

func TestChangeFileProvider(t *testing.T) {
	expected := errors.New("whoops")
	exec := func(
		_ context.Context, stdin io.Reader, stdout, stderr io.Writer, command ...string,
	) error {
		assert.Assert(t, stdout != nil, "should capture stdout")
		assert.Assert(t, stderr != nil, "should capture stderr")

		b, err := io.ReadAll(stdin)
		assert.NilError(t, err)

		assert.Assert(t, strings.Contains(string(b), "pg_tde_change_global_key_provider_file"))

		joined := strings.Join(command, " ")
		assert.Assert(t, strings.Contains(joined, "--set=provider_name="+naming.PGTDEFileProvider))
		assert.Assert(t, strings.Contains(joined, "--set=key_path=/pgdata/tde-new-key"))

		return expected
	}

	keyPath := NewFileProvider(testFileSpec()).GetStagedCredentialPath().FileProvider.KeyPath
	assert.Equal(t, expected, changeFileProvider(t.Context(), exec, keyPath))
}

// TestFileProviderReconcile mirrors TestVaultProviderReconcile: the provider and
// the global key may already exist even on the initial setup path, so each step
// recovers from a failure by driving the state towards the spec and lets the
// following step decide whether that worked.
func TestFileProviderReconcile(t *testing.T) {
	file := testFileSpec()
	provider := NewFileProvider(file)
	paths := provider.GetCredentialPath()

	statement := func(sql string) string {
		for _, name := range []string{
			"pg_tde_add_global_key_provider_file",
			"pg_tde_change_global_key_provider_file",
			"pg_tde_create_key_using_global_key_provider",
			"pg_tde_set_default_key_using_global_key_provider",
		} {
			if strings.Contains(sql, name) {
				return name
			}
		}
		return sql
	}

	execSequence := func(called *[]string, failures map[string]error) postgres.Executor {
		return func(
			_ context.Context, stdin io.Reader, stdout, stderr io.Writer, command ...string,
		) error {
			b, err := io.ReadAll(stdin)
			assert.NilError(t, err)

			name := statement(string(b))
			*called = append(*called, name)
			return failures[name]
		}
	}

	newCluster := func(revision string) *crunchyv1beta1.PostgresCluster {
		cluster := &crunchyv1beta1.PostgresCluster{}
		cluster.Spec.Extensions.PGTDE.File = file
		cluster.Status.PGTDERevision = revision
		cluster.UID = "test-uid"
		return cluster
	}

	t.Run("initial setup", func(t *testing.T) {
		var called []string
		err := provider.Reconcile(t.Context(), execSequence(&called, nil),
			newCluster(""), paths)

		assert.NilError(t, err)
		assert.DeepEqual(t, called, []string{
			"pg_tde_add_global_key_provider_file",
			"pg_tde_create_key_using_global_key_provider",
			"pg_tde_set_default_key_using_global_key_provider",
		})
	})

	t.Run("existing provider is rewritten", func(t *testing.T) {
		var called []string
		err := provider.Reconcile(t.Context(),
			execSequence(&called, map[string]error{
				"pg_tde_add_global_key_provider_file": errors.New("already exists"),
			}),
			newCluster(""), paths)

		assert.NilError(t, err)
		assert.DeepEqual(t, called, []string{
			"pg_tde_add_global_key_provider_file",
			// The existing provider must be overwritten rather than trusted to
			// match the spec.
			"pg_tde_change_global_key_provider_file",
			"pg_tde_create_key_using_global_key_provider",
			"pg_tde_set_default_key_using_global_key_provider",
		})
	})

	t.Run("provider unusable", func(t *testing.T) {
		expectedErr := errors.New("add file provider: no such file or directory")
		var called []string
		err := provider.Reconcile(t.Context(),
			execSequence(&called, map[string]error{
				"pg_tde_add_global_key_provider_file":    errors.New("no such file or directory"),
				"pg_tde_change_global_key_provider_file": errors.New("no such provider"),
			}),
			newCluster(""), paths)

		assert.Equal(t, expectedErr.Error(), err.Error())
		assert.DeepEqual(t, called, []string{
			"pg_tde_add_global_key_provider_file",
			"pg_tde_change_global_key_provider_file",
		})
	})

	t.Run("existing key", func(t *testing.T) {
		var called []string
		err := provider.Reconcile(t.Context(),
			execSequence(&called, map[string]error{
				"pg_tde_create_key_using_global_key_provider": errors.New("already exists"),
			}),
			newCluster(""), paths)

		assert.NilError(t, err)
		assert.DeepEqual(t, called, []string{
			"pg_tde_add_global_key_provider_file",
			"pg_tde_create_key_using_global_key_provider",
			"pg_tde_set_default_key_using_global_key_provider",
		})
	})

	t.Run("key unusable", func(t *testing.T) {
		expectedErr := errors.New("create global key: permission denied")
		var called []string
		err := provider.Reconcile(t.Context(),
			execSequence(&called, map[string]error{
				"pg_tde_create_key_using_global_key_provider":      errors.New("permission denied"),
				"pg_tde_set_default_key_using_global_key_provider": errors.New("key not found"),
			}),
			newCluster(""), paths)

		assert.Equal(t, expectedErr.Error(), err.Error())
	})

	t.Run("revision set changes the provider", func(t *testing.T) {
		var called []string
		err := provider.Reconcile(t.Context(), execSequence(&called, nil),
			newCluster("some-revision"), paths)

		assert.NilError(t, err)
		assert.DeepEqual(t, called, []string{
			"pg_tde_change_global_key_provider_file",
		})
	})

	t.Run("revision set and the change fails", func(t *testing.T) {
		expected := errors.New("change error")
		var called []string
		err := provider.Reconcile(t.Context(),
			execSequence(&called, map[string]error{
				"pg_tde_change_global_key_provider_file": expected,
			}),
			newCluster("some-revision"), paths)

		assert.Equal(t, expected, err,
			"an existing cluster must not silently fall back to adding a provider")
	})
}

func TestFileStageCredentials(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Namespace: "ns1", Name: "key-secret"},
		Data: map[string][]byte{
			"key-file": []byte("principal-key-bytes"),
		},
	}

	provider := NewFileProvider(testFileSpec())
	staged := provider.GetStagedCredentialPath()

	t.Run("WritesToEveryPod", func(t *testing.T) {
		var calls []execCall
		k8s := fake.NewClientBuilder().WithObjects(secret).Build()
		pods := newPods("pgc1-instance1-abcd-0", "pgc1-instance2-efgh-0")

		assert.NilError(t, provider.StageCredentials(ctx, k8s, execRecorder(&calls, nil),
			"ns1", pods, naming.ContainerDatabase, staged))

		// pg_tde names one path cluster-wide, but each instance has its own
		// /pgdata, so every one of them needs its own copy.
		assert.Equal(t, len(calls), 2, "the key on each of two instances")
		for i, pod := range pods {
			assert.Equal(t, calls[i].pod, pod.Name)
			assert.Equal(t, calls[i].stdin, "principal-key-bytes",
				"the key should be piped in, not interpolated into the command")
			assert.Assert(t, strings.Contains(calls[i].command[2], staged.FileProvider.KeyPath))
		}
	})

	t.Run("ReadsTheSecretOnce", func(t *testing.T) {
		var calls []execCall
		gets := 0
		k8s := &countingClient{
			Client: fake.NewClientBuilder().WithObjects(secret).Build(),
			gets:   &gets,
		}

		assert.NilError(t, provider.StageCredentials(ctx, k8s, execRecorder(&calls, nil),
			"ns1", newPods("a", "b", "c"), naming.ContainerDatabase, staged))

		assert.Equal(t, gets, 1,
			"the key Secret should be read once, not once per instance")
	})

	t.Run("MissingSecretWritesNothing", func(t *testing.T) {
		var calls []execCall
		k8s := fake.NewClientBuilder().Build()

		err := provider.StageCredentials(ctx, k8s, execRecorder(&calls, nil),
			"ns1", newPods("a", "b"), naming.ContainerDatabase, staged)

		assert.ErrorContains(t, err, "key-secret")
		assert.Equal(t, len(calls), 0,
			"the Secret is read before anything is written to any Pod")
	})

	t.Run("MissingKey", func(t *testing.T) {
		var calls []execCall
		k8s := fake.NewClientBuilder().WithObjects(secret).Build()

		badKey := testFileSpec()
		badKey.KeySecret.Key = "nope"
		badKeyProvider := NewFileProvider(badKey)

		err := badKeyProvider.StageCredentials(ctx, k8s, execRecorder(&calls, nil),
			"ns1", newPods("a"), naming.ContainerDatabase,
			badKeyProvider.GetStagedCredentialPath())

		assert.ErrorContains(t, err, `key "nope" not found`)
		assert.Equal(t, len(calls), 0)
	})

	t.Run("FailureNamesThePod", func(t *testing.T) {
		var calls []execCall
		k8s := fake.NewClientBuilder().WithObjects(secret).Build()

		exec := execRecorder(&calls, func(call execCall) error {
			if call.pod == "b" {
				return errors.New("no space left on device")
			}
			return nil
		})

		err := provider.StageCredentials(ctx, k8s, exec,
			"ns1", newPods("a", "b", "c"), naming.ContainerDatabase, staged)

		assert.ErrorContains(t, err, "pod b")
		assert.ErrorContains(t, err, "no space left on device")
		assert.Equal(t, len(calls), 2,
			"staging stops at the first instance it cannot write to")
	})
}

func TestFileCleanupStagedCredentials(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	provider := NewFileProvider(testFileSpec())
	staged := provider.GetStagedCredentialPath()

	t.Run("RemovesFromEveryPod", func(t *testing.T) {
		var calls []execCall
		pods := newPods("a", "b", "c")

		assert.NilError(t, provider.CleanupStagedCredentials(ctx,
			execRecorder(&calls, nil), pods, naming.ContainerDatabase, staged))

		assert.Equal(t, len(calls), 3, "each instance has its own copy to remove")
		for i, pod := range pods {
			assert.Equal(t, calls[i].pod, pod.Name)
			assert.Assert(t, strings.Contains(calls[i].command[2], "rm -f"))
			assert.Assert(t, strings.Contains(calls[i].command[2], staged.FileProvider.KeyPath))
		}
	})

	// A key left behind sits in plaintext on a PersistentVolume until something
	// removes it, so an unreachable instance has to be reported even though the
	// others were cleaned.
	t.Run("ReportsFailureAndKeepsGoing", func(t *testing.T) {
		var calls []execCall
		exec := execRecorder(&calls, func(call execCall) error {
			if call.pod == "a" {
				return errors.New("container not running")
			}
			return nil
		})

		err := provider.CleanupStagedCredentials(ctx, exec,
			newPods("a", "b", "c"), naming.ContainerDatabase, staged)

		assert.ErrorContains(t, err, "pod a")
		assert.Equal(t, len(calls), 3,
			"one unreachable instance must not stop the others being cleaned")
	})
}
