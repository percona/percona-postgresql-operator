package pgstatstatements

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/pkg/errors"
	"gotest.tools/v3/assert"

	"github.com/percona/percona-postgresql-operator/v3/internal/patroni"
	"github.com/percona/percona-postgresql-operator/v3/internal/postgres"
	"github.com/percona/percona-postgresql-operator/v3/pkg/apis/upstream.pgv2.percona.com/v1beta1"
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
		assert.Equal(t, string(b), strings.Trim(`
SET client_min_messages = WARNING; CREATE EXTENSION IF NOT EXISTS pg_stat_statements;
		`, "\t\n"))

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
		assert.Equal(t, string(b), strings.Trim(`
SET client_min_messages = WARNING; DROP EXTENSION IF EXISTS pg_stat_statements;
		`, "\t\n"))

		return expected
	}

	ctx := context.Background()
	assert.Equal(t, expected, DisableInPostgreSQL(ctx, exec))
}

func TestPostgreSQLParameters(t *testing.T) {
	newCluster := func(crVersion string) *v1beta1.PostgresCluster {
		cluster := new(v1beta1.PostgresCluster)
		cluster.Labels = map[string]string{v1beta1.LabelVersion: crVersion}
		return cluster
	}

	t.Run("crVersion < 3.2.0", func(t *testing.T) {
		cluster := newCluster("3.1.0")
		parameters := postgres.Parameters{
			Mandatory: postgres.NewParameterSet(),
		}

		// No comma when empty.
		PostgreSQLParameters(cluster, &parameters)

		assert.Assert(t, parameters.Default == nil)
		assert.DeepEqual(t, parameters.Mandatory.AsMap(), map[string]string{
			"pg_stat_statements.track": "all",
			"shared_preload_libraries": "pg_stat_statements",
		})

		// Appended when not empty.
		parameters.Mandatory.Add("shared_preload_libraries", "some,existing")
		PostgreSQLParameters(cluster, &parameters)

		assert.Assert(t, parameters.Default == nil)
		assert.DeepEqual(t, parameters.Mandatory.AsMap(), map[string]string{
			"pg_stat_statements.track": "all",
			"shared_preload_libraries": "some,existing,pg_stat_statements",
		})
	})

	t.Run("crVersion >= 3.2.0", func(t *testing.T) {
		cluster := newCluster("3.2.0")
		parameters := postgres.Parameters{
			Default:   postgres.NewParameterSet(),
			Mandatory: postgres.NewParameterSet(),
		}

		PostgreSQLParameters(cluster, &parameters)

		assert.DeepEqual(t, parameters.Default.AsMap(), map[string]string{
			"pg_stat_statements.track": "top",
		})
		assert.DeepEqual(t, parameters.Mandatory.AsMap(), map[string]string{
			"shared_preload_libraries": "pg_stat_statements",
		})
	})

	t.Run("crVersion >= 3.2.0 user value is honoured", func(t *testing.T) {
		cluster := newCluster("3.2.0")
		cluster.Spec.PostgresVersion = 17
		assert.NilError(t, cluster.Default(context.Background(), nil))

		parameters := postgres.NewParameters()
		PostgreSQLParameters(cluster, &parameters)

		get := func(input map[string]any) any {
			config := patroni.DynamicConfiguration(cluster, input, postgres.HBAs{}, parameters)
			return config["postgresql"].(map[string]any)["parameters"].(map[string]any)["pg_stat_statements.track"]
		}

		assert.Equal(t, get(nil), "top")
		assert.Equal(t, get(map[string]any{
			"postgresql": map[string]any{
				"parameters": map[string]any{"pg_stat_statements.track": "all"},
			},
		}), "all")
	})
}
