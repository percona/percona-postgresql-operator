package pgtde

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"gotest.tools/v3/assert"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"

	"github.com/percona/percona-postgresql-operator/v2/internal/naming"
	"github.com/percona/percona-postgresql-operator/v2/internal/postgres"
	crunchyv1beta1 "github.com/percona/percona-postgresql-operator/v2/pkg/apis/postgres-operator.crunchydata.com/v1beta1"
)

func TestEnableInPostgreSQL(t *testing.T) {
	expected := errors.New("whoops")
	exec := func(
		_ context.Context, stdin io.Reader, stdout, stderr io.Writer, command ...string,
	) error {
		assert.Assert(t, stdout != nil, "should capture stdout")
		assert.Assert(t, stderr != nil, "should capture stderr")

		assert.Assert(t, strings.Contains(strings.Join(command, "\n"),
			`SELECT datname FROM pg_catalog.pg_database`,
		), "expected all databases and templates")

		b, err := io.ReadAll(stdin)
		assert.NilError(t, err)
		assert.Equal(t, string(b), strings.Join([]string{
			`SET client_min_messages = WARNING;`,
			`CREATE EXTENSION IF NOT EXISTS pg_tde;`,
			`ALTER EXTENSION pg_tde UPDATE;`,
		}, "\n"))

		return expected
	}

	ctx := t.Context()
	assert.Equal(t, expected, enableInPostgreSQL(ctx, exec))
}

func TestDisableInPostgreSQL(t *testing.T) {
	expected := errors.New("whoops")
	exec := func(
		_ context.Context, stdin io.Reader, stdout, stderr io.Writer, command ...string,
	) error {
		assert.Assert(t, stdout != nil, "should capture stdout")
		assert.Assert(t, stderr != nil, "should capture stderr")

		assert.Assert(t, strings.Contains(strings.Join(command, "\n"),
			`SELECT datname FROM pg_catalog.pg_database`,
		), "expected all databases and templates")

		b, err := io.ReadAll(stdin)
		assert.NilError(t, err)
		assert.Equal(t, string(b), strings.Join([]string{
			`SET client_min_messages = WARNING;`,
			`DROP EXTENSION IF EXISTS pg_tde;`,
		}, "\n"))

		return expected
	}

	ctx := context.Background()
	assert.Equal(t, expected, disableInPostgreSQL(ctx, exec))
}

func TestPostgreSQLParameters(t *testing.T) {
	parameters := postgres.Parameters{
		Mandatory: postgres.NewParameterSet(),
	}

	// No comma when empty.
	PostgreSQLParameters(&parameters)

	assert.Assert(t, parameters.Default == nil)
	assert.DeepEqual(t, parameters.Mandatory.AsMap(), map[string]string{
		"shared_preload_libraries": "pg_tde",
	})

	// Appended when not empty.
	parameters.Mandatory.Add("shared_preload_libraries", "some,existing")
	PostgreSQLParameters(&parameters)

	assert.Assert(t, parameters.Default == nil)
	assert.DeepEqual(t, parameters.Mandatory.AsMap(), map[string]string{
		"shared_preload_libraries": "some,existing,pg_tde",
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
		vault := &crunchyv1beta1.PGTDEVaultSpec{
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
		assert.Equal(t, expected, addVaultProvider(ctx, exec, vault))
	})

	t.Run("already exists", func(t *testing.T) {
		exec := func(
			_ context.Context, stdin io.Reader, stdout, stderr io.Writer, command ...string,
		) error {
			_, _ = stderr.Write([]byte("ERROR: already exists"))
			return nil
		}

		ctx := context.Background()
		vault := &crunchyv1beta1.PGTDEVaultSpec{
			Host:      "https://vault.example.com",
			MountPath: "secret/data",
			TokenSecret: crunchyv1beta1.PGTDESecretObjectReference{
				Name: "token-secret",
				Key:  "token-key",
			},
		}
		assert.Assert(t, errors.Is(addVaultProvider(ctx, exec, vault), errAlreadyExists))
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
		vault := &crunchyv1beta1.PGTDEVaultSpec{
			Host:      "https://vault.example.com",
			MountPath: "secret/data",
			TokenSecret: crunchyv1beta1.PGTDESecretObjectReference{
				Name: "token-secret",
				Key:  "token-key",
			},
		}
		assert.NilError(t, addVaultProvider(ctx, exec, vault))
	})
}

func TestCreateGlobalKey(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		expected := errors.New("whoops")
		clusterID := types.UID("test-cluster-uid")
		exec := func(
			_ context.Context, stdin io.Reader, stdout, stderr io.Writer, command ...string,
		) error {
			assert.Assert(t, stdout != nil, "should capture stdout")
			assert.Assert(t, stderr != nil, "should capture stderr")

			b, err := io.ReadAll(stdin)
			assert.NilError(t, err)
			sql := string(b)

			assert.Assert(t, strings.Contains(sql, "pg_tde_create_key_using_global_key_provider"))

			joined := strings.Join(command, " ")
			assert.Assert(t, strings.Contains(joined, "--set=provider_name="+naming.PGTDEVaultProvider))
			assert.Assert(t, strings.Contains(joined,
				"--set=global_key="+fmt.Sprintf("%s-%s", naming.PGTDEGlobalKey, clusterID)))

			return expected
		}

		ctx := t.Context()
		assert.Equal(t, expected, createGlobalKey(ctx, exec, clusterID))
	})

	t.Run("already exists", func(t *testing.T) {
		clusterID := types.UID("test-cluster-uid")
		exec := func(
			_ context.Context, stdin io.Reader, stdout, stderr io.Writer, command ...string,
		) error {
			_, _ = stderr.Write([]byte("ERROR: already exists"))
			return nil
		}

		ctx := t.Context()
		assert.Assert(t, errors.Is(createGlobalKey(ctx, exec, clusterID), errAlreadyExists))
	})
}

func TestSetDefaultKey(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		expected := errors.New("whoops")
		clusterID := types.UID("test-cluster-uid")
		exec := func(
			_ context.Context, stdin io.Reader, stdout, stderr io.Writer, command ...string,
		) error {
			assert.Assert(t, stdout != nil, "should capture stdout")
			assert.Assert(t, stderr != nil, "should capture stderr")

			b, err := io.ReadAll(stdin)
			assert.NilError(t, err)
			sql := string(b)

			assert.Assert(t, strings.Contains(sql, "pg_tde_set_default_key_using_global_key_provider"))

			joined := strings.Join(command, " ")
			assert.Assert(t, strings.Contains(joined, "--set=provider_name="+naming.PGTDEVaultProvider))
			assert.Assert(t, strings.Contains(joined,
				"--set=global_key="+fmt.Sprintf("%s-%s", naming.PGTDEGlobalKey, clusterID)))

			return expected
		}

		ctx := context.Background()
		assert.Equal(t, expected, setDefaultKey(ctx, exec, clusterID))
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
		vault := &crunchyv1beta1.PGTDEVaultSpec{
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
		assert.Equal(t, expected, changeVaultProvider(ctx, exec, vault))
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
		vault := &crunchyv1beta1.PGTDEVaultSpec{
			Host:      "https://vault.example.com",
			MountPath: "secret/data",
			TokenSecret: crunchyv1beta1.PGTDESecretObjectReference{
				Name: "token-secret",
				Key:  "token-key",
			},
		}
		assert.NilError(t, changeVaultProvider(ctx, exec, vault))
	})
}

func TestReconcileExtension(t *testing.T) {
	t.Run("disabled successfully", func(t *testing.T) {
		exec := func(
			_ context.Context, stdin io.Reader, stdout, stderr io.Writer, command ...string,
		) error {
			return nil
		}

		ctx := t.Context()
		recorder := record.NewFakeRecorder(10)
		cluster := &crunchyv1beta1.PostgresCluster{}
		cluster.Spec.Extensions.PGTDE.Enabled = false
		cluster.Generation = 1

		err := ReconcileExtension(ctx, exec, recorder, cluster)
		assert.NilError(t, err)

		condition := meta.FindStatusCondition(cluster.Status.Conditions, crunchyv1beta1.PGTDEEnabled)
		assert.Assert(t, condition != nil)
		assert.Equal(t, condition.Status, metav1.ConditionFalse)
		assert.Equal(t, condition.Reason, "Disabled")
		assert.Equal(t, condition.Message, "pg_tde is disabled in PerconaPGCluster")
		assert.Equal(t, condition.ObservedGeneration, int64(1))
	})

	t.Run("disable error records event", func(t *testing.T) {
		expected := errors.New("disable failed")
		exec := func(
			_ context.Context, stdin io.Reader, stdout, stderr io.Writer, command ...string,
		) error {
			return expected
		}

		ctx := t.Context()
		recorder := record.NewFakeRecorder(10)
		cluster := &crunchyv1beta1.PostgresCluster{}
		cluster.Spec.Extensions.PGTDE.Enabled = false

		err := ReconcileExtension(ctx, exec, recorder, cluster)
		assert.Equal(t, expected, err)

		select {
		case event := <-recorder.Events:
			assert.Assert(t, strings.Contains(event, "pgTdeEnabled"))
			assert.Assert(t, strings.Contains(event, "Unable to disable pg_tde"))
		default:
			t.Fatal("expected event to be recorded")
		}
	})

	t.Run("enabled successfully", func(t *testing.T) {
		exec := func(
			_ context.Context, stdin io.Reader, stdout, stderr io.Writer, command ...string,
		) error {
			return nil
		}

		ctx := t.Context()
		recorder := record.NewFakeRecorder(10)
		cluster := &crunchyv1beta1.PostgresCluster{}
		cluster.Spec.Extensions.PGTDE.Enabled = true
		cluster.Generation = 2

		err := ReconcileExtension(ctx, exec, recorder, cluster)
		assert.NilError(t, err)

		condition := meta.FindStatusCondition(cluster.Status.Conditions, crunchyv1beta1.PGTDEEnabled)
		assert.Assert(t, condition != nil)
		assert.Equal(t, condition.Status, metav1.ConditionTrue)
		assert.Equal(t, condition.Reason, "Enabled")
		assert.Equal(t, condition.Message, "pg_tde is enabled in PerconaPGCluster")
		assert.Equal(t, condition.ObservedGeneration, int64(2))
	})

	t.Run("enable error records event", func(t *testing.T) {
		expected := errors.New("enable failed")
		exec := func(
			_ context.Context, stdin io.Reader, stdout, stderr io.Writer, command ...string,
		) error {
			return expected
		}

		ctx := t.Context()
		recorder := record.NewFakeRecorder(10)
		cluster := &crunchyv1beta1.PostgresCluster{}
		cluster.Spec.Extensions.PGTDE.Enabled = true

		err := ReconcileExtension(ctx, exec, recorder, cluster)
		assert.Equal(t, expected, err)

		select {
		case event := <-recorder.Events:
			assert.Assert(t, strings.Contains(event, "pgTdeDisabled"))
			assert.Assert(t, strings.Contains(event, "Unable to install pg_tde"))
		default:
			t.Fatal("expected event to be recorded")
		}
	})
}

func TestReconcileVaultProvider(t *testing.T) {
	vault := &crunchyv1beta1.PGTDEVaultSpec{
		Host:      "https://vault.example.com",
		MountPath: "secret/data",
		TokenSecret: crunchyv1beta1.PGTDESecretObjectReference{
			Name: "token-secret",
			Key:  "token-key",
		},
	}

	t.Run("first time all succeed", func(t *testing.T) {
		callCount := 0
		exec := func(
			_ context.Context, stdin io.Reader, stdout, stderr io.Writer, command ...string,
		) error {
			callCount++
			return nil
		}

		ctx := t.Context()
		cluster := &crunchyv1beta1.PostgresCluster{}
		cluster.Spec.Extensions.PGTDE.Vault = vault
		cluster.UID = "test-uid"

		err := ReconcileVaultProvider(ctx, exec, cluster)
		assert.NilError(t, err)
		assert.Equal(t, callCount, 3)
	})

	t.Run("first time addVaultProvider fails", func(t *testing.T) {
		expected := errors.New("vault error")
		exec := func(
			_ context.Context, stdin io.Reader, stdout, stderr io.Writer, command ...string,
		) error {
			return expected
		}

		ctx := t.Context()
		cluster := &crunchyv1beta1.PostgresCluster{}
		cluster.Spec.Extensions.PGTDE.Vault = vault
		cluster.UID = "test-uid"

		err := ReconcileVaultProvider(ctx, exec, cluster)
		assert.Equal(t, expected, err)
	})

	t.Run("first time addVaultProvider already exists proceeds", func(t *testing.T) {
		callCount := 0
		exec := func(
			_ context.Context, stdin io.Reader, stdout, stderr io.Writer, command ...string,
		) error {
			callCount++
			if callCount == 1 {
				_, _ = stderr.Write([]byte("already exists"))
				return nil
			}
			return nil
		}

		ctx := t.Context()
		cluster := &crunchyv1beta1.PostgresCluster{}
		cluster.Spec.Extensions.PGTDE.Vault = vault
		cluster.UID = "test-uid"

		err := ReconcileVaultProvider(ctx, exec, cluster)
		assert.NilError(t, err)
		assert.Equal(t, callCount, 3)
	})

	t.Run("first time createGlobalKey fails", func(t *testing.T) {
		expected := errors.New("key error")
		callCount := 0
		exec := func(
			_ context.Context, stdin io.Reader, stdout, stderr io.Writer, command ...string,
		) error {
			callCount++
			if callCount == 2 {
				return expected
			}
			return nil
		}

		ctx := t.Context()
		cluster := &crunchyv1beta1.PostgresCluster{}
		cluster.Spec.Extensions.PGTDE.Vault = vault
		cluster.UID = "test-uid"

		err := ReconcileVaultProvider(ctx, exec, cluster)
		assert.Equal(t, expected, err)
		assert.Equal(t, callCount, 2)
	})

	t.Run("first time createGlobalKey already exists proceeds", func(t *testing.T) {
		callCount := 0
		exec := func(
			_ context.Context, stdin io.Reader, stdout, stderr io.Writer, command ...string,
		) error {
			callCount++
			if callCount == 2 {
				_, _ = stderr.Write([]byte("already exists"))
				return nil
			}
			return nil
		}

		ctx := t.Context()
		cluster := &crunchyv1beta1.PostgresCluster{}
		cluster.Spec.Extensions.PGTDE.Vault = vault
		cluster.UID = "test-uid"

		err := ReconcileVaultProvider(ctx, exec, cluster)
		assert.NilError(t, err)
		assert.Equal(t, callCount, 3)
	})

	t.Run("first time setDefaultKey fails", func(t *testing.T) {
		expected := errors.New("default key error")
		callCount := 0
		exec := func(
			_ context.Context, stdin io.Reader, stdout, stderr io.Writer, command ...string,
		) error {
			callCount++
			if callCount == 3 {
				return expected
			}
			return nil
		}

		ctx := t.Context()
		cluster := &crunchyv1beta1.PostgresCluster{}
		cluster.Spec.Extensions.PGTDE.Vault = vault
		cluster.UID = "test-uid"

		err := ReconcileVaultProvider(ctx, exec, cluster)
		assert.Equal(t, expected, err)
		assert.Equal(t, callCount, 3)
	})

	t.Run("revision set calls changeVaultProvider", func(t *testing.T) {
		callCount := 0
		exec := func(
			_ context.Context, stdin io.Reader, stdout, stderr io.Writer, command ...string,
		) error {
			callCount++
			b, _ := io.ReadAll(stdin)
			assert.Assert(t, strings.Contains(string(b), "pg_tde_change_global_key_provider_vault_v2"))
			return nil
		}

		ctx := t.Context()
		cluster := &crunchyv1beta1.PostgresCluster{}
		cluster.Spec.Extensions.PGTDE.Vault = vault
		cluster.Status.PGTDERevision = "some-revision"
		cluster.UID = "test-uid"

		err := ReconcileVaultProvider(ctx, exec, cluster)
		assert.NilError(t, err)
		assert.Equal(t, callCount, 1)
	})

	t.Run("revision set changeVaultProvider fails", func(t *testing.T) {
		expected := errors.New("change error")
		exec := func(
			_ context.Context, stdin io.Reader, stdout, stderr io.Writer, command ...string,
		) error {
			return expected
		}

		ctx := t.Context()
		cluster := &crunchyv1beta1.PostgresCluster{}
		cluster.Spec.Extensions.PGTDE.Vault = vault
		cluster.Status.PGTDERevision = "some-revision"
		cluster.UID = "test-uid"

		err := ReconcileVaultProvider(ctx, exec, cluster)
		assert.Equal(t, expected, err)
	})
}
