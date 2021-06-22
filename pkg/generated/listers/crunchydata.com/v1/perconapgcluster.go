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

// Code generated by lister-gen. DO NOT EDIT.

package v1

import (
	v1 "github.com/percona/percona-postgresql-operator/pkg/apis/crunchydata.com/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/cache"
)

// PerconaPGclusterLister helps list PerconaPGclusters.
// All objects returned here must be treated as read-only.
type PerconaPGclusterLister interface {
	// List lists all PerconaPGclusters in the indexer.
	// Objects returned here must be treated as read-only.
	List(selector labels.Selector) (ret []*v1.PerconaPGCluster, err error)
	// PerconaPGclusters returns an object that can list and get PerconaPGclusters.
	Pgclusters(namespace string) PerconaPGClusterNamespaceLister
	PgclusterListerExpansion
}

// perconaPGClusterLister implements the PerconaPGclusterLister interface.
type perconaPGClusterLister struct {
	indexer cache.Indexer
}

// NewPgclusterLister returns a new PgclusterLister.
func NewPerconaPGclusterLister(indexer cache.Indexer) PerconaPGclusterLister {
	return &perconaPGClusterLister{indexer: indexer}
}

// List lists all Pgclusters in the indexer.
func (s *perconaPGClusterLister) List(selector labels.Selector) (ret []*v1.PerconaPGCluster, err error) {
	err = cache.ListAll(s.indexer, selector, func(m interface{}) {
		ret = append(ret, m.(*v1.PerconaPGCluster))
	})
	return ret, err
}

// Pgclusters returns an object that can list and get Pgclusters.
func (s *perconaPGClusterLister) Pgclusters(namespace string) PerconaPGClusterNamespaceLister {
	return perconaPGClusterNamespaceLister{indexer: s.indexer, namespace: namespace}
}

// PgclusterNamespaceLister helps list and get Pgclusters.
// All objects returned here must be treated as read-only.
type PerconaPGClusterNamespaceLister interface {
	// List lists all Pgclusters in the indexer for a given namespace.
	// Objects returned here must be treated as read-only.
	List(selector labels.Selector) (ret []*v1.PerconaPGCluster, err error)
	// Get retrieves the Pgcluster from the indexer for a given namespace and name.
	// Objects returned here must be treated as read-only.
	Get(name string) (*v1.PerconaPGCluster, error)
	PerconaPGClusterNamespaceListerExpansion
}

// perconaPGClusterNamespaceLister implements the PerconaPGClusterNamespaceLister
// interface.
type perconaPGClusterNamespaceLister struct {
	indexer   cache.Indexer
	namespace string
}

// List lists all Pgclusters in the indexer for a given namespace.
func (s perconaPGClusterNamespaceLister) List(selector labels.Selector) (ret []*v1.PerconaPGCluster, err error) {
	err = cache.ListAllByNamespace(s.indexer, s.namespace, selector, func(m interface{}) {
		ret = append(ret, m.(*v1.PerconaPGCluster))
	})
	return ret, err
}

// Get retrieves the Pgcluster from the indexer for a given namespace and name.
func (s perconaPGClusterNamespaceLister) Get(name string) (*v1.PerconaPGCluster, error) {
	obj, exists, err := s.indexer.GetByKey(s.namespace + "/" + name)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, errors.NewNotFound(v1.Resource("perconapgcluster"), name)
	}
	return obj.(*v1.PerconaPGCluster), nil
}
