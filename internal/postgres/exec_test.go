// Copyright 2021 - 2024 Crunchy Data Solutions, Inc.
//
// SPDX-License-Identifier: Apache-2.0

package postgres

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/go-logr/logr/funcr"
	"github.com/pkg/errors"
	"gotest.tools/v3/assert"
	"k8s.io/client-go/rest"

	"github.com/percona/percona-postgresql-operator/v3/internal/controller/runtime"
	"github.com/percona/percona-postgresql-operator/v3/internal/logging"
	"github.com/percona/percona-postgresql-operator/v3/internal/testing/require"
)

// This example demonstrates how Executor can work with exec.Cmd.
func ExampleExecutor_execCmd() {
	_ = Executor(func(
		ctx context.Context, stdin io.Reader, stdout, stderr io.Writer, command ...string,
	) error {
		// #nosec G204 Nothing calls the function defined in this example.
		cmd := exec.CommandContext(ctx, command[0], command[1:]...)
		cmd.Stdin, cmd.Stdout, cmd.Stderr = stdin, stdout, stderr
		return cmd.Run()
	})
}

func TestExecutorExec(t *testing.T) {
	expected := errors.New("pass-through")
	fn := func(
		_ context.Context, stdin io.Reader, stdout, stderr io.Writer, command ...string,
	) error {
		b, err := io.ReadAll(stdin)
		assert.NilError(t, err)
		assert.Equal(t, string(b), `statements; to run;`)

		assert.DeepEqual(t, command, []string{
			"psql", "-Xw", "--file=-",
			"--set=CASE=sEnSiTiVe",
			"--set=different=vars",
			"--set=lots=of",
		})

		_, _ = io.WriteString(stdout, "some stdout")
		_, _ = io.WriteString(stderr, "and stderr")
		return expected
	}

	stdout, stderr, err := Executor(fn).Exec(
		context.Background(),
		strings.NewReader(`statements; to run;`),
		map[string]string{
			"lots":      "of",
			"different": "vars",
			"CASE":      "sEnSiTiVe",
		}, nil)

	assert.Equal(t, expected, err, "expected function to be called")
	assert.Equal(t, stdout, "some stdout")
	assert.Equal(t, stderr, "and stderr")
}

func TestExecutorExecInAllDatabases(t *testing.T) {
	expected := errors.New("exact")
	fn := func(
		_ context.Context, stdin io.Reader, stdout, stderr io.Writer, command ...string,
	) error {
		b, err := io.ReadAll(stdin)
		assert.NilError(t, err)
		assert.Equal(t, string(b), `the; stuff;`)

		assert.DeepEqual(t, command, []string{
			"bash", "-ceu", "--", `
sql_target=$(< /dev/stdin)
sql_databases="$1"
shift 1

databases=$(psql "$@" -Xw -Aqt --file=- <<< "${sql_databases}")
while IFS= read -r database; do
	PGDATABASE="${database}" psql "$@" -Xw --file=- <<< "${sql_target}"
done <<< "${databases}"
`,
			"-",
			`SET search_path = '';SELECT datname FROM pg_catalog.pg_database WHERE datallowconn AND datname NOT IN ('template0')`,
			"--set=CASE=sEnSiTiVe",
			"--set=different=vars",
			"--set=lots=of",
		})

		_, _ = io.WriteString(stdout, "some stdout")
		_, _ = io.WriteString(stderr, "and stderr")
		return expected
	}

	stdout, stderr, err := Executor(fn).ExecInAllDatabases(
		context.Background(),
		`the; stuff;`,
		map[string]string{
			"lots":      "of",
			"different": "vars",
			"CASE":      "sEnSiTiVe",
		})

	assert.Equal(t, expected, err, "expected function to be called")
	assert.Equal(t, stdout, "some stdout")
	assert.Equal(t, stderr, "and stderr")
}

func TestExecutorExecInDatabasesFromQuery(t *testing.T) {
	expected := errors.New("splat")
	fn := func(
		_ context.Context, stdin io.Reader, stdout, stderr io.Writer, command ...string,
	) error {
		b, err := io.ReadAll(stdin)
		assert.NilError(t, err)
		assert.Equal(t, string(b), `statements; to run;`)

		assert.DeepEqual(t, command, []string{
			"bash", "-ceu", "--", `
sql_target=$(< /dev/stdin)
sql_databases="$1"
shift 1

databases=$(psql "$@" -Xw -Aqt --file=- <<< "${sql_databases}")
while IFS= read -r database; do
	PGDATABASE="${database}" psql "$@" -Xw --file=- <<< "${sql_target}"
done <<< "${databases}"
`,
			"-",
			`db query`,
			"--set=CASE=sEnSiTiVe",
			"--set=different=vars",
			"--set=lots=of",
		})

		// Use the PGDATABASE environment variable to ensure the value is not
		// interpreted as a connection string.
		//
		// > $ psql -Xw -d 'host=127.0.0.1'
		// > psql: error: fe_sendauth: no password supplied
		// >
		// > $ PGDATABASE='host=127.0.0.1' psql -Xw
		// > psql: error: FATAL:  database "host=127.0.0.1" does not exist
		//
		// TODO(cbandy): Create a test that actually runs psql.
		assert.Assert(t, strings.Contains(command[3], `PGDATABASE="${database}" psql`))

		_, _ = io.WriteString(stdout, "some stdout")
		_, _ = io.WriteString(stderr, "and stderr")
		return expected
	}

	stdout, stderr, err := Executor(fn).ExecInDatabasesFromQuery(
		context.Background(), `db query`, `statements; to run;`, map[string]string{
			"lots":      "of",
			"different": "vars",
			"CASE":      "sEnSiTiVe",
		})

	assert.Equal(t, expected, err, "expected function to be called")
	assert.Equal(t, stdout, "some stdout")
	assert.Equal(t, stderr, "and stderr")

	t.Run("ShellCheck", func(t *testing.T) {
		shellcheck := require.ShellCheck(t)

		_, _, _ = Executor(func(
			_ context.Context, _ io.Reader, _, _ io.Writer, command ...string,
		) error {
			// Expect a bash command with an inline script.
			assert.DeepEqual(t, command[:3], []string{"bash", "-ceu", "--"})
			assert.Assert(t, len(command) > 3)
			script := command[3]

			// Write out that inline script.
			dir := t.TempDir()
			file := filepath.Join(dir, "script.bash")
			assert.NilError(t, os.WriteFile(file, []byte(script), 0o600))

			// Expect shellcheck to be happy.
			cmd := exec.CommandContext(context.Background(), shellcheck, "--enable=all", file)
			output, err := cmd.CombinedOutput()
			assert.NilError(t, err, "%q\n%s", cmd.Args, output)

			return nil
		}).ExecInDatabasesFromQuery(context.Background(), "", "", nil)
	})
}

func TestExecutorLogging(t *testing.T) {
	const verifier = "SCRAM-SHA-256$4096:c2FsdA==$c3RvcmVk:c2VydmVy"

	capture := func(t *testing.T, verbosity int) (context.Context, *[]map[string]any) {
		var entries []map[string]any
		logger := funcr.NewJSON(func(object string) {
			var entry map[string]any
			assert.NilError(t, json.Unmarshal([]byte(object), &entry))
			entries = append(entries, entry)
		}, funcr.Options{Verbosity: verbosity})
		return logging.NewContext(context.Background(), logger), &entries
	}

	failing := Executor(func(
		_ context.Context, stdin io.Reader, stdout, stderr io.Writer, _ ...string,
	) error {
		_, _ = io.Copy(io.Discard, stdin)
		_, _ = io.WriteString(stdout, "out "+verifier)
		_, _ = io.WriteString(stderr, "ERROR: boom md5"+strings.Repeat("a", 32))
		return errors.New("command terminated with exit code 1")
	})

	t.Run("ExecFailure", func(t *testing.T) {
		ctx, entries := capture(t, 0)

		_, _, err := failing.Exec(ctx,
			strings.NewReader(`ALTER ROLE x PASSWORD '`+verifier+`';`),
			map[string]string{"verifier": "plain", "username": "x", "token_path": "/pgdata/token"},
			[]string{"--no-align"})
		assert.ErrorContains(t, err, "exit code 1")

		assert.Equal(t, len(*entries), 1)
		entry := (*entries)[0]
		assert.Equal(t, entry["msg"], "failed to execute SQL")
		assert.Equal(t, entry["error"], "command terminated with exit code 1")
		assert.Equal(t, entry["sql"], `ALTER ROLE x PASSWORD '<redacted>';`)
		assert.DeepEqual(t, entry["variables"], map[string]any{
			"verifier": "<redacted>", "username": "x", "token_path": "/pgdata/token",
		})
		assert.DeepEqual(t, entry["psqlArgs"], []any{"--no-align"})
		assert.Equal(t, entry["stdout"], "out <redacted>")
		assert.Equal(t, entry["stderr"], "ERROR: boom <redacted>")
		for _, key := range []string{"namespace", "pod", "container"} {
			assert.Assert(t, entry[key] != nil, "missing %q", key)
		}
	})

	t.Run("ExecInDatabasesFromQueryFailure", func(t *testing.T) {
		ctx, entries := capture(t, 0)

		_, _, err := failing.ExecInDatabasesFromQuery(ctx, `db query`, `SELECT 1;`, nil)
		assert.ErrorContains(t, err, "exit code 1")

		assert.Equal(t, len(*entries), 1)
		entry := (*entries)[0]
		assert.Equal(t, entry["msg"], "failed to execute SQL")
		assert.Equal(t, entry["sql"], `SELECT 1;`)
		assert.Equal(t, entry["databasesQuery"], `db query`)
	})

	t.Run("SuccessOnlyAtDebug", func(t *testing.T) {
		succeeding := Executor(func(
			context.Context, io.Reader, io.Writer, io.Writer, ...string,
		) error {
			return nil
		})

		ctx, entries := capture(t, 0)
		_, _, err := succeeding.Exec(ctx, strings.NewReader(`SELECT 1;`), nil, nil)
		assert.NilError(t, err)
		assert.Equal(t, len(*entries), 0)

		ctx, entries = capture(t, 1)
		_, _, err = succeeding.Exec(ctx, strings.NewReader(`SELECT 1;`), nil, nil)
		assert.NilError(t, err)
		assert.Equal(t, len(*entries), 1)
		assert.Equal(t, (*entries)[0]["msg"], "executed SQL")
		assert.Equal(t, (*entries)[0]["sql"], `SELECT 1;`)
	})

	t.Run("SQLOmitted", func(t *testing.T) {
		ctx, entries := capture(t, 0)

		_, _, err := failing.Exec(WithSQLOmittedFromLogs(ctx),
			strings.NewReader(`CREATE ROLE app LOGIN PASSWORD 'plaintext';`), nil, nil)
		assert.ErrorContains(t, err, "exit code 1")

		assert.Equal(t, len(*entries), 1)
		entry := (*entries)[0]
		assert.Equal(t, entry["msg"], "failed to execute SQL")
		assert.Equal(t, entry["sql"], "<omitted>")
		assert.Equal(t, entry["stdout"], "out <redacted>")
		assert.Equal(t, entry["stderr"], "ERROR: boom <redacted>")

		_, _, err = failing.ExecInDatabasesFromQuery(WithSQLOmittedFromLogs(ctx),
			`db query`, `CREATE ROLE app LOGIN PASSWORD 'plaintext';`, nil)
		assert.ErrorContains(t, err, "exit code 1")
		assert.Equal(t, len(*entries), 2)
		assert.Equal(t, (*entries)[1]["sql"], "<omitted>")
	})

	t.Run("Truncation", func(t *testing.T) {
		long := strings.Repeat("x", maxLoggedLength+10)
		assert.Equal(t, redactForLog(long),
			strings.Repeat("x", maxLoggedLength)+"... (truncated)")
	})

	t.Run("LargeSQLIsStreamed", func(t *testing.T) {
		const size = 4 << 20

		// A verifier straddles the end of the logged part of the SQL.
		sql := strings.Repeat("x", maxLoggedLength-20) + "'" + verifier + "'" + strings.Repeat("y", size)

		var received int64
		streaming := Executor(func(
			_ context.Context, stdin io.Reader, _, _ io.Writer, _ ...string,
		) error {
			// The executor gets every byte, unchanged.
			n, err := io.Copy(io.Discard, stdin)
			received = n
			assert.NilError(t, err)
			return errors.New("command terminated with exit code 3")
		})

		ctx, entries := capture(t, 0)
		_, _, err := streaming.Exec(ctx, strings.NewReader(sql), nil, nil)
		assert.ErrorContains(t, err, "exit code 3")
		assert.Equal(t, received, int64(len(sql)))

		assert.Equal(t, len(*entries), 1)
		logged := (*entries)[0]["sql"].(string)
		assert.Assert(t, !strings.Contains(logged, "c2FsdA"), "verifier leaked: %q", logged[len(logged)-100:])
		assert.Equal(t, logged, strings.Repeat("x", maxLoggedLength-20)+"'<redacted>'"+
			strings.Repeat("y", 8)+"... (truncated)")
	})

	t.Run("TruncatedAfterRedaction", func(t *testing.T) {
		// Redacting many verifiers makes the logged part shorter than
		// maxLoggedLength, yet the SQL continues past it.
		sql := strings.Repeat("'"+verifier+"',", sqlPrefixLength) + "SELECT 1;"

		ctx, entries := capture(t, 0)
		_, _, err := failing.Exec(ctx, strings.NewReader(sql), nil, nil)
		assert.ErrorContains(t, err, "exit code 1")

		logged := (*entries)[0]["sql"].(string)
		assert.Assert(t, len(logged) < maxLoggedLength)
		assert.Assert(t, strings.HasSuffix(logged, "... (truncated)"), "%q", logged[len(logged)-40:])
		assert.Assert(t, !strings.Contains(logged, "c2FsdA"))
	})

	t.Run("PrefixReadBeforeExecuting", func(t *testing.T) {
		// The executor never reads stdin, as when psql fails before reading
		// or a PodExecutor is still copying stdin after it returns.
		var calls int
		unread := Executor(func(
			context.Context, io.Reader, io.Writer, io.Writer, ...string,
		) error {
			calls++
			return errors.New("command terminated with exit code 2")
		})

		sql := strings.Repeat("z", 1<<20)
		ctx, entries := capture(t, 0)
		_, _, err := unread.Exec(ctx, strings.NewReader(sql), nil, nil)
		assert.ErrorContains(t, err, "exit code 2")
		assert.Equal(t, (*entries)[0]["sql"], strings.Repeat("z", maxLoggedLength)+"... (truncated)")

		// A reader that fails is not executed.
		broken := errors.New("broken reader")
		_, _, err = unread.Exec(ctx, iotest.ErrReader(broken), nil, nil)
		assert.Error(t, err, "read SQL to log before executing: broken reader")
		assert.Assert(t, errors.Is(err, broken), "the cause is kept")
		assert.Equal(t, calls, 1)
	})

	t.Run("PodExecutorTarget", func(t *testing.T) {
		// Nothing listens on this address, so the exec fails without a network.
		podExec, err := runtime.NewPodExecutor(&rest.Config{Host: "http://127.0.0.1:1"})
		assert.NilError(t, err)

		ctx, entries := capture(t, 0)
		_, _, err = Executor(func(
			ctx context.Context, stdin io.Reader, stdout, stderr io.Writer, command ...string,
		) error {
			return podExec(ctx, "some-ns", "some-pod", "database", stdin, stdout, stderr, command...)
		}).Exec(ctx, strings.NewReader(`SELECT 1;`), nil, nil)
		assert.Assert(t, err != nil)

		assert.Assert(t, len(*entries) >= 1)
		entry := (*entries)[len(*entries)-1]
		assert.Equal(t, entry["msg"], "failed to execute SQL")
		assert.Equal(t, entry["namespace"], "some-ns")
		assert.Equal(t, entry["pod"], "some-pod")
		assert.Equal(t, entry["container"], "database")
		assert.Equal(t, entry["sql"], `SELECT 1;`)
	})
}
