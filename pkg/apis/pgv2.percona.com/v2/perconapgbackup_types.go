package v2

import (
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/percona/percona-postgresql-operator/percona/pgbackrest"
	crunchyv1beta1 "github.com/percona/percona-postgresql-operator/pkg/apis/postgres-operator.crunchydata.com/v1beta1"
)

func init() {
	SchemeBuilder.Register(&PerconaPGBackup{}, &PerconaPGBackupList{})
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=pg-backup
// +kubebuilder:printcolumn:name="Cluster",type=string,JSONPath=".spec.pgCluster",description="Cluster name"
// +kubebuilder:printcolumn:name="Repo",type=string,JSONPath=".spec.repoName",description="Repo name"
// +kubebuilder:printcolumn:name="Destination",type=string,JSONPath=".status.destination",description="Backup destination"
// +kubebuilder:printcolumn:name="Status",type=string,JSONPath=".status.state",description="Job status"
// +kubebuilder:printcolumn:name="Type",type="string",JSONPath=".status.backupType",description="Backup type"
// +kubebuilder:printcolumn:name="Completed",type=date,JSONPath=".status.completed",description="Completed time"
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=".metadata.creationTimestamp",description="Created time"
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +operator-sdk:csv:customresourcedefinitions:order=2
// +operator-sdk:csv:customresourcedefinitions:resources={{CronJob,v1beta1},{Job,v1}}
//
// PerconaPGBackup is the CRD that defines a Percona PostgreSQL Backup
type PerconaPGBackup struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`

	Spec   PerconaPGBackupSpec   `json:"spec"`
	Status PerconaPGBackupStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
// PerconaPGBackupList contains a list of PerconaPGBackup
type PerconaPGBackupList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []PerconaPGBackup `json:"items"`
}

type PerconaPGBackupSpec struct {
	PGCluster string `json:"pgCluster"`

	// The name of the pgBackRest repo to run the backup command against.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Pattern=^repo[1-4]
	RepoName string `json:"repoName"`

	// Command line options to include when running the pgBackRest backup command.
	// https://pgbackrest.org/command.html#command-backup
	// +optional
	Options []string `json:"options,omitempty"`
}

type PGBackupState string

const (
	BackupNew       PGBackupState = ""
	BackupStarting  PGBackupState = "Starting"
	BackupRunning   PGBackupState = "Running"
	BackupFailed    PGBackupState = "Failed"
	BackupSucceeded PGBackupState = "Succeeded"
)

type PerconaPGBackupStatus struct {
	JobName     string                         `json:"jobName,omitempty"`
	State       PGBackupState                  `json:"state,omitempty"`
	CompletedAt *metav1.Time                   `json:"completed,omitempty"`
	Destination string                         `json:"destination,omitempty"`
	BackupType  PGBackupType                   `json:"backupType,omitempty"`
	StorageType PGBackupStorageType            `json:"storageType,omitempty"`
	Repo        *crunchyv1beta1.PGBackRestRepo `json:"repo,omitempty"`
	Image       string                         `json:"image,omitempty"`
	BackupName  string                         `json:"backupName,omitempty"`
}

type PGBackupStorageType string

const (
	PGBackupStorageTypeFilesystem PGBackupStorageType = "filesystem"
	PGBackupStorageTypeAzure      PGBackupStorageType = "azure"
	PGBackupStorageTypeGCS        PGBackupStorageType = "gcs"
	PGBackupStorageTypeS3         PGBackupStorageType = "s3"
)

type PGBackupType string

const (
	PGBackupTypeFull         PGBackupType = "full"
	PGBackupTypeDifferential PGBackupType = "differential"
	PGBackupTypeIncremental  PGBackupType = "incremental"
)

func (b *PerconaPGBackup) Default() {
	b.Spec.Options = append(b.Spec.Options, fmt.Sprintf(`--annotation="%s"="%s"`, pgbackrest.AnnotationBackupName, b.Name))
}
