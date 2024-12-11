// Copyright 2021 - 2024 Crunchy Data Solutions, Inc.
//
// SPDX-License-Identifier: Apache-2.0

package v1beta1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// PGBackRestJobStatus contains information about the state of a pgBackRest Job.
type PGBackRestJobStatus struct {

	// A unique identifier for the manual backup as provided using the "pgbackrest-backup"
	// annotation when initiating a backup.
	// +kubebuilder:validation:Required
	ID string `json:"id"`

	// Specifies whether or not the Job is finished executing (does not indicate success or
	// failure).
	// +kubebuilder:validation:Required
	Finished bool `json:"finished"`

	// Represents the time the manual backup Job was acknowledged by the Job controller.
	// It is represented in RFC3339 form and is in UTC.
	// +optional
	StartTime *metav1.Time `json:"startTime,omitempty"`

	// Represents the time the manual backup Job was determined by the Job controller
	// to be completed.  This field is only set if the backup completed successfully.
	// Additionally, it is represented in RFC3339 form and is in UTC.
	// +optional
	CompletionTime *metav1.Time `json:"completionTime,omitempty"`

	// The number of actively running manual backup Pods.
	// +optional
	Active int32 `json:"active,omitempty"`

	// The number of Pods for the manual backup Job that reached the "Succeeded" phase.
	// +optional
	Succeeded int32 `json:"succeeded,omitempty"`

	// The number of Pods for the manual backup Job that reached the "Failed" phase.
	// +optional
	Failed int32 `json:"failed,omitempty"`
}

type PGBackRestScheduledBackupStatus struct {

	// The name of the associated pgBackRest scheduled backup CronJob
	// +kubebuilder:validation:Required
	CronJobName string `json:"cronJobName,omitempty"`

	// The name of the associated pgBackRest repository
	// +kubebuilder:validation:Required
	RepoName string `json:"repo,omitempty"`

	// The pgBackRest backup type for this Job
	// +kubebuilder:validation:Required
	Type string `json:"type,omitempty"`

	// Represents the time the manual backup Job was acknowledged by the Job controller.
	// It is represented in RFC3339 form and is in UTC.
	// +optional
	StartTime *metav1.Time `json:"startTime,omitempty"`

	// Represents the time the manual backup Job was determined by the Job controller
	// to be completed.  This field is only set if the backup completed successfully.
	// Additionally, it is represented in RFC3339 form and is in UTC.
	// +optional
	CompletionTime *metav1.Time `json:"completionTime,omitempty"`

	// The number of actively running manual backup Pods.
	// +optional
	Active int32 `json:"active,omitempty"`

	// The number of Pods for the manual backup Job that reached the "Succeeded" phase.
	// +optional
	Succeeded int32 `json:"succeeded,omitempty"`

	// The number of Pods for the manual backup Job that reached the "Failed" phase.
	// +optional
	Failed int32 `json:"failed,omitempty"`
}

// PGBackRestArchive defines a pgBackRest archive configuration
type PGBackRestArchive struct {
	// +optional
	Metadata *Metadata `json:"metadata,omitempty"`

	// Projected volumes containing custom pgBackRest configuration.  These files are mounted
	// under "/etc/pgbackrest/conf.d" alongside any pgBackRest configuration generated by the
	// PostgreSQL Operator:
	// https://pgbackrest.org/configuration.html
	// +optional
	Configuration []corev1.VolumeProjection `json:"configuration,omitempty"`

	// Global pgBackRest configuration settings.  These settings are included in the "global"
	// section of the pgBackRest configuration generated by the PostgreSQL Operator, and then
	// mounted under "/etc/pgbackrest/conf.d":
	// https://pgbackrest.org/configuration.html
	// +optional
	Global map[string]string `json:"global,omitempty"`

	// The image name to use for pgBackRest containers.  Utilized to run
	// pgBackRest repository hosts and backups. The image may also be set using
	// the RELATED_IMAGE_PGBACKREST environment variable
	// +optional
	Image string `json:"image,omitempty"`

	// Jobs field allows configuration for all backup jobs
	// +optional
	Jobs *BackupJobs `json:"jobs,omitempty"`

	// Defines a pgBackRest repository
	// +kubebuilder:validation:MinItems=1
	// +listType=map
	// +listMapKey=name
	Repos []PGBackRestRepo `json:"repos"`

	// Defines configuration for a pgBackRest dedicated repository host.  This section is only
	// applicable if at least one "volume" (i.e. PVC-based) repository is defined in the "repos"
	// section, therefore enabling a dedicated repository host Deployment.
	// +optional
	RepoHost *PGBackRestRepoHost `json:"repoHost,omitempty"`

	// Defines details for manual pgBackRest backup Jobs
	// +optional
	Manual *PGBackRestManualBackup `json:"manual,omitempty"`

	// Defines details for performing an in-place restore using pgBackRest
	// +optional
	Restore *PGBackRestRestore `json:"restore,omitempty"`

	// Configuration for pgBackRest sidecar containers
	// +optional
	Sidecars *PGBackRestSidecars `json:"sidecars,omitempty"`
}

// PGBackRestSidecars defines the configuration for pgBackRest sidecar containers
type PGBackRestSidecars struct {
	// Defines the configuration for the pgBackRest sidecar container
	// +optional
	PGBackRest *Sidecar `json:"pgbackrest,omitempty"`

	// Defines the configuration for the pgBackRest config sidecar container
	// +optional
	PGBackRestConfig *Sidecar `json:"pgbackrestConfig,omitempty"`
}

type BackupJobs struct {
	// Resource limits for backup jobs. Includes manual, scheduled and replica
	// create backups
	// +optional
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`

	// Priority class name for the pgBackRest backup Job pods.
	// More info: https://kubernetes.io/docs/concepts/scheduling-eviction/pod-priority-preemption/
	// +optional
	PriorityClassName *string `json:"priorityClassName,omitempty"`

	// Scheduling constraints of pgBackRest backup Job pods.
	// More info: https://kubernetes.io/docs/concepts/scheduling-eviction/assign-pod-node
	// +optional
	Affinity *corev1.Affinity `json:"affinity,omitempty"`

	// Tolerations of pgBackRest backup Job pods.
	// More info: https://kubernetes.io/docs/concepts/scheduling-eviction/taint-and-toleration
	// +optional
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`

	// Limit the lifetime of a Job that has finished.
	// More info: https://kubernetes.io/docs/concepts/workloads/controllers/job
	// +optional
	// +kubebuilder:validation:Minimum=60
	TTLSecondsAfterFinished *int32 `json:"ttlSecondsAfterFinished,omitempty"`

	// SecurityContext defines the security settings for PGBackRest pod.
	// +optional
	SecurityContext *corev1.PodSecurityContext `json:"securityContext,omitempty"`
}

// PGBackRestManualBackup contains information that is used for creating a
// pgBackRest backup that is invoked manually (i.e. it's unscheduled).
type PGBackRestManualBackup struct {
	// The name of the pgBackRest repo to run the backup command against.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Pattern=^repo[1-4]
	RepoName string `json:"repoName"`

	// Command line options to include when running the pgBackRest backup command.
	// https://pgbackrest.org/command.html#command-backup
	// +optional
	Options []string `json:"options,omitempty"`
}

// PGBackRestRepoHost represents a pgBackRest dedicated repository host
type PGBackRestRepoHost struct {

	// Scheduling constraints of the Dedicated repo host pod.
	// Changing this value causes repo host to restart.
	// More info: https://kubernetes.io/docs/concepts/scheduling-eviction/assign-pod-node
	// +optional
	Affinity *corev1.Affinity `json:"affinity,omitempty"`

	// Priority class name for the pgBackRest repo host pod. Changing this value
	// causes PostgreSQL to restart.
	// More info: https://kubernetes.io/docs/concepts/scheduling-eviction/pod-priority-preemption/
	// +optional
	PriorityClassName *string `json:"priorityClassName,omitempty"`

	// Resource requirements for a pgBackRest repository host
	// +optional
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`

	// Tolerations of a PgBackRest repo host pod. Changing this value causes a restart.
	// More info: https://kubernetes.io/docs/concepts/scheduling-eviction/taint-and-toleration
	// +optional
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`

	// Topology spread constraints of a Dedicated repo host pod. Changing this
	// value causes the repo host to restart.
	// More info: https://kubernetes.io/docs/concepts/workloads/pods/pod-topology-spread-constraints/
	// +optional
	TopologySpreadConstraints []corev1.TopologySpreadConstraint `json:"topologySpreadConstraints,omitempty"`

	// ConfigMap containing custom SSH configuration.
	// Deprecated: Repository hosts use mTLS for encryption, authentication, and authorization.
	// +optional
	SSHConfiguration *corev1.ConfigMapProjection `json:"sshConfigMap,omitempty"`

	// Secret containing custom SSH keys.
	// Deprecated: Repository hosts use mTLS for encryption, authentication, and authorization.
	// +optional
	SSHSecret *corev1.SecretProjection `json:"sshSecret,omitempty"`

	// SecurityContext defines the security settings for PGBackRest pod.
	// +optional
	SecurityContext *corev1.PodSecurityContext `json:"securityContext,omitempty"`
}

// PGBackRestRestore defines an in-place restore for the PostgresCluster.
type PGBackRestRestore struct {

	// Whether or not in-place pgBackRest restores are enabled for this PostgresCluster.
	// +kubebuilder:default=false
	Enabled *bool `json:"enabled"`

	*PostgresClusterDataSource `json:",inline"`
}

// PGBackRestBackupSchedules defines a pgBackRest scheduled backup
type PGBackRestBackupSchedules struct {
	// Validation set to minimum length of six to account for @daily option

	// Defines the Cron schedule for a full pgBackRest backup.
	// Follows the standard Cron schedule syntax:
	// https://k8s.io/docs/concepts/workloads/controllers/cron-jobs/#cron-schedule-syntax
	// +optional
	// +kubebuilder:validation:MinLength=6
	Full *string `json:"full,omitempty"`

	// Defines the Cron schedule for a differential pgBackRest backup.
	// Follows the standard Cron schedule syntax:
	// https://k8s.io/docs/concepts/workloads/controllers/cron-jobs/#cron-schedule-syntax
	// +optional
	// +kubebuilder:validation:MinLength=6
	Differential *string `json:"differential,omitempty"`

	// Defines the Cron schedule for an incremental pgBackRest backup.
	// Follows the standard Cron schedule syntax:
	// https://k8s.io/docs/concepts/workloads/controllers/cron-jobs/#cron-schedule-syntax
	// +optional
	// +kubebuilder:validation:MinLength=6
	Incremental *string `json:"incremental,omitempty"`
}

// PGBackRestStatus defines the status of pgBackRest within a PostgresCluster
type PGBackRestStatus struct {

	// Status information for manual backups
	// +optional
	ManualBackup *PGBackRestJobStatus `json:"manualBackup,omitempty"`

	// Status information for scheduled backups
	// +optional
	ScheduledBackups []PGBackRestScheduledBackupStatus `json:"scheduledBackups,omitempty"`

	// Status information for the pgBackRest dedicated repository host
	// +optional
	RepoHost *RepoHostStatus `json:"repoHost,omitempty"`

	// Status information for pgBackRest repositories
	// +optional
	// +listType=map
	// +listMapKey=name
	Repos []RepoStatus `json:"repos,omitempty"`

	// Status information for in-place restores
	// +optional
	Restore *PGBackRestJobStatus `json:"restore,omitempty"`
}

// PGBackRestRepo represents a pgBackRest repository.  Only one of its members may be specified.
type PGBackRestRepo struct {
	// Please note that as a Union type that follows OpenAPI 3.0 'oneOf' semantics, the following KEP
	// will be applicable once implemented:
	// https://github.com/kubernetes/enhancements/tree/master/keps/sig-api-machinery/1027-api-unions

	// The name of the repository
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Pattern=^repo[1-4]
	// +kubebuilder:default="repo1"
	Name string `json:"name"`

	// Defines the schedules for the pgBackRest backups
	// Full, Differential and Incremental backup types are supported:
	// https://pgbackrest.org/user-guide.html#concept/backup
	// +optional
	BackupSchedules *PGBackRestBackupSchedules `json:"schedules,omitempty"`

	// Represents a pgBackRest repository that is created using Azure storage
	// +optional
	Azure *RepoAzure `json:"azure,omitempty"`

	// Represents a pgBackRest repository that is created using Google Cloud Storage
	// +optional
	GCS *RepoGCS `json:"gcs,omitempty"`

	// RepoS3 represents a pgBackRest repository that is created using AWS S3 (or S3-compatible)
	// storage
	// +optional
	S3 *RepoS3 `json:"s3,omitempty"`

	// Represents a pgBackRest repository that is created using a PersistentVolumeClaim
	// +optional
	Volume *RepoPVC `json:"volume,omitempty"`
}

// RepoHostStatus defines the status of a pgBackRest repository host
type RepoHostStatus struct {
	metav1.TypeMeta `json:",inline"`

	// Whether or not the pgBackRest repository host is ready for use
	// +optional
	Ready bool `json:"ready"`
}

// RepoPVC represents a pgBackRest repository that is created using a PersistentVolumeClaim
type RepoPVC struct {

	// Defines a PersistentVolumeClaim spec used to create and/or bind a volume
	// +kubebuilder:validation:Required
	VolumeClaimSpec corev1.PersistentVolumeClaimSpec `json:"volumeClaimSpec"`
}

// RepoAzure represents a pgBackRest repository that is created using Azure storage
type RepoAzure struct {

	// The Azure container utilized for the repository
	// +kubebuilder:validation:Required
	Container string `json:"container"`
}

// RepoGCS represents a pgBackRest repository that is created using Google Cloud Storage
type RepoGCS struct {

	// The GCS bucket utilized for the repository
	// +kubebuilder:validation:Required
	Bucket string `json:"bucket"`
}

// RepoS3 represents a pgBackRest repository that is created using AWS S3 (or S3-compatible)
// storage
type RepoS3 struct {

	// The S3 bucket utilized for the repository
	// +kubebuilder:validation:Required
	Bucket string `json:"bucket"`

	// A valid endpoint corresponding to the specified region
	// +kubebuilder:validation:Required
	Endpoint string `json:"endpoint"`

	// The region corresponding to the S3 bucket
	// +kubebuilder:validation:Required
	Region string `json:"region"`
}

// RepoStatus the status of a pgBackRest repository
type RepoStatus struct {

	// The name of the pgBackRest repository
	// +kubebuilder:validation:Required
	Name string `json:"name"`

	// Whether or not the pgBackRest repository PersistentVolumeClaim is bound to a volume
	// +optional
	Bound bool `json:"bound,omitempty"`

	// The name of the volume the containing the pgBackRest repository
	// +optional
	VolumeName string `json:"volume,omitempty"`

	// Specifies whether or not a stanza has been successfully created for the repository
	// +optional
	StanzaCreated bool `json:"stanzaCreated"`

	// ReplicaCreateBackupReady indicates whether a backup exists in the repository as needed
	// to bootstrap replicas.
	ReplicaCreateBackupComplete bool `json:"replicaCreateBackupComplete,omitempty"`

	// A hash of the required fields in the spec for defining an Azure, GCS or S3 repository,
	// Utilized to detect changes to these fields and then execute pgBackRest stanza-create
	// commands accordingly.
	// +optional
	RepoOptionsHash string `json:"repoOptionsHash,omitempty"`
}

// PGBackRestDataSource defines a pgBackRest configuration specifically for restoring from cloud-based data source
type PGBackRestDataSource struct {
	// Projected volumes containing custom pgBackRest configuration.  These files are mounted
	// under "/etc/pgbackrest/conf.d" alongside any pgBackRest configuration generated by the
	// PostgreSQL Operator:
	// https://pgbackrest.org/configuration.html
	// +optional
	Configuration []corev1.VolumeProjection `json:"configuration,omitempty"`

	// Global pgBackRest configuration settings.  These settings are included in the "global"
	// section of the pgBackRest configuration generated by the PostgreSQL Operator, and then
	// mounted under "/etc/pgbackrest/conf.d":
	// https://pgbackrest.org/configuration.html
	// +optional
	Global map[string]string `json:"global,omitempty"`

	// Defines a pgBackRest repository
	// +kubebuilder:validation:Required
	Repo PGBackRestRepo `json:"repo"`

	// The name of an existing pgBackRest stanza to use as the data source for the new PostgresCluster.
	// Defaults to `db` if not provided.
	// +kubebuilder:default="db"
	Stanza string `json:"stanza"`

	// Command line options to include when running the pgBackRest restore command.
	// https://pgbackrest.org/command.html#command-restore
	// +optional
	Options []string `json:"options,omitempty"`

	// Resource requirements for the pgBackRest restore Job.
	// +optional
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`

	// Scheduling constraints of the pgBackRest restore Job.
	// More info: https://kubernetes.io/docs/concepts/scheduling-eviction/assign-pod-node
	// +optional
	Affinity *corev1.Affinity `json:"affinity,omitempty"`

	// Priority class name for the pgBackRest restore Job pod. Changing this
	// value causes PostgreSQL to restart.
	// More info: https://kubernetes.io/docs/concepts/scheduling-eviction/pod-priority-preemption/
	// +optional
	PriorityClassName *string `json:"priorityClassName,omitempty"`

	// Tolerations of the pgBackRest restore Job.
	// More info: https://kubernetes.io/docs/concepts/scheduling-eviction/taint-and-toleration
	// +optional
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`
}
