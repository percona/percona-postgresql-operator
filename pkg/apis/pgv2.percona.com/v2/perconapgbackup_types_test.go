package v2

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

func TestPITRestoreDateTime_MarshalJSON(t *testing.T) {
	type testCase struct {
		data     any
		expected string
	}
	tests := map[string]testCase{
		"default": {
			data:     PITRestoreDateTime{},
			expected: `null`,
		},
		"pointer": {
			data:     &PITRestoreDateTime{},
			expected: `null`,
		},
		"nil pointer": {
			data:     (*PITRestoreDateTime)(nil),
			expected: `null`,
		},
		"non-pointer zero date time": {
			data: PITRestoreDateTime{
				Time: ptr.To(metav1.NewTime(time.Time{})),
			},
			expected: `"0001-01-01 00:00:00.000000+0000"`,
		},
		"pointer zero date time": {
			data: &PITRestoreDateTime{
				Time: ptr.To(metav1.NewTime(time.Time{})),
			},
			expected: `"0001-01-01 00:00:00.000000+0000"`,
		},
		"non-pointer with date time": {
			data: PITRestoreDateTime{
				Time: ptr.To(metav1.NewTime(time.Date(2025, time.November, 21, 13, 14, 15, 345600000, time.UTC))),
			},
			expected: `"2025-11-21 13:14:15.345600+0000"`,
		},
		"pointer with date time": {
			data: &PITRestoreDateTime{
				Time: ptr.To(metav1.NewTime(time.Date(2025, time.November, 21, 13, 14, 15, 345600000, time.UTC))),
			},
			expected: `"2025-11-21 13:14:15.345600+0000"`,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			require.NotPanics(t, func() {
				b, err := json.Marshal(test.data)
				require.NoError(t, err)
				require.JSONEq(t, test.expected, string(b))
			})
		})
	}
}
