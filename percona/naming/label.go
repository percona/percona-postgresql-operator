package naming

const (
	LabelOperatorVersion = PrefixPerconaPGV2 + "version"

	// LabelLogicalReplica marks every object that belongs to a logical replica
	// with the replica's name.
	LabelLogicalReplica = PrefixPerconaPGV2 + "logical-replica"
	// LabelBackupSource is set on PerconaPGBackup resources to indicate how
	// the backup was triggered.
	LabelBackupSource = PrefixPerconaPGV2 + "backup-source"

	// LabelBackupSourceScheduled is the value of LabelBackupSource for backups
	// created by the cluster's backup schedule.
	LabelBackupSourceScheduled = "scheduled"
)
