package pmm

import (
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	"github.com/percona/percona-postgresql-operator/internal/postgres"
	v2 "github.com/percona/percona-postgresql-operator/pkg/apis/pgv2.percona.com/v2"
)

const (
	secretKey   = "PMM_SERVER_KEY"   // nolint:gosec
	secretToken = "PMM_SERVER_TOKEN" // nolint:gosec
)

// Container evaluates the pmm secret and according to the existence of the pmm token, determines
// if the PMM2 or the PMM3 container should be configured.
func Container(secret *corev1.Secret, pgc *v2.PerconaPGCluster) (corev1.Container, error) {
	if secret == nil {
		return corev1.Container{}, fmt.Errorf("secret is nil")
	}
	if v, exists := secret.Data[secretToken]; exists && len(v) != 0 {
		return sidecarContainerV3(pgc), nil
	}
	if v, exists := secret.Data[secretKey]; exists && len(v) != 0 {
		return sidecarContainerV2(pgc), nil
	}

	return corev1.Container{}, fmt.Errorf("can't enable PMM: neither %s for PMM3 nor %s for PMM2 exist in the provided secret or they are empty", secretToken, secretKey)
}

// sidecarContainerV2 refers to the construction of the PMM2 container.
func sidecarContainerV2(pgc *v2.PerconaPGCluster) corev1.Container {
	ports := []corev1.ContainerPort{{ContainerPort: 7777}}

	for port := 30100; port <= 30105; port++ {
		// can't overflow int32, disable linter
		ports = append(ports, corev1.ContainerPort{ContainerPort: int32(port)}) // nolint:gosec
	}

	pmmSpec := pgc.Spec.PMM

	container := corev1.Container{
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
					Port:   intstr.FromInt32(7777),
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
						"pmm-admin unregister --force",
					},
				},
			},
		},
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      "cert-volume",
				MountPath: "/pgconf/tls",
				ReadOnly:  true,
			},
			{
				Name:      postgres.DataVolumeMount().Name,
				MountPath: postgres.DataVolumeMount().MountPath,
				ReadOnly:  true,
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
						Key: secretKey,
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
				Name:  "PMM_AGENT_LOG_LEVEL",
				Value: "info",
			},
			{
				Name:  "PMM_AGENT_DEBUG",
				Value: "false",
			},
			{
				Name:  "PMM_AGENT_TRACE",
				Value: "false",
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
				Name:  "DB_USER",
				Value: v2.UserMonitoring,
			},
			{
				Name: "DB_PASS",
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: pgc.UserMonitoring(),
						},
						Key: "password",
					},
				},
			},
			{
				Name:  "PMM_AGENT_PRERUN_SCRIPT",
				Value: agentPrerunScript(pgc.Spec.PMM.QuerySource),
			},
		},
	}

	if pgc.CompareVersion("2.3.0") >= 0 {
		container.Env = append(container.Env, corev1.EnvVar{
			Name:  "PMM_AGENT_PATHS_TEMPDIR",
			Value: "/tmp",
		})
	}

	return container
}

// sidecarContainerV3 refers to the construction of the PMM3 container.
func sidecarContainerV3(pgc *v2.PerconaPGCluster) corev1.Container {
	ports := []corev1.ContainerPort{{ContainerPort: 7777}}

	for port := 30100; port <= 30105; port++ {
		// can't overflow int32, disable linter
		ports = append(ports, corev1.ContainerPort{ContainerPort: int32(port)}) // nolint:gosec
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
					Port:   intstr.FromInt32(7777),
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
						"pmm-admin unregister --force",
					},
				},
			},
		},
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      "cert-volume",
				MountPath: "/pgconf/tls",
				ReadOnly:  true,
			},
			{
				Name:      postgres.DataVolumeMount().Name,
				MountPath: postgres.DataVolumeMount().MountPath,
				ReadOnly:  true,
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
				Name:  "PMM_AGENT_SERVER_ADDRESS",
				Value: pmmSpec.ServerHost,
			},
			{
				Name:  "PMM_AGENT_SERVER_USERNAME",
				Value: "service_token",
			},
			{
				Name: "PMM_AGENT_SERVER_PASSWORD",
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: pmmSpec.Secret,
						},
						Key: secretToken,
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
				Value: "/usr/local/percona/pmm/config/pmm-agent.yaml",
			},
			{
				Name:  "PMM_AGENT_LOG_LEVEL",
				Value: "info",
			},
			{
				Name:  "PMM_AGENT_DEBUG",
				Value: "false",
			},
			{
				Name:  "PMM_AGENT_TRACE",
				Value: "false",
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
				Name:  "DB_USER",
				Value: v2.UserMonitoring,
			},
			{
				Name: "DB_PASS",
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: pgc.UserMonitoring(),
						},
						Key: "password",
					},
				},
			},
			{
				Name:  "PMM_AGENT_PRERUN_SCRIPT",
				Value: agentPrerunScript(pgc.Spec.PMM.QuerySource),
			},
			{
				Name:  "PMM_AGENT_PATHS_TEMPDIR",
				Value: "/tmp",
			},
		},
	}
}

func agentPrerunScript(querySource v2.PMMQuerySource) string {
	wait := "pmm-admin status --wait=10s"
	annotate := "pmm-admin annotate --service-name=$(PMM_AGENT_SETUP_NODE_NAME) 'Service restarted'"

	addServiceArgs := []string{
		"--username=$(DB_USER)",
		"--password='$(DB_PASS)'",
		"--host=127.0.0.1",
		"--port=5432",
		"--tls-cert-file=/pgconf/tls/tls.crt",
		"--tls-key-file=/pgconf/tls/tls.key",
		"--tls-ca-file=/pgconf/tls/ca.crt",
		"--tls-skip-verify",
		"--skip-connection-check",
		"--metrics-mode=push",
		"--service-name=$(PMM_AGENT_SETUP_NODE_NAME)",
		fmt.Sprintf("--query-source=%s", querySource),
	}
	addService := fmt.Sprintf("pmm-admin add postgresql %s", strings.Join(addServiceArgs, " "))

	return wait + "; " + addService + "; " + annotate
}
