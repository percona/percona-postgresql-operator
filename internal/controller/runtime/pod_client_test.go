package runtime

import (
	"context"
	"testing"

	"gotest.tools/v3/assert"
)

func TestPodExecTarget(t *testing.T) {
	// Recording without a target in the context does nothing.
	recordPodExecTarget(context.Background(), "ns", "pod", "container")

	ctx, target := WithPodExecTarget(context.Background())
	assert.Equal(t, *target, PodExecTarget{})

	recordPodExecTarget(ctx, "ns", "pod", "container")
	assert.Equal(t, *target, PodExecTarget{Namespace: "ns", Pod: "pod", Container: "container"})
}
