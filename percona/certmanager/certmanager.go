package certmanager

import (
	"context"
	"os"
	"regexp"
	"time"

	"github.com/cert-manager/cert-manager/pkg/apis/certmanager"
	v1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	cmmeta "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	"github.com/cert-manager/cert-manager/pkg/util/cmapichecker"
	"github.com/pkg/errors"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/percona/percona-postgresql-operator/v2/internal/naming"
	"github.com/percona/percona-postgresql-operator/v2/pkg/apis/upstream.pgv2.percona.com/v1beta1"
)

type Controller interface {
	Check(ctx context.Context, config *rest.Config, ns string) error
	CertificateExists(ctx context.Context, namespace, certName string) (bool, error)

	ApplyIssuer(ctx context.Context, cluster *v1beta1.PostgresCluster) error
	ApplyCAIssuer(ctx context.Context, cluster *v1beta1.PostgresCluster) error
	ApplyCACertificate(ctx context.Context, cluster *v1beta1.PostgresCluster) error

	ApplyClusterCertificate(ctx context.Context, cluster *v1beta1.PostgresCluster, dnsNames []string) error
	ApplyInstanceCertificate(ctx context.Context, cluster *v1beta1.PostgresCluster, instanceName string, dnsNames []string) error
	ApplyPGBouncerCertificate(ctx context.Context, cluster *v1beta1.PostgresCluster, dnsNames []string) error
	ApplyReplicationCertificate(ctx context.Context, cluster *v1beta1.PostgresCluster) error
	ApplyPGBackRestClientCertificate(ctx context.Context, cluster *v1beta1.PostgresCluster) error
	ApplyPGBackRestRepoCertificate(ctx context.Context, cluster *v1beta1.PostgresCluster, dnsNames []string) error
}

const (
	// DefaultCertDuration is the default certificate duration: 1 year
	DefaultCertDuration = 365 * 24 * time.Hour

	// DefaultRenewBefore is the default renewal time: 30 days before expiry
	DefaultRenewBefore = 30 * 24 * time.Hour
)

// IssuerMode describes how the operator should treat
// cluster.Spec.TLS.IssuerConf (K8SPG-951).
type IssuerMode int

const (
	// IssuerModeManagedNamespaced: issuerConf is unset, or its Kind is "" or
	// "Issuer" — the operator owns a namespaced self-signed CA issuer, CA
	// certificate, and CA-backed TLS issuer (the long-standing behavior).
	IssuerModeManagedNamespaced IssuerMode = iota
	// IssuerModeManagedCluster: issuerConf.Kind is "ClusterIssuer" and the
	// operator can read the named ClusterIssuer (or it doesn't exist yet) —
	// the operator owns a cluster-scoped self-signed CA ClusterIssuer, a CA
	// certificate in cert-manager's shared namespace, and a CA-backed TLS
	// ClusterIssuer.
	IssuerModeManagedCluster
	// IssuerModeExternal: issuerConf.Kind is a third-party kind, or is
	// "ClusterIssuer" but the operator is Forbidden from reading it — every
	// leaf Certificate references issuerConf directly and the operator
	// creates nothing issuer-related.
	IssuerModeExternal
)

// CertManagerNamespace returns cert-manager's shared "cluster resource
// namespace" — where a ClusterIssuer's spec.ca.secretName is resolved from,
// as opposed to the namespace of whatever references the ClusterIssuer.
// Configurable via the CERTMANAGER_NAMESPACE environment variable; defaults
// to "cert-manager".
func CertManagerNamespace() string {
	if ns := os.Getenv("CERTMANAGER_NAMESPACE"); ns != "" {
		return ns
	}
	return "cert-manager"
}

// issuerConf returns cluster.Spec.TLS.IssuerConf, or nil if TLS or
// IssuerConf is unset.
func issuerConf(cluster *v1beta1.PostgresCluster) *cmmeta.IssuerReference {
	if cluster.Spec.TLS == nil {
		return nil
	}
	return cluster.Spec.TLS.IssuerConf
}

// ResolveIssuerMode determines how the operator should handle
// cluster.Spec.TLS.IssuerConf. For Kind == "ClusterIssuer" it performs a
// live Get to check whether the operator can read the named ClusterIssuer:
// Forbidden (no RBAC for clusterissuers.cert-manager.io — the default,
// since none is shipped) downgrades to IssuerModeExternal so the operator
// doesn't try to own something it can't inspect. NotFound is not an error
// here — the operator will create it as part of managing it.
func ResolveIssuerMode(ctx context.Context, cl client.Client, cluster *v1beta1.PostgresCluster) (IssuerMode, error) {
	ic := issuerConf(cluster)
	if ic == nil {
		return IssuerModeManagedNamespaced, nil
	}

	switch ic.Kind {
	case "", v1.IssuerKind:
		return IssuerModeManagedNamespaced, nil
	case v1.ClusterIssuerKind:
		existing := &v1.ClusterIssuer{}
		err := cl.Get(ctx, types.NamespacedName{Name: ic.Name}, existing)
		switch {
		case err == nil, k8serrors.IsNotFound(err):
			return IssuerModeManagedCluster, nil
		case k8serrors.IsForbidden(err):
			return IssuerModeExternal, nil
		default:
			return IssuerModeManagedNamespaced, errors.Wrap(err, "failed to get cluster issuer")
		}
	default:
		return IssuerModeExternal, nil
	}
}

// issuerRef resolves what a leaf Certificate's spec.issuerRef should be for
// the given mode:
//   - IssuerModeExternal: issuerConf's Name/Kind/Group verbatim (Group
//     defaults to "cert-manager.io" when empty — matches cert-manager's own
//     IssuerReference default).
//   - IssuerModeManagedCluster: the cluster-scoped TLS ClusterIssuer named
//     by issuerConf.Name (required by the CRD whenever issuerConf is set).
//   - IssuerModeManagedNamespaced: the namespaced TLS Issuer, named by
//     issuerConf.Name when set, otherwise the auto-generated name.
func issuerRef(cluster *v1beta1.PostgresCluster, mode IssuerMode) cmmeta.IssuerReference {
	switch mode {
	case IssuerModeExternal:
		ic := issuerConf(cluster)
		group := ic.Group
		if group == "" {
			group = certmanager.GroupName
		}
		return cmmeta.IssuerReference{Name: ic.Name, Kind: ic.Kind, Group: group}
	case IssuerModeManagedCluster:
		return cmmeta.IssuerReference{Name: issuerConf(cluster).Name, Kind: v1.ClusterIssuerKind}
	default:
		name := naming.TLSIssuer(cluster).Name
		if ic := issuerConf(cluster); ic != nil && ic.Name != "" {
			name = ic.Name
		}
		return cmmeta.IssuerReference{Name: name, Kind: v1.IssuerKind}
	}
}

type controller struct {
	cl         client.Client
	scheme     *runtime.Scheme
	dryRun     bool
	newChecker func(config *rest.Config, ns string) (cmapichecker.Interface, error)
}

var _ Controller = new(controller)

type NewControllerFunc func(cl client.Client, scheme *runtime.Scheme, dryRun bool) Controller

func NewController(cl client.Client, scheme *runtime.Scheme, dryRun bool) Controller {
	if dryRun {
		cl = client.NewDryRunClient(cl)
	}
	return &controller{
		cl:         cl,
		scheme:     scheme,
		dryRun:     dryRun,
		newChecker: cmapichecker.New,
	}
}

var (
	ErrCertManagerNotFound = errors.New("cert-manager is not found")
	ErrCertManagerNotReady = errors.New("cert-manager is not ready")
)

func (c *controller) Check(ctx context.Context, config *rest.Config, ns string) error {
	log := logf.FromContext(ctx)
	checker, err := c.newChecker(config, ns)
	if err != nil {
		return err
	}
	err = checker.Check(ctx)
	if err != nil {
		switch err := translateCheckError(err); {
		case errors.Is(err, cmapichecker.ErrCertManagerCRDsNotFound):
			return ErrCertManagerNotFound
		case errors.Is(err, cmapichecker.ErrWebhookCertificateFailure), errors.Is(err, cmapichecker.ErrWebhookServiceFailure), errors.Is(err, cmapichecker.ErrWebhookDeploymentFailure):
			log.Info("cert-manager is not ready", "reason", cmapichecker.TranslateToSimpleError(err))
			return ErrCertManagerNotReady
		}
		return err
	}
	return nil
}

// CertificateExists reports whether a cert-manager Certificate CR with the
// given name exists in the namespace. NotFound is returned as (false, nil);
// other API errors are returned as (false, err).
func (c *controller) CertificateExists(ctx context.Context, namespace, certName string) (bool, error) {
	existing := &v1.Certificate{}
	err := c.cl.Get(ctx, types.NamespacedName{Name: certName, Namespace: namespace}, existing)
	if err == nil {
		return true, nil
	}

	if k8serrors.IsNotFound(err) {
		return false, nil
	}

	return false, errors.Wrapf(err, "get certificate/%s", certName)
}

// ApplyIssuer creates the CA-backed Issuer resource that signs every leaf
// Certificate for the given PostgresCluster (or a cluster-scoped CA-backed
// ClusterIssuer when spec.tls.issuerConf.kind is "ClusterIssuer" —
// K8SPG-951). No-op when the resolved mode is external.
func (c *controller) ApplyIssuer(ctx context.Context, cluster *v1beta1.PostgresCluster) error {
	mode, err := ResolveIssuerMode(ctx, c.cl, cluster)
	if err != nil {
		return errors.Wrap(err, "failed to resolve issuer mode")
	}
	if mode == IssuerModeExternal {
		return nil
	}

	if mode == IssuerModeManagedCluster {
		caSecretName := naming.ClusterCACertSecret(cluster, CertManagerNamespace()).Name
		meta := metav1.ObjectMeta{Name: issuerRef(cluster, mode).Name}

		existing := &v1.ClusterIssuer{}
		err := c.cl.Get(ctx, types.NamespacedName{Name: meta.Name}, existing)
		if err == nil {
			return nil
		}
		if !k8serrors.IsNotFound(err) {
			return errors.Wrap(err, "failed to get cluster issuer")
		}

		issuer := &v1.ClusterIssuer{
			ObjectMeta: meta,
			Spec: v1.IssuerSpec{
				IssuerConfig: v1.IssuerConfig{
					CA: &v1.CAIssuer{SecretName: caSecretName},
				},
			},
		}
		if err := c.cl.Create(ctx, issuer); err != nil {
			return errors.Wrap(err, "failed to create cluster issuer")
		}
		return nil
	}

	meta := naming.TLSIssuer(cluster)
	if ic := issuerConf(cluster); ic != nil && ic.Name != "" {
		meta.Name = ic.Name
	}

	existing := &v1.Issuer{}
	err = c.cl.Get(ctx, types.NamespacedName{Name: meta.Name, Namespace: meta.Namespace}, existing)
	if err == nil {
		hasOwnerRef, err := controllerutil.HasOwnerReference(existing.OwnerReferences, cluster, c.scheme)
		if err != nil {
			return errors.Wrap(err, "check owner reference")
		}

		if !hasOwnerRef {
			gvk := v1beta1.SchemeBuilder.GroupVersion.WithKind("PostgresCluster")
			existing.OwnerReferences = []metav1.OwnerReference{{
				APIVersion:         gvk.GroupVersion().String(),
				Kind:               gvk.Kind,
				Name:               cluster.GetName(),
				UID:                cluster.GetUID(),
				BlockOwnerDeletion: ptr.To(true),
				Controller:         ptr.To(true),
			}}
			return errors.Wrap(c.cl.Update(ctx, existing), "failed to update issuer")
		}

		return nil
	}
	if !k8serrors.IsNotFound(err) {
		return errors.Wrap(err, "failed to get issuer")
	}

	issuer := &v1.Issuer{
		ObjectMeta: meta,
		Spec: v1.IssuerSpec{
			IssuerConfig: v1.IssuerConfig{
				CA: &v1.CAIssuer{
					SecretName: naming.PostgresRootCASecret(cluster).Name,
				},
			},
		},
	}

	if err := controllerutil.SetControllerReference(cluster, issuer, c.scheme); err != nil {
		return errors.Wrap(err, "failed to set controller reference")
	}

	if err := c.cl.Create(ctx, issuer); err != nil {
		return errors.Wrap(err, "failed to create issuer")
	}

	return nil
}

// ApplyCAIssuer creates a SelfSigned Issuer resource for the given
// PostgresCluster (or a cluster-scoped SelfSigned ClusterIssuer when
// spec.tls.issuerConf.kind is "ClusterIssuer" — K8SPG-951). No-op when the
// resolved mode is external.
func (c *controller) ApplyCAIssuer(ctx context.Context, cluster *v1beta1.PostgresCluster) error {
	mode, err := ResolveIssuerMode(ctx, c.cl, cluster)
	if err != nil {
		return errors.Wrap(err, "failed to resolve issuer mode")
	}
	if mode == IssuerModeExternal {
		return nil
	}

	spec := v1.IssuerSpec{
		IssuerConfig: v1.IssuerConfig{
			SelfSigned: &v1.SelfSignedIssuer{},
		},
	}

	if mode == IssuerModeManagedCluster {
		meta := naming.ClusterCAIssuer(cluster)

		existing := &v1.ClusterIssuer{}
		err := c.cl.Get(ctx, types.NamespacedName{Name: meta.Name}, existing)
		if err == nil {
			return nil
		}
		if !k8serrors.IsNotFound(err) {
			return errors.Wrap(err, "failed to get CA cluster issuer")
		}

		issuer := &v1.ClusterIssuer{ObjectMeta: meta, Spec: spec}
		if err := c.cl.Create(ctx, issuer); err != nil {
			return errors.Wrap(err, "failed to create ca cluster issuer")
		}
		return nil
	}

	meta := naming.CAIssuer(cluster)

	existing := &v1.Issuer{}
	err = c.cl.Get(ctx, types.NamespacedName{Name: meta.Name, Namespace: meta.Namespace}, existing)
	if err == nil {
		hasOwnerRef, err := controllerutil.HasOwnerReference(existing.OwnerReferences, cluster, c.scheme)
		if err != nil {
			return errors.Wrap(err, "check owner reference")
		}

		if !hasOwnerRef {
			gvk := v1beta1.SchemeBuilder.GroupVersion.WithKind("PostgresCluster")
			existing.OwnerReferences = []metav1.OwnerReference{{
				APIVersion:         gvk.GroupVersion().String(),
				Kind:               gvk.Kind,
				Name:               cluster.GetName(),
				UID:                cluster.GetUID(),
				BlockOwnerDeletion: ptr.To(true),
				Controller:         ptr.To(true),
			}}
			return errors.Wrap(c.cl.Update(ctx, existing), "failed to update issuer")
		}

		return nil
	}
	if !k8serrors.IsNotFound(err) {
		return errors.Wrap(err, "failed to get CA issuer")
	}

	issuer := &v1.Issuer{ObjectMeta: meta, Spec: spec}

	if err := controllerutil.SetControllerReference(cluster, issuer, c.scheme); err != nil {
		return errors.Wrap(err, "failed to set controller reference")
	}

	if err := c.cl.Create(ctx, issuer); err != nil {
		return errors.Wrap(err, "failed to create ca issuer")
	}

	return nil
}

// ApplyCACertificate creates the self-signed CA Certificate for the given
// PostgresCluster. For IssuerModeManagedCluster (K8SPG-951), it's placed in
// cert-manager's shared namespace under a cluster-qualified name and gets no
// owner reference (it may be shared by other PostgresClusters). No-op for
// IssuerModeExternal.
func (c *controller) ApplyCACertificate(ctx context.Context, cluster *v1beta1.PostgresCluster) error {
	mode, err := ResolveIssuerMode(ctx, c.cl, cluster)
	if err != nil {
		return errors.Wrap(err, "failed to resolve issuer mode")
	}
	if mode == IssuerModeExternal {
		return nil
	}

	caDuration := DefaultCertDuration
	if cluster.Spec.TLS != nil && cluster.Spec.TLS.CAValidityDuration != nil {
		caDuration = cluster.Spec.TLS.CAValidityDuration.Duration
	}

	clusterScoped := mode == IssuerModeManagedCluster

	var secretMeta metav1.ObjectMeta
	var issuerRefValue cmmeta.IssuerReference
	if clusterScoped {
		secretMeta = naming.ClusterCACertSecret(cluster, CertManagerNamespace())
		issuerRefValue = cmmeta.IssuerReference{Name: naming.ClusterCAIssuer(cluster).Name, Kind: v1.ClusterIssuerKind}
	} else {
		secretMeta = naming.PostgresRootCASecret(cluster)
		issuerRefValue = cmmeta.IssuerReference{Name: naming.CAIssuer(cluster).Name, Kind: v1.IssuerKind}
	}
	certName := secretMeta.Name
	certNamespace := secretMeta.Namespace

	existing := &v1.Certificate{}
	err = c.cl.Get(ctx, types.NamespacedName{Name: certName, Namespace: certNamespace}, existing)
	if err == nil {
		needsUpdate := false

		if !clusterScoped {
			hasOwnerRef, err := controllerutil.HasOwnerReference(existing.OwnerReferences, cluster, c.scheme)
			if err != nil {
				return errors.Wrap(err, "check owner reference")
			}

			if !hasOwnerRef {
				gvk := v1beta1.SchemeBuilder.GroupVersion.WithKind("PostgresCluster")
				existing.OwnerReferences = []metav1.OwnerReference{{
					APIVersion:         gvk.GroupVersion().String(),
					Kind:               gvk.Kind,
					Name:               cluster.GetName(),
					UID:                cluster.GetUID(),
					BlockOwnerDeletion: ptr.To(true),
					Controller:         ptr.To(true),
				}}
				needsUpdate = true
			}
		}

		if existing.Spec.Duration != nil && existing.Spec.Duration.Duration != caDuration {
			existing.Spec.Duration = &metav1.Duration{Duration: caDuration}
			needsUpdate = true
		}

		if !needsUpdate {
			return nil
		}

		return errors.Wrap(c.cl.Update(ctx, existing), "failed to update ca certificate")
	}
	if !k8serrors.IsNotFound(err) {
		return errors.Wrap(err, "failed to get CA certificate")
	}

	cert := &v1.Certificate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      certName,
			Namespace: certNamespace,
			Labels: naming.WithPerconaLabels(map[string]string{
				naming.LabelCluster: cluster.Name,
			}, cluster.Name, "", cluster.Labels[naming.LabelVersion]),
		},
		Spec: v1.CertificateSpec{
			SecretName:  certName,
			CommonName:  cluster.Name + "-ca",
			IsCA:        true,
			IssuerRef:   issuerRefValue,
			Duration:    &metav1.Duration{Duration: caDuration},
			RenewBefore: &metav1.Duration{Duration: DefaultRenewBefore},
			PrivateKey: &v1.CertificatePrivateKey{
				Algorithm:      v1.ECDSAKeyAlgorithm,
				Size:           256,
				RotationPolicy: v1.RotationPolicyNever,
			},
			SecretTemplate: &v1.CertificateSecretTemplate{
				Labels: naming.WithPerconaLabels(map[string]string{
					naming.LabelCluster: cluster.Name,
				}, cluster.Name, "", cluster.Labels[naming.LabelVersion]),
			},
		},
	}

	if !clusterScoped {
		if err := controllerutil.SetControllerReference(cluster, cert, c.scheme); err != nil {
			return errors.Wrap(err, "failed to set controller reference")
		}
	}

	if err := c.cl.Create(ctx, cert); err != nil {
		return errors.Wrap(err, "failed to create ca certificate")
	}

	return nil
}

// ApplyClusterCertificate creates a cert-manager Certificate resource for the cluster's
// primary and replica services TLS certificate.
func (c *controller) ApplyClusterCertificate(ctx context.Context, cluster *v1beta1.PostgresCluster, dnsNames []string) error {
	if len(dnsNames) == 0 {
		return errors.New("dnsNames cannot be empty")
	}

	mode, err := ResolveIssuerMode(ctx, c.cl, cluster)
	if err != nil {
		return errors.Wrap(err, "failed to resolve issuer mode")
	}
	wantIssuerRef := issuerRef(cluster, mode)

	certName := ClusterCertificateName(cluster)

	certDuration := DefaultCertDuration
	if cluster.Spec.TLS != nil && cluster.Spec.TLS.CertValidityDuration != nil {
		certDuration = cluster.Spec.TLS.CertValidityDuration.Duration
	}

	existing := &v1.Certificate{}
	err = c.cl.Get(ctx, types.NamespacedName{Name: certName, Namespace: cluster.Namespace}, existing)
	if err == nil {
		needsUpdate := false

		hasOwnerRef, err := controllerutil.HasOwnerReference(existing.OwnerReferences, cluster, c.scheme)
		if err != nil {
			return errors.Wrap(err, "check owner reference")
		}

		if !hasOwnerRef {
			gvk := v1beta1.SchemeBuilder.GroupVersion.WithKind("PostgresCluster")
			existing.OwnerReferences = []metav1.OwnerReference{{
				APIVersion:         gvk.GroupVersion().String(),
				Kind:               gvk.Kind,
				Name:               cluster.GetName(),
				UID:                cluster.GetUID(),
				BlockOwnerDeletion: ptr.To(true),
				Controller:         ptr.To(true),
			}}
			needsUpdate = true
		}

		if existing.Spec.Duration != nil && existing.Spec.Duration.Duration != certDuration {
			existing.Spec.Duration = &metav1.Duration{Duration: certDuration}
			needsUpdate = true
		}

		if existing.Spec.IssuerRef != wantIssuerRef {
			existing.Spec.IssuerRef = wantIssuerRef
			needsUpdate = true
		}

		if !needsUpdate {
			return nil
		}

		return errors.Wrap(c.cl.Update(ctx, existing), "failed to update cluster certificate")
	}
	if !k8serrors.IsNotFound(err) {
		return errors.Wrap(err, "failed to get cluster certificate")
	}

	cert := &v1.Certificate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      certName,
			Namespace: cluster.Namespace,
			Labels: naming.WithPerconaLabels(map[string]string{
				naming.LabelCluster:            cluster.Name,
				naming.LabelClusterCertificate: "postgres-tls",
			}, cluster.Name, "", cluster.Labels[naming.LabelVersion]),
		},
		Spec: v1.CertificateSpec{
			SecretName:  certName,
			CommonName:  cluster.Name + "-postgres",
			DNSNames:    dnsNames,
			IssuerRef:   wantIssuerRef,
			Duration:    &metav1.Duration{Duration: certDuration},
			RenewBefore: &metav1.Duration{Duration: DefaultRenewBefore},
			PrivateKey: &v1.CertificatePrivateKey{
				Algorithm:      v1.ECDSAKeyAlgorithm,
				Size:           256,
				RotationPolicy: v1.RotationPolicyNever,
			},
			Usages: []v1.KeyUsage{
				v1.UsageServerAuth,
				v1.UsageClientAuth,
				v1.UsageDigitalSignature,
				v1.UsageKeyEncipherment,
			},
			SecretTemplate: &v1.CertificateSecretTemplate{
				Labels: naming.WithPerconaLabels(map[string]string{
					naming.LabelCluster:            cluster.Name,
					naming.LabelClusterCertificate: "postgres-tls",
				}, cluster.Name, "", cluster.Labels[naming.LabelVersion]),
			},
		},
	}

	if err := controllerutil.SetControllerReference(cluster, cert, c.scheme); err != nil {
		return errors.Wrap(err, "failed to set controller reference")
	}

	if err := c.cl.Create(ctx, cert); err != nil {
		return errors.Wrap(err, "failed to create cluster certificate")
	}

	return nil
}

// ApplyInstanceCertificate creates a cert-manager Certificate resource for a specific
// PostgreSQL instance.
func (c *controller) ApplyInstanceCertificate(ctx context.Context, cluster *v1beta1.PostgresCluster, instanceName string, dnsNames []string) error {
	if len(dnsNames) == 0 {
		return errors.New("dnsNames cannot be empty")
	}

	mode, err := ResolveIssuerMode(ctx, c.cl, cluster)
	if err != nil {
		return errors.Wrap(err, "failed to resolve issuer mode")
	}
	wantIssuerRef := issuerRef(cluster, mode)

	certName := InstanceCertificateName(instanceName)
	secretName := instanceName + "-certs"

	certDuration := DefaultCertDuration
	if cluster.Spec.TLS != nil && cluster.Spec.TLS.CertValidityDuration != nil {
		certDuration = cluster.Spec.TLS.CertValidityDuration.Duration
	}

	existing := &v1.Certificate{}
	err = c.cl.Get(ctx, types.NamespacedName{Name: certName, Namespace: cluster.Namespace}, existing)
	if err == nil {
		needsUpdate := false

		hasOwnerRef, err := controllerutil.HasOwnerReference(existing.OwnerReferences, cluster, c.scheme)
		if err != nil {
			return errors.Wrap(err, "check owner reference")
		}

		if !hasOwnerRef {
			gvk := v1beta1.SchemeBuilder.GroupVersion.WithKind("PostgresCluster")
			existing.OwnerReferences = []metav1.OwnerReference{{
				APIVersion:         gvk.GroupVersion().String(),
				Kind:               gvk.Kind,
				Name:               cluster.GetName(),
				UID:                cluster.GetUID(),
				BlockOwnerDeletion: ptr.To(true),
				Controller:         ptr.To(true),
			}}
			needsUpdate = true
		}

		if existing.Spec.Duration != nil && existing.Spec.Duration.Duration != certDuration {
			existing.Spec.Duration = &metav1.Duration{Duration: certDuration}
			needsUpdate = true
		}

		if existing.Spec.IssuerRef != wantIssuerRef {
			existing.Spec.IssuerRef = wantIssuerRef
			needsUpdate = true
		}

		if !needsUpdate {
			return nil
		}

		return errors.Wrap(c.cl.Update(ctx, existing), "failed to update instance certificate")
	}
	if !k8serrors.IsNotFound(err) {
		return errors.Wrap(err, "failed to get instance certificate")
	}

	cert := &v1.Certificate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      certName,
			Namespace: cluster.Namespace,
			Labels: naming.WithPerconaLabels(map[string]string{
				naming.LabelCluster:  cluster.Name,
				naming.LabelInstance: instanceName,
			}, cluster.Name, "", cluster.Labels[naming.LabelVersion]),
		},
		Spec: v1.CertificateSpec{
			SecretName:  secretName,
			CommonName:  instanceName,
			DNSNames:    dnsNames,
			IssuerRef:   wantIssuerRef,
			Duration:    &metav1.Duration{Duration: certDuration},
			RenewBefore: &metav1.Duration{Duration: DefaultRenewBefore},
			PrivateKey: &v1.CertificatePrivateKey{
				Algorithm:      v1.ECDSAKeyAlgorithm,
				Size:           256,
				RotationPolicy: v1.RotationPolicyNever,
			},
			Usages: []v1.KeyUsage{
				v1.UsageServerAuth,
				v1.UsageClientAuth,
				v1.UsageDigitalSignature,
				v1.UsageKeyEncipherment,
			},
			SecretTemplate: &v1.CertificateSecretTemplate{
				Labels: naming.WithPerconaLabels(map[string]string{
					naming.LabelCluster:  cluster.Name,
					naming.LabelInstance: instanceName,
				}, cluster.Name, "", cluster.Labels[naming.LabelVersion]),
			},
		},
	}

	if err := controllerutil.SetControllerReference(cluster, cert, c.scheme); err != nil {
		return errors.Wrap(err, "failed to set controller reference")
	}

	if err := c.cl.Create(ctx, cert); err != nil {
		return errors.Wrap(err, "failed to create instance certificate")
	}

	return nil
}

// ApplyPGBouncerCertificate creates a cert-manager Certificate resource for PgBouncer.
func (c *controller) ApplyPGBouncerCertificate(ctx context.Context, cluster *v1beta1.PostgresCluster, dnsNames []string) error {
	if len(dnsNames) == 0 {
		return errors.New("dnsNames cannot be empty")
	}

	mode, err := ResolveIssuerMode(ctx, c.cl, cluster)
	if err != nil {
		return errors.Wrap(err, "failed to resolve issuer mode")
	}
	wantIssuerRef := issuerRef(cluster, mode)

	secretMeta := naming.ClusterPGBouncer(cluster)
	certName := PGBouncerCertificateName(cluster)

	certDuration := DefaultCertDuration
	if cluster.Spec.TLS != nil && cluster.Spec.TLS.CertValidityDuration != nil {
		certDuration = cluster.Spec.TLS.CertValidityDuration.Duration
	}

	existing := &v1.Certificate{}
	err = c.cl.Get(ctx, types.NamespacedName{Name: certName, Namespace: cluster.Namespace}, existing)
	if err == nil {
		needsUpdate := false

		hasOwnerRef, err := controllerutil.HasOwnerReference(existing.OwnerReferences, cluster, c.scheme)
		if err != nil {
			return errors.Wrap(err, "check owner reference")
		}

		if !hasOwnerRef {
			gvk := v1beta1.SchemeBuilder.GroupVersion.WithKind("PostgresCluster")
			existing.OwnerReferences = []metav1.OwnerReference{{
				APIVersion:         gvk.GroupVersion().String(),
				Kind:               gvk.Kind,
				Name:               cluster.GetName(),
				UID:                cluster.GetUID(),
				BlockOwnerDeletion: ptr.To(true),
				Controller:         ptr.To(true),
			}}
			needsUpdate = true
		}

		if existing.Spec.Duration != nil && existing.Spec.Duration.Duration != certDuration {
			existing.Spec.Duration = &metav1.Duration{Duration: certDuration}
			needsUpdate = true
		}

		if existing.Spec.IssuerRef != wantIssuerRef {
			existing.Spec.IssuerRef = wantIssuerRef
			needsUpdate = true
		}

		if !needsUpdate {
			return nil
		}

		return errors.Wrap(c.cl.Update(ctx, existing), "failed to update pgbouncer certificate")
	}
	if !k8serrors.IsNotFound(err) {
		return errors.Wrap(err, "failed to get pgbouncer certificate")
	}

	cert := &v1.Certificate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      certName,
			Namespace: cluster.Namespace,
			Labels: naming.WithPerconaLabels(map[string]string{
				naming.LabelCluster: cluster.Name,
				naming.LabelRole:    naming.RolePGBouncer,
			}, cluster.Name, "pgbouncer", cluster.Labels[naming.LabelVersion]),
		},
		Spec: v1.CertificateSpec{
			SecretName:  secretMeta.Name + "-frontend-tls",
			CommonName:  truncateForCommonName(cluster.Name, "-pgbouncer"),
			DNSNames:    dnsNames,
			IssuerRef:   wantIssuerRef,
			Duration:    &metav1.Duration{Duration: certDuration},
			RenewBefore: &metav1.Duration{Duration: DefaultRenewBefore},
			PrivateKey: &v1.CertificatePrivateKey{
				Algorithm:      v1.ECDSAKeyAlgorithm,
				Size:           256,
				RotationPolicy: v1.RotationPolicyNever,
			},
			Usages: []v1.KeyUsage{
				v1.UsageServerAuth,
				v1.UsageClientAuth,
				v1.UsageDigitalSignature,
				v1.UsageKeyEncipherment,
			},
			SecretTemplate: &v1.CertificateSecretTemplate{
				Labels: naming.WithPerconaLabels(map[string]string{
					naming.LabelCluster: cluster.Name,
					naming.LabelRole:    naming.RolePGBouncer,
				}, cluster.Name, "pgbouncer", cluster.Labels[naming.LabelVersion]),
			},
		},
	}

	if err := controllerutil.SetControllerReference(cluster, cert, c.scheme); err != nil {
		return errors.Wrap(err, "failed to set controller reference")
	}

	if err := c.cl.Create(ctx, cert); err != nil {
		return errors.Wrap(err, "failed to create pgbouncer certificate")
	}

	return nil
}

// ApplyReplicationCertificate creates a cert-manager Certificate resource for the replication client.
func (c *controller) ApplyReplicationCertificate(ctx context.Context, cluster *v1beta1.PostgresCluster) error {
	mode, err := ResolveIssuerMode(ctx, c.cl, cluster)
	if err != nil {
		return errors.Wrap(err, "failed to resolve issuer mode")
	}
	wantIssuerRef := issuerRef(cluster, mode)

	secretMeta := naming.ReplicationClientCertSecret(cluster)
	certName := ReplicationCertificateName(cluster)
	commonName := "_crunchyrepl"

	certDuration := DefaultCertDuration
	if cluster.Spec.TLS != nil && cluster.Spec.TLS.CertValidityDuration != nil {
		certDuration = cluster.Spec.TLS.CertValidityDuration.Duration
	}

	existing := &v1.Certificate{}
	err = c.cl.Get(ctx, types.NamespacedName{Name: certName, Namespace: cluster.Namespace}, existing)
	if err == nil {
		needsUpdate := false

		hasOwnerRef, err := controllerutil.HasOwnerReference(existing.OwnerReferences, cluster, c.scheme)
		if err != nil {
			return errors.Wrap(err, "check owner reference")
		}

		if !hasOwnerRef {
			gvk := v1beta1.SchemeBuilder.GroupVersion.WithKind("PostgresCluster")
			existing.OwnerReferences = []metav1.OwnerReference{{
				APIVersion:         gvk.GroupVersion().String(),
				Kind:               gvk.Kind,
				Name:               cluster.GetName(),
				UID:                cluster.GetUID(),
				BlockOwnerDeletion: ptr.To(true),
				Controller:         ptr.To(true),
			}}
			needsUpdate = true
		}

		if existing.Spec.Duration != nil && existing.Spec.Duration.Duration != certDuration {
			existing.Spec.Duration = &metav1.Duration{Duration: certDuration}
			needsUpdate = true
		}

		if existing.Spec.IssuerRef != wantIssuerRef {
			existing.Spec.IssuerRef = wantIssuerRef
			needsUpdate = true
		}

		if !needsUpdate {
			return nil
		}

		return errors.Wrap(c.cl.Update(ctx, existing), "failed to update replication certificate")
	}
	if !k8serrors.IsNotFound(err) {
		return errors.Wrap(err, "failed to get replication certificate")
	}

	cert := &v1.Certificate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      certName,
			Namespace: cluster.Namespace,
			Labels: naming.WithPerconaLabels(map[string]string{
				naming.LabelCluster:            cluster.Name,
				naming.LabelClusterCertificate: "replication-client-tls",
			}, cluster.Name, "", cluster.Labels[naming.LabelVersion]),
		},
		Spec: v1.CertificateSpec{
			SecretName:  secretMeta.Name,
			CommonName:  commonName,
			DNSNames:    []string{commonName},
			IssuerRef:   wantIssuerRef,
			Duration:    &metav1.Duration{Duration: certDuration},
			RenewBefore: &metav1.Duration{Duration: DefaultRenewBefore},
			PrivateKey: &v1.CertificatePrivateKey{
				Algorithm:      v1.ECDSAKeyAlgorithm,
				Size:           256,
				RotationPolicy: v1.RotationPolicyNever,
			},
			Usages: []v1.KeyUsage{
				v1.UsageClientAuth,
				v1.UsageDigitalSignature,
				v1.UsageKeyEncipherment,
			},
			SecretTemplate: &v1.CertificateSecretTemplate{
				Labels: naming.WithPerconaLabels(map[string]string{
					naming.LabelCluster:            cluster.Name,
					naming.LabelClusterCertificate: "replication-client-tls",
				}, cluster.Name, "", cluster.Labels[naming.LabelVersion]),
			},
		},
	}

	if err := controllerutil.SetControllerReference(cluster, cert, c.scheme); err != nil {
		return errors.Wrap(err, "failed to set controller reference")
	}

	if err := c.cl.Create(ctx, cert); err != nil {
		return errors.Wrap(err, "failed to create replication certificate")
	}

	return nil
}

// ApplyPGBackRestClientCertificate creates a cert-manager Certificate resource
// for the pgBackRest client used by all PostgreSQL instances to connect to the
// repository host.
func (c *controller) ApplyPGBackRestClientCertificate(ctx context.Context, cluster *v1beta1.PostgresCluster) error {
	mode, err := ResolveIssuerMode(ctx, c.cl, cluster)
	if err != nil {
		return errors.Wrap(err, "failed to resolve issuer mode")
	}
	wantIssuerRef := issuerRef(cluster, mode)

	secretMeta := naming.PGBackRestClientCertSecret(cluster)
	certName := PGBackRestClientCertificateName(cluster)

	certDuration := DefaultCertDuration
	if cluster.Spec.TLS != nil && cluster.Spec.TLS.PGBackRestCertValidityDuration != nil {
		certDuration = cluster.Spec.TLS.PGBackRestCertValidityDuration.Duration
	}

	// The common name must match what pgBackRest expects in its tls-server-auth option.
	// All instances in the cluster share a single client certificate identified by cluster UID.
	commonName := "pgbackrest@" + string(cluster.GetUID())

	existing := &v1.Certificate{}
	err = c.cl.Get(ctx, types.NamespacedName{Name: certName, Namespace: cluster.Namespace}, existing)
	if err == nil {
		needsUpdate := false

		hasOwnerRef, err := controllerutil.HasOwnerReference(existing.OwnerReferences, cluster, c.scheme)
		if err != nil {
			return errors.Wrap(err, "check owner reference")
		}

		if !hasOwnerRef {
			gvk := v1beta1.SchemeBuilder.GroupVersion.WithKind("PostgresCluster")
			existing.OwnerReferences = []metav1.OwnerReference{{
				APIVersion:         gvk.GroupVersion().String(),
				Kind:               gvk.Kind,
				Name:               cluster.GetName(),
				UID:                cluster.GetUID(),
				BlockOwnerDeletion: ptr.To(true),
				Controller:         ptr.To(true),
			}}
			needsUpdate = true
		}

		if existing.Spec.Duration != nil && existing.Spec.Duration.Duration != certDuration {
			existing.Spec.Duration = &metav1.Duration{Duration: certDuration}
			needsUpdate = true
		}

		if existing.Spec.CommonName != commonName {
			existing.Spec.CommonName = commonName
			existing.Spec.DNSNames = []string{commonName}
			needsUpdate = true
		}

		if existing.Spec.IssuerRef != wantIssuerRef {
			existing.Spec.IssuerRef = wantIssuerRef
			needsUpdate = true
		}

		if !needsUpdate {
			return nil
		}

		return errors.Wrap(c.cl.Update(ctx, existing), "failed to update pgbackrest client certificate")
	}
	if !k8serrors.IsNotFound(err) {
		return errors.Wrap(err, "failed to get pgbackrest client certificate")
	}

	cert := &v1.Certificate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      certName,
			Namespace: cluster.Namespace,
			Labels: naming.WithPerconaLabels(map[string]string{
				naming.LabelCluster: cluster.Name,
			}, cluster.Name, "", cluster.Labels[naming.LabelVersion]),
		},
		Spec: v1.CertificateSpec{
			SecretName:  secretMeta.Name,
			CommonName:  commonName,
			DNSNames:    []string{commonName},
			IssuerRef:   wantIssuerRef,
			Duration:    &metav1.Duration{Duration: certDuration},
			RenewBefore: &metav1.Duration{Duration: DefaultRenewBefore},
			PrivateKey: &v1.CertificatePrivateKey{
				Algorithm:      v1.ECDSAKeyAlgorithm,
				Size:           256,
				RotationPolicy: v1.RotationPolicyNever,
			},
			Usages: []v1.KeyUsage{
				v1.UsageClientAuth,
				v1.UsageDigitalSignature,
				v1.UsageKeyEncipherment,
			},
			SecretTemplate: &v1.CertificateSecretTemplate{
				Labels: naming.WithPerconaLabels(map[string]string{
					naming.LabelCluster: cluster.Name,
				}, cluster.Name, "", cluster.Labels[naming.LabelVersion]),
			},
		},
	}

	if err := controllerutil.SetControllerReference(cluster, cert, c.scheme); err != nil {
		return errors.Wrap(err, "failed to set controller reference")
	}

	if err := c.cl.Create(ctx, cert); err != nil {
		return errors.Wrap(err, "failed to create pgbackrest client certificate")
	}

	return nil
}

// ApplyPGBackRestRepoCertificate creates a cert-manager Certificate resource
// for the pgBackRest repository host server.
func (c *controller) ApplyPGBackRestRepoCertificate(ctx context.Context, cluster *v1beta1.PostgresCluster, dnsNames []string) error {
	if len(dnsNames) == 0 {
		return errors.New("dnsNames cannot be empty")
	}

	mode, err := ResolveIssuerMode(ctx, c.cl, cluster)
	if err != nil {
		return errors.Wrap(err, "failed to resolve issuer mode")
	}
	wantIssuerRef := issuerRef(cluster, mode)

	secretMeta := naming.PGBackRestRepoCertSecret(cluster)
	certName := PGBackRestRepoCertificateName(cluster)

	certDuration := DefaultCertDuration
	if cluster.Spec.TLS != nil && cluster.Spec.TLS.PGBackRestCertValidityDuration != nil {
		certDuration = cluster.Spec.TLS.PGBackRestCertValidityDuration.Duration
	}

	existing := &v1.Certificate{}
	err = c.cl.Get(ctx, types.NamespacedName{Name: certName, Namespace: cluster.Namespace}, existing)
	if err == nil {
		needsUpdate := false

		hasOwnerRef, err := controllerutil.HasOwnerReference(existing.OwnerReferences, cluster, c.scheme)
		if err != nil {
			return errors.Wrap(err, "check owner reference")
		}

		if !hasOwnerRef {
			gvk := v1beta1.SchemeBuilder.GroupVersion.WithKind("PostgresCluster")
			existing.OwnerReferences = []metav1.OwnerReference{{
				APIVersion:         gvk.GroupVersion().String(),
				Kind:               gvk.Kind,
				Name:               cluster.GetName(),
				UID:                cluster.GetUID(),
				BlockOwnerDeletion: ptr.To(true),
				Controller:         ptr.To(true),
			}}
			needsUpdate = true
		}

		if existing.Spec.Duration != nil && existing.Spec.Duration.Duration != certDuration {
			existing.Spec.Duration = &metav1.Duration{Duration: certDuration}
			needsUpdate = true
		}

		if existing.Spec.IssuerRef != wantIssuerRef {
			existing.Spec.IssuerRef = wantIssuerRef
			needsUpdate = true
		}

		if !needsUpdate {
			return nil
		}

		return errors.Wrap(c.cl.Update(ctx, existing), "failed to update pgbackrest repo certificate")
	}
	if !k8serrors.IsNotFound(err) {
		return errors.Wrap(err, "failed to get pgbackrest repo certificate")
	}

	cert := &v1.Certificate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      certName,
			Namespace: cluster.Namespace,
			Labels: naming.WithPerconaLabels(map[string]string{
				naming.LabelCluster: cluster.Name,
			}, cluster.Name, "", cluster.Labels[naming.LabelVersion]),
		},
		Spec: v1.CertificateSpec{
			SecretName:  secretMeta.Name,
			CommonName:  truncateForCommonName(cluster.Name, "-pgbackrest-repo"),
			DNSNames:    dnsNames,
			IssuerRef:   wantIssuerRef,
			Duration:    &metav1.Duration{Duration: certDuration},
			RenewBefore: &metav1.Duration{Duration: DefaultRenewBefore},
			PrivateKey: &v1.CertificatePrivateKey{
				Algorithm:      v1.ECDSAKeyAlgorithm,
				Size:           256,
				RotationPolicy: v1.RotationPolicyNever,
			},
			Usages: []v1.KeyUsage{
				v1.UsageServerAuth,
				v1.UsageDigitalSignature,
				v1.UsageKeyEncipherment,
			},
			SecretTemplate: &v1.CertificateSecretTemplate{
				Labels: naming.WithPerconaLabels(map[string]string{
					naming.LabelCluster: cluster.Name,
				}, cluster.Name, "", cluster.Labels[naming.LabelVersion]),
			},
		},
	}

	if err := controllerutil.SetControllerReference(cluster, cert, c.scheme); err != nil {
		return errors.Wrap(err, "failed to set controller reference")
	}

	if err := c.cl.Create(ctx, cert); err != nil {
		return errors.Wrap(err, "failed to create pgbackrest repo certificate")
	}

	return nil
}

func translateCheckError(err error) error {
	const crdsMapping3Error = `error finding the scope of the object: failed to get restmapping: unable to retrieve the complete list of server APIs: cert-manager.io/v1: no matches for cert-manager.io/v1, Resource=`
	// TODO: remove as soon as TranslateToSimpleError uses this regexp
	regexErrCertManagerCRDsNotFound := regexp.MustCompile(`^(` + regexp.QuoteMeta(crdsMapping3Error) + `)$`)

	if regexErrCertManagerCRDsNotFound.MatchString(err.Error()) {
		return cmapichecker.ErrCertManagerCRDsNotFound
	}

	return cmapichecker.TranslateToSimpleError(err)
}

const maxCommonNameLength = 64

func truncateForCommonName(clusterName, suffix string) string {
	maxLen := maxCommonNameLength - len(suffix)
	if len(clusterName) > maxLen {
		clusterName = clusterName[:maxLen]
	}
	return clusterName + suffix
}
