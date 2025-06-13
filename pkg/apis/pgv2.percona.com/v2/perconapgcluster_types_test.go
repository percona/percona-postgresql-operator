package v2

import (
	"testing"
	"os"
	"fmt"

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

	t.Run("Spec.Image should be empty by default", func(t *testing.T) {
		assert.Equal(t, cluster.PostgresImage(), "")
	})

	t.Run("Spec.Image should use env variables if present", func(t *testing.T) {
		os.Setenv(testEnv, testDefaultImage)
		defer os.Unsetenv(testEnv)

		assert.Equal(t, cluster.PostgresImage(), testDefaultImage)
	})

	t.Run("Spec.Image should use defined variable", func (t *testing.T) {
		os.Setenv(testEnv, testDefaultImage)
		defer os.Unsetenv(testEnv)

		cluster.Spec.Image = testSpecificImage

		assert.Equal(t, cluster.PostgresImage(), testSpecificImage)
	})
}
