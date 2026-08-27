package logcollector

import (
	"fmt"

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"

	"github.com/percona/percona-postgresql-operator/v3/internal/naming"
	"github.com/percona/percona-postgresql-operator/v3/internal/postgres"
	"github.com/percona/percona-postgresql-operator/v3/percona/logcollector/logrotate"
	pNaming "github.com/percona/percona-postgresql-operator/v3/percona/naming"
	v2 "github.com/percona/percona-postgresql-operator/v3/pkg/apis/pgv2.percona.com/v2"
)

const (
	containerName                    = "logs"
	configMapNameSuffix              = "log-collector-config"
	volumeName                       = "log-collector-volume"
	fluentBitCustomConfigurationFile = "fluentbit_custom.yaml"
	customConfigMountPath            = "/opt/crunchy/logcollector/fluentbit/custom"
	entrypoint                       = "/opt/crunchy/logcollector/entrypoint.sh"
)

// postgresLogPath returns the directory where Postgres' logging collector writes
// server logs. By default this is the "log" directory inside the versioned data
// directory (e.g. /pgdata/pg18/log), which already exists on the persistent data
// volume. Users may relocate it via spec.patroni.dynamicConfiguration's
// "log_directory" parameter; we resolve that value with the same sanitization
// Postgres applies so the collector always reads where Postgres actually writes.
func postgresLogPath(cr *v2.PerconaPGCluster) string {
	dataDir := fmt.Sprintf("/pgdata/pg%d", cr.Spec.PostgresVersion)

	logDirectory := "log"
	if dir := logDirectoryParameter(cr); dir != "" {
		logDirectory = dir
	}

	directory, _, _ := postgres.ResolveLogDirectory(dataDir, logDirectory)
	return directory
}

// logDirectoryParameter returns the raw "log_directory" Postgres parameter set
// via spec.patroni.dynamicConfiguration, or "" when it is unset.
func logDirectoryParameter(cr *v2.PerconaPGCluster) string {
	if cr.Spec.Patroni == nil || cr.Spec.Patroni.DynamicConfiguration == nil {
		return ""
	}

	postgresql, ok := cr.Spec.Patroni.DynamicConfiguration["postgresql"].(map[string]any)
	if !ok {
		return ""
	}

	params, ok := postgresql["parameters"].(map[string]any)
	if !ok {
		return ""
	}

	dir, _ := params["log_directory"].(string)
	return dir
}

func configMapName(prefix string) string {
	if prefix == "" {
		return configMapNameSuffix
	}
	return prefix + "-" + configMapNameSuffix
}

// postgresUserID is the UID PostgreSQL owns its 0700/0600 logs with; the
// collector sidecars must match it to read and rotate them.
const postgresUserID int64 = 26

func securityContext(cr *v2.PerconaPGCluster) *corev1.SecurityContext {
	if cr.Spec.LogCollector != nil && cr.Spec.LogCollector.ContainerSecurityContext != nil {
		return cr.Spec.LogCollector.ContainerSecurityContext
	}

	sc := &corev1.SecurityContext{
		RunAsNonRoot:             new(true),
		AllowPrivilegeEscalation: new(false),
		Privileged:               new(false),
		Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
		SeccompProfile:           &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
	}

	// OpenShift assigns one shared UID to every container in the pod.
	if cr.Spec.OpenShift == nil || !*cr.Spec.OpenShift {
		sc.RunAsUser = new(postgresUserID)
		sc.RunAsGroup = new(postgresUserID)
	}

	return sc
}

// volumes returns the pod-level volumes required by the log collector
// sidecars for a PostgreSQL instance pod.
func volumes(cr *v2.PerconaPGCluster) []corev1.Volume {
	if !cr.LogCollectorEnabled() {
		return nil
	}

	var vols []corev1.Volume

	if cr.Spec.LogCollector.Configuration != "" {
		vols = append(vols, corev1.Volume{
			Name: volumeName,
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: configMapName(cr.Name),
					},
				},
			},
		})
	}

	if v := logrotateVolume(cr); v != nil {
		vols = append(vols, *v)
	}

	vols = append(vols, cr.Spec.LogCollector.Volumes...)

	return vols
}

func logrotateVolume(cr *v2.PerconaPGCluster) *corev1.Volume {
	lr := cr.Spec.LogCollector.LogRotate
	if lr == nil || (lr.Configuration == "" && lr.ExtraConfig.Name == "") {
		return nil
	}

	var sources []corev1.VolumeProjection
	if lr.Configuration != "" {
		sources = append(sources, corev1.VolumeProjection{
			ConfigMap: &corev1.ConfigMapProjection{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: logrotate.ConfigMapName(cr.Name),
				},
			},
		})
	}
	if lr.ExtraConfig.Name != "" {
		sources = append(sources, corev1.VolumeProjection{
			ConfigMap: &corev1.ConfigMapProjection{
				LocalObjectReference: lr.ExtraConfig,
			},
		})
	}

	return &corev1.Volume{
		Name: logrotate.VolumeName,
		VolumeSource: corev1.VolumeSource{
			Projected: &corev1.ProjectedVolumeSource{Sources: sources},
		},
	}
}

// instanceContainers returns the log collector sidecars for a PostgreSQL instance pod.
func instanceContainers(cr *v2.PerconaPGCluster) ([]corev1.Container, error) {
	return containers(cr, postgres.DataVolumeMount(), instanceEnv(cr))
}

const (
	// nssWrapperInitName stages libnss_wrapper.so for the logrotate sidecar.
	nssWrapperInitName = "nss-wrapper-init-logcollector"
	// nssWrapperLibPath is where the logrotate sidecar preloads the library from;
	// it must match logrotate's LD_PRELOAD path on the shared /tmp volume.
	nssWrapperLibPath = "/tmp/nss_wrapper/libnss_wrapper.so"
)

// instanceInitContainers returns the init containers the log collector needs on a
// PostgreSQL instance pod. The logrotate sidecar runs the Fluent Bit image, which
// does not bundle libnss_wrapper.so; this init container runs the Postgres image
// (which ships the library under /usr/lib64) and stages it onto the shared /tmp
// volume so the sidecar can LD_PRELOAD it. It is only wired when the log collector
// is enabled, so clusters without log collection keep their pod template unchanged
// (and are not rolled on a minor operator upgrade).
func instanceInitContainers(cr *v2.PerconaPGCluster) []corev1.Container {
	if !cr.LogCollectorEnabled() {
		return nil
	}

	return []corev1.Container{{
		Name:            nssWrapperInitName,
		Image:           cr.Spec.Image,
		ImagePullPolicy: cr.Spec.ImagePullPolicy,
		// The /tmp volume mount is added to every container by the operator; create
		// the nss_wrapper directory defensively so ordering against the shared
		// nss-wrapper-init container does not matter.
		Command: []string{"bash", "-c",
			"mkdir -p /tmp/nss_wrapper && " +
				"{ [[ -f " + nssWrapperLibPath + " ]] || cp /usr/lib64/libnss_wrapper.so " + nssWrapperLibPath + "; }",
		},
		SecurityContext: securityContext(cr),
		Resources:       cr.Spec.LogCollector.Resources,
	}}
}

func containers(cr *v2.PerconaPGCluster, dataMount corev1.VolumeMount, env []corev1.EnvVar) ([]corev1.Container, error) {
	if !cr.LogCollectorEnabled() {
		return nil, nil
	}

	logs, err := logContainer(cr, dataMount, env)
	if err != nil {
		return nil, err
	}

	rotate, err := logrotate.Container(cr, dataMount)
	if err != nil {
		return nil, err
	}
	rotate.SecurityContext = securityContext(cr)

	return []corev1.Container{*logs, *rotate}, nil
}

func logContainer(cr *v2.PerconaPGCluster, dataMount corev1.VolumeMount, env []corev1.EnvVar) (*corev1.Container, error) {
	if cr.Spec.LogCollector == nil {
		return nil, errors.New("logcollector can't be nil")
	}

	env = append(env, cr.Spec.LogCollector.Env...)

	container := corev1.Container{
		Name:            containerName,
		Image:           cr.Spec.LogCollector.Image,
		ImagePullPolicy: cr.Spec.LogCollector.ImagePullPolicy,
		SecurityContext: securityContext(cr),
		Resources:       cr.Spec.LogCollector.Resources,
		LivenessProbe:   cr.Spec.LogCollector.LivenessProbe,
		ReadinessProbe:  cr.Spec.LogCollector.ReadinessProbe,
		Command:         []string{entrypoint},
		Args:            []string{"fluent-bit"},
		Env:             env,
		EnvFrom:         append([]corev1.EnvFromSource(nil), cr.Spec.LogCollector.EnvFrom...),
		VolumeMounts: []corev1.VolumeMount{
			{Name: dataMount.Name, MountPath: dataMount.MountPath},
			// entrypoint + fluent-bit config, staged by the operator init container.
			{Name: pNaming.CrunchyBinVolumeName, MountPath: pNaming.CrunchyBinVolumePath},
		},
	}

	if len(container.EnvFrom) == 0 {
		container.EnvFrom = nil
	}

	if cr.Spec.LogCollector.Configuration != "" {
		container.VolumeMounts = append(container.VolumeMounts, corev1.VolumeMount{
			Name:      volumeName,
			MountPath: customConfigMountPath,
		})
	}

	container.VolumeMounts = append(container.VolumeMounts, cr.Spec.LogCollector.VolumeMounts...)

	return &container, nil
}

func instanceEnv(cr *v2.PerconaPGCluster) []corev1.EnvVar {
	return append(
		[]corev1.EnvVar{
			{Name: "PG_LOG_DIR", Value: postgresLogPath(cr)},
			{Name: "PGBACKREST_LOG_DIR", Value: naming.PGBackRestPGDataLogPath},
		},
		podEnv()...,
	)
}

func podEnv() []corev1.EnvVar {
	return []corev1.EnvVar{
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
	}
}
