package clientcmd

import (
	"context"
	"io"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes/scheme"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/remotecommand"
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
		return err
	}

	// Connect this process' std{in,out,err} to the remote shell process.
	return exec.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdin:  stdin,
		Stdout: stdout,
		Stderr: stderr,
		Tty:    tty,
	})
}

//func NewPodExecutor(config *)

//func newPodExecutor(config *rest.Config) (podExecutor, error) {
//	client, err := newPodClient(config)
//
//	return func(
//		namespace, pod, container string,
//		stdin io.Reader, stdout, stderr io.Writer, command ...string,
//	) error {
//		request := client.Post().
//			Resource("pods").SubResource("exec").
//			Namespace(namespace).Name(pod).
//			VersionedParams(&corev1.PodExecOptions{
//				Container: container,
//				Command:   command,
//				Stdin:     stdin != nil,
//				Stdout:    stdout != nil,
//				Stderr:    stderr != nil,
//			}, scheme.ParameterCodec)
//
//		exec, err := remotecommand.NewSPDYExecutor(config, "POST", request.URL())
//
//		if err == nil {
//			err = exec.Stream(remotecommand.StreamOptions{
//				Stdin:  stdin,
//				Stdout: stdout,
//				Stderr: stderr,
//			})
//		}
//
//		return err
//	}, err
//}

func (c *Client) REST() restclient.Interface {
	return c.client.RESTClient()
}
