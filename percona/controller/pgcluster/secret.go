package pgcluster

import (
	"context"
	"crypto/md5"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/percona/percona-postgresql-operator/internal/logging"
	v2 "github.com/percona/percona-postgresql-operator/pkg/apis/pgv2.percona.com/v2"
)

func getEnvFromSecrets(ctx context.Context, cl client.Client, cr *v2.PerconaPGCluster, envFromSource []corev1.EnvFromSource) ([]corev1.Secret, error) {
	log := logging.FromContext(ctx)
	var secrets []corev1.Secret
	for _, source := range envFromSource {
		var secret corev1.Secret
		if err := cl.Get(ctx, types.NamespacedName{
			Name:      source.SecretRef.LocalObjectReference.Name,
			Namespace: cr.Namespace,
		}, &secret); err != nil {
			if k8serrors.IsNotFound(err) {
				log.V(1).Info(fmt.Sprintf("Secret %s not found", secret.Name))
				continue
			}
			return nil, err
		}
		secrets = append(secrets, secret)
	}
	return secrets, nil
}

func getSecretHash(secrets ...corev1.Secret) (string, error) {
	var data string

	for _, secret := range secrets {
		data += fmt.Sprintln(secret.Data)
	}

	hash := fmt.Sprintf("%x", md5.Sum([]byte(data)))

	return hash, nil
}
