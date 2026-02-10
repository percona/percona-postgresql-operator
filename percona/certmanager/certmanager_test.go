package certmanager

import (
	"context"
	"fmt"
	"testing"

	v1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	"github.com/cert-manager/cert-manager/pkg/util/cmapichecker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	sigs "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/percona/percona-postgresql-operator/v2/internal/naming"
	"github.com/percona/percona-postgresql-operator/v2/pkg/apis/postgres-operator.crunchydata.com/v1beta1"
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

	t.Run("issuer is cluster-scoped and cannot have namespace-scoped owner", func(t *testing.T) {
		err := ctrl.ApplyIssuer(t.Context(), cluster)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to set controller reference")
	})
}

func TestApplyCAIssuer(t *testing.T) {
	cluster := testCluster()
	cluster.Name = "ca-test-cluster"
	client := setupFakeClient(t, cluster)

	ctrl := NewController(client, client.Scheme(), false)

	t.Run("CA issuer is cluster-scoped and cannot have namespace-scoped owner", func(t *testing.T) {
		err := ctrl.ApplyCAIssuer(t.Context(), cluster)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to set controller reference")
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

		require.Len(t, cert.OwnerReferences, 1)
		assert.Equal(t, cluster.Name, cert.OwnerReferences[0].Name)

		// fail when CA certificate already exists
		err = ctrl.ApplyCACertificate(t.Context(), cluster)
		require.Error(t, err)
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

		assert.Contains(t, cert.Spec.Usages, v1.UsageServerAuth)
		assert.Contains(t, cert.Spec.Usages, v1.UsageClientAuth)
		assert.Contains(t, cert.Spec.Usages, v1.UsageDigitalSignature)
		assert.Contains(t, cert.Spec.Usages, v1.UsageKeyEncipherment)

		assert.NotNil(t, cert.Spec.SecretTemplate)
		assert.NotEmpty(t, cert.Spec.SecretTemplate.Labels)
		assert.Equal(t, cluster.Name, cert.Spec.SecretTemplate.Labels[naming.LabelCluster])
		assert.Equal(t, "postgres-tls", cert.Spec.SecretTemplate.Labels[naming.LabelClusterCertificate])

		require.Len(t, cert.OwnerReferences, 1)
		assert.Equal(t, cluster.Name, cert.OwnerReferences[0].Name)

		// return nil when certificate already exists
		dnsNames = []string{"test.example.com"}
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

		require.Len(t, cert.OwnerReferences, 1)
		assert.Equal(t, cluster.Name, cert.OwnerReferences[0].Name)
	})

	t.Run("return nil when instance certificate already exists", func(t *testing.T) {
		instanceName := "test-cluster-instance-0"
		dnsNames := []string{"test.example.com"}
		err := ctrl.ApplyInstanceCertificate(t.Context(), cluster, instanceName, dnsNames)
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
		assert.Equal(t, dnsNames, cert.Spec.DNSNames)
		assert.Equal(t, naming.TLSIssuer(cluster).Name, cert.Spec.IssuerRef.Name)
		assert.Equal(t, v1.IssuerKind, cert.Spec.IssuerRef.Kind)
		assert.NotNil(t, cert.Spec.Duration)
		assert.Equal(t, DefaultCertDuration, cert.Spec.Duration.Duration)

		assert.NotNil(t, cert.Spec.SecretTemplate)
		assert.Equal(t, cluster.Name, cert.Spec.SecretTemplate.Labels[naming.LabelCluster])
		assert.Equal(t, naming.RolePGBouncer, cert.Spec.SecretTemplate.Labels[naming.LabelRole])

		require.Len(t, cert.OwnerReferences, 1)
		assert.Equal(t, cluster.Name, cert.OwnerReferences[0].Name)

		// return nil when pgbouncer certificate already exists
		dnsNames = []string{"test.example.com"}
		err = ctrl.ApplyPGBouncerCertificate(t.Context(), cluster, dnsNames)
		require.NoError(t, err)
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
