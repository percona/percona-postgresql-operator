// Copyright 2021 - 2024 Crunchy Data Solutions, Inc.
//
// SPDX-License-Identifier: Apache-2.0

package postgrescluster

import (
	"context"

	gover "github.com/hashicorp/go-version"
	"github.com/pkg/errors"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/percona/percona-postgresql-operator/internal/naming"
	"github.com/percona/percona-postgresql-operator/internal/pki"
	"github.com/percona/percona-postgresql-operator/pkg/apis/postgres-operator.crunchydata.com/v1beta1"
)

const (
	// https://www.postgresql.org/docs/current/ssl-tcp.html
	clusterCertFile = "tls.crt"
	clusterKeyFile  = "tls.key"
	rootCertFile    = "ca.crt"
)

// +kubebuilder:rbac:groups="",resources="secrets",verbs={get}
// +kubebuilder:rbac:groups="",resources="secrets",verbs={create,patch}

// reconcileRootCertificate ensures the root certificate, stored
// in the relevant secret, has been created and is not 'bad' due
// to being expired, formatted incorrectly, etc.
// If it is bad for some reason, a new root certificate is
// generated for use.
func (r *Reconciler) reconcileRootCertificate(
	ctx context.Context, cluster *v1beta1.PostgresCluster,
) (
	*pki.RootCertificateAuthority, error,
) {
	const keyCertificate, keyPrivateKey = "root.crt", "root.key"

	// K8SPG-553
	existing := &corev1.Secret{
		ObjectMeta: naming.PostgresRootCASecret(cluster),
	}

	privateKey := keyPrivateKey
	certificateKey := keyCertificate
	if cluster.Spec.CustomRootCATLSSecret != nil {
		existing.Name = cluster.Spec.CustomRootCATLSSecret.Name

		for _, i := range cluster.Spec.CustomRootCATLSSecret.Items {
			switch i.Path {
			case keyCertificate:
				certificateKey = i.Key
			case keyPrivateKey:
				privateKey = i.Key
			}
		}
	}

	err := errors.WithStack(
		r.Client.Get(ctx, client.ObjectKeyFromObject(existing), existing))
	// K8SPG-555: we need to check ca certificate from old operator versions
	// TODO: remove when 2.4.0 will become unsupported
	if k8serrors.IsNotFound(err) {
		nn := client.ObjectKeyFromObject(existing)
		nn.Name = naming.RootCertSecret
		err = errors.WithStack(
			r.Client.Get(ctx, nn, existing))
		if err == nil {
			existing.Name = naming.RootCertSecret
		}
	}
	if k8serrors.IsNotFound(err) {
		err = nil
	}

	root := &pki.RootCertificateAuthority{}

	if err == nil {
		// Unmarshal and validate the stored root. These first errors can
		// be ignored because they result in an invalid root which is then
		// correctly regenerated.
		// K8SPG-553
		_ = root.Certificate.UnmarshalText(existing.Data[certificateKey])
		_ = root.PrivateKey.UnmarshalText(existing.Data[privateKey])

		if cluster.Spec.CustomRootCATLSSecret != nil {
			return root, err
		}

		if !pki.RootIsValid(root) {
			root, err = pki.NewRootCertificateAuthority()
			err = errors.WithStack(err)
		}
	}

	// K8SPG-555
	intent := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      existing.Name,
			Namespace: existing.Namespace,
		},
	}
	intent.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("Secret"))
	intent.Data = make(map[string][]byte)
	intent.ObjectMeta.OwnerReferences = existing.ObjectMeta.OwnerReferences

	if cluster.Labels != nil {
		currVersion, err := gover.NewVersion(cluster.Labels[naming.LabelVersion])
		if err == nil && currVersion.GreaterThanOrEqual(gover.Must(gover.NewVersion("2.6.0"))) && cluster.Spec.Metadata != nil {
			intent.Labels = cluster.Spec.Metadata.Labels
			intent.Annotations = cluster.Spec.Metadata.Annotations
		}
	}

	// A root secret is scoped to the namespace where postgrescluster(s)
	// are deployed. For operator deployments with postgresclusters in more than
	// one namespace, there will be one root per namespace.
	// During reconciliation, the owner reference block of the root secret is
	// updated to include the postgrescluster as an owner.
	// However, unlike the leaf certificate, the postgrescluster will not be
	// set as the controller. This allows for multiple owners to guide garbage
	// collection, but avoids any errors related to setting multiple controllers.
	// https://docs.k8s.io/concepts/workloads/controllers/garbage-collection/#owners-and-dependents
	if err == nil {
		err = errors.WithStack(r.setOwnerReference(cluster, intent))
	}
	if err == nil {
		intent.Data[keyCertificate], err = root.Certificate.MarshalText()
		err = errors.WithStack(err)
	}
	if err == nil {
		intent.Data[keyPrivateKey], err = root.PrivateKey.MarshalText()
		err = errors.WithStack(err)
	}
	if err == nil {
		err = errors.WithStack(r.apply(ctx, intent))
	}

	return root, err
}

// +kubebuilder:rbac:groups="",resources="secrets",verbs={get}
// +kubebuilder:rbac:groups="",resources="secrets",verbs={create,patch}

// reconcileClusterCertificate first checks if a custom certificate
// secret is configured. If so, that secret projection is returned.
// Otherwise, a secret containing a generated leaf certificate, stored in
// the relevant secret, has been created and is not 'bad' due to being
// expired, formatted incorrectly, etc. If it is bad for any reason, a new
// leaf certificate is generated using the current root certificate.
// In either case, the relevant secret is expected to contain three files:
// tls.crt, tls.key and ca.crt which are the TLS certificate, private key
// and CA certificate, respectively.
func (r *Reconciler) reconcileClusterCertificate(
	ctx context.Context, root *pki.RootCertificateAuthority,
	cluster *v1beta1.PostgresCluster, primaryService *corev1.Service,
	replicaService *corev1.Service,
) (
	*corev1.SecretProjection, error,
) {
	// if a custom postgrescluster secret is provided, just return it
	if cluster.Spec.CustomTLSSecret != nil {
		return cluster.Spec.CustomTLSSecret, nil
	}

	const keyCertificate, keyPrivateKey, rootCA = "tls.crt", "tls.key", "ca.crt"

	existing := &corev1.Secret{ObjectMeta: naming.PostgresTLSSecret(cluster)}
	err := errors.WithStack(client.IgnoreNotFound(
		r.Client.Get(ctx, client.ObjectKeyFromObject(existing), existing)))

	leaf := &pki.LeafCertificate{}
	dnsNames := append(naming.ServiceDNSNames(ctx, primaryService), naming.ServiceDNSNames(ctx, replicaService)...)
	dnsFQDN := dnsNames[0]

	if err == nil {
		// Unmarshal and validate the stored leaf. These first errors can
		// be ignored because they result in an invalid leaf which is then
		// correctly regenerated.
		_ = leaf.Certificate.UnmarshalText(existing.Data[keyCertificate])
		_ = leaf.PrivateKey.UnmarshalText(existing.Data[keyPrivateKey])

		leaf, err = root.RegenerateLeafWhenNecessary(leaf, dnsFQDN, dnsNames)
		err = errors.WithStack(err)
	}

	intent := &corev1.Secret{ObjectMeta: naming.PostgresTLSSecret(cluster)}
	intent.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("Secret"))
	intent.Data = make(map[string][]byte)
	intent.ObjectMeta.OwnerReferences = existing.ObjectMeta.OwnerReferences

	intent.Annotations = naming.Merge(cluster.Spec.Metadata.GetAnnotationsOrNil())
	intent.Labels = naming.Merge(
		cluster.Spec.Metadata.GetLabelsOrNil(),
		naming.WithPerconaLabels(map[string]string{
			naming.LabelCluster:            cluster.Name,
			naming.LabelClusterCertificate: "postgres-tls",
		}, cluster.Name, "", cluster.Labels[naming.LabelVersion]))

	// K8SPG-330: Keep this commented in case of conflicts.
	// We don't want to delete TLS secrets on cluster deletion.
	// if err == nil {
	// 	err = errors.WithStack(r.setControllerReference(cluster, intent))
	// }

	if err == nil {
		intent.Data[keyCertificate], err = leaf.Certificate.MarshalText()
		err = errors.WithStack(err)
	}
	if err == nil {
		intent.Data[keyPrivateKey], err = leaf.PrivateKey.MarshalText()
		err = errors.WithStack(err)
	}
	if err == nil {
		intent.Data[rootCA], err = root.Certificate.MarshalText()
		err = errors.WithStack(err)
	}

	// TODO(tjmoore4): The generated postgrescluster secret is only created
	// when a custom secret is not specified. However, if the secret is
	// initially created and a custom secret is later used, the generated
	// secret is currently left in place.
	if err == nil {
		err = errors.WithStack(r.apply(ctx, intent))
	}

	return clusterCertSecretProjection(intent), err
}

// +kubebuilder:rbac:groups="",resources="secrets",verbs={get}
// +kubebuilder:rbac:groups="",resources="secrets",verbs={create,patch}

// instanceCertificate populates intent with the DNS leaf certificate and
// returns it. It also ensures the leaf certificate, stored in the relevant
// secret, has been created and is not 'bad' due to being expired, formatted
// incorrectly, etc. In addition, a check is made to ensure the leaf cert's
// authority key ID matches the corresponding root cert's subject
// key ID (i.e. the root cert is the 'parent' of the leaf cert).
// If it is bad for any reason, a new leaf certificate is generated
// using the current root certificate
func (*Reconciler) instanceCertificate(
	ctx context.Context, instance *appsv1.StatefulSet,
	existing, intent *corev1.Secret, root *pki.RootCertificateAuthority,
) (
	*pki.LeafCertificate, error,
) {
	var err error
	const keyCertificate, keyPrivateKey = "dns.crt", "dns.key"

	leaf := &pki.LeafCertificate{}

	// RFC 2818 states that the certificate DNS names must be used to verify
	// HTTPS identity.
	dnsNames := naming.InstancePodDNSNames(ctx, instance)
	dnsFQDN := dnsNames[0]

	if err == nil {
		// Unmarshal and validate the stored leaf. These first errors can
		// be ignored because they result in an invalid leaf which is then
		// correctly regenerated.
		_ = leaf.Certificate.UnmarshalText(existing.Data[keyCertificate])
		_ = leaf.PrivateKey.UnmarshalText(existing.Data[keyPrivateKey])

		leaf, err = root.RegenerateLeafWhenNecessary(leaf, dnsFQDN, dnsNames)
		err = errors.WithStack(err)
	}

	if err == nil {
		intent.Data[keyCertificate], err = leaf.Certificate.MarshalText()
		err = errors.WithStack(err)
	}
	if err == nil {
		intent.Data[keyPrivateKey], err = leaf.PrivateKey.MarshalText()
		err = errors.WithStack(err)
	}

	return leaf, err
}

// clusterCertSecretProjection returns a secret projection of the postgrescluster's
// CA, key, and certificate to include in the instance configuration volume.
func clusterCertSecretProjection(certificate *corev1.Secret) *corev1.SecretProjection {
	return &corev1.SecretProjection{
		LocalObjectReference: corev1.LocalObjectReference{
			Name: certificate.Name,
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
}
