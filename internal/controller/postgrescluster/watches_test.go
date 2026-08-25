// Copyright 2021 - 2024 Crunchy Data Solutions, Inc.
//
// SPDX-License-Identifier: Apache-2.0

package postgrescluster

import (
	"testing"

	"gotest.tools/v3/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllertest"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/percona/percona-postgresql-operator/v2/pkg/apis/upstream.pgv2.percona.com/v1beta1"
)

func TestWatchCertManagerSecrets(t *testing.T) {
	ctx := t.Context()
	reconciler := &Reconciler{}
	h := reconciler.watchCertManagerSecrets()

	t.Run("secret without cluster label enqueues nothing", func(t *testing.T) {
		queue := &controllertest.Queue{TypedInterface: workqueue.NewTyped[reconcile.Request]()}
		h.Generic(ctx, event.GenericEvent{Object: &corev1.Secret{}}, queue)
		assert.Equal(t, queue.Len(), 0)
	})

	t.Run("secret with cluster label enqueues reconcile for that cluster", func(t *testing.T) {
		queue := &controllertest.Queue{TypedInterface: workqueue.NewTyped[reconcile.Request]()}
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "test-ns",
				Labels: map[string]string{
					"postgres-operator.crunchydata.com/cluster": "my-cluster",
				},
			},
		}
		h.Generic(ctx, event.GenericEvent{Object: secret}, queue)
		assert.Equal(t, queue.Len(), 1)

		item, _ := queue.Get()
		assert.Equal(t, item, reconcile.Request{NamespacedName: client.ObjectKey{
			Namespace: "test-ns",
			Name:      "my-cluster",
		}})
		queue.Done(item)
	})
}

func TestWatchPodsUpdate(t *testing.T) {
	ctx := t.Context()
	queue := &controllertest.Queue{TypedInterface: workqueue.NewTyped[reconcile.Request]()}
	reconciler := &Reconciler{}

	update := reconciler.watchPods().UpdateFunc
	assert.Assert(t, update != nil)

	// No metadata; no reconcile.
	update(ctx, event.UpdateEvent{
		ObjectOld: &corev1.Pod{},
		ObjectNew: &corev1.Pod{},
	}, queue)
	assert.Equal(t, queue.Len(), 0)

	// Cluster label, but nothing else; no reconcile.
	update(ctx, event.UpdateEvent{
		ObjectOld: &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{
					"postgres-operator.crunchydata.com/cluster": "starfish",
				},
			},
		},
		ObjectNew: &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{
					"postgres-operator.crunchydata.com/cluster": "starfish",
				},
			},
		},
	}, queue)
	assert.Equal(t, queue.Len(), 0)

	// Cluster standby leader changed; one reconcile by label.
	update(ctx, event.UpdateEvent{
		ObjectOld: &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					"status": `{"role":"standby_leader"}`,
				},
				Labels: map[string]string{
					"postgres-operator.crunchydata.com/cluster": "starfish",
				},
			},
		},
		ObjectNew: &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "some-ns",
				Labels: map[string]string{
					"postgres-operator.crunchydata.com/cluster": "starfish",
					"postgres-operator.crunchydata.com/role":    "master",
				},
			},
		},
	}, queue)
	assert.Equal(t, queue.Len(), 1)

	item, _ := queue.Get()
	expected := reconcile.Request{}
	expected.Namespace = "some-ns"
	expected.Name = "starfish"
	assert.Equal(t, item, expected)
	queue.Done(item)

	t.Run("PendingRestart", func(t *testing.T) {
		expected := reconcile.Request{}
		expected.Namespace = "some-ns"
		expected.Name = "starfish"

		base := &corev1.Pod{}
		base.Namespace = "some-ns"
		base.Labels = map[string]string{
			"postgres-operator.crunchydata.com/cluster": "starfish",
		}

		pending := base.DeepCopy()
		pending.Annotations = map[string]string{
			"status": `{"pending_restart":true}`,
		}

		// Newly pending; one reconcile by label.
		update(ctx, event.UpdateEvent{
			ObjectOld: base.DeepCopy(),
			ObjectNew: pending.DeepCopy(),
		}, queue)
		assert.Equal(t, queue.Len(), 1, "expected one reconcile")

		item, _ := queue.Get()
		assert.Equal(t, item, expected)
		queue.Done(item)

		// Still pending; one reconcile by label.
		update(ctx, event.UpdateEvent{
			ObjectOld: pending.DeepCopy(),
			ObjectNew: pending.DeepCopy(),
		}, queue)
		assert.Equal(t, queue.Len(), 1, "expected one reconcile")

		item, _ = queue.Get()
		assert.Equal(t, item, expected)
		queue.Done(item)

		// No longer pending; one reconcile by label.
		update(ctx, event.UpdateEvent{
			ObjectOld: pending.DeepCopy(),
			ObjectNew: base.DeepCopy(),
		}, queue)
		assert.Equal(t, queue.Len(), 1, "expected one reconcile")

		item, _ = queue.Get()
		assert.Equal(t, item, expected)
		queue.Done(item)
	})

	// Pod annotation with arbitrary key; no reconcile.
	update(ctx, event.UpdateEvent{
		ObjectOld: &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					"clortho": "vince",
				},
				Labels: map[string]string{
					"postgres-operator.crunchydata.com/cluster": "starfish",
				},
			},
		},
		ObjectNew: &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					"clortho": "vin",
				},
				Labels: map[string]string{
					"postgres-operator.crunchydata.com/cluster": "starfish",
				},
			},
		},
	}, queue)
	assert.Equal(t, queue.Len(), 0)

	// Pod annotation with suggested-pgdata-pvc-size; reconcile.
	update(ctx, event.UpdateEvent{
		ObjectOld: &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					"suggested-pgdata-pvc-size": "5000Mi",
				},
				Labels: map[string]string{
					"postgres-operator.crunchydata.com/cluster": "starfish",
				},
			},
		},
		ObjectNew: &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					"suggested-pgdata-pvc-size": "8000Mi",
				},
				Labels: map[string]string{
					"postgres-operator.crunchydata.com/cluster": "starfish",
				},
			},
		},
	}, queue)
	assert.Equal(t, queue.Len(), 1)
	item, _ = queue.Get()
	queue.Done(item)

	// A repo-host volume suggestion also triggers reconciliation.
	update(ctx, event.UpdateEvent{
		ObjectOld: &corev1.Pod{ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{"suggested-pgbackrest-repo1-pvc-size": "5Gi"},
			Labels:      map[string]string{"postgres-operator.crunchydata.com/cluster": "starfish"},
		}},
		ObjectNew: &corev1.Pod{ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{"suggested-pgbackrest-repo1-pvc-size": "8Gi"},
			Labels:      map[string]string{"postgres-operator.crunchydata.com/cluster": "starfish"},
		}},
	}, queue)
	assert.Equal(t, queue.Len(), 1)
}

func TestWatchAdditionalTrustedCASecrets(t *testing.T) {
	ctx := t.Context()

	scheme := runtime.NewScheme()
	assert.NilError(t, corev1.AddToScheme(scheme))
	assert.NilError(t, v1beta1.AddToScheme(scheme))

	referencing := &v1beta1.PostgresCluster{
		ObjectMeta: metav1.ObjectMeta{Namespace: "ns1", Name: "referencing-cluster"},
		Spec: v1beta1.PostgresClusterSpec{
			TLS: &v1beta1.TLSSpec{
				AdditionalTrustedCAs: []corev1.LocalObjectReference{{Name: "some-ca"}},
			},
		},
	}
	notReferencing := &v1beta1.PostgresCluster{
		ObjectMeta: metav1.ObjectMeta{Namespace: "ns1", Name: "other-cluster"},
	}

	cc := fake.NewClientBuilder().WithScheme(scheme).
		WithIndex(&v1beta1.PostgresCluster{},
			v1beta1.IndexFieldAdditionalTrustedCASecrets,
			v1beta1.AdditionalTrustedCASecretsIndexerFunc).
		WithObjects(referencing, notReferencing).
		Build()

	reconciler := &Reconciler{Client: cc}
	h := reconciler.watchAdditionalTrustedCASecrets()

	t.Run("secret not referenced by any cluster enqueues nothing", func(t *testing.T) {
		queue := &controllertest.Queue{TypedInterface: workqueue.NewTyped[reconcile.Request]()}
		secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: "ns1", Name: "unrelated-ca"}}
		h.Create(ctx, event.CreateEvent{Object: secret}, queue)
		assert.Equal(t, queue.Len(), 0)
	})

	t.Run("secret referenced by a cluster enqueues that cluster", func(t *testing.T) {
		queue := &controllertest.Queue{TypedInterface: workqueue.NewTyped[reconcile.Request]()}
		secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: "ns1", Name: "some-ca"}}
		h.Create(ctx, event.CreateEvent{Object: secret}, queue)
		assert.Equal(t, queue.Len(), 1)

		item, _ := queue.Get()
		assert.Equal(t, item, reconcile.Request{NamespacedName: client.ObjectKey{
			Namespace: "ns1",
			Name:      "referencing-cluster",
		}})
		queue.Done(item)
	})

	t.Run("secret in a different namespace enqueues nothing", func(t *testing.T) {
		queue := &controllertest.Queue{TypedInterface: workqueue.NewTyped[reconcile.Request]()}
		secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: "ns2", Name: "some-ca"}}
		h.Create(ctx, event.CreateEvent{Object: secret}, queue)
		assert.Equal(t, queue.Len(), 0)
	})
}
