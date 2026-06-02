// Copyright 2021 - 2026 Percona, LLC
//
// SPDX-License-Identifier: Apache-2.0

package images

import (
	_ "embed"
	"fmt"

	"gopkg.in/yaml.v3"
)

//go:embed default-images.yaml
var defaultImagesYAML []byte

// DefaultConfig holds the default image configuration loaded from embedded YAML
var DefaultConfig *DefaultImagesConfig

// mustLoadDefault parses the embedded default-images.yaml and returns the configuration
func mustLoadDefault() *DefaultImagesConfig {
	var cfg DefaultImagesConfig
	if err := yaml.Unmarshal(defaultImagesYAML, &cfg); err != nil {
		panic(fmt.Sprintf("failed to parse embedded default images: %v", err))
	}
	return &cfg
}

func init() {
	DefaultConfig = mustLoadDefault()
}
