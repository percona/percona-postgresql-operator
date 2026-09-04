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
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/percona/percona-postgresql-operator/v3/internal/naming"
	"github.com/percona/percona-postgresql-operator/v3/percona/logcollector/logrotate"
	pNaming "github.com/percona/percona-postgresql-operator/v3/percona/naming"
	"github.com/percona/percona-postgresql-operator/v3/percona/version"
	v2 "github.com/percona/percona-postgresql-operator/v3/pkg/apis/pgv2.percona.com/v2"
	crunchyv1beta1 "github.com/percona/percona-postgresql-operator/v3/pkg/apis/upstream.pgv2.percona.com/v1beta1"
)

func TestReconcileLogRotate(t *testing.T) {
	const (
		clusterName = "my-cluster"
		namespace   = "default"
	)

	require.NoError(t, v2.AddToScheme(scheme.Scheme))

	logCollectorSpec := func(lr *v2.LogRotateSpec) *v2.LogCollectorSpec {
		return &v2.LogCollectorSpec{
			Enabled:   new(true),
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
				Enabled:   new(false),
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
				Enabled:       new(true),
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
				require.NoError(t, controllerutil.SetControllerReference(tt.cr, tt.wantCM, scheme.Scheme))
				got.ResourceVersion = ""
				assert.Equal(t, tt.wantCM, got)
			}

			require.NotEmpty(t, tt.cr.Spec.InstanceSets)
			set := tt.cr.Spec.InstanceSets[0]

			var wantSidecars []corev1.Container
			var wantVolumes []corev1.Volume
			var wantInitContainers []corev1.Container
			if tt.wantAppended {
				var err error
				wantSidecars, err = instanceContainers(tt.cr)
				require.NoError(t, err)
				wantVolumes = volumes(tt.cr)
				wantInitContainers = instanceInitContainers(tt.cr)
			}
			assert.Equal(t, wantSidecars, set.Sidecars)
			assert.Equal(t, wantVolumes, set.SidecarVolumes)
			assert.Equal(t, wantInitContainers, set.InitContainers)
		})
	}
}

// TestExtraConfigRollsPods checks that editing the extraConfig ConfigMap changes
// the hash stamped on the instance sets, so the pods roll.
func TestExtraConfigRollsPods(t *testing.T) {
	const (
		clusterName = "my-cluster"
		namespace   = "default"
		extraCMName = "my-logrotate-config"
	)

	require.NoError(t, v2.AddToScheme(scheme.Scheme))

	newCR := func() *v2.PerconaPGCluster {
		return &v2.PerconaPGCluster{
			ObjectMeta: metav1.ObjectMeta{Name: clusterName, Namespace: namespace},
			Spec: v2.PerconaPGClusterSpec{
				CRVersion: version.Version(),
				LogCollector: &v2.LogCollectorSpec{
					Enabled: new(true),
					Image:   "log-test-image",
					LogRotate: &v2.LogRotateSpec{
						ExtraConfig: corev1.LocalObjectReference{Name: extraCMName},
					},
				},
				InstanceSets: v2.PGInstanceSets{{Name: "instance1"}},
			},
		}
	}

	extraCM := func(data string) *corev1.ConfigMap {
		return &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: extraCMName, Namespace: namespace},
			Data:       map[string]string{"custom.conf": data},
		}
	}

	hashOf := func(cr *v2.PerconaPGCluster) string {
		require.NotEmpty(t, cr.Spec.InstanceSets)
		set := cr.Spec.InstanceSets[0]
		require.NotNil(t, set.Metadata)
		return set.Metadata.Annotations[pNaming.AnnotationLogCollectorConfigHash]
	}

	c := fake.NewClientBuilder().WithScheme(scheme.Scheme).
		WithObjects(extraCM("size 10M")).Build()

	cr1 := newCR()
	require.NoError(t, Reconcile(t.Context(), c, cr1))
	hash1 := hashOf(cr1)
	require.NotEmpty(t, hash1)

	// Edit the referenced ConfigMap; the hash must change so the pods roll.
	require.NoError(t, c.Update(t.Context(), extraCM("size 20M")))

	cr2 := newCR()
	require.NoError(t, Reconcile(t.Context(), c, cr2))
	hash2 := hashOf(cr2)

	assert.NotEqual(t, hash1, hash2, "config hash should change when extraConfig contents change")
}

func TestResolveDefaultEnabled(t *testing.T) {
	const (
		clusterName = "test-cluster"
		namespace   = "default"
	)

	require.NoError(t, v2.AddToScheme(scheme.Scheme))
	require.NoError(t, crunchyv1beta1.AddToScheme(scheme.Scheme))

	newCR := func(enabled *bool) *v2.PerconaPGCluster {
		return &v2.PerconaPGCluster{
			ObjectMeta: metav1.ObjectMeta{Name: clusterName, Namespace: namespace},
			Spec: v2.PerconaPGClusterSpec{
				CRVersion: version.Version(),
				LogCollector: &v2.LogCollectorSpec{
					Enabled: enabled,
					Image:   "log-test-image",
				},
				InstanceSets: v2.PGInstanceSets{{Name: "instance1"}},
			},
		}
	}

	postgresClusterWithSidecar := &crunchyv1beta1.PostgresCluster{
		ObjectMeta: metav1.ObjectMeta{Name: clusterName, Namespace: namespace},
		Spec: crunchyv1beta1.PostgresClusterSpec{
			InstanceSets: []crunchyv1beta1.PostgresInstanceSetSpec{
				{
					Containers: []corev1.Container{
						{Name: "logs"},
					},
				},
			},
		},
	}

	postgresClusterWithoutSidecar := &crunchyv1beta1.PostgresCluster{
		ObjectMeta: metav1.ObjectMeta{Name: clusterName, Namespace: namespace},
		Spec: crunchyv1beta1.PostgresClusterSpec{
			InstanceSets: []crunchyv1beta1.PostgresInstanceSetSpec{
				{
					Containers: []corev1.Container{},
				},
			},
		},
	}

	tests := map[string]struct {
		cr              *v2.PerconaPGCluster
		existingCluster *crunchyv1beta1.PostgresCluster
		wantEnabled     *bool
	}{
		"nil LogCollector is left unchanged": {
			cr: &v2.PerconaPGCluster{
				ObjectMeta: metav1.ObjectMeta{Name: clusterName, Namespace: namespace},
				Spec: v2.PerconaPGClusterSpec{
					CRVersion:    version.Version(),
					LogCollector: nil,
					InstanceSets: v2.PGInstanceSets{{Name: "instance1"}},
				},
			},
			wantEnabled: nil,
		},
		"explicit true is not overridden": {
			cr:          newCR(new(true)),
			wantEnabled: new(true),
		},
		"explicit false is not overridden": {
			cr:          newCR(new(false)),
			wantEnabled: new(false),
		},
		"new cluster defaults to enabled": {
			cr:          newCR(nil),
			wantEnabled: new(true),
		},
		"existing cluster with sidecar preserves enabled": {
			cr:              newCR(nil),
			existingCluster: postgresClusterWithSidecar,
			wantEnabled:     new(true),
		},
		"existing cluster without sidecar preserves disabled": {
			cr:              newCR(nil),
			existingCluster: postgresClusterWithoutSidecar,
			wantEnabled:     new(false),
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			builder := fake.NewClientBuilder().WithScheme(scheme.Scheme)
			if tt.existingCluster != nil {
				builder = builder.WithObjects(tt.existingCluster)
			}
			c := builder.Build()

			err := resolveDefaultEnabled(t.Context(), c, tt.cr)
			require.NoError(t, err)

			if tt.cr.Spec.LogCollector == nil {
				return
			}
			if tt.wantEnabled == nil {
				assert.Nil(t, tt.cr.Spec.LogCollector.Enabled)
			} else {
				require.NotNil(t, tt.cr.Spec.LogCollector.Enabled)
				assert.Equal(t, *tt.wantEnabled, *tt.cr.Spec.LogCollector.Enabled)
			}
		})
	}
}
