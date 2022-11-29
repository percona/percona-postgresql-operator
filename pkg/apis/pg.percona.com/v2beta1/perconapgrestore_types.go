package v2beta1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func init() {
	SchemeBuilder.Register(&PerconaPGRestore{}, &PerconaPGRestoreList{})
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
//
// PerconaPGRestore is the CRD that defines a Percona PostgreSQL Restore
type PerconaPGRestore struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`

	Spec   PerconaPGRestoreSpec   `json:"spec"`
	Status PerconaPGRestoreStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
// PerconaPGRestoreList contains a list of PerconaPGRestore
type PerconaPGRestoreList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []PerconaPGRestore `json:"items"`
}

type PerconaPGRestoreSpec struct {
	// The name of the PerconaPGCluster to perform restore.
	// +kubebuilder:validation:Required
	PGCluster string `json:"pgCluster"`

	// The name of the pgBackRest repo within the source PostgresCluster that contains the backups
	// that should be utilized to perform a pgBackRest restore when initializing the data source
	// for the new PostgresCluster.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Pattern=^repo[1-4]
	RepoName string `json:"repoName"`

	// Command line options to include when running the pgBackRest restore command.
	// https://pgbackrest.org/command.html#command-restore
	// +optional
	Options []string `json:"options,omitempty"`
}

type PGRestoreState string

const (
	RestoreNew       PGRestoreState = ""
	RestoreStarting  PGRestoreState = "Starting"
	RestoreRunning   PGRestoreState = "Running"
	RestoreFailed    PGRestoreState = "Failed"
	RestoreSucceeded PGRestoreState = "Succeeded"
)

type PerconaPGRestoreStatus struct {
	JobName     string         `json:"jobName,omitempty"`
	State       PGRestoreState `json:"state,omitempty"`
	CompletedAt *metav1.Time   `json:"completed,omitempty"`
}
