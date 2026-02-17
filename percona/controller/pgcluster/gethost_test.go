package pgcluster

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	v2 "github.com/percona/percona-postgresql-operator/v2/pkg/apis/pgv2.percona.com/v2"
	"github.com/percona/percona-postgresql-operator/v2/pkg/apis/postgres-operator.crunchydata.com/v1beta1"
)

func TestGetHost(t *testing.T) {
	ctx := context.Background()

	const (
		clusterName = "test-cluster"
		ns          = "test-ns"
	)

	tests := []struct {
		name         string
		proxy        *v2.PGProxySpec
		svc          *corev1.Service
		expectedHost string
		expectErr    bool
	}{
		{
			name:         "proxy is nil",
			proxy:        nil,
			expectedHost: clusterName + "-primary." + ns + ".svc",
		},
		{
			name:         "proxy set but PGBouncer is nil",
			proxy:        &v2.PGProxySpec{PGBouncer: nil},
			expectedHost: clusterName + "-primary." + ns + ".svc",
		},
		{
			name:         "PGBouncer configured, no ServiceExpose",
			proxy:        &v2.PGProxySpec{PGBouncer: &v2.PGBouncerSpec{}},
			expectedHost: clusterName + "-pgbouncer." + ns + ".svc",
		},
		{
			name: "PGBouncer configured, ServiceExpose is ClusterIP",
			proxy: &v2.PGProxySpec{PGBouncer: &v2.PGBouncerSpec{
				ServiceExpose: &v2.ServiceExpose{Type: string(corev1.ServiceTypeClusterIP)},
			}},
			expectedHost: clusterName + "-pgbouncer." + ns + ".svc",
		},
		{
			name: "PGBouncer configured, ServiceExpose is NodePort",
			proxy: &v2.PGProxySpec{PGBouncer: &v2.PGBouncerSpec{
				ServiceExpose: &v2.ServiceExpose{Type: string(corev1.ServiceTypeNodePort)},
			}},
			expectedHost: clusterName + "-pgbouncer." + ns + ".svc",
		},
		{
			name: "PGBouncer LoadBalancer with IP",
			proxy: &v2.PGProxySpec{PGBouncer: &v2.PGBouncerSpec{
				ServiceExpose: &v2.ServiceExpose{Type: string(corev1.ServiceTypeLoadBalancer)},
			}},
			svc: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      clusterName + "-pgbouncer",
					Namespace: ns,
				},
				Status: corev1.ServiceStatus{
					LoadBalancer: corev1.LoadBalancerStatus{
						Ingress: []corev1.LoadBalancerIngress{
							{IP: "10.0.0.1"},
						},
					},
				},
			},
			expectedHost: "10.0.0.1",
		},
		{
			name: "PGBouncer LoadBalancer with hostname",
			proxy: &v2.PGProxySpec{PGBouncer: &v2.PGBouncerSpec{
				ServiceExpose: &v2.ServiceExpose{Type: string(corev1.ServiceTypeLoadBalancer)},
			}},
			svc: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      clusterName + "-pgbouncer",
					Namespace: ns,
				},
				Status: corev1.ServiceStatus{
					LoadBalancer: corev1.LoadBalancerStatus{
						Ingress: []corev1.LoadBalancerIngress{
							{Hostname: "my-lb.example.com"},
						},
					},
				},
			},
			expectedHost: "my-lb.example.com",
		},
		{
			name: "PGBouncer LoadBalancer with both IP and hostname prefers hostname",
			proxy: &v2.PGProxySpec{PGBouncer: &v2.PGBouncerSpec{
				ServiceExpose: &v2.ServiceExpose{Type: string(corev1.ServiceTypeLoadBalancer)},
			}},
			svc: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      clusterName + "-pgbouncer",
					Namespace: ns,
				},
				Status: corev1.ServiceStatus{
					LoadBalancer: corev1.LoadBalancerStatus{
						Ingress: []corev1.LoadBalancerIngress{
							{IP: "10.0.0.1", Hostname: "my-lb.example.com"},
						},
					},
				},
			},
			expectedHost: "my-lb.example.com",
		},
		{
			name: "PGBouncer LoadBalancer with no ingress returns empty host",
			proxy: &v2.PGProxySpec{PGBouncer: &v2.PGBouncerSpec{
				ServiceExpose: &v2.ServiceExpose{Type: string(corev1.ServiceTypeLoadBalancer)},
			}},
			svc: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      clusterName + "-pgbouncer",
					Namespace: ns,
				},
			},
			expectedHost: "",
		},
		{
			name: "PGBouncer LoadBalancer service not found",
			proxy: &v2.PGProxySpec{PGBouncer: &v2.PGBouncerSpec{
				ServiceExpose: &v2.ServiceExpose{Type: string(corev1.ServiceTypeLoadBalancer)},
			}},
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cr := &v2.PerconaPGCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      clusterName,
					Namespace: ns,
				},
				Spec: v2.PerconaPGClusterSpec{
					Proxy: tt.proxy,
				},
			}

			var objs []client.Object
			if tt.svc != nil {
				objs = append(objs, tt.svc)
			}

			s := scheme.Scheme
			err := v1beta1.AddToScheme(s)
			require.NoError(t, err)
			err = v2.AddToScheme(s)
			require.NoError(t, err)

			cl := fake.NewClientBuilder().
				WithScheme(s).
				WithObjects(objs...).
				WithStatusSubresource(objs...).
				Build()

			r := &PGClusterReconciler{
				Client: cl,
			}

			host, err := r.getHost(ctx, cr)
			if tt.expectErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.expectedHost, host)
		})
	}
}
