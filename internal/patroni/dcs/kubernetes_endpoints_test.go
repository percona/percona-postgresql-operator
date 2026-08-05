// Copyright 2021 - 2024 Crunchy Data Solutions, Inc.
//
// SPDX-License-Identifier: Apache-2.0

package dcs

import (
	"context"
	"testing"
	"time"

	"gotest.tools/v3/assert"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/percona/percona-postgresql-operator/v2/internal/naming"
	"github.com/percona/percona-postgresql-operator/v2/internal/testing/cmp"
	"github.com/percona/percona-postgresql-operator/v2/internal/testing/require"
	"github.com/percona/percona-postgresql-operator/v2/pkg/apis/upstream.pgv2.percona.com/v1beta1"
)

func TestKubernetesEndpointsClusterYAML(t *testing.T) {
	cluster := new(v1beta1.PostgresCluster)
	assert.NilError(t, cluster.Default(context.Background(), nil))
	cluster.Namespace = "some-namespace"
	cluster.Name = "cluster-name"

	dcsYAML := (kubernetesEndpointsBackend{}).ClusterYAML(cluster)
	assert.Assert(t, cmp.MarshalMatches(dcsYAML, `
kubernetes:
  labels:
    postgres-operator.crunchydata.com/cluster: cluster-name
  namespace: some-namespace
  role_label: postgres-operator.crunchydata.com/role
  scope_label: postgres-operator.crunchydata.com/patroni
  use_endpoints: true
	`))
}

func TestKubernetesEndpointsInstanceYAML(t *testing.T) {
	dcsYAML := (kubernetesEndpointsBackend{}).InstanceYAML(new(v1beta1.PostgresCluster))
	assert.Assert(t, dcsYAML == nil)
}

func TestKubernetesEndpointsInstanceEnvVars(t *testing.T) {
	leaderService := new(corev1.Service)
	leaderService.Spec.Ports = []corev1.ServicePort{{Name: "postgres"}}
	leaderService.Spec.Ports[0].TargetPort.StrVal = "postgres"
	containers := []corev1.Container{{Name: "okay"}}
	containers[0].Ports = []corev1.ContainerPort{{
		Name: "postgres", ContainerPort: 9999, Protocol: corev1.ProtocolTCP,
	}}

	vars := (kubernetesEndpointsBackend{}).InstanceEnvVars(new(v1beta1.PostgresCluster), leaderService, containers)

	assert.Assert(t, cmp.MarshalMatches(vars, `
- name: PATRONI_KUBERNETES_POD_IP
  valueFrom:
    fieldRef:
      apiVersion: v1
      fieldPath: status.podIP
- name: PATRONI_KUBERNETES_PORTS
  value: |
    - name: postgres
      port: 9999
      protocol: TCP
	`))
}

func TestKubernetesEndpointsPermissions(t *testing.T) {
	cluster := new(v1beta1.PostgresCluster)

	t.Run("Upstream", func(t *testing.T) {
		permissions := (kubernetesEndpointsBackend{}).Permissions(cluster)
		assert.Assert(t, cmp.MarshalMatches(permissions, `
- apiGroups:
  - ""
  resources:
  - endpoints
  verbs:
  - create
  - deletecollection
  - get
  - list
  - patch
  - watch
- apiGroups:
  - ""
  resources:
  - services
  verbs:
  - create
		`))
	})

	t.Run("OpenShift", func(t *testing.T) {
		cluster := cluster.DeepCopy()
		cluster.Spec.OpenShift = new(bool)
		*cluster.Spec.OpenShift = true

		permissions := (kubernetesEndpointsBackend{}).Permissions(cluster)
		assert.Assert(t, cmp.MarshalMatches(permissions, `
- apiGroups:
  - ""
  resources:
  - endpoints
  verbs:
  - create
  - deletecollection
  - get
  - list
  - patch
  - watch
- apiGroups:
  - ""
  resources:
  - endpoints/restricted
  verbs:
  - create
- apiGroups:
  - ""
  resources:
  - services
  verbs:
  - create
		`))
	})
}

func TestKubernetesEndpointsDistributedConfigurationService(t *testing.T) {
	cluster := new(v1beta1.PostgresCluster)
	cluster.Namespace = "ns1"
	cluster.Name = "pg1"

	service := (kubernetesEndpointsBackend{}).DistributedConfigurationService(cluster)
	assert.Assert(t, service != nil)
	assert.Equal(t, service.Namespace, "ns1")
	assert.Equal(t, service.Name, naming.PatroniScope(cluster)+"-config")
	assert.Equal(t, service.Spec.ClusterIP, corev1.ClusterIPNone)
	assert.Assert(t, service.Spec.Selector == nil, "got %v", service.Spec.Selector)
}

func TestKubernetesEndpointsLeaderLeaseService(t *testing.T) {
	cluster := &v1beta1.PostgresCluster{}
	cluster.Namespace = "ns1"
	cluster.Name = "pg2"
	cluster.Spec.Port = new(int32(9876))
	cluster.Labels = map[string]string{
		naming.LabelVersion: "2.3.0",
	}

	alwaysExpect := func(t testing.TB, service *corev1.Service) {
		assert.Assert(t, cmp.MarshalMatches(service.TypeMeta, `
apiVersion: v1
kind: Service
		`))
		assert.Equal(t, service.Name, "pg2-ha")
		assert.Equal(t, service.Namespace, "ns1")

		// Always gets a ClusterIP (never None).
		assert.Equal(t, service.Spec.ClusterIP, "")
		assert.Assert(t, service.Spec.Selector == nil,
			"got %v", service.Spec.Selector)
	}

	t.Run("NoServiceSpec", func(t *testing.T) {
		service, err := (kubernetesEndpointsBackend{}).LeaderLeaseService(cluster, new(record.FakeRecorder))
		assert.NilError(t, err)
		alwaysExpect(t, service)
		// Defaults to ClusterIP.
		assert.Equal(t, service.Spec.Type, corev1.ServiceTypeClusterIP)
		assert.Assert(t, cmp.MarshalMatches(service.Spec.Ports, `
- name: postgres
  port: 9876
  protocol: TCP
  targetPort: postgres
		`))
	})

	t.Run("AnnotationsLabels", func(t *testing.T) {
		cluster := cluster.DeepCopy()
		cluster.Spec.Metadata = &v1beta1.Metadata{
			Annotations: map[string]string{"a": "v1"},
			Labels:      map[string]string{"b": "v2"},
		}

		service, err := (kubernetesEndpointsBackend{}).LeaderLeaseService(cluster, new(record.FakeRecorder))
		assert.NilError(t, err)

		assert.DeepEqual(t, service.ObjectMeta.Annotations, map[string]string{
			"a": "v1",
		})
		assert.DeepEqual(t, service.ObjectMeta.Labels, map[string]string(naming.WithPerconaLabels(map[string]string{
			"b": "v2",
			"postgres-operator.crunchydata.com/cluster": "pg2",
			"postgres-operator.crunchydata.com/patroni": "pg2-ha",
		}, "pg2", "", "2.3.0")))

		// Add metadata to individual service
		cluster.Spec.Service = &v1beta1.ServiceSpec{
			Metadata: &v1beta1.Metadata{
				Annotations: map[string]string{"c": "v3"},
				Labels: map[string]string{"d": "v4",
					"postgres-operator.crunchydata.com/cluster": "wrongName"},
			},
		}

		service, err = (kubernetesEndpointsBackend{}).LeaderLeaseService(cluster, new(record.FakeRecorder))
		assert.NilError(t, err)

		assert.DeepEqual(t, service.ObjectMeta.Annotations, map[string]string{
			"a": "v1",
			"c": "v3",
		})
		assert.DeepEqual(t, service.ObjectMeta.Labels, map[string]string(naming.WithPerconaLabels(map[string]string{
			"b": "v2",
			"d": "v4",
			"postgres-operator.crunchydata.com/cluster": "pg2",
			"postgres-operator.crunchydata.com/patroni": "pg2-ha",
		}, "pg2", "", "2.3.0")))
	})

	types := []struct {
		Type   string
		Expect func(testing.TB, *corev1.Service)
	}{
		{Type: "ClusterIP", Expect: func(t testing.TB, service *corev1.Service) {
			assert.Equal(t, service.Spec.Type, corev1.ServiceTypeClusterIP)
		}},
		{Type: "NodePort", Expect: func(t testing.TB, service *corev1.Service) {
			assert.Equal(t, service.Spec.Type, corev1.ServiceTypeNodePort)
		}},
		{Type: "LoadBalancer", Expect: func(t testing.TB, service *corev1.Service) {
			assert.Equal(t, service.Spec.Type, corev1.ServiceTypeLoadBalancer)
		}},
	}

	for _, test := range types {
		t.Run(test.Type, func(t *testing.T) {
			cluster := cluster.DeepCopy()
			cluster.Spec.Service = &v1beta1.ServiceSpec{Type: test.Type}

			service, err := (kubernetesEndpointsBackend{}).LeaderLeaseService(cluster, new(record.FakeRecorder))
			assert.NilError(t, err)
			alwaysExpect(t, service)
			test.Expect(t, service)
		})
	}

	t.Run("NodePortWithClusterIP", func(t *testing.T) {
		cluster := cluster.DeepCopy()
		cluster.Spec.Service = &v1beta1.ServiceSpec{Type: "ClusterIP", NodePort: new(int32(32000))}

		recorder := new(record.FakeRecorder)
		service, err := (kubernetesEndpointsBackend{}).LeaderLeaseService(cluster, recorder)
		assert.ErrorContains(t, err, `NodePort cannot be set with type ClusterIP on Service "pg2-ha"`)
		assert.Assert(t, service == nil)
	})
}

func TestKubernetesEndpointsPrimaryService(t *testing.T) {
	cluster := new(v1beta1.PostgresCluster)
	cluster.Spec.Port = new(int32(2600))

	t.Run("NoLeader", func(t *testing.T) {
		spec, subset, err := (kubernetesEndpointsBackend{}).PrimaryService(cluster, nil)
		assert.ErrorContains(t, err, "not implemented")
		assert.DeepEqual(t, spec, corev1.ServiceSpec{})
		assert.Assert(t, subset == nil)
	})

	t.Run("ResolvesToLeaderClusterIP", func(t *testing.T) {
		leader := &corev1.Service{}
		leader.Spec.ClusterIP = "1.9.8.3"

		spec, subset, err := (kubernetesEndpointsBackend{}).PrimaryService(cluster, leader)
		assert.NilError(t, err)

		assert.Equal(t, spec.ClusterIP, corev1.ClusterIPNone)
		assert.Assert(t, spec.Selector == nil, "got %v", spec.Selector)
		assert.Assert(t, cmp.MarshalMatches(spec.Ports, `
- name: postgres
  port: 2600
  protocol: TCP
  targetPort: postgres
		`))

		assert.Assert(t, subset != nil)
		assert.Assert(t, cmp.MarshalMatches(subset, `
addresses:
- ip: 1.9.8.3
ports:
- name: postgres
  port: 2600
  protocol: TCP
		`))
	})
}

func TestKubernetesEndpointsObserve(t *testing.T) {
	_, cc := require.Kubernetes2(t)
	require.ParallelCapacity(t, 0)
	ns := require.Namespace(t, cc)
	ctx := context.Background()

	cluster := new(v1beta1.PostgresCluster)
	cluster.Namespace = ns.Name
	cluster.Name = "observe-test"

	t.Run("NotFound, not ready", func(t *testing.T) {
		observation, err := (kubernetesEndpointsBackend{}).Observe(ctx, cc, cluster, false)
		assert.NilError(t, err)
		assert.Equal(t, observation.SystemIdentifier, "")
		assert.Equal(t, observation.RequeueAfter, time.Duration(0))
	})

	t.Run("NotFound, ready", func(t *testing.T) {
		observation, err := (kubernetesEndpointsBackend{}).Observe(ctx, cc, cluster, true)
		assert.NilError(t, err)
		assert.Equal(t, observation.SystemIdentifier, "")
		assert.Equal(t, observation.RequeueAfter, time.Second)
	})

	t.Run("initialize annotation present", func(t *testing.T) {
		endpoints := &corev1.Endpoints{ObjectMeta: naming.PatroniDistributedConfiguration(cluster)}
		endpoints.Annotations = map[string]string{"initialize": "123456"}
		assert.NilError(t, cc.Create(ctx, endpoints))
		t.Cleanup(func() { assert.Check(t, client.IgnoreNotFound(cc.Delete(ctx, endpoints))) })

		observation, err := (kubernetesEndpointsBackend{}).Observe(ctx, cc, cluster, false)
		assert.NilError(t, err)
		assert.Equal(t, observation.SystemIdentifier, "123456")
		assert.Equal(t, observation.RequeueAfter, time.Duration(0))
	})
}

func TestKubernetesEndpointsDelete(t *testing.T) {
	_, cc := require.Kubernetes2(t)
	require.ParallelCapacity(t, 0)
	ns := require.Namespace(t, cc)
	ctx := context.Background()

	cluster := new(v1beta1.PostgresCluster)
	cluster.Namespace = ns.Name
	cluster.Name = "delete-test"

	endpoints := &corev1.Endpoints{ObjectMeta: naming.PatroniDistributedConfiguration(cluster)}
	endpoints.Labels = map[string]string{
		naming.LabelCluster: cluster.Name,
		naming.LabelPatroni: naming.PatroniScope(cluster),
	}
	assert.NilError(t, cc.Create(ctx, endpoints))

	assert.NilError(t, (kubernetesEndpointsBackend{}).Delete(ctx, cc, cluster))

	err := cc.Get(ctx, client.ObjectKeyFromObject(endpoints), endpoints)
	assert.Assert(t, apierrors.IsNotFound(err), "expected the Endpoints to be deleted, got %v", err)
}
