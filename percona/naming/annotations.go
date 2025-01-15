package naming

const (
	// AnnotationPGBackrestBackup is the annotation that is added to a PerconaPGCluster to initiate a manual
	// backup.  The value of the annotation will be a unique identifier for a backup Job (e.g. a
	// timestamp), which will be stored in the PostgresCluster status to properly track completion
	// of the Job.  Also used to annotate the backup Job itself as needed to identify the backup
	// ID associated with a specific manual backup Job.
	AnnotationPGBackrestBackup = PrefixPerconaPGV2 + "pgbackrest-backup"

	// AnnotationPGBackrestBackupJobName is the annotation that is added to a PerconaPGClusterBackup.
	// The value of the annotation will be a name of an existing backup job
	AnnotationPGBackrestBackupJobName = AnnotationPGBackrestBackup + "-job-name"

	// AnnotationPGBackrestBackupJobType is the annotation that is added to a PerconaPGClusterBackup.
	// The value of the annotation will be a type of a backup (e.g. "manual" or "replica-create).
	AnnotationPGBackrestBackupJobType = AnnotationPGBackrestBackup + "-job-type"

	// AnnotationPGBackRestRestore is the annotation that is added to a PerconaPGCluster to initiate an in-place
	// restore.  The value of the annotation will be a unique identfier for a restore Job (e.g. a
	// timestamp), which will be stored in the PostgresCluster status to properly track completion
	// of the Job.
	AnnotationPGBackRestRestore = PrefixPerconaPGV2 + "pgbackrest-restore"

	// AnnotationPMMSecretHash is the annotation that is added to instance annotations to
	// rollout restart PG pods in case PMM credentials are rotated.
	AnnotationPMMSecretHash = PrefixPerconaPGV2 + "pmm-secret-hash"

	// AnnotationMonitorUserSecretHash is the annotation that is added to instance annotations to
	// rollout restart PG pods in case monitor user password is changed.
	AnnotationMonitorUserSecretHash = PrefixPerconaPGV2 + "monitor-user-secret-hash"

	// AnnotationBackupInProgress is the annotation that is added to PerconaPGCluster to
	// indicate that backup is in progress.
	AnnotationBackupInProgress = PrefixPerconaPGV2 + "backup-in-progress"

	// AnnotationClusterBootstrapRestore is the annotation that is added to PerconaPGRestore to
	// indicate that it is a cluster bootstrap restore.
	AnnotationClusterBootstrapRestore = PrefixPerconaPGV2 + "cluster-bootstrap-restore"

	AnnotationPatroniVersion = PrefixPerconaPGV2 + "patroni-version"
)
