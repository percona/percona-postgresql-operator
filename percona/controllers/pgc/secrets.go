package pgc

import (
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"time"

	"github.com/percona/percona-postgresql-operator/internal/util"
	"github.com/percona/percona-postgresql-operator/percona/controllers/pgcluster"
	crv1 "github.com/percona/percona-postgresql-operator/pkg/apis/crunchydata.com/v1"
	"github.com/pkg/errors"
	v1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type UserSecretData struct {
	Password string `json:"password"`
	Roles    string `json:"roles"`
	Name     string `json:"username"`
	UsersTXT string `json:"users.txt"`
}
type SecretData struct {
	Name string
	Data UserSecretData
}

func (c *Controller) CreateNewInternalSecrets(clusterName, secretName, clusterUser, namespace string) error {
	ctx := context.TODO()
	if len(secretName) == 0 {
		secretName = clusterName + "-users"
	}
	secretsData := []SecretData{}
	usersSecret, err := c.Client.CoreV1().Secrets(namespace).Get(ctx, secretName, metav1.GetOptions{})
	if err != nil && !kerrors.IsNotFound(err) {
		return errors.Wrapf(err, "get secret %s", secretName)
	} else if kerrors.IsNotFound(err) {
		generatedSecretsData, err := c.GenerateUsersInternalSecretsData(clusterName, clusterUser)
		if err != nil {
			return errors.Wrap(err, "generate users internal data")
		}
		err = c.createUsersSecret(generatedSecretsData, secretName, namespace)
		if err != nil {
			return errors.Wrap(err, "create users secret")
		}
		secretsData = generatedSecretsData
	} else {
		generatedSecretsData, err := c.generateUsersInternalSecretsDataFromSecret(usersSecret, clusterName)
		if err != nil {
			return errors.Wrap(err, "generate users internal data from secret")
		}
		secretsData = generatedSecretsData
	}

	err = c.createUsersInternalSecrets(secretsData, namespace, clusterName)
	if err != nil {
		return errors.Wrap(err, "create users internal secrets")
	}

	return nil
}

func (c *Controller) createUsersSecret(secretsData []SecretData, secretName, namespace string) error {
	ctx := context.TODO()
	data := make(map[string][]byte)
	for _, secret := range secretsData {
		data[secret.Data.Name] = []byte(secret.Data.Password)
	}
	secretData, err := json.Marshal(data)
	if err != nil {
		return errors.Wrap(err, "marshal users secret data")
	}
	secretDataHash := sha256Hash(secretData)
	s := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: namespace,
			Annotations: map[string]string{
				"last-applied-secret": secretDataHash,
			},
		},
		Data: data,
	}
	_, err = c.Client.CoreV1().Secrets(namespace).Create(ctx, s, metav1.CreateOptions{})
	if err != nil && !kerrors.IsAlreadyExists(err) {
		return errors.Wrapf(err, "create secret %s", secretName)
	}

	return nil
}

func (c *Controller) generateUsersInternalSecretsDataFromSecret(usersSecret *v1.Secret, clusterName string) ([]SecretData, error) {
	secrets := []SecretData{}
	for userName, password := range usersSecret.Data {
		secretData, err := generateUserSecretData(userName, clusterName, string(password))
		if err != nil {
			return nil, errors.Wrapf(err, "get secret data for %s", userName)
		}
		secrets = append(secrets, secretData)
	}

	return secrets, nil
}

func (c *Controller) GenerateUsersInternalSecretsData(clusterName, clusterUser string) ([]SecretData, error) {
	bouncerUserData, err := generateUserSecretData("pgbouncer", clusterName, "")
	if err != nil {
		return nil, errors.Wrap(err, "generate pgbouncer")
	}
	postgresUserData, err := generateUserSecretData("postgres", clusterName, "")
	if err != nil {
		return nil, errors.Wrap(err, "generate postgres")
	}
	primaryUserData, err := generateUserSecretData("primaryuser", clusterName, "")
	if err != nil {
		return nil, errors.Wrap(err, "generate primaryuser")
	}
	clusterCustomUser, err := generateUserSecretData(clusterUser, clusterName, "")
	if err != nil {
		return nil, errors.Wrap(err, "generate cluster users")
	}
	secrets := []SecretData{
		bouncerUserData,
		postgresUserData,
		primaryUserData,
		clusterCustomUser,
	}

	return secrets, nil
}

func generateUserSecretData(userName, clusterName, password string) (SecretData, error) {
	var secretName, roles, usersTXT string
	if len(password) == 0 {
		generatedPassword, err := util.GeneratePassword(util.DefaultGeneratedPasswordLength)
		if err != nil {
			return SecretData{}, errors.Wrap(err, "generate password")
		}
		password = generatedPassword
	}

	switch userName {
	case "postgres":
		secretName = clusterName + "-postgres-secret"
	case "primaryuser":
		secretName = clusterName + "-primaryuser-secret"
	case "pgbouncer":
		secretName = clusterName + "-pgbouncer-secret"
		roles = ""
		usersTXT = `"pgbouncer" "md5` + fmt.Sprintf("%x", md5.Sum([]byte(password+userName))) + `"`
	default:
		secretName = clusterName + "-" + userName + "-secret"
	}

	return SecretData{
		Name: secretName,
		Data: UserSecretData{
			Name:     userName,
			Password: password,
			Roles:    roles,
			UsersTXT: usersTXT,
		},
	}, nil
}

func (c *Controller) createUsersInternalSecrets(secrets []SecretData, namespace, clusterName string) error {
	ctx := context.TODO()
	for _, secret := range secrets {
		data := make(map[string]string)
		labels := map[string]string{
			"pg-cluster": clusterName,
		}
		if len(secret.Data.Password) > 0 {
			data["password"] = secret.Data.Password
		}
		if len(secret.Data.Name) > 0 {
			name := secret.Data.Name
			if secret.Data.Name == "pgbouncer" {
				name = ""
				labels["crunchy-pgbouncer"] = "true"
			}
			data["username"] = name
		}
		if len(secret.Data.Roles) > 0 {
			data["roles"] = secret.Data.Roles
		}
		if len(secret.Data.UsersTXT) > 0 {
			data["users.txt"] = secret.Data.UsersTXT
		}

		s := &v1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      secret.Name,
				Namespace: namespace,
				Labels:    labels,
			},
			StringData: data,
		}
		_, err := c.Client.CoreV1().Secrets(namespace).Create(ctx, s, metav1.CreateOptions{})
		if err != nil && !kerrors.IsAlreadyExists(err) {
			return errors.Wrapf(err, "create secret %s", secret.Name)
		}
	}

	return nil
}

func (c *Controller) updateUsersInternalSecrets(secrets []SecretData, namespace, clusterName string) error {
	ctx := context.TODO()
	pgCluster, err := c.Client.CrunchydataV1().Pgclusters(namespace).Get(ctx, clusterName, metav1.GetOptions{})
	if err != nil {
		return errors.Wrapf(err, "get pgcluster resource")
	}
	for _, secret := range secrets {
		s, err := c.Client.CoreV1().Secrets(namespace).Get(ctx, secret.Name, metav1.GetOptions{})
		if err != nil && !kerrors.IsNotFound(err) {
			return errors.Wrapf(err, "get secret %s", secret.Name)
		} else if kerrors.IsNotFound(err) {
			continue
		}
		if pass, ok := s.Data["password"]; ok {
			if string(pass) == secret.Data.Password {
				continue
			}
		}
		err = updateUserPassword(c.Client, secret.Data.Name, secret.Data.Password, pgCluster)
		if err != nil {
			return errors.Wrapf(err, "update user password")
		}

		s, err = c.Client.CoreV1().Secrets(namespace).Get(ctx, secret.Name, metav1.GetOptions{})
		if err != nil {
			return errors.Wrapf(err, "get secret %s", secret.Name)
		}
		s.Data = map[string][]byte{
			"password":  []byte(secret.Data.Password),
			"roles":     []byte(secret.Data.Roles),
			"username":  []byte(secret.Data.Name),
			"users.txt": []byte(secret.Data.UsersTXT),
		}
		if secret.Data.Name == "pgbouncer" {
			perconaPGCluster, err := c.Client.CrunchydataV1().PerconaPGClusters(namespace).Get(ctx, clusterName, metav1.GetOptions{})
			if err != nil {
				return errors.Wrapf(err, "get perconapgcluster resource")
			}
			err = pgcluster.ChangeBouncerSize(c.Client, perconaPGCluster, 0)
			if err != nil {
				return errors.Wrap(err, "change bouncer size to 0")
			}
			err = c.waitBouncerTermination(perconaPGCluster)
			if err != nil {
				return errors.Wrap(err, "wait pgBouncer termination")
			}

			s.ResourceVersion = ""
			_, err = c.Client.CoreV1().Secrets(namespace).Create(ctx, s, metav1.CreateOptions{})
			if err != nil {
				return errors.Wrapf(err, "update secret %s", secret.Name)
			}
			err = pgcluster.ChangeBouncerSize(c.Client, perconaPGCluster, perconaPGCluster.Spec.PGBouncer.Size)
			if err != nil {
				return errors.Wrap(err, "change bouncer size")
			}
			continue
		}
		_, err = c.Client.CoreV1().Secrets(namespace).Update(ctx, s, metav1.UpdateOptions{})
		if err != nil {
			return errors.Wrapf(err, "update secret %s", secret.Name)
		}
	}

	return nil
}

func (c *Controller) waitBouncerTermination(perconaPGCluster *crv1.PerconaPGCluster) error {
	ctx := context.TODO()
	for i := 0; i <= 30; i++ {
		time.Sleep(5 * time.Second)
		bouncerTerminated := false
		_, err := c.Client.AppsV1().Deployments(perconaPGCluster.Namespace).Get(ctx,
			perconaPGCluster.Name+"-pgbouncer", metav1.GetOptions{})
		if err != nil && kerrors.IsNotFound(err) {
			bouncerTerminated = true

		}
		primaryDepl, err := c.Client.AppsV1().Deployments(perconaPGCluster.Namespace).Get(ctx,
			perconaPGCluster.Name, metav1.GetOptions{})
		if err != nil && !kerrors.IsNotFound(err) {
			return errors.Wrap(err, "get pgprimary deployment")
		}
		if primaryDepl.Status.Replicas == primaryDepl.Status.AvailableReplicas && bouncerTerminated {
			return nil
		}
	}
	return errors.New(perconaPGCluster.Name + "-pgbouncer didn't stop properly")
}

func sha256Hash(data []byte) string {
	return fmt.Sprintf("%x", sha256.Sum256(data))
}
