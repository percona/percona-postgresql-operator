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
	Namespace          string               `json:"namespace"`
	Database           string               `json:"database"`
	User               string               `json:"user"`
	Port               string               `json:"port"`
	UserLabels         map[string]string    `json:"userLabels"`
	WalStorage         PerconaPgStorageSpec `json:"walStorage"`
	TablespaceStorages TablespaceStorages   `json:"tablespaceStorages"`
	Pause              bool                 `json:"pause"`
	Standby            bool                 `json:"standby"`
	TlSOnly            bool                 `json:"tlsOnly"`
	DisableAutofail    bool                 `json:"disableAutofail"`
	KeepData           bool                 `json:"keepData"`
	PGPrimary          PGPrimary            `json:"pgPrimary"`
	PGReplicas         *PGReplicas          `json:"pgReplicas"`
	Badger             Badger               `json:"badger"`
	PGBouncer          PgBouncer            `json:"pgBouncer"`
	PGDataSource       PGDataSource         `json:"pgDataSource"`
	PMM                PMMSpec              `json:"pmm"`
	Backup             Backup               `json:"backup"`
}

type PerconaPGClusterStatus struct {
	PGCluster  PgclusterStatus
	PGReplicas map[string]PgreplicaStatus
}

type TablespaceStorages struct {
	Lake  PerconaPgStorageSpec `json:"lake"`
	Ocean PerconaPgStorageSpec `json:"ocean"`
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
	Image              string               `json:"image"`
	Customconfig       string               `json:"customconfig"`
	Resources          Resources            `json:"resources"`
	VolumeSpec         PerconaPgStorageSpec `json:"volumeSpec"`
	Labels             map[string]string    `json:"labels"`
	Annotations        map[string]string    `json:"annotations"`
	Affinity           v1.Affinity          `json:"affinity"`
	AntiAffinityType   PodAntiAffinityType  `json:"antiAffinityType"`
	ImagePullPolicy    string               `json:"imagePullPolicy"`
	Tolerations        []v1.Toleration      `json:"tolerations"`
	NodeSelector       string               `json:"nodeSelector"`
	RuntimeClassName   string               `json:"runtimeClassName"`
	PodSecurityContext string               `json:"podSecurityContext"`
	Expose             Expose               `json:"expose"`
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
}

type Storage struct {
	Type        string `json:"type"`
	Bucket      string `json:"bucket"`
	Region      string `json:"region"`
	EndpointURL string `json:"endpointUrl"`
	KeyType     string `json:"keyType"`
	URIStyle    string `json:"uriStyle"`
	VerifyTLS   bool   `json:"verifyTLS"`
}

type CronJob struct {
	Name     string `json:"name"`
	Schedule string `json:"schedule"`
	Keep     int64  `json:"keep"`
	Type     string `json:"type"`
	Storage  string `json:"storage"`
}

// PerconaPgStorageSpec ...
type PerconaPgStorageSpec struct {
	Name               string `json:"name"`
	StorageClass       string `json:"storageclass"`
	AccessMode         string `json:"accessmode"`
	Size               string `json:"size"`
	StorageType        string `json:"storagetype"`
	SupplementalGroups string `json:"supplementalgroups"`
	MatchLabels        string `json:"matchLabels"`
}

// PMMSpec contains settings for PMM
type PMMSpec struct {
	Enabled    bool            `json:"enabled"`
	Image      string          `json:"image"`
	ServerHost string          `json:"serverHost,omitempty"`
	ServerUser string          `json:"serverUser,omitempty"`
	PMMSecret  string          `json:"pmmSecret,omitempty"`
	Resources  v1.ResourceList `json:"resources"`
	Limits     v1.ResourceList `json:"limits"`
}

// PerconaPGClusterList is the CRD that defines a Percona PG Cluster List
// swagger:ignore
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type PerconaPGClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []PerconaPGCluster `json:"items"`
}
