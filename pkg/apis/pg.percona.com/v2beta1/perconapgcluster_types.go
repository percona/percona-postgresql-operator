package v2beta1

import (
	"os"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	crunchyv1beta1 "github.com/percona/percona-postgresql-operator/pkg/apis/postgres-operator.crunchydata.com/v1beta1"
)

func init() {
	SchemeBuilder.Register(&PerconaPGCluster{}, &PerconaPGClusterList{})
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="State",type=string,JSONPath=".status.state"

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
//
// PerconaPGCluster is the CRD that defines a Percona PG Cluster
type PerconaPGCluster struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`

	Spec   PerconaPGClusterSpec   `json:"spec"`
	Status PerconaPGClusterStatus `json:"status,omitempty"`
}

type PerconaPGClusterSpec struct {
	// The image name to use for PostgreSQL containers. When omitted, the value
	// comes from an operator environment variable. For standard PostgreSQL images,
	// the format is RELATED_IMAGE_POSTGRES_{postgresVersion},
	// e.g. RELATED_IMAGE_POSTGRES_13. For PostGIS enabled PostgreSQL images,
	// the format is RELATED_IMAGE_POSTGRES_{postgresVersion}_GIS_{postGISVersion},
	// e.g. RELATED_IMAGE_POSTGRES_13_GIS_3.1.
	// +optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,order=1
	Image string `json:"image,omitempty"`

	// ImagePullPolicy is used to determine when Kubernetes will attempt to
	// pull (download) container images.
	// More info: https://kubernetes.io/docs/concepts/containers/images/#image-pull-policy
	// +kubebuilder:validation:Enum={Always,Never,IfNotPresent}
	// +optional
	ImagePullPolicy corev1.PullPolicy `json:"imagePullPolicy,omitempty"`

	// The image pull secrets used to pull from a private registry
	// Changing this value causes all running pods to restart.
	// https://k8s.io/docs/tasks/configure-pod-container/pull-image-private-registry/
	// +optional
	ImagePullSecrets []corev1.LocalObjectReference `json:"imagePullSecrets,omitempty"`

	// The port on which PostgreSQL should listen.
	// +optional
	// +kubebuilder:default=5432
	// +kubebuilder:validation:Minimum=1024
	Port *int32 `json:"port,omitempty"`

	// Specification of the service that exposes the PostgreSQL primary instance.
	// +optional
	Expose *ServiceExpose `json:"expose,omitempty"`

	// The major version of PostgreSQL installed in the PostgreSQL image
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Minimum=10
	// +kubebuilder:validation:Maximum=14
	// +operator-sdk:csv:customresourcedefinitions:type=spec,order=1
	PostgresVersion int `json:"postgresVersion"`

	Secrets SecretsSpec `json:"secrets,omitempty"`

	// Run this cluster as a read-only copy of an existing cluster or archive.
	// +optional
	Standby *crunchyv1beta1.PostgresStandbySpec `json:"standby,omitempty"`

	// Whether or not the PostgreSQL cluster is being deployed to an OpenShift
	// environment. If the field is unset, the operator will automatically
	// detect the environment.
	// +optional
	OpenShift *bool `json:"openshift,omitempty"`

	// +optional
	Patroni *crunchyv1beta1.PatroniSpec `json:"patroni,omitempty"`

	// Users to create inside PostgreSQL and the databases they should access.
	// The default creates one user that can access one database matching the
	// PostgresCluster name. An empty list creates no users. Removing a user
	// from this list does NOT drop the user nor revoke their access.
	// +listType=map
	// +listMapKey=name
	// +optional
	Users []crunchyv1beta1.PostgresUserSpec `json:"users,omitempty"`

	// DatabaseInitSQL defines a ConfigMap containing custom SQL that will
	// be run after the cluster is initialized. This ConfigMap must be in the same
	// namespace as the cluster.
	// +optional
	DatabaseInitSQL *crunchyv1beta1.DatabaseInitSQL `json:"databaseInitSQL,omitempty"`

	// Whether or not the PostgreSQL cluster should be stopped.
	// When this is true, workloads are scaled to zero and CronJobs
	// are suspended.
	// Other resources, such as Services and Volumes, remain in place.
	// +optional
	Shutdown *bool `json:"shutdown,omitempty"`

	// Suspends the rollout and reconciliation of changes made to the
	// PostgresCluster spec.
	// +optional
	Paused *bool `json:"paused,omitempty"`

	// Specifies a data source for bootstrapping the PostgreSQL cluster.
	// +optional
	DataSource *crunchyv1beta1.DataSource `json:"dataSource,omitempty"`

	// Specifies one or more sets of PostgreSQL pods that replicate data for
	// this cluster.
	InstanceSets PGInstanceSets `json:"instances"`

	// The specification of a proxy that connects to PostgreSQL.
	// +optional
	Proxy *PGProxySpec `json:"proxy,omitempty"`

	// PostgreSQL backup configuration
	// +kubebuilder:validation:Required
	Backups crunchyv1beta1.Backups `json:"backups"`

	// The specification of PMM sidecars.
	// +optional
	PMM *PMMSpec `json:"pmm,omitempty"`
}

type AppState string

const (
	AppStateInit     AppState = "initializing"
	AppStateShutdown AppState = "shutdown"
	AppStateReady    AppState = "ready"
	AppStateError    AppState = "error"
)

type PerconaPGClusterStatus struct {
	State                                AppState `json:"state"`
	crunchyv1beta1.PostgresClusterStatus `json:",inline"`
}

type PMMSpec struct {
	// +kubebuilder:validation:Required
	Enabled bool `json:"enabled"`

	// +kubebuilder:validation:Required
	Image string `json:"image"`

	// ImagePullPolicy is used to determine when Kubernetes will attempt to
	// pull (download) container images.
	// More info: https://kubernetes.io/docs/concepts/containers/images/#image-pull-policy
	// +kubebuilder:validation:Enum={Always,Never,IfNotPresent}
	// +optional
	ImagePullPolicy corev1.PullPolicy `json:"imagePullPolicy,omitempty"`

	// +kubebuilder:validation:Required
	ServerHost string `json:"serverHost,omitempty"`

	// +kubebuilder:validation:Required
	Secret string `json:"secret,omitempty"`

	// Compute resources of a PMM container.
	// +optional
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`

	// +optional
	ContainerSecurityContext *corev1.SecurityContext `json:"containerSecurityContext,omitempty"`

	// +optional
	RuntimeClassName *string `json:"runtimeClassName,omitempty"`
}

type SecretsSpec struct {
	// The secret containing the Certificates and Keys to encrypt PostgreSQL
	// traffic will need to contain the server TLS certificate, TLS key and the
	// Certificate Authority certificate with the data keys set to tls.crt,
	// tls.key and ca.crt, respectively. It will then be mounted as a volume
	// projection to the '/pgconf/tls' directory. For more information on
	// Kubernetes secret projections, please see
	// https://k8s.io/docs/concepts/configuration/secret/#projection-of-secret-keys-to-specific-paths
	// NOTE: If CustomTLSSecret is provided, CustomReplicationClientTLSSecret
	// MUST be provided and the ca.crt provided must be the same.
	// +optional
	CustomTLSSecret *corev1.SecretProjection `json:"customTLSSecret,omitempty"`

	// The secret containing the replication client certificates and keys for
	// secure connections to the PostgreSQL server. It will need to contain the
	// client TLS certificate, TLS key and the Certificate Authority certificate
	// with the data keys set to tls.crt, tls.key and ca.crt, respectively.
	// NOTE: If CustomReplicationClientTLSSecret is provided, CustomTLSSecret
	// MUST be provided and the ca.crt provided must be the same.
	// +optional
	CustomReplicationClientTLSSecret *corev1.SecretProjection `json:"customReplicationTLSSecret,omitempty"`
}

// +listType=map
// +listMapKey=name
// +kubebuilder:validation:MinItems=1
// +operator-sdk:csv:customresourcedefinitions:type=spec,order=2
type PGInstanceSets []PGInstanceSetSpec

func (p PGInstanceSets) ToCrunchy() []crunchyv1beta1.PostgresInstanceSetSpec {
	set := make([]crunchyv1beta1.PostgresInstanceSetSpec, len(p))

	for i, inst := range p {
		set[i] = inst.ToCrunchy()
	}

	return set
}

type PGInstanceSetSpec struct {
	// +optional
	Metadata *crunchyv1beta1.Metadata `json:"metadata,omitempty"`

	// This value goes into the name of an appsv1.StatefulSet, the hostname of
	// a corev1.Pod, and label values. The pattern below is IsDNS1123Label
	// wrapped in "()?" to accommodate the empty default.
	//
	// The Pods created by a StatefulSet have a "controller-revision-hash" label
	// comprised of the StatefulSet name, a dash, and a 10-character hash.
	// The length below is derived from limitations on label values:
	//
	//   63 (max) ≥ len(cluster) + 1 (dash)
	//                + len(set) + 1 (dash) + 4 (id)
	//                + 1 (dash) + 10 (hash)
	//
	// See: https://issue.k8s.io/64023

	// Name that associates this set of PostgreSQL pods. This field is optional
	// when only one instance set is defined. Each instance set in a cluster
	// must have a unique name. The combined length of this and the cluster name
	// must be 46 characters or less.
	// +optional
	// +kubebuilder:default=""
	// +kubebuilder:validation:Pattern=`^([a-z0-9]([-a-z0-9]*[a-z0-9])?)?$`
	Name string `json:"name"`

	// Scheduling constraints of a PostgreSQL pod. Changing this value causes
	// PostgreSQL to restart.
	// More info: https://kubernetes.io/docs/concepts/scheduling-eviction/assign-pod-node
	// +optional
	Affinity *corev1.Affinity `json:"affinity,omitempty"`

	// Custom sidecars for PostgreSQL instance pods. Changing this value causes
	// PostgreSQL to restart.
	// +optional
	Sidecars []corev1.Container `json:"sidecars,omitempty"`

	// Priority class name for the PostgreSQL pod. Changing this value causes
	// PostgreSQL to restart.
	// More info: https://kubernetes.io/docs/concepts/scheduling-eviction/pod-priority-preemption/
	// +optional
	PriorityClassName *string `json:"priorityClassName,omitempty"`

	// Number of desired PostgreSQL pods.
	// +optional
	// +kubebuilder:default=1
	// +kubebuilder:validation:Minimum=1
	Replicas *int32 `json:"replicas,omitempty"`

	// Minimum number of pods that should be available at a time.
	// Defaults to one when the replicas field is greater than one.
	// +optional
	MinAvailable *intstr.IntOrString `json:"minAvailable,omitempty"`

	// Compute resources of a PostgreSQL container.
	// +optional
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`

	// Tolerations of a PostgreSQL pod. Changing this value causes PostgreSQL to restart.
	// More info: https://kubernetes.io/docs/concepts/scheduling-eviction/taint-and-toleration
	// +optional
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`

	// Topology spread constraints of a PostgreSQL pod. Changing this value causes
	// PostgreSQL to restart.
	// More info: https://kubernetes.io/docs/concepts/workloads/pods/pod-topology-spread-constraints/
	// +optional
	TopologySpreadConstraints []corev1.TopologySpreadConstraint `json:"topologySpreadConstraints,omitempty"`

	// Defines a separate PersistentVolumeClaim for PostgreSQL's write-ahead log.
	// More info: https://www.postgresql.org/docs/current/wal.html
	// +optional
	WALVolumeClaimSpec *corev1.PersistentVolumeClaimSpec `json:"walVolumeClaimSpec,omitempty"`

	// Defines a PersistentVolumeClaim for PostgreSQL data.
	// More info: https://kubernetes.io/docs/concepts/storage/persistent-volumes
	// +kubebuilder:validation:Required
	DataVolumeClaimSpec corev1.PersistentVolumeClaimSpec `json:"dataVolumeClaimSpec"`
}

func (p PGInstanceSetSpec) ToCrunchy() crunchyv1beta1.PostgresInstanceSetSpec {
	return crunchyv1beta1.PostgresInstanceSetSpec{
		Metadata:                  p.Metadata,
		Name:                      p.Name,
		Affinity:                  p.Affinity,
		Containers:                p.Sidecars,
		PriorityClassName:         p.PriorityClassName,
		Replicas:                  p.Replicas,
		MinAvailable:              p.MinAvailable,
		Resources:                 p.Resources,
		Tolerations:               p.Tolerations,
		TopologySpreadConstraints: p.TopologySpreadConstraints,
		WALVolumeClaimSpec:        p.WALVolumeClaimSpec,
		DataVolumeClaimSpec:       p.DataVolumeClaimSpec,
	}
}

type ServiceExpose struct {
	crunchyv1beta1.Metadata `json:",inline"`

	// The port on which this service is exposed when type is NodePort or
	// LoadBalancer. Value must be in-range and not in use or the operation will
	// fail. If unspecified, a port will be allocated if this Service requires one.
	// - https://kubernetes.io/docs/concepts/services-networking/service/#type-nodeport
	// +optional
	NodePort *int32 `json:"nodePort,omitempty"`

	// More info: https://kubernetes.io/docs/concepts/services-networking/service/#publishing-services-service-types
	//
	// +optional
	// +kubebuilder:default=ClusterIP
	// +kubebuilder:validation:Enum={ClusterIP,NodePort,LoadBalancer}
	Type string `json:"type,omitempty"`
}

func (s *ServiceExpose) ToCrunchy() *crunchyv1beta1.ServiceSpec {
	if s == nil {
		return nil
	}

	return &crunchyv1beta1.ServiceSpec{
		Metadata: &crunchyv1beta1.Metadata{
			Annotations: s.Annotations,
			Labels:      s.Labels,
		},
		NodePort: s.NodePort,
		Type:     s.Type,
	}
}

type PGProxySpec struct {
	// Defines a PgBouncer proxy and connection pooler.
	PGBouncer *PGBouncerSpec `json:"pgBouncer"`
}

func (p *PGProxySpec) ToCrunchy() *crunchyv1beta1.PostgresProxySpec {
	if p == nil {
		return nil
	}

	return &crunchyv1beta1.PostgresProxySpec{
		PGBouncer: p.PGBouncer.ToCrunchy(),
	}
}

type PGBouncerSpec struct {
	// +optional
	Metadata *crunchyv1beta1.Metadata `json:"metadata,omitempty"`

	// Scheduling constraints of a PgBouncer pod. Changing this value causes
	// PgBouncer to restart.
	// More info: https://kubernetes.io/docs/concepts/scheduling-eviction/assign-pod-node
	// +optional
	Affinity *corev1.Affinity `json:"affinity,omitempty"`

	// Configuration settings for the PgBouncer process. Changes to any of these
	// values will be automatically reloaded without validation. Be careful, as
	// you may put PgBouncer into an unusable state.
	// More info: https://www.pgbouncer.org/usage.html#reload
	// +optional
	Config crunchyv1beta1.PGBouncerConfiguration `json:"config,omitempty"`

	// Custom sidecars for a PgBouncer pod. Changing this value causes
	// PgBouncer to restart.
	// +optional
	Sidecars []corev1.Container `json:"sidecars,omitempty"`

	// A secret projection containing a certificate and key with which to encrypt
	// connections to PgBouncer. The "tls.crt", "tls.key", and "ca.crt" paths must
	// be PEM-encoded certificates and keys. Changing this value causes PgBouncer
	// to restart.
	// More info: https://kubernetes.io/docs/concepts/configuration/secret/#projection-of-secret-keys-to-specific-paths
	// +optional
	CustomTLSSecret *corev1.SecretProjection `json:"customTLSSecret,omitempty"`

	// Name of a container image that can run PgBouncer 1.15 or newer. Changing
	// this value causes PgBouncer to restart. The image may also be set using
	// the RELATED_IMAGE_PGBOUNCER environment variable.
	// More info: https://kubernetes.io/docs/concepts/containers/images
	// +optional
	Image string `json:"image,omitempty"`

	// Port on which PgBouncer should listen for client connections. Changing
	// this value causes PgBouncer to restart.
	// +optional
	// +kubebuilder:default=5432
	// +kubebuilder:validation:Minimum=1024
	Port *int32 `json:"port,omitempty"`

	// Priority class name for the pgBouncer pod. Changing this value causes
	// PostgreSQL to restart.
	// More info: https://kubernetes.io/docs/concepts/scheduling-eviction/pod-priority-preemption/
	// +optional
	PriorityClassName *string `json:"priorityClassName,omitempty"`

	// Number of desired PgBouncer pods.
	// +optional
	// +kubebuilder:default=1
	// +kubebuilder:validation:Minimum=0
	Replicas *int32 `json:"replicas,omitempty"`

	// Minimum number of pods that should be available at a time.
	// Defaults to one when the replicas field is greater than one.
	// +optional
	MinAvailable *intstr.IntOrString `json:"minAvailable,omitempty"`

	// Compute resources of a PgBouncer container. Changing this value causes
	// PgBouncer to restart.
	// More info: https://kubernetes.io/docs/concepts/configuration/manage-resources-containers
	// +optional
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`

	// Specification of the service that exposes PgBouncer.
	// +optional
	ServiceExpose *ServiceExpose `json:"expose,omitempty"`

	// Tolerations of a PgBouncer pod. Changing this value causes PgBouncer to
	// restart.
	// More info: https://kubernetes.io/docs/concepts/scheduling-eviction/taint-and-toleration
	// +optional
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`

	// Topology spread constraints of a PgBouncer pod. Changing this value causes
	// PgBouncer to restart.
	// More info: https://kubernetes.io/docs/concepts/workloads/pods/pod-topology-spread-constraints/
	// +optional
	TopologySpreadConstraints []corev1.TopologySpreadConstraint `json:"topologySpreadConstraints,omitempty"`
}

func (p *PGBouncerSpec) ToCrunchy() *crunchyv1beta1.PGBouncerPodSpec {
	spec := &crunchyv1beta1.PGBouncerPodSpec{
		Metadata:                  p.Metadata,
		Affinity:                  p.Affinity,
		Config:                    p.Config,
		Containers:                p.Sidecars,
		CustomTLSSecret:           p.CustomTLSSecret,
		Image:                     p.Image,
		Port:                      p.Port,
		PriorityClassName:         p.PriorityClassName,
		Replicas:                  p.Replicas,
		MinAvailable:              p.MinAvailable,
		Resources:                 p.Resources,
		Service:                   p.ServiceExpose.ToCrunchy(),
		Tolerations:               p.Tolerations,
		TopologySpreadConstraints: p.TopologySpreadConstraints,
	}

	spec.Default()

	return spec
}

// +kubebuilder:object:root=true
// PostgresClusterList contains a list of PostgresCluster
type PerconaPGClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []PerconaPGCluster `json:"items"`
}

const annotationPrefix = "pg.percona.com/"

const (
	// PGBackRestBackup is the annotation that is added to a PerconaPGCluster to initiate a manual
	// backup.  The value of the annotation will be a unique identifier for a backup Job (e.g. a
	// timestamp), which will be stored in the PostgresCluster status to properly track completion
	// of the Job.  Also used to annotate the backup Job itself as needed to identify the backup
	// ID associated with a specific manual backup Job.
	AnnotationPGBackrestBackup = annotationPrefix + "pgbackrest-backup"

	// PGBackRestRestore is the annotation that is added to a PerconaPGCluster to initiate an in-place
	// restore.  The value of the annotation will be a unique identfier for a restore Job (e.g. a
	// timestamp), which will be stored in the PostgresCluster status to properly track completion
	// of the Job.
	AnnotationPGBackRestRestore = annotationPrefix + "pgbackrest-restore"
)

const DefaultVersionServiceEndpoint = "https://check.percona.com"

func GetDefaultVersionServiceEndpoint() string {
	endpoint := os.Getenv("PERCONA_VS_FALLBACK_URI")

	if len(endpoint) != 0 {
		return endpoint
	}

	return DefaultVersionServiceEndpoint
}
