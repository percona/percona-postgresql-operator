package naming

// PerconaPGCluster finalizers
const (
	FinalizerDeletePVC     = PrefixPercona + "delete-pvc"
	FinalizerDeleteSSL     = PrefixPercona + "delete-ssl"
	FinalizerStopWatchers  = PrefixPercona + "stop-watchers" //nolint:gosec
	FinalizerDeleteBackups = PrefixPercona + "delete-backups"
)

// PerconaPGRestore finalizers
const (
	FinalizerDeleteRestore = PrefixPercona + "delete-restore" //nolint:gosec
	FinalizerDeleteBackup  = PrefixPercona + "delete-backup"  //nolint:gosec
)
