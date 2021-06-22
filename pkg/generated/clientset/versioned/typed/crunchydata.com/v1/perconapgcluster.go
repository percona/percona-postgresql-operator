/*
Copyright 2020 - 2021 Crunchy Data Solutions, Inc.
Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Code generated by client-gen. DO NOT EDIT.

package v1

import (
	"context"
	"time"

	v1 "github.com/percona/percona-postgresql-operator/pkg/apis/crunchydata.com/v1"
	scheme "github.com/percona/percona-postgresql-operator/pkg/generated/clientset/versioned/scheme"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	types "k8s.io/apimachinery/pkg/types"
	watch "k8s.io/apimachinery/pkg/watch"
	rest "k8s.io/client-go/rest"
)

// PerconaPGClustersGetter has a method to return a PgclusterInterface.
// A group's client should implement this interface.
type PerconaPGClustersGetter interface {
	PerconaPGClusters(namespace string) PerconaPGClusterInterface
}

// PgclusterInterface has methods to work with Pgcluster resources.
type PerconaPGClusterInterface interface {
	Create(ctx context.Context, pgcluster *v1.PerconaPGCluster, opts metav1.CreateOptions) (*v1.PerconaPGCluster, error)
	Update(ctx context.Context, pgcluster *v1.PerconaPGCluster, opts metav1.UpdateOptions) (*v1.PerconaPGCluster, error)
	UpdateStatus(ctx context.Context, pgcluster *v1.PerconaPGCluster, opts metav1.UpdateOptions) (*v1.PerconaPGCluster, error)
	Delete(ctx context.Context, name string, opts metav1.DeleteOptions) error
	DeleteCollection(ctx context.Context, opts metav1.DeleteOptions, listOpts metav1.ListOptions) error
	Get(ctx context.Context, name string, opts metav1.GetOptions) (*v1.PerconaPGCluster, error)
	List(ctx context.Context, opts metav1.ListOptions) (*v1.PerconaPGClusterList, error)
	Watch(ctx context.Context, opts metav1.ListOptions) (watch.Interface, error)
	Patch(ctx context.Context, name string, pt types.PatchType, data []byte, opts metav1.PatchOptions, subresources ...string) (result *v1.PerconaPGCluster, err error)
	PgclusterExpansion
}

// perconapgclusters implements PgclusterInterface
type perconapgclusters struct {
	client rest.Interface
	ns     string
}

// newPgclusters returns a Pgclusters
func newPerconaPGClusters(c *CrunchydataV1Client, namespace string) *perconapgclusters {
	return &perconapgclusters{
		client: c.RESTClient(),
		ns:     namespace,
	}
}

// Get takes name of the pgcluster, and returns the corresponding pgcluster object, and an error if there is any.
func (c *perconapgclusters) Get(ctx context.Context, name string, options metav1.GetOptions) (result *v1.PerconaPGCluster, err error) {
	result = &v1.PerconaPGCluster{}
	err = c.client.Get().
		Namespace(c.ns).
		Resource("perconapgclusters").
		Name(name).
		VersionedParams(&options, scheme.ParameterCodec).
		Do(ctx).
		Into(result)
	return
}

// List takes label and field selectors, and returns the list of perconapgclusters that match those selectors.
func (c *perconapgclusters) List(ctx context.Context, opts metav1.ListOptions) (result *v1.PerconaPGClusterList, err error) {
	var timeout time.Duration
	if opts.TimeoutSeconds != nil {
		timeout = time.Duration(*opts.TimeoutSeconds) * time.Second
	}
	result = &v1.PerconaPGClusterList{}
	err = c.client.Get().
		Namespace(c.ns).
		Resource("perconapgclusters").
		VersionedParams(&opts, scheme.ParameterCodec).
		Timeout(timeout).
		Do(ctx).
		Into(result)
	return
}

// Watch returns a watch.Interface that watches the requested perconapgclusters.
func (c *perconapgclusters) Watch(ctx context.Context, opts metav1.ListOptions) (watch.Interface, error) {
	var timeout time.Duration
	if opts.TimeoutSeconds != nil {
		timeout = time.Duration(*opts.TimeoutSeconds) * time.Second
	}
	opts.Watch = true
	return c.client.Get().
		Namespace(c.ns).
		Resource("perconapgclusters").
		VersionedParams(&opts, scheme.ParameterCodec).
		Timeout(timeout).
		Watch(ctx)
}

// Create takes the representation of a pgcluster and creates it.  Returns the server's representation of the pgcluster, and an error, if there is any.
func (c *perconapgclusters) Create(ctx context.Context, pgcluster *v1.PerconaPGCluster, opts metav1.CreateOptions) (result *v1.PerconaPGCluster, err error) {
	result = &v1.PerconaPGCluster{}
	err = c.client.Post().
		Namespace(c.ns).
		Resource("perconapgclusters").
		VersionedParams(&opts, scheme.ParameterCodec).
		Body(pgcluster).
		Do(ctx).
		Into(result)
	return
}

// Update takes the representation of a pgcluster and updates it. Returns the server's representation of the pgcluster, and an error, if there is any.
func (c *perconapgclusters) Update(ctx context.Context, pgcluster *v1.PerconaPGCluster, opts metav1.UpdateOptions) (result *v1.PerconaPGCluster, err error) {
	result = &v1.PerconaPGCluster{}
	err = c.client.Put().
		Namespace(c.ns).
		Resource("perconapgclusters").
		Name(pgcluster.Name).
		VersionedParams(&opts, scheme.ParameterCodec).
		Body(pgcluster).
		Do(ctx).
		Into(result)
	return
}

// UpdateStatus was generated because the type contains a Status member.
// Add a +genclient:noStatus comment above the type to avoid generating UpdateStatus().
func (c *perconapgclusters) UpdateStatus(ctx context.Context, pgcluster *v1.PerconaPGCluster, opts metav1.UpdateOptions) (result *v1.PerconaPGCluster, err error) {
	result = &v1.PerconaPGCluster{}
	err = c.client.Put().
		Namespace(c.ns).
		Resource("perconapgclusters").
		Name(pgcluster.Name).
		SubResource("status").
		VersionedParams(&opts, scheme.ParameterCodec).
		Body(pgcluster).
		Do(ctx).
		Into(result)
	return
}

// Delete takes name of the pgcluster and deletes it. Returns an error if one occurs.
func (c *perconapgclusters) Delete(ctx context.Context, name string, opts metav1.DeleteOptions) error {
	return c.client.Delete().
		Namespace(c.ns).
		Resource("perconapgclusters").
		Name(name).
		Body(&opts).
		Do(ctx).
		Error()
}

// DeleteCollection deletes a collection of objects.
func (c *perconapgclusters) DeleteCollection(ctx context.Context, opts metav1.DeleteOptions, listOpts metav1.ListOptions) error {
	var timeout time.Duration
	if listOpts.TimeoutSeconds != nil {
		timeout = time.Duration(*listOpts.TimeoutSeconds) * time.Second
	}
	return c.client.Delete().
		Namespace(c.ns).
		Resource("perconapgclusters").
		VersionedParams(&listOpts, scheme.ParameterCodec).
		Timeout(timeout).
		Body(&opts).
		Do(ctx).
		Error()
}

// Patch applies the patch and returns the patched pgcluster.
func (c *perconapgclusters) Patch(ctx context.Context, name string, pt types.PatchType, data []byte, opts metav1.PatchOptions, subresources ...string) (result *v1.PerconaPGCluster, err error) {
	result = &v1.PerconaPGCluster{}
	err = c.client.Patch(pt).
		Namespace(c.ns).
		Resource("perconapgclusters").
		Name(name).
		SubResource(subresources...).
		VersionedParams(&opts, scheme.ParameterCodec).
		Body(data).
		Do(ctx).
		Into(result)
	return
}
