package logcollector

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/percona/percona-postgresql-operator/v2/internal/naming"
	"github.com/percona/percona-postgresql-operator/v2/percona/logcollector/logrotate"
	"github.com/percona/percona-postgresql-operator/v2/percona/version"
	v2 "github.com/percona/percona-postgresql-operator/v2/pkg/apis/pgv2.percona.com/v2"
)

func TestReconcileLogRotate(t *testing.T) {
	const (
		clusterName = "my-cluster"
		namespace   = "default"
	)

	logCollectorSpec := func(lr *v2.LogRotateSpec) *v2.LogCollectorSpec {
		return &v2.LogCollectorSpec{
			Enabled:   true,
			Image:     "log-test-image",
			LogRotate: lr,
		}
	}

	newCR := func(crVersion string, lc *v2.LogCollectorSpec, instances ...string) *v2.PerconaPGCluster {
		var sets v2.PGInstanceSets
		for _, name := range instances {
			sets = append(sets, v2.PGInstanceSetSpec{Name: name})
		}
		return &v2.PerconaPGCluster{
			ObjectMeta: metav1.ObjectMeta{Name: clusterName, Namespace: namespace},
			Spec: v2.PerconaPGClusterSpec{
				CRVersion:    crVersion,
				LogCollector: lc,
				InstanceSets: sets,
			},
		}
	}

	cmKey := types.NamespacedName{
		Name:      logrotate.ConfigMapName(clusterName),
		Namespace: namespace,
	}

	managedCM := func(data string) *corev1.ConfigMap {
		return &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      cmKey.Name,
				Namespace: cmKey.Namespace,
				Labels:    map[string]string{naming.LabelCluster: clusterName},
			},
			Data: map[string]string{logrotate.PostgresConfig: data},
		}
	}
	preExisting := func() *corev1.ConfigMap { return managedCM("old-config") }

	tests := map[string]struct {
		initialCM    *corev1.ConfigMap
		cr           *v2.PerconaPGCluster
		wantCM       *corev1.ConfigMap
		wantAppended bool
	}{
		"created when configuration is set": {
			cr:           newCR(version.Version(), logCollectorSpec(&v2.LogRotateSpec{Configuration: "new-config"}), "instance1"),
			wantCM:       managedCM("new-config"),
			wantAppended: true,
		},
		"updated when data differs": {
			initialCM:    preExisting(),
			cr:           newCR(version.Version(), logCollectorSpec(&v2.LogRotateSpec{Configuration: "updated-config"}), "instance1"),
			wantCM:       managedCM("updated-config"),
			wantAppended: true,
		},
		"deleted when LogRotate is nil": {
			initialCM:    preExisting(),
			cr:           newCR(version.Version(), logCollectorSpec(nil), "instance1"),
			wantAppended: true,
		},
		"deleted when Configuration is empty; schedule propagates": {
			initialCM:    preExisting(),
			cr:           newCR(version.Version(), logCollectorSpec(&v2.LogRotateSpec{Schedule: "*/5 * * * *"}), "instance1"),
			wantAppended: true,
		},
		"deleted when log collector is disabled": {
			initialCM: preExisting(),
			cr: newCR(version.Version(), &v2.LogCollectorSpec{
				Enabled:   false,
				Image:     "log-test-image",
				LogRotate: &v2.LogRotateSpec{Configuration: "ignored"},
			}, "instance1"),
		},
		"deleted when log collector is nil": {
			initialCM: preExisting(),
			cr:        newCR(version.Version(), nil, "instance1"),
		},
		"no-op when disabled and CM absent": {
			cr: newCR(version.Version(), nil, "instance1"),
		},
		"version gate skips CM and sidecars": {
			cr: newCR("2.9.0", &v2.LogCollectorSpec{
				Enabled:       true,
				Image:         "log-test-image",
				Configuration: "cfg",
				LogRotate:     &v2.LogRotateSpec{Configuration: "lr"},
			}, "instance1"),
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			builder := fake.NewClientBuilder().WithScheme(scheme.Scheme)
			if tt.initialCM != nil {
				builder = builder.WithObjects(tt.initialCM)
			}
			c := builder.Build()

			err := Reconcile(t.Context(), c, tt.cr)
			require.NoError(t, err)
			got := &corev1.ConfigMap{}
			err = c.Get(t.Context(), cmKey, got)
			if tt.wantCM == nil {
				assert.True(t, k8serrors.IsNotFound(err), "expected CM absent, got err=%v", err)
			} else {
				require.NoError(t, err)
				got.ResourceVersion = ""
				assert.Equal(t, tt.wantCM, got)
			}

			require.NotEmpty(t, tt.cr.Spec.InstanceSets)
			set := tt.cr.Spec.InstanceSets[0]

			var wantSidecars []corev1.Container
			var wantVolumes []corev1.Volume
			if tt.wantAppended {
				var err error
				wantSidecars, err = instanceContainers(tt.cr)
				require.NoError(t, err)
				wantVolumes = volumes(tt.cr)
			}
			assert.Equal(t, wantSidecars, set.Sidecars)
			assert.Equal(t, wantVolumes, set.SidecarVolumes)
		})
	}
}
