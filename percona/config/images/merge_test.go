// Copyright 2021 - 2026 Percona, LLC
//
// SPDX-License-Identifier: Apache-2.0

package images

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDeepMergeConfigs(t *testing.T) {
	t.Run("user is nil", func(t *testing.T) {
		base := &DefaultImagesConfig{Registry: "docker.io"}
		result := DeepMergeConfigs(base, nil)
		assert.Equal(t, base, result)
	})

	t.Run("base is nil", func(t *testing.T) {
		user := &DefaultImagesConfig{Registry: "quay.io"}
		result := DeepMergeConfigs(nil, user)
		assert.Equal(t, user, result)
	})

	t.Run("both nil", func(t *testing.T) {
		result := DeepMergeConfigs(nil, nil)
		assert.Nil(t, result)
	})

	t.Run("registry override", func(t *testing.T) {
		base := &DefaultImagesConfig{Registry: "docker.io"}
		user := &DefaultImagesConfig{Registry: "quay.io"}
		result := DeepMergeConfigs(base, user)
		assert.Equal(t, "quay.io", result.Registry)
	})

	t.Run("registry from base when user empty", func(t *testing.T) {
		base := &DefaultImagesConfig{Registry: "docker.io"}
		user := &DefaultImagesConfig{Registry: ""}
		result := DeepMergeConfigs(base, user)
		assert.Equal(t, "docker.io", result.Registry)
	})

	t.Run("merge versions - override existing", func(t *testing.T) {
		base := &DefaultImagesConfig{
			Registry: "docker.io",
			Versions: []VersionImages{
				{
					CRVersion: "2.9.0",
					Repositories: map[string]string{
						"postgres":   "percona/postgres",
						"pgbackrest": "percona/pgbackrest",
					},
					Tags: VersionTags{
						Postgres:   map[string]string{"14": "1.0", "15": "2.0"},
						PGBackRest: "2.9.0",
					},
				},
			},
		}

		user := &DefaultImagesConfig{
			Registry: "",
			Versions: []VersionImages{
				{
					CRVersion: "2.9.0",
					Repositories: map[string]string{
						"postgres": "custom/postgres",
					},
					Tags: VersionTags{
						Postgres:   map[string]string{"14": "1.1"},
						PGBackRest: "2.9.1",
					},
				},
			},
		}

		result := DeepMergeConfigs(base, user)
		assert.Equal(t, 1, len(result.Versions))
		assert.Equal(t, "2.9.0", result.Versions[0].CRVersion)
		assert.Equal(t, "custom/postgres", result.Versions[0].Repositories["postgres"])
		assert.Equal(t, "percona/pgbackrest", result.Versions[0].Repositories["pgbackrest"]) // from base
		assert.Equal(t, "1.1", result.Versions[0].Tags.Postgres["14"])
		assert.Equal(t, "2.0", result.Versions[0].Tags.Postgres["15"]) // from base
		assert.Equal(t, "2.9.1", result.Versions[0].Tags.PGBackRest)
	})

	t.Run("merge versions - add new version", func(t *testing.T) {
		base := &DefaultImagesConfig{
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
			},
		}

		user := &DefaultImagesConfig{
			Registry: "",
			Versions: []VersionImages{
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

		result := DeepMergeConfigs(base, user)
		assert.Equal(t, 2, len(result.Versions))
		// Check that both versions exist
		has29 := false
		has210 := false
		for _, v := range result.Versions {
			if v.CRVersion == "2.9.0" {
				has29 = true
			}
			if v.CRVersion == "2.10.0" {
				has210 = true
			}
		}
		assert.True(t, has29)
		assert.True(t, has210)
	})

	t.Run("merge postgres tags map", func(t *testing.T) {
		base := &DefaultImagesConfig{
			Versions: []VersionImages{
				{
					CRVersion: "2.9.0",
					Repositories: map[string]string{
						"postgres": "percona/postgres",
					},
					Tags: VersionTags{
						Postgres: map[string]string{"14": "1.0", "15": "2.0"},
					},
				},
			},
		}

		user := &DefaultImagesConfig{
			Versions: []VersionImages{
				{
					CRVersion: "2.9.0",
					Repositories: map[string]string{
						"postgres": "percona/postgres",
					},
					Tags: VersionTags{
						Postgres: map[string]string{"16": "3.0"},
					},
				},
			},
		}

		result := DeepMergeConfigs(base, user)
		assert.Equal(t, 3, len(result.Versions[0].Tags.Postgres))
		assert.Equal(t, "1.0", result.Versions[0].Tags.Postgres["14"])
		assert.Equal(t, "2.0", result.Versions[0].Tags.Postgres["15"])
		assert.Equal(t, "3.0", result.Versions[0].Tags.Postgres["16"])
	})

	t.Run("merge postgresGIS tags map", func(t *testing.T) {
		base := &DefaultImagesConfig{
			Versions: []VersionImages{
				{
					CRVersion: "2.9.0",
					Repositories: map[string]string{
						"postgresGIS": "percona/postgis",
					},
					Tags: VersionTags{
						PostgresGIS: map[string]string{"14-gis-3.3": "1.0"},
					},
				},
			},
		}

		user := &DefaultImagesConfig{
			Versions: []VersionImages{
				{
					CRVersion: "2.9.0",
					Repositories: map[string]string{
						"postgresGIS": "percona/postgis",
					},
					Tags: VersionTags{
						PostgresGIS: map[string]string{"15-gis-3.3": "2.0"},
					},
				},
			},
		}

		result := DeepMergeConfigs(base, user)
		assert.Equal(t, 2, len(result.Versions[0].Tags.PostgresGIS))
		assert.Equal(t, "1.0", result.Versions[0].Tags.PostgresGIS["14-gis-3.3"])
		assert.Equal(t, "2.0", result.Versions[0].Tags.PostgresGIS["15-gis-3.3"])
	})
}

func TestMergeTags(t *testing.T) {
	t.Run("merge single value tags", func(t *testing.T) {
		base := VersionTags{
			PGBackRest: "2.9.0",
			PGBouncer:  "2.9.0",
			PGAdmin:    "2.9.0",
		}

		user := VersionTags{
			PGBackRest: "2.10.0",
			PGAdmin:    "2.10.0",
		}

		result := mergeTags(base, user)
		assert.Equal(t, "2.10.0", result.PGBackRest)
		assert.Equal(t, "2.9.0", result.PGBouncer) // from base
		assert.Equal(t, "2.10.0", result.PGAdmin)
	})

	t.Run("empty user values don't override", func(t *testing.T) {
		base := VersionTags{
			PGBackRest: "2.9.0",
		}

		user := VersionTags{
			PGBackRest: "",
		}

		result := mergeTags(base, user)
		assert.Equal(t, "2.9.0", result.PGBackRest)
	})

	t.Run("does not mutate base maps", func(t *testing.T) {
		base := VersionTags{
			Postgres: map[string]string{"14": "1.0", "15": "2.0"},
		}

		user := VersionTags{
			Postgres: map[string]string{"16": "3.0"},
		}

		// Capture original base map state
		originalBasePostgres := make(map[string]string)
		for k, v := range base.Postgres {
			originalBasePostgres[k] = v
		}

		result := mergeTags(base, user)

		// Verify result has merged data
		assert.Equal(t, 3, len(result.Postgres))
		assert.Equal(t, "3.0", result.Postgres["16"])

		// Verify base was NOT mutated
		assert.Equal(t, 2, len(base.Postgres), "base map should not have new keys added")
		assert.Equal(t, originalBasePostgres, base.Postgres, "base map should be unchanged")
	})
}

func TestPickString(t *testing.T) {
	t.Run("user non-empty", func(t *testing.T) {
		result := pickString("user", "base")
		assert.Equal(t, "user", result)
	})

	t.Run("user empty", func(t *testing.T) {
		result := pickString("", "base")
		assert.Equal(t, "base", result)
	})
}
