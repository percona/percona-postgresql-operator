package pgcron

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/pkg/errors"
	"gotest.tools/v3/assert"

	"github.com/percona/percona-postgresql-operator/v2/internal/postgres"
)

func TestEnableInPostgreSQL(t *testing.T) {
	expected := errors.New("whoops")
	exec := func(
		_ context.Context, stdin io.Reader, stdout, stderr io.Writer, command ...string,
	) error {
		assert.Assert(t, stdout != nil, "should capture stdout")
		assert.Assert(t, stderr != nil, "should capture stderr")
		assert.DeepEqual(t, command, []string{
			"psql", "-Xw", "--file=-",
			"--dbname=postgres",
			"--set=ON_ERROR_STOP=on",
			"--set=QUIET=on",
		})

		b, err := io.ReadAll(stdin)
		assert.NilError(t, err)
		assert.Equal(t, string(b), `SET client_min_messages = WARNING; CREATE EXTENSION IF NOT EXISTS pg_cron; ALTER EXTENSION pg_cron UPDATE;`)

		return expected
	}

	assert.Equal(t, expected, EnableInPostgreSQL(t.Context(), exec))
}

func TestDisableInPostgreSQL(t *testing.T) {
	expected := errors.New("whoops")
	exec := func(
		_ context.Context, stdin io.Reader, stdout, stderr io.Writer, command ...string,
	) error {
		assert.Assert(t, stdout != nil, "should capture stdout")
		assert.Assert(t, stderr != nil, "should capture stderr")
		assert.DeepEqual(t, command, []string{
			"psql", "-Xw", "--file=-",
			"--dbname=postgres",
			"--set=ON_ERROR_STOP=on",
			"--set=QUIET=on",
		})

		b, err := io.ReadAll(stdin)
		assert.NilError(t, err)
		assert.Equal(t, string(b), `SET client_min_messages = WARNING; DROP EXTENSION IF EXISTS pg_cron;`)

		return expected
	}

	assert.Equal(t, expected, DisableInPostgreSQL(t.Context(), exec))
}

func TestPostgreSQLParameters(t *testing.T) {
	parameters := postgres.Parameters{
		Mandatory: postgres.NewParameterSet(),
	}

	PostgreSQLParameters(&parameters)

	assert.Assert(t, parameters.Default == nil)
	assert.DeepEqual(t, parameters.Mandatory.AsMap(), map[string]string{
		"shared_preload_libraries": "pg_cron",
	})

	parameters.Mandatory.Add("shared_preload_libraries", "some,existing")
	PostgreSQLParameters(&parameters)

	assert.Assert(t, parameters.Default == nil)
	assert.DeepEqual(t, parameters.Mandatory.AsMap(), map[string]string{
		"shared_preload_libraries": "some,existing,pg_cron",
	})
}

func TestEnableInPostgreSQLDoesNotUseAllDatabases(t *testing.T) {
	exec := func(
		_ context.Context, _ io.Reader, _, _ io.Writer, command ...string,
	) error {
		assert.Assert(t, !strings.Contains(
			strings.Join(command, "\n"),
			`SELECT datname FROM pg_catalog.pg_database`,
		), "expected only the postgres database")
		return nil
	}

	assert.NilError(t, EnableInPostgreSQL(t.Context(), exec))
}
