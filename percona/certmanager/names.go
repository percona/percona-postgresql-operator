// Copyright 2024 Percona LLC
//
// SPDX-License-Identifier: Apache-2.0

package certmanager

import (
	"github.com/percona/percona-postgresql-operator/v3/internal/naming"
	"github.com/percona/percona-postgresql-operator/v3/pkg/apis/upstream.pgv2.percona.com/v1beta1"
)

// Names of the cert-manager Certificate CRs that this controller manages.
// These are the single source of truth — both the Apply* methods that create
// the Certificates and the callers that look them up (e.g. the K8SPG-1007
// recovery branches) must use these helpers so the two stay in sync.

// ClusterCertificateName returns the name of the cert-manager Certificate CR
// that issues the cluster TLS leaf. The Certificate and its issued Secret
// share a name.
func ClusterCertificateName(cluster *v1beta1.PostgresCluster) string {
	return naming.PostgresTLSSecret(cluster).Name
}

// InstanceCertificateName returns the name of the cert-manager Certificate CR
// that issues an instance's leaf. instanceName is the instance's StatefulSet
// name.
func InstanceCertificateName(instanceName string) string {
	return instanceName + "-cert"
}

// ReplicationCertificateName returns the name of the cert-manager Certificate
// CR that issues the Patroni replication client leaf. The Certificate and its
// issued Secret share a name.
func ReplicationCertificateName(cluster *v1beta1.PostgresCluster) string {
	return naming.ReplicationClientCertSecret(cluster).Name
}

// PGBouncerCertificateName returns the name of the cert-manager Certificate CR
// that issues the PgBouncer frontend leaf.
func PGBouncerCertificateName(cluster *v1beta1.PostgresCluster) string {
	return cluster.Name + "-pgbouncer-cert"
}

// PGBackRestClientCertificateName returns the name of the cert-manager
// Certificate CR that issues the pgBackRest client leaf.
func PGBackRestClientCertificateName(cluster *v1beta1.PostgresCluster) string {
	return cluster.Name + "-pgbackrest-client-cert"
}

// PGBackRestRepoCertificateName returns the name of the cert-manager
// Certificate CR that issues the pgBackRest repo host leaf.
func PGBackRestRepoCertificateName(cluster *v1beta1.PostgresCluster) string {
	return cluster.Name + "-pgbackrest-repo-cert"
}
