package pgtde

import (
	"context"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/pkg/errors"
	"gotest.tools/v3/assert"
	"k8s.io/apimachinery/pkg/types"

	"github.com/percona/percona-postgresql-operator/v2/internal/naming"
	"github.com/percona/percona-postgresql-operator/v2/internal/postgres"
	crunchyv1beta1 "github.com/percona/percona-postgresql-operator/v2/pkg/apis/upstream.pgv2.percona.com/v1beta1"
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
		"pg_tde.wal_encrypt":       "off",
	})

	// Appended when not empty.
	parameters.Mandatory.Add("shared_preload_libraries", "some,existing")
	PostgreSQLParameters(&parameters)

	assert.Assert(t, parameters.Default == nil)
	assert.DeepEqual(t, parameters.Mandatory.AsMap(), map[string]string{
		"shared_preload_libraries": "some,existing,pg_tde",
		"pg_tde.wal_encrypt":       "off",
	})
}

func TestCreateGlobalKey(t *testing.T) {
	// The key belongs to whichever provider created it, so the caller's provider
	// name has to reach the statement: creating the key against one provider and
	// setting it as the default against another cannot resolve.
	for _, providerName := range []string{
		naming.PGTDEVaultProvider, naming.PGTDEFileProvider,
	} {
		t.Run(providerName, func(t *testing.T) {
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
				assert.Assert(t, strings.Contains(joined, "--set=provider_name="+providerName))
				assert.Assert(t, strings.Contains(joined,
					"--set=global_key="+fmt.Sprintf("%s-%s", naming.PGTDEGlobalKey, clusterID)))

				return expected
			}

			ctx := t.Context()
			assert.Equal(t, expected, createGlobalKey(ctx, exec, clusterID, providerName))
		})
	}

	t.Run("does not interpret stderr", func(t *testing.T) {
		clusterID := types.UID("test-cluster-uid")
		exec := func(
			_ context.Context, stdin io.Reader, stdout, stderr io.Writer, command ...string,
		) error {
			// Whether the key already existed is decided by setDefaultKey,
			// not by reading the message psql printed.
			_, _ = stderr.Write([]byte("ERROR: already exists"))
			return nil
		}

		ctx := t.Context()
		assert.NilError(t, createGlobalKey(ctx, exec, clusterID, naming.PGTDEVaultProvider))
	})
}

func TestSetDefaultKey(t *testing.T) {
	for _, providerName := range []string{
		naming.PGTDEVaultProvider, naming.PGTDEFileProvider,
	} {
		t.Run(providerName, func(t *testing.T) {
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
				assert.Assert(t, strings.Contains(joined, "--set=provider_name="+providerName))
				assert.Assert(t, strings.Contains(joined,
					"--set=global_key="+fmt.Sprintf("%s-%s", naming.PGTDEGlobalKey, clusterID)))

				return expected
			}

			ctx := context.Background()
			assert.Equal(t, expected, setDefaultKey(ctx, exec, clusterID, providerName))
		})
	}
}

func TestReconcileExtension(t *testing.T) {
	// ReconcileExtension must stay free of side effects on the cluster: the
	// controller runs it against a fake executor to hash the SQL it would send.
	t.Run("reports nothing", func(t *testing.T) {
		for _, enabled := range []bool{true, false} {
			var sql string
			exec := func(
				_ context.Context, stdin io.Reader, stdout, stderr io.Writer, command ...string,
			) error {
				b, err := io.ReadAll(stdin)
				assert.NilError(t, err)
				sql = string(b)
				return nil
			}

			cluster := &crunchyv1beta1.PostgresCluster{}
			cluster.Spec.Extensions.PGTDE.Enabled = enabled

			assert.NilError(t, ReconcileExtension(t.Context(), exec, cluster))
			assert.Equal(t, len(cluster.Status.Conditions), 0,
				"a dry run must not claim pg_tde reached any state")

			if enabled {
				assert.Assert(t, strings.Contains(sql, "CREATE EXTENSION IF NOT EXISTS pg_tde"))
			} else {
				assert.Assert(t, strings.Contains(sql, "DROP EXTENSION IF EXISTS pg_tde"))
			}
		}
	})

	t.Run("propagates errors", func(t *testing.T) {
		expected := errors.New("whoops")
		exec := func(
			_ context.Context, stdin io.Reader, stdout, stderr io.Writer, command ...string,
		) error {
			return expected
		}

		cluster := &crunchyv1beta1.PostgresCluster{}
		cluster.Spec.Extensions.PGTDE.Enabled = true

		assert.Equal(t, expected, ReconcileExtension(t.Context(), exec, cluster))
	})
}
