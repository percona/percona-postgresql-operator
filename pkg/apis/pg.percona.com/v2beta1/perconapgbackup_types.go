package v2beta1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func init() {
	SchemeBuilder.Register(&PerconaPGBackup{}, &PerconaPGBackupList{})
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
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
	JobName     string        `json:"jobName,omitempty"`
	State       PGBackupState `json:"state,omitempty"`
	CompletedAt *metav1.Time  `json:"completed,omitempty"`
}
