// Copyright 2021 - 2024 Crunchy Data Solutions, Inc.
//
// SPDX-License-Identifier: Apache-2.0

// Package dcs owns everything specific to the distributed configuration
// store (DCS) backend Patroni uses. Generic Patroni logic lives in
// internal/patroni; DCS-specific behavior belongs here.
package dcs

import (
	"context"
	"time"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/percona/percona-postgresql-operator/v3/pkg/apis/upstream.pgv2.percona.com/v1beta1"
)

// Backend describes the behavior a Patroni DCS backend must provide.
// Implementations are stateless; any dependency they need (a client, an
// executor, an event recorder) is passed as a parameter.
type Backend interface {
	// --- Patroni configuration additions ---

	// ClusterYAML returns top-level Patroni config keys owned by this
	// backend (e.g. "kubernetes"), merged into the cluster-wide config.
	ClusterYAML(cluster *v1beta1.PostgresCluster) map[string]any

	// InstanceYAML returns top-level Patroni config keys owned by this
	// backend, merged into each instance's config.
	InstanceYAML(cluster *v1beta1.PostgresCluster) map[string]any

	// --- Pod additions ---

	// InstanceEnvVars returns backend-specific environment variables for
	// Patroni's instance container.
	InstanceEnvVars(cluster *v1beta1.PostgresCluster,
		leaderService *corev1.Service, podContainers []corev1.Container) []corev1.EnvVar

	// --- RBAC ---

	// Permissions returns backend-specific RBAC rules for Patroni's
	// ServiceAccount, in addition to the generic rules patroni.Permissions
	// always grants.
	Permissions(cluster *v1beta1.PostgresCluster) []rbacv1.PolicyRule

	// --- Kubernetes objects this backend owns for its own bookkeeping ---

	// DistributedConfigurationService returns the Service this backend needs
	// to protect its distributed-configuration objects, or nil when it owns
	// no such object.
	DistributedConfigurationService(cluster *v1beta1.PostgresCluster) *corev1.Service

	// LeaderLeaseService returns the Service that resolves to the elected
	// Patroni leader, or nil when this backend owns no such object.
	LeaderLeaseService(cluster *v1beta1.PostgresCluster,
		recorder record.EventRecorder) (*corev1.Service, error)

	// PrimaryService returns the ServiceSpec and, if this backend needs the
	// operator to manage them itself, EndpointSubset that route traffic to
	// cluster's current PostgreSQL primary. leaderService is this backend's
	// own LeaderLeaseService result (nil if it has none, or hasn't been
	// created yet). endpointSubset is nil when the ServiceSpec's Selector
	// already routes traffic on its own (e.g. a future backend using pod
	// labels), in which case the operator manages no Endpoints for this
	// Service.
	PrimaryService(cluster *v1beta1.PostgresCluster, leaderService *corev1.Service) (
		spec corev1.ServiceSpec, endpointSubset *corev1.EndpointSubset, err error)

	// --- Runtime observation ---

	// Observe reports what this backend can tell us about Patroni's runtime
	// state this reconcile. readyInstance tells the backend whether any
	// instance is currently Ready, since "not bootstrapped yet" vs.
	// "bootstrapped but our signal is delayed" needs different requeue
	// handling and is backend-specific policy.
	Observe(ctx context.Context, cli client.Client, cluster *v1beta1.PostgresCluster,
		readyInstance bool) (Observation, error)

	// Delete removes any backend-owned objects during cluster teardown.
	Delete(ctx context.Context, cli client.Client, cluster *v1beta1.PostgresCluster) error
}

// Observation is what a backend learned about Patroni's runtime state on a
// single reconcile pass.
type Observation struct {
	// SystemIdentifier is "" when not yet known.
	SystemIdentifier string

	// RequeueAfter is 0 when no explicit requeue is needed.
	RequeueAfter time.Duration
}

// For selects the DCS backend for cluster. Only Kubernetes Endpoints is
// implemented today; a future backend (e.g. Kubernetes ConfigMaps, etcd)
// adds a case here.
func For(cluster *v1beta1.PostgresCluster) Backend {
	return kubernetesEndpointsBackend{}
}
