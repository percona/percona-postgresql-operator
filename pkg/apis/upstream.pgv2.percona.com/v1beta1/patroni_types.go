// Copyright 2021 - 2024 Crunchy Data Solutions, Inc.
//
// SPDX-License-Identifier: Apache-2.0

package v1beta1

// +kubebuilder:validation:XValidation:rule="(has(oldSelf.dcs) ? oldSelf.dcs.type : 'kubernetes') == (has(self.dcs) ? self.dcs.type : 'kubernetes')",message="DCS type is immutable after cluster creation"
type PatroniSpec struct {
	// Patroni dynamic configuration settings. Changes to this value will be
	// automatically reloaded without validation. Changes to certain PostgreSQL
	// parameters cause PostgreSQL to restart.
	// More info: https://patroni.readthedocs.io/en/latest/dynamic_configuration.html
	// +optional
	// +kubebuilder:pruning:PreserveUnknownFields
	// +kubebuilder:validation:Schemaless
	// +kubebuilder:validation:Type=object
	DynamicConfiguration SchemalessObject `json:"dynamicConfiguration,omitempty"`

	// TTL of the cluster leader lock. "Think of it as the
	// length of time before initiation of the automatic failover process."
	// Changing this value causes PostgreSQL to restart.
	// +optional
	// +kubebuilder:default=30
	// +kubebuilder:validation:Minimum=3
	LeaderLeaseDurationSeconds *int32 `json:"leaderLeaseDurationSeconds,omitempty"`

	// The port on which Patroni should listen.
	// Changing this value causes PostgreSQL to restart.
	// +optional
	// +kubebuilder:default=8008
	// +kubebuilder:validation:Minimum=1024
	Port *int32 `json:"port,omitempty"`

	// The interval for refreshing the leader lock and applying
	// dynamicConfiguration. Must be less than leaderLeaseDurationSeconds.
	// Changing this value causes PostgreSQL to restart.
	// +optional
	// +kubebuilder:default=10
	// +kubebuilder:validation:Minimum=1
	SyncPeriodSeconds *int32 `json:"syncPeriodSeconds,omitempty"`

	// Switchover gives options to perform ad hoc switchovers in a PostgresCluster.
	// +optional
	Switchover *PatroniSwitchover `json:"switchover,omitempty"`

	// CreateReplicaMethods allows overriding create_replica_methods for all instances.
	// +optional
	CreateReplicaMethods []CreateReplicaMethod `json:"createReplicaMethods,omitempty"`

	// DCS configures the distributed configuration store backend.
	// Defaults to the Kubernetes-native backend (Endpoints).
	// N.B. Changing the DCS type causes downtime; all instances must restart simultaneously.
	// +optional
	DCS *PatroniDCS `json:"dcs,omitempty"`

	// RemoveDataDirectoryOnDivergedTimelines allows controlling remove_data_directory_on_diverged_timelines in Patroni cluster config.
	// +optional
	RemoveDataDirectoryOnDivergedTimelines bool `json:"removeDataDirectoryOnDivergedTimelines,omitempty"`
}

// GetDCS returns the DCS configuration, or nil if unset.
func (s *PatroniSpec) GetDCS() *PatroniDCS {
	if s == nil {
		return nil
	}
	return s.DCS
}

// PatroniDCSType identifies which DCS backend Patroni should use.
// +kubebuilder:validation:Enum={kubernetes,etcd}
type PatroniDCSType string

const (
	PatroniDCSTypeKubernetes PatroniDCSType = "kubernetes"
	PatroniDCSTypeEtcd       PatroniDCSType = "etcd"
)

// PatroniDCS configures the Patroni distributed configuration store (DCS).
// +kubebuilder:validation:XValidation:rule="self.type != 'etcd' || (has(self.etcd) && size(self.etcd.endpoints) > 0)",message="etcd.endpoints must be non-empty when type is etcd"
// +kubebuilder:validation:XValidation:rule="!has(oldSelf.type) || oldSelf.type == self.type",message="DCS type is immutable after cluster creation"
type PatroniDCS struct {
	// Type of DCS backend. Defaults to "kubernetes".
	// Changing this value causes cluster downtime; all instances must restart.
	// This field is immutable after cluster creation.
	// +optional
	// +kubebuilder:default=kubernetes
	Type PatroniDCSType `json:"type,omitempty"`

	// Etcd holds settings for the external etcd DCS backend.
	// Required when type is "etcd".
	// +optional
	Etcd *PatroniEtcdSpec `json:"etcd,omitempty"`
}

// PatroniEtcdSpec defines connectivity to an external etcd cluster used as DCS.
// +kubebuilder:validation:XValidation:rule="self.endpoints.all(e, e.startsWith('https://')) || self.endpoints.all(e, e.startsWith('http://'))",message="all endpoints must use the same scheme (http or https)"
type PatroniEtcdSpec struct {
	// Endpoints is the list of etcd endpoints including scheme and port.
	// Example: ["https://etcd.etcd-cluster.svc:2379"]
	// The scheme of the first endpoint determines the protocol used.
	// All endpoints must use the same scheme.
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=7
	// +kubebuilder:validation:items:Pattern=`^https?://[^/]`
	Endpoints []string `json:"endpoints"`

	// TLSSecret is the name of a Secret in the same namespace with keys
	// ca.crt, tls.crt, and tls.key for mutual TLS with etcd.
	// +optional
	TLSSecret string `json:"tlsSecret,omitempty"`

	// AuthSecret is the name of a Secret in the same namespace with keys
	// username and password for etcd authentication.
	// +optional
	AuthSecret string `json:"authSecret,omitempty"`
}

// +kubebuilder:validation:Enum={basebackup,pgbackrest}
type CreateReplicaMethod string

type PatroniSwitchover struct {

	// Whether or not the operator should allow switchovers in a PostgresCluster
	// +required
	// +kubebuilder:default=false
	Enabled bool `json:"enabled"`

	// The instance that should become primary during a switchover. This field is
	// optional when Type is "Switchover" and required when Type is "Failover".
	// When it is not specified, a healthy replica is automatically selected.
	// +optional
	TargetInstance *string `json:"targetInstance,omitempty"`

	// Type of switchover to perform. Valid options are Switchover and Failover.
	// "Switchover" changes the primary instance of a healthy PostgresCluster.
	// "Failover" forces a particular instance to be primary, regardless of other
	// factors. A TargetInstance must be specified to failover.
	// NOTE: The Failover type is reserved as the "last resort" case.
	// +kubebuilder:validation:Enum={Switchover,Failover}
	// +kubebuilder:default:=Switchover
	// +optional
	Type string `json:"type,omitempty"`
}

// PatroniSwitchover types.
const (
	PatroniSwitchoverTypeFailover   = "Failover"
	PatroniSwitchoverTypeSwitchover = "Switchover"

	// K8SPG-718
	patroniDefaultPort = int32(8008)
)

// Default sets the default values for certain Patroni configuration attributes,
// including:
// - Lock Lease Duration
// - Patroni's API port
// - Frequency of syncing with Kube API
func (s *PatroniSpec) Default() {
	if s.LeaderLeaseDurationSeconds == nil {
		s.LeaderLeaseDurationSeconds = new(int32)
		*s.LeaderLeaseDurationSeconds = 30
	}
	if s.Port == nil {
		s.Port = new(int32)
		*s.Port = 8008
	}
	if s.SyncPeriodSeconds == nil {
		s.SyncPeriodSeconds = new(int32)
		*s.SyncPeriodSeconds = 10
	}
}

// GetPort returns the patroni port. Added as part of K8SPG-718.
func (s *PatroniSpec) GetPort() int32 {
	patroniPort := patroniDefaultPort
	if s != nil && s.Port != nil {
		patroniPort = *s.Port
	}
	return patroniPort
}

type PatroniStatus struct {

	// - "database_system_identifier" of https://github.com/zalando/patroni/blob/v2.0.1/docs/rest_api.rst#monitoring-endpoint
	// - "system_identifier" of https://www.postgresql.org/docs/current/functions-info.html#FUNCTIONS-PG-CONTROL-SYSTEM
	// - "systemid" of https://www.postgresql.org/docs/current/protocol-replication.html

	// The PostgreSQL system identifier reported by Patroni.
	// +optional
	SystemIdentifier string `json:"systemIdentifier,omitempty"`

	// Tracks the execution of the switchover requests.
	// +optional
	Switchover *string `json:"switchover,omitempty"`

	// Tracks the current timeline during switchovers
	// +optional
	SwitchoverTimeline *int64 `json:"switchoverTimeline,omitempty"`
}
