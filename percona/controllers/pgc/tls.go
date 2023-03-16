package pgc

import (
	"context"
	"time"

	cm "github.com/jetstack/cert-manager/pkg/apis/certmanager/v1"
	cmmeta "github.com/jetstack/cert-manager/pkg/apis/meta/v1"
	"github.com/percona/percona-postgresql-operator/percona/tls"
	"github.com/pkg/errors"
	v1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"

	crv1 "github.com/percona/percona-postgresql-operator/pkg/apis/crunchydata.com/v1"
)

func (c *Controller) handleTLS(cr *crv1.PerconaPGCluster) error {
	if !cr.TLSEnabled() {
		return nil
	}

	err := c.createSSLByCertManager(cr)
	if err != nil {
		if cr.Spec.TLS != nil && cr.Spec.TLS.IssuerConf != nil {
			return errors.Wrap(err, "create ssl with cert manager")
		}
		err = c.createSSLManualy(cr)
		if err != nil {
			return errors.Wrap(err, "create ssl internally")
		}
	}

	return nil
}

func (c *Controller) createSSLManualy(cluster *crv1.PerconaPGCluster) error {
	ca, cert, key, err := tls.Issue([]string{cluster.Name, cluster.Name + "-pgbouncer"})
	if err != nil {
		return errors.Wrap(err, "issue TLS")
	}
	ctx := context.TODO()
	caSecretName := cluster.Name + "-ssl-ca"
	if len(cluster.Spec.SSLCA) > 0 {
		caSecretName = cluster.Spec.SSLCA
	}
	caSecret := v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: caSecretName,
		},
		Data: map[string][]byte{
			"ca.crt": ca,
		},
	}
	keyPairSecretName := cluster.Name + "-ssl-keypair"
	if len(cluster.Spec.SSLSecretName) > 0 {
		keyPairSecretName = cluster.Spec.SSLSecretName
	}
	keyPairSecret := v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: keyPairSecretName,
		},
		Data: map[string][]byte{
			"tls.crt": cert,
			"tls.key": key,
		},
		Type: v1.SecretTypeTLS,
	}

	_, err = c.Client.CoreV1().Secrets(cluster.Namespace).Create(ctx, &caSecret, metav1.CreateOptions{})
	if err != nil && !kerrors.IsAlreadyExists(err) {
		return errors.Wrapf(err, "create secret %s", caSecret.Name)
	}
	_, err = c.Client.CoreV1().Secrets(cluster.Namespace).Create(ctx, &keyPairSecret, metav1.CreateOptions{})
	if err != nil && !kerrors.IsAlreadyExists(err) {
		return errors.Wrapf(err, "create secret %s", keyPairSecret.Name)
	}

	return nil
}

func (c *Controller) createSSLByCertManager(cr *crv1.PerconaPGCluster) error {
	owner, err := ownerRef(cr, scheme.Scheme)
	if err != nil {
		return err
	}
	ownerReferences := []metav1.OwnerReference{owner}
	issuerName := cr.Name + "-pgo-issuer"
	caIssuerName := cr.Name + "-pgo-ca-issuer"
	issuerKind := "Issuer"
	issuerGroup := ""
	dnsNames := []string{
		cr.Name,
		cr.Name + "-pgbouncer",
		"*." + cr.Name,
		"*." + cr.Name + "-pgbouncer",
	}
	if cr.Spec.TLS != nil && cr.Spec.TLS.IssuerConf != nil {
		issuerKind = cr.Spec.TLS.IssuerConf.Kind
		issuerName = cr.Spec.TLS.IssuerConf.Name
		issuerGroup = cr.Spec.TLS.IssuerConf.Group
	} else {
		if err := c.createIssuer(ownerReferences, cr.Namespace, caIssuerName, ""); err != nil {
			return err
		}
		caSecretName := cr.Name + "-ssl-ca"
		if len(cr.Spec.SSLCA) > 0 {
			caSecretName = cr.Spec.SSLCA
		}
		caCert := &cm.Certificate{
			ObjectMeta: metav1.ObjectMeta{
				Name:            cr.Name + "-ssl-ca",
				Namespace:       cr.Namespace,
				OwnerReferences: ownerReferences,
			},
			Spec: cm.CertificateSpec{
				SecretName: caSecretName,
				CommonName: cr.Name + "-ca",
				DNSNames:   dnsNames,
				IsCA:       true,
				IssuerRef: cmmeta.ObjectReference{
					Name:  caIssuerName,
					Kind:  issuerKind,
					Group: issuerGroup,
				},
				Duration:    &metav1.Duration{Duration: 87600 * time.Hour},
				RenewBefore: &metav1.Duration{Duration: 730 * time.Hour},
			},
		}

		_, err = c.Client.CMClient.Certificates(cr.Namespace).Create(context.TODO(), caCert, metav1.CreateOptions{})

		if err != nil && !kerrors.IsAlreadyExists(err) {
			return errors.Wrap(err, "create ca certificate")
		}

		if err := c.waitForCerts(cr.Namespace, caCert.Spec.SecretName); err != nil {
			return err
		}

		if err := c.createIssuer(ownerReferences, cr.Namespace, issuerName, caCert.Spec.SecretName); err != nil {
			return err
		}
	}
	keyPairSecretName := cr.Name + "-ssl-keypair"
	if len(cr.Spec.SSLSecretName) > 0 {
		keyPairSecretName = cr.Spec.SSLSecretName
	}
	kubeCert := &cm.Certificate{
		ObjectMeta: metav1.ObjectMeta{
			Name:            cr.Name + "-ssl",
			Namespace:       cr.Namespace,
			OwnerReferences: ownerReferences,
		},
		Spec: cm.CertificateSpec{
			SecretName: keyPairSecretName,
			CommonName: cr.Name + "-ssl",
			DNSNames:   dnsNames,
			IsCA:       false,
			IssuerRef: cmmeta.ObjectReference{
				Name:  issuerName,
				Kind:  issuerKind,
				Group: issuerGroup,
			},
		},
	}

	if cr.Spec.TLS != nil && len(cr.Spec.TLS.SANs) > 0 {
		kubeCert.Spec.DNSNames = append(kubeCert.Spec.DNSNames, cr.Spec.TLS.SANs...)
	}

	_, err = c.Client.CMClient.Certificates(cr.Namespace).Create(context.TODO(), kubeCert, metav1.CreateOptions{})
	if err != nil && !kerrors.IsAlreadyExists(err) {
		return errors.Wrap(err, "create certificate")
	}

	return c.waitForCerts(cr.Namespace, cr.Spec.SSLSecretName)
}

func (c *Controller) waitForCerts(namespace string, secretsList ...string) error {
	ticker := time.NewTicker(3 * time.Second)
	timeoutTimer := time.NewTimer(30 * time.Second)
	defer timeoutTimer.Stop()
	defer ticker.Stop()
	for {
		select {
		case <-timeoutTimer.C:
			return errors.Errorf("timeout: can't get tls certificates from certmanager, %s", secretsList)
		case <-ticker.C:
			sucessCount := 0
			for _, secretName := range secretsList {
				_, err := c.Client.CoreV1().Secrets(namespace).Get(context.TODO(), secretName, metav1.GetOptions{})
				if err != nil && !kerrors.IsNotFound(err) {
					return errors.Wrapf(err, "create secret %s", secretName)
				} else if err == nil {
					sucessCount++
				}
			}
			if sucessCount == len(secretsList) {
				return nil
			}
		}
	}
}

func (c *Controller) createIssuer(ownRef []metav1.OwnerReference, namespace, issuer string, caCertSecret string) error {
	spec := cm.IssuerSpec{}
	if caCertSecret == "" {
		spec = cm.IssuerSpec{
			IssuerConfig: cm.IssuerConfig{
				SelfSigned: &cm.SelfSignedIssuer{},
			},
		}
	} else {
		spec = cm.IssuerSpec{
			IssuerConfig: cm.IssuerConfig{
				CA: &cm.CAIssuer{SecretName: caCertSecret},
			},
		}
	}

	issuerObject := cm.Issuer{
		ObjectMeta: metav1.ObjectMeta{
			Name:            issuer,
			Namespace:       namespace,
			OwnerReferences: ownRef,
		},
		Spec: spec,
	}
	_, err := c.Client.CMClient.Issuers(namespace).Create(context.TODO(), &issuerObject, metav1.CreateOptions{})
	if err != nil && !kerrors.IsAlreadyExists(err) {
		return errors.Wrap(err, "create issuer object")
	}

	return nil
}

func ownerRef(ro runtime.Object, scheme *runtime.Scheme) (metav1.OwnerReference, error) {
	gvk, err := apiutil.GVKForObject(ro, scheme)
	if err != nil {
		return metav1.OwnerReference{}, err
	}

	trueVar := true

	ca, err := meta.Accessor(ro)
	if err != nil {
		return metav1.OwnerReference{}, err
	}

	return metav1.OwnerReference{
		APIVersion: gvk.GroupVersion().String(),
		Kind:       gvk.Kind,
		Name:       ca.GetName(),
		UID:        ca.GetUID(),
		Controller: &trueVar,
	}, nil
}
