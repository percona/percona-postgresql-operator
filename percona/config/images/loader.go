// Copyright 2021 - 2026 Percona, LLC
//
// SPDX-License-Identifier: Apache-2.0

package images

import (
	"context"
	"os"
	"path/filepath"

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"
)

const (
	// DefaultConfigMapName is the default name of the ConfigMap containing image configuration
	DefaultConfigMapName = "percona-pg-image-config"
	// DefaultConfigMapKey is the key in the ConfigMap data containing the YAML configuration
	DefaultConfigMapKey = "images.yaml"

	// EnvImageConfigPath environment variable for custom config file path
	EnvImageConfigPath = "PERCONA_PG_IMAGE_CONFIG"
	// EnvImageConfigCM environment variable for custom ConfigMap name
	EnvImageConfigCM = "PERCONA_PG_IMAGE_CONFIG_CM"
	// EnvImageConfigNS environment variable for custom ConfigMap namespace
	EnvImageConfigNS = "PERCONA_PG_IMAGE_CONFIG_NS"
)

// DefaultConfigMapNamespace returns the default namespace for the image configuration ConfigMap.
// It uses the operator's namespace from PGO_NAMESPACE environment variable.
func DefaultConfigMapNamespace() string {
	ns := os.Getenv("PGO_NAMESPACE")
	if ns == "" {
		// Fallback to default namespace if PGO_NAMESPACE is not set
		return "postgres-operator"
	}
	return ns
}

// ImageConfigLoader handles loading and merging of image configurations
type ImageConfigLoader struct {
	embedded   *DefaultImagesConfig
	userConfig *DefaultImagesConfig
	merged     *DefaultImagesConfig
}

// NewImageConfigLoader creates a new ImageConfigLoader with embedded defaults
func NewImageConfigLoader() *ImageConfigLoader {
	return &ImageConfigLoader{
		embedded: DefaultConfig,
	}
}

// Embedded returns the embedded default configuration
func (l *ImageConfigLoader) Embedded() *DefaultImagesConfig {
	return l.embedded
}

// LoadUserConfigFromFile loads user configuration from a YAML file.
// Returns error if file doesn't exist, fails to parse, or path validation fails.
func (l *ImageConfigLoader) LoadUserConfigFromFile(path string) error {
	// Clean the path first to resolve any .. or . components
	cleanPath := filepath.Clean(path)

	// Verify the cleaned path is absolute
	if !filepath.IsAbs(cleanPath) {
		return errors.Errorf("config file path must be absolute, got: %q", cleanPath)
	}

	// Verify file extension is .yaml or .yml
	if ext := filepath.Ext(cleanPath); ext != ".yaml" && ext != ".yml" {
		return errors.Errorf("config file must have .yaml or .yml extension, got: %q", ext)
	}

	data, err := os.ReadFile(cleanPath)
	if err != nil {
		return errors.Wrapf(err, "read config file %q", cleanPath)
	}

	var cfg DefaultImagesConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return errors.Wrapf(err, "parse config file %q", cleanPath)
	}

	l.userConfig = &cfg
	return nil
}

// LoadUserConfigFromConfigMap loads user configuration from a Kubernetes ConfigMap.
// Returns error if ConfigMap exists but key is missing or YAML parsing fails.
// Returns the raw Kubernetes API error for NotFound so callers can handle it appropriately.
func (l *ImageConfigLoader) LoadUserConfigFromConfigMap(
	ctx context.Context,
	k8sClient client.Client,
	cmName, cmNamespace, cmKey string,
) error {
	cm := &corev1.ConfigMap{}
	err := k8sClient.Get(ctx, types.NamespacedName{
		Name: cmName, Namespace: cmNamespace,
	}, cm)
	if err != nil {
		// Don't wrap NotFound errors so callers can check with apierrors.IsNotFound()
		if apierrors.IsNotFound(err) {
			return err
		}
		return errors.Wrapf(err, "get ConfigMap %s/%s", cmNamespace, cmName)
	}

	data, ok := cm.Data[cmKey]
	if !ok {
		return errors.Errorf("ConfigMap %s/%s exists but key %q is missing", cmNamespace, cmName, cmKey)
	}

	var cfg DefaultImagesConfig
	if err := yaml.Unmarshal([]byte(data), &cfg); err != nil {
		return errors.Wrapf(err, "ConfigMap %s/%s: failed to parse YAML in key %q", cmNamespace, cmName, cmKey)
	}

	l.userConfig = &cfg
	return nil
}

// LoadAuto attempts to load user configuration from environment or ConfigMap
// If neither is available, it continues with embedded defaults
func (l *ImageConfigLoader) LoadAuto(ctx context.Context, k8sClient client.Client) error {
	// Check environment variable for file path first
	if configPath := os.Getenv(EnvImageConfigPath); configPath != "" {
		return l.LoadUserConfigFromFile(configPath)
	}

	// Determine ConfigMap name and namespace
	cmName := DefaultConfigMapName
	if env := os.Getenv(EnvImageConfigCM); env != "" {
		cmName = env
	}

	cmNamespace := DefaultConfigMapNamespace()
	if env := os.Getenv(EnvImageConfigNS); env != "" {
		cmNamespace = env
	}

	err := l.LoadUserConfigFromConfigMap(ctx, k8sClient, cmName, cmNamespace, DefaultConfigMapKey)
	if err != nil {
		// Only silently ignore "ConfigMap not found" - this is expected when using defaults
		// All other errors (parse failures, permission errors, missing key) should be returned
		// because they indicate a misconfigured ConfigMap that the user explicitly created
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	}

	return nil
}

// Merge combines user configuration with embedded configuration
// User values take precedence over embedded defaults
func (l *ImageConfigLoader) Merge() error {
	if l.userConfig == nil {
		l.merged = l.embedded
		return nil
	}

	l.merged = DeepMergeConfigs(l.embedded, l.userConfig)
	return nil
}

// GetMergedConfig returns the merged configuration
func (l *ImageConfigLoader) GetMergedConfig() *DefaultImagesConfig {
	return l.merged
}

// InitializeGlobalConfig initializes the global image configuration
// It attempts to load user configuration and merges it with embedded defaults
func InitializeGlobalConfig(ctx context.Context, k8sClient client.Client) error {
	loader := NewImageConfigLoader()

	if err := loader.LoadAuto(ctx, k8sClient); err != nil {
		return err
	}

	if err := loader.Merge(); err != nil {
		return err
	}

	SetGlobalConfig(loader.GetMergedConfig())
	return nil
}
