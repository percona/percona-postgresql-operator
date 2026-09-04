package v2

import (
	"context"
	"os"
	"regexp"
	"slices"
	"strconv"

	gover "github.com/hashicorp/go-version"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	runtime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/percona/percona-postgresql-operator/v3/internal/config"
	"github.com/percona/percona-postgresql-operator/v3/internal/logging"
	"github.com/percona/percona-postgresql-operator/v3/internal/naming"
	pNaming "github.com/percona/percona-postgresql-operator/v3/percona/naming"
	"github.com/percona/percona-postgresql-operator/v3/percona/version"
	crunchyv1beta1 "github.com/percona/percona-postgresql-operator/v3/pkg/apis/upstream.pgv2.percona.com/v1beta1"
)

var allowedWALLevels = []string{"logical", "replica"}

func init() {
	SchemeBuilder.Register(&PerconaPGCluster{}, &PerconaPGClusterList{})
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=pg
// +kubebuilder:printcolumn:name="Endpoint",type=string,JSONPath=".status.host"
// +kubebuilder:printcolumn:name="Status",type=string,JSONPath=".status.state"
// +kubebuilder:printcolumn:name="Postgres",type=string,JSONPath=".status.postgres.ready"
// +kubebuilder:printcolumn:name="PGBouncer",type=string,JSONPath=".status.pgbouncer.ready"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +operator-sdk:csv:customresourcedefinitions:order=1
// +operator-sdk:csv:customresourcedefinitions:resources={{ConfigMap,v1},{Secret,v1},{Service,v1},{CronJob,v1beta1},{Deployment,v1},{Job,v1},{StatefulSet,v1},{PersistentVolumeClaim,v1}}
//
// PerconaPGCluster is the CRD that defines a Percona PG Cluster
type PerconaPGCluster struct { //nolint:recvcheck
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`

	Spec   PerconaPGClusterSpec   `json:"spec"`
	Status PerconaPGClusterStatus `json:"status,omitempty"`
}

// +kubebuilder:validation:XValidation:rule="!has(self.extensions) || !has(self.extensions.pg_tde) || !has(self.extensions.pg_tde.enabled) || !self.extensions.pg_tde.enabled || self.postgresVersion >= 16",message="pg_tde is only supported for PG16 and above"
// +kubebuilder:validation:XValidation:rule="!has(self.users) || self.postgresVersion >= 15 || self.users.all(u, !has(u.grantPublicSchemaAccess) || !u.grantPublicSchemaAccess)",message="PostgresVersion must be >= 15 if grantPublicSchemaAccess exists and is true"
// +kubebuilder:validation:XValidation:rule="!has(self.logicalReplicas) || size(self.logicalReplicas) == 0 || self.postgresVersion >= 17",message="spec.logicalReplicas requires spec.postgresVersion >= 17"
// +kubebuilder:validation:XValidation:rule="!has(self.logicalReplicas) || size(self.logicalReplicas) == 0 || !has(self.backups) || !has(self.backups.enabled) || self.backups.enabled || self.logicalReplicas.all(r, has(r.bootstrapMethod) && r.bootstrapMethod == 'pg_basebackup')",message="spec.logicalReplicas[].bootstrapMethod must be 'pg_basebackup' when spec.backups.enabled is false"
type PerconaPGClusterSpec struct {
	// +optional
	Metadata *crunchyv1beta1.Metadata `json:"metadata,omitempty"`

	// Version of the operator. Update this to new version after operator
	// upgrade to apply changes to Kubernetes objects. Default is the latest
	// version.
	// +optional
	// +kubebuilder:validation:XValidation:rule="self == \"\" || self.matches('^[0-9]+\\\\.[0-9]+\\\\.[0-9]+([-+][a-zA-Z0-9.+-]+)?$')",message="CRVersion must be a valid semantic version"
	CRVersion string `json:"crVersion,omitempty"`

	InitContainer *crunchyv1beta1.InitContainerSpec `json:"initContainer,omitempty"`

	// The image name to use for PostgreSQL containers.
	// +optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,order=1
	Image string `json:"image,omitempty"`

	// ImagePullPolicy is used to determine when Kubernetes will attempt to
	// pull (download) container images.
	// More info: https://kubernetes.io/docs/concepts/containers/images/#image-pull-policy
	// +kubebuilder:validation:Enum={Always,Never,IfNotPresent}
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// +optional
	ImagePullPolicy corev1.PullPolicy `json:"imagePullPolicy,omitempty"`

	// The image pull secrets used to pull from a private registry
	// Changing this value causes all running pods to restart.
	// https://k8s.io/docs/tasks/configure-pod-container/pull-image-private-registry/
	// +optional
	ImagePullSecrets []corev1.LocalObjectReference `json:"imagePullSecrets,omitempty"`

	TLSOnly bool `json:"tlsOnly,omitempty"`

	// +optional
	TLS *crunchyv1beta1.TLSSpec `json:"tls,omitempty"`

	// The port on which PostgreSQL should listen.
	// +optional
	// +kubebuilder:default=5432
	// +kubebuilder:validation:Minimum=1024
	Port *int32 `json:"port,omitempty"`

	// Specification of the service that exposes the PostgreSQL primary instance.
	// +optional
	Expose *ServiceExpose `json:"expose,omitempty"`

	// Specification of the service that exposes PostgreSQL replica instances
	// +optional
	ExposeReplicas *ServiceExpose `json:"exposeReplicas,omitempty"`

	// The major version of PostgreSQL installed in the PostgreSQL image
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Minimum=12
	// +kubebuilder:validation:Maximum=19
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	PostgresVersion int `json:"postgresVersion"`

	Secrets SecretsSpec `json:"secrets,omitempty"`

	// Run this cluster as a read-only copy of an existing cluster or archive.
	// +optional
	Standby *StandbySpec `json:"standby,omitempty"`

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
	Pause *bool `json:"pause,omitempty"`

	// Suspends the rollout and reconciliation of changes made to the
	// PostgresCluster spec.
	// +optional
	Unmanaged *bool `json:"unmanaged,omitempty"`

	// Specifies a data source for bootstrapping the PostgreSQL cluster.
	// +optional
	DataSource *crunchyv1beta1.DataSource `json:"dataSource,omitempty"`

	// Specifies one or more sets of PostgreSQL pods that replicate data for
	// this cluster.
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	InstanceSets PGInstanceSets `json:"instances"`

	// The specification of a proxy that connects to PostgreSQL.
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// +optional
	Proxy *PGProxySpec `json:"proxy,omitempty"`

	// PostgreSQL backup configuration
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	Backups Backups `json:"backups"`

	// The specification of PMM sidecars.
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// +optional
	PMM *PMMSpec `json:"pmm,omitempty"`

	// The specification of the log collector sidecar.
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// +optional
	LogCollector *LogCollectorSpec `json:"logcollector,omitempty"`

	// The specification of extensions.
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// +optional
	Extensions ExtensionsSpec `json:"extensions,omitempty"`

	// Indicates whether schemas are automatically created for the user
	// specified in `spec.users` across all databases associated with that user.
	// +optional
	AutoCreateUserSchema *bool `json:"autoCreateUserSchema,omitempty"`

	ClusterServiceDNSSuffix string `json:"clusterServiceDNSSuffix,omitempty"`

	// Configuration for PostgreSQL config files and server parameters.
	// Use spec.config.files to mount files (e.g. LDAP CA certificate) under
	// /etc/postgres, and spec.config.parameters to set postgresql.conf values.
	// +optional
	Config *crunchyv1beta1.PostgresConfigSpec `json:"config,omitempty"`

	// Defines custom pg_hba.conf authentication rules. Rules are evaluated
	// after mandatory operator rules and before the default scram-sha-256
	// fallback. Use this together with spec.config.files to supply supporting
	// files such as an LDAP CA certificate.
	// +optional
	Authentication *crunchyv1beta1.PostgresClusterAuthentication `json:"authentication,omitempty"`

	// Logical replicas are read-write PostgreSQL instances in this cluster that
	// receive changes from the primary over logical replication. Each one is
	// seeded with a physical copy of the primary, taken the way its
	// bootstrapMethod says, and converted with pg_createsubscriber, which
	// requires spec.postgresVersion to be 17 or higher.
	// +optional
	LogicalReplicas LogicalReplicas `json:"logicalReplicas,omitempty"`
}

// +listType=map
// +listMapKey=name
type LogicalReplicas []LogicalReplicaSpec

// ToCrunchy projects the logical replicas onto the Crunchy spec, which needs
// only their names.
func (l LogicalReplicas) ToCrunchy() []crunchyv1beta1.LogicalReplicaSpec {
	if len(l) == 0 {
		return nil
	}

	out := make([]crunchyv1beta1.LogicalReplicaSpec, 0, len(l))
	for _, replica := range l {
		out = append(out, crunchyv1beta1.LogicalReplicaSpec{Name: replica.Name})
	}
	return out
}

type LogicalReplicaSpec struct {
	// Name of the logical replica. It is used to name the StatefulSet, Service
	// and PersistentVolumeClaim of the replica, as well as the publications,
	// subscriptions and replication slots backing it.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MaxLength=20
	// +kubebuilder:validation:Pattern=`^[a-z][a-z0-9-]*[a-z0-9]$`
	Name string `json:"name"`

	// Databases to replicate. When empty, every database in the cluster except
	// the templates and "postgres" is replicated.
	// +listType=set
	// +optional
	Databases []crunchyv1beta1.PostgresIdentifier `json:"databases,omitempty"`

	// BootstrapMethod selects how the data volume is seeded before
	// pg_createsubscriber converts it into a subscriber.
	//
	// "pgbackrest" restores the cluster's most recent backup and puts no load on
	// the primary. "pg_basebackup" streams a fresh copy straight from the
	// primary and needs no pgBackRest repository, which is the only option when
	// spec.backups.enabled is false.
	//
	// It is only read while the replica is being bootstrapped; changing it on a
	// replica that already exists has no effect.
	// +kubebuilder:validation:Enum={pgbackrest,pg_basebackup}
	// +kubebuilder:default=pgbackrest
	// +optional
	BootstrapMethod LogicalReplicaBootstrapMethod `json:"bootstrapMethod,omitempty"`

	// Defines the data volume of the logical replica.
	// +kubebuilder:validation:Required
	DataVolumeClaimSpec corev1.PersistentVolumeClaimSpec `json:"dataVolumeClaimSpec"`

	// +optional
	Metadata *crunchyv1beta1.Metadata `json:"metadata,omitempty"`

	// +optional
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`

	// +optional
	Affinity *corev1.Affinity `json:"affinity,omitempty"`

	// +optional
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`

	// +optional
	PriorityClassName *string `json:"priorityClassName,omitempty"`

	// Specification of the service that exposes this logical replica.
	// +optional
	Expose *ServiceExpose `json:"expose,omitempty"`

	// StartupProbe sets the startup probe for the logical replica container.
	// +optional
	StartupProbe *corev1.Probe `json:"startupProbe,omitempty"`

	// LivenessProbe sets the liveness probe for the logical replica container.
	// +optional
	LivenessProbe *corev1.Probe `json:"livenessProbe,omitempty"`

	// ReadinessProbe sets the readiness probe for the logical replica container.
	// +optional
	ReadinessProbe *corev1.Probe `json:"readinessProbe,omitempty"`
}

func (cr *PerconaPGCluster) IsPaused() bool {
	return cr.Spec.Pause != nil && *cr.Spec.Pause
}

// LogicalReplicaBootstrapMethod selects how the data volume of a logical
// replica is seeded with a physical copy of the primary, before
// pg_createsubscriber converts it into a subscriber.
type LogicalReplicaBootstrapMethod string

const (
	LogicalReplicaBootstrapMethodPGBackRest   LogicalReplicaBootstrapMethod = "pgbackrest"
	LogicalReplicaBootstrapMethodPGBaseBackup LogicalReplicaBootstrapMethod = "pg_basebackup"
)

// BootstrapMethodOrDefault returns the method the data volume of the replica is
// seeded with. The field carries a CRD default, so this only matters for a spec
// the API server has not defaulted.
func (s *LogicalReplicaSpec) BootstrapMethodOrDefault() LogicalReplicaBootstrapMethod {
	if s.BootstrapMethod == "" {
		return LogicalReplicaBootstrapMethodPGBackRest
	}
	return s.BootstrapMethod
}

// LogicalReplicasEnabled returns whether the cluster has any logical replica
// configured.
func (cr *PerconaPGCluster) LogicalReplicasEnabled() bool {
	return cr.CompareVersion("3.1.0") >= 0 && len(cr.Spec.LogicalReplicas) > 0
}

// rePostgresIdentifier is the pattern the CRD enforces on spec.users[].name.
var rePostgresIdentifier = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)

// defaultUser returns the user and database named after the cluster that the
// crunchy layer creates on its own when spec.users is unset. ok is false when
// the cluster name cannot be a PostgreSQL identifier, which is exactly when the
// crunchy layer skips the default too.
func (cr *PerconaPGCluster) defaultUser() (crunchyv1beta1.PostgresUserSpec, bool) {
	if len(cr.Name) > 63 || !rePostgresIdentifier.MatchString(cr.Name) {
		return crunchyv1beta1.PostgresUserSpec{}, false
	}

	identifier := crunchyv1beta1.PostgresIdentifier(cr.Name)
	return crunchyv1beta1.PostgresUserSpec{
		Name:      identifier,
		Databases: []crunchyv1beta1.PostgresIdentifier{identifier},
		Password: &crunchyv1beta1.PostgresPasswordSpec{
			Type: crunchyv1beta1.PostgresPasswordTypeAlphaNumeric,
		},
	}, true
}

type ContainerOptions struct {
	Env     []corev1.EnvVar        `json:"env,omitempty"`
	EnvFrom []corev1.EnvFromSource `json:"envFrom,omitempty"`
}

type StandbySpec struct {
	*crunchyv1beta1.PostgresStandbySpec `json:",inline"`

	// +optional
	// MaxAcceptableLag is the maximum WAL lag allowed for the standby cluster, measured in bytes of WAL data.
	// This represents the maximum amount of WAL data that the standby can be behind the primary.
	// If the lag exceeds this value, the standby cluster is marked as unready.
	// If unset, lag is not checked.
	MaxAcceptableLag *resource.Quantity `json:"maxAcceptableLag,omitempty"`
}

func (cr *PerconaPGCluster) ShouldCheckStandbyLag() bool {
	return cr.CompareVersion("2.9.0") >= 0 &&
		cr.Spec.Standby != nil &&
		cr.Spec.Standby.Enabled &&
		cr.Spec.Standby.MaxAcceptableLag != nil
}

func (cr *PerconaPGCluster) Default() {
	if len(cr.Spec.CRVersion) == 0 {
		cr.Spec.CRVersion = version.Version()
	}

	for i := range cr.Spec.InstanceSets {
		if cr.Spec.InstanceSets[i].Metadata == nil {
			cr.Spec.InstanceSets[i].Metadata = new(crunchyv1beta1.Metadata)
		}
		if cr.Spec.InstanceSets[i].Metadata.Labels == nil {
			cr.Spec.InstanceSets[i].Metadata.Labels = make(map[string]string)
		}
		cr.Spec.InstanceSets[i].Metadata.Labels[LabelOperatorVersion] = cr.Spec.CRVersion
	}

	if cr.CompareVersion("2.9.0") < 0 || cr.Spec.Proxy.IsSet() {
		if cr.Spec.Proxy == nil {
			cr.Spec.Proxy = &PGProxySpec{}
		}

		if cr.Spec.Proxy.PGBouncer == nil {
			cr.Spec.Proxy.PGBouncer = &PGBouncerSpec{}
		}

		if cr.Spec.Proxy.PGBouncer.Metadata == nil {
			cr.Spec.Proxy.PGBouncer.Metadata = &crunchyv1beta1.Metadata{}
		}
		if cr.Spec.Proxy.PGBouncer.Metadata.Labels == nil {
			cr.Spec.Proxy.PGBouncer.Metadata.Labels = make(map[string]string)
		}
		cr.Spec.Proxy.PGBouncer.Metadata.Labels[LabelOperatorVersion] = cr.Spec.CRVersion
	}

	if cr.Spec.Backups.IsEnabled() {
		if cr.Spec.Backups.TrackLatestRestorableTime == nil {
			cr.Spec.Backups.TrackLatestRestorableTime = new(true)
		}
		if cr.Spec.Backups.PGBackRest.Metadata == nil {
			cr.Spec.Backups.PGBackRest.Metadata = new(crunchyv1beta1.Metadata)
		}
		if cr.Spec.Backups.PGBackRest.Metadata.Labels == nil {
			cr.Spec.Backups.PGBackRest.Metadata.Labels = make(map[string]string)
		}
		cr.Spec.Backups.PGBackRest.Metadata.Labels[LabelOperatorVersion] = cr.Spec.CRVersion

		if cr.Spec.Backups.PGBackRest.Jobs == nil {
			cr.Spec.Backups.PGBackRest.Jobs = new(crunchyv1beta1.BackupJobs)
		}
	}

	cr.SetExtensionDefaults()

	if cr.CompareVersion("3.1.0") >= 0 && cr.Spec.Backups.Enabled == nil {
		cr.Spec.Backups.Enabled = new(true)
	}

	if cr.CompareVersion("2.9.0") < 0 && cr.Spec.Config == nil {
		cr.Spec.Config = &crunchyv1beta1.PostgresConfigSpec{}
	}

	if cr.Spec.Backups.IsVolumeSnapshotsEnabled() &&
		cr.Spec.Backups.VolumeSnapshots.Mode == VolumeSnapshotModeOffline &&
		cr.Spec.Backups.VolumeSnapshots.OfflineConfig == nil {
		cr.Spec.Backups.VolumeSnapshots.OfflineConfig = DefaultOfflineSnapshotConfig()
	}

	if cr.CompareVersion("2.6.0") >= 0 && cr.Spec.AutoCreateUserSchema == nil {
		cr.Spec.AutoCreateUserSchema = new(true)
	}
}

func (cr *PerconaPGCluster) SetExtensionDefaults() {
	// for backward compatibility, delete after 3.4.0
	if cr.Spec.Extensions.BuiltIn.PGStatMonitor != nil {
		cr.Spec.Extensions.PGStatMonitor.Enabled = cr.Spec.Extensions.BuiltIn.PGStatMonitor
	}
	if cr.Spec.Extensions.BuiltIn.PGStatStatements != nil {
		cr.Spec.Extensions.PGStatStatements.Enabled = cr.Spec.Extensions.BuiltIn.PGStatStatements
	}
	if cr.Spec.Extensions.BuiltIn.PGAudit != nil {
		cr.Spec.Extensions.PGAudit.Enabled = cr.Spec.Extensions.BuiltIn.PGAudit
	}
	if cr.Spec.Extensions.BuiltIn.PGRepack != nil {
		cr.Spec.Extensions.PGRepack.Enabled = cr.Spec.Extensions.BuiltIn.PGRepack
	}
	if cr.Spec.Extensions.BuiltIn.PGVector != nil {
		cr.Spec.Extensions.PGVector.Enabled = cr.Spec.Extensions.BuiltIn.PGVector
	}

	if cr.Spec.Extensions.PGStatMonitor.Enabled == nil {
		cr.Spec.Extensions.PGStatMonitor.Enabled = new(true)
		if cr.CompareVersion("2.9.0") >= 0 {
			var qs PMMQuerySource
			if cr.PMMEnabled() {
				qs = cr.Spec.PMM.QuerySource
			}
			cr.Spec.Extensions.PGStatMonitor.Enabled = new(qs == PgStatMonitor)
		}
	}
	if cr.Spec.Extensions.PGStatStatements.Enabled == nil {
		cr.Spec.Extensions.PGStatStatements.Enabled = new(false)
		if cr.CompareVersion("2.9.0") >= 0 {
			var qs PMMQuerySource
			if cr.PMMEnabled() {
				qs = cr.Spec.PMM.QuerySource
			}
			cr.Spec.Extensions.PGStatStatements.Enabled = new(qs == PgStatStatements)
		}
	}

	if cr.Spec.Extensions.PGAudit.Enabled == nil {
		cr.Spec.Extensions.PGAudit.Enabled = new(true)
	}
	if cr.Spec.Extensions.PGVector.Enabled == nil {
		cr.Spec.Extensions.PGVector.Enabled = new(false)
	}
	if cr.Spec.Extensions.PGRepack.Enabled == nil {
		cr.Spec.Extensions.PGRepack.Enabled = new(false)
	}
	if cr.Spec.Extensions.SetUser.Enabled == nil {
		cr.Spec.Extensions.SetUser.Enabled = new(false)
	}
	if cr.Spec.Extensions.PGCron.Enabled == nil {
		cr.Spec.Extensions.PGCron.Enabled = new(false)
	}

	// for backward compatibility, delete after 3.4.0
	if cr.Spec.Extensions.BuiltIn.PGStatMonitor == nil {
		cr.Spec.Extensions.BuiltIn.PGStatMonitor = cr.Spec.Extensions.PGStatMonitor.Enabled
	}
	if cr.Spec.Extensions.BuiltIn.PGStatStatements == nil {
		cr.Spec.Extensions.BuiltIn.PGStatStatements = cr.Spec.Extensions.PGStatStatements.Enabled
	}
	if cr.Spec.Extensions.BuiltIn.PGAudit == nil {
		cr.Spec.Extensions.BuiltIn.PGAudit = cr.Spec.Extensions.PGAudit.Enabled
	}
	if cr.Spec.Extensions.BuiltIn.PGVector == nil {
		cr.Spec.Extensions.BuiltIn.PGVector = cr.Spec.Extensions.PGVector.Enabled
	}
	if cr.Spec.Extensions.BuiltIn.PGRepack == nil {
		cr.Spec.Extensions.BuiltIn.PGRepack = cr.Spec.Extensions.PGRepack.Enabled
	}
}

func (cr *PerconaPGCluster) Validate() error {
	if cr.Spec.DataSource != nil && cr.Spec.Backups.PGBackRest.Image == "" && os.Getenv("RELATED_IMAGE_PGBACKREST") == "" {
		return errors.New("spec.backups.pgbackrest.image or RELATED_IMAGE_PGBACKREST is required when spec.dataSource is set")
	}
	if ptr.Deref(cr.Spec.Extensions.BuiltIn.PGStatMonitor, false) &&
		ptr.Deref(cr.Spec.Extensions.BuiltIn.PGStatStatements, false) {
		return errors.New("pg_stat_monitor and pg_stat_statements cannot both be enabled")
	}
	// Extension packages are not built for PostgreSQL 19 (beta) yet; loading them
	// via shared_preload_libraries would make postgres fail to start.
	// pgAudit is the exception: the PG 19 community image compiles it from source.
	// Remove this check once PostgreSQL 19 goes GA and the extensions are available.
	if cr.Spec.PostgresVersion >= 19 {
		for _, ext := range []struct {
			name    string
			enabled *bool
		}{
			{"pg_cron", cr.Spec.Extensions.PGCron.Enabled},
			{"set_user", cr.Spec.Extensions.SetUser.Enabled},
		} {
			if ptr.Deref(ext.enabled, false) {
				return errors.Errorf("spec.extensions.%s.enabled cannot be set for PostgreSQL %d: extension packages are not built for beta releases", ext.name, cr.Spec.PostgresVersion)
			}
		}
	}
	if err := cr.ValidateDynamicConfiguration(); err != nil {
		return err
	}
	if err := cr.ValidateLogicalReplicas(); err != nil {
		return err
	}
	return nil
}

// ValidateLogicalReplicas checks the invariants of spec.logicalReplicas that
// cannot be expressed with kubebuilder markers.
func (cr *PerconaPGCluster) ValidateLogicalReplicas() error {
	if len(cr.Spec.LogicalReplicas) == 0 {
		return nil
	}

	// A logical replica names a StatefulSet in the same namespace as the
	// instance sets do, so the names must not collide.
	instanceSets := make(map[string]struct{}, len(cr.Spec.InstanceSets))
	for _, set := range cr.Spec.InstanceSets {
		instanceSets[set.Name] = struct{}{}
	}

	seen := make(map[string]struct{}, len(cr.Spec.LogicalReplicas))
	for _, replica := range cr.Spec.LogicalReplicas {
		if _, ok := seen[replica.Name]; ok {
			return errors.Errorf("duplicate spec.logicalReplicas name %q", replica.Name)
		}
		seen[replica.Name] = struct{}{}

		if _, ok := instanceSets[replica.Name]; ok {
			return errors.Errorf("spec.logicalReplicas name %q conflicts with an instance set of the same name", replica.Name)
		}

		dbs := make(map[crunchyv1beta1.PostgresIdentifier]struct{}, len(replica.Databases))
		for _, db := range replica.Databases {
			if _, ok := dbs[db]; ok {
				return errors.Errorf("duplicate database %q in spec.logicalReplicas %q", db, replica.Name)
			}
			dbs[db] = struct{}{}
		}
	}

	return nil
}

func (cr *PerconaPGCluster) ValidateDynamicConfiguration() error {
	if cr.Spec.Patroni == nil || cr.Spec.Patroni.DynamicConfiguration == nil {
		return nil
	}

	postgresql, ok := cr.Spec.Patroni.DynamicConfiguration["postgresql"].(map[string]any)
	if !ok {
		return nil
	}

	params, ok := postgresql["parameters"].(map[string]any)
	if !ok {
		return nil
	}

	walLevel, ok := params["wal_level"].(string)
	if ok && !slices.Contains(allowedWALLevels, walLevel) {
		return errors.Errorf("invalid value for spec.patroni.dynamicConfiguration.postgresql.parameters.wal_level: %q; must be 'logical' or 'replica'", walLevel)
	}

	return nil
}

func (cr *PerconaPGCluster) PostgresImage() string {
	image := cr.Spec.Image
	postgresVersion := cr.Spec.PostgresVersion
	return config.PostgresContainerImageString(image, postgresVersion, "")
}

func (cr *PerconaPGCluster) ToCrunchy(ctx context.Context, postgresCluster *crunchyv1beta1.PostgresCluster, scheme *runtime.Scheme) (*crunchyv1beta1.PostgresCluster, error) {
	log := logging.FromContext(ctx)

	if postgresCluster == nil {
		postgresCluster = &crunchyv1beta1.PostgresCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:       cr.Name,
				Namespace:  cr.Namespace,
				Finalizers: []string{naming.Finalizer},
			},
		}
	}

	if err := controllerutil.SetControllerReference(cr, postgresCluster, scheme); err != nil {
		return nil, err
	}

	// omitting error because it is always nil
	_ = postgresCluster.Default(ctx, postgresCluster)

	annotations := make(map[string]string)
	for k, v := range cr.Annotations {
		switch k {
		case corev1.LastAppliedConfigAnnotation:
			continue
		default:
			annotations[pNaming.ToCrunchyAnnotation(k)] = v
		}
	}

	if cr.Spec.AutoCreateUserSchema != nil && *cr.Spec.AutoCreateUserSchema {
		annotations[naming.AutoCreateUserSchemaAnnotation] = "true"
	}

	postgresCluster.Annotations = annotations
	postgresCluster.Labels = cr.Labels
	if postgresCluster.Labels == nil {
		postgresCluster.Labels = make(map[string]string)
	}
	postgresCluster.Labels[LabelOperatorVersion] = cr.Spec.CRVersion

	postgresCluster.Spec.Metadata = cr.Spec.Metadata
	postgresCluster.Spec.Image = cr.Spec.Image
	postgresCluster.Spec.ImagePullPolicy = cr.Spec.ImagePullPolicy
	postgresCluster.Spec.ImagePullSecrets = cr.Spec.ImagePullSecrets

	postgresCluster.Spec.PostgresVersion = cr.Spec.PostgresVersion
	postgresCluster.Spec.Port = cr.Spec.Port
	postgresCluster.Spec.OpenShift = cr.Spec.OpenShift
	postgresCluster.Spec.Paused = cr.Spec.Unmanaged
	postgresCluster.Spec.Shutdown = cr.Spec.Pause

	if cr.Spec.Standby != nil {
		postgresCluster.Spec.Standby = cr.Spec.Standby.PostgresStandbySpec
	}
	postgresCluster.Spec.Service = cr.Spec.Expose.ToCrunchy(cr.Spec.CRVersion)
	postgresCluster.Spec.ReplicaService = cr.Spec.ExposeReplicas.ToCrunchy(cr.Spec.CRVersion)

	postgresCluster.Spec.CustomReplicationClientTLSSecret = cr.Spec.Secrets.CustomReplicationClientTLSSecret
	postgresCluster.Spec.CustomTLSSecret = cr.Spec.Secrets.CustomTLSSecret
	postgresCluster.Spec.CustomRootCATLSSecret = cr.Spec.Secrets.CustomRootCATLSSecret

	postgresCluster.Spec.Backups = cr.Spec.Backups.ToCrunchy(cr.Spec.CRVersion)
	for i := range postgresCluster.Spec.Backups.PGBackRest.Repos {
		repo := postgresCluster.Spec.Backups.PGBackRest.Repos[i]

		if repo.BackupSchedules == nil {
			continue
		}
		repo.BackupSchedules.Differential = nil
		repo.BackupSchedules.Full = nil
		repo.BackupSchedules.Incremental = nil
	}

	postgresCluster.Spec.DataSource = cr.Spec.DataSource
	postgresCluster.Spec.DatabaseInitSQL = cr.Spec.DatabaseInitSQL
	postgresCluster.Spec.Patroni = cr.Spec.Patroni

	users := make([]crunchyv1beta1.PostgresUserSpec, 0)

	for _, user := range cr.Spec.Users {
		if user.Name == UserMonitoring {
			log.Info(UserMonitoring + " user is reserved, it'll be ignored.")
			continue
		}
		if user.Name == UserLogicalReplication {
			log.Info(UserLogicalReplication + " user is reserved, it'll be ignored.")
			continue
		}
		users = append(users, user)
	}

	// The crunchy layer creates a user and a database named after the cluster
	// only when spec.users is unset, so the reserved users below would take that
	// default away from a cluster that declares no users of its own.
	if len(users) == 0 && (cr.PMMEnabled() || cr.LogicalReplicasEnabled()) {
		if user, ok := cr.defaultUser(); ok {
			users = append(users, user)
		}
	}

	if cr.PMMEnabled() {
		users = append(users, crunchyv1beta1.PostgresUserSpec{
			Name:    UserMonitoring,
			Options: "SUPERUSER",
			Password: &crunchyv1beta1.PostgresPasswordSpec{
				Type: crunchyv1beta1.PostgresPasswordTypeAlphaNumeric,
			},
		})
	}

	// SUPERUSER because pg_createsubscriber creates a publication FOR
	// ALL TABLES, which plain REPLICATION roles such as _crunchyrepl cannot do.
	if cr.LogicalReplicasEnabled() {
		users = append(users, crunchyv1beta1.PostgresUserSpec{
			Name:    UserLogicalReplication,
			Options: "SUPERUSER REPLICATION",
			Password: &crunchyv1beta1.PostgresPasswordSpec{
				Type: crunchyv1beta1.PostgresPasswordTypeAlphaNumeric,
			},
		})
	}

	postgresCluster.Spec.Users = users

	// crunchy layer renders pg_hba rules, server parameters and
	// Patroni's ignore_slots from this.
	postgresCluster.Spec.LogicalReplicas = cr.Spec.LogicalReplicas.ToCrunchy()

	postgresCluster.Spec.InstanceSets = cr.Spec.InstanceSets.ToCrunchy()
	postgresCluster.Spec.Proxy = cr.Spec.Proxy.ToCrunchy(cr.Spec.CRVersion)

	postgresCluster.Spec.Extensions.PGTDE = cr.Spec.Extensions.PGTDE
	if cr.Spec.Extensions.PGStatMonitor.Enabled != nil {
		postgresCluster.Spec.Extensions.PGStatMonitor = *cr.Spec.Extensions.PGStatMonitor.Enabled
	}
	if cr.Spec.Extensions.PGStatStatements.Enabled != nil {
		postgresCluster.Spec.Extensions.PGStatStatements = *cr.Spec.Extensions.PGStatStatements.Enabled
	}
	if cr.Spec.Extensions.PGAudit.Enabled != nil {
		postgresCluster.Spec.Extensions.PGAudit = *cr.Spec.Extensions.PGAudit.Enabled
	}
	if cr.Spec.Extensions.PGVector.Enabled != nil {
		postgresCluster.Spec.Extensions.PGVector = *cr.Spec.Extensions.PGVector.Enabled
	}
	if cr.Spec.Extensions.PGRepack.Enabled != nil {
		postgresCluster.Spec.Extensions.PGRepack = *cr.Spec.Extensions.PGRepack.Enabled
	}
	if cr.Spec.Extensions.PGCron.Enabled != nil {
		postgresCluster.Spec.Extensions.PGCron = *cr.Spec.Extensions.PGCron.Enabled
	}
	if cr.Spec.Extensions.SetUser.Enabled != nil {
		postgresCluster.Spec.Extensions.SetUser = *cr.Spec.Extensions.SetUser.Enabled
	}

	postgresCluster.Spec.TLSOnly = cr.Spec.TLSOnly
	postgresCluster.Spec.TLS = cr.Spec.TLS

	postgresCluster.Spec.InitContainer = cr.Spec.InitContainer
	postgresCluster.Spec.ClusterServiceDNSSuffix = cr.Spec.ClusterServiceDNSSuffix
	postgresCluster.Spec.Config = cr.Spec.Config
	postgresCluster.Spec.Authentication = cr.Spec.Authentication

	return postgresCluster, nil
}

func (cr *PerconaPGCluster) Version() *gover.Version {
	crVersion := cr.Spec.CRVersion
	if crVersion == "" {
		crVersion = version.Version()
	}
	return gover.Must(gover.NewVersion(crVersion))
}

func (cr *PerconaPGCluster) CompareVersion(ver string) int {
	return cr.Version().Compare(gover.Must(gover.NewVersion(ver)))
}

type AppState string

const (
	AppStateInit     AppState = "initializing"
	AppStatePaused   AppState = "paused"
	AppStateStopping AppState = "stopping"
	AppStateReady    AppState = "ready"
)

type PostgresInstanceSetStatus struct {
	Name string `json:"name"`

	Size int32 `json:"size"`

	Ready int32 `json:"ready"`
}

type PostgresStatus struct {
	// +optional
	Size int32 `json:"size"`

	// +optional
	Ready int32 `json:"ready"`

	// +optional
	InstanceSets []PostgresInstanceSetStatus `json:"instances"`

	// +optional
	Version int `json:"version"`

	// +optional
	ImageID string `json:"imageID"`

	// +optional
	Distribution string `json:"distribution,omitempty"`
}

// PostgreSQL distribution values reported in PostgresStatus.Distribution.
const (
	PostgresDistributionPercona   = "percona"
	PostgresDistributionCommunity = "community"
)

type PGBouncerStatus struct {
	Size int32 `json:"size"`

	Ready int32 `json:"ready"`
}

type PerconaPGClusterStatus struct {
	// +optional
	// +operator-sdk:csv:customresourcedefinitions:type=status
	Postgres PostgresStatus `json:"postgres"`

	// +optional
	// +operator-sdk:csv:customresourcedefinitions:type=status
	PGBouncer PGBouncerStatus `json:"pgbouncer"`

	// +optional
	// +operator-sdk:csv:customresourcedefinitions:type=status
	State AppState `json:"state"`

	// +optional
	// +operator-sdk:csv:customresourcedefinitions:type=status
	// Deprecated: Use Patroni instead. This field will be removed in a future release.
	PatroniVersion string `json:"patroniVersion"`

	// +optional
	// +operator-sdk:csv:customresourcedefinitions:type=status
	Patroni Patroni `json:"patroni,omitempty"`

	// Status information for pgBackRest
	// +optional
	PGBackRest *crunchyv1beta1.PGBackRestStatus `json:"pgbackrest,omitempty"`

	// +optional
	// +operator-sdk:csv:customresourcedefinitions:type=status
	Host string `json:"host"`

	// +optional
	// +operator-sdk:csv:customresourcedefinitions:type=status
	InstalledCustomExtensions []string `json:"installedCustomExtensions"`

	// +optional
	// +operator-sdk:csv:customresourcedefinitions:type=status
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// +optional
	// +operator-sdk:csv:customresourcedefinitions:type=status
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// +optional
	// +operator-sdk:csv:customresourcedefinitions:type=status
	Standby *StandbyStatus `json:"standby,omitempty"`

	// +optional
	// +listType=map
	// +listMapKey=name
	// +operator-sdk:csv:customresourcedefinitions:type=status
	LogicalReplicas []LogicalReplicaStatus `json:"logicalReplicas,omitempty"`
}

type StandbyStatus struct {
	LagLastComputedAt *metav1.Time `json:"lagLastComputedAt,omitempty"`
	LagBytes          int64        `json:"lagBytes,omitempty"`
}

type LogicalReplicaState string

const (
	// LogicalReplicaStateBootstrapping means the replica is being seeded and
	// converted by the bootstrap Job, or has been and is not serving yet.
	LogicalReplicaStateBootstrapping LogicalReplicaState = "bootstrapping"

	LogicalReplicaStateReady LogicalReplicaState = "ready"

	// LogicalReplicaStateBroken means replication has stopped and the replica
	// needs to be recreated.
	LogicalReplicaStateBroken LogicalReplicaState = "broken"

	// LogicalReplicaStateSuspended means the replica was stopped on purpose,
	// because the cluster it replicates is being restored. Not an error.
	LogicalReplicaStateSuspended LogicalReplicaState = "suspended"
)

const (
	// LogicalReplicaReasonSourceSlotMissing means the replication slot backing
	// this replica is gone from the primary. A slot lives only on the primary
	// that created it, so a failover is the most common cause.
	LogicalReplicaReasonSourceSlotMissing = "SourceSlotMissing"

	LogicalReplicaReasonSubscriptionDisabled = "SubscriptionDisabled"

	// LogicalReplicaReasonApplyWorkerDown means a subscription is enabled but has
	// no running apply worker, usually because it cannot reach the primary. It
	// retries forever without disabling the subscription, so nothing else shows
	// that replication has stopped.
	LogicalReplicaReasonApplyWorkerDown = "ApplyWorkerDown"

	LogicalReplicaReasonBootstrapFailed = "BootstrapFailed"
	LogicalReplicaReasonPodNotFound     = "LogicalReplicaPodNotFound"

	// LogicalReplicaReasonPrimaryNotReady means the primary does not yet carry
	// everything the bootstrap needs; the ReadyForLogicalReplication condition
	// says which prerequisite is missing.
	LogicalReplicaReasonPrimaryNotReady = "PrimaryNotReady"

	LogicalReplicaReasonClusterPaused = "ClusterPaused"

	LogicalReplicaReasonSourceRestoring = "SourceRestoring"

	// LogicalReplicaReasonSourceRestored means the data directory this replica was
	// seeded from has been replaced by a restore. The replication slots went with
	// it - pgBackRest does not back up pg_replslot - and a point-in-time restore
	// has rewound the primary past changes the replica already applied, so nothing
	// short of seeding it again fixes it.
	LogicalReplicaReasonSourceRestored = "SourceRestored"

	// LogicalReplicaReasonSourceUpgraded means the cluster went through a major
	// version upgrade after this replica was seeded.
	LogicalReplicaReasonSourceUpgraded = "SourceUpgraded"

	// LogicalReplicaReasonWaitingForDataVolume means the data volume of an
	// earlier incarnation of this replica is still being deleted.
	LogicalReplicaReasonWaitingForDataVolume = "WaitingForDataVolume"

	// LogicalReplicaReasonWaitingForDatabases means the databases this replica
	// would replicate do not exist on the primary yet. The set is frozen for the
	// replica's lifetime, so it waits rather than seeding from a partial list.
	LogicalReplicaReasonWaitingForDatabases = "WaitingForDatabases"

	// LogicalReplicaReasonAwaitingCleanup means the replica has been removed from
	// the spec but its objects on the primary could not be dropped yet. Its status
	// is kept until they are: forgetting it leaks a slot that pins WAL.
	LogicalReplicaReasonAwaitingCleanup = "AwaitingCleanup"
)

type LogicalReplicaStatus struct {
	Name string `json:"name"`

	// +optional
	State LogicalReplicaState `json:"state,omitempty"`

	// +optional
	Reason string `json:"reason,omitempty"`

	// +optional
	Message string `json:"message,omitempty"`

	// Databases replicated by this replica. It is resolved once, when the
	// replica is bootstrapped, and does not change afterwards.
	// +optional
	Databases []string `json:"databases,omitempty"`

	// SeededAt is when the data this replica serves was copied from the
	// cluster.
	// +optional
	SeededAt *metav1.Time `json:"seededAt,omitempty"`

	// PostgresVersion is the major PostgreSQL version of the data directory this
	// replica holds, recorded when it was seeded. The data directory is scoped to
	// a major version and pg_upgrade never touches it, so once spec.postgresVersion
	// moves past this the replica cannot be started again.
	// +optional
	PostgresVersion int `json:"postgresVersion,omitempty"`

	// InvalidatedAt is when the operator established that the data on this
	// replica can no longer be reconciled with the cluster, because the cluster
	// was restored in place or upgraded to a new major version after the replica
	// was seeded from it. The replica stays stopped until it is removed from
	// spec.logicalReplicas and added back, which seeds it again from scratch.
	// +optional
	InvalidatedAt *metav1.Time `json:"invalidatedAt,omitempty"`
}

type Patroni struct {
	// +optional
	Status *crunchyv1beta1.PatroniStatus `json:"status,omitempty"`

	// +optional
	// +operator-sdk:csv:customresourcedefinitions:type=status
	Version string `json:"version"`
}

// Backups struct.
// +kubebuilder:validation:XValidation:rule="(has(self.enabled) && self.enabled == false) || (has(self.pgbackrest.repos) && size(self.pgbackrest.repos) > 0)",message="At least one repository must be configured when backups are enabled"
type Backups struct { //nolint:recvcheck
	// Enabled controls whether backups are enabled for the cluster.
	// Defaulted to true by the operator for crVersion >= 3.1.0.
	// +optional
	Enabled *bool `json:"enabled,omitempty"`

	// pgBackRest archive configuration
	// +optional
	PGBackRest PGBackRestArchive `json:"pgbackrest"`

	// Enable tracking latest restorable time
	TrackLatestRestorableTime *bool `json:"trackLatestRestorableTime,omitempty"`

	// VolumeSnapshots configuration
	// +optional
	VolumeSnapshots *VolumeSnapshots `json:"volumeSnapshots,omitempty"`
}

type VolumeSnapshotMode string

const (
	// VolumeSnapshotModeOffline is the mode for taking offline VolumeSnapshots.
	// With this mode, the operator will stop a replica and take a snapshot of the PVC.
	VolumeSnapshotModeOffline VolumeSnapshotMode = "offline"
)

type VolumeSnapshots struct {
	// Mode of the VolumeSnapshot.
	// +kubebuilder:validation:Enum={offline}
	// +kubebuilder:default=offline
	// +optional
	Mode VolumeSnapshotMode `json:"mode,omitempty"`

	// Name of the VolumeSnapshotClass to use.
	// +kubebuilder:validation:Required
	ClassName string `json:"className"`

	// Defines the Cron schedule for a VolumeSnapshot.
	// Follows the standard Cron schedule syntax:
	// https://k8s.io/docs/concepts/workloads/controllers/cron-jobs/#cron-schedule-syntax
	// +optional
	// +kubebuilder:validation:MinLength=6
	Schedule *string `json:"schedule,omitempty"`

	// Configuration for offline snapshot operations.
	// Ignored if mode is not offline.
	// +optional
	OfflineConfig *OfflineSnapshotConfig `json:"offlineConfig,omitempty"`

	// Jobs allows configuration for all VolumeSnapshot jobs.
	// +optional
	Jobs *VolumeSnapshotJobSpec `json:"jobs,omitempty"`
}

type VolumeSnapshotJobSpec struct {
	// Tolerations that will be applied on the VolumeSnapshot Job.
	// +optional
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`
}

func DefaultOfflineSnapshotConfig() *OfflineSnapshotConfig {
	return &OfflineSnapshotConfig{
		Checkpoint: &CheckpointConfig{
			Enabled:        new(true),
			TimeoutSeconds: new(int32(300)),
		},
	}
}

type OfflineSnapshotConfig struct {
	// Checkpoint configuration for offline snapshot operations.
	// +optional
	Checkpoint *CheckpointConfig `json:"checkpoint,omitempty"`
}

type CheckpointConfig struct {
	// If set, a checkpoint is requested.
	// +optional
	// +kubebuilder:default=true
	Enabled *bool `json:"enabled,omitempty"`

	// Timeout for the checkpoint operation.
	// Ignored if checkpoint is not enabled.
	// +optional
	// +kubebuilder:validation:Minimum=30
	// +kubebuilder:default=300
	TimeoutSeconds *int32 `json:"timeoutSeconds,omitempty"`
}

func (b Backups) IsVolumeSnapshotsEnabled() bool {
	return b.VolumeSnapshots != nil && b.VolumeSnapshots.ClassName != ""
}

func (b Backups) IsEnabled() bool {
	return b.Enabled == nil || *b.Enabled
}

func (b Backups) ToCrunchy(version string) crunchyv1beta1.Backups {
	if b.Enabled != nil && !*b.Enabled {
		return crunchyv1beta1.Backups{
			Enabled: new(false),
			PGBackRest: crunchyv1beta1.PGBackRestArchive{
				Image: b.PGBackRest.Image,
			},
		}
	}

	var sc *crunchyv1beta1.PGBackRestSidecars

	sc = b.PGBackRest.Containers

	currVersion, err := gover.NewVersion(version)
	if err == nil && currVersion.LessThan(gover.Must(gover.NewVersion("2.4.0"))) {
		sc = b.PGBackRest.Sidecars
	}

	backups := crunchyv1beta1.Backups{
		Enabled: b.Enabled,
		PGBackRest: crunchyv1beta1.PGBackRestArchive{
			Metadata:      b.PGBackRest.Metadata,
			Configuration: b.PGBackRest.Configuration,
			Global:        b.PGBackRest.Global,
			Image:         b.PGBackRest.Image,
			Jobs:          b.PGBackRest.Jobs,
			Repos:         b.PGBackRest.Repos,
			RepoHost:      b.PGBackRest.RepoHost,
			Manual:        b.PGBackRest.Manual,
			Restore:       b.PGBackRest.Restore,
			InitContainer: b.PGBackRest.InitContainer,
			Sidecars:      sc,
			Env:           b.PGBackRest.Env,
			EnvFrom:       b.PGBackRest.EnvFrom,
		},
	}

	if currVersion != nil && currVersion.GreaterThanOrEqual(gover.Must(gover.NewVersion("2.8.0"))) {
		backups.TrackLatestRestorableTime = b.TrackLatestRestorableTime
	}

	return backups
}

type PGBackRestArchive struct {
	// +optional
	Metadata *crunchyv1beta1.Metadata `json:"metadata,omitempty"`

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

	// +optional
	InitContainer *crunchyv1beta1.InitContainerSpec `json:"initContainer,omitempty"` // K8SPG-613

	// Jobs field allows configuration for all backup jobs
	// +optional
	Jobs *crunchyv1beta1.BackupJobs `json:"jobs,omitempty"`

	// Defines a pgBackRest repository
	// +listType=map
	// +listMapKey=name
	// +optional
	Repos []crunchyv1beta1.PGBackRestRepo `json:"repos"`

	// Defines configuration for a pgBackRest dedicated repository host.  This section is only
	// applicable if at least one "volume" (i.e. PVC-based) repository is defined in the "repos"
	// section, therefore enabling a dedicated repository host Deployment.
	// +optional
	RepoHost *crunchyv1beta1.PGBackRestRepoHost `json:"repoHost,omitempty"`

	// Defines details for manual pgBackRest backup Jobs
	// +optional
	Manual *crunchyv1beta1.PGBackRestManualBackup `json:"manual,omitempty"`

	// Defines details for performing an in-place restore using pgBackRest
	// +optional
	Restore *crunchyv1beta1.PGBackRestRestore `json:"restore,omitempty"`

	// Deprecated: Use Containers instead
	// +optional
	Sidecars *crunchyv1beta1.PGBackRestSidecars `json:"sidecars,omitempty"`

	// Configuration for pgBackRest sidecar containers
	// +optional
	Containers *crunchyv1beta1.PGBackRestSidecars `json:"containers,omitempty"`

	// K8SPG-833
	Env []corev1.EnvVar `json:"env,omitempty"`
	// K8SPG-833
	EnvFrom []corev1.EnvFromSource `json:"envFrom,omitempty"`
}

type PMMQuerySource string

const (
	PgStatStatements PMMQuerySource = "pgstatstatements"
	PgStatMonitor    PMMQuerySource = "pgstatmonitor"
)

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

	// +optional
	CustomClusterName string `json:"customClusterName,omitempty"`

	// +optional
	PostgresParams string `json:"postgresParams,omitempty"`

	// +kubebuilder:validation:Required
	Secret string `json:"secret,omitempty"`

	// +kubebuilder:validation:Enum={pgstatmonitor,pgstatstatements}
	// +kubebuilder:default=pgstatstatements
	// +kubebuilder:validation:Required
	QuerySource PMMQuerySource `json:"querySource,omitempty"`

	// Compute resources of a PMM container.
	// +optional
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`

	// +optional
	ContainerSecurityContext *corev1.SecurityContext `json:"containerSecurityContext,omitempty"`

	// +optional
	RuntimeClassName *string `json:"runtimeClassName,omitempty"`
}

func (cr *PerconaPGCluster) PMMEnabled() bool {
	return cr.Spec.PMM != nil && cr.Spec.PMM.Enabled
}

type LogCollectorSpec struct {
	// Enabled turns the log collector on or off. When unset, it defaults to on
	// for new clusters and off for existing ones.
	// +optional
	Enabled *bool `json:"enabled,omitempty"`

	// +kubebuilder:validation:Required
	Image string `json:"image"`

	// +kubebuilder:validation:Enum={Always,Never,IfNotPresent}
	// +optional
	ImagePullPolicy corev1.PullPolicy `json:"imagePullPolicy,omitempty"`

	// Custom Fluent Bit configuration, merged into the log collector pipeline.
	// Must be in Fluent Bit's YAML configuration format (the classic ".conf"
	// format is not supported); this is what enables YAML-only features such as
	// pipeline processors (e.g. opentelemetry_envelope). Invalid configuration
	// is ignored by the collector at startup.
	// +optional
	Configuration string `json:"configuration,omitempty"`

	// +optional
	Env []corev1.EnvVar `json:"env,omitempty"`

	// +optional
	EnvFrom []corev1.EnvFromSource `json:"envFrom,omitempty"`

	// +optional
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`

	// +optional
	ContainerSecurityContext *corev1.SecurityContext `json:"containerSecurityContext,omitempty"`

	// LivenessProbe sets the liveness probe for the fluent-bit log collector
	// container. When not set, the container has no liveness probe.
	// +optional
	LivenessProbe *corev1.Probe `json:"livenessProbe,omitempty"`

	// ReadinessProbe sets the readiness probe for the fluent-bit log collector
	// container. When not set, the container has no readiness probe.
	// +optional
	ReadinessProbe *corev1.Probe `json:"readinessProbe,omitempty"`

	// +optional
	VolumeMounts []corev1.VolumeMount `json:"volumeMounts,omitempty"`

	// +optional
	Volumes []corev1.Volume `json:"volumes,omitempty"`

	// +optional
	LogRotate *LogRotateSpec `json:"logRotate,omitempty"`
}

type LogRotateSpec struct {
	// Configuration allows overriding the default logrotate configuration.
	// +optional
	Configuration string `json:"configuration,omitempty"`

	// ExtraConfig allows specifying logrotate configuration files in addition to
	// the main configuration file. This should be a reference to a ConfigMap in
	// the same namespace. Keys must contain the .conf extension to be processed
	// correctly.
	// +optional
	ExtraConfig corev1.LocalObjectReference `json:"extraConfig,omitempty"`

	// Schedule is the cron schedule on which logrotate runs.
	// +kubebuilder:default="0 0 * * *"
	// +optional
	Schedule string `json:"schedule,omitempty"`

	// LivenessProbe sets the liveness probe for the logrotate container.
	// When not set, the container has no liveness probe.
	// +optional
	LivenessProbe *corev1.Probe `json:"livenessProbe,omitempty"`

	// ReadinessProbe sets the readiness probe for the logrotate container.
	// When not set, the container has no readiness probe.
	// +optional
	ReadinessProbe *corev1.Probe `json:"readinessProbe,omitempty"`
}

func (cr *PerconaPGCluster) LogCollectorEnabled() bool {
	return cr.Spec.LogCollector != nil &&
		cr.Spec.LogCollector.Enabled != nil &&
		*cr.Spec.LogCollector.Enabled
}

type CustomExtensionSpec struct {
	Name     string `json:"name,omitempty"`
	Version  string `json:"version,omitempty"`
	Checksum string `json:"checksum,omitempty"`
}

type CustomExtensionsStorageSpec struct {
	// +kubebuilder:validation:Enum={s3,gcs,azure}
	Type           string                   `json:"type,omitempty"`
	Bucket         string                   `json:"bucket,omitempty"`
	Region         string                   `json:"region,omitempty"`
	Endpoint       string                   `json:"endpoint,omitempty"`
	ForcePathStyle bool                     `json:"forcePathStyle,omitempty"`
	DisableSSL     bool                     `json:"disableSSL,omitempty"`
	Secret         *corev1.SecretProjection `json:"secret,omitempty"`
}

type BuiltInExtensionsSpec struct {
	PGStatMonitor    *bool `json:"pg_stat_monitor,omitempty"`
	PGStatStatements *bool `json:"pg_stat_statements,omitempty"`
	PGAudit          *bool `json:"pg_audit,omitempty"`
	PGVector         *bool `json:"pgvector,omitempty"`
	PGRepack         *bool `json:"pg_repack,omitempty"`
}

type BuiltInExtensionSpec struct {
	Enabled *bool `json:"enabled,omitempty"`
}

// +kubebuilder:validation:XValidation:rule="!has(oldSelf.pg_tde) || !has(oldSelf.pg_tde.vault) || !has(oldSelf.pg_tde.enabled) || !oldSelf.pg_tde.enabled || has(self.pg_tde.vault)",message="to disable pg_tde first set enabled=false without removing vault and wait for pod restarts"
type ExtensionsSpec struct {
	Image           string                      `json:"image,omitempty"`
	ImagePullPolicy corev1.PullPolicy           `json:"imagePullPolicy,omitempty"`
	Storage         CustomExtensionsStorageSpec `json:"storage,omitempty"`

	// Deprecated: Use extensions.<extension> instead. This field will be removed after 3.4.0.
	BuiltIn BuiltInExtensionsSpec `json:"builtin,omitempty"`

	PGStatMonitor    BuiltInExtensionSpec     `json:"pg_stat_monitor,omitempty"`
	PGStatStatements BuiltInExtensionSpec     `json:"pg_stat_statements,omitempty"`
	PGAudit          BuiltInExtensionSpec     `json:"pg_audit,omitempty"`
	PGVector         BuiltInExtensionSpec     `json:"pgvector,omitempty"`
	PGRepack         BuiltInExtensionSpec     `json:"pg_repack,omitempty"`
	PGCron           BuiltInExtensionSpec     `json:"pg_cron,omitempty"`
	SetUser          BuiltInExtensionSpec     `json:"set_user,omitempty"`
	PGTDE            crunchyv1beta1.PGTDESpec `json:"pg_tde,omitempty"`

	Custom []CustomExtensionSpec `json:"custom,omitempty"`
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

	// The secret containing the root CA certificate and key for
	// secure connections to the PostgreSQL server. It will need to contain the
	// CA TLS certificate and CA TLS key with the data keys set to
	// root.crt and root.key, respectively.
	// +optional
	CustomRootCATLSSecret *corev1.SecretProjection `json:"customRootCATLSSecret,omitempty"`
}

// +listType=map
// +listMapKey=name
// +kubebuilder:validation:MinItems=1
type PGInstanceSets []PGInstanceSetSpec

func (p PGInstanceSets) ToCrunchy() []crunchyv1beta1.PostgresInstanceSetSpec {
	set := make([]crunchyv1beta1.PostgresInstanceSetSpec, len(p))

	for i, inst := range p {
		set[i] = inst.ToCrunchy()
	}

	return set
}

type PGInstanceSetSpec struct { //nolint:recvcheck
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

	SidecarVolumes []corev1.Volume             `json:"sidecarVolumes,omitempty"`
	SidecarPVCs    []crunchyv1beta1.SidecarPVC `json:"sidecarPVCs,omitempty"`

	// K8SPG-440
	// Additional volumes to mount into the PostgreSQL instance container.
	// Changing this value causes PostgreSQL to restart.
	// +optional
	// +listType=map
	// +listMapKey=name
	ExtraVolumes []crunchyv1beta1.ExtraVolume `json:"extraVolumes,omitempty"`

	// Configuration for instance default sidecar containers.
	// +optional
	Containers *crunchyv1beta1.InstanceSidecars `json:"containers,omitempty"`

	// Additional init containers for PostgreSQL instance pods. Changing this value causes
	// PostgreSQL to restart.
	// +optional
	InitContainers []corev1.Container `json:"initContainers,omitempty"`

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

	// The list of tablespaces volumes to mount for this postgrescluster
	// This field requires enabling TablespaceVolumes feature gate
	// +listType=map
	// +listMapKey=name
	// +optional
	TablespaceVolumes []crunchyv1beta1.TablespaceVolume `json:"tablespaceVolumes,omitempty"`

	// The list of volume mounts to mount to PostgreSQL instance pods. Changing this value causes
	// PostgreSQL to restart.
	// +optional
	VolumeMounts []corev1.VolumeMount `json:"volumeMounts,omitempty"`

	// SecurityContext defines the security settings for a PostgreSQL pod.
	// +optional
	SecurityContext *corev1.PodSecurityContext `json:"securityContext,omitempty"`

	// K8SPG-708
	// InitContainer defines the init container for the instance container of a PostgreSQL pod.
	// +optional
	InitContainer *crunchyv1beta1.InitContainerSpec `json:"initContainer,omitempty"`

	Env     []corev1.EnvVar        `json:"env,omitempty"`
	EnvFrom []corev1.EnvFromSource `json:"envFrom,omitempty"`
}

func (p PGInstanceSetSpec) ToCrunchy() crunchyv1beta1.PostgresInstanceSetSpec {
	return crunchyv1beta1.PostgresInstanceSetSpec{
		Metadata:                  p.Metadata,
		Name:                      p.Name,
		Affinity:                  p.Affinity,
		Containers:                p.Sidecars,
		Sidecars:                  p.Containers,
		SidecarVolumes:            p.SidecarVolumes,
		SidecarPVCs:               p.SidecarPVCs,
		ExtraVolumes:              p.ExtraVolumes,
		InitContainers:            p.InitContainers,
		PriorityClassName:         p.PriorityClassName,
		Replicas:                  p.Replicas,
		MinAvailable:              p.MinAvailable,
		Resources:                 p.Resources,
		Tolerations:               p.Tolerations,
		TopologySpreadConstraints: p.TopologySpreadConstraints,
		WALVolumeClaimSpec:        p.WALVolumeClaimSpec,
		DataVolumeClaimSpec:       p.DataVolumeClaimSpec,
		VolumeMounts:              p.VolumeMounts,
		SecurityContext:           p.SecurityContext,
		TablespaceVolumes:         p.TablespaceVolumes,
		InitContainer:             p.InitContainer,
		Env:                       p.Env,
		EnvFrom:                   p.EnvFrom,
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

	// LoadBalancerClass specifies the class of the load balancer implementation
	// to be used. This field is supported for Service Type LoadBalancer only.
	//
	// More info:
	// https://kubernetes.io/docs/concepts/services-networking/service/#load-balancer-class
	// +optional
	LoadBalancerClass *string `json:"loadBalancerClass,omitempty"`

	// LoadBalancerSourceRanges is a list of IP CIDRs allowed access to load.
	// This field will be ignored if the cloud-provider does not support the feature.
	// +optional
	LoadBalancerSourceRanges []string `json:"loadBalancerSourceRanges,omitempty"`

	// ExternalDNS generates the external-dns annotations for this service, so that
	// the external-dns operator publishes a DNS record pointing at it.
	// +optional
	ExternalDNS *ExternalDNSConfig `json:"externalDNS,omitempty"`
}

type ExternalDNSConfig struct {
	// Hostname is the DNS name external-dns publishes for this service.
	// The bounds are RFC 1035: at most 253 characters overall, each label at
	// most 63. Anything longer is rejected by external-dns and by certificate
	// issuance, so it is rejected at admission instead.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?(\.[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?)*$`
	Hostname string `json:"hostname"`

	// TTL in seconds of the published record. No ttl annotation is written when
	// unset or zero, leaving external-dns to use its own default.
	// +optional
	// +kubebuilder:validation:Minimum=0
	TTL int `json:"ttl,omitempty"`
}

// ServiceAnnotations returns the user-provided annotations of this service plus
// the external-dns annotations generated from ExternalDNS. When ExternalDNS is
// set the result is a copy, so callers never mutate the CR.
func (s *ServiceExpose) ServiceAnnotations() map[string]string {
	if s == nil {
		return nil
	}
	if s.ExternalDNS == nil {
		return s.Annotations
	}

	annotations := naming.Merge(s.Annotations)
	annotations[pNaming.AnnotationExternalDNSHostname] = s.ExternalDNS.Hostname
	// externalDNS owns the ttl key the same way it owns the hostname above, so an
	// unset ttl falls back to the external-dns default rather than to a ttl left
	// behind in expose.annotations.
	delete(annotations, pNaming.AnnotationExternalDNSTTL)
	if s.ExternalDNS.TTL > 0 {
		annotations[pNaming.AnnotationExternalDNSTTL] = strconv.Itoa(s.ExternalDNS.TTL)
	}
	annotations[pNaming.AnnotationExternalDNSManaged] = "true"

	return annotations
}

func (s *ServiceExpose) ToCrunchy(version string) *crunchyv1beta1.ServiceSpec {
	if s == nil {
		return nil
	}

	serviceSpec := &crunchyv1beta1.ServiceSpec{
		Metadata: &crunchyv1beta1.Metadata{
			Annotations: s.ServiceAnnotations(),
			Labels:      s.Labels,
		},
		NodePort:                 s.NodePort,
		Type:                     s.Type,
		LoadBalancerSourceRanges: s.LoadBalancerSourceRanges,
	}

	currVersion, err := gover.NewVersion(version)
	if err == nil && currVersion.GreaterThanOrEqual(gover.Must(gover.NewVersion("2.8.0"))) {
		serviceSpec.LoadBalancerClass = s.LoadBalancerClass
	}

	return serviceSpec
}

type PGProxySpec struct {
	// Defines a PgBouncer proxy and connection pooler.
	PGBouncer *PGBouncerSpec `json:"pgBouncer"`
}

func (p *PGProxySpec) IsSet() bool {
	return p != nil && p.PGBouncer != nil
}

func (p *PGProxySpec) PGBouncerEnabled() bool {
	return p.IsSet() && (p.PGBouncer.Replicas == nil || *p.PGBouncer.Replicas != 0)
}

func (p *PGProxySpec) ToCrunchy(version string) *crunchyv1beta1.PostgresProxySpec {
	if p == nil {
		return nil
	}

	return &crunchyv1beta1.PostgresProxySpec{
		PGBouncer: p.PGBouncer.ToCrunchy(version),
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

	SidecarVolumes []corev1.Volume             `json:"sidecarVolumes,omitempty"`
	SidecarPVCs    []crunchyv1beta1.SidecarPVC `json:"sidecarPVCs,omitempty"`

	// Configuration for pgBouncer default sidecar containers.
	// +optional
	Containers *crunchyv1beta1.PGBouncerSidecars `json:"containers,omitempty"`

	// A secret projection containing a certificate and key with which to encrypt
	// connections to PgBouncer. The "tls.crt", "tls.key", and "ca.crt" paths must
	// be PEM-encoded certificates and keys. Changing this value causes PgBouncer
	// to restart.
	// More info: https://kubernetes.io/docs/concepts/configuration/secret/#projection-of-secret-keys-to-specific-paths
	// +optional
	CustomTLSSecret *corev1.SecretProjection `json:"customTLSSecret,omitempty"`

	// K8SPG-952
	// Additional CA bundles that PgBouncer should trust when verifying client certificates.
	// Each item is a reference to a Secret that contains a PEM-encoded CA bundle in key `ca.crt`.
	AdditionalTrustedCAs []corev1.LocalObjectReference `json:"additionalTrustedCAs,omitempty"`

	// Allow SUPERUSERs to connect through PGBouncer.
	// +optional
	ExposeSuperusers bool `json:"exposeSuperusers,omitempty"`

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

	// SecurityContext defines the security settings for PGBouncer pods.
	// +optional
	SecurityContext *corev1.PodSecurityContext `json:"securityContext,omitempty"`

	// Secret with users to add to PgBouncer's authentication file. Each key is
	// a PgBouncer user name and its value is the password or verifier.
	// +optional
	UsersSecret *corev1.LocalObjectReference `json:"usersSecret,omitempty"`

	Env     []corev1.EnvVar        `json:"env,omitempty"`
	EnvFrom []corev1.EnvFromSource `json:"envFrom,omitempty"`

	// If set, pauses pgbouncer connections.
	Paused *bool `json:"paused,omitempty"`
}

func (p *PGBouncerSpec) ToCrunchy(version string) *crunchyv1beta1.PGBouncerPodSpec {
	if p == nil {
		return nil
	}

	spec := &crunchyv1beta1.PGBouncerPodSpec{
		Metadata:                  p.Metadata,
		Affinity:                  p.Affinity,
		Config:                    p.Config,
		Containers:                p.Sidecars,
		SidecarVolumes:            p.SidecarVolumes,
		SidecarPVCs:               p.SidecarPVCs,
		Sidecars:                  p.Containers,
		CustomTLSSecret:           p.CustomTLSSecret,
		ExposeSuperusers:          p.ExposeSuperusers,
		Image:                     p.Image,
		Port:                      p.Port,
		PriorityClassName:         p.PriorityClassName,
		Replicas:                  p.Replicas,
		MinAvailable:              p.MinAvailable,
		Resources:                 p.Resources,
		Service:                   p.ServiceExpose.ToCrunchy(version),
		Tolerations:               p.Tolerations,
		TopologySpreadConstraints: p.TopologySpreadConstraints,
		SecurityContext:           p.SecurityContext,
		UsersSecret:               p.UsersSecret,
		Env:                       p.Env,
		EnvFrom:                   p.EnvFrom,
		AdditionalTrustedCAs:      p.AdditionalTrustedCAs,
		Paused:                    p.Paused,
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

const labelPrefix = "pgv2.percona.com/"

const (
	LabelOperatorVersion = labelPrefix + "version"
	LabelPMMSecret       = labelPrefix + "pmm-secret"
)

// ConditionPMMReady indicates whether the PMM sidecar is configured for the
// cluster. It is False when PMM is enabled but misconfigured (e.g. the secret
// is missing or doesn't contain PMM_SERVER_TOKEN).
const ConditionPMMReady = "PMMReady"

const (
	UserMonitoring = "monitor"

	// UserLogicalReplication is the reserved superuser that pg_createsubscriber
	// connects to the primary as, and that the replica streams as. It only exists
	// while the cluster has at least one logical replica.
	UserLogicalReplication = "logicalrepl"
)

// UserMonitoring constructs the monitoring user.
func (pgc PerconaPGCluster) UserMonitoring() string {
	return pgc.Name + "-" + naming.RolePostgresUser + "-" + UserMonitoring
}

func (cr *PerconaPGCluster) EnvFromSecrets() []string {
	secrets := []string{}

	addSecrets := func(envFrom []corev1.EnvFromSource) {
		for _, v := range envFrom {
			if v.SecretRef == nil {
				continue
			}
			secrets = append(secrets, v.SecretRef.Name)
		}
	}

	for _, set := range cr.Spec.InstanceSets {
		addSecrets(set.EnvFrom)
	}

	addSecrets(cr.Spec.Backups.PGBackRest.EnvFrom)
	if cr.Spec.Backups.PGBackRest.Manual != nil {
		addSecrets(cr.Spec.Backups.PGBackRest.Manual.EnvFrom)
	}
	if cr.Spec.Backups.PGBackRest.Restore != nil {
		addSecrets(cr.Spec.Backups.PGBackRest.Restore.EnvFrom)
	}
	if cr.Spec.Proxy != nil && cr.Spec.Proxy.PGBouncer != nil {
		addSecrets(cr.Spec.Proxy.PGBouncer.EnvFrom)
	}

	return secrets
}

const IndexFieldEnvFromSecrets = "pgCluster.envFromSecrets" //nolint:gosec

var EnvFromSecretsIndexerFunc client.IndexerFunc = func(obj client.Object) []string {
	cr, ok := obj.(*PerconaPGCluster)
	if !ok {
		return nil
	}
	return cr.EnvFromSecrets()
}

func (cr *PerconaPGCluster) PGBouncerUserSecrets() []string {
	if cr.Spec.Proxy == nil || cr.Spec.Proxy.PGBouncer == nil ||
		cr.Spec.Proxy.PGBouncer.UsersSecret == nil || cr.Spec.Proxy.PGBouncer.UsersSecret.Name == "" {
		return nil
	}

	return []string{cr.Spec.Proxy.PGBouncer.UsersSecret.Name}
}

const IndexFieldPGBouncerUserSecrets = "pgCluster.pgBouncerUserSecrets" //nolint:gosec

var PGBouncerUserSecretsIndexerFunc client.IndexerFunc = func(obj client.Object) []string {
	cr, ok := obj.(*PerconaPGCluster)
	if !ok {
		return nil
	}
	return cr.PGBouncerUserSecrets()
}

// LogRotateExtraConfigMaps returns the names of the ConfigMaps the log collector
// references through logRotate.extraConfig.
func (cr *PerconaPGCluster) LogRotateExtraConfigMaps() []string {
	if cr.Spec.LogCollector == nil || cr.Spec.LogCollector.LogRotate == nil ||
		cr.Spec.LogCollector.LogRotate.ExtraConfig.Name == "" {
		return nil
	}
	return []string{cr.Spec.LogCollector.LogRotate.ExtraConfig.Name}
}

const IndexFieldLogRotateExtraConfig = "pgCluster.logRotateExtraConfig"

var LogRotateExtraConfigIndexerFunc client.IndexerFunc = func(obj client.Object) []string {
	cr, ok := obj.(*PerconaPGCluster)
	if !ok {
		return nil
	}
	return cr.LogRotateExtraConfigMaps()
}
