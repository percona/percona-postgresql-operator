// Copyright 2021 - 2024 Crunchy Data Solutions, Inc.
//
// SPDX-License-Identifier: Apache-2.0

package runtime

import (
	"context"
	"io"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
	"k8s.io/client-go/util/flowcontrol"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// podExecutor runs command on container in pod in namespace. Non-nil streams
// (stdin, stdout, and stderr) are attached the to the remote process.
type PodExecutor func(
	ctx context.Context, namespace, pod, container string,
	stdin io.Reader, stdout, stderr io.Writer, command ...string,
) error

func newPodClient(config *rest.Config) (rest.Interface, error) {
	codecs := serializer.NewCodecFactory(scheme.Scheme)
	gvk, _ := apiutil.GVKForObject(&corev1.Pod{}, scheme.Scheme)
	httpClient, err := rest.HTTPClientFor(config)
	if err != nil {
		return nil, err
	}
	return apiutil.RESTClientForGVK(gvk, false, false, config, codecs, httpClient)
}

// +kubebuilder:rbac:groups="",resources="pods/exec",verbs={create}

func NewPodExecutor(config *rest.Config) (PodExecutor, error) {
	// Create a copy of the config to avoid modifying the original
	configCopy := rest.CopyConfig(config)

	// Ensure throttling is disabled by setting a fake rate limiter
	configCopy.RateLimiter = flowcontrol.NewFakeAlwaysRateLimiter()

	client, err := newPodClient(configCopy)

	return func(
		ctx context.Context, namespace, pod, container string,
		stdin io.Reader, stdout, stderr io.Writer, command ...string,
	) error {
		request := client.Post().
			Resource("pods").SubResource("exec").
			Namespace(namespace).Name(pod).
			VersionedParams(&corev1.PodExecOptions{
				Container: container,
				Command:   command,
				Stdin:     stdin != nil,
				Stdout:    stdout != nil,
				Stderr:    stderr != nil,
			}, scheme.ParameterCodec)

		exec, err := remotecommand.NewSPDYExecutor(configCopy, "POST", request.URL())

		log := logf.FromContext(ctx)
		log.V(1).Info("Running command in pod", "pod", pod, "container", container, "command", command)
		if err == nil {
			err = exec.StreamWithContext(ctx, remotecommand.StreamOptions{
				Stdin:  stdin,
				Stdout: stdout,
				Stderr: stderr,
			})
		}

		return err
	}, err
}
