package perconaPG

import (
	"context"
	"testing"

	"github.com/percona/percona-postgresql-operator/v2/percona/version"
	"gotest.tools/v3/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	pNaming "github.com/percona/percona-postgresql-operator/v2/percona/naming"
	v2 "github.com/percona/percona-postgresql-operator/v2/pkg/apis/pgv2.percona.com/v2"
)

func TestGetPrimaryPod(t *testing.T) {
	ctx := context.Background()

	tests := map[string]struct {
		cr            *v2.PerconaPGCluster
		pods          []corev1.Pod
		expectedError string
		expectedPod   string
	}{
		"patroni 4.1.0 with annotation": {
			cr: &v2.PerconaPGCluster{
				Spec: v2.PerconaPGClusterSpec{
					CRVersion: version.Version(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "test-namespace",
					Annotations: map[string]string{
						pNaming.AnnotationPatroniVersion: "4.1.0",
					},
				},
				Status: v2.PerconaPGClusterStatus{
					Patroni: v2.Patroni{
						Version: "4.0.0",
					},
				},
			},
			pods: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-cluster-primary-0",
						Namespace: "test-namespace",
						Labels: map[string]string{
							"app.kubernetes.io/instance":             "test-cluster",
							"postgres-operator.crunchydata.com/role": "primary",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-cluster-primary-1",
						Namespace: "test-namespace",
						Labels: map[string]string{
							"app.kubernetes.io/instance":             "test-cluster",
							"postgres-operator.crunchydata.com/role": "something",
						},
					},
				},
			},
			expectedPod: "test-cluster-primary-0",
		},
		"patroni 4.0.0 without annotation": {
			cr: &v2.PerconaPGCluster{
				Spec: v2.PerconaPGClusterSpec{
					CRVersion: version.Version(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "test-namespace",
				},
				Status: v2.PerconaPGClusterStatus{
					Patroni: v2.Patroni{
						Version: "4.0.0",
					},
				},
			},
			pods: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-cluster-primary-0",
						Namespace: "test-namespace",
						Labels: map[string]string{
							"app.kubernetes.io/instance":             "test-cluster",
							"postgres-operator.crunchydata.com/role": "primary",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-cluster-primary-1",
						Namespace: "test-namespace",
						Labels: map[string]string{
							"app.kubernetes.io/instance":             "test-cluster",
							"postgres-operator.crunchydata.com/role": "something",
						},
					},
				},
			},
			expectedPod: "test-cluster-primary-0",
		},
		"patroni 3.x with master role": {
			cr: &v2.PerconaPGCluster{
				Spec: v2.PerconaPGClusterSpec{
					CRVersion: "2.7.0",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "test-namespace",
				},
				Status: v2.PerconaPGClusterStatus{
					PatroniVersion: "3.0.0",
				},
			},
			pods: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-cluster-master-0",
						Namespace: "test-namespace",
						Labels: map[string]string{
							"app.kubernetes.io/instance":             "test-cluster",
							"postgres-operator.crunchydata.com/role": "master",
						},
					},
				},
			},
			expectedPod: "test-cluster-master-0",
		},
		"patroni version from annotation overrides status for version >= 2.8.0": {
			cr: &v2.PerconaPGCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "test-namespace",
					Annotations: map[string]string{
						pNaming.AnnotationPatroniVersion: "4.1.0",
					},
				},
				Spec: v2.PerconaPGClusterSpec{
					PostgresVersion: 16,
					CRVersion:       version.Version(),
				},
				Status: v2.PerconaPGClusterStatus{
					PatroniVersion: "3.0.0",
					Postgres: v2.PostgresStatus{
						Version: 16,
					},
				},
			},
			pods: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-cluster-primary-0",
						Namespace: "test-namespace",
						Labels: map[string]string{
							"app.kubernetes.io/instance":             "test-cluster",
							"postgres-operator.crunchydata.com/role": "primary",
						},
					},
				},
			},
			expectedPod: "test-cluster-primary-0",
		},
		"patroni version from status used for version < 2.8.0": {
			cr: &v2.PerconaPGCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "test-namespace",
					Annotations: map[string]string{
						pNaming.AnnotationPatroniVersion: "4.0.0",
					},
				},
				Spec: v2.PerconaPGClusterSpec{
					PostgresVersion: 14,
					CRVersion:       "2.7.0",
				},
				Status: v2.PerconaPGClusterStatus{
					PatroniVersion: "3.0.0",
					Postgres: v2.PostgresStatus{
						Version: 14,
					},
				},
			},
			pods: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-cluster-master-0",
						Namespace: "test-namespace",
						Labels: map[string]string{
							"app.kubernetes.io/instance":             "test-cluster",
							"postgres-operator.crunchydata.com/role": "master",
						},
					},
				},
			},
			expectedPod: "test-cluster-master-0",
		},
		"no primary pod found": {
			cr: &v2.PerconaPGCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "test-namespace",
					Annotations: map[string]string{
						pNaming.AnnotationPatroniVersion: "4.1.0",
					},
				},
				Spec: v2.PerconaPGClusterSpec{
					PostgresVersion: 14,
					CRVersion:       version.Version(),
				},
			},
			pods:          []corev1.Pod{},
			expectedError: "no primary pod found",
		},
		"multiple primary pods found": {
			cr: &v2.PerconaPGCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "test-namespace",
				},
				Status: v2.PerconaPGClusterStatus{
					PatroniVersion: "4.0.0",
				},
				Spec: v2.PerconaPGClusterSpec{
					CRVersion: version.Version(),
				},
			},
			pods: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-cluster-primary-0",
						Namespace: "test-namespace",
						Labels: map[string]string{
							"app.kubernetes.io/instance":             "test-cluster",
							"postgres-operator.crunchydata.com/role": "primary",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-cluster-primary-1",
						Namespace: "test-namespace",
						Labels: map[string]string{
							"app.kubernetes.io/instance":             "test-cluster",
							"postgres-operator.crunchydata.com/role": "primary",
						},
					},
				},
			},
			expectedError: "multiple primary pods found",
		},
		"invalid patroni version returns the default primary": {
			cr: &v2.PerconaPGCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "test-namespace",
				},
				Status: v2.PerconaPGClusterStatus{
					PatroniVersion: "invalid-version",
				},
				Spec: v2.PerconaPGClusterSpec{
					CRVersion: version.Version(),
				},
			},
			pods: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-cluster-primary-0",
						Namespace: "test-namespace",
						Labels: map[string]string{
							"app.kubernetes.io/instance":             "test-cluster",
							"postgres-operator.crunchydata.com/role": "primary",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-cluster-master-1",
						Namespace: "test-namespace",
						Labels: map[string]string{
							"app.kubernetes.io/instance":             "test-cluster",
							"postgres-operator.crunchydata.com/role": "master",
						},
					},
				},
			},
			expectedPod: "test-cluster-primary-0",
		},
		"patroni 4.1.0-beta with primary role": {
			cr: &v2.PerconaPGCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "test-namespace",
					Annotations: map[string]string{
						pNaming.AnnotationPatroniVersion: "4.1.0-beta.1",
					},
				},
				Spec: v2.PerconaPGClusterSpec{
					CRVersion: version.Version(),
				},
			},
			pods: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-cluster-primary-0",
						Namespace: "test-namespace",
						Labels: map[string]string{
							"app.kubernetes.io/instance":             "test-cluster",
							"postgres-operator.crunchydata.com/role": "primary",
						},
					},
				},
			},
			expectedPod: "test-cluster-primary-0",
		},
		"patroni 3.9.9 with master role (just before 4.0.0)": {
			cr: &v2.PerconaPGCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "test-namespace",
				},
				Status: v2.PerconaPGClusterStatus{
					PatroniVersion: "3.9.9",
				},
				Spec: v2.PerconaPGClusterSpec{
					CRVersion: "2.7.0",
				},
			},
			pods: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-cluster-master-0",
						Namespace: "test-namespace",
						Labels: map[string]string{
							"app.kubernetes.io/instance":             "test-cluster",
							"postgres-operator.crunchydata.com/role": "master",
						},
					},
				},
			},
			expectedPod: "test-cluster-master-0",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			err := corev1.AddToScheme(scheme)
			assert.NilError(t, err)
			err = v2.AddToScheme(scheme)
			assert.NilError(t, err)

			objects := []runtime.Object{tt.cr}
			for i := range tt.pods {
				objects = append(objects, &tt.pods[i])
			}

			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithRuntimeObjects(objects...).
				Build()

			pod, err := GetPrimaryPod(ctx, fakeClient, tt.cr)

			if tt.expectedError != "" {
				assert.ErrorContains(t, err, tt.expectedError)
				assert.Assert(t, pod == nil)
			} else {
				assert.NilError(t, err)
				assert.Assert(t, pod != nil)
				assert.Equal(t, pod.Name, tt.expectedPod)
			}
		})
	}
}
