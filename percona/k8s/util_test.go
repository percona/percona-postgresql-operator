package k8s

import (
	"context"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	"github.com/percona/percona-postgresql-operator/pkg/apis/postgres-operator.crunchydata.com/v1beta1"
)

type testGetCluster func() *v1beta1.PostgresCluster

type testGetComponentWithInit func(cr *v1beta1.PostgresCluster) ComponentWithInit

var getPGBackrestComponent = func(cr *v1beta1.PostgresCluster) ComponentWithInit {
	return &cr.Spec.Backups.PGBackRest
}

func TestInitContainer(t *testing.T) {
	ctx := context.Background()
	cr, err := readDefaultCR("test-init-image", "test-init-image")
	if err != nil {
		t.Fatal(err)
	}
	cl, err := buildFakeClient(ctx, cr)
	if err != nil {
		t.Fatal(err)
	}

	crunchyCr := new(v1beta1.PostgresCluster)
	if err := cl.Get(ctx, client.ObjectKeyFromObject(cr), crunchyCr); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name         string
		component    string
		image        string
		pullPolicy   corev1.PullPolicy
		secCtx       *corev1.SecurityContext
		resources    corev1.ResourceRequirements
		getCluster   testGetCluster
		getComponent testGetComponentWithInit
		expected     string
	}{
		{
			"nothing is specified",
			"",
			"",
			"",
			nil,
			corev1.ResourceRequirements{},
			func() *v1beta1.PostgresCluster { return crunchyCr.DeepCopy() },
			func(cr *v1beta1.PostgresCluster) ComponentWithInit { return nil },
			`
command:
- /usr/local/bin/init-entrypoint.sh
name: -init
resources: {}
terminationMessagePath: /dev/termination-log
terminationMessagePolicy: File
volumeMounts:
- mountPath: /opt/crunchy
  name: crunchy-bin
            `,
		},
		{
			"pgbackrest InitContainer is not specified",
			"component",
			"image",
			corev1.PullAlways,
			&corev1.SecurityContext{
				RunAsUser:                ptr.To(int64(1001)),
				RunAsGroup:               ptr.To(int64(26)),
				AllowPrivilegeEscalation: ptr.To(true),
			},
			corev1.ResourceRequirements{
				Limits: corev1.ResourceList{
					corev1.ResourceMemory: resource.MustParse("128Mi"),
				},
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("100m"),
					corev1.ResourceMemory: resource.MustParse("64Mi"),
				},
				Claims: []corev1.ResourceClaim{{
					Name:    "claim",
					Request: "req",
				}},
			},
			func() *v1beta1.PostgresCluster {
				cr := crunchyCr.DeepCopy()
				cr.Spec.Backups.PGBackRest.InitContainer = nil
				return cr
			},
			getPGBackrestComponent,
			`
command:
- /usr/local/bin/init-entrypoint.sh
image: image
imagePullPolicy: Always
name: component-init
resources:
  claims:
  - name: claim
    request: req
  limits:
    memory: 128Mi
  requests:
    cpu: 100m
    memory: 64Mi
securityContext:
  allowPrivilegeEscalation: true
  runAsGroup: 26
  runAsUser: 1001
terminationMessagePath: /dev/termination-log
terminationMessagePolicy: File
volumeMounts:
- mountPath: /opt/crunchy
  name: crunchy-bin
            `,
		},
		{
			"pgbackrest everything is specified",
			"component",
			"image",
			corev1.PullAlways,
			&corev1.SecurityContext{
				RunAsUser:                ptr.To(int64(1001)),
				RunAsGroup:               ptr.To(int64(26)),
				AllowPrivilegeEscalation: ptr.To(true),
			},
			corev1.ResourceRequirements{
				Limits: corev1.ResourceList{
					corev1.ResourceMemory: resource.MustParse("128Mi"),
				},
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("100m"),
					corev1.ResourceMemory: resource.MustParse("64Mi"),
				},
				Claims: []corev1.ResourceClaim{{
					Name:    "claim",
					Request: "req",
				}},
			},
			func() *v1beta1.PostgresCluster {
				cr := crunchyCr.DeepCopy()
				cr.Spec.Backups.PGBackRest.InitContainer = &v1beta1.InitContainerSpec{}
				cr.Spec.Backups.PGBackRest.InitContainer.Resources = &corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						corev1.ResourceMemory: resource.MustParse("1280Mi"),
					},
					Requests: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("1000m"),
						corev1.ResourceMemory: resource.MustParse("640Mi"),
					},
					Claims: []corev1.ResourceClaim{{
						Name:    "claim2",
						Request: "req2",
					}},
				}
				cr.Spec.Backups.PGBackRest.InitContainer.ContainerSecurityContext = &corev1.SecurityContext{
					RunAsUser:                ptr.To(int64(26)),
					RunAsGroup:               ptr.To(int64(1001)),
					AllowPrivilegeEscalation: ptr.To(false),
				}
				return cr
			},
			getPGBackrestComponent,
			`
command:
- /usr/local/bin/init-entrypoint.sh
image: image
imagePullPolicy: Always
name: component-init
resources:
  claims:
  - name: claim2
    request: req2
  limits:
    memory: 1280Mi
  requests:
    cpu: "1"
    memory: 640Mi
securityContext:
  allowPrivilegeEscalation: false
  runAsGroup: 1001
  runAsUser: 26
terminationMessagePath: /dev/termination-log
terminationMessagePolicy: File
volumeMounts:
- mountPath: /opt/crunchy
  name: crunchy-bin
            `,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("OPERATOR_NAMESPACE", cr.Namespace)
			cr := tt.getCluster().DeepCopy()

			container := InitContainer(tt.getCluster(), tt.component, tt.image, tt.pullPolicy, tt.secCtx, tt.resources, tt.getComponent(cr))
			data, err := yaml.Marshal(container)
			if err != nil {
				t.Fatal(err)
			}
			if strings.TrimSpace(string(data)) != strings.TrimSpace(tt.expected) {
				t.Fatal("expected:\n", tt.expected, "\ngot:\n", string(data))
			}
		})
	}
}

func TestInitImage(t *testing.T) {
	ctx := context.Background()

	cr, err := readDefaultCR("test-init-image", "test-init-image")
	if err != nil {
		t.Fatal(err)
	}
	cr.Spec.InitContainer.Image = ""

	operatorDepl, err := readDefaultOperator(cr.Name+"-operator", cr.Namespace)
	if err != nil {
		t.Fatal(err)
	}
	operatorPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      operatorDepl.Name,
			Namespace: operatorDepl.Namespace,
		},
		Spec: operatorDepl.Spec.Template.Spec,
	}
	operatorPod.Spec.Containers[0].Image = "operator-image"
	cl, err := buildFakeClient(ctx, cr, operatorPod)
	if err != nil {
		t.Fatal(err)
	}

	crunchyCr := new(v1beta1.PostgresCluster)
	if err := cl.Get(ctx, client.ObjectKeyFromObject(cr), crunchyCr); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name       string
		getCluster testGetCluster
		component  testGetComponentWithInit
		expected   string
	}{
		{
			"not specified init image",
			func() *v1beta1.PostgresCluster { return crunchyCr.DeepCopy() },
			func(cr *v1beta1.PostgresCluster) ComponentWithInit { return nil },
			"operator-image",
		},
		{
			"pgbackrest not specified init image",
			func() *v1beta1.PostgresCluster { return crunchyCr.DeepCopy() },
			getPGBackrestComponent,
			"operator-image",
		},
		{
			"pgbackrest not specified init image with different versions",
			func() *v1beta1.PostgresCluster {
				cr := crunchyCr.DeepCopy()

				oldVersion := "1.2.0"

				cr.Labels = map[string]string{v1beta1.LabelVersion: oldVersion}
				return cr
			},
			getPGBackrestComponent,
			"operator-image:1.2.0",
		},
		{
			"pgbackrest general init image",
			func() *v1beta1.PostgresCluster {
				cr := crunchyCr.DeepCopy()
				cr.Spec.InitContainer.Image = "general-init-image"
				return cr
			},
			getPGBackrestComponent,
			"general-init-image",
		},
		{
			"pgbackrest custom init image",
			func() *v1beta1.PostgresCluster {
				cr := crunchyCr.DeepCopy()
				cr.Spec.InitContainer.Image = "general-init-image"
				cr.Spec.Backups.PGBackRest.InitContainer = &v1beta1.InitContainerSpec{}
				cr.Spec.Backups.PGBackRest.InitContainer.Image = "custom-image"
				return cr
			},
			getPGBackrestComponent,
			"custom-image",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("OPERATOR_NAMESPACE", cr.Namespace)
			t.Setenv("HOSTNAME", operatorPod.Name)
			cr := tt.getCluster().DeepCopy()

			res, err := InitImage(ctx, cl, cr, tt.component(cr))
			if err != nil {
				t.Fatal(err)
			}

			if tt.expected != res {
				t.Fatal("expected:", tt.expected, "got:", res)
			}
		})
	}
}
