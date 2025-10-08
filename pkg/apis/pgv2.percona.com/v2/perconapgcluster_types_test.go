package v2

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"gotest.tools/v3/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/percona/percona-postgresql-operator/internal/naming"
	"github.com/percona/percona-postgresql-operator/percona/version"
	crunchyv1beta1 "github.com/percona/percona-postgresql-operator/pkg/apis/postgres-operator.crunchydata.com/v1beta1"
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

			assert.Equal(t, cluster.PostgresImage(), tt.expectedImage)
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
				assert.Equal(t, actual.Name, expected.Name)
				assert.Equal(t, actual.Namespace, expected.Namespace)
				assert.DeepEqual(t, actual.Finalizers, []string{naming.Finalizer})
				assert.Equal(t, actual.Spec.PostgresVersion, expected.Spec.PostgresVersion)
				assert.Equal(t, actual.Labels[LabelOperatorVersion], expected.Spec.CRVersion)
				assert.Equal(t, len(actual.Spec.InstanceSets), 1)
				assert.Equal(t, len(actual.Spec.InstanceSets), len(expected.Spec.InstanceSets))
				assert.Equal(t, actual.Spec.InstanceSets[0].Name, expected.Spec.InstanceSets[0].Name)

				assert.Equal(t, actual.Spec.Service.Type, expected.Spec.Expose.Type)
				assert.Assert(t, actual.Spec.Service.LoadBalancerClass != nil)
				assert.Equal(t, actual.Spec.Service.LoadBalancerClass, expected.Spec.Expose.LoadBalancerClass)

				assert.Equal(t, actual.Spec.ReplicaService.Type, expected.Spec.ExposeReplicas.Type)
				assert.Assert(t, actual.Spec.ReplicaService.LoadBalancerClass != nil)
				assert.Equal(t, actual.Spec.ReplicaService.LoadBalancerClass, expected.Spec.ExposeReplicas.LoadBalancerClass)
				assert.Equal(t, *actual.Spec.Backups.TrackLatestRestorableTime, true)
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
				assert.Equal(t, actual.Spec.PostgresVersion, expected.Spec.PostgresVersion)
				assert.Equal(t, actual.Spec.Port, expected.Spec.Port)
				assert.Equal(t, actual.Spec.TLSOnly, expected.Spec.TLSOnly)
				assert.Equal(t, actual.Labels["test-label"], "test-value")
				assert.Equal(t, actual.Labels[LabelOperatorVersion], expected.Spec.CRVersion)
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
				assert.Equal(t, hasMonitoringUser, true)
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
				assert.Equal(t, actual.Annotations[naming.AutoCreateUserSchemaAnnotation], "true")
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
				assert.Equal(t, len(result.Spec.Users), 2)
				userNames := make([]string, len(result.Spec.Users))
				for i, user := range result.Spec.Users {
					userNames[i] = string(user.Name)
				}
				assert.Assert(t, contains(userNames, "regular-user"))
				assert.Assert(t, contains(userNames, "another-user"))
				assert.Assert(t, !contains(userNames, UserMonitoring))
			},
		},
	}

	for testName, tt := range tests {
		t.Run(testName, func(t *testing.T) {
			tt.expectedPerconaPGCluster.Default()

			crunchyCluster, err := tt.expectedPerconaPGCluster.ToCrunchy(ctx, tt.inputPostgresCluster, scheme)

			if tt.expectedError {
				assert.Assert(t, err != nil)
				return
			}

			assert.NilError(t, err)
			assert.Assert(t, crunchyCluster != nil)

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
