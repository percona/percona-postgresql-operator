package naming

const (
	ConditionClusterIsReadyForBackup = "ReadyForBackup"
	ConditionAPIGroupMigration       = "APIGroupMigration"

	// ConditionStandbyLagging is the type used in a condition to indicate whether or not
	// the standby cluster is lagging behind the main site
	ConditionStandbyLagging = "StandbyLagging"
)
