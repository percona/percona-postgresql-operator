package db

import (
	"errors"
	"strings"

	log "github.com/sirupsen/logrus"
	v1 "k8s.io/api/core/v1"

	"github.com/percona/percona-postgresql-operator/internal/kubeapi"
)

func ExecuteSQL(clientset kubeapi.Interface, pod *v1.Pod, port, sql string, extraCommandArgs []string) (string, error) {
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
