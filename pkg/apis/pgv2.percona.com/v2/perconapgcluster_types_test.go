package v2

import (
	"fmt"
	"os"
	"testing"

	"gotest.tools/v3/assert"
)

func TestPerconaPGCluster_Default(t *testing.T) {
	// cr.Default() should not panic on PerconaPGCluster with empty fields
	new(PerconaPGCluster).Default()
}

func TestPerconaPGCluster_BackupsEnabled(t *testing.T) {
	trueVal := true
	falseVal := false

	tests := map[string]struct {
		spec     PerconaPGClusterSpec
		expected bool
	}{

		"Enabled is nil, should return true because default is true": {
			spec:     PerconaPGClusterSpec{Backups: Backups{Enabled: nil}},
			expected: true,
		},
		"Enabled is true, should return true": {
			spec:     PerconaPGClusterSpec{Backups: Backups{Enabled: &trueVal}},
			expected: true,
		},
		"Enabled is false, should return false": {
			spec:     PerconaPGClusterSpec{Backups: Backups{Enabled: &falseVal}},
			expected: false,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			actual := tt.spec.Backups.IsEnabled()
			assert.Equal(t, tt.expected, actual)
		})
	}
}

func TestPerconaPGCluster_PostgresImage(t *testing.T) {
	cluster := new(PerconaPGCluster)
	cluster.Default()

	postgresVersion := 16
	testDefaultImage := fmt.Sprintf("test_default_image:%d", postgresVersion)
	testSpecificImage := fmt.Sprintf("test_defined_image:%d", postgresVersion)
	testEnv := fmt.Sprintf("RELATED_IMAGE_POSTGRES_%d", postgresVersion)

	cluster.Spec.PostgresVersion = postgresVersion

	tests := map[string]struct {
		expectedImage string
		setImage      string
		envImage      string
	}{
		"Spec.Image should be empty by default": {
			expectedImage: "",
			setImage:      "",
			envImage:      "",
		},
		"Spec.Image should use env variables if present": {
			expectedImage: testDefaultImage,
			setImage:      "",
			envImage:      testDefaultImage,
		},
		"Spec.Image should use defined variable": {
			expectedImage: testSpecificImage,
			setImage:      testSpecificImage,
			envImage:      testDefaultImage,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {

			cluster.Spec.Image = tt.setImage

			if tt.envImage != "" {
				err := os.Setenv(testEnv, tt.envImage)

				if err != nil {
					t.Fatalf("Failed to set %s env variable: %v", testEnv, err)
				}

				defer func() {
					err := os.Unsetenv(testEnv)
					if err != nil {
						t.Errorf("Failed to unset %s env variable: %v", testEnv, err)
					}
				}()
			}

			assert.Equal(t, cluster.PostgresImage(), tt.expectedImage)
		})
	}
}
