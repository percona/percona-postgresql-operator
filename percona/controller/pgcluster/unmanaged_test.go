package pgcluster

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/percona/percona-postgresql-operator/v3/percona/utils/registry"
	"github.com/percona/percona-postgresql-operator/v3/pkg/apis/upstream.pgv2.percona.com/v1beta1"
)

func TestReconcileSkipsUnmanagedCluster(t *testing.T) {
	ctx := t.Context()

	tests := []struct {
		name             string
		unmanaged        bool
		expectedImage    string
		expectedPause    bool
		expectedWatchers int
	}{
		{
			name:             "unmanaged cluster",
			unmanaged:        true,
			expectedImage:    "image1",
			expectedPause:    true,
			expectedWatchers: 0,
		},
		{
			name:             "managed cluster",
			unmanaged:        false,
			expectedImage:    "image2",
			expectedPause:    false,
			expectedWatchers: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cr, err := readDefaultCR("unmanaged-cluster", "unmanaged-cluster")
			require.NoError(t, err)
			cr.Spec.Image = "image1"
			cr.Spec.Unmanaged = new(false)

			cl, err := buildFakeClient(ctx, cr)
			require.NoError(t, err)

			r := fakeClientReconciler(cl)

			_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(cr)})
			require.NoError(t, err)

			postgresCluster := &v1beta1.PostgresCluster{
				ObjectMeta: metav1.ObjectMeta{Name: cr.Name, Namespace: cr.Namespace},
			}
			require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(postgresCluster), postgresCluster))
			assert.Equal(t, "image1", postgresCluster.Spec.Image)

			require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(cr), cr))
			cr.Spec.Image = "image2"
			cr.Spec.Unmanaged = new(tt.unmanaged)
			require.NoError(t, cl.Update(ctx, cr))

			_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(cr)})
			require.NoError(t, err)

			require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(postgresCluster), postgresCluster))
			assert.Equal(t, tt.expectedImage, postgresCluster.Spec.Image)
			assert.Equal(t, tt.expectedPause, ptr.Deref(postgresCluster.Spec.Paused, false))
			assert.Len(t, r.Watchers.Names(), tt.expectedWatchers)
		})
	}
}

func fakeClientReconciler(cl client.Client) *PGClusterReconciler {
	return &PGClusterReconciler{
		Client:               cl,
		Platform:             "unknown",
		KubeVersion:          "1.36",
		Cron:                 NewCronRegistry(),
		Watchers:             registry.New(),
		Recorder:             new(record.FakeRecorder),
		ExternalChan:         make(chan event.GenericEvent, 1),
		StopExternalWatchers: make(chan event.DeleteEvent, 1),
	}
}
