package naming

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestManagedExternalDNSHostnames(t *testing.T) {
	managed := func(hostname string) map[string]string {
		return map[string]string{
			AnnotationExternalDNSHostname: hostname,
			AnnotationExternalDNSManaged:  "true",
		}
	}

	tests := map[string]struct {
		annotations []map[string]string
		expected    []string
	}{
		"no annotations": {
			annotations: nil,
			expected:    []string{},
		},
		"one managed hostname": {
			annotations: []map[string]string{managed("pg.example.com")},
			expected:    []string{"pg.example.com"},
		},
		"several managed hostnames keep their order": {
			annotations: []map[string]string{
				managed("pg.example.com"),
				managed("pg-replicas.example.com"),
			},
			expected: []string{"pg.example.com", "pg-replicas.example.com"},
		},
		// A hostname a user wrote into expose.annotations carries no marker, so
		// it must not change the certificate of an already running cluster.
		"unmarked hostname is ignored": {
			annotations: []map[string]string{
				{AnnotationExternalDNSHostname: "manual.example.com"},
			},
			expected: []string{},
		},
		"marker without a hostname yields nothing": {
			annotations: []map[string]string{
				{AnnotationExternalDNSManaged: "true"},
			},
			expected: []string{},
		},
		"nil and empty maps are skipped": {
			annotations: []map[string]string{nil, {}, managed("pg.example.com")},
			expected:    []string{"pg.example.com"},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tt.expected, ManagedExternalDNSHostnames(tt.annotations...))
		})
	}
}
