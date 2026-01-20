package clientcmd

import (
	"context"
	"io"

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes/scheme"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/remotecommand"
	"k8s.io/client-go/util/flowcontrol"
	"k8s.io/client-go/util/retry"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

type Client struct {
	client     corev1client.CoreV1Interface
	restconfig *restclient.Config
}

func NewClient() (*Client, error) {
	// Instantiate loader for kubeconfig file.
	kubeconfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		clientcmd.NewDefaultClientConfigLoadingRules(),
		&clientcmd.ConfigOverrides{},
	)

	// Get a rest.Config from the kubeconfig file.  This will be passed into all
	// the client objects we create.
	restconfig, err := kubeconfig.ClientConfig()
	if err != nil {
		return nil, err
	}

	// Ensure throttling is disabled by setting a fake rate limiter
	restconfig.RateLimiter = flowcontrol.NewFakeAlwaysRateLimiter()

	// Create a Kubernetes core/v1 client.
	cl, err := corev1client.NewForConfig(restconfig)
	if err != nil {
		return nil, err
	}

	return &Client{
		client:     cl,
		restconfig: restconfig,
	}, nil
}

func (c *Client) Exec(ctx context.Context, pod *corev1.Pod, containerName string, stdin io.Reader, stdout, stderr io.Writer, command ...string) error {
	// Prepare the API URL used to execute another process within the Pod.
	// In this case, we'll run a remote shell.
	tty := false
	req := c.client.RESTClient().
		Post().
		Namespace(pod.Namespace).
		Resource("pods").
		Name(pod.Name).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Container: containerName,
			Command:   command,
			Stdin:     stdin != nil,
			Stdout:    stdout != nil,
			Stderr:    stderr != nil,
			TTY:       tty,
		}, scheme.ParameterCodec)

	exec, err := remotecommand.NewSPDYExecutor(c.restconfig, "POST", req.URL())
	if err != nil {
		return errors.Wrap(err, "failed to create executor")
	}

	log := logf.FromContext(ctx)
	log.V(1).Info("Running command in pod", "pod", pod.Name, "container", containerName, "command", command)

	retryErr := retry.OnError(retry.DefaultRetry, func(err error) bool {
		return true // Retry on all errors
	}, func() error {
		// Connect this process' std{in,out,err} to the remote shell process.
		return exec.StreamWithContext(ctx, remotecommand.StreamOptions{
			Stdin:  stdin,
			Stdout: stdout,
			Stderr: stderr,
			Tty:    tty,
		})
	})

	if retryErr != nil {
		return errors.Wrap(retryErr, "failed to execute command in pod")
	}

	return nil
}

func (c *Client) REST() restclient.Interface {
	return c.client.RESTClient()
}
