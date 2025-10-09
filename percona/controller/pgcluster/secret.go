package pgcluster

import (
	"context"
	"crypto/md5" //nolint:gosec
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v2 "github.com/percona/percona-postgresql-operator/pkg/apis/pgv2.percona.com/v2"
)

func getEnvFromSecrets(ctx context.Context, cl client.Client, cr *v2.PerconaPGCluster, envFromSource []corev1.EnvFromSource) ([]corev1.Secret, error) {
	var secrets []corev1.Secret
	for _, source := range envFromSource {
		var secret corev1.Secret
		if err := cl.Get(ctx, types.NamespacedName{
			Name:      source.SecretRef.Name,
			Namespace: cr.Namespace,
		}, &secret); err != nil {
			return nil, err
		}
		secrets = append(secrets, secret)
	}
	return secrets, nil
}

func getSecretHash(secrets ...corev1.Secret) string {
	var data string

	for _, secret := range secrets {
		data += fmt.Sprintln(secret.Data)
	}

	return fmt.Sprintf("%x", md5.Sum([]byte(data))) //nolint:gosec
}
