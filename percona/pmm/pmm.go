package pmm

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	"github.com/percona/percona-postgresql-operator/pkg/apis/pg.percona.com/v2beta1"
)

func SidecarContainer(pgc *v2beta1.PerconaPGCluster) corev1.Container {
	ports := []corev1.ContainerPort{{ContainerPort: 7777}}
	for port := 30100; port <= 30105; port++ {
		ports = append(ports, corev1.ContainerPort{ContainerPort: int32(port)})
	}

	pmmSpec := pgc.Spec.PMM

	return corev1.Container{
		Name:            "pmm-client",
		Image:           pmmSpec.Image,
		ImagePullPolicy: pmmSpec.ImagePullPolicy,
		SecurityContext: pmmSpec.ContainerSecurityContext,
		Ports:           ports,
		Resources:       pmmSpec.Resources,
		LivenessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Path:   "/local/Status",
					Port:   intstr.FromInt(7777),
					Scheme: corev1.URISchemeHTTP,
				},
			},
			FailureThreshold:    3,
			InitialDelaySeconds: 60,
			PeriodSeconds:       10,
			SuccessThreshold:    1,
			TimeoutSeconds:      5,
		},
		Lifecycle: &corev1.Lifecycle{
			PreStop: &corev1.LifecycleHandler{
				Exec: &corev1.ExecAction{
					Command: []string{
						"bash",
						"-c",
						"pmm-admin inventory remove node --force $(pmm-admin status --json | python -c \"import sys, json; print(json.load(sys.stdin)['pmm_agent_status']['node_id'])\")",
					},
				},
			},
		},
		Env: []corev1.EnvVar{
			{
				Name: "POD_NAME",
				ValueFrom: &corev1.EnvVarSource{
					FieldRef: &corev1.ObjectFieldSelector{
						APIVersion: "v1",
						FieldPath:  "metadata.name",
					},
				},
			},
			{
				Name: "POD_NAMESPACE",
				ValueFrom: &corev1.EnvVarSource{
					FieldRef: &corev1.ObjectFieldSelector{
						APIVersion: "v1",
						FieldPath:  "metadata.namespace",
					},
				},
			},
			{
				Name:  "PMM_USER",
				Value: "api_key",
			},
			{
				Name:  "PMM_SERVER",
				Value: pmmSpec.ServerHost,
			},
			{
				Name:  "PMM_AGENT_SERVER_ADDRESS",
				Value: pmmSpec.ServerHost,
			},
			{
				Name:  "PMM_AGENT_SERVER_USERNAME",
				Value: "api_key",
			},
			{
				Name: "PMM_AGENT_SERVER_PASSWORD",
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: pmmSpec.Secret,
						},
						Key: "PMM_SERVER_KEY",
					},
				},
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
				Name: "PMM_AGENT_SETUP_NODE_NAME",
				Value: "$(POD_NAMESPACE)-$(POD_NAME)",
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
				Name:  "PMM_AGENT_SIDECAR",
				Value: "true",
			},
			{
				Name:  "PMM_AGENT_SIDECAR_SLEEP",
				Value: "5",
			},
			{
				Name:  "DB_TYPE",
				Value: "postgresql",
			},
			{
				Name:  "PMM_AGENT_PRERUN_SCRIPT",
				Value: "pmm-admin status --wait=10s; pmm-admin add postgresql --tls-skip-verify --skip-connection-check --metrics-mode=push --socket=/tmp/postgres/ --service-name=$(PMM_AGENT_SETUP_NODE_NAME) --query-source=pgstatmonitor; pmm-admin annotate --service-name=$(PMM_AGENT_SETUP_NODE_NAME) 'Service restarted'",
			},
		},
	}
}
