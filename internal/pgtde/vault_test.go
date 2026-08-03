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

// testVaultSpec is the vault configuration shared by the tests below.
func testVaultSpec() *crunchyv1beta1.PGTDEVaultSpec {
	return &crunchyv1beta1.PGTDEVaultSpec{
		Host:      "https://vault.example.com",
		MountPath: "secret/data",
		TokenSecret: crunchyv1beta1.PGTDESecretObjectReference{
			Name: "token-secret",
			Key:  "token-key",
		},
		CASecret: crunchyv1beta1.PGTDESecretObjectReference{
			Name: "ca-secret",
			Key:  "ca-key",
		},
	}
}

// vaultPaths returns the credential paths of a vault provider built from vault.
func vaultPaths(vault *crunchyv1beta1.PGTDEVaultSpec) (mounted, staged VaultProviderCredentialPath) {
	provider := NewVaultProvider(vault)
	return *provider.GetCredentialPath().VaultProvider,
		*provider.GetStagedCredentialPath().VaultProvider
}

func TestVaultCredentialPaths(t *testing.T) {
	t.Parallel()

	mounted, staged := vaultPaths(testVaultSpec())

	assert.Equal(t, mounted.TokenPath, naming.PGTDEMountPath+"/token-key",
		"the provider should read the token from the projected Secret")
	assert.Equal(t, mounted.CAPath, naming.PGTDEMountPath+"/ca-key")

	// The staged copies live on the data volume so they survive the restart
	// that mounts the new Secret.
	assert.Assert(t, strings.HasPrefix(staged.TokenPath, "/pgdata/"))
	assert.Assert(t, strings.HasPrefix(staged.CAPath, "/pgdata/"))
	assert.Assert(t, staged.TokenPath != staged.CAPath)
}

func TestVaultRevision(t *testing.T) {
	t.Parallel()

	vault := testVaultSpec()
	provider := NewVaultProvider(vault)

	base, err := provider.GetRevision(provider.GetCredentialPath())
	assert.NilError(t, err)
	assert.Assert(t, base != "")

	t.Run("Deterministic", func(t *testing.T) {
		again, err := NewVaultProvider(testVaultSpec()).GetRevision(
			NewVaultProvider(testVaultSpec()).GetCredentialPath())
		assert.NilError(t, err)
		assert.Equal(t, base, again, "same input should hash the same")
	})

	t.Run("StagedPathsDiffer", func(t *testing.T) {
		staged, err := provider.GetRevision(provider.GetStagedCredentialPath())
		assert.NilError(t, err)
		assert.Assert(t, staged != base,
			"the staged revision must differ from the standard revision; the "+
				"two-phase provider change relies on telling them apart")
	})

	// Every field that influences how PostgreSQL reaches Vault must change the
	// revision, otherwise a configuration change is silently never applied.
	for _, tc := range []struct {
		name   string
		mutate func(*crunchyv1beta1.PGTDEVaultSpec)
	}{
		{"Host", func(v *crunchyv1beta1.PGTDEVaultSpec) { v.Host = "https://other:8200" }},
		{"MountPath", func(v *crunchyv1beta1.PGTDEVaultSpec) { v.MountPath = "other" }},
		{"TokenSecretName", func(v *crunchyv1beta1.PGTDEVaultSpec) { v.TokenSecret.Name = "other" }},
		{"TokenSecretKey", func(v *crunchyv1beta1.PGTDEVaultSpec) { v.TokenSecret.Key = "other" }},
		{"CASecretName", func(v *crunchyv1beta1.PGTDEVaultSpec) { v.CASecret.Name = "other" }},
		{"CASecretKey", func(v *crunchyv1beta1.PGTDEVaultSpec) { v.CASecret.Key = "other" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			changed := testVaultSpec()
			tc.mutate(changed)

			// Some of the fields above feed into the credential paths, which
			// the provider recomputes from the spec it was given.
			other := NewVaultProvider(changed)
			rev, err := other.GetRevision(other.GetCredentialPath())
			assert.NilError(t, err)
			assert.Assert(t, rev != base, "changing %s should change the revision", tc.name)
		})
	}

	// Host and MountPath are hashed one after another and neither feeds into
	// the credential paths, so they isolate how the fields are delimited from
	// everything else that goes into the revision.
	hostMount := func(host, mountPath string) string {
		t.Helper()
		vault := testVaultSpec()
		vault.Host, vault.MountPath = host, mountPath

		provider := NewVaultProvider(vault)
		revision, err := provider.GetRevision(provider.GetCredentialPath())
		assert.NilError(t, err)
		return revision
	}

	// Without a delimiter, moving a character across a field boundary produces
	// the same revision, and reconcilePGTDEProviders takes its "matches the
	// spec" early return on a Vault the cluster has never been pointed at.
	t.Run("FieldBoundaries", func(t *testing.T) {
		assert.Assert(t,
			hostMount("https://vault:8200", "secret/data") !=
				hostMount("https://vault:8200secret", "/data"),
			"a character moved from one field to the next must change the revision")
	})

	// Delimiting with a quote is only injective when a quote appearing inside a
	// value is escaped.
	t.Run("QuotesInValues", func(t *testing.T) {
		assert.Assert(t, hostMount(`a"`, `b`) != hostMount(`a`, `"b`),
			"a quote inside a value must not fake a field boundary")
	})
}

func TestAddVaultProvider(t *testing.T) {
	t.Run("with CA secret", func(t *testing.T) {
		expected := errors.New("whoops")
		exec := func(
			_ context.Context, stdin io.Reader, stdout, stderr io.Writer, command ...string,
		) error {
			assert.Assert(t, stdout != nil, "should capture stdout")
			assert.Assert(t, stderr != nil, "should capture stderr")

			b, err := io.ReadAll(stdin)
			assert.NilError(t, err)
			sql := string(b)

			assert.Assert(t, strings.Contains(sql, "pg_tde_add_global_key_provider_vault_v2"))

			joined := strings.Join(command, " ")
			assert.Assert(t, strings.Contains(joined, "--set=provider_name="+naming.PGTDEVaultProvider))
			assert.Assert(t, strings.Contains(joined, "--set=vault_host=https://vault.example.com"))
			assert.Assert(t, strings.Contains(joined, "--set=vault_mount_path=secret/data"))
			assert.Assert(t, strings.Contains(joined, "--set=token_path="+naming.PGTDEMountPath+"/token-key"))
			assert.Assert(t, strings.Contains(joined, "--set=ca_path="+naming.PGTDEMountPath+"/ca-key"))

			return expected
		}

		ctx := context.Background()
		vault := testVaultSpec()
		mounted, _ := vaultPaths(vault)
		assert.Equal(t, expected,
			addVaultProvider(ctx, exec, vault, mounted.TokenPath, mounted.CAPath))
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

		ctx := context.Background()
		vault := testVaultSpec()
		vault.CASecret = crunchyv1beta1.PGTDESecretObjectReference{}
		mounted, _ := vaultPaths(vault)
		assert.NilError(t,
			addVaultProvider(ctx, exec, vault, mounted.TokenPath, mounted.CAPath))
	})

	t.Run("without CA secret", func(t *testing.T) {
		exec := func(
			_ context.Context, stdin io.Reader, stdout, stderr io.Writer, command ...string,
		) error {
			joined := strings.Join(command, " ")
			assert.Assert(t, strings.Contains(joined, "--set=ca_path="),
				"ca_path should be set to empty string")

			return nil
		}

		ctx := t.Context()
		vault := testVaultSpec()
		vault.CASecret = crunchyv1beta1.PGTDESecretObjectReference{}
		mounted, _ := vaultPaths(vault)
		assert.NilError(t,
			addVaultProvider(ctx, exec, vault, mounted.TokenPath, mounted.CAPath))
	})
}

func TestChangeVaultProvider(t *testing.T) {
	t.Run("with CA secret", func(t *testing.T) {
		expected := errors.New("whoops")
		exec := func(
			_ context.Context, stdin io.Reader, stdout, stderr io.Writer, command ...string,
		) error {
			assert.Assert(t, stdout != nil, "should capture stdout")
			assert.Assert(t, stderr != nil, "should capture stderr")

			b, err := io.ReadAll(stdin)
			assert.NilError(t, err)
			sql := string(b)

			assert.Assert(t, strings.Contains(sql, "pg_tde_change_global_key_provider_vault_v2"))

			joined := strings.Join(command, " ")
			assert.Assert(t, strings.Contains(joined, "--set=provider_name="+naming.PGTDEVaultProvider))
			assert.Assert(t, strings.Contains(joined, "--set=vault_host=https://vault.example.com"))
			assert.Assert(t, strings.Contains(joined, "--set=vault_mount_path=secret/data"))
			assert.Assert(t, strings.Contains(joined, "--set=token_path="+naming.PGTDEMountPath+"/token-key"))
			assert.Assert(t, strings.Contains(joined, "--set=ca_path="+naming.PGTDEMountPath+"/ca-key"))

			return expected
		}

		ctx := context.Background()
		vault := testVaultSpec()
		mounted, _ := vaultPaths(vault)
		assert.Equal(t, expected,
			changeVaultProvider(ctx, exec, vault, mounted.TokenPath, mounted.CAPath))
	})

	t.Run("without CA secret", func(t *testing.T) {
		exec := func(
			_ context.Context, stdin io.Reader, stdout, stderr io.Writer, command ...string,
		) error {
			joined := strings.Join(command, " ")
			assert.Assert(t, strings.Contains(joined, "--set=ca_path="),
				"ca_path should be set to empty string")

			return nil
		}

		ctx := context.Background()
		vault := testVaultSpec()
		vault.CASecret = crunchyv1beta1.PGTDESecretObjectReference{}
		mounted, _ := vaultPaths(vault)
		assert.NilError(t,
			changeVaultProvider(ctx, exec, vault, mounted.TokenPath, mounted.CAPath))
	})
}

// TestVaultProviderReconcile covers the recovery paths of a provider that may
// already exist even on the initial setup path: a cluster that is deleted and
// recreated with its PVCs retained, or one where pg_tde was disabled and
// re-enabled, starts with an empty PGTDERevision but a populated pg_tde state.
func TestVaultProviderReconcile(t *testing.T) {
	vault := testVaultSpec()
	provider := NewVaultProvider(vault)
	paths := provider.GetCredentialPath()

	// statement names the pg_tde function a psql invocation called, so the
	// tests below can describe the expected sequence instead of counting.
	statement := func(sql string) string {
		for _, name := range []string{
			"pg_tde_add_global_key_provider_vault_v2",
			"pg_tde_change_global_key_provider_vault_v2",
			"pg_tde_create_key_using_global_key_provider",
			"pg_tde_set_default_key_using_global_key_provider",
		} {
			if strings.Contains(sql, name) {
				return name
			}
		}
		return sql
	}

	// execSequence returns an Executor that records the pg_tde function each
	// call ran and fails the calls named in failures.
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
		cluster.Spec.Extensions.PGTDE.Vault = vault
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
			"pg_tde_add_global_key_provider_vault_v2",
			"pg_tde_create_key_using_global_key_provider",
			"pg_tde_set_default_key_using_global_key_provider",
		})
	})

	t.Run("existing provider is rewritten", func(t *testing.T) {
		// A cluster recreated on retained PVCs: the provider is already there,
		// possibly pointing at a different Vault than the spec asks for.
		var called []string
		err := provider.Reconcile(t.Context(),
			execSequence(&called, map[string]error{
				"pg_tde_add_global_key_provider_vault_v2": errors.New("already exists"),
			}),
			newCluster(""), paths)

		assert.NilError(t, err)
		assert.DeepEqual(t, called, []string{
			"pg_tde_add_global_key_provider_vault_v2",
			// The existing provider must be overwritten rather than trusted
			// to match the spec.
			"pg_tde_change_global_key_provider_vault_v2",
			"pg_tde_create_key_using_global_key_provider",
			"pg_tde_set_default_key_using_global_key_provider",
		})
	})

	t.Run("provider unusable", func(t *testing.T) {
		// Neither statement works, so this is not an "already exists" case.
		expectedErr := errors.New("add vault provider: vault is unreachable")
		var called []string
		err := provider.Reconcile(t.Context(),
			execSequence(&called, map[string]error{
				"pg_tde_add_global_key_provider_vault_v2":    errors.New("vault is unreachable"),
				"pg_tde_change_global_key_provider_vault_v2": errors.New("no such provider"),
			}),
			newCluster(""), paths)

		assert.Equal(t, expectedErr.Error(), err.Error())
		assert.DeepEqual(t, called, []string{
			"pg_tde_add_global_key_provider_vault_v2",
			"pg_tde_change_global_key_provider_vault_v2",
		})
	})

	t.Run("existing key", func(t *testing.T) {
		// Creating the key fails because it is already there; setting it as
		// the default proves the state is good.
		var called []string
		err := provider.Reconcile(t.Context(),
			execSequence(&called, map[string]error{
				"pg_tde_create_key_using_global_key_provider": errors.New("already exists"),
			}),
			newCluster(""), paths)

		assert.NilError(t, err)
		assert.DeepEqual(t, called, []string{
			"pg_tde_add_global_key_provider_vault_v2",
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

	t.Run("set default key fails", func(t *testing.T) {
		expectedErr := errors.New("set default key: oops")
		var called []string
		err := provider.Reconcile(t.Context(),
			execSequence(&called, map[string]error{
				"pg_tde_set_default_key_using_global_key_provider": errors.New("oops"),
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
			"pg_tde_change_global_key_provider_vault_v2",
		})
	})

	t.Run("revision set and the change fails", func(t *testing.T) {
		expected := errors.New("change error")
		var called []string
		err := provider.Reconcile(t.Context(),
			execSequence(&called, map[string]error{
				"pg_tde_change_global_key_provider_vault_v2": expected,
			}),
			newCluster("some-revision"), paths)

		assert.Equal(t, expected, err,
			"an existing cluster must not silently fall back to adding a provider")
	})
}

func TestVaultStageCredentials(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Namespace: "ns1", Name: "token-secret"},
		Data: map[string][]byte{
			"token-key": []byte("hvs.sometoken"),
		},
	}
	caSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Namespace: "ns1", Name: "ca-secret"},
		Data: map[string][]byte{
			"ca-key": []byte("-----BEGIN CERTIFICATE-----"),
		},
	}

	provider := NewVaultProvider(testVaultSpec())
	staged := provider.GetStagedCredentialPath()
	tokenPath := staged.VaultProvider.TokenPath
	caPath := staged.VaultProvider.CAPath

	t.Run("WritesToEveryPod", func(t *testing.T) {
		var calls []execCall
		k8s := fake.NewClientBuilder().WithObjects(secret, caSecret).Build()
		pods := newPods("pgc1-instance1-abcd-0", "pgc1-instance2-efgh-0", "pgc1-instance3-ijkl-0")

		assert.NilError(t, provider.StageCredentials(ctx, k8s, execRecorder(&calls, nil),
			"ns1", pods, naming.ContainerDatabase, staged))

		// pg_tde names one path cluster-wide, but each instance has its own
		// /pgdata, so every one of them needs its own copy.
		assert.Equal(t, len(calls), 6, "two files on each of three instances")

		for i, pod := range pods {
			token, ca := calls[i*2], calls[i*2+1]

			assert.Equal(t, token.pod, pod.Name)
			assert.Equal(t, token.stdin, "hvs.sometoken")
			assert.Assert(t, strings.Contains(token.command[2], tokenPath))

			assert.Equal(t, ca.pod, pod.Name)
			assert.Equal(t, ca.stdin, "-----BEGIN CERTIFICATE-----")
			assert.Assert(t, strings.Contains(ca.command[2], caPath))
		}
	})

	t.Run("ReadsEachSecretOnce", func(t *testing.T) {
		var calls []execCall
		gets := 0
		k8s := &countingClient{
			Client: fake.NewClientBuilder().WithObjects(secret, caSecret).Build(),
			gets:   &gets,
		}

		assert.NilError(t, provider.StageCredentials(ctx, k8s, execRecorder(&calls, nil),
			"ns1", newPods("a", "b", "c", "d"), naming.ContainerDatabase, staged))

		assert.Equal(t, gets, 2,
			"the token and CA Secrets should be read once, not once per instance")
	})

	t.Run("WithoutCASecret", func(t *testing.T) {
		var calls []execCall
		k8s := fake.NewClientBuilder().WithObjects(secret, caSecret).Build()

		noCA := testVaultSpec()
		noCA.CASecret = crunchyv1beta1.PGTDESecretObjectReference{}
		noCAProvider := NewVaultProvider(noCA)

		assert.NilError(t, noCAProvider.StageCredentials(ctx, k8s, execRecorder(&calls, nil),
			"ns1", newPods("a", "b"), naming.ContainerDatabase,
			noCAProvider.GetStagedCredentialPath()))

		assert.Equal(t, len(calls), 2, "only the token is staged")
	})

	t.Run("MissingSecretWritesNothing", func(t *testing.T) {
		var calls []execCall
		k8s := fake.NewClientBuilder().Build()

		err := provider.StageCredentials(ctx, k8s, execRecorder(&calls, nil),
			"ns1", newPods("a", "b"), naming.ContainerDatabase, staged)

		assert.ErrorContains(t, err, "token secret")
		assert.Equal(t, len(calls), 0,
			"the Secrets are read before anything is written to any Pod")
	})

	t.Run("MissingKey", func(t *testing.T) {
		var calls []execCall
		k8s := fake.NewClientBuilder().WithObjects(secret, caSecret).Build()

		badKey := testVaultSpec()
		badKey.TokenSecret.Key = "nope"
		badKeyProvider := NewVaultProvider(badKey)

		err := badKeyProvider.StageCredentials(ctx, k8s, execRecorder(&calls, nil),
			"ns1", newPods("a"), naming.ContainerDatabase,
			badKeyProvider.GetStagedCredentialPath())

		assert.ErrorContains(t, err, `key "nope" not found`)
		assert.Equal(t, len(calls), 0)
	})

	t.Run("FailureNamesThePod", func(t *testing.T) {
		var calls []execCall
		k8s := fake.NewClientBuilder().WithObjects(secret, caSecret).Build()

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
		assert.Equal(t, len(calls), 3,
			"staging stops at the first instance it cannot write to")
	})
}

func TestVaultCleanupStagedCredentials(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	provider := NewVaultProvider(testVaultSpec())
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
			assert.Assert(t, strings.Contains(calls[i].command[2], staged.VaultProvider.TokenPath))
			assert.Assert(t, strings.Contains(calls[i].command[2], staged.VaultProvider.CAPath))
		}
	})

	// A token left behind sits in plaintext on a PersistentVolume until
	// something removes it, so an unreachable instance has to be reported even
	// though the others were cleaned.
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

// TestVaultCAAgreement pins the three answers that have to match for a CA to
// work: whether it is projected into the Pod, where pg_tde is told to read it
// from, and where it is staged during a provider change. They were once three
// separate expressions over CASecret, and any pair of them disagreeing is a
// configuration the operator cannot serve.
func TestVaultCAAgreement(t *testing.T) {
	t.Parallel()

	vaultWith := func(name, key string) *crunchyv1beta1.PGTDEVaultSpec {
		return &crunchyv1beta1.PGTDEVaultSpec{
			Host:        "https://vault.example.com:8200",
			MountPath:   "secret/data",
			TokenSecret: crunchyv1beta1.PGTDESecretObjectReference{Name: "vault", Key: "token"},
			CASecret:    crunchyv1beta1.PGTDESecretObjectReference{Name: name, Key: key},
		}
	}

	// projectsCA reports whether the Pod would mount a CA certificate. The
	// volume projects the token and nothing else until a CA is configured, and
	// the two secrets may share a name, so count the sources rather than try to
	// tell them apart by name.
	projectsCA := func(vault *crunchyv1beta1.PGTDEVaultSpec) bool {
		sources := postgres.PGTDEVolume(
			&crunchyv1beta1.PGTDESpec{Enabled: true, Vault: vault}).Projected.Sources
		assert.Assert(t, len(sources) == 1 || len(sources) == 2,
			"expected the token and at most a CA, got %v", sources)
		return len(sources) == 2
	}

	for _, tc := range []struct {
		name     string
		vault    *crunchyv1beta1.PGTDEVaultSpec
		expected bool
	}{
		{"Both", vaultWith("vault", "ca.crt"), true},
		{"Neither", vaultWith("", ""), false},
		// Half a reference cannot be resolved, so it is no CA at all. The CRD
		// requires both once caSecret is given, but nothing in the operator
		// should depend on that to stay consistent.
		{"NameOnly", vaultWith("vault", ""), false},
		{"KeyOnly", vaultWith("", "ca.crt"), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mounted, staged := vaultPaths(tc.vault)

			assert.Equal(t, tc.vault.HasCA(), tc.expected)
			assert.Equal(t, mounted.CAPath != "", tc.expected,
				"the provider should name a CA only when one is configured")
			assert.Equal(t, staged.CAPath != "", tc.expected,
				"a CA should be staged only when one is configured")
			assert.Equal(t, projectsCA(tc.vault), tc.expected,
				"the Pod should mount a CA only when one is configured")
		})
	}
}
