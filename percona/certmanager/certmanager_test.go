package certmanager

import (
	"context"
	"fmt"
	"testing"
	"time"

	v1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	cmmeta "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	"github.com/cert-manager/cert-manager/pkg/util/cmapichecker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/rest"
	sigs "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/percona/percona-postgresql-operator/v2/internal/naming"
	"github.com/percona/percona-postgresql-operator/v2/pkg/apis/upstream.pgv2.percona.com/v1beta1"
)

func setupFakeClient(t *testing.T, objs ...sigs.Object) sigs.Client {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, v1beta1.AddToScheme(scheme))
	require.NoError(t, v1.AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))

	return fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()
}

func testCluster() *v1beta1.PostgresCluster {
	return &v1beta1.PostgresCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster",
			Namespace: "test-namespace",
			Labels: map[string]string{
				naming.LabelVersion: "2.5.0",
			},
			UID: "test-uid",
		},
		Spec: v1beta1.PostgresClusterSpec{},
	}
}

func TestNewController(t *testing.T) {
	client := setupFakeClient(t)
	scheme := runtime.NewScheme()

	t.Run("create controller without dry run", func(t *testing.T) {
		ctrl := NewController(client, scheme, false)
		require.NotNil(t, ctrl)

		c, ok := ctrl.(*controller)
		require.True(t, ok)
		assert.Equal(t, client, c.cl)
		assert.Equal(t, scheme, c.scheme)
		assert.False(t, c.dryRun)
	})

	t.Run("create controller with dry run", func(t *testing.T) {
		ctrl := NewController(client, scheme, true)
		require.NotNil(t, ctrl)

		c, ok := ctrl.(*controller)
		require.True(t, ok)
		assert.True(t, c.dryRun)
		assert.NotEqual(t, client, c.cl)
	})
}

type mockChecker struct {
	err error
}

func (m *mockChecker) Check(_ context.Context) error {
	return m.err
}

func TestCheck(t *testing.T) {
	t.Run("returns nil when checker succeeds", func(t *testing.T) {
		cl := setupFakeClient(t)
		ctrl := NewController(cl, cl.Scheme(), false).(*controller)
		ctrl.newChecker = func(_ *rest.Config, _ string) (cmapichecker.Interface, error) {
			return &mockChecker{err: nil}, nil
		}

		err := ctrl.Check(t.Context(), &rest.Config{}, "default")
		assert.NoError(t, err)
	})

	t.Run("returns error when newChecker fails", func(t *testing.T) {
		cl := setupFakeClient(t)
		ctrl := NewController(cl, cl.Scheme(), false).(*controller)
		ctrl.newChecker = func(_ *rest.Config, _ string) (cmapichecker.Interface, error) {
			return nil, fmt.Errorf("failed to create checker")
		}

		err := ctrl.Check(t.Context(), &rest.Config{}, "default")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to create checker")
	})

	t.Run("returns ErrCertManagerNotFound when CRDs not found", func(t *testing.T) {
		cl := setupFakeClient(t)
		ctrl := NewController(cl, cl.Scheme(), false).(*controller)
		ctrl.newChecker = func(_ *rest.Config, _ string) (cmapichecker.Interface, error) {
			return &mockChecker{
				err: fmt.Errorf(`no matches for kind "CertificateRequest" in group "cert-manager.io"`),
			}, nil
		}

		err := ctrl.Check(t.Context(), &rest.Config{}, "default")
		assert.ErrorIs(t, err, ErrCertManagerNotFound)
	})

	t.Run("returns ErrCertManagerNotFound for custom CRDs mapping error", func(t *testing.T) {
		cl := setupFakeClient(t)
		ctrl := NewController(cl, cl.Scheme(), false).(*controller)
		ctrl.newChecker = func(_ *rest.Config, _ string) (cmapichecker.Interface, error) {
			return &mockChecker{
				err: fmt.Errorf("error finding the scope of the object: failed to get restmapping: unable to retrieve the complete list of server APIs: cert-manager.io/v1: no matches for cert-manager.io/v1, Resource="),
			}, nil
		}

		err := ctrl.Check(t.Context(), &rest.Config{}, "default")
		assert.ErrorIs(t, err, ErrCertManagerNotFound)
	})

	t.Run("returns ErrCertManagerNotReady for webhook service failure", func(t *testing.T) {
		cl := setupFakeClient(t)
		ctrl := NewController(cl, cl.Scheme(), false).(*controller)
		ctrl.newChecker = func(_ *rest.Config, _ string) (cmapichecker.Interface, error) {
			return &mockChecker{
				err: fmt.Errorf(`Post "https://cert-manager-webhook.cert-manager.svc:443/mutate?timeout=10s": service "cert-manager-webhook" not found`),
			}, nil
		}

		err := ctrl.Check(t.Context(), &rest.Config{}, "default")
		assert.ErrorIs(t, err, ErrCertManagerNotReady)
	})

	t.Run("returns ErrCertManagerNotReady for webhook deployment failure", func(t *testing.T) {
		cl := setupFakeClient(t)
		ctrl := NewController(cl, cl.Scheme(), false).(*controller)
		ctrl.newChecker = func(_ *rest.Config, _ string) (cmapichecker.Interface, error) {
			return &mockChecker{
				err: fmt.Errorf(`Post "https://cert-manager-webhook.cert-manager.svc:443/mutate?timeout=10s": dial tcp 10.96.38.90:443: connect: connection refused`),
			}, nil
		}

		err := ctrl.Check(t.Context(), &rest.Config{}, "default")
		assert.ErrorIs(t, err, ErrCertManagerNotReady)
	})

	t.Run("returns ErrCertManagerNotReady for webhook certificate failure", func(t *testing.T) {
		cl := setupFakeClient(t)
		ctrl := NewController(cl, cl.Scheme(), false).(*controller)
		ctrl.newChecker = func(_ *rest.Config, _ string) (cmapichecker.Interface, error) {
			return &mockChecker{
				err: fmt.Errorf(`Post "https://cert-manager-webhook.cert-manager.svc:443/mutate?timeout=10s": x509: certificate signed by unknown authority`),
			}, nil
		}

		err := ctrl.Check(t.Context(), &rest.Config{}, "default")
		assert.ErrorIs(t, err, ErrCertManagerNotReady)
	})

	t.Run("returns original error for unhandled checker errors", func(t *testing.T) {
		cl := setupFakeClient(t)
		ctrl := NewController(cl, cl.Scheme(), false).(*controller)
		ctrl.newChecker = func(_ *rest.Config, _ string) (cmapichecker.Interface, error) {
			return &mockChecker{
				err: fmt.Errorf("some unexpected error"),
			}, nil
		}

		err := ctrl.Check(t.Context(), &rest.Config{}, "default")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "some unexpected error")
	})
}

func TestApplyIssuer(t *testing.T) {
	cluster := testCluster()
	client := setupFakeClient(t, cluster)

	ctrl := NewController(client, client.Scheme(), false)

	t.Run("create TLS issuer in cluster namespace", func(t *testing.T) {
		err := ctrl.ApplyIssuer(t.Context(), cluster)
		require.NoError(t, err)

		issuer := &v1.Issuer{}
		meta := naming.TLSIssuer(cluster)
		err = client.Get(t.Context(), sigs.ObjectKey{
			Namespace: meta.Namespace,
			Name:      meta.Name,
		}, issuer)
		require.NoError(t, err)

		assert.Equal(t, meta.Name, issuer.Name)
		assert.Equal(t, cluster.Namespace, issuer.Namespace)
		assert.NotNil(t, issuer.Spec.CA)
		assert.Equal(t, naming.PostgresRootCASecret(cluster).Name, issuer.Spec.CA.SecretName)

		require.Len(t, issuer.OwnerReferences, 1)
		assert.Equal(t, cluster.Name, issuer.OwnerReferences[0].Name)

		// return nil when issuer already exists
		err = ctrl.ApplyIssuer(t.Context(), cluster)
		require.NoError(t, err)
	})

	t.Run("skip when external issuer", func(t *testing.T) {
		cluster := testCluster()
		cluster.Name = "tls-issuer-external"
		cluster.Spec.TLS = &v1beta1.TLSSpec{
			IssuerConf: &cmmeta.IssuerReference{Name: "vault-issuer", Kind: "VaultClusterIssuer"},
		}
		client := setupFakeClient(t, cluster)
		ctrl := NewController(client, client.Scheme(), false)

		err := ctrl.ApplyIssuer(t.Context(), cluster)
		require.NoError(t, err)

		list := &v1.IssuerList{}
		require.NoError(t, client.List(t.Context(), list))
		assert.Len(t, list.Items, 0)
	})

	t.Run("create cluster-scoped TLS issuer when Kind is ClusterIssuer", func(t *testing.T) {
		cluster := testCluster()
		cluster.Name = "tls-issuer-cluster-scoped"
		cluster.Spec.TLS = &v1beta1.TLSSpec{
			IssuerConf: &cmmeta.IssuerReference{Name: "shared-tls-issuer", Kind: v1.ClusterIssuerKind},
		}
		client := setupFakeClient(t, cluster)
		ctrl := NewController(client, client.Scheme(), false)

		err := ctrl.ApplyIssuer(t.Context(), cluster)
		require.NoError(t, err)

		issuer := &v1.ClusterIssuer{}
		err = client.Get(t.Context(), sigs.ObjectKey{Name: "shared-tls-issuer"}, issuer)
		require.NoError(t, err)
		require.NotNil(t, issuer.Spec.CA)
		assert.Equal(t, naming.ClusterCACertSecret(cluster, CertManagerNamespace()).Name, issuer.Spec.CA.SecretName)
		assert.Empty(t, issuer.OwnerReferences)

		// idempotent
		err = ctrl.ApplyIssuer(t.Context(), cluster)
		require.NoError(t, err)
	})

	t.Run("managed namespaced honors issuerConf name override", func(t *testing.T) {
		cluster := testCluster()
		cluster.Name = "tls-issuer-custom-name"
		cluster.Spec.TLS = &v1beta1.TLSSpec{
			IssuerConf: &cmmeta.IssuerReference{Name: "my-custom-issuer-name"},
		}
		client := setupFakeClient(t, cluster)
		ctrl := NewController(client, client.Scheme(), false)

		err := ctrl.ApplyIssuer(t.Context(), cluster)
		require.NoError(t, err)

		issuer := &v1.Issuer{}
		err = client.Get(t.Context(), sigs.ObjectKey{Namespace: cluster.Namespace, Name: "my-custom-issuer-name"}, issuer)
		require.NoError(t, err)
		require.Len(t, issuer.OwnerReferences, 1)
	})
}

func TestApplyCAIssuer(t *testing.T) {
	cluster := testCluster()
	cluster.Name = "ca-test-cluster"
	client := setupFakeClient(t, cluster)

	ctrl := NewController(client, client.Scheme(), false)

	t.Run("create CA issuer in cluster namespace", func(t *testing.T) {
		err := ctrl.ApplyCAIssuer(t.Context(), cluster)
		require.NoError(t, err)

		issuer := &v1.Issuer{}
		meta := naming.CAIssuer(cluster)
		err = client.Get(t.Context(), sigs.ObjectKey{
			Namespace: meta.Namespace,
			Name:      meta.Name,
		}, issuer)
		require.NoError(t, err)

		assert.Equal(t, meta.Name, issuer.Name)
		assert.Equal(t, cluster.Namespace, issuer.Namespace)
		assert.NotNil(t, issuer.Spec.SelfSigned)

		require.Len(t, issuer.OwnerReferences, 1)
		assert.Equal(t, cluster.Name, issuer.OwnerReferences[0].Name)

		// return nil when CA issuer already exists
		err = ctrl.ApplyCAIssuer(t.Context(), cluster)
		require.NoError(t, err)
	})

	t.Run("skip when external issuer", func(t *testing.T) {
		cluster := testCluster()
		cluster.Name = "ca-test-external"
		cluster.Spec.TLS = &v1beta1.TLSSpec{
			IssuerConf: &cmmeta.IssuerReference{Name: "vault-issuer", Kind: "VaultClusterIssuer"},
		}
		client := setupFakeClient(t, cluster)
		ctrl := NewController(client, client.Scheme(), false)

		err := ctrl.ApplyCAIssuer(t.Context(), cluster)
		require.NoError(t, err)

		list := &v1.IssuerList{}
		require.NoError(t, client.List(t.Context(), list))
		assert.Len(t, list.Items, 0)
	})

	t.Run("create cluster-scoped CA issuer when Kind is ClusterIssuer", func(t *testing.T) {
		cluster := testCluster()
		cluster.Name = "ca-test-cluster-scoped"
		cluster.Spec.TLS = &v1beta1.TLSSpec{
			IssuerConf: &cmmeta.IssuerReference{Name: "shared-tls-issuer", Kind: v1.ClusterIssuerKind},
		}
		client := setupFakeClient(t, cluster)
		ctrl := NewController(client, client.Scheme(), false)

		err := ctrl.ApplyCAIssuer(t.Context(), cluster)
		require.NoError(t, err)

		issuer := &v1.ClusterIssuer{}
		meta := naming.ClusterCAIssuer(cluster)
		err = client.Get(t.Context(), sigs.ObjectKey{Name: meta.Name}, issuer)
		require.NoError(t, err)
		assert.NotNil(t, issuer.Spec.SelfSigned)
		assert.Empty(t, issuer.OwnerReferences)

		// idempotent
		err = ctrl.ApplyCAIssuer(t.Context(), cluster)
		require.NoError(t, err)
	})
}

func TestApplyCACertificate(t *testing.T) {
	cluster := testCluster()
	cluster.Name = "ca-cert-test-cluster"
	client := setupFakeClient(t, cluster)

	ctrl := NewController(client, client.Scheme(), false)

	t.Run("create CA certificate", func(t *testing.T) {
		err := ctrl.ApplyCACertificate(t.Context(), cluster)
		require.NoError(t, err)

		cert := &v1.Certificate{}
		secretName := naming.PostgresRootCASecret(cluster)
		err = client.Get(t.Context(), sigs.ObjectKey{
			Namespace: cluster.Namespace,
			Name:      secretName.Name,
		}, cert)
		require.NoError(t, err)

		assert.Equal(t, secretName.Name, cert.Name)
		assert.Equal(t, cluster.Namespace, cert.Namespace)
		assert.Equal(t, secretName.Name, cert.Spec.SecretName)
		assert.Equal(t, cluster.Name+"-ca", cert.Spec.CommonName)
		assert.True(t, cert.Spec.IsCA)
		assert.Equal(t, naming.CAIssuer(cluster).Name, cert.Spec.IssuerRef.Name)
		assert.Equal(t, v1.IssuerKind, cert.Spec.IssuerRef.Kind)
		assert.NotNil(t, cert.Spec.Duration)
		assert.NotNil(t, cert.Spec.RenewBefore)
		assert.NotNil(t, cert.Spec.PrivateKey)
		assert.Equal(t, v1.ECDSAKeyAlgorithm, cert.Spec.PrivateKey.Algorithm)
		assert.Equal(t, 256, cert.Spec.PrivateKey.Size)
		assert.Equal(t, v1.RotationPolicyNever, cert.Spec.PrivateKey.RotationPolicy)

		assert.Equal(t, cluster.Name, cert.Labels[naming.LabelCluster])
		assert.NotEmpty(t, cert.Labels[naming.LabelPerconaManagedBy])

		require.Len(t, cert.OwnerReferences, 1)
		assert.Equal(t, cluster.Name, cert.OwnerReferences[0].Name)

		// return nil when certificate already exists
		err = ctrl.ApplyCACertificate(t.Context(), cluster)
		require.NoError(t, err)
	})

	t.Run("skip when external issuer", func(t *testing.T) {
		cluster := testCluster()
		cluster.Name = "ca-cert-external"
		cluster.Spec.TLS = &v1beta1.TLSSpec{
			IssuerConf: &cmmeta.IssuerReference{Name: "vault-issuer", Kind: "VaultClusterIssuer"},
		}
		client := setupFakeClient(t, cluster)
		ctrl := NewController(client, client.Scheme(), false)

		err := ctrl.ApplyCACertificate(t.Context(), cluster)
		require.NoError(t, err)

		list := &v1.CertificateList{}
		require.NoError(t, client.List(t.Context(), list))
		assert.Len(t, list.Items, 0)
	})

	t.Run("places CA certificate in cert-manager namespace when Kind is ClusterIssuer", func(t *testing.T) {
		cluster := testCluster()
		cluster.Name = "ca-cert-cluster-scoped"
		cluster.Spec.TLS = &v1beta1.TLSSpec{
			IssuerConf: &cmmeta.IssuerReference{Name: "shared-tls-issuer", Kind: v1.ClusterIssuerKind},
		}
		client := setupFakeClient(t, cluster)
		ctrl := NewController(client, client.Scheme(), false)

		err := ctrl.ApplyCACertificate(t.Context(), cluster)
		require.NoError(t, err)

		cert := &v1.Certificate{}
		meta := naming.ClusterCACertSecret(cluster, CertManagerNamespace())
		err = client.Get(t.Context(), sigs.ObjectKey{Namespace: meta.Namespace, Name: meta.Name}, cert)
		require.NoError(t, err)
		assert.Equal(t, meta.Name, cert.Spec.SecretName)
		assert.True(t, cert.Spec.IsCA)
		assert.Equal(t, naming.ClusterCAIssuer(cluster).Name, cert.Spec.IssuerRef.Name)
		assert.Equal(t, v1.ClusterIssuerKind, cert.Spec.IssuerRef.Kind)
		assert.Empty(t, cert.OwnerReferences)

		// idempotent
		err = ctrl.ApplyCACertificate(t.Context(), cluster)
		require.NoError(t, err)
	})
}

func TestApplyClusterCertificate(t *testing.T) {
	cluster := testCluster()
	cluster.Name = "cluster-cert-test"
	client := setupFakeClient(t, cluster)

	ctrl := NewController(client, client.Scheme(), false)

	t.Run("create cluster certificate successfully", func(t *testing.T) {
		dnsNames := []string{
			"test-cluster-primary.test-namespace.svc",
			"test-cluster-replicas.test-namespace.svc",
		}

		err := ctrl.ApplyClusterCertificate(t.Context(), cluster, dnsNames)
		require.NoError(t, err)

		cert := &v1.Certificate{}
		secretName := naming.PostgresTLSSecret(cluster)
		err = client.Get(t.Context(), sigs.ObjectKey{
			Namespace: cluster.Namespace,
			Name:      secretName.Name,
		}, cert)
		require.NoError(t, err)

		assert.Equal(t, secretName.Name, cert.Name)
		assert.Equal(t, cluster.Namespace, cert.Namespace)
		assert.Equal(t, secretName.Name, cert.Spec.SecretName)
		assert.Equal(t, cluster.Name+"-postgres", cert.Spec.CommonName)
		assert.Equal(t, dnsNames, cert.Spec.DNSNames)
		assert.Equal(t, naming.TLSIssuer(cluster).Name, cert.Spec.IssuerRef.Name)
		assert.Equal(t, v1.IssuerKind, cert.Spec.IssuerRef.Kind)
		assert.NotNil(t, cert.Spec.Duration)
		assert.Equal(t, DefaultCertDuration, cert.Spec.Duration.Duration)
		assert.NotNil(t, cert.Spec.RenewBefore)
		assert.Equal(t, DefaultRenewBefore, cert.Spec.RenewBefore.Duration)
		assert.NotNil(t, cert.Spec.PrivateKey)
		assert.Equal(t, v1.ECDSAKeyAlgorithm, cert.Spec.PrivateKey.Algorithm)
		assert.Equal(t, 256, cert.Spec.PrivateKey.Size)
		assert.Equal(t, v1.RotationPolicyNever, cert.Spec.PrivateKey.RotationPolicy)

		assert.Contains(t, cert.Spec.Usages, v1.UsageServerAuth)
		assert.Contains(t, cert.Spec.Usages, v1.UsageClientAuth)
		assert.Contains(t, cert.Spec.Usages, v1.UsageDigitalSignature)
		assert.Contains(t, cert.Spec.Usages, v1.UsageKeyEncipherment)

		assert.NotNil(t, cert.Spec.SecretTemplate)
		assert.NotEmpty(t, cert.Spec.SecretTemplate.Labels)
		assert.Equal(t, cluster.Name, cert.Spec.SecretTemplate.Labels[naming.LabelCluster])
		assert.Equal(t, "postgres-tls", cert.Spec.SecretTemplate.Labels[naming.LabelClusterCertificate])

		assert.Equal(t, cluster.Name, cert.Labels[naming.LabelCluster])
		assert.Equal(t, "postgres-tls", cert.Labels[naming.LabelClusterCertificate])
		assert.NotEmpty(t, cert.Labels[naming.LabelPerconaManagedBy])

		require.Len(t, cert.OwnerReferences, 1)
		assert.Equal(t, cluster.Name, cert.OwnerReferences[0].Name)

		// return nil when certificate already exists
		err = ctrl.ApplyClusterCertificate(t.Context(), cluster, dnsNames)
		require.NoError(t, err)
	})

	t.Run("fail with empty dnsNames", func(t *testing.T) {
		cluster2 := testCluster()
		cluster2.Name = "cluster-cert-test-2"
		client2 := setupFakeClient(t, cluster2)
		ctrl2 := NewController(client2, client2.Scheme(), false)

		err := ctrl2.ApplyClusterCertificate(t.Context(), cluster2, []string{})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "dnsNames cannot be empty")
	})
}

func TestApplyInstanceCertificate(t *testing.T) {
	cluster := testCluster()
	cluster.Name = "instance-cert-test"
	client := setupFakeClient(t, cluster)

	ctrl := NewController(client, client.Scheme(), false)

	t.Run("create instance certificate successfully", func(t *testing.T) {
		instanceName := "test-cluster-instance-0"
		dnsNames := []string{
			"test-cluster-instance-0.test-namespace.svc",
			"test-cluster-instance-0",
		}

		err := ctrl.ApplyInstanceCertificate(t.Context(), cluster, instanceName, dnsNames)
		require.NoError(t, err)

		cert := &v1.Certificate{}
		certName := instanceName + "-cert"
		err = client.Get(t.Context(), sigs.ObjectKey{
			Namespace: cluster.Namespace,
			Name:      certName,
		}, cert)
		require.NoError(t, err)

		assert.Equal(t, certName, cert.Name)
		assert.Equal(t, cluster.Namespace, cert.Namespace)
		assert.Equal(t, instanceName+"-certs", cert.Spec.SecretName)
		assert.Equal(t, instanceName, cert.Spec.CommonName)
		assert.Equal(t, dnsNames, cert.Spec.DNSNames)
		assert.Equal(t, naming.TLSIssuer(cluster).Name, cert.Spec.IssuerRef.Name)
		assert.Equal(t, v1.IssuerKind, cert.Spec.IssuerRef.Kind)
		assert.NotNil(t, cert.Spec.Duration)
		assert.Equal(t, DefaultCertDuration, cert.Spec.Duration.Duration)

		assert.NotNil(t, cert.Spec.SecretTemplate)
		assert.Equal(t, cluster.Name, cert.Spec.SecretTemplate.Labels[naming.LabelCluster])
		assert.Equal(t, instanceName, cert.Spec.SecretTemplate.Labels[naming.LabelInstance])

		assert.Equal(t, cluster.Name, cert.Labels[naming.LabelCluster])
		assert.Equal(t, instanceName, cert.Labels[naming.LabelInstance])
		assert.NotEmpty(t, cert.Labels[naming.LabelPerconaManagedBy])

		require.Len(t, cert.OwnerReferences, 1)
		assert.Equal(t, cluster.Name, cert.OwnerReferences[0].Name)

		// return nil when instance certificate already exists
		err = ctrl.ApplyInstanceCertificate(t.Context(), cluster, instanceName, dnsNames)
		require.NoError(t, err)
	})

	t.Run("fail with empty dnsNames", func(t *testing.T) {
		cluster2 := testCluster()
		cluster2.Name = "instance-cert-test-2"
		client2 := setupFakeClient(t, cluster2)
		ctrl2 := NewController(client2, client2.Scheme(), false)

		err := ctrl2.ApplyInstanceCertificate(t.Context(), cluster2, "instance-0", []string{})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "dnsNames cannot be empty")
	})
}

func TestApplyPGBouncerCertificate(t *testing.T) {
	cluster := testCluster()
	cluster.Name = "pgbouncer-cert-test"
	client := setupFakeClient(t, cluster)

	ctrl := NewController(client, client.Scheme(), false)

	t.Run("create pgbouncer certificate successfully", func(t *testing.T) {
		dnsNames := []string{
			"pgbouncer-cert-test-pgbouncer.test-namespace.svc",
			"pgbouncer-cert-test-pgbouncer",
		}

		err := ctrl.ApplyPGBouncerCertificate(t.Context(), cluster, dnsNames)
		require.NoError(t, err)

		cert := &v1.Certificate{}
		certName := cluster.Name + "-pgbouncer-cert"
		err = client.Get(t.Context(), sigs.ObjectKey{
			Namespace: cluster.Namespace,
			Name:      certName,
		}, cert)
		require.NoError(t, err)

		assert.Equal(t, certName, cert.Name)
		assert.Equal(t, cluster.Namespace, cert.Namespace)
		assert.Equal(t, cluster.Name+"-pgbouncer-frontend-tls", cert.Spec.SecretName)
		assert.Equal(t, cluster.Name+"-pgbouncer", cert.Spec.CommonName)
		assert.LessOrEqual(t, len(cert.Spec.CommonName), 64)
		assert.Equal(t, dnsNames, cert.Spec.DNSNames)
		assert.Equal(t, naming.TLSIssuer(cluster).Name, cert.Spec.IssuerRef.Name)
		assert.Equal(t, v1.IssuerKind, cert.Spec.IssuerRef.Kind)
		assert.NotNil(t, cert.Spec.Duration)
		assert.Equal(t, DefaultCertDuration, cert.Spec.Duration.Duration)

		assert.NotNil(t, cert.Spec.SecretTemplate)
		assert.Equal(t, cluster.Name, cert.Spec.SecretTemplate.Labels[naming.LabelCluster])
		assert.Equal(t, naming.RolePGBouncer, cert.Spec.SecretTemplate.Labels[naming.LabelRole])

		assert.Equal(t, cluster.Name, cert.Labels[naming.LabelCluster])
		assert.Equal(t, naming.RolePGBouncer, cert.Labels[naming.LabelRole])
		assert.NotEmpty(t, cert.Labels[naming.LabelPerconaManagedBy])

		require.Len(t, cert.OwnerReferences, 1)
		assert.Equal(t, cluster.Name, cert.OwnerReferences[0].Name)

		// return nil when pgbouncer certificate already exists
		err = ctrl.ApplyPGBouncerCertificate(t.Context(), cluster, dnsNames)
		require.NoError(t, err)
	})

	t.Run("commonName does not exceed 64 bytes", func(t *testing.T) {
		cluster3 := testCluster()
		cluster3.Name = "a-very-long-cluster-name-that-exceeds-fifty-four-characters-xx"
		client3 := setupFakeClient(t, cluster3)
		ctrl3 := NewController(client3, client3.Scheme(), false)

		dnsNames := []string{cluster3.Name + "-pgbouncer.test-namespace.svc"}
		err := ctrl3.ApplyPGBouncerCertificate(t.Context(), cluster3, dnsNames)
		require.NoError(t, err)

		cert := &v1.Certificate{}
		err = client3.Get(t.Context(), sigs.ObjectKey{
			Namespace: cluster3.Namespace,
			Name:      cluster3.Name + "-pgbouncer-cert",
		}, cert)
		require.NoError(t, err)

		assert.LessOrEqual(t, len(cert.Spec.CommonName), 64)
	})

	t.Run("fail with empty dnsNames", func(t *testing.T) {
		cluster2 := testCluster()
		cluster2.Name = "pgbouncer-cert-test-2"
		client2 := setupFakeClient(t, cluster2)
		ctrl2 := NewController(client2, client2.Scheme(), false)

		err := ctrl2.ApplyPGBouncerCertificate(t.Context(), cluster2, []string{})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "dnsNames cannot be empty")
	})
}

func TestApplyPGBackRestRepoCertificate(t *testing.T) {
	t.Run("create pgbackrest repo certificate successfully", func(t *testing.T) {
		cluster := testCluster()
		cluster.Name = "pgbackrest-repo-cert-test"
		client := setupFakeClient(t, cluster)
		ctrl := NewController(client, client.Scheme(), false)

		dnsNames := []string{
			"pgbackrest-repo-cert-test-repo-host-0.pgbackrest-repo-cert-test-pgbackrest.test-namespace.svc.cluster.local",
			"pgbackrest-repo-cert-test-repo-host-0.pgbackrest-repo-cert-test-pgbackrest.test-namespace.svc",
			"pgbackrest-repo-cert-test-repo-host-0.pgbackrest-repo-cert-test-pgbackrest.test-namespace",
			"pgbackrest-repo-cert-test-repo-host-0.pgbackrest-repo-cert-test-pgbackrest",
		}

		err := ctrl.ApplyPGBackRestRepoCertificate(t.Context(), cluster, dnsNames)
		require.NoError(t, err)

		cert := &v1.Certificate{}
		certName := cluster.Name + "-pgbackrest-repo-cert"
		err = client.Get(t.Context(), sigs.ObjectKey{
			Namespace: cluster.Namespace,
			Name:      certName,
		}, cert)
		require.NoError(t, err)

		assert.Equal(t, certName, cert.Name)
		assert.Equal(t, cluster.Namespace, cert.Namespace)
		assert.Equal(t, cluster.Name+"-pgbackrest-repo-tls", cert.Spec.SecretName)
		assert.Equal(t, cluster.Name+"-pgbackrest-repo", cert.Spec.CommonName)
		assert.LessOrEqual(t, len(cert.Spec.CommonName), 64)
		assert.Equal(t, dnsNames, cert.Spec.DNSNames)
		assert.Equal(t, naming.TLSIssuer(cluster).Name, cert.Spec.IssuerRef.Name)
		assert.Equal(t, v1.IssuerKind, cert.Spec.IssuerRef.Kind)
		assert.NotNil(t, cert.Spec.Duration)
		assert.Equal(t, DefaultCertDuration, cert.Spec.Duration.Duration)

		assert.Equal(t, cluster.Name, cert.Labels[naming.LabelCluster])
		assert.NotEmpty(t, cert.Labels[naming.LabelPerconaManagedBy])

		require.Len(t, cert.OwnerReferences, 1)
		assert.Equal(t, cluster.Name, cert.OwnerReferences[0].Name)

		// return nil when certificate already exists
		err = ctrl.ApplyPGBackRestRepoCertificate(t.Context(), cluster, dnsNames)
		require.NoError(t, err)
	})

	t.Run("commonName does not exceed 64 bytes for long cluster names", func(t *testing.T) {
		cluster := testCluster()
		cluster.Name = "long-cluster-name-that-makes-fqdn-too-long"
		client := setupFakeClient(t, cluster)
		ctrl := NewController(client, client.Scheme(), false)

		dnsNames := []string{
			cluster.Name + "-repo-host-0." + cluster.Name + "-pgbackrest.test-namespace.svc.cluster.local",
		}

		err := ctrl.ApplyPGBackRestRepoCertificate(t.Context(), cluster, dnsNames)
		require.NoError(t, err)

		cert := &v1.Certificate{}
		err = client.Get(t.Context(), sigs.ObjectKey{
			Namespace: cluster.Namespace,
			Name:      cluster.Name + "-pgbackrest-repo-cert",
		}, cert)
		require.NoError(t, err)

		assert.LessOrEqual(t, len(cert.Spec.CommonName), 64)
	})

	t.Run("fail with empty dnsNames", func(t *testing.T) {
		cluster := testCluster()
		cluster.Name = "pgbackrest-repo-cert-test-2"
		client := setupFakeClient(t, cluster)
		ctrl := NewController(client, client.Scheme(), false)

		err := ctrl.ApplyPGBackRestRepoCertificate(t.Context(), cluster, []string{})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "dnsNames cannot be empty")
	})

	t.Run("uses pgBackRestCertValidityDuration when set", func(t *testing.T) {
		pgbrDuration := 4320 * time.Hour // 180 days
		cluster := testCluster()
		cluster.Name = "pgbackrest-repo-cert-pgbr-dur"
		cluster.Spec.TLS = &v1beta1.TLSSpec{
			PGBackRestCertValidityDuration: &metav1.Duration{Duration: pgbrDuration},
		}
		client := setupFakeClient(t, cluster)
		ctrl := NewController(client, client.Scheme(), false)

		dnsNames := []string{cluster.Name + "-repo-host-0." + cluster.Name + "-pgbackrest.test-namespace.svc"}
		err := ctrl.ApplyPGBackRestRepoCertificate(t.Context(), cluster, dnsNames)
		require.NoError(t, err)

		cert := &v1.Certificate{}
		err = client.Get(t.Context(), sigs.ObjectKey{
			Namespace: cluster.Namespace,
			Name:      cluster.Name + "-pgbackrest-repo-cert",
		}, cert)
		require.NoError(t, err)
		assert.Equal(t, pgbrDuration, cert.Spec.Duration.Duration)
	})
}

func TestApplyPGBackRestClientCertificate(t *testing.T) {
	t.Run("create pgbackrest client certificate successfully", func(t *testing.T) {
		cluster := testCluster()
		cluster.Name = "pgbackrest-client-cert-test"
		client := setupFakeClient(t, cluster)
		ctrl := NewController(client, client.Scheme(), false)

		err := ctrl.ApplyPGBackRestClientCertificate(t.Context(), cluster)
		require.NoError(t, err)

		cert := &v1.Certificate{}
		certName := cluster.Name + "-pgbackrest-client-cert"
		err = client.Get(t.Context(), sigs.ObjectKey{
			Namespace: cluster.Namespace,
			Name:      certName,
		}, cert)
		require.NoError(t, err)

		assert.Equal(t, certName, cert.Name)
		assert.Equal(t, cluster.Namespace, cert.Namespace)
		assert.Equal(t, naming.PGBackRestClientCertSecret(cluster).Name, cert.Spec.SecretName)
		assert.Equal(t, "pgbackrest@"+string(cluster.GetUID()), cert.Spec.CommonName)
		assert.Equal(t, naming.TLSIssuer(cluster).Name, cert.Spec.IssuerRef.Name)
		assert.Equal(t, v1.IssuerKind, cert.Spec.IssuerRef.Kind)
		assert.NotNil(t, cert.Spec.Duration)
		assert.Equal(t, DefaultCertDuration, cert.Spec.Duration.Duration)

		assert.Contains(t, cert.Spec.Usages, v1.UsageClientAuth)
		assert.Contains(t, cert.Spec.Usages, v1.UsageDigitalSignature)
		assert.Contains(t, cert.Spec.Usages, v1.UsageKeyEncipherment)

		assert.NotNil(t, cert.Spec.SecretTemplate)
		assert.Equal(t, cluster.Name, cert.Spec.SecretTemplate.Labels[naming.LabelCluster])

		assert.Equal(t, cluster.Name, cert.Labels[naming.LabelCluster])
		assert.NotEmpty(t, cert.Labels[naming.LabelPerconaManagedBy])

		require.Len(t, cert.OwnerReferences, 1)
		assert.Equal(t, cluster.Name, cert.OwnerReferences[0].Name)

		// return nil when certificate already exists
		err = ctrl.ApplyPGBackRestClientCertificate(t.Context(), cluster)
		require.NoError(t, err)
	})

	t.Run("uses pgBackRestCertValidityDuration when set", func(t *testing.T) {
		pgbrDuration := 4320 * time.Hour // 180 days
		cluster := testCluster()
		cluster.Name = "pgbackrest-client-cert-pgbr-dur"
		cluster.Spec.TLS = &v1beta1.TLSSpec{
			PGBackRestCertValidityDuration: &metav1.Duration{Duration: pgbrDuration},
		}
		client := setupFakeClient(t, cluster)
		ctrl := NewController(client, client.Scheme(), false)

		err := ctrl.ApplyPGBackRestClientCertificate(t.Context(), cluster)
		require.NoError(t, err)

		cert := &v1.Certificate{}
		err = client.Get(t.Context(), sigs.ObjectKey{
			Namespace: cluster.Namespace,
			Name:      cluster.Name + "-pgbackrest-client-cert",
		}, cert)
		require.NoError(t, err)
		assert.Equal(t, pgbrDuration, cert.Spec.Duration.Duration)
	})
}

func TestApplyReplicationCertificate(t *testing.T) {
	t.Run("create replication certificate successfully", func(t *testing.T) {
		cluster := testCluster()
		cluster.Name = "replication-cert-test"
		client := setupFakeClient(t, cluster)
		ctrl := NewController(client, client.Scheme(), false)

		err := ctrl.ApplyReplicationCertificate(t.Context(), cluster)
		require.NoError(t, err)

		cert := &v1.Certificate{}
		certName := cluster.Name + "-replication-cert"
		err = client.Get(t.Context(), sigs.ObjectKey{
			Namespace: cluster.Namespace,
			Name:      certName,
		}, cert)
		require.NoError(t, err)

		assert.Equal(t, certName, cert.Name)
		assert.Equal(t, cluster.Namespace, cert.Namespace)
		assert.Equal(t, naming.ReplicationClientCertSecret(cluster).Name, cert.Spec.SecretName)
		assert.Equal(t, "_crunchyrepl", cert.Spec.CommonName)
		assert.Equal(t, []string{"_crunchyrepl"}, cert.Spec.DNSNames)
		assert.Equal(t, naming.TLSIssuer(cluster).Name, cert.Spec.IssuerRef.Name)
		assert.Equal(t, v1.IssuerKind, cert.Spec.IssuerRef.Kind)
		assert.NotNil(t, cert.Spec.Duration)
		assert.Equal(t, DefaultCertDuration, cert.Spec.Duration.Duration)
		assert.NotNil(t, cert.Spec.RenewBefore)
		assert.Equal(t, DefaultRenewBefore, cert.Spec.RenewBefore.Duration)
		assert.NotNil(t, cert.Spec.PrivateKey)
		assert.Equal(t, v1.ECDSAKeyAlgorithm, cert.Spec.PrivateKey.Algorithm)
		assert.Equal(t, 256, cert.Spec.PrivateKey.Size)
		assert.Equal(t, v1.RotationPolicyNever, cert.Spec.PrivateKey.RotationPolicy)

		assert.Contains(t, cert.Spec.Usages, v1.UsageClientAuth)
		assert.Contains(t, cert.Spec.Usages, v1.UsageDigitalSignature)
		assert.Contains(t, cert.Spec.Usages, v1.UsageKeyEncipherment)
		assert.NotContains(t, cert.Spec.Usages, v1.UsageServerAuth)

		assert.NotNil(t, cert.Spec.SecretTemplate)
		assert.Equal(t, cluster.Name, cert.Spec.SecretTemplate.Labels[naming.LabelCluster])
		assert.Equal(t, "replication-client-tls", cert.Spec.SecretTemplate.Labels[naming.LabelClusterCertificate])

		assert.Equal(t, cluster.Name, cert.Labels[naming.LabelCluster])
		assert.Equal(t, "replication-client-tls", cert.Labels[naming.LabelClusterCertificate])
		assert.NotEmpty(t, cert.Labels[naming.LabelPerconaManagedBy])

		require.Len(t, cert.OwnerReferences, 1)
		assert.Equal(t, cluster.Name, cert.OwnerReferences[0].Name)

		// return nil when certificate already exists
		err = ctrl.ApplyReplicationCertificate(t.Context(), cluster)
		require.NoError(t, err)
	})

	t.Run("uses CertValidityDuration when set", func(t *testing.T) {
		customDuration := 4320 * time.Hour // 180 days
		cluster := testCluster()
		cluster.Name = "replication-cert-dur"
		cluster.Spec.TLS = &v1beta1.TLSSpec{
			CertValidityDuration: &metav1.Duration{Duration: customDuration},
		}
		client := setupFakeClient(t, cluster)
		ctrl := NewController(client, client.Scheme(), false)

		err := ctrl.ApplyReplicationCertificate(t.Context(), cluster)
		require.NoError(t, err)

		cert := &v1.Certificate{}
		err = client.Get(t.Context(), sigs.ObjectKey{
			Namespace: cluster.Namespace,
			Name:      cluster.Name + "-replication-cert",
		}, cert)
		require.NoError(t, err)
		assert.Equal(t, customDuration, cert.Spec.Duration.Duration)
	})
}

func TestUpdateCertificateDuration(t *testing.T) {
	initialDuration := 2160 * time.Hour    // 90 days
	updatedDuration := 4320 * time.Hour    // 180 days
	updatedCADuration := 52560 * time.Hour // 6 years

	t.Run("CA certificate duration updated when spec changes", func(t *testing.T) {
		cluster := testCluster()
		cluster.Name = "update-ca-dur"
		cluster.Spec.TLS = &v1beta1.TLSSpec{
			CAValidityDuration: &metav1.Duration{Duration: initialDuration},
		}
		client := setupFakeClient(t, cluster)
		ctrl := NewController(client, client.Scheme(), false)

		err := ctrl.ApplyCACertificate(t.Context(), cluster)
		require.NoError(t, err)

		cluster.Spec.TLS.CAValidityDuration = &metav1.Duration{Duration: updatedCADuration}
		err = ctrl.ApplyCACertificate(t.Context(), cluster)
		require.NoError(t, err)

		cert := &v1.Certificate{}
		secretName := naming.PostgresRootCASecret(cluster)
		err = client.Get(t.Context(), sigs.ObjectKey{
			Namespace: cluster.Namespace,
			Name:      secretName.Name,
		}, cert)
		require.NoError(t, err)
		assert.Equal(t, updatedCADuration, cert.Spec.Duration.Duration)
	})

	t.Run("cluster certificate duration updated when spec changes", func(t *testing.T) {
		cluster := testCluster()
		cluster.Name = "update-cluster-dur"
		cluster.Spec.TLS = &v1beta1.TLSSpec{
			CertValidityDuration: &metav1.Duration{Duration: initialDuration},
		}
		client := setupFakeClient(t, cluster)
		ctrl := NewController(client, client.Scheme(), false)

		dnsNames := []string{"update-cluster-dur-primary.test-namespace.svc"}
		err := ctrl.ApplyClusterCertificate(t.Context(), cluster, dnsNames)
		require.NoError(t, err)

		cluster.Spec.TLS.CertValidityDuration = &metav1.Duration{Duration: updatedDuration}
		err = ctrl.ApplyClusterCertificate(t.Context(), cluster, dnsNames)
		require.NoError(t, err)

		cert := &v1.Certificate{}
		secretName := naming.PostgresTLSSecret(cluster)
		err = client.Get(t.Context(), sigs.ObjectKey{
			Namespace: cluster.Namespace,
			Name:      secretName.Name,
		}, cert)
		require.NoError(t, err)
		assert.Equal(t, updatedDuration, cert.Spec.Duration.Duration)
	})

	t.Run("instance certificate duration updated when spec changes", func(t *testing.T) {
		cluster := testCluster()
		cluster.Name = "update-inst-dur"
		cluster.Spec.TLS = &v1beta1.TLSSpec{
			CertValidityDuration: &metav1.Duration{Duration: initialDuration},
		}
		client := setupFakeClient(t, cluster)
		ctrl := NewController(client, client.Scheme(), false)

		instanceName := "update-inst-dur-instance-0"
		dnsNames := []string{instanceName + ".test-namespace.svc"}
		err := ctrl.ApplyInstanceCertificate(t.Context(), cluster, instanceName, dnsNames)
		require.NoError(t, err)

		cluster.Spec.TLS.CertValidityDuration = &metav1.Duration{Duration: updatedDuration}
		err = ctrl.ApplyInstanceCertificate(t.Context(), cluster, instanceName, dnsNames)
		require.NoError(t, err)

		cert := &v1.Certificate{}
		certName := instanceName + "-cert"
		err = client.Get(t.Context(), sigs.ObjectKey{
			Namespace: cluster.Namespace,
			Name:      certName,
		}, cert)
		require.NoError(t, err)
		assert.Equal(t, updatedDuration, cert.Spec.Duration.Duration)
	})

	t.Run("pgbouncer certificate duration updated when spec changes", func(t *testing.T) {
		cluster := testCluster()
		cluster.Name = "update-pgb-dur"
		cluster.Spec.TLS = &v1beta1.TLSSpec{
			CertValidityDuration: &metav1.Duration{Duration: initialDuration},
		}
		client := setupFakeClient(t, cluster)
		ctrl := NewController(client, client.Scheme(), false)

		dnsNames := []string{"update-pgb-dur-pgbouncer.test-namespace.svc"}
		err := ctrl.ApplyPGBouncerCertificate(t.Context(), cluster, dnsNames)
		require.NoError(t, err)

		cluster.Spec.TLS.CertValidityDuration = &metav1.Duration{Duration: updatedDuration}
		err = ctrl.ApplyPGBouncerCertificate(t.Context(), cluster, dnsNames)
		require.NoError(t, err)

		cert := &v1.Certificate{}
		certName := cluster.Name + "-pgbouncer-cert"
		err = client.Get(t.Context(), sigs.ObjectKey{
			Namespace: cluster.Namespace,
			Name:      certName,
		}, cert)
		require.NoError(t, err)
		assert.Equal(t, updatedDuration, cert.Spec.Duration.Duration)
	})

	t.Run("replication certificate duration updated when spec changes", func(t *testing.T) {
		cluster := testCluster()
		cluster.Name = "update-repl-dur"
		cluster.Spec.TLS = &v1beta1.TLSSpec{
			CertValidityDuration: &metav1.Duration{Duration: initialDuration},
		}
		client := setupFakeClient(t, cluster)
		ctrl := NewController(client, client.Scheme(), false)

		err := ctrl.ApplyReplicationCertificate(t.Context(), cluster)
		require.NoError(t, err)

		cluster.Spec.TLS.CertValidityDuration = &metav1.Duration{Duration: updatedDuration}
		err = ctrl.ApplyReplicationCertificate(t.Context(), cluster)
		require.NoError(t, err)

		cert := &v1.Certificate{}
		err = client.Get(t.Context(), sigs.ObjectKey{
			Namespace: cluster.Namespace,
			Name:      cluster.Name + "-replication-cert",
		}, cert)
		require.NoError(t, err)
		assert.Equal(t, updatedDuration, cert.Spec.Duration.Duration)
	})

	t.Run("pgbackrest client certificate duration updated when pgBackRestCertValidityDuration changes", func(t *testing.T) {
		cluster := testCluster()
		cluster.Name = "update-pgbr-client-dur"
		cluster.Spec.TLS = &v1beta1.TLSSpec{
			PGBackRestCertValidityDuration: &metav1.Duration{Duration: initialDuration},
		}
		client := setupFakeClient(t, cluster)
		ctrl := NewController(client, client.Scheme(), false)

		err := ctrl.ApplyPGBackRestClientCertificate(t.Context(), cluster)
		require.NoError(t, err)

		cluster.Spec.TLS.PGBackRestCertValidityDuration = &metav1.Duration{Duration: updatedDuration}
		err = ctrl.ApplyPGBackRestClientCertificate(t.Context(), cluster)
		require.NoError(t, err)

		cert := &v1.Certificate{}
		err = client.Get(t.Context(), sigs.ObjectKey{
			Namespace: cluster.Namespace,
			Name:      cluster.Name + "-pgbackrest-client-cert",
		}, cert)
		require.NoError(t, err)
		assert.Equal(t, updatedDuration, cert.Spec.Duration.Duration)
	})

	t.Run("pgbackrest repo certificate duration updated when pgBackRestCertValidityDuration changes", func(t *testing.T) {
		cluster := testCluster()
		cluster.Name = "update-pgbr-repo-dur"
		cluster.Spec.TLS = &v1beta1.TLSSpec{
			PGBackRestCertValidityDuration: &metav1.Duration{Duration: initialDuration},
		}
		client := setupFakeClient(t, cluster)
		ctrl := NewController(client, client.Scheme(), false)

		dnsNames := []string{cluster.Name + "-repo-host-0." + cluster.Name + "-pgbackrest.test-namespace.svc"}
		err := ctrl.ApplyPGBackRestRepoCertificate(t.Context(), cluster, dnsNames)
		require.NoError(t, err)

		cluster.Spec.TLS.PGBackRestCertValidityDuration = &metav1.Duration{Duration: updatedDuration}
		err = ctrl.ApplyPGBackRestRepoCertificate(t.Context(), cluster, dnsNames)
		require.NoError(t, err)

		cert := &v1.Certificate{}
		err = client.Get(t.Context(), sigs.ObjectKey{
			Namespace: cluster.Namespace,
			Name:      cluster.Name + "-pgbackrest-repo-cert",
		}, cert)
		require.NoError(t, err)
		assert.Equal(t, updatedDuration, cert.Spec.Duration.Duration)
	})
}

func TestCustomTLSDurations(t *testing.T) {
	customCertDuration := 2160 * time.Hour // 90 days
	customCADuration := 26280 * time.Hour  // 3 years

	t.Run("CA certificate uses custom CA duration", func(t *testing.T) {
		cluster := testCluster()
		cluster.Name = "custom-ca-dur"
		cluster.Spec.TLS = &v1beta1.TLSSpec{
			CAValidityDuration: &metav1.Duration{Duration: customCADuration},
		}
		client := setupFakeClient(t, cluster)
		ctrl := NewController(client, client.Scheme(), false)

		err := ctrl.ApplyCACertificate(t.Context(), cluster)
		require.NoError(t, err)

		cert := &v1.Certificate{}
		secretName := naming.PostgresRootCASecret(cluster)
		err = client.Get(t.Context(), sigs.ObjectKey{
			Namespace: cluster.Namespace,
			Name:      secretName.Name,
		}, cert)
		require.NoError(t, err)
		assert.Equal(t, customCADuration, cert.Spec.Duration.Duration)
		assert.Equal(t, DefaultRenewBefore, cert.Spec.RenewBefore.Duration)
	})

	t.Run("CA certificate uses default duration when TLS is nil", func(t *testing.T) {
		cluster := testCluster()
		cluster.Name = "default-ca-dur"
		client := setupFakeClient(t, cluster)
		ctrl := NewController(client, client.Scheme(), false)

		err := ctrl.ApplyCACertificate(t.Context(), cluster)
		require.NoError(t, err)

		cert := &v1.Certificate{}
		secretName := naming.PostgresRootCASecret(cluster)
		err = client.Get(t.Context(), sigs.ObjectKey{
			Namespace: cluster.Namespace,
			Name:      secretName.Name,
		}, cert)
		require.NoError(t, err)
		assert.Equal(t, DefaultCertDuration, cert.Spec.Duration.Duration)
	})

	t.Run("cluster certificate uses custom cert duration", func(t *testing.T) {
		cluster := testCluster()
		cluster.Name = "custom-cert-dur"
		cluster.Spec.TLS = &v1beta1.TLSSpec{
			CertValidityDuration: &metav1.Duration{Duration: customCertDuration},
		}
		client := setupFakeClient(t, cluster)
		ctrl := NewController(client, client.Scheme(), false)

		dnsNames := []string{"custom-cert-dur-primary.test-namespace.svc"}
		err := ctrl.ApplyClusterCertificate(t.Context(), cluster, dnsNames)
		require.NoError(t, err)

		cert := &v1.Certificate{}
		secretName := naming.PostgresTLSSecret(cluster)
		err = client.Get(t.Context(), sigs.ObjectKey{
			Namespace: cluster.Namespace,
			Name:      secretName.Name,
		}, cert)
		require.NoError(t, err)
		assert.Equal(t, customCertDuration, cert.Spec.Duration.Duration)
	})

	t.Run("instance certificate uses custom cert duration", func(t *testing.T) {
		cluster := testCluster()
		cluster.Name = "custom-inst-dur"
		cluster.Spec.TLS = &v1beta1.TLSSpec{
			CertValidityDuration: &metav1.Duration{Duration: customCertDuration},
		}
		client := setupFakeClient(t, cluster)
		ctrl := NewController(client, client.Scheme(), false)

		instanceName := "custom-inst-dur-instance-0"
		dnsNames := []string{instanceName + ".test-namespace.svc"}
		err := ctrl.ApplyInstanceCertificate(t.Context(), cluster, instanceName, dnsNames)
		require.NoError(t, err)

		cert := &v1.Certificate{}
		certName := instanceName + "-cert"
		err = client.Get(t.Context(), sigs.ObjectKey{
			Namespace: cluster.Namespace,
			Name:      certName,
		}, cert)
		require.NoError(t, err)
		assert.Equal(t, customCertDuration, cert.Spec.Duration.Duration)
	})

	t.Run("pgbouncer certificate uses custom cert duration", func(t *testing.T) {
		cluster := testCluster()
		cluster.Name = "custom-pgb-dur"
		cluster.Spec.TLS = &v1beta1.TLSSpec{
			CertValidityDuration: &metav1.Duration{Duration: customCertDuration},
		}
		client := setupFakeClient(t, cluster)
		ctrl := NewController(client, client.Scheme(), false)

		dnsNames := []string{"custom-pgb-dur-pgbouncer.test-namespace.svc"}
		err := ctrl.ApplyPGBouncerCertificate(t.Context(), cluster, dnsNames)
		require.NoError(t, err)

		cert := &v1.Certificate{}
		certName := cluster.Name + "-pgbouncer-cert"
		err = client.Get(t.Context(), sigs.ObjectKey{
			Namespace: cluster.Namespace,
			Name:      certName,
		}, cert)
		require.NoError(t, err)
		assert.Equal(t, customCertDuration, cert.Spec.Duration.Duration)
	})
}

// forbiddenGetClient wraps a client.Client and returns a Forbidden error from
// Get for *v1.ClusterIssuer, simulating a cluster that hasn't granted RBAC
// for clusterissuers.cert-manager.io (the default — see K8SPG-951's design:
// no such RBAC ships by default in either deployment mode).
type forbiddenGetClient struct {
	sigs.Client
}

func (f *forbiddenGetClient) Get(ctx context.Context, key sigs.ObjectKey, obj sigs.Object, opts ...sigs.GetOption) error {
	if _, ok := obj.(*v1.ClusterIssuer); ok {
		return k8serrors.NewForbidden(
			schema.GroupResource{Group: "cert-manager.io", Resource: "clusterissuers"},
			key.Name, fmt.Errorf("forbidden"))
	}
	return f.Client.Get(ctx, key, obj, opts...)
}

func TestCertManagerNamespace(t *testing.T) {
	t.Run("defaults to cert-manager", func(t *testing.T) {
		t.Setenv("CERTMANAGER_NAMESPACE", "")
		assert.Equal(t, "cert-manager", CertManagerNamespace())
	})

	t.Run("honors CERTMANAGER_NAMESPACE", func(t *testing.T) {
		t.Setenv("CERTMANAGER_NAMESPACE", "custom-cm-ns")
		assert.Equal(t, "custom-cm-ns", CertManagerNamespace())
	})
}

func TestResolveIssuerMode(t *testing.T) {
	t.Run("nil TLS returns managed namespaced", func(t *testing.T) {
		cluster := testCluster()
		cl := setupFakeClient(t, cluster)

		mode, err := ResolveIssuerMode(t.Context(), cl, cluster)
		require.NoError(t, err)
		assert.Equal(t, IssuerModeManagedNamespaced, mode)
	})

	t.Run("nil IssuerConf returns managed namespaced", func(t *testing.T) {
		cluster := testCluster()
		cluster.Spec.TLS = &v1beta1.TLSSpec{}
		cl := setupFakeClient(t, cluster)

		mode, err := ResolveIssuerMode(t.Context(), cl, cluster)
		require.NoError(t, err)
		assert.Equal(t, IssuerModeManagedNamespaced, mode)
	})

	t.Run("Kind Issuer returns managed namespaced", func(t *testing.T) {
		cluster := testCluster()
		cluster.Spec.TLS = &v1beta1.TLSSpec{
			IssuerConf: &cmmeta.IssuerReference{Name: "my-issuer", Kind: v1.IssuerKind},
		}
		cl := setupFakeClient(t, cluster)

		mode, err := ResolveIssuerMode(t.Context(), cl, cluster)
		require.NoError(t, err)
		assert.Equal(t, IssuerModeManagedNamespaced, mode)
	})

	t.Run("Kind ClusterIssuer not found returns managed cluster", func(t *testing.T) {
		cluster := testCluster()
		cluster.Spec.TLS = &v1beta1.TLSSpec{
			IssuerConf: &cmmeta.IssuerReference{Name: "shared-issuer", Kind: v1.ClusterIssuerKind},
		}
		cl := setupFakeClient(t, cluster)

		mode, err := ResolveIssuerMode(t.Context(), cl, cluster)
		require.NoError(t, err)
		assert.Equal(t, IssuerModeManagedCluster, mode)
	})

	t.Run("Kind ClusterIssuer readable returns managed cluster", func(t *testing.T) {
		cluster := testCluster()
		cluster.Spec.TLS = &v1beta1.TLSSpec{
			IssuerConf: &cmmeta.IssuerReference{Name: "shared-issuer", Kind: v1.ClusterIssuerKind},
		}
		existing := &v1.ClusterIssuer{ObjectMeta: metav1.ObjectMeta{Name: "shared-issuer"}}
		cl := setupFakeClient(t, cluster, existing)

		mode, err := ResolveIssuerMode(t.Context(), cl, cluster)
		require.NoError(t, err)
		assert.Equal(t, IssuerModeManagedCluster, mode)
	})

	t.Run("Kind ClusterIssuer forbidden returns external", func(t *testing.T) {
		cluster := testCluster()
		cluster.Spec.TLS = &v1beta1.TLSSpec{
			IssuerConf: &cmmeta.IssuerReference{Name: "shared-issuer", Kind: v1.ClusterIssuerKind},
		}
		cl := &forbiddenGetClient{Client: setupFakeClient(t, cluster)}

		mode, err := ResolveIssuerMode(t.Context(), cl, cluster)
		require.NoError(t, err)
		assert.Equal(t, IssuerModeExternal, mode)
	})

	t.Run("third-party Kind returns external", func(t *testing.T) {
		cluster := testCluster()
		cluster.Spec.TLS = &v1beta1.TLSSpec{
			IssuerConf: &cmmeta.IssuerReference{Name: "vault-issuer", Kind: "VaultClusterIssuer", Group: "vault.example.com"},
		}
		cl := setupFakeClient(t, cluster)

		mode, err := ResolveIssuerMode(t.Context(), cl, cluster)
		require.NoError(t, err)
		assert.Equal(t, IssuerModeExternal, mode)
	})
}

func TestIssuerRef(t *testing.T) {
	t.Run("managed namespaced without issuerConf uses generated name", func(t *testing.T) {
		cluster := testCluster()
		ref := issuerRef(cluster, IssuerModeManagedNamespaced)
		assert.Equal(t, naming.TLSIssuer(cluster).Name, ref.Name)
		assert.Equal(t, v1.IssuerKind, ref.Kind)
		assert.Equal(t, "", ref.Group)
	})

	t.Run("managed namespaced with issuerConf name override", func(t *testing.T) {
		cluster := testCluster()
		cluster.Spec.TLS = &v1beta1.TLSSpec{
			IssuerConf: &cmmeta.IssuerReference{Name: "custom-tls-issuer"},
		}
		ref := issuerRef(cluster, IssuerModeManagedNamespaced)
		assert.Equal(t, "custom-tls-issuer", ref.Name)
		assert.Equal(t, v1.IssuerKind, ref.Kind)
	})

	t.Run("managed cluster uses issuerConf name with ClusterIssuer kind", func(t *testing.T) {
		cluster := testCluster()
		cluster.Spec.TLS = &v1beta1.TLSSpec{
			IssuerConf: &cmmeta.IssuerReference{Name: "shared-issuer", Kind: v1.ClusterIssuerKind},
		}
		ref := issuerRef(cluster, IssuerModeManagedCluster)
		assert.Equal(t, "shared-issuer", ref.Name)
		assert.Equal(t, v1.ClusterIssuerKind, ref.Kind)
	})

	t.Run("external uses issuerConf verbatim with default group", func(t *testing.T) {
		cluster := testCluster()
		cluster.Spec.TLS = &v1beta1.TLSSpec{
			IssuerConf: &cmmeta.IssuerReference{Name: "vault-issuer", Kind: "VaultClusterIssuer"},
		}
		ref := issuerRef(cluster, IssuerModeExternal)
		assert.Equal(t, "vault-issuer", ref.Name)
		assert.Equal(t, "VaultClusterIssuer", ref.Kind)
		assert.Equal(t, "cert-manager.io", ref.Group)
	})

	t.Run("external preserves explicit group", func(t *testing.T) {
		cluster := testCluster()
		cluster.Spec.TLS = &v1beta1.TLSSpec{
			IssuerConf: &cmmeta.IssuerReference{Name: "vault-issuer", Kind: "VaultClusterIssuer", Group: "vault.example.com"},
		}
		ref := issuerRef(cluster, IssuerModeExternal)
		assert.Equal(t, "vault.example.com", ref.Group)
	})
}

func TestApplyCertificateIssuerRefDrift(t *testing.T) {
	newIssuerConf := func(kind string) *v1beta1.TLSSpec {
		return &v1beta1.TLSSpec{IssuerConf: &cmmeta.IssuerReference{Name: "vault-issuer", Kind: kind}}
	}

	t.Run("cluster certificate switches to external issuerRef on update", func(t *testing.T) {
		cluster := testCluster()
		cluster.Name = "drift-cluster-cert"
		client := setupFakeClient(t, cluster)
		ctrl := NewController(client, client.Scheme(), false)

		dnsNames := []string{"drift-cluster-cert-primary.test-namespace.svc"}
		require.NoError(t, ctrl.ApplyClusterCertificate(t.Context(), cluster, dnsNames))

		cluster.Spec.TLS = newIssuerConf("VaultClusterIssuer")
		require.NoError(t, ctrl.ApplyClusterCertificate(t.Context(), cluster, dnsNames))

		cert := &v1.Certificate{}
		secretName := naming.PostgresTLSSecret(cluster)
		require.NoError(t, client.Get(t.Context(), sigs.ObjectKey{Namespace: cluster.Namespace, Name: secretName.Name}, cert))
		assert.Equal(t, "vault-issuer", cert.Spec.IssuerRef.Name)
		assert.Equal(t, "VaultClusterIssuer", cert.Spec.IssuerRef.Kind)
	})

	t.Run("instance certificate switches issuerRef on update", func(t *testing.T) {
		cluster := testCluster()
		cluster.Name = "drift-instance-cert"
		client := setupFakeClient(t, cluster)
		ctrl := NewController(client, client.Scheme(), false)

		instanceName := "drift-instance-cert-instance-0"
		dnsNames := []string{instanceName + ".test-namespace.svc"}
		require.NoError(t, ctrl.ApplyInstanceCertificate(t.Context(), cluster, instanceName, dnsNames))

		cluster.Spec.TLS = newIssuerConf("VaultClusterIssuer")
		require.NoError(t, ctrl.ApplyInstanceCertificate(t.Context(), cluster, instanceName, dnsNames))

		cert := &v1.Certificate{}
		require.NoError(t, client.Get(t.Context(), sigs.ObjectKey{Namespace: cluster.Namespace, Name: instanceName + "-cert"}, cert))
		assert.Equal(t, "vault-issuer", cert.Spec.IssuerRef.Name)
	})

	t.Run("pgbouncer certificate switches issuerRef on update", func(t *testing.T) {
		cluster := testCluster()
		cluster.Name = "drift-pgbouncer-cert"
		client := setupFakeClient(t, cluster)
		ctrl := NewController(client, client.Scheme(), false)

		dnsNames := []string{"drift-pgbouncer-cert-pgbouncer.test-namespace.svc"}
		require.NoError(t, ctrl.ApplyPGBouncerCertificate(t.Context(), cluster, dnsNames))

		cluster.Spec.TLS = newIssuerConf("VaultClusterIssuer")
		require.NoError(t, ctrl.ApplyPGBouncerCertificate(t.Context(), cluster, dnsNames))

		cert := &v1.Certificate{}
		require.NoError(t, client.Get(t.Context(), sigs.ObjectKey{Namespace: cluster.Namespace, Name: cluster.Name + "-pgbouncer-cert"}, cert))
		assert.Equal(t, "vault-issuer", cert.Spec.IssuerRef.Name)
	})

	t.Run("replication certificate switches issuerRef on update", func(t *testing.T) {
		cluster := testCluster()
		cluster.Name = "drift-replication-cert"
		client := setupFakeClient(t, cluster)
		ctrl := NewController(client, client.Scheme(), false)

		require.NoError(t, ctrl.ApplyReplicationCertificate(t.Context(), cluster))

		cluster.Spec.TLS = newIssuerConf("VaultClusterIssuer")
		require.NoError(t, ctrl.ApplyReplicationCertificate(t.Context(), cluster))

		cert := &v1.Certificate{}
		require.NoError(t, client.Get(t.Context(), sigs.ObjectKey{Namespace: cluster.Namespace, Name: cluster.Name + "-replication-cert"}, cert))
		assert.Equal(t, "vault-issuer", cert.Spec.IssuerRef.Name)
	})

	t.Run("pgbackrest client certificate switches issuerRef on update", func(t *testing.T) {
		cluster := testCluster()
		cluster.Name = "drift-pgbr-client-cert"
		client := setupFakeClient(t, cluster)
		ctrl := NewController(client, client.Scheme(), false)

		require.NoError(t, ctrl.ApplyPGBackRestClientCertificate(t.Context(), cluster))

		cluster.Spec.TLS = newIssuerConf("VaultClusterIssuer")
		require.NoError(t, ctrl.ApplyPGBackRestClientCertificate(t.Context(), cluster))

		cert := &v1.Certificate{}
		require.NoError(t, client.Get(t.Context(), sigs.ObjectKey{Namespace: cluster.Namespace, Name: cluster.Name + "-pgbackrest-client-cert"}, cert))
		assert.Equal(t, "vault-issuer", cert.Spec.IssuerRef.Name)
	})

	t.Run("pgbackrest repo certificate switches issuerRef on update", func(t *testing.T) {
		cluster := testCluster()
		cluster.Name = "drift-pgbr-repo-cert"
		client := setupFakeClient(t, cluster)
		ctrl := NewController(client, client.Scheme(), false)

		dnsNames := []string{cluster.Name + "-repo-host-0." + cluster.Name + "-pgbackrest.test-namespace.svc"}
		require.NoError(t, ctrl.ApplyPGBackRestRepoCertificate(t.Context(), cluster, dnsNames))

		cluster.Spec.TLS = newIssuerConf("VaultClusterIssuer")
		require.NoError(t, ctrl.ApplyPGBackRestRepoCertificate(t.Context(), cluster, dnsNames))

		cert := &v1.Certificate{}
		require.NoError(t, client.Get(t.Context(), sigs.ObjectKey{Namespace: cluster.Namespace, Name: cluster.Name + "-pgbackrest-repo-cert"}, cert))
		assert.Equal(t, "vault-issuer", cert.Spec.IssuerRef.Name)
	})
}
