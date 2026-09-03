// Copyright 2021 - 2024 Crunchy Data Solutions, Inc.
//
// SPDX-License-Identifier: Apache-2.0

package postgrescluster

import (
	"context"
	"strings"

	"github.com/pkg/errors"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/percona/percona-postgresql-operator/v3/internal/logging"
	"github.com/percona/percona-postgresql-operator/v3/internal/naming"
	"github.com/percona/percona-postgresql-operator/v3/internal/pgbouncer"
	"github.com/percona/percona-postgresql-operator/v3/internal/pki"
	"github.com/percona/percona-postgresql-operator/v3/percona/certmanager"
	"github.com/percona/percona-postgresql-operator/v3/pkg/apis/upstream.pgv2.percona.com/v1beta1"
)

const (
	// https://www.postgresql.org/docs/current/ssl-tcp.html
	clusterCertFile = "tls.crt"
	clusterKeyFile  = "tls.key"
	rootCertFile    = "ca.crt"
)

// K8SPG-1045
func (r *Reconciler) reconcileTLSCondition(ctx context.Context, cluster *v1beta1.PostgresCluster) error {
	cond := metav1.Condition{
		Type:               v1beta1.ConditionTypeTLSSecretsReady,
		Status:             metav1.ConditionTrue,
		Reason:             "TLSSecretsFound",
		ObservedGeneration: cluster.GetGeneration(),
	}

	if cluster.Spec.TLS.GetCertManagementPolicy() != v1beta1.CertManagementUserProvidedOnly {
		cond.Message = "certManagementPolicy is " + string(cluster.Spec.TLS.GetCertManagementPolicy())
		meta.SetStatusCondition(&cluster.Status.Conditions, cond)
		return nil
	}

	var missing []string
	var invalid []string

	checkSecret := func(projection *corev1.SecretProjection, secretName string, requiredKeys ...string) error {
		if projection != nil {
			secretName = projection.Name
		}
		secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{
			Namespace: cluster.Namespace,
			Name:      secretName,
		}}
		err := r.Client.Get(ctx, client.ObjectKeyFromObject(secret), secret)
		if client.IgnoreNotFound(err) != nil {
			return errors.Wrapf(err, "get TLS secret %s", secret.Name)
		}
		if k8serrors.IsNotFound(err) {
			missing = append(missing, secret.Name)
			return nil
		}

		var emptyKeys []string
		for _, key := range requiredKeys {
			if len(secret.Data[key]) == 0 {
				emptyKeys = append(emptyKeys, key)
			}
		}
		if len(emptyKeys) > 0 {
			invalid = append(invalid, secret.Name+" (missing or empty keys: "+strings.Join(emptyKeys, ", ")+")")
		}

		return nil
	}

	if err := checkSecret(cluster.Spec.CustomRootCATLSSecret, naming.PostgresRootCASecret(cluster).Name); err != nil {
		return errors.Wrap(err, "check root ca secret")
	}
	if err := checkSecret(cluster.Spec.CustomTLSSecret, naming.PostgresTLSSecret(cluster).Name); err != nil {
		return errors.Wrap(err, "check custom tls secret")
	}
	if err := checkSecret(cluster.Spec.CustomReplicationClientTLSSecret, naming.ReplicationClientCertSecret(cluster).Name); err != nil {
		return errors.Wrap(err, "check replication client cert secret")
	}
	if err := checkSecret(nil, naming.PGBackRestSecret(cluster).Name); err != nil {
		return errors.Wrap(err, "check pgBackRest TLS secret")
	}

	if cluster.Spec.Proxy != nil && cluster.Spec.Proxy.PGBouncer != nil {
		customTLSSecret := cluster.Spec.Proxy.PGBouncer.CustomTLSSecret
		if customTLSSecret == nil {
			if err := checkSecret(nil, naming.ClusterPGBouncer(cluster).Name,
				pgbouncer.CertFrontendAuthoritySecretKey,
				pgbouncer.CertFrontendSecretKey,
				pgbouncer.CertFrontendPrivateKeySecretKey,
			); err != nil {
				return errors.Wrap(err, "check PgBouncer TLS secret")
			}
		} else if err := checkSecret(customTLSSecret, naming.ClusterPGBouncer(cluster).Name); err != nil {
			return errors.Wrap(err, "check custom PgBouncer TLS secret")
		}
	}

	if cluster.Spec.CustomTLSSecret == nil {
		instances := &appsv1.StatefulSetList{}
		if err := r.Client.List(
			ctx, instances,
			client.InNamespace(cluster.Namespace),
			client.MatchingLabels{
				naming.LabelCluster: cluster.Name,
				naming.LabelData:    naming.DataPostgres,
			},
		); err != nil {
			return errors.Wrap(err, "list instances to check TLS secrets")
		}

		for i := range instances.Items {
			if err := checkSecret(nil, naming.InstanceCertificates(&instances.Items[i]).Name); err != nil {
				return errors.Wrap(err, "check instance TLS secret")
			}
		}
	}

	if len(missing) > 0 {
		cond.Message = "Missing user-provided TLS secrets: " + strings.Join(missing, ", ") + ". certManagementPolicy is userProvidedOnly"
		cond.Reason = "TLSSecretsMissing"
		cond.Status = metav1.ConditionFalse
		meta.SetStatusCondition(&cluster.Status.Conditions, cond)
		return nil
	}
	if len(invalid) > 0 {
		cond.Message = "Invalid user-provided TLS secrets: " + strings.Join(invalid, ", ") + ". certManagementPolicy is userProvidedOnly"
		cond.Reason = "TLSSecretsInvalid"
		cond.Status = metav1.ConditionFalse
		meta.SetStatusCondition(&cluster.Status.Conditions, cond)
		return nil
	}

	meta.SetStatusCondition(&cluster.Status.Conditions, cond)
	return nil
}

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
	policy := cluster.Spec.TLS.GetCertManagementPolicy()
	mode, err := certmanager.ResolveIssuerMode(ctx, r.Client, cluster)
	if err != nil {
		return nil, errors.Wrap(err, "failed to resolve issuer mode")
	}
	if mode == certmanager.IssuerModeExternal {
		return nil, nil
	}

	const keyCertificate, keyPrivateKey = "root.crt", "root.key"

	// K8SPG-553
	existing := &corev1.Secret{
		ObjectMeta: naming.PostgresRootCASecret(cluster),
	}
	if mode == certmanager.IssuerModeManagedCluster {
		existing.ObjectMeta = naming.ClusterCACertSecret(cluster, certmanager.CertManagerNamespace())
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

	err = errors.WithStack(
		r.Client.Get(ctx, client.ObjectKeyFromObject(existing), existing))
	if k8serrors.IsNotFound(err) {
		err = nil

		certManagerSecret, certManagerErr := r.reconcileCertManagerRootCertificate(ctx, cluster)
		if certManagerErr != nil {
			return nil, certManagerErr
		}
		if certManagerSecret != nil {
			existing = certManagerSecret
		}
	}

	if policy == v1beta1.CertManagementUserProvidedOnly {
		if err != nil {
			return nil, errors.Wrap(err, "get user-provided root CA secret")
		}

		root := &pki.RootCertificateAuthority{}
		if err := root.Certificate.UnmarshalText(existing.Data[certificateKey]); err != nil {
			return nil, errors.Wrapf(err, "parse certificate in user-provided root CA secret %q", existing.Name)
		}
		if err := root.PrivateKey.UnmarshalText(existing.Data[privateKey]); err != nil {
			return nil, errors.Wrapf(err, "parse private key in user-provided root CA secret %q", existing.Name)
		}
		if !pki.RootIsValid(root) {
			return nil, errors.Errorf("user-provided root CA secret %q is invalid", existing.Name)
		}
		return root, nil
	}
	// If the secret is managed by cert-manager, parse it using cert-manager key names
	// (tls.crt/tls.key) and return without overwriting the secret with internal PKI.
	if policy == v1beta1.CertManagementAuto && err == nil && existing.Annotations["cert-manager.io/certificate-name"] != "" {
		if _, certManagerErr := r.reconcileCertManagerRootCertificate(ctx, cluster); certManagerErr != nil {
			return nil, certManagerErr
		}
		root := &pki.RootCertificateAuthority{}
		_ = root.Certificate.UnmarshalText(existing.Data["tls.crt"])
		_ = root.PrivateKey.UnmarshalText(existing.Data["tls.key"])
		if pki.RootIsValid(root) {
			return root, nil
		}
		return nil, errors.New("waiting for cert-manager to issue a valid CA certificate")
	}

	if mode == certmanager.IssuerModeManagedCluster {
		// The cluster-scoped CA cert/secret is entirely cert-manager's
		// responsibility; there is no internal-PKI fallback for it.
		return nil, errors.New("waiting for cert-manager to issue a valid CA certificate")
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
		ObjectMeta: naming.PostgresRootCASecret(cluster),
	}
	intent.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("Secret"))
	intent.Data = make(map[string][]byte)

	if cluster.Spec.Metadata != nil {
		intent.Labels = cluster.Spec.Metadata.Labels
		intent.Annotations = cluster.Spec.Metadata.Annotations
	}

	if err == nil {
		err = errors.WithStack(r.setControllerReference(cluster, intent))
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

// +kubebuilder:rbac:groups=certmanager.k8s.io,resources=issuers;certificates;certificaterequests,verbs=get;list;watch;create;update;patch;delete;deletecollection
// +kubebuilder:rbac:groups=cert-manager.io,resources=issuers;certificates;certificaterequests,verbs=get;list;watch;create;update;patch;delete;deletecollection

// reconcileCertManagerRootCertificate func.
func (r *Reconciler) reconcileCertManagerRootCertificate(
	ctx context.Context, cluster *v1beta1.PostgresCluster,
) (*corev1.Secret, error) {
	log := logging.FromContext(ctx)
	useCertManager, err := r.shouldUseCertManager(ctx, cluster)
	if err != nil {
		return nil, errors.Wrap(err, "error deciding whether to use cert-manager")
	}
	if !useCertManager {
		return nil, nil
	}
	c := r.CertManagerCtrlFunc(r.Client, r.Scheme, false)
	err = c.ApplyCAIssuer(ctx, cluster)
	if err != nil {
		return nil, errors.Wrap(err, "error applying CA issuer")
	}
	err = c.ApplyCACertificate(ctx, cluster)
	if err != nil {
		return nil, errors.Wrap(err, "error applying CA certificate")
	}

	mode, err := certmanager.ResolveIssuerMode(ctx, r.Client, cluster)
	if err != nil {
		return nil, errors.Wrap(err, "failed to resolve issuer mode")
	}
	secretMeta := naming.PostgresRootCASecret(cluster)
	if mode == certmanager.IssuerModeManagedCluster {
		secretMeta = naming.ClusterCACertSecret(cluster, certmanager.CertManagerNamespace())
	}

	// Try to fetch the CA secret created by cert-manager.
	secret := &corev1.Secret{ObjectMeta: secretMeta}
	if err := r.Client.Get(ctx, client.ObjectKeyFromObject(secret), secret); err != nil {
		if k8serrors.IsNotFound(err) {
			log.Info("waiting for cert-manager to issue CA certificate")
			return nil, errors.New("waiting for cert-manager to issue CA certificate")
		}
		return nil, errors.Wrap(err, "error getting cert-manager CA secret")
	}

	return secret, nil
}

// +kubebuilder:rbac:groups="",resources="secrets",verbs={get}
// +kubebuilder:rbac:groups="",resources="secrets",verbs={create,patch}

// reconcileClusterCertificate returns the cluster TLS secret projection.
// If CustomTLSSecret is set, that projection is returned. Otherwise the
// path depends on the root CA: when it is cert-manager-managed,
// cert-manager issues the leaf; when it is internal but a stale
// Certificate CR is left behind by K8SPG-1017, the CR is reconciled
// (K8SPG-1007 ownerRef recovery) before falling back to the internal PKI
// leaf. The returned secret contains tls.crt, tls.key and ca.crt.
func (r *Reconciler) reconcileClusterCertificate(
	ctx context.Context, root *pki.RootCertificateAuthority,
	cluster *v1beta1.PostgresCluster, primaryService *corev1.Service,
	replicaService *corev1.Service,
) (
	*corev1.SecretProjection, error,
) {
	if cluster.Spec.CustomTLSSecret != nil {
		return cluster.Spec.CustomTLSSecret, nil
	}

	if cluster.Spec.TLS.GetCertManagementPolicy() == v1beta1.CertManagementUserProvidedOnly {
		return r.reconcileUserProvidedClusterCertificate(ctx, cluster)
	}
	certManagerManaged, err := r.isRootCACertManagerManaged(ctx, cluster)
	if err != nil {
		return nil, errors.Wrap(err, "failed to check if cert-manager manages root CA")
	}

	if certManagerManaged {
		return r.reconcileCertManagerClusterCertificate(ctx, cluster, primaryService, replicaService)
	}

	// cluster certificates are not managed by cert-manager
	// but Certificate object exists due to the bug described in K8SPG-1017
	// we need to reconcile them anyway to update ownerRef for K8SPG-1007.
	if cert := certmanager.ClusterCertificateName(cluster); r.shouldReconcileCertManagerCertificate(ctx, cluster, cert) {
		_, err := r.reconcileCertManagerClusterCertificate(ctx, cluster, primaryService, replicaService)
		if err != nil {
			logging.FromContext(ctx).Error(err, "failed to reconcile Certificate", "name", cert)
		}
	}

	return r.reconcileInternalClusterCertificate(ctx, root, cluster, primaryService, replicaService)
}

func (r *Reconciler) reconcileUserProvidedClusterCertificate(
	ctx context.Context, cluster *v1beta1.PostgresCluster,
) (*corev1.SecretProjection, error) {
	secret := &corev1.Secret{ObjectMeta: naming.PostgresTLSSecret(cluster)}
	if err := r.Client.Get(ctx, client.ObjectKeyFromObject(secret), secret); err != nil {
		return nil, errors.Wrapf(err, "get user-provided TLS secret %s", secret.Name)
	}

	for _, key := range []string{clusterCertFile, clusterKeyFile, rootCertFile} {
		if len(secret.Data[key]) == 0 {
			return nil, errors.Errorf("user-provided TLS secret %q is missing key %q", secret.Name, key)
		}
	}

	return clusterCertSecretProjection(secret), nil
}

// reconcileInternalClusterCertificate creates a cluster certificate using internal PKI.
func (r *Reconciler) reconcileInternalClusterCertificate(
	ctx context.Context, root *pki.RootCertificateAuthority,
	cluster *v1beta1.PostgresCluster, primaryService *corev1.Service,
	replicaService *corev1.Service,
) (
	*corev1.SecretProjection, error,
) {
	const keyCertificate, keyPrivateKey, rootCA = "tls.crt", "tls.key", "ca.crt"

	existing := &corev1.Secret{ObjectMeta: naming.PostgresTLSSecret(cluster)}
	err := errors.WithStack(client.IgnoreNotFound(
		r.Client.Get(ctx, client.ObjectKeyFromObject(existing), existing),
	))
	if err != nil {
		return nil, errors.Wrap(err, "get secret")
	}

	leaf := &pki.LeafCertificate{}
	primaryServiceDNSNames, err := naming.ServiceDNSNames(ctx, primaryService, cluster.Spec.ClusterServiceDNSSuffix)
	if err != nil {
		return nil, errors.Wrap(err, "get primary service dns names")
	}

	replicaServiceDNSNames, err := naming.ServiceDNSNames(ctx, replicaService, cluster.Spec.ClusterServiceDNSSuffix)
	if err != nil {
		return nil, errors.Wrap(err, "get replica service dns names")
	}

	dnsNames := append(primaryServiceDNSNames, replicaServiceDNSNames...)
	dnsFQDN := dnsNames[0]
	dnsNames = append(dnsNames, cluster.Spec.TLS.GetSANs()...)

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
	intent.OwnerReferences = existing.OwnerReferences

	intent.Annotations = naming.Merge(cluster.Spec.Metadata.GetAnnotationsOrNil())
	intent.Labels = naming.Merge(
		cluster.Spec.Metadata.GetLabelsOrNil(),
		naming.WithPerconaLabels(map[string]string{
			naming.LabelCluster:            cluster.Name,
			naming.LabelClusterCertificate: "postgres-tls",
		}, cluster.Name, "", cluster.Labels[naming.LabelVersion]),
	)

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

// reconcileCertManagerClusterCertificate creates a cluster certificate using cert-manager.
// It first ensures the TLS issuer exists, then creates the cluster Certificate CR.
func (r *Reconciler) reconcileCertManagerClusterCertificate(
	ctx context.Context,
	cluster *v1beta1.PostgresCluster,
	primaryService *corev1.Service,
	replicaService *corev1.Service,
) (
	*corev1.SecretProjection, error,
) {
	c := r.CertManagerCtrlFunc(r.Client, r.Scheme, false)

	mode, err := certmanager.ResolveIssuerMode(ctx, r.Client, cluster)
	if err != nil {
		return nil, errors.Wrap(err, "failed to resolve issuer mode")
	}
	if mode != certmanager.IssuerModeExternal {
		if err := c.ApplyIssuer(ctx, cluster); err != nil {
			return nil, errors.Wrap(err, "failed to apply TLS issuer")
		}
	}

	primaryDNSNames, err := naming.ServiceDNSNames(ctx, primaryService, cluster.Spec.ClusterServiceDNSSuffix)
	if err != nil {
		return nil, errors.Wrap(err, "get primary service DNS names")
	}
	replicaDNSNames, err := naming.ServiceDNSNames(ctx, replicaService, cluster.Spec.ClusterServiceDNSSuffix)
	if err != nil {
		return nil, errors.Wrap(err, "get replica service DNS names")
	}
	dnsNames := append(primaryDNSNames, replicaDNSNames...)
	dnsNames = append(dnsNames, cluster.Spec.TLS.GetSANs()...)

	err = c.ApplyClusterCertificate(ctx, cluster, dnsNames)
	if err != nil {
		return nil, errors.Wrap(err, "failed to apply cluster certificate")
	}

	return clusterCertSecretProjection(&corev1.Secret{
		ObjectMeta: naming.PostgresTLSSecret(cluster),
	}), nil
}

// shouldReconcileCertManagerCertificate reports whether a stale cert-manager
// Certificate CR exists for the cluster and should be reconciled to update
// its ownerRef (K8SPG-1007 recovery for Certificates left behind by the
// K8SPG-1017 bug). Returns false when policy disables cert-manager,
// cert-manager is unavailable, or the Certificate CR does not exist.
func (r *Reconciler) shouldReconcileCertManagerCertificate(
	ctx context.Context, cluster *v1beta1.PostgresCluster, certName string,
) bool {
	useCertManager, err := r.shouldUseCertManager(ctx, cluster)
	if err != nil || !useCertManager {
		return false
	}

	// CertificateExists is read-only, so use the dry-run controller to match
	// the intent (no mutating cert-manager calls happen on this path).
	c := r.CertManagerCtrlFunc(r.Client, r.Scheme, true /* dry run */)

	exists, err := c.CertificateExists(ctx, cluster.Namespace, certName)

	return err == nil && exists
}

func (r *Reconciler) isRootCACertManagerManaged(ctx context.Context, cluster *v1beta1.PostgresCluster) (bool, error) {
	if cluster.Spec.CustomRootCATLSSecret != nil {
		return false, nil
	}

	mode, err := certmanager.ResolveIssuerMode(ctx, r.Client, cluster)
	if err != nil {
		return false, errors.Wrap(err, "failed to resolve issuer mode")
	}

	useCertManager, err := r.shouldUseCertManager(ctx, cluster)
	if err != nil {
		return false, err
	}

	if mode != certmanager.IssuerModeManagedNamespaced {
		if !useCertManager {
			return false, errors.New("cert-manager is required when spec.tls.issuerConf is set")
		}
		return true, nil
	}

	if !useCertManager {
		return false, nil
	}

	rootSecret := &corev1.Secret{ObjectMeta: naming.PostgresRootCASecret(cluster)}
	err = r.Client.Get(ctx, client.ObjectKeyFromObject(rootSecret), rootSecret)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return true, nil
		}
		return false, errors.WithStack(err)
	}

	return rootSecret.Annotations["cert-manager.io/certificate-name"] != "", nil
}

func (r *Reconciler) shouldUseCertManager(
	ctx context.Context, cluster *v1beta1.PostgresCluster,
) (bool, error) {
	if cluster.Spec.TLS.GetCertManagementPolicy() != v1beta1.CertManagementAuto {
		return false, nil
	}
	if r.RestConfig == nil {
		return false, nil
	}
	c := r.CertManagerCtrlFunc(r.Client, r.Scheme, true)
	err := c.Check(ctx, r.RestConfig, cluster.Namespace)
	if err != nil {
		switch {
		case errors.Is(err, certmanager.ErrCertManagerNotFound):
			return false, nil
		case errors.Is(err, certmanager.ErrCertManagerNotReady):
			logging.FromContext(ctx).Info("cert-manager is not ready, falling back to internal PKI")
			return false, nil
		}
		return false, err
	}
	return true, nil
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
	existing, intent *corev1.Secret, root *pki.RootCertificateAuthority, dnsSuffix string,
) (
	*pki.LeafCertificate, error,
) {
	var err error
	const keyCertificate, keyPrivateKey = "dns.crt", "dns.key"

	leaf := &pki.LeafCertificate{}

	// RFC 2818 states that the certificate DNS names must be used to verify
	// HTTPS identity.
	dnsNames := naming.InstancePodDNSNames(ctx, instance, dnsSuffix)
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
