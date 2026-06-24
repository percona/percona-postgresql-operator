// Copyright 2021 - 2026 Percona, LLC
//
// SPDX-License-Identifier: Apache-2.0

package images

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDefaultImagesConfig_GetImage(t *testing.T) {
	cfg := &DefaultImagesConfig{
		Registry: "docker.io",
		Versions: []VersionImages{
			{
				CRVersion: "2.9.0",
				Repositories: map[string]string{
					"postgres":    "percona/percona-postgresql",
					"pgbackrest":  "percona/pgbackrest",
					"pgbouncer":   "percona/pgbouncer",
					"pgadmin":     "percona/pgadmin4",
					"postgresGIS": "percona/percona-postgresql-gis",
				},
				Tags: VersionTags{
					Postgres: map[string]string{
						"14": "14.10-1",
						"15": "15.5-1",
					},
					PostgresGIS: map[string]string{
						"14": "14.10-3.3-1",
					},
					PGBackRest: "2.9.0",
					PGBouncer:  "2.9.0",
					PGAdmin:    "2.9.0",
				},
			},
		},
	}

	tests := []struct {
		name      string
		crVersion string
		component string
		pgVersion string
		expected  string
	}{
		{
			name:      "PostgreSQL 14",
			crVersion: "2.9.0",
			component: "postgres",
			pgVersion: "14",
			expected:  "docker.io/percona/percona-postgresql:14.10-1",
		},
		{
			name:      "PostgreSQL 15",
			crVersion: "2.9.0",
			component: "postgres",
			pgVersion: "15",
			expected:  "docker.io/percona/percona-postgresql:15.5-1",
		},
		{
			name:      "PostGIS 14",
			crVersion: "2.9.0",
			component: "postgresGIS",
			pgVersion: "14",
			expected:  "docker.io/percona/percona-postgresql-gis:14.10-3.3-1",
		},
		{
			name:      "pgBackRest",
			crVersion: "2.9.0",
			component: "pgbackrest",
			pgVersion: "",
			expected:  "docker.io/percona/pgbackrest:2.9.0",
		},
		{
			name:      "pgBouncer",
			crVersion: "2.9.0",
			component: "pgbouncer",
			pgVersion: "",
			expected:  "docker.io/percona/pgbouncer:2.9.0",
		},
		{
			name:      "pgAdmin",
			crVersion: "2.9.0",
			component: "pgadmin",
			pgVersion: "",
			expected:  "docker.io/percona/pgadmin4:2.9.0",
		},
		{
			name:      "unknown CR version",
			crVersion: "9.9.9",
			component: "postgres",
			pgVersion: "14",
			expected:  "",
		},
		{
			name:      "unknown PostgreSQL version",
			crVersion: "2.9.0",
			component: "postgres",
			pgVersion: "99",
			expected:  "",
		},
		{
			name:      "unknown component",
			crVersion: "2.9.0",
			component: "unknown",
			pgVersion: "14",
			expected:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := cfg.GetImage(tt.crVersion, tt.component, tt.pgVersion)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestDefaultImagesConfig_GetVersionConfig(t *testing.T) {
	cfg := &DefaultImagesConfig{
		Registry: "docker.io",
		Versions: []VersionImages{
			{
				CRVersion: "2.9.0",
				Repositories: map[string]string{
					"postgres": "percona/postgres",
				},
				Tags: VersionTags{
					Postgres: map[string]string{"14": "1.0"},
				},
			},
			{
				CRVersion: "2.10.0",
				Repositories: map[string]string{
					"postgres": "percona/postgres",
				},
				Tags: VersionTags{
					Postgres: map[string]string{"14": "2.0"},
				},
			},
		},
	}

	t.Run("found", func(t *testing.T) {
		result := cfg.getVersionConfig("2.9.0")
		assert.NotNil(t, result)
		assert.Equal(t, "2.9.0", result.CRVersion)
	})

	t.Run("not found", func(t *testing.T) {
		result := cfg.getVersionConfig("9.9.9")
		assert.Nil(t, result)
	})
}

func TestGlobalConfig(t *testing.T) {
	t.Run("SetGlobalConfig is idempotent", func(t *testing.T) {
		// This test verifies that SetGlobalConfig can only be called once
		// The first call sets the config, subsequent calls are ignored
		firstCfg := &DefaultImagesConfig{
			Registry: "first.io",
		}
		secondCfg := &DefaultImagesConfig{
			Registry: "second.io",
		}

		// First call should set the config
		SetGlobalConfig(firstCfg)
		result := GetGlobalConfig()
		assert.Equal(t, "first.io", result.Registry)

		// Second call should be ignored (sync.Once behavior)
		SetGlobalConfig(secondCfg)
		result = GetGlobalConfig()
		assert.Equal(t, "first.io", result.Registry, "second SetGlobalConfig should be ignored")
	})
}
