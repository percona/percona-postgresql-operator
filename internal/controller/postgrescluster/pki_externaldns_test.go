// SPDX-License-Identifier: Apache-2.0

package postgrescluster

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"slices"
	"testing"

	"gotest.tools/v3/assert"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/percona/percona-postgresql-operator/v3/internal/naming"
	"github.com/percona/percona-postgresql-operator/v3/internal/testing/require"
	pNaming "github.com/percona/percona-postgresql-operator/v3/percona/naming"
	"github.com/percona/percona-postgresql-operator/v3/pkg/apis/upstream.pgv2.percona.com/v1beta1"
)

// A client reaching the cluster through the hostname published by external-dns
// still verifies the certificate against that name, so the hostnames the
// operator manages have to be part of the server certificate's SANs.
func TestClusterCertificateExternalDNSNames(t *testing.T) {
	_, tClient := setupKubernetes(t)
	require.ParallelCapacity(t, 1)

	ctx := context.Background()
	namespace := setupNamespace(t, tClient).Name

	r := &Reconciler{Client: tClient, Owner: ControllerName}

	primaryService := new(corev1.Service)
	primaryService.Namespace, primaryService.Name = namespace, "the-primary"

	replicaService := new(corev1.Service)
	replicaService.Namespace, replicaService.Name = namespace, "the-replicas"

	managed := func(hostname string) *v1beta1.ServiceSpec {
		return &v1beta1.ServiceSpec{
			Type: "ClusterIP",
			Metadata: &v1beta1.Metadata{Annotations: map[string]string{
				pNaming.AnnotationExternalDNSHostname: hostname,
				pNaming.AnnotationExternalDNSManaged:  "true",
			}},
		}
	}

	// leafDNSNames reconciles the cluster certificate and returns the SANs of
	// the certificate that ends up in the cluster's TLS secret.
	leafDNSNames := func(t *testing.T, cluster *v1beta1.PostgresCluster) []string {
		t.Helper()

		root, err := r.reconcileRootCertificate(ctx, cluster)
		assert.NilError(t, err)

		_, err = r.reconcileInternalClusterCertificate(ctx, root, cluster, primaryService, replicaService)
		assert.NilError(t, err)

		secret := &corev1.Secret{ObjectMeta: naming.PostgresTLSSecret(cluster)}
		assert.NilError(t, tClient.Get(ctx, client.ObjectKeyFromObject(secret), secret))

		block, _ := pem.Decode(secret.Data["tls.crt"])
		assert.Assert(t, block != nil, "no PEM block in tls.crt")

		parsed, err := x509.ParseCertificate(block.Bytes)
		assert.NilError(t, err)

		return parsed.DNSNames
	}

	t.Run("managed hostnames become SANs", func(t *testing.T) {
		cluster := testCluster()
		cluster.Name, cluster.Namespace = "external-dns-managed", namespace
		cluster.Spec.Service = managed("pg.example.com")
		cluster.Spec.ReplicaService = managed("pg-replicas.example.com")
		assert.NilError(t, tClient.Create(ctx, cluster))

		names := leafDNSNames(t, cluster)

		// The in-cluster FQDN stays first so it remains the common name.
		assert.Equal(t, names[0], "the-primary."+namespace+".svc.cluster.local")
		assert.Assert(t, slices.Contains(names, "pg.example.com"), "got %v", names)
		assert.Assert(t, slices.Contains(names, "pg-replicas.example.com"), "got %v", names)
	})

	t.Run("hostnames written by hand are not SANs", func(t *testing.T) {
		cluster := testCluster()
		cluster.Name, cluster.Namespace = "external-dns-manual", namespace
		// No ownership marker: the user put this in expose.annotations, so the
		// operator must not reissue their certificate because of it.
		cluster.Spec.Service = &v1beta1.ServiceSpec{
			Type: "ClusterIP",
			Metadata: &v1beta1.Metadata{Annotations: map[string]string{
				pNaming.AnnotationExternalDNSHostname: "manual.example.com",
			}},
		}
		assert.NilError(t, tClient.Create(ctx, cluster))

		names := leafDNSNames(t, cluster)
		assert.Assert(t, !slices.Contains(names, "manual.example.com"), "got %v", names)
	})
}
