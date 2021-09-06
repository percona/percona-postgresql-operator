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
	log "github.com/sirupsen/logrus"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
)

const (
	sqlAlterRole      = `ALTER ROLE %s`
	sqlPasswordClause = `PASSWORD %s`
)

func updateUser(clientset kubeapi.Interface, username, password string, cluster *crv1.Pgcluster) {
	ctx := context.TODO()

	// first, find the primary Pod
	pod, err := util.GetPrimaryPod(clientset, cluster)
	// if the primary Pod cannot be found, we're going to continue on for the
	// other clusters, but provide some sort of error message in the response
	if err != nil {
		log.Error(err)
	}

	// alright, so we can start building up some SQL now, as the other commands
	// here can all occur within ALTER ROLE!
	//
	// We first build it up with the username, being careful to escape the
	// identifier to avoid SQL injections :)
	sql := fmt.Sprintf(sqlAlterRole, util.SQLQuoteIdentifier(username))

	// Though we do have an awesome function for setting a PostgreSQL password
	// (SetPostgreSQLPassword) the problem is we are going to be adding too much
	// to the string here, and we don't always know if the password is being
	// updated, which is one of the requirements of the function. So we will
	// perform any query execution here in this module

	// Speaking of passwords...let's first determine if the user updated their
	// password. See generatePassword for how precedence is given for password
	// updates

	//	pwType := request.PasswordType
	//	if pwType == "" {
	pwType := cluster.Spec.PasswordType
	rotatePassword := true
	passwordLength := 10
	//}

	passwordType, _ := apiserver.GetPasswordType(pwType)
	isChanged, password, hashedPassword, err := generatePassword(username,
		password, passwordType, rotatePassword, passwordLength)
	// in the off-chance there is an error generating the password, record it
	// and return
	if err != nil {
		log.Error(err)

		return
	}
	resultPassword := ""
	if isChanged {
		resultPassword = password
		sql = fmt.Sprintf("%s %s", sql,
			fmt.Sprintf(sqlPasswordClause, util.SQLQuoteLiteral(hashedPassword)))
		client, err := kubeapi.NewClient()
		if err != nil {
			log.Errorf("new client: %s", err.Error())
		}

		RESTConfig := client.Config
		// Sync user to pgAdmin, if enabled
		if err := updatePgAdmin(clientset, RESTConfig, cluster, username, resultPassword); err != nil {
			log.Error(err)

			return
		}
	}

	// now, check to see if the request wants to expire the user's password
	// this will leverage the PostgreSQL ability to set a date as "-infinity"
	// so that the password is 100% expired
	//
	// Expiring the user also takes precedence over trying to move the update
	// password timeline, which we check for next
	//
	// Next we check to ensure the user wants to explicitly un-expire a
	// password, and/or ensure that the expiration time is unlimited. This takes
	// precednece over setting an explicitly expiration period, which we check
	// for last

	// Now, determine if we want to enable or disable the login. Enable takes
	// precedence over disable
	// None of these have SQL injectionsas they are fixed constants

	/*
		switch request.LoginState {
		case msgs.UpdateUserLoginEnable:
			sql = fmt.Sprintf("%s %s", sql, sqlEnableLoginClause)
		case msgs.UpdateUserLoginDisable:
			sql = fmt.Sprintf("%s %s", sql, sqlDisableLoginClause)
		case msgs.UpdateUserLoginDoNothing: // this is never reached -- no-op
		}
	*/
	// execute the SQL! if there is an error, return the results
	if _, err := executeSQL(clientset, pod, cluster.Spec.Port, sql, []string{}); err != nil {
		log.Error(err)

		// even though we return in the next line, having an explicit return here
		// in case we add any additional logic beyond this point
		return
	}

	// If the password did change, it is not updated in the database. If the user
	// has a "managed" account (i.e. there is a secret for this user account"),
	// we can now updated the value of that password in the secret
	if isChanged {
		secretName := crv1.UserSecretName(cluster, username)

		// only call update user secret if the secret exists
		if _, err := clientset.CoreV1().Secrets(cluster.Namespace).Get(ctx, secretName, metav1.GetOptions{}); err == nil {
			// if we cannot update the user secret, only warn that we cannot do so
			if err := util.UpdateUserSecret(clientset, cluster, username, resultPassword); err != nil {
				log.Warn(err)
			}
		}
	}

	return
}

// sqlCommand is the command that needs to be executed for running SQL
var sqlCommand = []string{"psql", "-A", "-t"}

func executeSQL(clientset kubeapi.Interface, pod *v1.Pod, port, sql string, extraCommandArgs []string) (string, error) {
	command := sqlCommand

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
		return "", fmt.Errorf(stderr)
	}

	return stdout, nil
}

func generatePassword(username, password string, passwordType pgpassword.PasswordType,
	generatePassword bool, generatedPasswordLength int) (bool, string, string, error) {
	// first, an early exit: nothing is updated
	if password == "" && !generatePassword {
		return false, "", "", nil
	}

	// give precedence to the user customized password
	if password == "" && generatePassword {
		// Determine if the user passed in a password length, otherwise us the
		// default
		passwordLength := generatedPasswordLength

		if passwordLength == 0 {
			passwordLength = util.GeneratedPasswordLength(apiserver.Pgo.Cluster.PasswordLength)
		}

		// generate the password
		generatedPassword, err := util.GeneratePassword(passwordLength)
		// if there is an error, return
		if err != nil {
			return false, "", "", err
		}

		password = generatedPassword
	}

	// finally, hash the password
	postgresPassword, err := pgpassword.NewPostgresPassword(passwordType, username, password)
	if err != nil {
		return false, "", "", err
	}

	hashedPassword, err := postgresPassword.Build()
	if err != nil {
		return false, "", "", err
	}

	// return!
	return true, password, hashedPassword, nil
}

func updatePgAdmin(clientset kubeapi.Interface, RESTConfig *rest.Config, cluster *crv1.Pgcluster, username, password string) error {
	ctx := context.TODO()

	// Sync user to pgAdmin, if enabled
	qr, err := pgadmin.GetPgAdminQueryRunner(clientset, RESTConfig, cluster)
	// if there is an error, return as such
	if err != nil {
		return err
	}

	// likewise, if there is no pgAdmin associated this cluster, return no error
	if qr == nil {
		return nil
	}

	// proceed onward
	// Get service details and prep connection metadata
	service, err := clientset.CoreV1().Services(cluster.Namespace).Get(ctx, cluster.Name, metav1.GetOptions{})
	if err != nil {
		return err
	}

	// set up the server entry data
	dbService := pgadmin.ServerEntryFromPgService(service, cluster.Name)
	dbService.Password = password

	// attempt to set the username/password for this user in the pgadmin
	// deployment
	if err := pgadmin.SetLoginPassword(qr, username, password); err != nil {
		return err
	}

	// if the service name for the database is present, also set the cluster
	// if it's not set, early exit
	if dbService.Name == "" {
		return nil
	}

	if err := pgadmin.SetClusterConnection(qr, username, dbService); err != nil {
		return err
	}

	return nil
}
