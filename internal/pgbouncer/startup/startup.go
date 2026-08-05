// Package startup holds the contract between the operator and the
// pgbouncer-startup binary that runs as the PgBouncer container's startup
// probe: the paths it reads, the environment it expects, and the admin
// credentials it connects with.
//
// The binary is copied out of the operator image into the PgBouncer image by
// the init container, so it is built with CGO_ENABLED=0 to stay independent of
// the target image's libc. This package must therefore stay free of any
// dependency that requires cgo — notably internal/postgres, which pulls in
// pg_query_go.
package startup

const (
	// ConfigDirectory is where the PgBouncer configuration volume is mounted.
	ConfigDirectory = "/etc/pgbouncer"

	// LogDirectory is where the PgBouncer log volume is mounted.
	LogDirectory = "/var/logs"

	// LogAbsolutePath is where the binary appends its own output, so that a
	// failed startup can be inspected after the fact.
	LogAbsolutePath = LogDirectory + "/startup.log"

	// PausedFileProjectionPath is the paused marker's path within the
	// configuration volume, and PausedFileAbsolutePath is its full path. The
	// marker is projected optionally: it is absent unless the cluster is paused.
	PausedFileProjectionPath = "pgbouncer-paused"
	PausedFileAbsolutePath   = ConfigDirectory + "/" + PausedFileProjectionPath

	// PausedValue is the marker's content when connections should be paused.
	PausedValue = "1"

	// AdminUser is the PgBouncer user allowed to issue PAUSE and RESUME.
	AdminUser = "_crunchypgbounceradmin"

	// AdminPasswordEnvVar names the environment variable holding AdminUser's
	// password in the PgBouncer container.
	AdminPasswordEnvVar = "PGBOUNCER_ADMIN_PASSWORD" // #nosec G101 this is a name, not a credential

	// PauseTimeoutSeconds is the timeout for setting pause.
	PauseTimeoutSeconds = 30
)
