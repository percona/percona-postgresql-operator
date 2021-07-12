package pmm

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/percona/percona-postgresql-operator/internal/kubeapi"
	crv1 "github.com/percona/percona-postgresql-operator/pkg/apis/crunchydata.com/v1"

	"github.com/pkg/errors"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

const (
	pmmContainerName = "pmm-client"
)

func UpdatePMMSidecar(clientset kubeapi.Interface, cluster *crv1.Pgcluster, deployment *appsv1.Deployment) error {
	ctx := context.TODO()
	cl, err := clientset.CrunchydataV1().PerconaPGClusters(cluster.Namespace).Get(ctx, cluster.Name, metav1.GetOptions{})
	if err != nil {
		return errors.Wrap(err, "get perconapgcluster resource: %s")
	}

	return AddOrRemovePMMSidecar(cl, cluster.Name, deployment)
}

func AddOrRemovePMMSidecar(cl *crv1.PerconaPGCluster, clusterName string, deployment *appsv1.Deployment) error {
	removePMMSidecar(deployment)
	if !cl.Spec.PMM.Enabled {
		return nil
	}
	cl.Name = deployment.Name
	container := GetPMMContainer(cl, clusterName)
	deployment.Spec.Template.Spec.Containers = append(deployment.Spec.Template.Spec.Containers, container)

	return nil
}

func removePMMSidecar(deployment *appsv1.Deployment) {
	// first, find the container entry in the list of containers and remove it
	containers := []v1.Container{}
	for _, c := range deployment.Spec.Template.Spec.Containers {
		// skip if this is the PMM container
		if c.Name == pmmContainerName {
			continue
		}
		containers = append(containers, c)
	}

	deployment.Spec.Template.Spec.Containers = containers
}

func HandlePMMTemplate(template []byte, cluster *crv1.PerconaPGCluster) ([]byte, error) {
	if !cluster.Spec.PMM.Enabled {
		return bytes.Replace(template, []byte("<pmmContainer>"), []byte(""), -1), nil
	}
	pmmContainerBytes, err := GetPMMContainerJSON(cluster)
	if err != nil {
		return nil, errors.Wrap(err, "get pmm container json: %s")
	}

	return bytes.Replace(template, []byte("<pmmContainer>"), append([]byte(", "), pmmContainerBytes...), -1), nil
}

func GetPMMContainerJSON(pgc *crv1.PerconaPGCluster) ([]byte, error) {
	c := GetPMMContainer(pgc, pgc.Name)
	b, err := json.Marshal(c)
	if err != nil {
		return nil, errors.Wrap(err, "marshal container")
	}

	return b, nil
}

func GetPMMContainer(pgc *crv1.PerconaPGCluster, clusterName string) v1.Container {
	return v1.Container{
		Name:  "pmm-client",
		Image: pgc.Spec.PMM.Image,
		LivenessProbe: &v1.Probe{
			Handler: v1.Handler{
				HTTPGet: &v1.HTTPGetAction{
					Path:   "/local/Status",
					Port:   intstr.FromInt(7777),
					Scheme: v1.URISchemeHTTP,
				},
			},
			FailureThreshold:    3,
			InitialDelaySeconds: 60,
			PeriodSeconds:       10,
			SuccessThreshold:    1,
			TimeoutSeconds:      5,
		},
		Lifecycle: &v1.Lifecycle{
			PreStop: &v1.Handler{
				Exec: &v1.ExecAction{
					Command: []string{
						"bash",
						"-c",
						"pmm-admin inventory remove node --force $(pmm-admin status --json | python -c \"import sys, json; print(json.load(sys.stdin)['pmm_agent_status']['node_id'])\")",
					},
				},
			},
		},
		Ports: []v1.ContainerPort{
			{
				ContainerPort: 7777,
				Protocol:      v1.ProtocolTCP,
			},
			{
				ContainerPort: 30100,
			},
			{
				ContainerPort: 30101,
			},
			{
				ContainerPort: 30102,
			},
			{
				ContainerPort: 30103,
			},
			{
				ContainerPort: 30104,
			},
			{
				ContainerPort: 30105,
			},
		},
		Resources: v1.ResourceRequirements{
			Limits:   pgc.Spec.PMM.Resources.Limits,
			Requests: pgc.Spec.PMM.Resources.Requests,
		},
		Env: []v1.EnvVar{
			{
				Name:  "PMM_USER",
				Value: pgc.Spec.PMM.ServerUser,
			},
			{
				Name:  "PMM_SERVER",
				Value: pgc.Spec.PMM.ServerHost,
			},
			{
				Name:  "CLIENT_PORT_LISTEN",
				Value: "7777",
			},
			{
				Name:  "CLIENT_PORT_MIN",
				Value: "30100",
			},
			{
				Name:  "CLIENT_PORT_MAX",
				Value: "30105",
			},
			{
				Name: "POD_NAME",
				ValueFrom: &v1.EnvVarSource{
					FieldRef: &v1.ObjectFieldSelector{
						APIVersion: "v1",
						FieldPath:  "metadata.name",
					},
				},
			},
			{
				Name: "POD_NAMESPASE",
				ValueFrom: &v1.EnvVarSource{
					FieldRef: &v1.ObjectFieldSelector{
						APIVersion: "v1",
						FieldPath:  "metadata.namespace",
					},
				},
			},
			{
				Name:  "PMM_AGENT_SERVER_ADDRESS",
				Value: pgc.Spec.PMM.ServerHost,
			},
			{
				Name:  "PMM_AGENT_SERVER_USERNAME",
				Value: pgc.Spec.PMM.ServerUser,
			},
			{
				Name: "PMM_AGENT_SERVER_PASSWORD",
				ValueFrom: &v1.EnvVarSource{
					SecretKeyRef: &v1.SecretKeySelector{
						LocalObjectReference: v1.LocalObjectReference{
							Name: pgc.Spec.PMM.PMMSecret,
						},
						Key: "password",
					},
				},
			},
			{
				Name:  "PMM_AGENT_LISTEN_PORT",
				Value: "7777",
			},
			{
				Name:  "PMM_AGENT_PORTS_MIN",
				Value: "30100",
			},
			{
				Name:  "PMM_AGENT_PORTS_MAX",
				Value: "30105",
			},
			{
				Name:  "PMM_AGENT_CONFIG_FILE",
				Value: "/usr/local/percona/pmm2/config/pmm-agent.yaml",
			},
			{
				Name:  "PMM_AGENT_SERVER_INSECURE_TLS",
				Value: "1",
			},
			{
				Name:  "PMM_AGENT_LISTEN_ADDRESS",
				Value: "0.0.0.0",
			},
			{
				Name:  "PMM_AGENT_SETUP_NODE_NAME",
				Value: pgc.Name,
			},
			{
				Name:  "PMM_AGENT_SETUP_METRICS_MODE",
				Value: "push",
			},
			{
				Name:  "PMM_AGENT_SETUP",
				Value: "1",
			},
			{
				Name:  "PMM_AGENT_SETUP_FORCE",
				Value: "1",
			},
			{
				Name:  "PMM_AGENT_SETUP_NODE_TYPE",
				Value: "container",
			},
			{
				Name:  "DB_TYPE",
				Value: "postgresql",
			},
			{
				Name: "DB_PASS",
				ValueFrom: &v1.EnvVarSource{
					SecretKeyRef: &v1.SecretKeySelector{
						LocalObjectReference: v1.LocalObjectReference{
							Name: clusterName + "-postgres-secret",
						},
						Key: "password",
					},
				},
			},
			{
				Name:  "PMM_AGENT_PRERUN_SCRIPT",
				Value: "pmm-admin status --wait=10s; pmm-admin add postgresql --tls-skip-verify --skip-connection-check --metrics-mode=push --username=postgres --password=$(DB_PASS) --service-name=$(PMM_AGENT_SETUP_NODE_NAME) --host=$(POD_NAME) --port=5432 --query-source=pgstatmonitor; pmm-admin annotate --service-name=$(PMM_AGENT_SETUP_NODE_NAME) 'Service restarted'",
			},
		},
	}
}
