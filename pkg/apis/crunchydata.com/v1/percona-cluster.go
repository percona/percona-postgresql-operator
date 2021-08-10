package v1

import (
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// PerconaPGCluster is the CRD that defines a Percona PG Cluster
//
// swagger:ignore Pgcluster
// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type PerconaPGCluster struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`
	Spec              PerconaPGClusterSpec   `json:"spec"`
	Status            PerconaPGClusterStatus `json:"status,omitempty"`
}

// PerconaPGClusterSpec is the CRD that defines a Percona PG Cluster Spec
// swagger:ignore
type PerconaPGClusterSpec struct {
	Namespace          string                 `json:"namespace"`
	Database           string                 `json:"database"`
	User               string                 `json:"user"`
	Port               string                 `json:"port"`
	UserLabels         map[string]string      `json:"userLabels"`
	WalStorage         PVCStorage             `json:"walStorage"`
	TablespaceStorages map[string]PVCStorage  `json:"tablespaceStorages"`
	Pause              bool                   `json:"pause"`
	Standby            bool                   `json:"standby"`
	TlSOnly            bool                   `json:"tlsOnly"`
	DisableAutofail    bool                   `json:"disableAutofail"`
	KeepData           bool                   `json:"keepData"`
	KeepBackups        bool                   `json:"keepBackups"`
	PGPrimary          PGPrimary              `json:"pgPrimary"`
	PGReplicas         *PGReplicas            `json:"pgReplicas"`
	PGBadger           Badger                 `json:"pgBadger"`
	PGBouncer          PgBouncer              `json:"pgBouncer"`
	PGDataSource       PGDataSourceSpec       `json:"pgDataSource"`
	PMM                PMMSpec                `json:"pmm"`
	Backup             Backup                 `json:"backup"`
	SecurityContext    *v1.PodSecurityContext `json:"securityContext"`
}

type PerconaPGClusterStatus struct {
	PGCluster  PgclusterStatus
	PGReplicas map[string]PgreplicaStatus
}

type PVCStorage struct {
	VolumeSpec PgStorageSpec `json:"volumeSpec"`
}

type Badger struct {
	Enabled bool   `json:"enabled"`
	Image   string `json:"image"`
	Port    int    `json:"port"`
}

type PgBouncer struct {
	Image            string              `json:"image"`
	Size             int32               `json:"size"`
	Resources        Resources           `json:"resources"`
	TLSSecret        string              `json:"tlsSecret"`
	Expose           Expose              `json:"expose"`
	AntiAffinityType PodAntiAffinityType `json:"antiAffinityType"`
}

type PGDataSource struct {
	Namespace   string `json:"namespace"`
	RestoreFrom string `json:"restoreFrom"`
	RestoreOpts string `json:"restoreOpts"`
}

type PGPrimary struct {
	Image              string              `json:"image"`
	Customconfig       string              `json:"customconfig"`
	Resources          Resources           `json:"resources"`
	VolumeSpec         *PgStorageSpec      `json:"volumeSpec"`
	Labels             map[string]string   `json:"labels"`
	Annotations        map[string]string   `json:"annotations"`
	Affinity           v1.Affinity         `json:"affinity"`
	AntiAffinityType   PodAntiAffinityType `json:"antiAffinityType"`
	ImagePullPolicy    string              `json:"imagePullPolicy"`
	Tolerations        []v1.Toleration     `json:"tolerations"`
	NodeSelector       string              `json:"nodeSelector"`
	RuntimeClassName   string              `json:"runtimeClassName"`
	PodSecurityContext string              `json:"podSecurityContext"`
	Expose             Expose              `json:"expose"`
}

type PGReplicas struct {
	HotStandby HotStandby `json:"hotStandby"`
}

type HotStandby struct {
	Size              int               `json:"size"`
	Resources         *Resources        `json:"resources"`
	VolumeSpec        *PgStorageSpec    `json:"volumeSpec"`
	Labels            map[string]string `json:"labels"`
	Annotations       map[string]string `json:"annotations"`
	Affinity          *v1.Affinity      `json:"affinity"`
	EnableSyncStandby bool              `json:"enableSyncStandby"`
	Expose            Expose            `json:"expose"`
}
type Expose struct {
	ServiceType              v1.ServiceType    `json:"serviceType"`
	LoadBalancerSourceRanges []string          `json:"loadBalancerSourceRanges"`
	Annotations              map[string]string `json:"annotations"`
	Labels                   map[string]string `json:"labels"`
}

type Resources struct {
	Requests v1.ResourceList `json:"requests"`
	Limits   v1.ResourceList `json:"limits"`
}

type Backup struct {
	Image             string                `json:"image"`
	BackrestRepoImage string                `json:"backrestRepoImage"`
	ServiceAccount    string                `json:"serviceAccount"`
	Resources         Resources             `json:"resources"`
	VolumeSpec        *PgStorageSpec        `json:"volumeSpec"`
	Storages          map[string]Storage    `json:"storages"`
	Schedule          []CronJob             `json:"schedule"`
	StorageTypes      []BackrestStorageType `json:"storageTypes"`
	AntiAffinityType  PodAntiAffinityType   `json:"antiAffinityType"`
	RepoPath          string                `json:"repoPath"`
}

type StorageType string

type Storage struct {
	Type        StorageType `json:"type"`
	Bucket      string      `json:"bucket"`
	Region      string      `json:"region"`
	EndpointURL string      `json:"endpointUrl"`
	KeyType     string      `json:"keyType"`
	URIStyle    string      `json:"uriStyle"`
	VerifyTLS   bool        `json:"verifyTLS"`
}

type CronJob struct {
	Name     string `json:"name"`
	Schedule string `json:"schedule"`
	Keep     int64  `json:"keep"`
	Type     string `json:"type"`
	Storage  string `json:"storage"`
}

// PMMSpec contains settings for PMM
type PMMSpec struct {
	Enabled    bool      `json:"enabled"`
	Image      string    `json:"image"`
	ServerHost string    `json:"serverHost,omitempty"`
	ServerUser string    `json:"serverUser,omitempty"`
	PMMSecret  string    `json:"pmmSecret,omitempty"`
	Resources  Resources `json:"resources"`
}

// PerconaPGClusterList is the CRD that defines a Percona PG Cluster List
// swagger:ignore
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type PerconaPGClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []PerconaPGCluster `json:"items"`
}
