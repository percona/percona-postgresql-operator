package pmm

import (
	"context"
	"fmt"
	"strings"

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

func UpdatePMMSidecar(clientset kubeapi.Interface, cluster *crv1.Pgcluster, deployment *appsv1.Deployment, nodeName string) error {
	ctx := context.TODO()
	cl, err := clientset.CrunchydataV1().PerconaPGClusters(cluster.Namespace).Get(ctx, cluster.Name, metav1.GetOptions{})
	if err != nil {
		return errors.Wrap(err, "get perconapgcluster resource: %s")
	}

	AddOrRemovePMMContainer(cl, cluster.Name, nodeName, deployment)
	return nil
}

func AddOrRemovePMMContainer(cl *crv1.PerconaPGCluster, clusterName, nodeName string, deployment *appsv1.Deployment) {
	containers := []v1.Container{}
	added := false
	for _, c := range deployment.Spec.Template.Spec.Containers {
		if c.Name == pmmContainerName {
			if !cl.Spec.PMM.Enabled {
				continue
			}
			containers = append(containers, GetPMMContainer(cl, clusterName, nodeName))
			added = true
			continue
		}
		containers = append(containers, c)
	}
	if !added && cl.Spec.PMM.Enabled {
		containers = append(containers, GetPMMContainer(cl, clusterName, nodeName))
	}
	deployment.Spec.Template.Spec.Containers = containers
}

func GetPMMContainer(pgc *crv1.PerconaPGCluster, clusterName, nodeName string) v1.Container {
	pmm_container := v1.Container{
		Name:            pmmContainerName,
		Image:           pgc.Spec.PMM.Image,
		ImagePullPolicy: v1.PullPolicy(pgc.Spec.PMM.ImagePullPolicy),
		LivenessProbe: &v1.Probe{
			ProbeHandler: v1.ProbeHandler{
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
			PreStop: &v1.LifecycleHandler{
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
				Value: nodeName,
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
				Name:  "PMM_AGENT_SIDECAR",
				Value: "true",
			},
			{
				Name:  "PMM_AGENT_SIDECAR_SLEEP",
				Value: "5",
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
			getPMMAgentPrerunScript(pgc),
		},
	}
	if pgc.TLSEnabled() {
		pmm_container.VolumeMounts = []v1.VolumeMount{
			{
				MountPath: "/pgconf/tls",
				Name:      "tls-server",
			},
		}
	}
	return pmm_container
}

func getPMMAgentPrerunScript(pgc *crv1.PerconaPGCluster) v1.EnvVar {
	addServiceArgs := []string{
		"--skip-connection-check",
		"--metrics-mode=push",
		"--username=postgres",
		"--password=$(DB_PASS)",
		"--service-name=$(PMM_AGENT_SETUP_NODE_NAME)",
		"--host=$(POD_NAME)",
		"--port=5432",
		"--query-source=pgstatmonitor",
	}

	if pgc.TLSEnabled() {
		addServiceArgs = append(addServiceArgs, []string{
			"--tls",
			"--tls-skip-verify",
			"--tls-cert-file=/pgconf/tls/tls.crt",
			"--tls-key-file=/pgconf/tls/tls.key",
			"--tls-ca-file=/pgconf/tls/ca.crt",
		}...)
	}

	pmmWait := "pmm-admin status --wait=10s;"
	pmmAddService := fmt.Sprintf("pmm-admin add postgresql %s;", strings.Join(addServiceArgs, " "))
	pmmAnnotate := "pmm-admin annotate --service-name=$(PMM_AGENT_SETUP_NODE_NAME) 'Service restarted'"
	prerunScript := pmmWait + "\n" + pmmAddService + "\n" + pmmAnnotate

	return v1.EnvVar{
		Name:  "PMM_AGENT_PRERUN_SCRIPT",
		Value: prerunScript,
	}
}
