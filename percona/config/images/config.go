// Copyright 2021 - 2026 Percona, LLC
//
// SPDX-License-Identifier: Apache-2.0

package images

import (
	_ "embed"

	"github.com/pkg/errors"
	"sigs.k8s.io/yaml"
)

//go:embed default-images.yaml
var defaultImagesYAML []byte

// DefaultConfig holds the default image configuration loaded from embedded YAML
var DefaultConfig *DefaultImagesConfig

// mustLoadDefault parses the embedded default-images.yaml and returns the configuration
func mustLoadDefault() *DefaultImagesConfig {
	var cfg DefaultImagesConfig
	if err := yaml.Unmarshal(defaultImagesYAML, &cfg); err != nil {
		panic(errors.Wrap(err, "failed to parse embedded default images"))
	}
	return &cfg
}

func init() {
	DefaultConfig = mustLoadDefault()
}
