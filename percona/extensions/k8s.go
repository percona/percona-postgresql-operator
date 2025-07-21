package extensions

import (
	"fmt"
	"strconv"

	corev1 "k8s.io/api/core/v1"

	"github.com/percona/percona-postgresql-operator/percona/naming"
	pgv2 "github.com/percona/percona-postgresql-operator/pkg/apis/pgv2.percona.com/v2"
)

func GetExtensionKey(pgMajor int, name, version string) string {
	return fmt.Sprintf("%s-pg%d-%s", name, pgMajor, version)
}

// ExtensionRelocatorContainer returns a container that will relocate extensions from the base image (i.e. pg_stat_monitor, pg_audit)
// to the data directory so we don't lose them when user adds a custom extension.
func ExtensionRelocatorContainer(cr *pgv2.PerconaPGCluster, image string, imagePullPolicy corev1.PullPolicy, postgresVersion int) corev1.Container {
	mounts := []corev1.VolumeMount{
		{
			Name:      "postgres-data",
			MountPath: "/pgdata",
		},
	}
	containerName := "extension-relocator"
	if cr.CompareVersion("2.4.0") >= 0 {
		containerName = fmt.Sprintf("extension-relocator-%d", postgresVersion)
	}

	command := "/usr/local/bin/relocate-extensions.sh"

	if cr.CompareVersion("2.8.0") >= 0 {
		command = "/opt/crunchy/bin/relocate-extensions.sh"
		mounts = append(mounts, corev1.VolumeMount{
			Name:      naming.CrunchyBinVolumeName,
			MountPath: naming.CrunchyBinVolumePath,
		})
	}
	return corev1.Container{
		Name:            containerName,
		Image:           image,
		ImagePullPolicy: imagePullPolicy,
		Command:         []string{command},
		Env: []corev1.EnvVar{
			{
				Name:  "PG_VERSION",
				Value: strconv.Itoa(postgresVersion),
			},
		},
		VolumeMounts: mounts,
	}
}

func ExtensionInstallerContainer(cr *pgv2.PerconaPGCluster, postgresVersion int, spec *pgv2.ExtensionsSpec, extensions string, openshift *bool) corev1.Container {
	mounts := []corev1.VolumeMount{
		{
			Name:      "postgres-data",
			MountPath: "/pgdata",
		},
	}
	mounts = append(mounts, ExtensionVolumeMounts(postgresVersion)...)

	containerName := "extension-installer"
	if cr.CompareVersion("2.4.0") >= 0 {
		containerName = fmt.Sprintf("extension-installer-%d", postgresVersion)
	}

	container := corev1.Container{
		Name:            containerName,
		Image:           spec.Image,
		ImagePullPolicy: spec.ImagePullPolicy,
		Command:         []string{"/usr/local/bin/install-extensions.sh"},
		Env: []corev1.EnvVar{
			{
				Name:  "STORAGE_TYPE",
				Value: spec.Storage.Type,
			},
			{
				Name:  "STORAGE_ENDPOINT",
				Value: spec.Storage.Endpoint,
			},
			{
				Name:  "STORAGE_REGION",
				Value: spec.Storage.Region,
			},
			{
				Name:  "STORAGE_BUCKET",
				Value: spec.Storage.Bucket,
			},
			{
				Name:  "INSTALL_EXTENSIONS",
				Value: extensions,
			},
			{
				Name:  "PG_VERSION",
				Value: strconv.Itoa(postgresVersion),
			},
			{
				Name:  "PGDATA_EXTENSIONS",
				Value: "/pgdata/extension/" + strconv.Itoa(postgresVersion),
			},
		},
		EnvFrom: []corev1.EnvFromSource{
			{
				SecretRef: &corev1.SecretEnvSource{
					LocalObjectReference: spec.Storage.Secret.LocalObjectReference,
				},
			},
		},
		VolumeMounts: mounts,
	}

	if openshift == nil || !*openshift {
		container.SecurityContext = &corev1.SecurityContext{
			RunAsUser: func() *int64 {
				uid := int64(26)
				return &uid
			}(),
		}
	}

	return container
}

func ExtensionVolumeMounts(postgresVersion int) []corev1.VolumeMount {
	return []corev1.VolumeMount{
		{
			Name:      "postgres-data",
			MountPath: fmt.Sprintf("/usr/pgsql-%d/share/extension", postgresVersion),
			SubPath:   fmt.Sprintf("extension/%d/usr/pgsql-%[1]d/share/extension", postgresVersion),
		},
		{
			Name:      "postgres-data",
			MountPath: fmt.Sprintf("/usr/pgsql-%d/lib", postgresVersion),
			SubPath:   fmt.Sprintf("extension/%d/usr/pgsql-%[1]d/lib", postgresVersion),
		},
	}
}
