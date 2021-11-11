package pgc

import (
	"context"
	"fmt"
	"strings"

	"github.com/percona/percona-postgresql-operator/internal/apiserver"
	"github.com/percona/percona-postgresql-operator/internal/kubeapi"
	"github.com/percona/percona-postgresql-operator/internal/pgadmin"
	pgpassword "github.com/percona/percona-postgresql-operator/internal/postgres/password"
	"github.com/percona/percona-postgresql-operator/internal/util"
	crv1 "github.com/percona/percona-postgresql-operator/pkg/apis/crunchydata.com/v1"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
)

const (
	sqlAlterUser      = `ALTER USER %s`
	sqlPasswordClause = `WITH PASSWORD %s`
)

func (c *Controller) UpdateUsers(usersSecret *v1.Secret, clusterName, namespace string) error {
	usersSecretsData, err := c.generateUsersInternalSecretsDataFromSecret(usersSecret, clusterName)
	if err != nil {
		return errors.Wrap(err, "generate users internal secrets data")
	}
	err = c.updateUsersInternalSecrets(usersSecretsData, namespace, clusterName)
	if err != nil {
		return errors.Wrap(err, "update users internal secrets")
	}
	return nil
}

func updateUserPassword(clientset kubeapi.Interface, username, password string, cluster *crv1.Pgcluster) error {
	pod, err := util.GetPrimaryPod(clientset, cluster)
	if err != nil {
		return errors.Wrap(err, "get primary pod")
	}

	sql := fmt.Sprintf(sqlAlterUser, util.SQLQuoteIdentifier(username))

	pwType := cluster.Spec.PasswordType
	passwordType, err := apiserver.GetPasswordType(pwType)
	if err != nil {
		return errors.Wrap(err, "get password type")
	}

	hashedPassword, err := getHashedPassword(username, password, passwordType)
	if err != nil {
		return errors.Wrap(err, "generate password")
	}

	sql = fmt.Sprintf("%s %s", sql, fmt.Sprintf(sqlPasswordClause, util.SQLQuoteLiteral(hashedPassword)))
	client, err := kubeapi.NewClient()
	if err != nil {
		return errors.Wrap(err, "new client")
	}

	if err := updatePgAdmin(clientset, client.Config, cluster, username, password); err != nil {
		return errors.Wrap(err, "update pgAdmin")
	}
	if _, err := executeSQL(clientset, pod, cluster.Spec.Port, sql, []string{}); err != nil {
		return errors.Wrap(err, "execute sql")
	}

	return nil
}

func executeSQL(clientset kubeapi.Interface, pod *v1.Pod, port, sql string, extraCommandArgs []string) (string, error) {
	command := []string{"psql", "-A", "-t"}

	// add the port
	command = append(command, "-p", port)

	// add any extra arguments
	command = append(command, extraCommandArgs...)

	// execute into the primary pod to run the query
	client, err := kubeapi.NewClient()
	if err != nil {
		log.Errorf("new client: %s", err.Error())
	}

	RESTConfig := client.Config
	stdout, stderr, err := kubeapi.ExecToPodThroughAPI(RESTConfig,
		clientset, command,
		"database", pod.Name, pod.ObjectMeta.Namespace, strings.NewReader(sql))

	// if there is an error executing the command, which includes the stderr,
	// return the error
	if err != nil {
		return "", err
	} else if stderr != "" {
		return "", errors.New(stderr)
	}

	return stdout, nil
}

func getHashedPassword(username, password string, passwordType pgpassword.PasswordType) (string, error) {
	postgresPassword, err := pgpassword.NewPostgresPassword(passwordType, username, password)
	if err != nil {
		return "", errors.Wrap(err, "new postgres password")
	}

	hashedPassword, err := postgresPassword.Build()
	if err != nil {
		return "", err
	}
	return hashedPassword, nil
}

func updatePgAdmin(clientset kubeapi.Interface, RESTConfig *rest.Config, cluster *crv1.Pgcluster, username, password string) error {
	ctx := context.TODO()

	qr, err := pgadmin.GetPgAdminQueryRunner(clientset, RESTConfig, cluster)
	if err != nil {
		return errors.Wrap(err, "get pgAdmin query runner")
	}

	// if there is no pgAdmin associated this cluster, return no error
	if qr == nil {
		return nil
	}

	// Get service details and prep connection metadata
	service, err := clientset.CoreV1().Services(cluster.Namespace).Get(ctx, cluster.Name, metav1.GetOptions{})
	if err != nil {
		return errors.Wrap(err, "get cluster service")
	}

	// set up the server entry data
	dbService := pgadmin.ServerEntryFromPgService(service, cluster.Name)
	dbService.Password = password

	// attempt to set the username/password for this user in the pgadmin
	// deployment
	if err := pgadmin.SetLoginPassword(qr, username, password); err != nil {
		return errors.Wrap(err, "pgAdmin set  login password")
	}

	// if the service name for the database is present, also set the cluster
	// if it's not set, early exit
	if dbService.Name == "" {
		return nil
	}

	if err := pgadmin.SetClusterConnection(qr, username, dbService); err != nil {
		return errors.Wrap(err, "pgAdmin set cluster connection")
	}

	return nil
}
