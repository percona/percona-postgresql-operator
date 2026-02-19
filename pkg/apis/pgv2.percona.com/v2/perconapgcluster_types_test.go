package v2

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	k8sptr "k8s.io/utils/ptr"

	"github.com/percona/percona-postgresql-operator/v2/internal/naming"
	"github.com/percona/percona-postgresql-operator/v2/percona/version"
	crunchyv1beta1 "github.com/percona/percona-postgresql-operator/v2/pkg/apis/postgres-operator.crunchydata.com/v1beta1"
)

func TestPerconaPGCluster_Default(t *testing.T) {
	// cr.Default() should not panic on PerconaPGCluster with empty fields
	new(PerconaPGCluster).Default()
}

func TestPerconaPGCluster_BackupsEnabled(t *testing.T) {
	trueVal := true
	falseVal := false

	tests := map[string]struct {
		spec     PerconaPGClusterSpec
		expected bool
	}{

		"Enabled is nil, should return true because default is true": {
			spec:     PerconaPGClusterSpec{Backups: Backups{Enabled: nil}},
			expected: true,
		},
		"Enabled is true, should return true": {
			spec:     PerconaPGClusterSpec{Backups: Backups{Enabled: &trueVal}},
			expected: true,
		},
		"Enabled is false, should return false": {
			spec:     PerconaPGClusterSpec{Backups: Backups{Enabled: &falseVal}},
			expected: false,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			actual := tt.spec.Backups.IsEnabled()
			assert.Equal(t, tt.expected, actual)
		})
	}
}

func TestPerconaPGCluster_Proxy(t *testing.T) {
	t.Run("Proxy is nil, CRVersion < 2.9.0", func(t *testing.T) {
		cr := new(PerconaPGCluster)
		cr.Spec.CRVersion = "2.8.0"
		cr.Default()
		assert.NotNil(t, cr.Spec.Proxy)
		assert.NotNil(t, cr.Spec.Proxy.PGBouncer)
		assert.NotNil(t, cr.Spec.Proxy.PGBouncer.Metadata)
		assert.NotNil(t, cr.Spec.Proxy.PGBouncer.Metadata.Labels)
		assert.Equal(t, cr.Spec.CRVersion, cr.Spec.Proxy.PGBouncer.Metadata.Labels[LabelOperatorVersion])
	})
	t.Run("Proxy is nil, CRVersion >= 2.9.0", func(t *testing.T) {
		cr := new(PerconaPGCluster)
		cr.Spec.CRVersion = "2.9.0"
		cr.Default()
		assert.Nil(t, cr.Spec.Proxy)
	})
	t.Run("Proxy is not nil", func(t *testing.T) {
		cr := new(PerconaPGCluster)
		cr.Spec.Proxy = &PGProxySpec{
			PGBouncer: &PGBouncerSpec{},
		}
		cr.Spec.CRVersion = "2.9.0"
		cr.Default()
		assert.NotNil(t, cr.Spec.Proxy)
		assert.NotNil(t, cr.Spec.Proxy.PGBouncer)
		assert.NotNil(t, cr.Spec.Proxy.PGBouncer.Metadata)
		assert.NotNil(t, cr.Spec.Proxy.PGBouncer.Metadata.Labels)
		assert.Equal(t, cr.Spec.CRVersion, cr.Spec.Proxy.PGBouncer.Metadata.Labels[LabelOperatorVersion])
	})
}

func TestPerconaPGCluster_PostgresImage(t *testing.T) {
	cluster := new(PerconaPGCluster)
	cluster.Default()

	postgresVersion := 16
	testDefaultImage := fmt.Sprintf("test_default_image:%d", postgresVersion)
	testSpecificImage := fmt.Sprintf("test_defined_image:%d", postgresVersion)
	testEnv := fmt.Sprintf("RELATED_IMAGE_POSTGRES_%d", postgresVersion)

	cluster.Spec.PostgresVersion = postgresVersion

	tests := map[string]struct {
		expectedImage string
		setImage      string
		envImage      string
	}{
		"Spec.Image should be empty by default": {
			expectedImage: "",
			setImage:      "",
			envImage:      "",
		},
		"Spec.Image should use env variables if present": {
			expectedImage: testDefaultImage,
			setImage:      "",
			envImage:      testDefaultImage,
		},
		"Spec.Image should use defined variable": {
			expectedImage: testSpecificImage,
			setImage:      testSpecificImage,
			envImage:      testDefaultImage,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {

			cluster.Spec.Image = tt.setImage

			if tt.envImage != "" {
				err := os.Setenv(testEnv, tt.envImage)

				if err != nil {
					t.Fatalf("Failed to set %s env variable: %v", testEnv, err)
				}

				defer func() {
					err := os.Unsetenv(testEnv)
					if err != nil {
						t.Errorf("Failed to unset %s env variable: %v", testEnv, err)
					}
				}()
			}

			assert.Equal(t, tt.expectedImage, cluster.PostgresImage())
		})
	}
}

func TestPerconaPGCluster_ToCrunchy(t *testing.T) {
	scheme := runtime.NewScheme()
	err := corev1.AddToScheme(scheme)
	require.NoError(t, err)
	err = crunchyv1beta1.AddToScheme(scheme)
	require.NoError(t, err)
	err = AddToScheme(scheme)
	require.NoError(t, err)

	ctx := context.Background()

	tests := map[string]struct {
		name                     string
		expectedPerconaPGCluster *PerconaPGCluster
		inputPostgresCluster     *crunchyv1beta1.PostgresCluster
		expectedError            bool
		assertClusterFunc        func(t *testing.T, actual *crunchyv1beta1.PostgresCluster, expected *PerconaPGCluster)
	}{
		"creates new PostgresCluster when nil input": {
			expectedPerconaPGCluster: &PerconaPGCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "test-namespace",
				},
				Spec: PerconaPGClusterSpec{
					CRVersion:       version.Version(),
					PostgresVersion: 15,
					Expose: &ServiceExpose{
						Type:              "LoadBalancer",
						LoadBalancerClass: &[]string{"someloadbalancerclass"}[0],
					},
					ExposeReplicas: &ServiceExpose{
						Type:              "LoadBalancer",
						LoadBalancerClass: &[]string{"someloadbalancerclass"}[0],
					},
					InstanceSets: PGInstanceSets{
						{
							Name:     "instance1",
							Replicas: &[]int32{1}[0],
							DataVolumeClaimSpec: corev1.PersistentVolumeClaimSpec{
								AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
							},
						},
					},
					Backups: Backups{
						PGBackRest: PGBackRestArchive{
							Repos: []crunchyv1beta1.PGBackRestRepo{
								{Name: "repo1"},
							},
						},
					},
				},
			},
			assertClusterFunc: func(t *testing.T, actual *crunchyv1beta1.PostgresCluster, expected *PerconaPGCluster) {
				assert.Equal(t, expected.Name, actual.Name)
				assert.Equal(t, expected.Namespace, actual.Namespace)
				assert.Equal(t, []string{naming.Finalizer}, actual.Finalizers)
				assert.Equal(t, expected.Spec.PostgresVersion, actual.Spec.PostgresVersion)
				assert.Equal(t, expected.Spec.CRVersion, actual.Labels[LabelOperatorVersion])
				assert.Len(t, actual.Spec.InstanceSets, 1)
				assert.Len(t, expected.Spec.InstanceSets, len(actual.Spec.InstanceSets))
				assert.Equal(t, expected.Spec.InstanceSets[0].Name, actual.Spec.InstanceSets[0].Name)

				assert.Equal(t, expected.Spec.Expose.Type, actual.Spec.Service.Type)
				assert.NotNil(t, actual.Spec.Service.LoadBalancerClass)
				assert.Equal(t, expected.Spec.Expose.LoadBalancerClass, actual.Spec.Service.LoadBalancerClass)

				assert.Equal(t, expected.Spec.ExposeReplicas.Type, actual.Spec.ReplicaService.Type)
				assert.NotNil(t, actual.Spec.ReplicaService.LoadBalancerClass)
				assert.Equal(t, expected.Spec.ExposeReplicas.LoadBalancerClass, actual.Spec.ReplicaService.LoadBalancerClass)
				assert.NotNil(t, actual.Spec.Backups.TrackLatestRestorableTime)
				assert.True(t, *actual.Spec.Backups.TrackLatestRestorableTime)
			},
		},
		"updates existing PostgresCluster": {
			expectedPerconaPGCluster: &PerconaPGCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "test-namespace",
					Labels: map[string]string{
						"test-label": "test-value",
					},
					Annotations: map[string]string{
						"test-annotation": "test-value",
					},
				},
				Spec: PerconaPGClusterSpec{
					CRVersion:       "2.5.0",
					PostgresVersion: 14,
					Port:            &[]int32{5432}[0],
					TLSOnly:         true,
					InstanceSets: PGInstanceSets{
						{
							Name:     "instance1",
							Replicas: &[]int32{2}[0],
							DataVolumeClaimSpec: corev1.PersistentVolumeClaimSpec{
								AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
							},
						},
					},
					Backups: Backups{
						PGBackRest: PGBackRestArchive{
							Repos: []crunchyv1beta1.PGBackRestRepo{
								{Name: "repo1"},
							},
						},
					},
				},
			},
			inputPostgresCluster: &crunchyv1beta1.PostgresCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "existing-cluster",
					Namespace: "test-namespace",
				},
			},
			assertClusterFunc: func(t *testing.T, actual *crunchyv1beta1.PostgresCluster, expected *PerconaPGCluster) {
				assert.Equal(t, expected.Spec.PostgresVersion, actual.Spec.PostgresVersion)
				assert.Equal(t, expected.Spec.Port, actual.Spec.Port)
				assert.Equal(t, expected.Spec.TLSOnly, actual.Spec.TLSOnly)
				assert.Equal(t, "test-value", actual.Labels["test-label"])
				assert.Equal(t, expected.Spec.CRVersion, actual.Labels[LabelOperatorVersion])
			},
		},
		"handles PMM enabled scenario": {
			expectedPerconaPGCluster: &PerconaPGCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "test-namespace",
				},
				Spec: PerconaPGClusterSpec{
					CRVersion:       "2.5.0",
					PostgresVersion: 15,
					PMM: &PMMSpec{
						Enabled:     true,
						QuerySource: PgStatStatements,
					},
					InstanceSets: PGInstanceSets{
						{
							Name:     "instance1",
							Replicas: &[]int32{1}[0],
							DataVolumeClaimSpec: corev1.PersistentVolumeClaimSpec{
								AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
							},
						},
					},
					Backups: Backups{
						PGBackRest: PGBackRestArchive{
							Repos: []crunchyv1beta1.PGBackRestRepo{
								{Name: "repo1"},
							},
						},
					},
				},
			},
			assertClusterFunc: func(t *testing.T, actual *crunchyv1beta1.PostgresCluster, _ *PerconaPGCluster) {
				hasMonitoringUser := false
				for _, user := range actual.Spec.Users {
					if user.Name == UserMonitoring {
						hasMonitoringUser = true
						break
					}
				}
				assert.True(t, hasMonitoringUser)
			},
		},
		"handles AutoCreateUserSchema annotation": {
			expectedPerconaPGCluster: &PerconaPGCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "test-namespace",
				},
				Spec: PerconaPGClusterSpec{
					CRVersion:            "2.5.0",
					PostgresVersion:      15,
					AutoCreateUserSchema: &[]bool{true}[0],
					InstanceSets: PGInstanceSets{
						{
							Name:     "instance1",
							Replicas: &[]int32{1}[0],
							DataVolumeClaimSpec: corev1.PersistentVolumeClaimSpec{
								AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
							},
						},
					},
					Backups: Backups{
						PGBackRest: PGBackRestArchive{
							Repos: []crunchyv1beta1.PGBackRestRepo{
								{Name: "repo1"},
							},
						},
					},
				},
			},
			assertClusterFunc: func(t *testing.T, actual *crunchyv1beta1.PostgresCluster, _ *PerconaPGCluster) {
				assert.Equal(t, "true", actual.Annotations[naming.AutoCreateUserSchemaAnnotation])
			},
		},
		"filters out reserved monitoring user": {
			expectedPerconaPGCluster: &PerconaPGCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "test-namespace",
				},
				Spec: PerconaPGClusterSpec{
					CRVersion:       "2.5.0",
					PostgresVersion: 15,
					Users: []crunchyv1beta1.PostgresUserSpec{
						{Name: "regular-user"},
						{Name: UserMonitoring},
						{Name: "another-user"},
					},
					InstanceSets: PGInstanceSets{
						{
							Name:     "instance1",
							Replicas: &[]int32{1}[0],
							DataVolumeClaimSpec: corev1.PersistentVolumeClaimSpec{
								AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
							},
						},
					},
					Backups: Backups{
						PGBackRest: PGBackRestArchive{
							Repos: []crunchyv1beta1.PGBackRestRepo{
								{Name: "repo1"},
							},
						},
					},
				},
			},
			assertClusterFunc: func(t *testing.T, result *crunchyv1beta1.PostgresCluster, original *PerconaPGCluster) {
				assert.Len(t, result.Spec.Users, 2)
				userNames := make([]string, len(result.Spec.Users))
				for i, user := range result.Spec.Users {
					userNames[i] = string(user.Name)
				}
				assert.True(t, contains(userNames, "regular-user"))
				assert.True(t, contains(userNames, "another-user"))
				assert.False(t, contains(userNames, UserMonitoring))
			},
		},
		"handles nil proxy": {
			expectedPerconaPGCluster: &PerconaPGCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "test-namespace",
				},
				Spec: PerconaPGClusterSpec{
					CRVersion:       "2.9.0",
					PostgresVersion: 15,
					Proxy:           nil,
					InstanceSets: PGInstanceSets{
						{
							Name:     "instance1",
							Replicas: &[]int32{1}[0],
							DataVolumeClaimSpec: corev1.PersistentVolumeClaimSpec{
								AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
							},
						},
					},
					Backups: Backups{
						PGBackRest: PGBackRestArchive{
							Repos: []crunchyv1beta1.PGBackRestRepo{
								{Name: "repo1"},
							},
						},
					},
				},
			},
			assertClusterFunc: func(t *testing.T, actual *crunchyv1beta1.PostgresCluster, _ *PerconaPGCluster) {
				assert.Nil(t, actual.Spec.Proxy)
			},
		},
	}

	for testName, tt := range tests {
		t.Run(testName, func(t *testing.T) {
			tt.expectedPerconaPGCluster.Default()

			crunchyCluster, err := tt.expectedPerconaPGCluster.ToCrunchy(ctx, tt.inputPostgresCluster, scheme)

			if tt.expectedError {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.NotNil(t, crunchyCluster)

			if tt.assertClusterFunc != nil {
				tt.assertClusterFunc(t, crunchyCluster, tt.expectedPerconaPGCluster)
			}
		})
	}
}

// Helper function to check if a slice contains a string
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

func TestShouldCheckStandbyLag(t *testing.T) {
	testCases := []struct {
		descr    string
		expected bool
		cr       *PerconaPGCluster
	}{
		{
			descr: "CRVersion < 2.9.0",
			cr: &PerconaPGCluster{
				Spec: PerconaPGClusterSpec{
					CRVersion: "2.8.0",
				},
			},
			expected: false,
		},
		{
			descr: "CRVersion < 2.9.0, standby!=nil",
			cr: &PerconaPGCluster{
				Spec: PerconaPGClusterSpec{
					CRVersion: "2.8.0",
					Standby: &StandbySpec{
						PostgresStandbySpec: &crunchyv1beta1.PostgresStandbySpec{
							Enabled: true,
						},
					},
				},
			},
			expected: false,
		},
		{
			descr: "CRVersion = 2.9.0, standby.enabled=true",
			cr: &PerconaPGCluster{
				Spec: PerconaPGClusterSpec{
					CRVersion: "2.8.0",
					Standby: &StandbySpec{
						PostgresStandbySpec: &crunchyv1beta1.PostgresStandbySpec{
							Enabled: true,
						},
					},
				},
			},
			expected: false,
		},
		{
			descr: "standby=nil",
			cr: &PerconaPGCluster{
				Spec: PerconaPGClusterSpec{
					CRVersion: "2.9.0",
				},
			},
			expected: false,
		},
		{
			descr: "standby.enabled=false",
			cr: &PerconaPGCluster{
				Spec: PerconaPGClusterSpec{
					CRVersion: "2.9.0",
					Standby: &StandbySpec{
						PostgresStandbySpec: &crunchyv1beta1.PostgresStandbySpec{
							Enabled: false,
						},
					},
				},
			},
			expected: false,
		},
		{
			descr: "standby.enabled=true, maxAcceptableLag=nil",
			cr: &PerconaPGCluster{
				Spec: PerconaPGClusterSpec{
					CRVersion: "2.9.0",
					Standby: &StandbySpec{
						PostgresStandbySpec: &crunchyv1beta1.PostgresStandbySpec{
							Enabled: false,
						},
					},
				},
			},
			expected: false,
		},
		{
			descr: "standby.enabled=true, maxAcceptableLag=0",
			cr: &PerconaPGCluster{
				Spec: PerconaPGClusterSpec{
					CRVersion: "2.9.0",
					Standby: &StandbySpec{
						PostgresStandbySpec: &crunchyv1beta1.PostgresStandbySpec{
							Enabled: true,
						},
						MaxAcceptableLag: k8sptr.To(resource.MustParse("0")),
					},
				},
			},
			expected: true,
		},
	}

	for _, tt := range testCases {
		t.Run(tt.descr, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.cr.ShouldCheckStandbyLag())
		})
	}
}
