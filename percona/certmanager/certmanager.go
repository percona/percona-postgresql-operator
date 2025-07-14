package certmanager

import (
	"context"
	v1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	cmmeta "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	"github.com/percona/percona-postgresql-operator/internal/naming"
	"github.com/percona/percona-postgresql-operator/pkg/apis/postgres-operator.crunchydata.com/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"regexp"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"time"

	"github.com/cert-manager/cert-manager/pkg/util/cmapichecker"
	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

type Controller interface {
	Check(ctx context.Context, config *rest.Config, ns string) error
	ApplyIssuer(ctx context.Context, cluster *v1beta1.PostgresCluster) error
	ApplyCAIssuer(ctx context.Context, cluster *v1beta1.PostgresCluster) error
	ApplyCACertificate(ctx context.Context, cluster *v1beta1.PostgresCluster) error
}

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

func translateCheckError(err error) error {
	const crdsMapping3Error = `error finding the scope of the object: failed to get restmapping: unable to retrieve the complete list of server APIs: cert-manager.io/v1: no matches for cert-manager.io/v1, Resource=`
	// TODO: remove as soon as TranslateToSimpleError uses this regexp
	regexErrCertManagerCRDsNotFound := regexp.MustCompile(`^(` + regexp.QuoteMeta(crdsMapping3Error) + `)$`)

	if regexErrCertManagerCRDsNotFound.MatchString(err.Error()) {
		return cmapichecker.ErrCertManagerCRDsNotFound
	}

	return cmapichecker.TranslateToSimpleError(err)
}
