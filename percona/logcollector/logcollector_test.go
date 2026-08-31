package logcollector

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	"github.com/percona/percona-postgresql-operator/v3/internal/naming"
	"github.com/percona/percona-postgresql-operator/v3/internal/postgres"
	"github.com/percona/percona-postgresql-operator/v3/percona/logcollector/logrotate"
	pNaming "github.com/percona/percona-postgresql-operator/v3/percona/naming"
	"github.com/percona/percona-postgresql-operator/v3/percona/version"
	v2 "github.com/percona/percona-postgresql-operator/v3/pkg/apis/pgv2.percona.com/v2"
	crunchyv1beta1 "github.com/percona/percona-postgresql-operator/v3/pkg/apis/upstream.pgv2.percona.com/v1beta1"
)

type builderFn func(cr *v2.PerconaPGCluster) ([]corev1.Container, error)

func TestContainers(t *testing.T) {
	testEnvVar := []corev1.EnvVar{
		{Name: "TEST_ENV1", Value: "test-value1"},
	}
	testEnvFrom := []corev1.EnvFromSource{
		{
			ConfigMapRef: &corev1.ConfigMapEnvSource{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: "test-configmap",
				},
			},
		},
	}
	testLogRotate := &v2.LogRotateSpec{
		Configuration: "my-logrotate-config",
		ExtraConfig: corev1.LocalObjectReference{
			Name: "my-logrotate-extra-config",
		},
		Schedule: "0 0 * * *",
	}

	dataMount := postgres.DataVolumeMount()

	const pgVersion = 16
	instanceLogEnv := []corev1.EnvVar{
		{Name: "PG_LOG_DIR", Value: "/pgdata/pg16/log"},
		{Name: "PGBACKREST_LOG_DIR", Value: naming.PGBackRestPGDataLogPath},
	}

	tests := map[string]struct {
		builder                builderFn
		dataMount              corev1.VolumeMount
		logSpecificEnv         []corev1.EnvVar
		logCollector           *v2.LogCollectorSpec
		logRotate              *v2.LogRotateSpec
		expectedContainerNames []string
		build                  bool
	}{
		"nil logcollector (instance)": {
			builder:      instanceContainers,
			logCollector: nil,
		},
		"logcollector disabled (instance)": {
			builder: instanceContainers,
			logCollector: &v2.LogCollectorSpec{
				Enabled: new(false),
			},
		},
		"logcollector enabled (instance)": {
			builder:        instanceContainers,
			dataMount:      dataMount,
			logSpecificEnv: instanceLogEnv,
			logCollector: &v2.LogCollectorSpec{
				Enabled:         new(true),
				Image:           "log-test-image",
				ImagePullPolicy: corev1.PullIfNotPresent,
			},
			expectedContainerNames: []string{"logs", "logrotate"},
			build:                  true,
		},
		"logcollector enabled with configuration (instance)": {
			builder:        instanceContainers,
			dataMount:      dataMount,
			logSpecificEnv: instanceLogEnv,
			logCollector: &v2.LogCollectorSpec{
				Enabled:         new(true),
				Image:           "log-test-image",
				ImagePullPolicy: corev1.PullIfNotPresent,
				Configuration:   "my-config",
			},
			expectedContainerNames: []string{"logs", "logrotate"},
			build:                  true,
		},
		"logcollector enabled with env variable (instance)": {
			builder:        instanceContainers,
			dataMount:      dataMount,
			logSpecificEnv: instanceLogEnv,
			logCollector: &v2.LogCollectorSpec{
				Enabled:         new(true),
				Image:           "log-test-image",
				ImagePullPolicy: corev1.PullIfNotPresent,
				Configuration:   "my-config",
				Env:             testEnvVar,
				EnvFrom:         testEnvFrom,
			},
			expectedContainerNames: []string{"logs", "logrotate"},
			build:                  true,
		},
		"logcollector enabled with probes (instance)": {
			builder:        instanceContainers,
			dataMount:      dataMount,
			logSpecificEnv: instanceLogEnv,
			logCollector: &v2.LogCollectorSpec{
				Enabled:         new(true),
				Image:           "log-test-image",
				ImagePullPolicy: corev1.PullIfNotPresent,
				LivenessProbe: &corev1.Probe{
					ProbeHandler: corev1.ProbeHandler{
						HTTPGet: &corev1.HTTPGetAction{Path: "/", Port: intstr.FromInt32(2020)},
					},
					InitialDelaySeconds: 10,
				},
				ReadinessProbe: &corev1.Probe{
					ProbeHandler: corev1.ProbeHandler{
						HTTPGet: &corev1.HTTPGetAction{Path: "/api/v1/health", Port: intstr.FromInt32(2020)},
					},
					InitialDelaySeconds: 10,
				},
			},
			expectedContainerNames: []string{"logs", "logrotate"},
			build:                  true,
		},
		"logcollector enabled with logrotate probes (instance)": {
			builder:        instanceContainers,
			dataMount:      dataMount,
			logSpecificEnv: instanceLogEnv,
			logCollector: &v2.LogCollectorSpec{
				Enabled:         new(true),
				Image:           "log-test-image",
				ImagePullPolicy: corev1.PullIfNotPresent,
			},
			logRotate: &v2.LogRotateSpec{
				Schedule: "0 0 * * *",
				LivenessProbe: &corev1.Probe{
					ProbeHandler: corev1.ProbeHandler{
						Exec: &corev1.ExecAction{Command: []string{"/bin/true"}},
					},
					InitialDelaySeconds: 30,
				},
				ReadinessProbe: &corev1.Probe{
					ProbeHandler: corev1.ProbeHandler{
						Exec: &corev1.ExecAction{Command: []string{"/bin/true"}},
					},
					InitialDelaySeconds: 5,
				},
			},
			expectedContainerNames: []string{"logs", "logrotate"},
			build:                  true,
		},
		"logcollector enabled with logrotate (instance)": {
			builder:        instanceContainers,
			dataMount:      dataMount,
			logSpecificEnv: instanceLogEnv,
			logCollector: &v2.LogCollectorSpec{
				Enabled:         new(true),
				Image:           "log-test-image",
				ImagePullPolicy: corev1.PullIfNotPresent,
			},
			logRotate:              testLogRotate,
			expectedContainerNames: []string{"logs", "logrotate"},
			build:                  true,
		},
	}

	for name, tt := range tests {
		spec := tt.logCollector
		if tt.logRotate != nil {
			spec.LogRotate = tt.logRotate
		}
		t.Run(name, func(t *testing.T) {
			cr := &v2.PerconaPGCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-cluster",
					Namespace: "default",
				},
				Spec: v2.PerconaPGClusterSpec{
					CRVersion:       version.Version(),
					PostgresVersion: pgVersion,
					LogCollector:    spec,
				},
			}

			containers, err := tt.builder(cr)
			require.NoError(t, err)

			if !tt.build {
				assert.Nil(t, containers)
				return
			}

			var gotNames []string
			for _, c := range containers {
				gotNames = append(gotNames, c.Name)
			}
			assert.Equal(t, tt.expectedContainerNames, gotNames)

			expected := expectedContainers(tt.dataMount, tt.logSpecificEnv, spec.Configuration, spec.Env, spec.EnvFrom, spec.LivenessProbe, spec.ReadinessProbe, tt.logRotate)
			assert.Equal(t, expected, containers)
		})
	}
}

func TestPostgresLogPath(t *testing.T) {
	const pgVersion = 16

	patroniWithLogDirectory := func(value any) *crunchyv1beta1.PatroniSpec {
		return &crunchyv1beta1.PatroniSpec{
			DynamicConfiguration: crunchyv1beta1.SchemalessObject{
				"postgresql": map[string]any{
					"parameters": map[string]any{
						"log_directory": value,
					},
				},
			},
		}
	}

	tests := map[string]struct {
		patroni  *crunchyv1beta1.PatroniSpec
		expected string
	}{
		"default (no dynamicConfiguration)": {
			patroni:  nil,
			expected: "/pgdata/pg16/log",
		},
		"explicit default value": {
			patroni:  patroniWithLogDirectory("log"),
			expected: "/pgdata/pg16/log",
		},
		"safe absolute path is honored": {
			patroni:  patroniWithLogDirectory("/pgdata/custom-logs"),
			expected: "/pgdata/custom-logs",
		},
		"relative path expands under data directory": {
			patroni:  patroniWithLogDirectory("mylogs"),
			expected: "/pgdata/pg16/mylogs",
		},
		"unsafe path falls back to a safe directory": {
			patroni:  patroniWithLogDirectory("/pgdata/pg16"),
			expected: "/pgdata/logs/postgres",
		},
		"non-string value is ignored": {
			patroni:  patroniWithLogDirectory(123),
			expected: "/pgdata/pg16/log",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			cr := &v2.PerconaPGCluster{
				Spec: v2.PerconaPGClusterSpec{
					PostgresVersion: pgVersion,
					Patroni:         tt.patroni,
				},
			}
			assert.Equal(t, tt.expected, postgresLogPath(cr))
		})
	}
}

func expectedContainers(
	dataMount corev1.VolumeMount,
	logSpecificEnv []corev1.EnvVar,
	configuration string,
	extraEnv []corev1.EnvVar,
	envFrom []corev1.EnvFromSource,
	livenessProbe *corev1.Probe,
	readinessProbe *corev1.Probe,
	logrotateConfig *v2.LogRotateSpec,
) []corev1.Container {
	envs := append([]corev1.EnvVar(nil), logSpecificEnv...)
	envs = append(envs,
		corev1.EnvVar{
			Name: "POD_NAMESPACE",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					FieldPath: "metadata.namespace",
				},
			},
		},
		corev1.EnvVar{
			Name: "POD_NAME",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					FieldPath: "metadata.name",
				},
			},
		},
	)
	envs = append(envs, extraEnv...)

	logsC := corev1.Container{
		Name:            "logs",
		Image:           "log-test-image",
		ImagePullPolicy: corev1.PullIfNotPresent,
		SecurityContext: expectedSecurityContext(),
		LivenessProbe:   livenessProbe,
		ReadinessProbe:  readinessProbe,
		Command:         []string{"/opt/crunchy/logcollector/entrypoint.sh"},
		Args:            []string{"fluent-bit"},
		Env:             envs,
		EnvFrom:         envFrom,
		VolumeMounts: []corev1.VolumeMount{
			{Name: dataMount.Name, MountPath: dataMount.MountPath},
			{Name: pNaming.CrunchyBinVolumeName, MountPath: pNaming.CrunchyBinVolumePath},
		},
	}
	if configuration != "" {
		logsC.VolumeMounts = append(logsC.VolumeMounts, corev1.VolumeMount{
			Name:      volumeName,
			MountPath: "/opt/crunchy/logcollector/fluentbit/custom",
		})
	}

	logRotateC := corev1.Container{
		Name:            "logrotate",
		Image:           "log-test-image",
		ImagePullPolicy: corev1.PullIfNotPresent,
		SecurityContext: expectedSecurityContext(),
		LivenessProbe:   logrotateLivenessProbe(logrotateConfig),
		ReadinessProbe:  logrotateReadinessProbe(logrotateConfig),
		Command:         []string{"/opt/crunchy/logcollector/entrypoint.sh"},
		Args:            []string{"logrotate"},
		Env: []corev1.EnvVar{
			{
				Name: "POD_NAMESPACE",
				ValueFrom: &corev1.EnvVarSource{
					FieldRef: &corev1.ObjectFieldSelector{
						FieldPath: "metadata.namespace",
					},
				},
			},
			{
				Name: "POD_NAME",
				ValueFrom: &corev1.EnvVarSource{
					FieldRef: &corev1.ObjectFieldSelector{
						FieldPath: "metadata.name",
					},
				},
			},
			{
				Name:  "LOGROTATE_SCHEDULE",
				Value: logrotateSchedule(logrotateConfig),
			},
			{Name: "LD_PRELOAD", Value: "/tmp/nss_wrapper/libnss_wrapper.so"},
			{Name: "NSS_WRAPPER_PASSWD", Value: "/tmp/nss_wrapper/postgres/passwd"},
			{Name: "NSS_WRAPPER_GROUP", Value: "/tmp/nss_wrapper/postgres/group"},
		},
		VolumeMounts: []corev1.VolumeMount{
			{Name: dataMount.Name, MountPath: dataMount.MountPath},
			{Name: pNaming.CrunchyBinVolumeName, MountPath: pNaming.CrunchyBinVolumePath},
		},
	}
	if logrotateConfig != nil {
		if logrotateConfig.Configuration != "" || logrotateConfig.ExtraConfig.Name != "" {
			logRotateC.VolumeMounts = append(logRotateC.VolumeMounts, corev1.VolumeMount{
				Name:      logrotate.VolumeName,
				MountPath: "/opt/crunchy/logcollector/logrotate/conf.d",
			})
		}
	}

	return []corev1.Container{logsC, logRotateC}
}

func expectedSecurityContext() *corev1.SecurityContext {
	return &corev1.SecurityContext{
		RunAsUser:                new(int64(26)),
		RunAsGroup:               new(int64(26)),
		RunAsNonRoot:             new(true),
		AllowPrivilegeEscalation: new(false),
		Privileged:               new(false),
		Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
		SeccompProfile:           &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
	}
}

func logrotateSchedule(lr *v2.LogRotateSpec) string {
	if lr != nil && lr.Schedule != "" {
		return lr.Schedule
	}
	return "0 0 * * *"
}

func logrotateLivenessProbe(lr *v2.LogRotateSpec) *corev1.Probe {
	if lr == nil {
		return nil
	}
	return lr.LivenessProbe
}

func logrotateReadinessProbe(lr *v2.LogRotateSpec) *corev1.Probe {
	if lr == nil {
		return nil
	}
	return lr.ReadinessProbe
}

func TestUserVolumeMounts(t *testing.T) {
	mount := corev1.VolumeMount{Name: "s3-ca", MountPath: "/etc/fluentbit/tls", ReadOnly: true}
	cr := &v2.PerconaPGCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "my-cluster", Namespace: "default"},
		Spec: v2.PerconaPGClusterSpec{
			CRVersion:       version.Version(),
			PostgresVersion: 16,
			LogCollector: &v2.LogCollectorSpec{
				Enabled:      new(true),
				Image:        "log-test-image",
				VolumeMounts: []corev1.VolumeMount{mount},
			},
		},
	}

	containers, err := instanceContainers(cr)
	require.NoError(t, err)
	assert.Len(t, containers, 2)
	for _, c := range containers {
		assert.Contains(t, c.VolumeMounts, mount, "container %q missing user volume mount", c.Name)
	}
}

func TestVolumes(t *testing.T) {
	base := func(spec *v2.LogCollectorSpec) *v2.PerconaPGCluster {
		return &v2.PerconaPGCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "my-cluster", Namespace: "default"},
			Spec: v2.PerconaPGClusterSpec{
				CRVersion:    version.Version(),
				LogCollector: spec,
			},
		}
	}

	fluentBitVol := corev1.Volume{
		Name: volumeName,
		VolumeSource: corev1.VolumeSource{
			ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{Name: "my-cluster-log-collector-config"},
			},
		},
	}
	managedLogrotateSource := corev1.VolumeProjection{
		ConfigMap: &corev1.ConfigMapProjection{
			LocalObjectReference: corev1.LocalObjectReference{Name: "my-cluster-log-collector-logrotate-config"},
		},
	}
	extraLogrotateSource := corev1.VolumeProjection{
		ConfigMap: &corev1.ConfigMapProjection{
			LocalObjectReference: corev1.LocalObjectReference{Name: "extra-cm"},
		},
	}
	logrotateVol := func(sources ...corev1.VolumeProjection) corev1.Volume {
		return corev1.Volume{
			Name: logrotate.VolumeName,
			VolumeSource: corev1.VolumeSource{
				Projected: &corev1.ProjectedVolumeSource{Sources: sources},
			},
		}
	}
	caVol := corev1.Volume{
		Name: "s3-ca",
		VolumeSource: corev1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{SecretName: "minio-tls"},
		},
	}

	tests := map[string]struct {
		spec     *v2.LogCollectorSpec
		expected []corev1.Volume
	}{
		"disabled": {
			spec: &v2.LogCollectorSpec{Enabled: new(false), Configuration: "x"},
		},
		"nil": {},
		"enabled without configuration": {
			spec: &v2.LogCollectorSpec{Enabled: new(true)},
		},
		"fluent-bit configuration only": {
			spec:     &v2.LogCollectorSpec{Enabled: new(true), Configuration: "cfg"},
			expected: []corev1.Volume{fluentBitVol},
		},
		"logrotate configuration only": {
			spec: &v2.LogCollectorSpec{
				Enabled:   new(true),
				LogRotate: &v2.LogRotateSpec{Configuration: "cfg"},
			},
			expected: []corev1.Volume{logrotateVol(managedLogrotateSource)},
		},
		"logrotate extraConfig only": {
			spec: &v2.LogCollectorSpec{
				Enabled:   new(true),
				LogRotate: &v2.LogRotateSpec{ExtraConfig: corev1.LocalObjectReference{Name: "extra-cm"}},
			},
			expected: []corev1.Volume{logrotateVol(extraLogrotateSource)},
		},
		"logrotate configuration and extraConfig": {
			spec: &v2.LogCollectorSpec{
				Enabled: new(true),
				LogRotate: &v2.LogRotateSpec{
					Configuration: "cfg",
					ExtraConfig:   corev1.LocalObjectReference{Name: "extra-cm"},
				},
			},
			expected: []corev1.Volume{logrotateVol(managedLogrotateSource, extraLogrotateSource)},
		},
		"all sources": {
			spec: &v2.LogCollectorSpec{
				Enabled:       new(true),
				Configuration: "fbcfg",
				LogRotate: &v2.LogRotateSpec{
					Configuration: "lrcfg",
					ExtraConfig:   corev1.LocalObjectReference{Name: "extra-cm"},
				},
			},
			expected: []corev1.Volume{fluentBitVol, logrotateVol(managedLogrotateSource, extraLogrotateSource)},
		},
		"user volumes appended": {
			spec: &v2.LogCollectorSpec{
				Enabled:       new(true),
				Configuration: "fbcfg",
				Volumes:       []corev1.Volume{caVol},
			},
			expected: []corev1.Volume{fluentBitVol, caVol},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tt.expected, volumes(base(tt.spec)))
		})
	}
}
