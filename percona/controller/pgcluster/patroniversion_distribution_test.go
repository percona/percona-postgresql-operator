package pgcluster

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"

	v2 "github.com/percona/percona-postgresql-operator/v3/pkg/apis/pgv2.percona.com/v2"
)

func TestGetPostgresDistribution(t *testing.T) {
	// Shorten the backoff so the retry paths don't sleep for real seconds.
	origBackoff := distributionExecBackoff
	distributionExecBackoff = wait.Backoff{Duration: time.Millisecond, Factor: 1.0, Steps: 3, Cap: 10 * time.Millisecond}
	t.Cleanup(func() { distributionExecBackoff = origBackoff })

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "cluster-instance1-abcd-0", Namespace: "pgtest"},
	}
	const container = "database"

	const perconaOut = "postgres (PostgreSQL) 17.5 - Percona Server for PostgreSQL 17.5.1\n"
	const communityOut = "postgres (PostgreSQL) 17.5\n"

	// step describes what the mocked PodExec does on a single attempt.
	type step struct {
		stdout string
		stderr string
		err    error
	}

	tests := []struct {
		name      string
		steps     []step
		want      string
		wantErr   bool
		errSubstr string
		wantCalls int
	}{
		{
			name:      "percona output is detected",
			steps:     []step{{stdout: perconaOut}},
			want:      v2.PostgresDistributionPercona,
			wantCalls: 1,
		},
		{
			name:      "non-percona output falls back to community",
			steps:     []step{{stdout: communityOut}},
			want:      v2.PostgresDistributionCommunity,
			wantCalls: 1,
		},
		{
			name:      "empty output falls back to community",
			steps:     []step{{stdout: ""}},
			want:      v2.PostgresDistributionCommunity,
			wantCalls: 1,
		},
		{
			name:      "non-retryable exec error surfaces stderr and does not retry",
			steps:     []step{{stderr: "  permission denied\n", err: errors.New("command terminated with exit code 1")}},
			wantErr:   true,
			errSubstr: "permission denied",
			wantCalls: 1,
		},
		{
			// The first attempt hits the transient startup race AND writes the
			// Percona marker; the retry must reset the buffer so the stale marker
			// cannot leak into the community result.
			name: "retries on transient container-not-found and resets buffers",
			steps: []step{
				{stdout: perconaOut, err: errors.New("container not found")},
				{stdout: communityOut},
			},
			want:      v2.PostgresDistributionCommunity,
			wantCalls: 2,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			r := &PGClusterReconciler{
				PodExec: func(_ context.Context, ns, podName, c string, _ io.Reader, stdout, stderr io.Writer, cmd ...string) error {
					assert.Equal(t, pod.Namespace, ns)
					assert.Equal(t, pod.Name, podName)
					assert.Equal(t, container, c)
					assert.Equal(t, []string{"postgres", "-V"}, cmd)

					require.Less(t, calls, len(tc.steps), "PodExec called more times than expected")
					s := tc.steps[calls]
					calls++
					if s.stdout != "" {
						_, _ = io.WriteString(stdout, s.stdout)
					}
					if s.stderr != "" {
						_, _ = io.WriteString(stderr, s.stderr)
					}
					return s.err
				},
			}

			got, err := r.getPostgresDistribution(context.Background(), pod, container)

			assert.Equal(t, tc.wantCalls, calls, "unexpected number of exec attempts")

			if tc.wantErr {
				require.Error(t, err)
				assert.Empty(t, got)
				if tc.errSubstr != "" {
					assert.Contains(t, err.Error(), tc.errSubstr)
				}
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}
