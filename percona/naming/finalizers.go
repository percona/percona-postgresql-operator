package naming

// PerconaPGCluster finalizers
const (
	FinalizerDeletePVC    = PrefixPercona + "delete-pvc"
	FinalizerDeleteSSL    = PrefixPercona + "delete-ssl"
	FinalizerStopWatchers = PrefixPercona + "stop-watchers" //nolint:gosec
)

// PerconaPGRestore finalizers
const (
	FinalizerDeleteRestore = PrefixPercona + "delete-restore" //nolint:gosec
)
