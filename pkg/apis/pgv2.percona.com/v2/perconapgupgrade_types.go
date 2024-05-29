package v2

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	crunchyv1beta1 "github.com/percona/percona-postgresql-operator/pkg/apis/postgres-operator.crunchydata.com/v1beta1"
)

func init() {
	SchemeBuilder.Register(&PerconaPGUpgrade{}, &PerconaPGUpgradeList{})
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// PerconaPGUpgrade is the Schema for the perconapgupgrades API
type PerconaPGUpgrade struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`

	Spec   PerconaPGUpgradeSpec   `json:"spec"`
	Status PerconaPGUpgradeStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
// PerconaPGRestoreList contains a list of PerconaPGRestore
type PerconaPGUpgradeList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []PerconaPGUpgrade `json:"items"`
}

type PerconaPGUpgradeSpec struct {
	// +optional
	Metadata *crunchyv1beta1.Metadata `json:"metadata,omitempty"`

	// The name of the cluster to be updated
	// +required
	// +kubebuilder:validation:MinLength=1
	PostgresClusterName string `json:"postgresClusterName"`

	// The image name to use for major PostgreSQL upgrades.
	// +required
	Image *string `json:"image"`

	// ImagePullPolicy is used to determine when Kubernetes will attempt to
	// pull (download) container images.
	// More info: https://kubernetes.io/docs/concepts/containers/images/#image-pull-policy
	// +kubebuilder:validation:Enum={Always,Never,IfNotPresent}
	// +optional
	ImagePullPolicy corev1.PullPolicy `json:"imagePullPolicy,omitempty"`

	// TODO(benjaminjb) Check the behavior: does updating ImagePullSecrets cause
	// all running PGUpgrade pods to restart?

	// The image pull secrets used to pull from a private registry.
	// Changing this value causes all running PGUpgrade pods to restart.
	// https://k8s.io/docs/tasks/configure-pod-container/pull-image-private-registry/
	// +optional
	ImagePullSecrets []corev1.LocalObjectReference `json:"imagePullSecrets,omitempty"`

	// TODO(benjaminjb): define webhook validation to make sure
	// `fromPostgresVersion` is below `toPostgresVersion`
	// or leverage other validation rules, such as the Common Expression Language
	// rules currently in alpha as of Kubernetes 1.23
	// - https://kubernetes.io/docs/tasks/extend-kubernetes/custom-resources/custom-resource-definitions/#validation-rules

	// The major version of PostgreSQL before the upgrade.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Minimum=12
	// +kubebuilder:validation:Maximum=16
	FromPostgresVersion int `json:"fromPostgresVersion"`

	// TODO(benjaminjb): define webhook validation to make sure
	// `fromPostgresVersion` is below `toPostgresVersion`
	// or leverage other validation rules, such as the Common Expression Language
	// rules currently in alpha as of Kubernetes 1.23

	// The major version of PostgreSQL to be upgraded to.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Minimum=13
	// +kubebuilder:validation:Maximum=16
	ToPostgresVersion int `json:"toPostgresVersion"`

	// The image name to use for PostgreSQL containers after upgrade.
	// When omitted, the value comes from an operator environment variable.
	// +optional
	ToPostgresImage string `json:"toPostgresImage,omitempty"`

	// Resource requirements for the PGUpgrade container.
	// +optional
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`

	// Scheduling constraints of the PGUpgrade pod.
	// More info: https://kubernetes.io/docs/concepts/scheduling-eviction/assign-pod-node
	// +optional
	Affinity *corev1.Affinity `json:"affinity,omitempty"`

	// TODO(benjaminjb) Check the behavior: does updating PriorityClassName cause
	// PGUpgrade to restart?

	// Priority class name for the PGUpgrade pod. Changing this
	// value causes PGUpgrade pod to restart.
	// More info: https://kubernetes.io/docs/concepts/scheduling-eviction/pod-priority-preemption/
	// +optional
	PriorityClassName *string `json:"priorityClassName,omitempty"`

	// Tolerations of the PGUpgrade pod.
	// More info: https://kubernetes.io/docs/concepts/scheduling-eviction/taint-and-toleration
	// +optional
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`

	// Init container to run before the upgrade container.
	// +optional
	InitContainers []corev1.Container `json:"initContainers,omitempty"`

	// The list of volume mounts to mount to upgrade pod.
	// +optional
	VolumeMounts []corev1.VolumeMount `json:"volumeMounts,omitempty"`
}

type PerconaPGUpgradeStatus struct {
	crunchyv1beta1.PGUpgradeStatus `json:",inline"`
}
