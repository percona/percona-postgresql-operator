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

	// FinalizerSnapshotInProgress is set on PerconaPGBackup objects.
	// It ensures that any changes made to the PGCluster are reverted upon
	// snapshot completion (success or failure) or pre-mature deletion of the PGBackup.
	FinalizerSnapshotInProgress = PrefixPercona + "snapshot-in-progress" //nolint:gosec
)

// PerconaPGBackup job finalizers
const (
	FinalizerKeepJob = PrefixPerconaInternal + "keep-job" //nolint:gosec
)
