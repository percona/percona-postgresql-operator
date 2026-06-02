// Copyright 2021 - 2026 Percona, LLC
//
// SPDX-License-Identifier: Apache-2.0

package images

// DeepMergeConfigs merges user configuration into base configuration
// User values take precedence over base values at all levels
func DeepMergeConfigs(base, user *DefaultImagesConfig) *DefaultImagesConfig {
	if base == nil {
		return user
	}
	if user == nil {
		return base
	}

	result := &DefaultImagesConfig{
		Registry: pickString(user.Registry, base.Registry),
		Versions: make([]VersionImages, 0),
	}

	// Create map for user versions for quick lookup
	userVersions := make(map[string]*VersionImages)
	for i := range user.Versions {
		userVersions[user.Versions[i].CRVersion] = &user.Versions[i]
	}

	// Process base versions
	for _, baseVer := range base.Versions {
		mergedVer := VersionImages{
			CRVersion:    baseVer.CRVersion,
			Repositories: make(map[string]string),
			Tags:         baseVer.Tags,
		}

		// Copy base repositories
		for k, v := range baseVer.Repositories {
			mergedVer.Repositories[k] = v
		}

		// Merge user overrides if present for this CR version
		if userVer, ok := userVersions[baseVer.CRVersion]; ok {
			// Override repositories
			for k, v := range userVer.Repositories {
				mergedVer.Repositories[k] = v
			}
			mergedVer.Tags = mergeTags(baseVer.Tags, userVer.Tags)
			delete(userVersions, baseVer.CRVersion)
		}

		result.Versions = append(result.Versions, mergedVer)
	}

	// Add new user versions not in base
	for crVer, userVer := range userVersions {
		newVer := VersionImages{
			CRVersion:    crVer,
			Repositories: make(map[string]string),
			Tags:         userVer.Tags,
		}
		for k, v := range userVer.Repositories {
			newVer.Repositories[k] = v
		}
		result.Versions = append(result.Versions, newVer)
	}

	return result
}

// mergeTags merges user tags into base tags
func mergeTags(base, user VersionTags) VersionTags {
	result := base

	// Merge postgres tags map
	if len(user.Postgres) > 0 {
		if result.Postgres == nil {
			result.Postgres = make(map[string]string)
		}
		for k, v := range user.Postgres {
			result.Postgres[k] = v
		}
	}

	// Merge postgresGIS tags map
	if len(user.PostgresGIS) > 0 {
		if result.PostgresGIS == nil {
			result.PostgresGIS = make(map[string]string)
		}
		for k, v := range user.PostgresGIS {
			result.PostgresGIS[k] = v
		}
	}

	// Override single-value tags if non-empty
	result.PGBackRest = pickString(user.PGBackRest, base.PGBackRest)
	result.PGBouncer = pickString(user.PGBouncer, base.PGBouncer)
	result.PGAdmin = pickString(user.PGAdmin, base.PGAdmin)
	result.PGExporter = pickString(user.PGExporter, base.PGExporter)
	result.StandalonePGAdmin = pickString(user.StandalonePGAdmin, base.StandalonePGAdmin)

	return result
}

// pickString returns user value if non-empty, otherwise base value
func pickString(user, base string) string {
	if user != "" {
		return user
	}
	return base
}
