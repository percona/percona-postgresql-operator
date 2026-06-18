// Copyright 2021 - 2024 Crunchy Data Solutions, Inc.
//
// SPDX-License-Identifier: Apache-2.0

package pgcluster

import (
	"context"
	"strings"
	"testing"

	"gotest.tools/v3/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	v2 "github.com/percona/percona-postgresql-operator/v2/pkg/apis/pgv2.percona.com/v2"
	v1beta1 "github.com/percona/percona-postgresql-operator/v2/pkg/apis/upstream.pgv2.percona.com/v1beta1"
)

func newEtcdTestReconciler(t *testing.T, objs ...corev1.Secret) *PGClusterReconciler {
	t.Helper()

	s := scheme.Scheme
	if err := v1beta1.AddToScheme(s); err != nil {
		t.Fatalf("AddToScheme: %v", err)
	}
	if err := v2.AddToScheme(s); err != nil {
		t.Fatalf("AddToScheme: %v", err)
	}

	builder := fake.NewClientBuilder().WithScheme(s)
	for i := range objs {
		builder = builder.WithObjects(&objs[i])
	}

	return &PGClusterReconciler{
		Client:   builder.Build(),
		Recorder: record.NewFakeRecorder(10),
	}
}

func drainEvents(r *PGClusterReconciler) []string {
	ch := r.Recorder.(*record.FakeRecorder).Events
	var events []string
	for {
		select {
		case e := <-ch:
			events = append(events, e)
		default:
			return events
		}
	}
}

func etcdCR(namespace string, dcs *v1beta1.PatroniDCS) *v2.PerconaPGCluster {
	return &v2.PerconaPGCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "test-cluster", Namespace: namespace},
		Spec: v2.PerconaPGClusterSpec{
			Patroni: &v1beta1.PatroniSpec{DCS: dcs},
		},
	}
}

func TestReconcilePatroniEtcd(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("nil patroni spec returns nil", func(t *testing.T) {
		r := newEtcdTestReconciler(t)
		cr := &v2.PerconaPGCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster", Namespace: "ns"},
		}
		assert.NilError(t, r.reconcilePatroniEtcd(ctx, cr))
		assert.Equal(t, len(drainEvents(r)), 0)
	})

	t.Run("kubernetes DCS returns nil", func(t *testing.T) {
		r := newEtcdTestReconciler(t)
		cr := etcdCR("ns", &v1beta1.PatroniDCS{Type: v1beta1.PatroniDCSTypeKubernetes})
		assert.NilError(t, r.reconcilePatroniEtcd(ctx, cr))
		assert.Equal(t, len(drainEvents(r)), 0)
	})

	t.Run("nil dcs returns nil", func(t *testing.T) {
		r := newEtcdTestReconciler(t)
		cr := etcdCR("ns", nil)
		assert.NilError(t, r.reconcilePatroniEtcd(ctx, cr))
		assert.Equal(t, len(drainEvents(r)), 0)
	})

	t.Run("etcd DCS no secrets returns nil", func(t *testing.T) {
		r := newEtcdTestReconciler(t)
		cr := etcdCR("ns", &v1beta1.PatroniDCS{
			Type: v1beta1.PatroniDCSTypeEtcd,
			Etcd: &v1beta1.PatroniEtcdSpec{
				Endpoints: []string{"https://etcd:2379"},
			},
		})
		assert.NilError(t, r.reconcilePatroniEtcd(ctx, cr))
		assert.Equal(t, len(drainEvents(r)), 0)
	})

	t.Run("tls secret missing emits warning and error", func(t *testing.T) {
		r := newEtcdTestReconciler(t)
		cr := etcdCR("ns", &v1beta1.PatroniDCS{
			Type: v1beta1.PatroniDCSTypeEtcd,
			Etcd: &v1beta1.PatroniEtcdSpec{
				Endpoints: []string{"https://etcd:2379"},
				TLSSecret: "missing-tls-secret",
			},
		})
		err := r.reconcilePatroniEtcd(ctx, cr)
		assert.ErrorContains(t, err, "missing-tls-secret")
		events := drainEvents(r)
		assert.Equal(t, len(events), 1)
		assert.Assert(t, strings.Contains(events[0], "EtcdTLSSecretNotFound"))
	})

	t.Run("tls secret missing ca.crt key emits warning and error", func(t *testing.T) {
		secret := corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "tls-secret", Namespace: "ns"},
			Data: map[string][]byte{
				"tls.crt": []byte("cert"),
				"tls.key": []byte("key"),
				// ca.crt is intentionally absent
			},
		}
		r := newEtcdTestReconciler(t, secret)
		cr := etcdCR("ns", &v1beta1.PatroniDCS{
			Type: v1beta1.PatroniDCSTypeEtcd,
			Etcd: &v1beta1.PatroniEtcdSpec{
				Endpoints: []string{"https://etcd:2379"},
				TLSSecret: "tls-secret",
			},
		})
		err := r.reconcilePatroniEtcd(ctx, cr)
		assert.ErrorContains(t, err, "ca.crt")
		events := drainEvents(r)
		assert.Equal(t, len(events), 1)
		assert.Assert(t, strings.Contains(events[0], "EtcdTLSSecretNotFound"))
	})

	t.Run("auth secret missing emits warning and error", func(t *testing.T) {
		r := newEtcdTestReconciler(t)
		cr := etcdCR("ns", &v1beta1.PatroniDCS{
			Type: v1beta1.PatroniDCSTypeEtcd,
			Etcd: &v1beta1.PatroniEtcdSpec{
				Endpoints:  []string{"https://etcd:2379"},
				AuthSecret: "missing-auth-secret",
			},
		})
		err := r.reconcilePatroniEtcd(ctx, cr)
		assert.ErrorContains(t, err, "missing-auth-secret")
		events := drainEvents(r)
		assert.Equal(t, len(events), 1)
		assert.Assert(t, strings.Contains(events[0], "EtcdAuthSecretNotFound"))
	})

	t.Run("auth secret missing password key emits warning and error", func(t *testing.T) {
		secret := corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "auth-secret", Namespace: "ns"},
			Data: map[string][]byte{
				"username": []byte("etcduser"),
				// password is intentionally absent
			},
		}
		r := newEtcdTestReconciler(t, secret)
		cr := etcdCR("ns", &v1beta1.PatroniDCS{
			Type: v1beta1.PatroniDCSTypeEtcd,
			Etcd: &v1beta1.PatroniEtcdSpec{
				Endpoints:  []string{"https://etcd:2379"},
				AuthSecret: "auth-secret",
			},
		})
		err := r.reconcilePatroniEtcd(ctx, cr)
		assert.ErrorContains(t, err, "password")
		events := drainEvents(r)
		assert.Equal(t, len(events), 1)
		assert.Assert(t, strings.Contains(events[0], "EtcdAuthSecretNotFound"))
	})

	t.Run("both secrets valid returns nil", func(t *testing.T) {
		tlsSecret := corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "tls-secret", Namespace: "ns"},
			Data: map[string][]byte{
				"ca.crt":  []byte("ca"),
				"tls.crt": []byte("cert"),
				"tls.key": []byte("key"),
			},
		}
		authSecret := corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "auth-secret", Namespace: "ns"},
			Data: map[string][]byte{
				"username": []byte("etcduser"),
				"password": []byte("etcdpass"),
			},
		}
		r := newEtcdTestReconciler(t, tlsSecret, authSecret)
		cr := etcdCR("ns", &v1beta1.PatroniDCS{
			Type: v1beta1.PatroniDCSTypeEtcd,
			Etcd: &v1beta1.PatroniEtcdSpec{
				Endpoints:  []string{"https://etcd:2379"},
				TLSSecret:  "tls-secret",
				AuthSecret: "auth-secret",
			},
		})
		assert.NilError(t, r.reconcilePatroniEtcd(ctx, cr))
		assert.Equal(t, len(drainEvents(r)), 0)
	})
}

