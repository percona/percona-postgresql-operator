package naming

// PerconaPGCluster finalizers
const (
	FinalizerDeletePVC     = PrefixPercona + "delete-pvc"
	FinalizerDeleteSSL     = PrefixPercona + "delete-ssl"
	FinalizerStopWatchers  = PrefixPerconaInternal + "stop-watchers" //nolint:gosec
	FinalizerDeleteBackups = PrefixPercona + "delete-backups"

	FinalizerStopWatchersDeprecated = PrefixPercona + "stop-watchers" //nolint:gosec
)

// PerconaPGRestore finalizers
const (
	FinalizerDeleteRestore = PrefixPerconaInternal + "delete-restore" //nolint:gosec
)

// PerconaPGBackup finalizers
const (
	FinalizerDeleteBackup = PrefixPerconaInternal + "delete-backup" //nolint:gosec
)
