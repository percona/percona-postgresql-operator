// Copyright 2021 - 2026 Percona, LLC
//
// SPDX-License-Identifier: Apache-2.0

package images

// VersionImages defines image configuration for a specific CR version
type VersionImages struct {
	CRVersion    string            `json:"crVersion"`
	Repositories map[string]string `json:"repositories"`
	Tags         VersionTags       `json:"tags"`
}

// VersionTags separates PostgreSQL tags (per version) from component tags (single)
type VersionTags struct {
	Postgres          map[string]string `json:"postgres"`
	PostgresGIS       map[string]string `json:"postgresGIS"`
	PGBackRest        string            `json:"pgbackrest"`
	PGBouncer         string            `json:"pgbouncer"`
	PGAdmin           string            `json:"pgadmin"`
	PGExporter        string            `json:"pgexporter"`
	StandalonePGAdmin string            `json:"standalonePGAdmin"`
}

// DefaultImagesConfig is the root configuration structure
type DefaultImagesConfig struct {
	// Global registry for all images (e.g., "docker.io", "quay.io")
	Registry string          `json:"registry"`
	Versions []VersionImages `json:"versions"`
}

// GetImage builds full image reference: registry/repository:tag
func (c *DefaultImagesConfig) GetImage(crVersion, component, pgVersion string) string {
	versionConfig := c.getVersionConfig(crVersion)
	if versionConfig == nil {
		return ""
	}

	repo := versionConfig.Repositories[component]
	if repo == "" {
		return ""
	}

	tag := c.getTag(versionConfig, component, pgVersion)
	if tag == "" {
		return ""
	}

	return c.buildImageRef(repo, tag)
}

// getVersionConfig returns the VersionImages for a specific CR version
func (c *DefaultImagesConfig) getVersionConfig(crVersion string) *VersionImages {
	for i := range c.Versions {
		if c.Versions[i].CRVersion == crVersion {
			return &c.Versions[i]
		}
	}
	return nil
}

// getTag returns the tag for a specific component and PostgreSQL version
func (c *DefaultImagesConfig) getTag(v *VersionImages, component, pgVersion string) string {
	switch component {
	case "postgres":
		if v.Tags.Postgres != nil {
			return v.Tags.Postgres[pgVersion]
		}
	case "postgresGIS":
		if v.Tags.PostgresGIS != nil {
			return v.Tags.PostgresGIS[pgVersion]
		}
	case "pgbackrest":
		return v.Tags.PGBackRest
	case "pgbouncer":
		return v.Tags.PGBouncer
	case "pgadmin":
		return v.Tags.PGAdmin
	case "pgexporter":
		return v.Tags.PGExporter
	case "standalonePGAdmin":
		return v.Tags.StandalonePGAdmin
	}
	return ""
}

// buildImageRef constructs the full image reference from repository and tag
func (c *DefaultImagesConfig) buildImageRef(repository, tag string) string {
	if repository == "" || tag == "" {
		return ""
	}

	image := repository
	if c.Registry != "" {
		image = c.Registry + "/" + repository
	}

	return image + ":" + tag
}

// Global config accessor
var globalConfig *DefaultImagesConfig

// SetGlobalConfig sets the global image configuration
func SetGlobalConfig(config *DefaultImagesConfig) {
	globalConfig = config
}

// GetGlobalConfig returns the global image configuration
func GetGlobalConfig() *DefaultImagesConfig {
	return globalConfig
}

// GetImageForCluster is a convenience function to get image for a specific CR version and component
func GetImageForCluster(crVersion, component, pgVersion string) string {
	if globalConfig == nil {
		return ""
	}
	return globalConfig.GetImage(crVersion, component, pgVersion)
}
