package pgtde

import (
	"sort"
	"strings"
	"testing"

	"github.com/pkg/errors"
	"gotest.tools/v3/assert"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"

	"github.com/percona/percona-postgresql-operator/v2/internal/naming"
	"github.com/percona/percona-postgresql-operator/v2/internal/postgres"
	crunchyv1beta1 "github.com/percona/percona-postgresql-operator/v2/pkg/apis/upstream.pgv2.percona.com/v1beta1"
)

func TestReportExtension(t *testing.T) {
	t.Run("enabled successfully", func(t *testing.T) {
		recorder := record.NewFakeRecorder(10)
		cluster := &crunchyv1beta1.PostgresCluster{}
		cluster.Spec.Extensions.PGTDE.Enabled = true
		cluster.Generation = 2

		ReportExtension(cluster, recorder, nil)

		condition := meta.FindStatusCondition(cluster.Status.Conditions, crunchyv1beta1.PGTDEEnabled)
		assert.Assert(t, condition != nil)
		assert.Equal(t, condition.Status, metav1.ConditionTrue)
		assert.Equal(t, condition.Reason, "Enabled")
		assert.Equal(t, condition.Message, "pg_tde is enabled in PerconaPGCluster")
		assert.Equal(t, condition.ObservedGeneration, int64(2))
	})

	t.Run("disabled successfully", func(t *testing.T) {
		recorder := record.NewFakeRecorder(10)
		cluster := &crunchyv1beta1.PostgresCluster{}
		cluster.Spec.Extensions.PGTDE.Enabled = false
		cluster.Generation = 1

		ReportExtension(cluster, recorder, nil)

		condition := meta.FindStatusCondition(cluster.Status.Conditions, crunchyv1beta1.PGTDEEnabled)
		assert.Assert(t, condition != nil)
		assert.Equal(t, condition.Status, metav1.ConditionFalse)
		assert.Equal(t, condition.Reason, "Disabled")
		assert.Equal(t, condition.Message, "pg_tde is disabled in PerconaPGCluster")
		assert.Equal(t, condition.ObservedGeneration, int64(1))
	})

	t.Run("install error records event", func(t *testing.T) {
		recorder := record.NewFakeRecorder(10)
		cluster := &crunchyv1beta1.PostgresCluster{}
		cluster.Spec.Extensions.PGTDE.Enabled = true

		ReportExtension(cluster, recorder, errors.New("enable failed"))

		select {
		case event := <-recorder.Events:
			assert.Assert(t, strings.Contains(event, "PGTDEInstallFailed"))
			assert.Assert(t, strings.Contains(event, "Unable to install pg_tde"))
		default:
			t.Fatal("expected event to be recorded")
		}

		assert.Equal(t, len(cluster.Status.Conditions), 0,
			"a failed CREATE EXTENSION must not report pg_tde as enabled")
	})

	t.Run("disable error records event", func(t *testing.T) {
		recorder := record.NewFakeRecorder(10)
		cluster := &crunchyv1beta1.PostgresCluster{}
		cluster.Spec.Extensions.PGTDE.Enabled = false

		ReportExtension(cluster, recorder, errors.New("disable failed"))

		select {
		case event := <-recorder.Events:
			assert.Assert(t, strings.Contains(event, "PGTDEDisableFailed"))
			assert.Assert(t, strings.Contains(event, "Unable to disable pg_tde"))
		default:
			t.Fatal("expected event to be recorded")
		}
	})

	t.Run("failure keeps the previous condition", func(t *testing.T) {
		recorder := record.NewFakeRecorder(10)
		cluster := &crunchyv1beta1.PostgresCluster{}
		cluster.Spec.Extensions.PGTDE.Enabled = false

		// pg_tde is installed; the user asked to disable it and DROP failed.
		ReportExtension(cluster, recorder, nil)
		cluster.Spec.Extensions.PGTDE.Enabled = true
		ReportExtension(cluster, recorder, nil)
		cluster.Spec.Extensions.PGTDE.Enabled = false
		ReportExtension(cluster, recorder, errors.New("nope"))

		condition := meta.FindStatusCondition(cluster.Status.Conditions, crunchyv1beta1.PGTDEEnabled)
		assert.Assert(t, condition != nil)
		assert.Equal(t, condition.Status, metav1.ConditionTrue,
			"the extension is still installed, so it must stay in shared_preload_libraries")
	})
}

func TestNewProviderForCluster(t *testing.T) {
	t.Parallel()

	clusterWith := func(spec crunchyv1beta1.PGTDESpec) *crunchyv1beta1.PostgresCluster {
		cluster := &crunchyv1beta1.PostgresCluster{}
		cluster.Spec.Extensions.PGTDE = spec
		return cluster
	}

	t.Run("Vault", func(t *testing.T) {
		provider := NewProviderForCluster(clusterWith(crunchyv1beta1.PGTDESpec{
			Enabled: true, Vault: testVaultSpec(),
		}))
		_, ok := provider.(*vaultProvider)
		assert.Assert(t, ok, "expected a vault provider, got %T", provider)
	})

	t.Run("File", func(t *testing.T) {
		provider := NewProviderForCluster(clusterWith(crunchyv1beta1.PGTDESpec{
			Enabled: true, File: testFileSpec(),
		}))
		_, ok := provider.(*fileProvider)
		assert.Assert(t, ok, "expected a file provider, got %T", provider)
	})

	// The two providers hold different keys, so a cluster configured for both
	// has to settle on one of them rather than alternate between reconciles.
	t.Run("BothPrefersVault", func(t *testing.T) {
		provider := NewProviderForCluster(clusterWith(crunchyv1beta1.PGTDESpec{
			Enabled: true, Vault: testVaultSpec(), File: testFileSpec(),
		}))
		_, ok := provider.(*vaultProvider)
		assert.Assert(t, ok, "expected a vault provider, got %T", provider)
	})

	// reconcilePGTDEProviders and reconcileInstance both check the spec before
	// they ask for a provider; nothing else may assume one exists.
	t.Run("Neither", func(t *testing.T) {
		assert.Assert(t, NewProviderForCluster(clusterWith(
			crunchyv1beta1.PGTDESpec{Enabled: true})) == nil)
	})
}

func TestChangePhase(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		provider KeyProvider
	}{
		{"Vault", NewVaultProvider(testVaultSpec())},
		{"File", NewFileProvider(testFileSpec())},
	} {
		t.Run(tc.name, func(t *testing.T) {
			standardRevision, tempRevision := revisions(t, tc.provider)
			assert.Assert(t, standardRevision != tempRevision,
				"the two-phase credential change relies on telling the mounted "+
					"credentials apart from the staged copies")

			for _, phase := range []struct {
				name     string
				revision string
				expected Phase
			}{
				{"InitialSetup", "", InitialSetup},
				{"Configured", standardRevision, Configured},
				{"Finalize", tempRevision, Finalize},
				{"StageCredentials", "a-revision-from-another-provider", StageCredentials},
			} {
				t.Run(phase.name, func(t *testing.T) {
					change, err := ChangePhase(tc.provider, phase.revision)
					assert.NilError(t, err)

					assert.Equal(t, change.Phase, phase.expected)
					assert.Equal(t, change.StandardRevision, standardRevision)
					assert.Equal(t, change.TempRevision, tempRevision)
				})
			}

			// reconcileInstance holds the Pods' pg-tde volume in exactly one
			// phase. Holding in any other one pins the StatefulSet to
			// credentials the provider has already moved off of, and the Pods
			// never roll.
			t.Run("HoldsTheVolumeInOnePhase", func(t *testing.T) {
				held := map[Phase]bool{}
				for _, revision := range []string{
					"", standardRevision, tempRevision, "a-revision-from-another-provider",
				} {
					change, err := ChangePhase(tc.provider, revision)
					assert.NilError(t, err)
					held[change.Phase] = change.Phase == StageCredentials
				}

				assert.DeepEqual(t, held, map[Phase]bool{
					InitialSetup:     false,
					Configured:       false,
					StageCredentials: true,
					Finalize:         false,
				})
			})
		})
	}
}

// TestMountedCredentialPathsAgree pins the agreement between the paths a key
// provider is pointed at and the files the Pod actually has. pg_tde resolves
// one path per credential cluster-wide, so a provider naming a path nothing was
// projected to fails every request for the key, and a credential projected
// under a name the provider does not know is dead weight in the Pod.
func TestMountedCredentialPathsAgree(t *testing.T) {
	t.Parallel()

	// The mount path is the prefix every projected file appears under, so it
	// has to be the one the providers build their paths from.
	assert.Equal(t, postgres.PGTDEVolumeMount().MountPath, naming.PGTDEMountPath)
	assert.Equal(t, postgres.PGTDEVolumeMount().Name, naming.PGTDEVolume)

	// mountedPaths returns the paths of the files the database container reads
	// out of the pg-tde volume.
	mountedPaths := func(spec *crunchyv1beta1.PGTDESpec) []string {
		var paths []string
		for _, source := range postgres.PGTDEVolume(spec).Projected.Sources {
			for _, item := range source.Secret.Items {
				paths = append(paths, postgres.PGTDEVolumeMount().MountPath+"/"+item.Path)
			}
		}
		sort.Strings(paths)
		return paths
	}

	vaultWithCA := testVaultSpec()
	vaultNoCA := testVaultSpec()
	vaultNoCA.CASecret = crunchyv1beta1.PGTDESecretObjectReference{}

	for _, tc := range []struct {
		name string
		spec *crunchyv1beta1.PGTDESpec
	}{
		{"Vault", &crunchyv1beta1.PGTDESpec{Enabled: true, Vault: vaultWithCA}},
		{"VaultWithoutCA", &crunchyv1beta1.PGTDESpec{Enabled: true, Vault: vaultNoCA}},
		{"File", &crunchyv1beta1.PGTDESpec{Enabled: true, File: testFileSpec()}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cluster := &crunchyv1beta1.PostgresCluster{}
			cluster.Spec.Extensions.PGTDE = *tc.spec

			provider := NewProviderForCluster(cluster)
			assert.Assert(t, provider != nil)

			assert.DeepEqual(t, providerPaths(provider.GetCredentialPath()),
				mountedPaths(tc.spec))

			// The staged copies are written to the data volume instead, so they
			// must not collide with the mounted ones: the two-phase change
			// rewrites the provider from one set to the other.
			for _, staged := range providerPaths(provider.GetStagedCredentialPath()) {
				assert.Assert(t, !strings.HasPrefix(staged, naming.PGTDEMountPath+"/"),
					"a staged credential must not shadow a mounted one, got %q", staged)
			}
		})
	}
}

// providerPaths returns the paths a key provider names, sorted.
func providerPaths(path CredentialPath) []string {
	var paths []string

	if vault := path.VaultProvider; vault != nil {
		for _, p := range []string{vault.TokenPath, vault.CAPath} {
			if p != "" {
				paths = append(paths, p)
			}
		}
	}
	if file := path.FileProvider; file != nil && file.KeyPath != "" {
		paths = append(paths, file.KeyPath)
	}

	sort.Strings(paths)
	return paths
}

// revisions returns the revision of the credentials named in the spec and the
// revision of their staged copies, the two values ChangePhase compares
// Status.PGTDERevision against.
func revisions(t *testing.T, provider KeyProvider) (standard, staged string) {
	t.Helper()

	standard, err := provider.GetRevision(provider.GetCredentialPath())
	assert.NilError(t, err)
	staged, err = provider.GetRevision(provider.GetStagedCredentialPath())
	assert.NilError(t, err)

	return standard, staged
}
