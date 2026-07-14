// Copyright 2021 - 2024 Crunchy Data Solutions, Inc.
//
// SPDX-License-Identifier: Apache-2.0

package postgrescluster

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"

	cmv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	cmmeta "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	"github.com/pkg/errors"
	"gotest.tools/v3/assert"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/percona/percona-postgresql-operator/v2/internal/naming"
	"github.com/percona/percona-postgresql-operator/v2/internal/pki"
	"github.com/percona/percona-postgresql-operator/v2/internal/testing/require"
	"github.com/percona/percona-postgresql-operator/v2/percona/certmanager"
	"github.com/percona/percona-postgresql-operator/v2/pkg/apis/upstream.pgv2.percona.com/v1beta1"
)

// TestReconcileCerts tests the proper reconciliation of the root ca certificate
// secret, leaf certificate secrets and the updates that occur when updates are
// made to the cluster certificates generally. For the removal of ownership
// references and deletion of the root CA cert secret, a separate Kuttl test is
// used due to the need for proper garbage collection.
func TestReconcileCerts(t *testing.T) {
	// Garbage collector cleans up test resources before the test completes
	if strings.EqualFold(os.Getenv("USE_EXISTING_CLUSTER"), "true") {
		t.Skip("USE_EXISTING_CLUSTER: Test fails due to garbage collection")
	}

	_, tClient := setupKubernetes(t)
	require.ParallelCapacity(t, 2)
	ctx := context.Background()
	namespace := setupNamespace(t, tClient).Name

	r := &Reconciler{
		Client:              tClient,
		Owner:               ControllerName,
		CertManagerCtrlFunc: certmanager.NewController,
	}

	// set up cluster1
	clusterName1 := "hippocluster1"

	// set up test cluster1
	cluster1 := testCluster()
	cluster1.Name = clusterName1
	cluster1.Namespace = namespace
	if err := tClient.Create(ctx, cluster1); err != nil {
		t.Error(err)
	}

	// set up test cluster2
	cluster2Name := "hippocluster2"

	cluster2 := testCluster()
	cluster2.Name = cluster2Name
	cluster2.Namespace = namespace
	if err := tClient.Create(ctx, cluster2); err != nil {
		t.Error(err)
	}

	primaryService := new(corev1.Service)
	primaryService.Namespace = namespace
	primaryService.Name = "the-primary"

	replicaService := new(corev1.Service)
	replicaService.Namespace = namespace
	replicaService.Name = "the-replicas"

	t.Run("check root certificate reconciliation", func(t *testing.T) {
		initialRoot, err := r.reconcileRootCertificate(ctx, cluster1)
		assert.NilError(t, err)

		// K8SPG-553
		cluster1CASecret := &corev1.Secret{
			ObjectMeta: naming.PostgresRootCASecret(cluster1),
		}
		cluster1CASecret.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("Secret"))

		cluster2CASecret := &corev1.Secret{
			ObjectMeta: naming.PostgresRootCASecret(cluster2),
		}
		cluster2CASecret.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("Secret"))

		t.Run("check root CA secret for cluster1", func(t *testing.T) {
			err := tClient.Get(ctx, client.ObjectKeyFromObject(cluster1CASecret), cluster1CASecret)
			assert.NilError(t, err)

			assert.Check(t, len(cluster1CASecret.ObjectMeta.OwnerReferences) == 1, "first owner reference not set")

			expectedOR := metav1.OwnerReference{
				APIVersion:         "upstream.pgv2.percona.com/v1beta1",
				Kind:               "PostgresCluster",
				Name:               "hippocluster1",
				UID:                cluster1.UID,
				Controller:         new(true),
				BlockOwnerDeletion: new(true),
			}

			assert.Equal(t, cluster1CASecret.ObjectMeta.OwnerReferences[0].APIVersion, expectedOR.APIVersion)
			assert.Equal(t, cluster1CASecret.ObjectMeta.OwnerReferences[0].Kind, expectedOR.Kind)
			assert.Equal(t, cluster1CASecret.ObjectMeta.OwnerReferences[0].Name, expectedOR.Name)
			assert.Equal(t, *cluster1CASecret.ObjectMeta.OwnerReferences[0].Controller, true)
			assert.Equal(t, *cluster1CASecret.ObjectMeta.OwnerReferences[0].BlockOwnerDeletion, true)
		})

		t.Run("check root CA secret for cluster2", func(t *testing.T) {
			_, err := r.reconcileRootCertificate(ctx, cluster2)
			assert.NilError(t, err)

			err = tClient.Get(ctx, client.ObjectKeyFromObject(cluster2CASecret), cluster2CASecret)
			assert.NilError(t, err)

			clist := &v1beta1.PostgresClusterList{}
			assert.NilError(t, tClient.List(ctx, clist))

			assert.Check(t, len(cluster2CASecret.ObjectMeta.OwnerReferences) == 1, "should be single owner reference")

			expectedOR := metav1.OwnerReference{
				APIVersion:         "upstream.pgv2.percona.com/v1beta1",
				Kind:               "PostgresCluster",
				Name:               "hippocluster2",
				UID:                cluster2.UID,
				Controller:         new(true),
				BlockOwnerDeletion: new(true),
			}

			assert.Equal(t, cluster2CASecret.ObjectMeta.OwnerReferences[0].APIVersion, expectedOR.APIVersion)
			assert.Equal(t, cluster2CASecret.ObjectMeta.OwnerReferences[0].Kind, expectedOR.Kind)
			assert.Equal(t, cluster2CASecret.ObjectMeta.OwnerReferences[0].Name, expectedOR.Name)
			assert.Equal(t, *cluster2CASecret.ObjectMeta.OwnerReferences[0].Controller, true)
			assert.Equal(t, *cluster2CASecret.ObjectMeta.OwnerReferences[0].BlockOwnerDeletion, true)
		})

		t.Run("root certificate is returned correctly", func(t *testing.T) {
			rootCertMeta := naming.PostgresRootCASecret(cluster1)
			fromSecret, err := getCertFromSecret(ctx, tClient, rootCertMeta.Name, rootCertMeta.Namespace, "root.crt")
			assert.NilError(t, err)

			// assert returned certificate matches the one created earlier
			assert.DeepEqual(t, *fromSecret, initialRoot.Certificate)
		})

		t.Run("root certificate changes", func(t *testing.T) {
			// force the generation of a new root cert
			// create an empty secret and apply the change
			emptyRootSecret := &corev1.Secret{}
			emptyRootSecret.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("Secret"))
			rootCertMeta := naming.PostgresRootCASecret(cluster1)
			emptyRootSecret.ObjectMeta = rootCertMeta
			emptyRootSecret.Data = make(map[string][]byte)
			assert.NilError(t, r.apply(ctx, emptyRootSecret))

			// reconcile the root cert secret, creating a new root cert
			returnedRoot, err := r.reconcileRootCertificate(ctx, cluster1)
			assert.NilError(t, err)

			fromSecret, err := getCertFromSecret(ctx, tClient, rootCertMeta.Name, rootCertMeta.Namespace, "root.crt")
			assert.NilError(t, err)

			// check that the cert from the secret does not equal the initial certificate
			assert.Assert(t, !fromSecret.Equal(initialRoot.Certificate))

			// check that the returned cert matches the cert from the secret
			assert.DeepEqual(t, *fromSecret, returnedRoot.Certificate)
		})
	})

	t.Run("check leaf certificate reconciliation", func(t *testing.T) {
		initialRoot, err := r.reconcileRootCertificate(ctx, cluster1)
		assert.NilError(t, err)

		// instance with minimal required fields
		instance := &appsv1.StatefulSet{
			TypeMeta: metav1.TypeMeta{
				APIVersion: appsv1.SchemeGroupVersion.String(),
				Kind:       "StatefulSet",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      clusterName1,
				Namespace: namespace,
			},
			Spec: appsv1.StatefulSetSpec{
				ServiceName: clusterName1,
			},
		}

		t.Run("check leaf certificate in secret", func(t *testing.T) {
			existing := &corev1.Secret{Data: make(map[string][]byte)}
			intent := &corev1.Secret{Data: make(map[string][]byte)}

			initialLeafCert, err := r.instanceCertificate(ctx, instance, existing, intent, initialRoot, "")
			assert.NilError(t, err)

			fromSecret := &pki.LeafCertificate{}
			assert.NilError(t, fromSecret.Certificate.UnmarshalText(intent.Data["dns.crt"]))
			assert.NilError(t, fromSecret.PrivateKey.UnmarshalText(intent.Data["dns.key"]))

			assert.DeepEqual(t, fromSecret, initialLeafCert)
		})

		t.Run("check that the leaf certs update when root changes", func(t *testing.T) {
			// force the generation of a new root cert
			// create an empty secret and apply the change
			emptyRootSecret := &corev1.Secret{}
			emptyRootSecret.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("Secret"))
			rootCertMeta := naming.PostgresRootCASecret(cluster1)
			emptyRootSecret.ObjectMeta = rootCertMeta
			emptyRootSecret.Data = make(map[string][]byte)
			assert.NilError(t, r.apply(ctx, emptyRootSecret))

			// reconcile the root cert secret
			newRootCert, err := r.reconcileRootCertificate(ctx, cluster1)
			assert.NilError(t, err)

			existing := &corev1.Secret{Data: make(map[string][]byte)}
			intent := &corev1.Secret{Data: make(map[string][]byte)}

			initialLeaf, err := r.instanceCertificate(ctx, instance, existing, intent, initialRoot, "")
			assert.NilError(t, err)

			// reconcile the certificate
			newLeaf, err := r.instanceCertificate(ctx, instance, existing, intent, newRootCert, "")
			assert.NilError(t, err)

			// assert old leaf cert does not match the newly reconciled one
			assert.Assert(t, !initialLeaf.Certificate.Equal(newLeaf.Certificate))

			// 'reconcile' the certificate when the secret does not change. The returned leaf certificate should not change
			newLeaf2, err := r.instanceCertificate(ctx, instance, intent, intent, newRootCert, "")
			assert.NilError(t, err)

			// check that the leaf cert did not change after another reconciliation
			assert.DeepEqual(t, newLeaf2, newLeaf)
		})
	})

	t.Run("check cluster certificate secret reconciliation", func(t *testing.T) {
		// example auto-generated secret projection
		testSecretProjection := &corev1.SecretProjection{
			LocalObjectReference: corev1.LocalObjectReference{
				Name: fmt.Sprintf(naming.ClusterCertSecret, cluster1.Name),
			},
			Items: []corev1.KeyToPath{
				{
					Key:  clusterCertFile,
					Path: clusterCertFile,
				},
				{
					Key:  clusterKeyFile,
					Path: clusterKeyFile,
				},
				{
					Key:  rootCertFile,
					Path: rootCertFile,
				},
			},
		}

		// example custom secret projection
		customSecretProjection := &corev1.SecretProjection{
			LocalObjectReference: corev1.LocalObjectReference{
				Name: "customsecret",
			},
			Items: []corev1.KeyToPath{
				{
					Key:  clusterCertFile,
					Path: clusterCertFile,
				},
				{
					Key:  clusterKeyFile,
					Path: clusterKeyFile,
				},
				{
					Key:  rootCertFile,
					Path: rootCertFile,
				},
			},
		}

		cluster2.Spec.CustomTLSSecret = customSecretProjection

		cluster1Root, err := r.reconcileRootCertificate(ctx, cluster1)
		assert.NilError(t, err)

		cluster2Root, err := r.reconcileRootCertificate(ctx, cluster2)
		assert.NilError(t, err)

		t.Run("check standard secret projection", func(t *testing.T) {
			secretCertProj, err := r.reconcileClusterCertificate(ctx, cluster1Root, cluster1, primaryService, replicaService)
			assert.NilError(t, err)

			assert.DeepEqual(t, testSecretProjection, secretCertProj)
		})

		t.Run("check custom secret projection", func(t *testing.T) {
			customSecretCertProj, err := r.reconcileClusterCertificate(ctx, cluster2Root, cluster2, primaryService, replicaService)
			assert.NilError(t, err)

			assert.DeepEqual(t, customSecretProjection, customSecretCertProj)
		})

		t.Run("check switch to a custom secret projection", func(t *testing.T) {
			// simulate a new custom secret
			testSecret := &corev1.Secret{}
			testSecret.Namespace, testSecret.Name = namespace, "newcustomsecret"
			// simulate cluster spec update
			cluster2.Spec.CustomTLSSecret.LocalObjectReference.Name = "newcustomsecret"

			// get the expected secret projection
			testSecretProjection := clusterCertSecretProjection(testSecret)

			// reconcile the secret project using the normal process
			customSecretCertProj, err := r.reconcileClusterCertificate(ctx, cluster2Root, cluster2, primaryService, replicaService)
			assert.NilError(t, err)

			// results should be the same
			assert.DeepEqual(t, testSecretProjection, customSecretCertProj)
		})

		t.Run("check cluster certificate secret", func(t *testing.T) {
			// get the cluster cert secret
			initialClusterCertSecret := &corev1.Secret{}
			err := tClient.Get(ctx, types.NamespacedName{
				Name:      fmt.Sprintf(naming.ClusterCertSecret, cluster1.Name),
				Namespace: namespace,
			}, initialClusterCertSecret)
			assert.NilError(t, err)

			// force the generation of a new root cert
			// create an empty secret and apply the change
			emptyRootSecret := &corev1.Secret{}
			emptyRootSecret.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("Secret"))
			rootCertMeta := naming.PostgresRootCASecret(cluster1)
			emptyRootSecret.ObjectMeta = rootCertMeta
			emptyRootSecret.Data = make(map[string][]byte)
			assert.NilError(t, r.apply(ctx, emptyRootSecret))

			// reconcile the root cert secret, creating a new root cert
			returnedRoot, err := r.reconcileRootCertificate(ctx, cluster1)
			assert.NilError(t, err)

			// pass in the new root, which should result in a new cluster cert
			_, err = r.reconcileClusterCertificate(ctx, returnedRoot, cluster1, primaryService, replicaService)
			assert.NilError(t, err)

			// get the new cluster cert secret
			newClusterCertSecret := &corev1.Secret{}
			err = tClient.Get(ctx, types.NamespacedName{
				Name:      fmt.Sprintf(naming.ClusterCertSecret, cluster1.Name),
				Namespace: namespace,
			}, newClusterCertSecret)
			assert.NilError(t, err)

			assert.Assert(t, !reflect.DeepEqual(initialClusterCertSecret, newClusterCertSecret))

			leaf := &pki.LeafCertificate{}
			assert.NilError(t, leaf.Certificate.UnmarshalText(newClusterCertSecret.Data["tls.crt"]))
			assert.NilError(t, leaf.PrivateKey.UnmarshalText(newClusterCertSecret.Data["tls.key"]))

			assert.Assert(t,
				strings.HasPrefix(leaf.Certificate.CommonName(), "the-primary."+namespace+".svc."),
				"got %q", leaf.Certificate.CommonName())

			if dnsNames := leaf.Certificate.DNSNames(); assert.Check(t, len(dnsNames) > 1) {
				assert.DeepEqual(t, dnsNames[1:4], []string{
					"the-primary." + namespace + ".svc",
					"the-primary." + namespace,
					"the-primary",
				})
				assert.DeepEqual(t, dnsNames[5:8], []string{
					"the-replicas." + namespace + ".svc",
					"the-replicas." + namespace,
					"the-replicas",
				})
			}
		})
	})
}

type mockCertManagerController struct{}

func (m *mockCertManagerController) Check(context.Context, *rest.Config, string) error { return nil }
func (m *mockCertManagerController) CertificateExists(context.Context, string, string) (bool, error) {
	return false, nil
}
func (m *mockCertManagerController) ApplyIssuer(context.Context, *v1beta1.PostgresCluster) error {
	panic("unexpected call")
}
func (m *mockCertManagerController) ApplyCAIssuer(context.Context, *v1beta1.PostgresCluster) error {
	panic("unexpected call")
}
func (m *mockCertManagerController) ApplyCACertificate(context.Context, *v1beta1.PostgresCluster) error {
	panic("unexpected call")
}
func (m *mockCertManagerController) ApplyClusterCertificate(context.Context, *v1beta1.PostgresCluster, []string) error {
	panic("unexpected call")
}
func (m *mockCertManagerController) ApplyInstanceCertificate(context.Context, *v1beta1.PostgresCluster, string, []string) error {
	panic("unexpected call")
}
func (m *mockCertManagerController) ApplyPGBouncerCertificate(context.Context, *v1beta1.PostgresCluster, []string) error {
	panic("unexpected call")
}
func (m *mockCertManagerController) ApplyReplicationCertificate(context.Context, *v1beta1.PostgresCluster) error {
	panic("unexpected call")
}
func (m *mockCertManagerController) ApplyPGBackRestClientCertificate(context.Context, *v1beta1.PostgresCluster) error {
	panic("unexpected call")
}
func (m *mockCertManagerController) ApplyPGBackRestRepoCertificate(context.Context, *v1beta1.PostgresCluster, []string) error {
	panic("unexpected call")
}

func mockCertManagerCtrlFunc(_ client.Client, _ *runtime.Scheme, _ bool) certmanager.Controller {
	return &mockCertManagerController{}
}

// recoveryCertManagerController reports the cert-manager Certificate object as
// existing and records which Apply* methods are invoked. It drives the
// K8SPG-1007 recovery branches that reconcile a stale Certificate left behind
// by the K8SPG-1017 bug, when the cluster otherwise uses internal PKI.
type recoveryCertManagerController struct {
	applyIssuerCalls               int
	applyClusterCertificateCalls   int
	applyInstanceCertificateCalls  int
	applyPGBouncerCertificateCalls int
	applyReplicationCalls          int
	applyPGBackRestClientCalls     int
}

func (m *recoveryCertManagerController) Check(context.Context, *rest.Config, string) error {
	return nil
}
func (m *recoveryCertManagerController) CertificateExists(context.Context, string, string) (bool, error) {
	return true, nil
}
func (m *recoveryCertManagerController) ApplyIssuer(context.Context, *v1beta1.PostgresCluster) error {
	m.applyIssuerCalls++
	return nil
}
func (m *recoveryCertManagerController) ApplyCAIssuer(context.Context, *v1beta1.PostgresCluster) error {
	return nil
}
func (m *recoveryCertManagerController) ApplyCACertificate(context.Context, *v1beta1.PostgresCluster) error {
	return nil
}
func (m *recoveryCertManagerController) ApplyClusterCertificate(context.Context, *v1beta1.PostgresCluster, []string) error {
	m.applyClusterCertificateCalls++
	return nil
}
func (m *recoveryCertManagerController) ApplyInstanceCertificate(context.Context, *v1beta1.PostgresCluster, string, []string) error {
	m.applyInstanceCertificateCalls++
	return nil
}
func (m *recoveryCertManagerController) ApplyPGBouncerCertificate(context.Context, *v1beta1.PostgresCluster, []string) error {
	m.applyPGBouncerCertificateCalls++
	return nil
}
func (m *recoveryCertManagerController) ApplyReplicationCertificate(context.Context, *v1beta1.PostgresCluster) error {
	m.applyReplicationCalls++
	return nil
}
func (m *recoveryCertManagerController) ApplyPGBackRestClientCertificate(context.Context, *v1beta1.PostgresCluster) error {
	m.applyPGBackRestClientCalls++
	return nil
}
func (m *recoveryCertManagerController) ApplyPGBackRestRepoCertificate(context.Context, *v1beta1.PostgresCluster, []string) error {
	return nil
}

func TestUpgradeCertManagerDoesNotTakeOverInternalPKI(t *testing.T) {
	if strings.EqualFold(os.Getenv("USE_EXISTING_CLUSTER"), "true") {
		t.Skip("USE_EXISTING_CLUSTER: Test fails due to garbage collection")
	}

	_, tClient := setupKubernetes(t)
	require.ParallelCapacity(t, 1)
	ctx := t.Context()
	namespace := require.Namespace(t, tClient).Name

	reconcilerWithoutCertManager := &Reconciler{
		Client:              tClient,
		Owner:               ControllerName,
		CertManagerCtrlFunc: certmanager.NewController,
	}

	cluster := testCluster()
	cluster.Name = "upgrade-test"
	cluster.Namespace = namespace
	assert.NilError(t, tClient.Create(ctx, cluster))

	root, err := reconcilerWithoutCertManager.reconcileRootCertificate(ctx, cluster)
	assert.NilError(t, err)

	primaryService := &corev1.Service{ObjectMeta: metav1.ObjectMeta{
		Namespace: namespace, Name: "upgrade-test-primary",
	}}
	replicaService := &corev1.Service{ObjectMeta: metav1.ObjectMeta{
		Namespace: namespace, Name: "upgrade-test-replicas",
	}}

	_, err = reconcilerWithoutCertManager.reconcileClusterCertificate(ctx, root, cluster, primaryService, replicaService)
	assert.NilError(t, err)

	rootSecret := &corev1.Secret{}
	assert.NilError(t, tClient.Get(ctx, types.NamespacedName{
		Name:      naming.PostgresRootCASecret(cluster).Name,
		Namespace: namespace,
	}, rootSecret))
	assert.Equal(t, rootSecret.Annotations["cert-manager.io/certificate-name"], "")

	clusterCertSecret := &corev1.Secret{}
	assert.NilError(t, tClient.Get(ctx, types.NamespacedName{
		Name:      fmt.Sprintf(naming.ClusterCertSecret, cluster.Name),
		Namespace: namespace,
	}, clusterCertSecret))
	originalCertData := clusterCertSecret.Data["tls.crt"]

	reconcilerWithCertManager := &Reconciler{
		Client:              tClient,
		Owner:               ControllerName,
		CertManagerCtrlFunc: mockCertManagerCtrlFunc,
		RestConfig:          &rest.Config{},
	}

	t.Run("isRootCACertManagerManaged returns false for internal PKI root", func(t *testing.T) {
		managed, err := reconcilerWithCertManager.isRootCACertManagerManaged(ctx, cluster)
		assert.NilError(t, err)
		assert.Assert(t, !managed)
	})

	t.Run("reconcileClusterCertificate uses internal PKI after upgrade", func(t *testing.T) {
		_, err := reconcilerWithCertManager.reconcileClusterCertificate(ctx, root, cluster, primaryService, replicaService)
		assert.NilError(t, err)

		updatedCertSecret := &corev1.Secret{}
		assert.NilError(t, tClient.Get(ctx, types.NamespacedName{
			Name:      fmt.Sprintf(naming.ClusterCertSecret, cluster.Name),
			Namespace: namespace,
		}, updatedCertSecret))

		assert.DeepEqual(t, originalCertData, updatedCertSecret.Data["tls.crt"])
	})

	t.Run("shouldReconcileCertManagerCertificate", func(t *testing.T) {
		t.Run("returns false when cert-manager is not installed", func(t *testing.T) {
			// RestConfig is nil, so isCertManagerInstalled short-circuits to false.
			r := &Reconciler{
				Client:              tClient,
				Owner:               ControllerName,
				CertManagerCtrlFunc: mockCertManagerCtrlFunc,
			}
			assert.Assert(t, !r.shouldReconcileCertManagerCertificate(ctx, namespace, "any-cert"))
		})

		t.Run("returns false when cert-manager Certificate does not exist", func(t *testing.T) {
			// The default mockCertManagerController reports CertificateExists=false.
			r := &Reconciler{
				Client:              tClient,
				Owner:               ControllerName,
				CertManagerCtrlFunc: mockCertManagerCtrlFunc,
				RestConfig:          &rest.Config{},
			}
			assert.Assert(t, !r.shouldReconcileCertManagerCertificate(ctx, namespace, "missing-cert"))
		})

		t.Run("returns true when cert-manager is installed and Certificate exists", func(t *testing.T) {
			r := &Reconciler{
				Client: tClient,
				Owner:  ControllerName,
				CertManagerCtrlFunc: func(_ client.Client, _ *runtime.Scheme, _ bool) certmanager.Controller {
					return &recoveryCertManagerController{}
				},
				RestConfig: &rest.Config{},
			}
			assert.Assert(t, r.shouldReconcileCertManagerCertificate(ctx, namespace, "any-cert"))
		})
	})

	t.Run("reconcileClusterCertificate recovers stale cert-manager Certificate (K8SPG-1007)", func(t *testing.T) {
		// Internal PKI is in use (root CA secret has no cert-manager annotation),
		// but a stale Certificate CR was left behind by K8SPG-1017. The reconciler
		// should reconcile it to update ownerRefs, then still write the internal
		// leaf secret.
		recovery := &recoveryCertManagerController{}
		r := &Reconciler{
			Client: tClient,
			Owner:  ControllerName,
			CertManagerCtrlFunc: func(_ client.Client, _ *runtime.Scheme, _ bool) certmanager.Controller {
				return recovery
			},
			RestConfig: &rest.Config{},
		}

		preCertData := clusterCertSecret.Data["tls.crt"]
		_, err := r.reconcileClusterCertificate(ctx, root, cluster, primaryService, replicaService)
		assert.NilError(t, err)

		assert.Equal(t, recovery.applyIssuerCalls, 1)
		assert.Equal(t, recovery.applyClusterCertificateCalls, 1)

		// Internal leaf is still written by the fall-through path.
		updatedCertSecret := &corev1.Secret{}
		assert.NilError(t, tClient.Get(ctx, types.NamespacedName{
			Name:      fmt.Sprintf(naming.ClusterCertSecret, cluster.Name),
			Namespace: namespace,
		}, updatedCertSecret))
		assert.DeepEqual(t, preCertData, updatedCertSecret.Data["tls.crt"])
	})

	t.Run("reconcileInstanceCertificates recovers stale cert-manager Certificate (K8SPG-1007)", func(t *testing.T) {
		recovery := &recoveryCertManagerController{}
		r := &Reconciler{
			Client: tClient,
			Owner:  ControllerName,
			CertManagerCtrlFunc: func(_ client.Client, _ *runtime.Scheme, _ bool) certmanager.Controller {
				return recovery
			},
			RestConfig: &rest.Config{},
		}

		instance := &appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      cluster.Name + "-instance1-abcd",
		}}
		instance.Spec.ServiceName = cluster.Name + "-pods"
		spec := &cluster.Spec.InstanceSets[0]

		returned, err := r.reconcileInstanceCertificates(ctx, cluster, spec, instance, root)
		assert.NilError(t, err)
		assert.Equal(t, recovery.applyInstanceCertificateCalls, 1)
		// Fall-through wrote the internal-PKI leaf into the instance certs secret.
		assert.Assert(t, len(returned.Data) > 0)
	})

	t.Run("reconcileReplicationSecret recovers stale cert-manager Certificate (K8SPG-1007)", func(t *testing.T) {
		recovery := &recoveryCertManagerController{}
		r := &Reconciler{
			Client: tClient,
			Owner:  ControllerName,
			CertManagerCtrlFunc: func(_ client.Client, _ *runtime.Scheme, _ bool) certmanager.Controller {
				return recovery
			},
			RestConfig: &rest.Config{},
		}

		returned, err := r.reconcileReplicationSecret(ctx, cluster, root)
		assert.NilError(t, err)
		assert.Equal(t, recovery.applyReplicationCalls, 1)
		// Fall-through wrote the internal-PKI replication leaf.
		assert.Assert(t, len(returned.Data) > 0)
	})

	t.Run("reconcilePGBackRestSecret recovers stale cert-manager Certificate (K8SPG-1007)", func(t *testing.T) {
		recovery := &recoveryCertManagerController{}
		r := &Reconciler{
			Client: tClient,
			Owner:  ControllerName,
			CertManagerCtrlFunc: func(_ client.Client, _ *runtime.Scheme, _ bool) certmanager.Controller {
				return recovery
			},
			RestConfig: &rest.Config{},
		}

		repoHost := &appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      cluster.Name + "-repo-host",
		}}
		repoHost.Spec.ServiceName = cluster.Name + "-pods"

		assert.NilError(t, r.reconcilePGBackRestSecret(ctx, cluster, repoHost, root))
		assert.Equal(t, recovery.applyPGBackRestClientCalls, 1)
		// Fall-through wrote the internal-PKI pgBackRest secret to the API.
		pgbackrestSecret := &corev1.Secret{}
		assert.NilError(t, tClient.Get(ctx, types.NamespacedName{
			Name:      naming.PGBackRestSecret(cluster).Name,
			Namespace: namespace,
		}, pgbackrestSecret))
		assert.Assert(t, len(pgbackrestSecret.Data) > 0)
	})

	t.Run("reconcilePGBouncerSecret recovers stale cert-manager Certificate (K8SPG-1007)", func(t *testing.T) {
		recovery := &recoveryCertManagerController{}
		r := &Reconciler{
			Client: tClient,
			Owner:  ControllerName,
			CertManagerCtrlFunc: func(_ client.Client, _ *runtime.Scheme, _ bool) certmanager.Controller {
				return recovery
			},
			RestConfig: &rest.Config{},
		}

		pgbouncerService := &corev1.Service{ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      cluster.Name + "-pgbouncer",
		}}

		returned, err := r.reconcilePGBouncerSecret(ctx, cluster, root, pgbouncerService)
		assert.NilError(t, err)
		assert.Equal(t, recovery.applyPGBouncerCertificateCalls, 1)
		// Fall-through populated the pgbouncer secret from internal PKI.
		assert.Assert(t, len(returned.Data) > 0)
	})

	t.Run("isRootCACertManagerManaged returns true for cert-manager root", func(t *testing.T) {
		rootSecret.Annotations = map[string]string{
			"cert-manager.io/certificate-name": "test-ca-cert",
		}
		assert.NilError(t, tClient.Update(ctx, rootSecret))

		managed, err := reconcilerWithCertManager.isRootCACertManagerManaged(ctx, cluster)
		assert.NilError(t, err)
		assert.Assert(t, managed)
	})

	t.Run("isRootCACertManagerManaged returns true when no root CA exists", func(t *testing.T) {
		freshCluster := testCluster()
		freshCluster.Name = "fresh-cluster"
		freshCluster.Namespace = namespace
		assert.NilError(t, tClient.Create(ctx, freshCluster))

		managed, err := reconcilerWithCertManager.isRootCACertManagerManaged(ctx, freshCluster)
		assert.NilError(t, err)
		assert.Assert(t, managed)
	})

	t.Run("isRootCACertManagerManaged returns false when CustomRootCATLSSecret is set", func(t *testing.T) {
		clusterWithCustomRoot := testCluster()
		clusterWithCustomRoot.Name = cluster.Name
		clusterWithCustomRoot.Namespace = namespace
		clusterWithCustomRoot.Spec.CustomRootCATLSSecret = &corev1.SecretProjection{
			LocalObjectReference: corev1.LocalObjectReference{Name: "my-custom-root-ca"},
		}

		managed, err := reconcilerWithCertManager.isRootCACertManagerManaged(ctx, clusterWithCustomRoot)
		assert.NilError(t, err)
		assert.Assert(t, !managed)
	})

	t.Run("isRootCACertManagerManaged returns false when cert-manager not installed", func(t *testing.T) {
		rNoCertManager := &Reconciler{
			Client:              tClient,
			Owner:               ControllerName,
			CertManagerCtrlFunc: certmanager.NewController,
		}

		managed, err := rNoCertManager.isRootCACertManagerManaged(ctx, cluster)
		assert.NilError(t, err)
		assert.Assert(t, !managed)
	})
}

// getCertFromSecret returns a parsed certificate from the named secret
func getCertFromSecret(
	ctx context.Context, tClient client.Client, name, namespace, dataKey string,
) (*pki.Certificate, error) {
	// get cert secret
	secret := &corev1.Secret{}
	if err := tClient.Get(ctx, types.NamespacedName{
		Name:      name,
		Namespace: namespace,
	}, secret); err != nil {
		return nil, err
	}

	// get the cert from the secret
	secretCRT, ok := secret.Data[dataKey]
	if !ok {
		return nil, errors.Errorf("could not retrieve %s", dataKey)
	}

	// parse the cert from binary encoded data
	fromSecret := &pki.Certificate{}
	return fromSecret, fromSecret.UnmarshalText(secretCRT)
}

// installedCertManagerController reports cert-manager as installed and lets
// every Apply* call succeed as a no-op — used for K8SPG-951 issuer-mode
// tests that only need isCertManagerInstalled to return true, not real
// Issuer/Certificate object creation (envtest has no cert-manager CRDs).
type installedCertManagerController struct{}

func (installedCertManagerController) Check(context.Context, *rest.Config, string) error {
	return nil
}
func (installedCertManagerController) CertificateExists(context.Context, string, string) (bool, error) {
	return false, nil
}
func (installedCertManagerController) ApplyIssuer(context.Context, *v1beta1.PostgresCluster) error {
	return nil
}
func (installedCertManagerController) ApplyCAIssuer(context.Context, *v1beta1.PostgresCluster) error {
	return nil
}
func (installedCertManagerController) ApplyCACertificate(context.Context, *v1beta1.PostgresCluster) error {
	return nil
}
func (installedCertManagerController) ApplyClusterCertificate(context.Context, *v1beta1.PostgresCluster, []string) error {
	return nil
}
func (installedCertManagerController) ApplyInstanceCertificate(context.Context, *v1beta1.PostgresCluster, string, []string) error {
	return nil
}
func (installedCertManagerController) ApplyPGBouncerCertificate(context.Context, *v1beta1.PostgresCluster, []string) error {
	return nil
}
func (installedCertManagerController) ApplyReplicationCertificate(context.Context, *v1beta1.PostgresCluster) error {
	return nil
}
func (installedCertManagerController) ApplyPGBackRestClientCertificate(context.Context, *v1beta1.PostgresCluster) error {
	return nil
}
func (installedCertManagerController) ApplyPGBackRestRepoCertificate(context.Context, *v1beta1.PostgresCluster, []string) error {
	return nil
}

func installedCertManagerCtrlFunc(_ client.Client, _ *runtime.Scheme, _ bool) certmanager.Controller {
	return installedCertManagerController{}
}

func TestIssuerModeAwareness(t *testing.T) {
	_, tClient := setupKubernetes(t)
	require.ParallelCapacity(t, 1)
	ctx := t.Context()
	namespace := require.Namespace(t, tClient).Name

	t.Run("reconcileRootCertificate returns nil for external issuer", func(t *testing.T) {
		cluster := testCluster()
		cluster.Name = "external-root-cert"
		cluster.Namespace = namespace
		cluster.Spec.TLS = &v1beta1.TLSSpec{
			IssuerConf: &cmmeta.IssuerReference{Name: "vault-issuer", Kind: "VaultClusterIssuer"},
		}
		assert.NilError(t, tClient.Create(ctx, cluster))

		r := &Reconciler{
			Client:              tClient,
			Owner:               ControllerName,
			CertManagerCtrlFunc: certmanager.NewController,
		}

		root, err := r.reconcileRootCertificate(ctx, cluster)
		assert.NilError(t, err)
		assert.Assert(t, root == nil)
	})

	t.Run("isRootCACertManagerManaged errors when cert-manager missing and issuerConf is set", func(t *testing.T) {
		cluster := testCluster()
		cluster.Name = "external-no-certmanager"
		cluster.Namespace = namespace
		cluster.Spec.TLS = &v1beta1.TLSSpec{
			IssuerConf: &cmmeta.IssuerReference{Name: "vault-issuer", Kind: "VaultClusterIssuer"},
		}
		assert.NilError(t, tClient.Create(ctx, cluster))

		r := &Reconciler{
			Client:              tClient,
			Owner:               ControllerName,
			CertManagerCtrlFunc: mockCertManagerCtrlFunc,
			RestConfig:          nil, // isCertManagerInstalled short-circuits to false
		}

		_, err := r.isRootCACertManagerManaged(ctx, cluster)
		assert.ErrorContains(t, err, "cert-manager is required")
	})

	t.Run("isRootCACertManagerManaged returns true immediately for external issuer when cert-manager installed", func(t *testing.T) {
		cluster := testCluster()
		cluster.Name = "external-with-certmanager"
		cluster.Namespace = namespace
		cluster.Spec.TLS = &v1beta1.TLSSpec{
			IssuerConf: &cmmeta.IssuerReference{Name: "vault-issuer", Kind: "VaultClusterIssuer"},
		}
		assert.NilError(t, tClient.Create(ctx, cluster))

		r := &Reconciler{
			Client:              tClient,
			Owner:               ControllerName,
			CertManagerCtrlFunc: installedCertManagerCtrlFunc,
			RestConfig:          &rest.Config{},
		}

		managed, err := r.isRootCACertManagerManaged(ctx, cluster)
		assert.NilError(t, err)
		assert.Assert(t, managed)
	})

	t.Run("isRootCACertManagerManaged returns true immediately for managed cluster issuer when cert-manager installed", func(t *testing.T) {
		cluster := testCluster()
		cluster.Name = "cluster-scoped-with-certmanager"
		cluster.Namespace = namespace
		cluster.Spec.TLS = &v1beta1.TLSSpec{
			IssuerConf: &cmmeta.IssuerReference{Name: "shared-tls-issuer", Kind: cmv1.ClusterIssuerKind},
		}
		assert.NilError(t, tClient.Create(ctx, cluster))

		// certmanager.ResolveIssuerMode issues a live Get for the named
		// ClusterIssuer. The shared envtest scheme used by setupKubernetes(t)
		// (internal/controller/runtime.Scheme) does not register cert-manager
		// types, and envtest has no cert-manager CRDs installed either, so
		// tClient can't serve that Get (pre-existing gap, out of scope for
		// this task's file set). A scheme-complete fake client stands in for
		// tClient as this Reconciler's Client so the ClusterIssuer Get
		// resolves to NotFound (mode ManagedCluster), matching what happens
		// against a real cluster where the operator can read the API but the
		// ClusterIssuer doesn't exist yet.
		certManagerScheme := runtime.NewScheme()
		assert.NilError(t, cmv1.AddToScheme(certManagerScheme))
		fakeClient := fake.NewClientBuilder().WithScheme(certManagerScheme).Build()

		r := &Reconciler{
			Client:              fakeClient,
			Owner:               ControllerName,
			CertManagerCtrlFunc: installedCertManagerCtrlFunc,
			RestConfig:          &rest.Config{},
		}

		managed, err := r.isRootCACertManagerManaged(ctx, cluster)
		assert.NilError(t, err)
		assert.Assert(t, managed)
	})

	t.Run("reconcileCertManagerClusterCertificate skips ApplyIssuer for external issuer", func(t *testing.T) {
		cluster := testCluster()
		cluster.Name = "external-cluster-cert"
		cluster.Namespace = namespace
		cluster.Spec.TLS = &v1beta1.TLSSpec{
			IssuerConf: &cmmeta.IssuerReference{Name: "vault-issuer", Kind: "VaultClusterIssuer"},
		}
		assert.NilError(t, tClient.Create(ctx, cluster))

		recovery := &recoveryCertManagerController{}
		r := &Reconciler{
			Client: tClient,
			Owner:  ControllerName,
			CertManagerCtrlFunc: func(_ client.Client, _ *runtime.Scheme, _ bool) certmanager.Controller {
				return recovery
			},
		}

		primaryService := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: "external-cluster-cert-primary"}}
		replicaService := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: "external-cluster-cert-replicas"}}

		_, err := r.reconcileCertManagerClusterCertificate(ctx, nil, cluster, primaryService, replicaService)
		assert.NilError(t, err)
		assert.Equal(t, recovery.applyIssuerCalls, 0)
	})
}
