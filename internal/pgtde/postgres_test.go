package pgtde

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"gotest.tools/v3/assert"
	"k8s.io/apimachinery/pkg/types"

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

	ctx := context.Background()
	assert.Equal(t, expected, EnableInPostgreSQL(ctx, exec))
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
	assert.Equal(t, expected, DisableInPostgreSQL(ctx, exec))
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
		assert.Equal(t, expected, AddVaultProvider(ctx, exec, vault))
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
		assert.Assert(t, errors.Is(AddVaultProvider(ctx, exec, vault), ErrAlreadyExists))
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
		assert.NilError(t, AddVaultProvider(ctx, exec, vault))
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

		ctx := context.Background()
		assert.Equal(t, expected, CreateGlobalKey(ctx, exec, clusterID))
	})

	t.Run("already exists", func(t *testing.T) {
		clusterID := types.UID("test-cluster-uid")
		exec := func(
			_ context.Context, stdin io.Reader, stdout, stderr io.Writer, command ...string,
		) error {
			_, _ = stderr.Write([]byte("ERROR: already exists"))
			return nil
		}

		ctx := context.Background()
		assert.Assert(t, errors.Is(CreateGlobalKey(ctx, exec, clusterID), ErrAlreadyExists))
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
		assert.Equal(t, expected, SetDefaultKey(ctx, exec, clusterID))
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
		assert.Equal(t, expected, ChangeVaultProvider(ctx, exec, vault))
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
		assert.NilError(t, ChangeVaultProvider(ctx, exec, vault))
	})
}
