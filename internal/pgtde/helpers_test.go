package pgtde

import (
	"context"
	"io"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/percona/percona-postgresql-operator/v2/internal/controller/runtime"
)

// execCall records a single invocation of a runtime.PodExecutor.
type execCall struct {
	namespace string
	pod       string
	container string
	stdin     string
	command   []string
}

// execRecorder returns a PodExecutor that appends every call to calls and
// returns the error produced by result, if any.
func execRecorder(calls *[]execCall, result func(call execCall) error) runtime.PodExecutor {
	return func(
		ctx context.Context, namespace, pod, container string,
		stdin io.Reader, stdout, stderr io.Writer, command ...string,
	) error {
		call := execCall{
			namespace: namespace,
			pod:       pod,
			container: container,
			command:   command,
		}
		if stdin != nil {
			b, err := io.ReadAll(stdin)
			if err != nil {
				return err
			}
			call.stdin = string(b)
		}

		*calls = append(*calls, call)

		if result != nil {
			if err := result(call); err != nil {
				return err
			}
		}

		// Stand in for the "wc -c" that writeTempFile appends to its write
		// command to detect short writes.
		if len(command) > 2 && strings.Contains(command[2], "wc -c") {
			_, _ = io.WriteString(stdout, strconv.Itoa(len(call.stdin))+"\n")
		}
		return nil
	}
}

// countingClient counts Get calls made against the embedded client.
type countingClient struct {
	client.Client
	gets *int
}

func (c *countingClient) Get(
	ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption,
) error {
	*c.gets++
	return c.Client.Get(ctx, key, obj, opts...)
}

// newPods returns Pods in namespace ns1 with the given names.
func newPods(names ...string) []*corev1.Pod {
	pods := make([]*corev1.Pod, 0, len(names))
	for _, name := range names {
		pods = append(pods, &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Namespace: "ns1", Name: name},
		})
	}
	return pods
}
