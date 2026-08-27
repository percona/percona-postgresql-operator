package logrotate

import (
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"

	pNaming "github.com/percona/percona-postgresql-operator/v3/percona/naming"
	v2 "github.com/percona/percona-postgresql-operator/v3/pkg/apis/pgv2.percona.com/v2"
)

const (
	containerName = "logrotate"

	configMapNameSuffix = "log-collector-logrotate-config"
	VolumeName          = "log-collector-logrotate-volume"

	// PostgresConfig is the ConfigMap key holding the operator-managed
	// logrotate snippet.
	PostgresConfig = "postgres.conf"

	defaultSchedule = "0 0 * * *"

	configMountPath = "/opt/crunchy/logcollector/logrotate/conf.d"
	entrypoint      = "/opt/crunchy/logcollector/entrypoint.sh"

	// nss_wrapper lets logrotate resolve its own (postgres) UID even when that
	// UID has no /etc/passwd entry in the collector image. The collector image
	// does not ship libnss_wrapper.so, so the nss-wrapper-init init container
	// (which runs the Postgres image) stages it onto the shared /tmp volume
	// alongside the passwd and group files; we preload it from there.
	nssWrapperLib    = "/tmp/nss_wrapper/libnss_wrapper.so"
	nssWrapperPasswd = "/tmp/nss_wrapper/postgres/passwd" // #nosec G101 this is a file path, not a credential
	nssWrapperGroup  = "/tmp/nss_wrapper/postgres/group"
)

func ConfigMapName(prefix string) string {
	if prefix == "" {
		return configMapNameSuffix
	}
	return prefix + "-" + configMapNameSuffix
}

func Container(cr *v2.PerconaPGCluster, dataMount corev1.VolumeMount) (*corev1.Container, error) {
	if cr.Spec.LogCollector == nil {
		return nil, errors.New("logcollector can't be nil")
	}

	container := corev1.Container{
		Name:            containerName,
		Image:           cr.Spec.LogCollector.Image,
		ImagePullPolicy: cr.Spec.LogCollector.ImagePullPolicy,
		SecurityContext: cr.Spec.LogCollector.ContainerSecurityContext,
		Resources:       cr.Spec.LogCollector.Resources,
		LivenessProbe:   livenessProbe(cr.Spec.LogCollector.LogRotate),
		ReadinessProbe:  readinessProbe(cr.Spec.LogCollector.LogRotate),
		Command:         []string{entrypoint},
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
				Value: schedule(cr.Spec.LogCollector.LogRotate),
			},
			// Resolve the postgres UID via nss_wrapper (library, passwd, and group
			// all staged by the nss-wrapper-init init container on the shared /tmp
			// volume).
			{Name: "LD_PRELOAD", Value: nssWrapperLib},
			{Name: "NSS_WRAPPER_PASSWD", Value: nssWrapperPasswd},
			{Name: "NSS_WRAPPER_GROUP", Value: nssWrapperGroup},
		},
		VolumeMounts: []corev1.VolumeMount{
			{Name: dataMount.Name, MountPath: dataMount.MountPath},
			// entrypoint + logrotate config, staged by the operator init container.
			{Name: pNaming.CrunchyBinVolumeName, MountPath: pNaming.CrunchyBinVolumePath},
		},
	}

	if lr := cr.Spec.LogCollector.LogRotate; lr != nil {
		if lr.Configuration != "" || lr.ExtraConfig.Name != "" {
			container.VolumeMounts = append(container.VolumeMounts, corev1.VolumeMount{
				Name:      VolumeName,
				MountPath: configMountPath,
			})
		}
	}

	container.VolumeMounts = append(container.VolumeMounts, cr.Spec.LogCollector.VolumeMounts...)

	return &container, nil
}

func schedule(lr *v2.LogRotateSpec) string {
	if lr != nil && lr.Schedule != "" {
		return lr.Schedule
	}
	return defaultSchedule
}

func livenessProbe(lr *v2.LogRotateSpec) *corev1.Probe {
	if lr == nil {
		return nil
	}
	return lr.LivenessProbe
}

func readinessProbe(lr *v2.LogRotateSpec) *corev1.Probe {
	if lr == nil {
		return nil
	}
	return lr.ReadinessProbe
}
