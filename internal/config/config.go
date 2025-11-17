// Copyright 2021 - 2024 Crunchy Data Solutions, Inc.
//
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"fmt"
	"os"

	"github.com/percona/percona-postgresql-operator/v2/pkg/apis/postgres-operator.crunchydata.com/v1beta1"
)

// defaultFromEnv reads the environment variable key when value is empty.
func defaultFromEnv(value, key string) string {
	if value == "" {
		return os.Getenv(key)
	}
	return value
}

// FetchKeyCommand returns the fetch_key_cmd value stored in the encryption_key_command
// variable used to enable TDE.
func FetchKeyCommand(spec *v1beta1.PostgresClusterSpec) string {
	if spec.Patroni != nil {
		if spec.Patroni.DynamicConfiguration != nil {
			configuration := spec.Patroni.DynamicConfiguration
			if configuration != nil {
				if postgresql, ok := configuration["postgresql"].(map[string]any); ok {
					if parameters, ok := postgresql["parameters"].(map[string]any); ok {
						if parameters["encryption_key_command"] != nil {
							return fmt.Sprintf("%s", parameters["encryption_key_command"])
						}
					}
				}
			}
		}
	}
	return ""
}

// Red Hat Marketplace requires operators to use environment variables be used
// for any image other than the operator itself. Those variables must start with
// "RELATED_IMAGE_" so that OSBS can transform their tag values into digests
// for a "disconnected" OLM CSV.

// - https://redhat-connect.gitbook.io/certified-operator-guide/troubleshooting-and-resources/offline-enabled-operators
// - https://osbs.readthedocs.io/en/latest/users.html#pullspec-locations

// PGBackRestContainerImage returns the container image to use for pgBackRest.
func PGBackRestContainerImage(cluster *v1beta1.PostgresCluster) string {
	image := cluster.Spec.Backups.PGBackRest.Image

	return defaultFromEnv(image, "RELATED_IMAGE_PGBACKREST")
}

// PGAdminContainerImage returns the container image to use for pgAdmin.
func PGAdminContainerImage(cluster *v1beta1.PostgresCluster) string {
	var image string
	if cluster.Spec.UserInterface != nil &&
		cluster.Spec.UserInterface.PGAdmin != nil {
		image = cluster.Spec.UserInterface.PGAdmin.Image
	}

	return defaultFromEnv(image, "RELATED_IMAGE_PGADMIN")
}

// StandalonePGAdminContainerImage returns the container image to use for pgAdmin.
func StandalonePGAdminContainerImage(pgadmin *v1beta1.PGAdmin) string {
	var image string
	if pgadmin.Spec.Image != nil {
		image = *pgadmin.Spec.Image
	}

	return defaultFromEnv(image, "RELATED_IMAGE_STANDALONE_PGADMIN")
}

// PGBouncerContainerImage returns the container image to use for pgBouncer.
func PGBouncerContainerImage(cluster *v1beta1.PostgresCluster) string {
	var image string
	if cluster.Spec.Proxy != nil &&
		cluster.Spec.Proxy.PGBouncer != nil {
		image = cluster.Spec.Proxy.PGBouncer.Image
	}

	return defaultFromEnv(image, "RELATED_IMAGE_PGBOUNCER")
}

// PGExporterContainerImage returns the container image to use for the
// PostgreSQL Exporter.
func PGExporterContainerImage(cluster *v1beta1.PostgresCluster) string {
	var image string
	if cluster.Spec.Monitoring != nil &&
		cluster.Spec.Monitoring.PGMonitor != nil &&
		cluster.Spec.Monitoring.PGMonitor.Exporter != nil {
		image = cluster.Spec.Monitoring.PGMonitor.Exporter.Image
	}

	return defaultFromEnv(image, "RELATED_IMAGE_PGEXPORTER")
}

// PostgresContainerImageString returns the container image to use for PostgreSQL (from string params).
// This func copies logic from original PostgresContainerImage as is, leaving PostgresContainerImage as a wrapper for upstream compatibility
func PostgresContainerImageString(image string, postgresVersion int, postGISVersion string) string {
	key := "RELATED_IMAGE_POSTGRES_" + fmt.Sprint(postgresVersion)

	if postGISVersion != "" {
		key += "_GIS_" + postGISVersion
	}

	return defaultFromEnv(image, key)
}

// PostgresContainerImage returns the container image to use for PostgreSQL.
// Made as a wrapper of PostgresContainerImageString for compat reasons
func PostgresContainerImage(cluster *v1beta1.PostgresCluster) string {
	image := cluster.Spec.Image
	postgresVersion := cluster.Spec.PostgresVersion
	postGISVersion := cluster.Spec.PostGISVersion

	return PostgresContainerImageString(image, postgresVersion, postGISVersion)
}

// PGONamespace returns the namespace where the PGO is running,
// based on the env var from the DownwardAPI
// If no env var is found, returns ""
func PGONamespace() string {
	return os.Getenv("PGO_NAMESPACE")
}

// VerifyImageValues checks that all container images required by the
// spec are defined. If any are undefined, a list is returned in an error.
func VerifyImageValues(cluster *v1beta1.PostgresCluster) error {

	var images []string

	// K8SPG-710: Image check will fail without a backup section in PostgresCluster
	if cluster.BackupSpecFound() && PGBackRestContainerImage(cluster) == "" {
		images = append(images, "crunchy-pgbackrest")
	}
	if PGAdminContainerImage(cluster) == "" &&
		cluster.Spec.UserInterface != nil &&
		cluster.Spec.UserInterface.PGAdmin != nil {
		images = append(images, "crunchy-pgadmin4")
	}
	if PGBouncerContainerImage(cluster) == "" &&
		cluster.Spec.Proxy != nil &&
		cluster.Spec.Proxy.PGBouncer != nil {
		images = append(images, "crunchy-pgbouncer")
	}
	if PGExporterContainerImage(cluster) == "" &&
		cluster.Spec.Monitoring != nil &&
		cluster.Spec.Monitoring.PGMonitor != nil &&
		cluster.Spec.Monitoring.PGMonitor.Exporter != nil {
		images = append(images, "crunchy-postgres-exporter")
	}
	if PostgresContainerImage(cluster) == "" {
		if cluster.Spec.PostGISVersion != "" {
			images = append(images, "crunchy-postgres-gis")
		} else {
			images = append(images, "crunchy-postgres")
		}
	}

	if len(images) > 0 {
		return fmt.Errorf("Missing image(s): %s", images)
	}

	return nil
}
