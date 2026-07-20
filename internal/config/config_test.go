// Copyright 2021 - 2024 Crunchy Data Solutions, Inc.
//
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"os"
	"strings"
	"testing"

	"gotest.tools/v3/assert"
	"sigs.k8s.io/yaml"

	"github.com/percona/percona-postgresql-operator/v3/pkg/apis/upstream.pgv2.percona.com/v1beta1"
)

func TestFetchKeyCommand(t *testing.T) {

	spec1 := v1beta1.PostgresClusterSpec{}
	assert.Assert(t, FetchKeyCommand(&spec1) == "")

	spec2 := v1beta1.PostgresClusterSpec{
		Patroni: &v1beta1.PatroniSpec{},
	}
	assert.Assert(t, FetchKeyCommand(&spec2) == "")

	spec3 := v1beta1.PostgresClusterSpec{
		Patroni: &v1beta1.PatroniSpec{
			DynamicConfiguration: map[string]any{},
		},
	}
	assert.Assert(t, FetchKeyCommand(&spec3) == "")

	spec4 := v1beta1.PostgresClusterSpec{
		Patroni: &v1beta1.PatroniSpec{
			DynamicConfiguration: map[string]any{
				"postgresql": map[string]any{},
			},
		},
	}
	assert.Assert(t, FetchKeyCommand(&spec4) == "")

	spec5 := v1beta1.PostgresClusterSpec{
		Patroni: &v1beta1.PatroniSpec{
			DynamicConfiguration: map[string]any{
				"postgresql": map[string]any{
					"parameters": map[string]any{},
				},
			},
		},
	}
	assert.Assert(t, FetchKeyCommand(&spec5) == "")

	spec6 := v1beta1.PostgresClusterSpec{
		Patroni: &v1beta1.PatroniSpec{
			DynamicConfiguration: map[string]any{
				"postgresql": map[string]any{
					"parameters": map[string]any{
						"encryption_key_command": "",
					},
				},
			},
		},
	}
	assert.Assert(t, FetchKeyCommand(&spec6) == "")

	spec7 := v1beta1.PostgresClusterSpec{
		Patroni: &v1beta1.PatroniSpec{
			DynamicConfiguration: map[string]any{
				"postgresql": map[string]any{
					"parameters": map[string]any{
						"encryption_key_command": "echo mykey",
					},
				},
			},
		},
	}
	assert.Assert(t, FetchKeyCommand(&spec7) == "echo mykey")

}

func TestPGAdminContainerImage(t *testing.T) {
	cluster := &v1beta1.PostgresCluster{}

	t.Setenv("RELATED_IMAGE_PGADMIN", "")
	os.Unsetenv("RELATED_IMAGE_PGADMIN")
	assert.Equal(t, PGAdminContainerImage(cluster), "")

	t.Setenv("RELATED_IMAGE_PGADMIN", "")
	assert.Equal(t, PGAdminContainerImage(cluster), "")

	t.Setenv("RELATED_IMAGE_PGADMIN", "env-var-pgadmin")
	assert.Equal(t, PGAdminContainerImage(cluster), "env-var-pgadmin")

	assert.NilError(t, yaml.Unmarshal([]byte(`{
		userInterface: { pgAdmin: { image: spec-image } },
	}`), &cluster.Spec))
	assert.Equal(t, PGAdminContainerImage(cluster), "spec-image")
}

func TestPGBackRestContainerImage(t *testing.T) {
	cluster := &v1beta1.PostgresCluster{}

	t.Setenv("RELATED_IMAGE_PGBACKREST", "")
	os.Unsetenv("RELATED_IMAGE_PGBACKREST")
	assert.Equal(t, PGBackRestContainerImage(cluster), "")

	t.Setenv("RELATED_IMAGE_PGBACKREST", "")
	assert.Equal(t, PGBackRestContainerImage(cluster), "")

	t.Setenv("RELATED_IMAGE_PGBACKREST", "env-var-pgbackrest")
	assert.Equal(t, PGBackRestContainerImage(cluster), "env-var-pgbackrest")

	assert.NilError(t, yaml.Unmarshal([]byte(`{
		backups: { pgBackRest: { image: spec-image } },
	}`), &cluster.Spec))
	assert.Equal(t, PGBackRestContainerImage(cluster), "spec-image")
}

func TestPGBouncerContainerImage(t *testing.T) {
	cluster := &v1beta1.PostgresCluster{}

	t.Setenv("RELATED_IMAGE_PGBOUNCER", "")
	os.Unsetenv("RELATED_IMAGE_PGBOUNCER")
	assert.Equal(t, PGBouncerContainerImage(cluster), "")

	t.Setenv("RELATED_IMAGE_PGBOUNCER", "")
	assert.Equal(t, PGBouncerContainerImage(cluster), "")

	t.Setenv("RELATED_IMAGE_PGBOUNCER", "env-var-pgbouncer")
	assert.Equal(t, PGBouncerContainerImage(cluster), "env-var-pgbouncer")

	assert.NilError(t, yaml.Unmarshal([]byte(`{
		proxy: { pgBouncer: { image: spec-image } },
	}`), &cluster.Spec))
	assert.Equal(t, PGBouncerContainerImage(cluster), "spec-image")
}

func TestPGExporterContainerImage(t *testing.T) {
	cluster := &v1beta1.PostgresCluster{}

	t.Setenv("RELATED_IMAGE_PGEXPORTER", "")
	os.Unsetenv("RELATED_IMAGE_PGEXPORTER")
	assert.Equal(t, PGExporterContainerImage(cluster), "")

	t.Setenv("RELATED_IMAGE_PGEXPORTER", "")
	assert.Equal(t, PGExporterContainerImage(cluster), "")

	t.Setenv("RELATED_IMAGE_PGEXPORTER", "env-var-pgexporter")
	assert.Equal(t, PGExporterContainerImage(cluster), "env-var-pgexporter")

	assert.NilError(t, yaml.Unmarshal([]byte(`{
		monitoring: { pgMonitor: { exporter: { image: spec-image } } },
	}`), &cluster.Spec))
	assert.Equal(t, PGExporterContainerImage(cluster), "spec-image")
}

func TestStandalonePGAdminContainerImage(t *testing.T) {
	pgadmin := &v1beta1.PGAdmin{}

	t.Setenv("RELATED_IMAGE_STANDALONE_PGADMIN", "")
	os.Unsetenv("RELATED_IMAGE_STANDALONE_PGADMIN")
	assert.Equal(t, StandalonePGAdminContainerImage(pgadmin), "")

	t.Setenv("RELATED_IMAGE_STANDALONE_PGADMIN", "")
	assert.Equal(t, StandalonePGAdminContainerImage(pgadmin), "")

	t.Setenv("RELATED_IMAGE_STANDALONE_PGADMIN", "env-var-pgadmin")
	assert.Equal(t, StandalonePGAdminContainerImage(pgadmin), "env-var-pgadmin")

	assert.NilError(t, yaml.Unmarshal([]byte(`{
		image: spec-image
	}`), &pgadmin.Spec))
	assert.Equal(t, StandalonePGAdminContainerImage(pgadmin), "spec-image")
}

func TestPostgresContainerImage(t *testing.T) {
	cluster := &v1beta1.PostgresCluster{}
	cluster.Spec.PostgresVersion = 12

	t.Setenv("RELATED_IMAGE_POSTGRES_12", "")
	os.Unsetenv("RELATED_IMAGE_POSTGRES_12")
	assert.Equal(t, PostgresContainerImage(cluster), "")

	t.Setenv("RELATED_IMAGE_POSTGRES_12", "")
	assert.Equal(t, PostgresContainerImage(cluster), "")

	t.Setenv("RELATED_IMAGE_POSTGRES_12", "env-var-postgres")
	assert.Equal(t, PostgresContainerImage(cluster), "env-var-postgres")

	cluster.Spec.Image = "spec-image"
	assert.Equal(t, PostgresContainerImage(cluster), "spec-image")

	cluster.Spec.Image = ""
	cluster.Spec.PostGISVersion = "3.0"
	t.Setenv("RELATED_IMAGE_POSTGRES_12_GIS_3.0", "env-var-postgis")
	assert.Equal(t, PostgresContainerImage(cluster), "env-var-postgis")

	cluster.Spec.Image = "spec-image"
	assert.Equal(t, PostgresContainerImage(cluster), "spec-image")
}

func TestVerifyImageValues(t *testing.T) {
	t.Run("postgres", func(t *testing.T) {
		cluster := &v1beta1.PostgresCluster{}
		cluster.Spec.PostgresVersion = 14
		t.Setenv("RELATED_IMAGE_POSTGRES_14", "")
		os.Unsetenv("RELATED_IMAGE_POSTGRES_14")

		err := VerifyImageValues(cluster)
		assert.ErrorContains(t, err, "postgres")
	})

	t.Run("postgres-gis", func(t *testing.T) {
		cluster := &v1beta1.PostgresCluster{}
		cluster.Spec.PostgresVersion = 14
		cluster.Spec.PostGISVersion = "3.3"
		t.Setenv("RELATED_IMAGE_POSTGRES_14_GIS_3.3", "")
		os.Unsetenv("RELATED_IMAGE_POSTGRES_14_GIS_3.3")

		err := VerifyImageValues(cluster)
		assert.ErrorContains(t, err, "postgres-gis")
	})

	t.Run("pgbackrest-enabled", func(t *testing.T) {
		cluster := &v1beta1.PostgresCluster{}
		cluster.Spec.PostgresVersion = 14
		enabled := true
		cluster.Spec.Backups.Enabled = &enabled
		t.Setenv("RELATED_IMAGE_PGBACKREST", "")
		os.Unsetenv("RELATED_IMAGE_PGBACKREST")

		err := VerifyImageValues(cluster)
		assert.ErrorContains(t, err, "pgbackrest")
	})

	t.Run("pgbackrest-disabled", func(t *testing.T) {
		cluster := &v1beta1.PostgresCluster{}
		cluster.Spec.PostgresVersion = 14
		enabled := false
		cluster.Spec.Backups.Enabled = &enabled
		t.Setenv("RELATED_IMAGE_PGBACKREST", "")
		os.Unsetenv("RELATED_IMAGE_PGBACKREST")

		err := VerifyImageValues(cluster)
		assert.Assert(t, !strings.Contains(err.Error(), "pgbackrest"))
	})

	t.Run("pgbouncer", func(t *testing.T) {
		cluster := &v1beta1.PostgresCluster{}
		cluster.Spec.PostgresVersion = 14
		cluster.Spec.Proxy = &v1beta1.PostgresProxySpec{
			PGBouncer: &v1beta1.PGBouncerPodSpec{},
		}
		t.Setenv("RELATED_IMAGE_PGBOUNCER", "")
		os.Unsetenv("RELATED_IMAGE_PGBOUNCER")

		err := VerifyImageValues(cluster)
		assert.ErrorContains(t, err, "pgbouncer")
	})

	t.Run("pgadmin4", func(t *testing.T) {
		cluster := &v1beta1.PostgresCluster{}
		cluster.Spec.PostgresVersion = 14
		cluster.Spec.UserInterface = &v1beta1.UserInterfaceSpec{
			PGAdmin: &v1beta1.PGAdminPodSpec{},
		}
		t.Setenv("RELATED_IMAGE_PGADMIN", "")
		os.Unsetenv("RELATED_IMAGE_PGADMIN")

		err := VerifyImageValues(cluster)
		assert.ErrorContains(t, err, "pgadmin4")
	})

	t.Run("postgres-exporter", func(t *testing.T) {
		cluster := &v1beta1.PostgresCluster{}
		cluster.Spec.PostgresVersion = 14
		cluster.Spec.Monitoring = &v1beta1.MonitoringSpec{
			PGMonitor: &v1beta1.PGMonitorSpec{
				Exporter: &v1beta1.ExporterSpec{},
			},
		}
		t.Setenv("RELATED_IMAGE_PGEXPORTER", "")
		os.Unsetenv("RELATED_IMAGE_PGEXPORTER")

		err := VerifyImageValues(cluster)
		assert.ErrorContains(t, err, "postgres-exporter")
	})

	t.Run("multiple missing images", func(t *testing.T) {
		enabled := true
		cluster := &v1beta1.PostgresCluster{}
		cluster.Spec.PostgresVersion = 14
		cluster.Spec.PostGISVersion = "3.3"
		cluster.Spec.Backups.Enabled = &enabled
		cluster.Spec.Proxy = &v1beta1.PostgresProxySpec{
			PGBouncer: &v1beta1.PGBouncerPodSpec{},
		}
		cluster.Spec.UserInterface = &v1beta1.UserInterfaceSpec{
			PGAdmin: &v1beta1.PGAdminPodSpec{},
		}
		cluster.Spec.Monitoring = &v1beta1.MonitoringSpec{
			PGMonitor: &v1beta1.PGMonitorSpec{
				Exporter: &v1beta1.ExporterSpec{},
			},
		}

		err := VerifyImageValues(cluster)
		assert.ErrorContains(t, err, "postgres-gis")
		assert.ErrorContains(t, err, "pgbackrest")
		assert.ErrorContains(t, err, "pgbouncer")
		assert.ErrorContains(t, err, "pgadmin4")
		assert.ErrorContains(t, err, "postgres-exporter")
	})

	t.Run("all images set", func(t *testing.T) {
		enabled := true
		cluster := &v1beta1.PostgresCluster{}
		cluster.Spec.PostgresVersion = 14
		cluster.Spec.PostGISVersion = "3.3"
		cluster.Spec.Backups.Enabled = &enabled
		cluster.Spec.Proxy = &v1beta1.PostgresProxySpec{
			PGBouncer: &v1beta1.PGBouncerPodSpec{},
		}
		cluster.Spec.UserInterface = &v1beta1.UserInterfaceSpec{
			PGAdmin: &v1beta1.PGAdminPodSpec{},
		}
		cluster.Spec.Monitoring = &v1beta1.MonitoringSpec{
			PGMonitor: &v1beta1.PGMonitorSpec{
				Exporter: &v1beta1.ExporterSpec{},
			},
		}

		t.Setenv("RELATED_IMAGE_POSTGRES_14_GIS_3.3", "img")
		t.Setenv("RELATED_IMAGE_PGBACKREST", "img")
		t.Setenv("RELATED_IMAGE_PGBOUNCER", "img")
		t.Setenv("RELATED_IMAGE_PGADMIN", "img")
		t.Setenv("RELATED_IMAGE_PGEXPORTER", "img")

		assert.NilError(t, VerifyImageValues(cluster))
	})
}
