package v2

import (
	"encoding/json"
	"fmt"
	"time"

	v "github.com/hashicorp/go-version"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

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
	JobName              string                         `json:"jobName,omitempty"`
	State                PGBackupState                  `json:"state,omitempty"`
	Error                string                         `json:"error,omitempty"`
	CompletedAt          *metav1.Time                   `json:"completed,omitempty"`
	Destination          string                         `json:"destination,omitempty"`
	BackupType           PGBackupType                   `json:"backupType,omitempty"`
	StorageType          PGBackupStorageType            `json:"storageType,omitempty"`
	Repo                 *crunchyv1beta1.PGBackRestRepo `json:"repo,omitempty"`
	Image                string                         `json:"image,omitempty"`
	BackupName           string                         `json:"backupName,omitempty"`
	CRVersion            string                         `json:"crVersion,omitempty"`
	LatestRestorableTime PITRestoreDateTime             `json:"latestRestorableTime,omitempty"`
}

// +kubebuilder:validation:Type=string
type PITRestoreDateTime struct {
	*metav1.Time `json:",inline"`
}

func (PITRestoreDateTime) OpenAPISchemaType() []string { return []string{"string"} }

func (PITRestoreDateTime) OpenAPISchemaFormat() string { return "" }

func (t *PITRestoreDateTime) UnmarshalJSON(b []byte) (err error) {
	if len(b) == 4 && string(b) == "null" {
		mt := metav1.NewTime(time.Time{})
		t.Time = &mt
		return nil
	}

	var str string

	if err = json.Unmarshal(b, &str); err != nil {
		return err
	}

	pt, err := time.Parse("2006-01-02 15:04:05.000000-0700", str)
	if err != nil {
		return
	}

	mt := metav1.NewTime(pt)
	t.Time = &mt

	return nil
}

func (t *PITRestoreDateTime) MarshalJSON() ([]byte, error) {
	if t.Time == nil {
		return []byte("null"), nil
	}

	return json.Marshal(t.Time.Format("2006-01-02 15:04:05.000000-0700"))
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

const (
	PGBackrestAnnotationBackupName = "percona.com/backup-name"
	PGBackrestAnnotationJobName    = "percona.com/backup-job-name"
	PGBackrestAnnotationJobType    = "percona.com/backup-job-type"
)

func (b *PerconaPGBackup) Default() {
	b.Spec.Options = append(b.Spec.Options, fmt.Sprintf(`--annotation="%s"="%s"`, PGBackrestAnnotationBackupName, b.Name))
}

func (b *PerconaPGBackup) CompareVersion(ver string) int {
	if b.Status.CRVersion == "" {
		return -1
	}
	backupVersion := v.Must(v.NewVersion(b.Status.CRVersion))
	return backupVersion.Compare(v.Must(v.NewVersion(ver)))
}
