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
	cl     client.Client
	scheme *runtime.Scheme
	dryRun bool
}

var _ Controller = new(controller)

type NewControllerFunc func(cl client.Client, scheme *runtime.Scheme, dryRun bool) Controller

func NewController(cl client.Client, scheme *runtime.Scheme, dryRun bool) Controller {
	if dryRun {
		cl = client.NewDryRunClient(cl)
	}
	return &controller{
		cl:     cl,
		scheme: scheme,
		dryRun: dryRun,
	}
}

var (
	ErrCertManagerNotFound = errors.New("cert-manager is not found")
	ErrCertManagerNotReady = errors.New("cert-manager is not ready")
)

func (c *controller) Check(ctx context.Context, config *rest.Config, ns string) error {
	log := logf.FromContext(ctx)
	checker, err := cmapichecker.New(config, ns)
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
	issuer := &v1.Issuer{
		ObjectMeta: naming.TLSIssuer(cluster),
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

	err := c.cl.Create(ctx, issuer)
	if err != nil {
		return err
	}

	return nil
}

// ApplyCAIssuer creates a SelfSigned Issuer resource for the given PostgresCluster.
func (c *controller) ApplyCAIssuer(ctx context.Context, cluster *v1beta1.PostgresCluster) error {
	issuer := &v1.Issuer{
		ObjectMeta: naming.CAIssuer(cluster),
		Spec: v1.IssuerSpec{
			IssuerConfig: v1.IssuerConfig{
				SelfSigned: &v1.SelfSignedIssuer{},
			},
		},
	}

	if err := controllerutil.SetControllerReference(cluster, issuer, c.scheme); err != nil {
		return errors.Wrap(err, "failed to set controller reference")
	}

	err := c.cl.Create(ctx, issuer)
	if err != nil {
		return errors.Wrap(err, "failed to create the issuer")
	}

	return nil
}

func (c *controller) ApplyCACertificate(ctx context.Context, cluster *v1beta1.PostgresCluster) error {
	cert := &v1.Certificate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      naming.PostgresRootCASecret(cluster).Name,
			Namespace: cluster.Namespace,
			Labels:    cluster.Labels,
		},
		Spec: v1.CertificateSpec{
			SecretName: naming.PostgresRootCASecret(cluster).Name,
			CommonName: cluster.Name + "-ca",
			IsCA:       true,
			IssuerRef: cmmeta.ObjectReference{
				Name: naming.CAIssuer(cluster).Name,
				Kind: v1.IssuerKind,
			},
			Duration:    &metav1.Duration{Duration: time.Hour * 24 * 365},
			RenewBefore: &metav1.Duration{Duration: 730 * time.Hour},
			PrivateKey: &v1.CertificatePrivateKey{
				Algorithm: v1.ECDSAKeyAlgorithm,
				Size:      256,
			},
		},
	}

	if err := controllerutil.SetControllerReference(cluster, cert, c.scheme); err != nil {
		return errors.Wrap(err, "failed to set controller reference")
	}

	err := c.cl.Create(ctx, cert)
	if err != nil {
		return errors.Wrap(err, "failed to create the issuer")
	}

	return nil
}

// ApplyClusterCertificate creates a cert-manager Certificate resource for the cluster's
// primary and replica services TLS certificate.
func (c *controller) ApplyClusterCertificate(ctx context.Context, cluster *v1beta1.PostgresCluster, dnsNames []string) error {
	log := logf.FromContext(ctx).WithName("ApplyClusterCertificate")

	if len(dnsNames) == 0 {
		return errors.New("dnsNames cannot be empty")
	}

	cert := &v1.Certificate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      naming.PostgresTLSSecret(cluster).Name,
			Namespace: cluster.Namespace,
		},
		Spec: v1.CertificateSpec{
			SecretName: naming.PostgresTLSSecret(cluster).Name,
			CommonName: cluster.Name + "-postgres",
			DNSNames:   dnsNames,
			IssuerRef: cmmeta.ObjectReference{
				Name: naming.TLSIssuer(cluster).Name,
				Kind: v1.IssuerKind,
			},
			Duration:    &metav1.Duration{Duration: DefaultCertDuration},
			RenewBefore: &metav1.Duration{Duration: DefaultRenewBefore},
			PrivateKey: &v1.CertificatePrivateKey{
				Algorithm: v1.ECDSAKeyAlgorithm,
				Size:      256,
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

	err := c.cl.Create(ctx, cert)
	if err != nil {
		if k8serrors.IsAlreadyExists(err) {
			log.V(1).Info("cluster certificate already exists", "name", cert.Name)
			return nil
		}
		return errors.Wrap(err, "failed to create cluster certificate")
	}

	log.Info("created cluster certificate", "name", cert.Name)
	return nil
}

// ApplyInstanceCertificate creates a cert-manager Certificate resource for a specific
// PostgreSQL instance.
func (c *controller) ApplyInstanceCertificate(ctx context.Context, cluster *v1beta1.PostgresCluster, instanceName string, dnsNames []string) error {
	log := logf.FromContext(ctx).WithName("ApplyInstanceCertificate")

	if len(dnsNames) == 0 {
		return errors.New("dnsNames cannot be empty")
	}

	certName := instanceName + "-cert"
	secretName := instanceName + "-certs"

	cert := &v1.Certificate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      certName,
			Namespace: cluster.Namespace,
		},
		Spec: v1.CertificateSpec{
			SecretName: secretName,
			CommonName: instanceName,
			DNSNames:   dnsNames,
			IssuerRef: cmmeta.ObjectReference{
				Name: naming.TLSIssuer(cluster).Name,
				Kind: v1.IssuerKind,
			},
			Duration:    &metav1.Duration{Duration: DefaultCertDuration},
			RenewBefore: &metav1.Duration{Duration: DefaultRenewBefore},
			PrivateKey: &v1.CertificatePrivateKey{
				Algorithm: v1.ECDSAKeyAlgorithm,
				Size:      256,
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

	err := c.cl.Create(ctx, cert)
	if err != nil {
		if k8serrors.IsAlreadyExists(err) {
			log.V(1).Info("instance certificate already exists", "name", cert.Name)
			return nil
		}
		return errors.Wrap(err, "failed to create instance certificate")
	}

	log.Info("created instance certificate", "name", cert.Name, "instance", instanceName)
	return nil
}

// ApplyPGBouncerCertificate creates a cert-manager Certificate resource for PgBouncer.
func (c *controller) ApplyPGBouncerCertificate(ctx context.Context, cluster *v1beta1.PostgresCluster, dnsNames []string) error {
	log := logf.FromContext(ctx).WithName("ApplyPGBouncerCertificate")

	if len(dnsNames) == 0 {
		return errors.New("dnsNames cannot be empty")
	}

	secretMeta := naming.ClusterPGBouncer(cluster)
	certName := cluster.Name + "-pgbouncer-cert"

	cert := &v1.Certificate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      certName,
			Namespace: cluster.Namespace,
		},
		Spec: v1.CertificateSpec{
			SecretName: secretMeta.Name + "-frontend-tls",
			CommonName: cluster.Name + "-pgbouncer",
			DNSNames:   dnsNames,
			IssuerRef: cmmeta.ObjectReference{
				Name: naming.TLSIssuer(cluster).Name,
				Kind: v1.IssuerKind,
			},
			Duration:    &metav1.Duration{Duration: DefaultCertDuration},
			RenewBefore: &metav1.Duration{Duration: DefaultRenewBefore},
			PrivateKey: &v1.CertificatePrivateKey{
				Algorithm: v1.ECDSAKeyAlgorithm,
				Size:      256,
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

	err := c.cl.Create(ctx, cert)
	if err != nil {
		if k8serrors.IsAlreadyExists(err) {
			log.V(1).Info("pgbouncer certificate already exists", "name", cert.Name)
			return nil
		}
		return errors.Wrap(err, "failed to create pgbouncer certificate")
	}

	log.Info("created pgbouncer certificate", "name", cert.Name)
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
