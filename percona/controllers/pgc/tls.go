package pgc

import (
	"context"

	"github.com/percona/percona-postgresql-operator/percona/tls"
	crv1 "github.com/percona/percona-postgresql-operator/pkg/apis/crunchydata.com/v1"
	"github.com/pkg/errors"
	v1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (c *Controller) handleTLS(cluster *crv1.PerconaPGCluster) error {
	if !cluster.Spec.TlSOnly {
		return nil
	}
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
