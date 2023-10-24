package pgbackup

import (
	"strconv"
	"testing"
)

func TestGetBackupTypeFromOpts(t *testing.T) {
	tests := []struct {
		Opts     string
		Expected string
	}{
		{"--type=manual", "manual"},
		{"--type manual", "manual"},
		{"--type=full", "full"},
		{"--type full", "full"},
		{"--type=incr", "incr"},
		{"--type incr", "incr"},
		{"--somearg=type --type manual", "manual"},
		{"sometext--type=manual --invalidarg", ""},
	}

	for i, tt := range tests {
		t.Run(t.Name()+strconv.Itoa(i), func(t *testing.T) {
			actual := getBackupTypeFromOpts(tt.Opts)
			if string(actual) != tt.Expected {
				t.Errorf("expected %s, got %s", tt.Expected, actual)
			}
		})
	}
}
