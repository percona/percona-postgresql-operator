package pmm

import (
	"testing"

	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/percona/percona-postgresql-operator/percona/version"
	v2 "github.com/percona/percona-postgresql-operator/pkg/apis/pgv2.percona.com/v2"
)

func TestContainer(t *testing.T) {
	pmmSpec := &v2.PMMSpec{
		Image:                    "percona/pmm-client:pmm",
		ImagePullPolicy:          corev1.PullIfNotPresent,
		ServerHost:               "pmm.server.local",
		Secret:                   "pmm-secret",
		PostgresParams:           "--environment=dev-postgres",
		Resources:                corev1.ResourceRequirements{},
		ContainerSecurityContext: &corev1.SecurityContext{},
	}

	pgc := &v2.PerconaPGCluster{
		Spec: v2.PerconaPGClusterSpec{
			CRVersion: version.Version(),
			PMM:       pmmSpec,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster",
			Namespace: "test-namespace",
		},
	}

	tests := map[string]struct {
		secret             func() *corev1.Secret
		verificationEnvVar func() corev1.EnvVar
		err                error
	}{
		"pmm2 container": {
			secret: func() *corev1.Secret {
				secret := &corev1.Secret{}
				secret.Data = map[string][]byte{
					"PMM_SERVER_KEY": []byte(`key`),
				}
				return secret
			},
			verificationEnvVar: func() corev1.EnvVar {
				return corev1.EnvVar{
					Name:  "PMM_AGENT_SERVER_USERNAME",
					Value: "api_key",
				}
			},
		},
		"pmm server key has no data": {
			secret: func() *corev1.Secret {
				secret := &corev1.Secret{}
				secret.Data = map[string][]byte{
					"PMM_SERVER_KEY": []byte(``),
				}
				return secret
			},
			err: errors.New("can't enable PMM: neither PMM_SERVER_TOKEN for PMM3 nor PMM_SERVER_KEY for PMM2 exist in the provided secret or they are empty"),
		},
		"pmm3 when both server key and token exist": {
			secret: func() *corev1.Secret {
				secret := &corev1.Secret{}
				secret.Data = map[string][]byte{
					"PMM_SERVER_KEY":   []byte(`key`),
					"PMM_SERVER_TOKEN": []byte(`token`),
				}
				return secret
			},
			verificationEnvVar: func() corev1.EnvVar {
				return corev1.EnvVar{
					Name:  "PMM_AGENT_SERVER_USERNAME",
					Value: "service_token",
				}
			},
		},
		"pmm3 when only token exists": {
			secret: func() *corev1.Secret {
				secret := &corev1.Secret{}
				secret.Data = map[string][]byte{
					"PMM_SERVER_TOKEN": []byte(`token`),
				}
				return secret
			},
			verificationEnvVar: func() corev1.EnvVar {
				return corev1.EnvVar{
					Name:  "PMM_AGENT_SERVER_USERNAME",
					Value: "service_token",
				}
			},
		},
		"pmm token has no data": {
			secret: func() *corev1.Secret {
				secret := &corev1.Secret{}
				secret.Data = map[string][]byte{
					"PMM_SERVER_TOKEN": []byte(``),
				}
				return secret
			},
			err: errors.New("can't enable PMM: neither PMM_SERVER_TOKEN for PMM3 nor PMM_SERVER_KEY for PMM2 exist in the provided secret or they are empty"),
		},
		"error due to missing secret": {
			secret: func() *corev1.Secret {
				secret := &corev1.Secret{}
				secret.Data = map[string][]byte{
					"RANDOM_SECRET": []byte(`foo`),
				}
				return secret
			},
			err: errors.New("can't enable PMM: neither PMM_SERVER_TOKEN for PMM3 nor PMM_SERVER_KEY for PMM2 exist in the provided secret or they are empty"),
		},
		"error due to nil secret": {
			secret: func() *corev1.Secret {
				return nil
			},
			err: errors.New("secret is nil"),
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			container, err := Container(tt.secret(), pgc)
			if tt.err != nil {
				assert.EqualError(t, err, tt.err.Error())
				return
			}
			require.NoError(t, err)
			assert.NotNil(t, container.VolumeMounts)
			assert.Contains(t, container.Env, tt.verificationEnvVar())
		})
	}

}

func TestSidecarContainerV2(t *testing.T) {
	pmmSpec := &v2.PMMSpec{
		Image:                    "percona/pmm-client:pmm2-enabled",
		ImagePullPolicy:          corev1.PullIfNotPresent,
		ServerHost:               "pmm.server.local",
		Secret:                   "pmm-secret",
		PostgresParams:           "--environment=dev-postgres",
		Resources:                corev1.ResourceRequirements{},
		ContainerSecurityContext: &corev1.SecurityContext{},
	}

	pgc := &v2.PerconaPGCluster{
		Spec: v2.PerconaPGClusterSpec{
			CRVersion: version.Version(),
			PMM:       pmmSpec,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster",
			Namespace: "test-namespace",
		},
	}

	container := sidecarContainerV2(pgc)

	assert.Equal(t, "pmm-client", container.Name)
	assert.Equal(t, pmmSpec.Image, container.Image)
	assert.Equal(t, pmmSpec.ImagePullPolicy, container.ImagePullPolicy)
	assert.Len(t, container.Ports, 7)

	expectedPorts := []int32{7777, 30100, 30101, 30102, 30103, 30104, 30105}
	for i, port := range container.Ports {
		assert.Equal(t, expectedPorts[i], port.ContainerPort)
	}

	assert.NotNil(t, container.LivenessProbe)
	assert.Equal(t, "/local/Status", container.LivenessProbe.HTTPGet.Path)
	assert.Equal(t, int32(7777), container.LivenessProbe.HTTPGet.Port.IntVal)

	assert.NotNil(t, container.Lifecycle)
	assert.NotNil(t, container.Lifecycle.PreStop)
	assert.Equal(t, []string{"bash", "-c", "pmm-admin unregister --force"}, container.Lifecycle.PreStop.Exec.Command)

	assert.Len(t, container.Env, 31)

	expectedEnvVars := map[string]string{
		"POD_NAME":                      "", // field reference is asserted separately
		"POD_NAMESPACE":                 "", // field reference is asserted separately
		"PMM_USER":                      "api_key",
		"PMM_SERVER":                    pmmSpec.ServerHost,
		"PMM_AGENT_SERVER_ADDRESS":      pmmSpec.ServerHost,
		"PMM_AGENT_SERVER_USERNAME":     "api_key",
		"PMM_AGENT_SERVER_PASSWORD":     "", // secret reference is asserted separately
		"CLIENT_PORT_LISTEN":            "7777",
		"CLIENT_PORT_MIN":               "30100",
		"CLIENT_PORT_MAX":               "30105",
		"PMM_AGENT_LISTEN_PORT":         "7777",
		"PMM_AGENT_PORTS_MIN":           "30100",
		"PMM_AGENT_PORTS_MAX":           "30105",
		"PMM_AGENT_CONFIG_FILE":         "/usr/local/percona/pmm2/config/pmm-agent.yaml",
		"PMM_AGENT_LOG_LEVEL":           "info",
		"PMM_AGENT_DEBUG":               "false",
		"PMM_AGENT_TRACE":               "false",
		"PMM_AGENT_SERVER_INSECURE_TLS": "1",
		"PMM_AGENT_LISTEN_ADDRESS":      "0.0.0.0",
		"PMM_AGENT_SETUP_NODE_NAME":     "$(POD_NAMESPACE)-$(POD_NAME)",
		"PMM_AGENT_SETUP_METRICS_MODE":  "push",
		"PMM_AGENT_SETUP":               "1",
		"PMM_AGENT_SETUP_FORCE":         "1",
		"PMM_AGENT_SETUP_NODE_TYPE":     "container",
		"PMM_AGENT_SIDECAR":             "true",
		"PMM_AGENT_SIDECAR_SLEEP":       "5",
		"DB_TYPE":                       "postgresql",
		"DB_USER":                       v2.UserMonitoring,
		"DB_PASS":                       "", // secret reference is asserted separately
		"PMM_AGENT_PRERUN_SCRIPT":       "pmm-admin status --wait=10s; pmm-admin add postgresql --username=$(DB_USER) --password='$(DB_PASS)' --host=127.0.0.1 --port=5432 --tls-cert-file=/pgconf/tls/tls.crt --tls-key-file=/pgconf/tls/tls.key --tls-ca-file=/pgconf/tls/ca.crt --tls-skip-verify --skip-connection-check --metrics-mode=push --service-name=$(PMM_AGENT_SETUP_NODE_NAME) --query-source= --cluster=test-cluster --environment=dev-postgres; pmm-admin annotate --service-name=$(PMM_AGENT_SETUP_NODE_NAME) 'Service restarted'",
		"PMM_AGENT_PATHS_TEMPDIR":       "/tmp",
	}

	for _, envVar := range container.Env {
		assert.Contains(t, expectedEnvVars, envVar.Name)

		switch envVar.Name {
		case "POD_NAME", "POD_NAMESPACE":
			assert.NotNil(t, envVar.ValueFrom)
			assert.NotNil(t, envVar.ValueFrom.FieldRef)
			assert.Equal(t, "v1", envVar.ValueFrom.FieldRef.APIVersion)

			expectedFieldPath := map[string]string{
				"POD_NAME":      "metadata.name",
				"POD_NAMESPACE": "metadata.namespace",
			}
			assert.Equal(t, expectedFieldPath[envVar.Name], envVar.ValueFrom.FieldRef.FieldPath)
		case "PMM_AGENT_SERVER_PASSWORD":
			assert.NotNil(t, envVar.ValueFrom)
			assert.NotNil(t, envVar.ValueFrom.SecretKeyRef)
			assert.Equal(t, "PMM_SERVER_KEY", envVar.ValueFrom.SecretKeyRef.Key)
			assert.Equal(t, "pmm-secret", envVar.ValueFrom.SecretKeyRef.Name)
		case "DB_PASS":
			assert.NotNil(t, envVar.ValueFrom)
			assert.NotNil(t, envVar.ValueFrom.SecretKeyRef)
			assert.Equal(t, "password", envVar.ValueFrom.SecretKeyRef.Key)
			assert.Equal(t, "test-cluster-pguser-monitor", envVar.ValueFrom.SecretKeyRef.Name)
		default:
			assert.Equal(t, expectedEnvVars[envVar.Name], envVar.Value)
		}
	}

	assert.Len(t, container.VolumeMounts, 1)
	assert.Equal(t, "/pgconf/tls", container.VolumeMounts[0].MountPath)
	assert.True(t, container.VolumeMounts[0].ReadOnly)
}

func TestSidecarContainerV3(t *testing.T) {
	pmmSpec := &v2.PMMSpec{
		Image:                    "percona/pmm-client:pmm3-enabled",
		ImagePullPolicy:          corev1.PullIfNotPresent,
		ServerHost:               "pmm.server.local",
		Secret:                   "pmm-secret",
		PostgresParams:           "--environment=dev-postgres",
		Resources:                corev1.ResourceRequirements{},
		ContainerSecurityContext: &corev1.SecurityContext{},
	}

	pgc := &v2.PerconaPGCluster{
		Spec: v2.PerconaPGClusterSpec{
			CRVersion: version.Version(),
			PMM:       pmmSpec,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster",
			Namespace: "test-namespace",
		},
	}

	container := sidecarContainerV3(pgc)

	assert.Equal(t, "pmm-client", container.Name)
	assert.Equal(t, pmmSpec.Image, container.Image)
	assert.Equal(t, pmmSpec.ImagePullPolicy, container.ImagePullPolicy)
	assert.Len(t, container.Ports, 7)

	expectedPorts := []int32{7777, 30100, 30101, 30102, 30103, 30104, 30105}
	for i, port := range container.Ports {
		assert.Equal(t, expectedPorts[i], port.ContainerPort)
	}

	assert.NotNil(t, container.LivenessProbe)
	assert.Equal(t, "/local/Status", container.LivenessProbe.HTTPGet.Path)
	assert.Equal(t, int32(7777), container.LivenessProbe.HTTPGet.Port.IntVal)

	assert.NotNil(t, container.Lifecycle)
	assert.NotNil(t, container.Lifecycle.PreStop)
	assert.Equal(t, []string{"bash", "-c", "pmm-admin unregister --force"}, container.Lifecycle.PreStop.Exec.Command)

	assert.Len(t, container.Env, 26)

	expectedEnvVars := map[string]string{
		"POD_NAME":                      "", // field reference is asserted separately
		"POD_NAMESPACE":                 "", // field reference is asserted separately
		"PMM_AGENT_SERVER_ADDRESS":      pmmSpec.ServerHost,
		"PMM_AGENT_SERVER_USERNAME":     "service_token",
		"PMM_AGENT_SERVER_PASSWORD":     "", // secret reference is asserted separately
		"PMM_AGENT_LISTEN_PORT":         "7777",
		"PMM_AGENT_PORTS_MIN":           "30100",
		"PMM_AGENT_PORTS_MAX":           "30105",
		"PMM_AGENT_CONFIG_FILE":         "/usr/local/percona/pmm/config/pmm-agent.yaml",
		"PMM_AGENT_LOG_LEVEL":           "info",
		"PMM_AGENT_DEBUG":               "false",
		"PMM_AGENT_TRACE":               "false",
		"PMM_AGENT_SERVER_INSECURE_TLS": "1",
		"PMM_AGENT_LISTEN_ADDRESS":      "0.0.0.0",
		"PMM_AGENT_SETUP_NODE_NAME":     "$(POD_NAMESPACE)-$(POD_NAME)",
		"PMM_AGENT_SETUP_METRICS_MODE":  "push",
		"PMM_AGENT_SETUP":               "1",
		"PMM_AGENT_SETUP_FORCE":         "1",
		"PMM_AGENT_SETUP_NODE_TYPE":     "container",
		"PMM_AGENT_SIDECAR":             "true",
		"PMM_AGENT_SIDECAR_SLEEP":       "5",
		"DB_TYPE":                       "postgresql",
		"DB_USER":                       v2.UserMonitoring,
		"DB_PASS":                       "", // secret reference is asserted separately
		"PMM_AGENT_PRERUN_SCRIPT":       "pmm-admin status --wait=10s; pmm-admin add postgresql --username=$(DB_USER) --password='$(DB_PASS)' --host=127.0.0.1 --port=5432 --tls-cert-file=/pgconf/tls/tls.crt --tls-key-file=/pgconf/tls/tls.key --tls-ca-file=/pgconf/tls/ca.crt --tls-skip-verify --skip-connection-check --metrics-mode=push --service-name=$(PMM_AGENT_SETUP_NODE_NAME) --query-source= --cluster=test-cluster --environment=dev-postgres; pmm-admin add external --scheme=https --listen-port=8008 --tls-skip-verify --service-name=$(PMM_AGENT_SETUP_NODE_NAME)-patroni-external; pmm-admin annotate --service-name=$(PMM_AGENT_SETUP_NODE_NAME) 'Service restarted'",
		"PMM_AGENT_PATHS_TEMPDIR":       "/tmp",
	}

	for _, envVar := range container.Env {
		assert.Contains(t, expectedEnvVars, envVar.Name)

		switch envVar.Name {
		case "POD_NAME", "POD_NAMESPACE":
			assert.NotNil(t, envVar.ValueFrom)
			assert.NotNil(t, envVar.ValueFrom.FieldRef)
			assert.Equal(t, "v1", envVar.ValueFrom.FieldRef.APIVersion)

			expectedFieldPath := map[string]string{
				"POD_NAME":      "metadata.name",
				"POD_NAMESPACE": "metadata.namespace",
			}
			assert.Equal(t, expectedFieldPath[envVar.Name], envVar.ValueFrom.FieldRef.FieldPath)
		case "PMM_AGENT_SERVER_PASSWORD":
			assert.NotNil(t, envVar.ValueFrom)
			assert.NotNil(t, envVar.ValueFrom.SecretKeyRef)
			assert.Equal(t, "PMM_SERVER_TOKEN", envVar.ValueFrom.SecretKeyRef.Key)
			assert.Equal(t, "pmm-secret", envVar.ValueFrom.SecretKeyRef.Name)
		case "DB_PASS":
			assert.NotNil(t, envVar.ValueFrom)
			assert.NotNil(t, envVar.ValueFrom.SecretKeyRef)
			assert.Equal(t, "password", envVar.ValueFrom.SecretKeyRef.Key)
			assert.Equal(t, "test-cluster-pguser-monitor", envVar.ValueFrom.SecretKeyRef.Name)
		default:
			assert.Equal(t, expectedEnvVars[envVar.Name], envVar.Value)
		}
	}

	assert.Len(t, container.VolumeMounts, 1)
	assert.Equal(t, "/pgconf/tls", container.VolumeMounts[0].MountPath)
	assert.True(t, container.VolumeMounts[0].ReadOnly)
}
