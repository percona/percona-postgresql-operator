package naming

const (
	ConditionClusterIsReadyForBackup = "ReadyForBackup"
	ConditionAPIGroupMigration       = "APIGroupMigration"

	// ConditionStandbyLagging is the type used in a condition to indicate whether or not
	// the standby cluster is lagging behind the main site
	ConditionStandbyLagging = "StandbyLagging"

	// ConditionReadyForLogicalReplication reports whether the primary carries
	// everything a logical replica bootstrap needs. A bootstrap started too
	// early cannot be retried without re-seeding.
	ConditionReadyForLogicalReplication = "ReadyForLogicalReplication"
)
