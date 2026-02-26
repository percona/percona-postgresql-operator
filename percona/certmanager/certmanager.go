package certmanager

import (
	"context"
	"regexp"
	"time"

	v1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	cmmeta "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	"github.com/cert-manager/cert-manager/pkg/util/cmapichecker"
	"github.com/pkg/errors"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/percona/percona-postgresql-operator/v2/internal/naming"
	"github.com/percona/percona-postgresql-operator/v2/pkg/apis/postgres-operator.crunchydata.com/v1beta1"
)

type Controller interface {
	Check(ctx context.Context, config *rest.Config, ns string) error
	ApplyIssuer(ctx context.Context, cluster *v1beta1.PostgresCluster) error
	ApplyCAIssuer(ctx context.Context, cluster *v1beta1.PostgresCluster) error
	ApplyCACertificate(ctx context.Context, cluster *v1beta1.PostgresCluster) error

	ApplyClusterCertificate(ctx context.Context, cluster *v1beta1.PostgresCluster, dnsNames []string) error
	ApplyInstanceCertificate(ctx context.Context, cluster *v1beta1.PostgresCluster, instanceName string, dnsNames []string) error
	ApplyPGBouncerCertificate(ctx context.Context, cluster *v1beta1.PostgresCluster, dnsNames []string) error
}

const (
	// DefaultCertDuration is the default certificate duration: 1 year
	DefaultCertDuration = 365 * 24 * time.Hour

	// DefaultRenewBefore is the default renewal time: 30 days before expiry
	DefaultRenewBefore = 30 * 24 * time.Hour
)

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
			log.Error(cmapichecker.TranslateToSimpleError(err), "cert-manager is not ready")
			return ErrCertManagerNotReady
		}
		return err
	}
	return nil
}

func (c *controller) ApplyIssuer(ctx context.Context, cluster *v1beta1.PostgresCluster) error {
	meta := naming.TLSIssuer(cluster)

	existing := &v1.Issuer{}
	err := c.cl.Get(ctx, types.NamespacedName{Name: meta.Name, Namespace: meta.Namespace}, existing)
	if err == nil {
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

// ApplyCAIssuer creates a SelfSigned Issuer resource for the given PostgresCluster.
func (c *controller) ApplyCAIssuer(ctx context.Context, cluster *v1beta1.PostgresCluster) error {
	meta := naming.CAIssuer(cluster)

	existing := &v1.Issuer{}
	err := c.cl.Get(ctx, types.NamespacedName{Name: meta.Name, Namespace: meta.Namespace}, existing)
	if err == nil {
		return nil
	}
	if !k8serrors.IsNotFound(err) {
		return errors.Wrap(err, "failed to get CA issuer")
	}

	issuer := &v1.Issuer{
		ObjectMeta: meta,
		Spec: v1.IssuerSpec{
			IssuerConfig: v1.IssuerConfig{
				SelfSigned: &v1.SelfSignedIssuer{},
			},
		},
	}

	if err := controllerutil.SetControllerReference(cluster, issuer, c.scheme); err != nil {
		return errors.Wrap(err, "failed to set controller reference")
	}

	if err := c.cl.Create(ctx, issuer); err != nil {
		return errors.Wrap(err, "failed to create ca issuer")
	}

	return nil
}

func (c *controller) ApplyCACertificate(ctx context.Context, cluster *v1beta1.PostgresCluster) error {
	certName := naming.PostgresRootCASecret(cluster).Name

	existing := &v1.Certificate{}
	err := c.cl.Get(ctx, types.NamespacedName{Name: certName, Namespace: cluster.Namespace}, existing)
	if err == nil {
		return nil
	}
	if !k8serrors.IsNotFound(err) {
		return errors.Wrap(err, "failed to get CA certificate")
	}

	caDuration := DefaultCertDuration
	if cluster.Spec.TLS != nil && cluster.Spec.TLS.CAValidityDuration != nil {
		caDuration = cluster.Spec.TLS.CAValidityDuration.Duration
	}

	cert := &v1.Certificate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      certName,
			Namespace: cluster.Namespace,
			Labels:    cluster.Labels,
		},
		Spec: v1.CertificateSpec{
			SecretName: certName,
			CommonName: cluster.Name + "-ca",
			IsCA:       true,
			IssuerRef: cmmeta.IssuerReference{
				Name: naming.CAIssuer(cluster).Name,
				Kind: v1.IssuerKind,
			},
			Duration:    &metav1.Duration{Duration: caDuration},
			RenewBefore: &metav1.Duration{Duration: DefaultRenewBefore},
			PrivateKey: &v1.CertificatePrivateKey{
				Algorithm:      v1.ECDSAKeyAlgorithm,
				Size:           256,
				RotationPolicy: v1.RotationPolicyNever,
			},
		},
	}

	if err := controllerutil.SetControllerReference(cluster, cert, c.scheme); err != nil {
		return errors.Wrap(err, "failed to set controller reference")
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

	certName := naming.PostgresTLSSecret(cluster).Name

	existing := &v1.Certificate{}
	err := c.cl.Get(ctx, types.NamespacedName{Name: certName, Namespace: cluster.Namespace}, existing)
	if err == nil {
		return nil
	}
	if !k8serrors.IsNotFound(err) {
		return errors.Wrap(err, "failed to get cluster certificate")
	}

	certDuration := DefaultCertDuration
	if cluster.Spec.TLS != nil && cluster.Spec.TLS.CertValidityDuration != nil {
		certDuration = cluster.Spec.TLS.CertValidityDuration.Duration
	}

	cert := &v1.Certificate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      certName,
			Namespace: cluster.Namespace,
		},
		Spec: v1.CertificateSpec{
			SecretName: certName,
			CommonName: cluster.Name + "-postgres",
			DNSNames:   dnsNames,
			IssuerRef: cmmeta.ObjectReference{
				Name: naming.TLSIssuer(cluster).Name,
				Kind: v1.IssuerKind,
			},
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

	certName := instanceName + "-cert"
	secretName := instanceName + "-certs"

	existing := &v1.Certificate{}
	err := c.cl.Get(ctx, types.NamespacedName{Name: certName, Namespace: cluster.Namespace}, existing)
	if err == nil {
		return nil
	}
	if !k8serrors.IsNotFound(err) {
		return errors.Wrap(err, "failed to get instance certificate")
	}

	certDuration := DefaultCertDuration
	if cluster.Spec.TLS != nil && cluster.Spec.TLS.CertValidityDuration != nil {
		certDuration = cluster.Spec.TLS.CertValidityDuration.Duration
	}

	cert := &v1.Certificate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      certName,
			Namespace: cluster.Namespace,
		},
		Spec: v1.CertificateSpec{
			SecretName: secretName,
			CommonName: instanceName,
			DNSNames:   dnsNames,
			IssuerRef: cmmeta.IssuerReference{
				Name: naming.TLSIssuer(cluster).Name,
				Kind: v1.IssuerKind,
			},
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

	secretMeta := naming.ClusterPGBouncer(cluster)
	certName := cluster.Name + "-pgbouncer-cert"

	existing := &v1.Certificate{}
	err := c.cl.Get(ctx, types.NamespacedName{Name: certName, Namespace: cluster.Namespace}, existing)
	if err == nil {
		return nil
	}
	if !k8serrors.IsNotFound(err) {
		return errors.Wrap(err, "failed to get pgbouncer certificate")
	}

	certDuration := DefaultCertDuration
	if cluster.Spec.TLS != nil && cluster.Spec.TLS.CertValidityDuration != nil {
		certDuration = cluster.Spec.TLS.CertValidityDuration.Duration
	}

	cert := &v1.Certificate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      certName,
			Namespace: cluster.Namespace,
		},
		Spec: v1.CertificateSpec{
			SecretName: secretMeta.Name + "-frontend-tls",
			CommonName: cluster.Name + "-pgbouncer",
			DNSNames:   dnsNames,
			IssuerRef: cmmeta.IssuerReference{
				Name: naming.TLSIssuer(cluster).Name,
				Kind: v1.IssuerKind,
			},
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

func translateCheckError(err error) error {
	const crdsMapping3Error = `error finding the scope of the object: failed to get restmapping: unable to retrieve the complete list of server APIs: cert-manager.io/v1: no matches for cert-manager.io/v1, Resource=`
	// TODO: remove as soon as TranslateToSimpleError uses this regexp
	regexErrCertManagerCRDsNotFound := regexp.MustCompile(`^(` + regexp.QuoteMeta(crdsMapping3Error) + `)$`)

	if regexErrCertManagerCRDsNotFound.MatchString(err.Error()) {
		return cmapichecker.ErrCertManagerCRDsNotFound
	}

	return cmapichecker.TranslateToSimpleError(err)
}
