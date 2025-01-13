package naming

import (
	"strings"
)

const (
	AnnotationPrefix        = "pgv2.percona.com/"
	CrunchyAnnotationPrefix = "postgres-operator.crunchydata.com/"
)

const (
	// AnnotationPGBackrestBackup is the annotation that is added to a PerconaPGCluster to initiate a manual
	// backup.  The value of the annotation will be a unique identifier for a backup Job (e.g. a
	// timestamp), which will be stored in the PostgresCluster status to properly track completion
	// of the Job.  Also used to annotate the backup Job itself as needed to identify the backup
	// ID associated with a specific manual backup Job.
	AnnotationPGBackrestBackup = AnnotationPrefix + "pgbackrest-backup"

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
	AnnotationPGBackRestRestore = AnnotationPrefix + "pgbackrest-restore"

	// AnnotationPMMSecretHash is the annotation that is added to instance annotations to
	// rollout restart PG pods in case PMM credentials are rotated.
	AnnotationPMMSecretHash = AnnotationPrefix + "pmm-secret-hash"

	// AnnotationMonitorUserSecretHash is the annotation that is added to instance annotations to
	// rollout restart PG pods in case monitor user password is changed.
	AnnotationMonitorUserSecretHash = AnnotationPrefix + "monitor-user-secret-hash"

	// AnnotationBackupInProgress is the annotation that is added to PerconaPGCluster to
	// indicate that backup is in progress.
	AnnotationBackupInProgress = AnnotationPrefix + "backup-in-progress"

	// AnnotationClusterBootstrapRestore is the annotation that is added to PerconaPGRestore to
	// indicate that it is a cluster bootstrap restore.
	AnnotationClusterBootstrapRestore = AnnotationPrefix + "cluster-bootstrap-restore"

	AnnotationPatroniVersion = AnnotationPrefix + "patroni-version"
)

func ToCrunchyAnnotation(annotation string) string {
	return replacePrefix(annotation, AnnotationPrefix, CrunchyAnnotationPrefix)
}

func ToPerconaAnnotation(annotation string) string {
	return replacePrefix(annotation, CrunchyAnnotationPrefix, AnnotationPrefix)
}

func replacePrefix(s, oldPrefix, newPrefix string) string {
	s, found := strings.CutPrefix(s, oldPrefix)
	if found {
		return newPrefix + s
	}
	return s
}
