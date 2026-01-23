package v2

import (
	"context"

	"github.com/pkg/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func init() {
	SchemeBuilder.Register(&PerconaPGRestore{}, &PerconaPGRestoreList{})
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=pg-restore
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +operator-sdk:csv:customresourcedefinitions:order=3
// +operator-sdk:csv:customresourcedefinitions:resources={{CronJob,v1beta1},{Job,v1}}
// +kubebuilder:printcolumn:name="Cluster",type="string",JSONPath=".spec.pgCluster",description="Cluster name"
// +kubebuilder:printcolumn:name="Status",type="string",JSONPath=".status.state",description="Job status"
// +kubebuilder:printcolumn:name="Completed",type="date",JSONPath=".status.completed",description="Completed time"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
//
// PerconaPGRestore is the CRD that defines a Percona PostgreSQL Restore
type PerconaPGRestore struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`

	// +kubebuilder:validation:XValidation:rule="has(self.repoName) || self.volumeSnapshotName != \"\"",message="either repoName or volumeSnapshotName must be set"
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
	// +kubebuilder:validation:Pattern=^repo[1-4]
	RepoName *string `json:"repoName,omitempty"`

	// The name of the VolumeSnapshot to perform restore from.
	// +optional
	VolumeSnapshotName string `json:"volumeSnapshotName,omitempty"`

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

func (r *PerconaPGRestore) IsCompleted() bool {
	return r.Status.State == RestoreSucceeded || r.Status.State == RestoreFailed
}

func (pgRestore *PerconaPGRestore) UpdateStatus(ctx context.Context, cl client.Client, updateFunc func(restore *PerconaPGRestore)) error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		restore := new(PerconaPGRestore)
		if err := cl.Get(ctx, client.ObjectKeyFromObject(pgRestore), restore); err != nil {
			return errors.Wrap(err, "get PGRestore")
		}

		updateFunc(restore)

		return cl.Status().Update(ctx, restore)
	})
}
