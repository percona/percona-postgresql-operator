// Copyright 2021 - 2024 Crunchy Data Solutions, Inc.
//
// SPDX-License-Identifier: Apache-2.0

package postgres

import (
	"bytes"
	"context"
	"io"
	"regexp"
	"sort"
	"strings"

	"github.com/pkg/errors"

	"github.com/percona/percona-postgresql-operator/v3/internal/controller/runtime"
	"github.com/percona/percona-postgresql-operator/v3/internal/logging"
)

// Executor provides methods for calling "psql".
type Executor func(
	ctx context.Context, stdin io.Reader, stdout, stderr io.Writer, command ...string,
) error

// Exec uses "psql" to execute sql. The sql statement(s) are passed via stdin
// and may contain psql variables that are assigned from the variables map.
// - https://www.postgresql.org/docs/current/app-psql.html#APP-PSQL-VARIABLES
func (exec Executor) Exec(
	ctx context.Context, sql io.Reader, variables map[string]string,
	psqlArgs []string,
) (string, string, error) {
	// Convert variables into `psql` arguments.
	args := make([]string, 0, len(variables)+len(psqlArgs))
	for k, v := range variables {
		args = append(args, "--set="+k+"="+v)
	}

	args = append(args, psqlArgs...)

	// The map iteration above is nondeterministic. Sort the arguments so that
	// calls to exec are deterministic.
	// - https://golang.org/ref/spec#For_range
	sort.Strings(args)

	// Read the beginning of the statement(s) before executing them so it can
	// be logged. The rest still streams to "psql" and is never held in memory.
	var prefix string
	var truncated bool
	if sql != nil && !sqlOmittedFromLogs(ctx) {
		var err error
		if prefix, truncated, sql, err = readSQLPrefix(sql); err != nil {
			return "", "", err
		}
	}

	// Execute `psql` without reading config files nor prompting for a password.
	ctx, target := runtime.WithPodExecTarget(ctx)
	var stdout, stderr bytes.Buffer
	err := exec(ctx, sql, &stdout, &stderr,
		append([]string{"psql", "-Xw", "--file=-"}, args...)...)

	logExecution(ctx, err, target, prefix, truncated, variables, stdout.String(), stderr.String(),
		"psqlArgs", psqlArgs)

	return stdout.String(), stderr.String(), err
}

// ExecInAllDatabases uses "bash" and "psql" to execute sql in every database
// that allows connections, including templates. The sql command(s) may contain
// psql variables that are assigned from the variables map.
// - https://www.postgresql.org/docs/current/app-psql.html#APP-PSQL-VARIABLES
func (exec Executor) ExecInAllDatabases(
	ctx context.Context, sql string, variables map[string]string,
) (string, string, error) {
	const databases = "" +
		// Prevent unexpected dereferences by emptying "search_path".
		// The "pg_catalog" schema is still searched.
		// - https://www.postgresql.org/docs/current/runtime-config-client.html#GUC-SEARCH-PATH
		`SET search_path = '';` +

		// Return the names of databases that allow connections, including
		// "template1". Exclude "template0" to ensure it is never manipulated.
		// - https://www.postgresql.org/docs/current/managing-databases.html
		`SELECT datname FROM pg_catalog.pg_database` +
		` WHERE datallowconn AND datname NOT IN ('template0')`

	return exec.ExecInDatabasesFromQuery(ctx, databases, sql, variables)
}

// ExecInDatabasesFromQuery uses "bash" and "psql" to execute sql in every
// database returned by the databases query. The sql statement(s) may contain
// psql variables that are assigned from the variables map.
// - https://www.postgresql.org/docs/current/app-psql.html#APP-PSQL-VARIABLES
func (exec Executor) ExecInDatabasesFromQuery(
	ctx context.Context, databases, sql string, variables map[string]string,
) (string, string, error) {
	// Use a Bash loop to call `psql` multiple times. The query to run in every
	// database is passed via standard input while the database query is passed
	// as the first argument. Remaining arguments are passed through to `psql`.
	stdin := strings.NewReader(sql)
	args := []string{databases}
	for k, v := range variables {
		args = append(args, "--set="+k+"="+v)
	}

	// The map iteration above is nondeterministic. Sort the variable arguments
	// so that calls to exec are deterministic.
	// - https://golang.org/ref/spec#For_range
	sort.Strings(args[1:])

	const script = `
sql_target=$(< /dev/stdin)
sql_databases="$1"
shift 1

databases=$(psql "$@" -Xw -Aqt --file=- <<< "${sql_databases}")
while IFS= read -r database; do
	PGDATABASE="${database}" psql "$@" -Xw --file=- <<< "${sql_target}"
done <<< "${databases}"
`

	// Execute the script with some error handling enabled.
	ctx, target := runtime.WithPodExecTarget(ctx)
	var stdout, stderr bytes.Buffer
	err := exec(ctx, stdin, &stdout, &stderr,
		append([]string{"bash", "-ceu", "--", script, "-"}, args...)...)

	logExecution(ctx, err, target, sql, false, variables, stdout.String(), stderr.String(),
		"databasesQuery", redactForLog(databases))

	return stdout.String(), stderr.String(), err
}

type omitSQLFromLogsKey struct{}

// WithSQLOmittedFromLogs returns a copy of ctx in which Exec,
// ExecInAllDatabases, and ExecInDatabasesFromQuery do not log the SQL they
// execute. Use it for SQL the operator did not write, such as databaseInitSQL,
// which may contain plaintext passwords that cannot be reliably redacted.
// Where the SQL ran, stdout, and stderr are still logged.
func WithSQLOmittedFromLogs(ctx context.Context) context.Context {
	return context.WithValue(ctx, omitSQLFromLogsKey{}, true)
}

func sqlOmittedFromLogs(ctx context.Context) bool {
	omit, _ := ctx.Value(omitSQLFromLogsKey{}).(bool)
	return omit
}

// maxLoggedLength is the number of bytes of SQL, stdout, or stderr that are
// logged before being truncated. The operator does not have any SQL this
// large; this protects against edge cases with large custom resources due to
// long names and/or many user/database pairs, and against the output of SQL
// provided by users (e.g. databaseInitSQL).
const maxLoggedLength = 16 << 10

// sensitiveVariable is the psql variable whose value is never logged. The
// pgbouncer and pgmonitor SQL pass password verifiers through it.
const sensitiveVariable = "verifier"

// passwordVerifier matches PostgreSQL password verifiers in either the
// SCRAM-SHA-256 or MD5 format.
// - https://www.postgresql.org/docs/current/catalog-pg-authid.html
var passwordVerifier = regexp.MustCompile(`SCRAM-SHA-256\$[^\s'"]+|md5[0-9a-f]{32}`)

// logExecution logs the SQL that was executed, the pod in which it was
// executed, and what it wrote to stdout and stderr. Failures are logged as
// errors so they can be diagnosed in production; successful executions are
// logged only at debug verbosity. Password verifiers and sensitive variables
// are redacted, and long values are truncated.
func logExecution(
	ctx context.Context, err error, target *runtime.PodExecTarget,
	sql string, sqlTruncated bool, variables map[string]string, stdout, stderr string,
	keysAndValues ...any,
) {
	log := logging.FromContext(ctx)
	if err == nil && !log.V(1).Enabled() {
		return
	}

	vars := make(map[string]string, len(variables))
	for k, v := range variables {
		if k == sensitiveVariable {
			v = "<redacted>"
		}
		vars[k] = redactForLog(v)
	}

	loggedSQL := "<omitted>"
	if !sqlOmittedFromLogs(ctx) {
		loggedSQL = redactPrefixForLog(sql, sqlTruncated)
	}

	log = log.WithValues(
		"namespace", target.Namespace,
		"pod", target.Pod,
		"container", target.Container,
		"sql", loggedSQL,
		"variables", vars,
		"stdout", redactForLog(stdout),
		"stderr", redactForLog(stderr),
	).WithValues(keysAndValues...)

	if err != nil {
		log.Error(err, "failed to execute SQL")
	} else {
		log.V(1).Info("executed SQL")
	}
}

// redactForLog removes password verifiers from s and truncates it to
// maxLoggedLength bytes.
func redactForLog(s string) string {
	return redactPrefixForLog(s, false)
}

// redactPrefixForLog removes password verifiers from s and truncates it to
// maxLoggedLength bytes. When truncated is true, s is only the beginning of a
// longer value. Verifiers are removed before truncating so that none is cut in
// half.
func redactPrefixForLog(s string, truncated bool) string {
	s = passwordVerifier.ReplaceAllString(s, "<redacted>")
	if len(s) > maxLoggedLength {
		s, truncated = s[:maxLoggedLength], true
	}
	if truncated {
		s += "... (truncated)"
	}
	return s
}

// sqlPrefixLength is how much of the SQL is read for logging. It exceeds
// maxLoggedLength so that a password verifier (~130 bytes) straddling the
// logged part is read whole and redacted before that part is truncated.
const sqlPrefixLength = maxLoggedLength + 256

// readSQLPrefix reads up to sqlPrefixLength bytes from sql and returns them
// with a reader that yields all of sql, prefix included. Only the prefix is
// held in memory. truncated is true when sql may continue past the prefix.
func readSQLPrefix(sql io.Reader) (prefix string, truncated bool, all io.Reader, err error) {
	buffer := make([]byte, sqlPrefixLength)
	n, err := io.ReadFull(sql, buffer)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return "", false, nil, errors.Wrap(err, "read SQL to log before executing")
	}

	// ReadFull returns no error only when it filled the buffer.
	buffer = buffer[:n]
	return string(buffer), err == nil, io.MultiReader(bytes.NewReader(buffer), sql), nil
}
