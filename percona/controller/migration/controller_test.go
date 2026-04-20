package migration

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/percona/percona-postgresql-operator/v2/internal/naming"
	"github.com/percona/percona-postgresql-operator/v2/pkg/apis/upstream.pgv2.percona.com/v1beta1"
)

const (
	testName      = "mycluster"
	testNamespace = "pgo"
	legacyUID     = types.UID("legacy-uid-0000")
	newUID        = types.UID("new-uid-0001")
)

func testScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	require.NoError(t, scheme.AddToScheme(s))
	require.NoError(t, v1beta1.AddToScheme(s))
	// Register the legacy GVK against Unstructured so the fake client's
	// RESTMapper can resolve it.
	s.AddKnownTypeWithName(LegacyGVK, &unstructured.Unstructured{})
	s.AddKnownTypeWithName(LegacyGVK.GroupVersion().WithKind("PostgresClusterList"), &unstructured.UnstructuredList{})
	return s
}

// testRESTMapper wires every GVK used by the migration controller into a
// RESTMapper so the fake client's `RESTMapping` calls succeed — the default
// in-memory mapper otherwise claims no kinds exist.
func testRESTMapper() meta.RESTMapper {
	m := meta.NewDefaultRESTMapper(nil)
	kinds := append([]schema.GroupVersionKind{
		LegacyGVK,
		v1beta1.GroupVersion.WithKind("PostgresCluster"),
	}, childKinds...)
	for _, gvk := range kinds {
		m.Add(gvk, meta.RESTScopeNamespace)
	}
	return m
}

func newLegacyPostgresCluster(finalizers ...string) *unstructured.Unstructured {
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(LegacyGVK)
	u.SetName(testName)
	u.SetNamespace(testNamespace)
	u.SetUID(legacyUID)
	u.SetFinalizers(finalizers)
	return u
}

func newUpstreamPostgresCluster() *v1beta1.PostgresCluster {
	return &v1beta1.PostgresCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testName,
			Namespace: testNamespace,
			UID:       newUID,
		},
	}
}

func ownedByLegacy() []metav1.OwnerReference {
	tru := true
	return []metav1.OwnerReference{{
		APIVersion:         LegacyGVK.GroupVersion().String(),
		Kind:               LegacyGVK.Kind,
		Name:               testName,
		UID:                legacyUID,
		Controller:         &tru,
		BlockOwnerDeletion: &tru,
	}}
}

func clusterLabels() map[string]string {
	return map[string]string{naming.LabelCluster: testName}
}

func TestReconcile_WaitsForNewPostgresCluster(t *testing.T) {
	ctx := context.Background()
	s := testScheme(t)

	legacy := newLegacyPostgresCluster(MigrationFinalizer)
	c := fake.NewClientBuilder().WithScheme(s).WithRESTMapper(testRESTMapper()).WithObjects(legacy).Build()

	r := &Reconciler{Client: c}
	res, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: client.ObjectKey{Name: testName, Namespace: testNamespace}})
	require.NoError(t, err)
	require.True(t, res.RequeueAfter > 0, "expected a requeue while waiting for new PostgresCluster")

	// Legacy object must not have been deleted while we are waiting.
	got := &unstructured.Unstructured{}
	got.SetGroupVersionKind(LegacyGVK)
	err = c.Get(ctx, client.ObjectKeyFromObject(legacy), got)
	require.NoError(t, err)
}

func TestReconcile_AddsMigrationFinalizer(t *testing.T) {
	ctx := context.Background()
	s := testScheme(t)

	legacy := newLegacyPostgresCluster() // no finalizer yet
	newPC := newUpstreamPostgresCluster()
	c := fake.NewClientBuilder().WithScheme(s).WithRESTMapper(testRESTMapper()).WithObjects(legacy, newPC).Build()

	r := &Reconciler{Client: c}
	_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: client.ObjectKey{Name: testName, Namespace: testNamespace}})
	require.NoError(t, err)

	got := &unstructured.Unstructured{}
	got.SetGroupVersionKind(LegacyGVK)
	require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(legacy), got))
	require.Contains(t, got.GetFinalizers(), MigrationFinalizer)
}

func TestReconcile_ReparentsAndDeletes(t *testing.T) {
	ctx := context.Background()
	s := testScheme(t)

	legacy := newLegacyPostgresCluster(MigrationFinalizer, naming.Finalizer)
	newPC := newUpstreamPostgresCluster()

	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:            testName + "-instance1",
			Namespace:       testNamespace,
			Labels:          clusterLabels(),
			OwnerReferences: ownedByLegacy(),
		},
	}
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:            testName + "-primary",
			Namespace:       testNamespace,
			Labels:          clusterLabels(),
			OwnerReferences: ownedByLegacy(),
		},
	}
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:            testName + "-pgbackrest",
			Namespace:       testNamespace,
			Labels:          clusterLabels(),
			OwnerReferences: ownedByLegacy(),
		},
	}
	// A Service in the same namespace but belonging to a different cluster -
	// must not be touched.
	otherSvc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "other-primary",
			Namespace: testNamespace,
			Labels:    map[string]string{naming.LabelCluster: "other"},
		},
	}

	c := fake.NewClientBuilder().WithScheme(s).WithRESTMapper(testRESTMapper()).
		WithObjects(legacy, newPC, sts, svc, secret, otherSvc).
		Build()

	r := &Reconciler{Client: c}
	_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: client.ObjectKey{Name: testName, Namespace: testNamespace}})
	require.NoError(t, err)

	// Legacy object is gone.
	got := &unstructured.Unstructured{}
	got.SetGroupVersionKind(LegacyGVK)
	err = c.Get(ctx, client.ObjectKeyFromObject(legacy), got)
	require.True(t, apierrors.IsNotFound(err), "legacy object should be deleted, got err=%v", err)

	// Children point at the new PostgresCluster.
	gotSts := &appsv1.StatefulSet{}
	require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(sts), gotSts))
	assertOwnedByTagged(t, "StatefulSet", gotSts.OwnerReferences, newUID)

	gotSvc := &corev1.Service{}
	require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(svc), gotSvc))
	assertOwnedByTagged(t, "Service", gotSvc.OwnerReferences, newUID)

	gotSecret := &corev1.Secret{}
	require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(secret), gotSecret))
	assertOwnedByTagged(t, "Secret", gotSecret.OwnerReferences, newUID)

	// The unrelated Service is untouched.
	other := &corev1.Service{}
	require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(otherSvc), other))
	require.Empty(t, other.OwnerReferences)
}

func TestReconcile_Idempotent(t *testing.T) {
	ctx := context.Background()
	s := testScheme(t)

	newPC := newUpstreamPostgresCluster()
	// Legacy already gone; only the new PostgresCluster exists. A stale
	// reconcile should be a no-op.
	c := fake.NewClientBuilder().WithScheme(s).WithRESTMapper(testRESTMapper()).WithObjects(newPC).Build()

	r := &Reconciler{Client: c}
	res, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: client.ObjectKey{Name: testName, Namespace: testNamespace}})
	require.NoError(t, err)
	require.Zero(t, res)
}

func assertOwnedBy(t *testing.T, refs []metav1.OwnerReference, uid types.UID) {
	t.Helper()
	assertOwnedByTagged(t, "resource", refs, uid)
}

func assertOwnedByTagged(t *testing.T, label string, refs []metav1.OwnerReference, uid types.UID) {
	t.Helper()
	for _, r := range refs {
		if r.UID == uid {
			return
		}
	}
	t.Fatalf("%s: expected owner reference with UID %s, got %+v", label, uid, refs)
}
