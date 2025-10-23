package pgcluster

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	pNaming "github.com/percona/percona-postgresql-operator/v2/percona/naming"
	v2 "github.com/percona/percona-postgresql-operator/v2/pkg/apis/pgv2.percona.com/v2"
)

var _ = Describe("patroni version check", Ordered, func() {
	ctx := context.Background()

	const crName = "patroni-version-test"
	const ns = crName
	crNamespacedName := types.NamespacedName{Name: crName, Namespace: ns}

	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:      crName,
			Namespace: ns,
		},
	}

	BeforeAll(func() {
		By("Creating the Namespace to perform the tests")
		err := k8sClient.Create(ctx, namespace)
		Expect(err).To(Not(HaveOccurred()))
	})

	AfterAll(func() {
		By("Deleting the Namespace to perform the tests")
		_ = k8sClient.Delete(ctx, namespace)
	})

	Context("With custom patroni version annotation", func() {
		cr, err := readDefaultCR(crName, ns)
		It("should read default cr.yaml", func() {
			Expect(err).NotTo(HaveOccurred())
		})

		It("should create PerconaPGCluster with custom patroni version", func() {
			if cr.Annotations == nil {
				cr.Annotations = make(map[string]string)
			}
			cr.Annotations[pNaming.AnnotationCustomPatroniVersion] = "3.2.1"

			status := cr.Status
			Expect(k8sClient.Create(ctx, cr)).Should(Succeed())
			cr.Status = status
			Expect(k8sClient.Status().Update(ctx, cr)).Should(Succeed())
		})

		It("should successfully reconcile patroni version check", func() {
			reconcilerInstance := reconciler(cr)
			err := reconcilerInstance.reconcilePatroniVersion(ctx, cr)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should copy custom patroni version to status", func() {
			updatedCR := &v2.PerconaPGCluster{}
			Expect(k8sClient.Get(ctx, crNamespacedName, updatedCR)).Should(Succeed())

			Expect(updatedCR.Status.Patroni.Version).To(Equal("3.2.1"))
			Expect(updatedCR.Status.PatroniVersion).To(Equal("3.2.1"))
			Expect(updatedCR.Annotations[pNaming.AnnotationPatroniVersion]).To(Equal("3.2.1"))
		})
	})

	Context("Without custom patroni version annotation for cr version <=2.7", func() {
		const crName2 = "patroni-version-test-2"
		const ns2 = crName2
		crNamespacedName2 := types.NamespacedName{Name: crName2, Namespace: ns2}

		namespace2 := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name:      crName2,
				Namespace: ns2,
			},
		}

		BeforeAll(func() {
			By("Creating the second namespace")
			err := k8sClient.Create(ctx, namespace2)
			Expect(err).To(Not(HaveOccurred()))
		})

		AfterAll(func() {
			By("Deleting the second namespace")
			_ = k8sClient.Delete(ctx, namespace2)
		})

		cr2, err := readDefaultCR(crName2, ns2)
		cr2.Spec.CRVersion = "2.7.0"
		It("should read default cr.yaml", func() {
			Expect(err).NotTo(HaveOccurred())
		})

		It("should create PerconaPGCluster without custom patroni version annotation", func() {
			if cr2.Annotations == nil {
				cr2.Annotations = make(map[string]string)
			}
			delete(cr2.Annotations, pNaming.AnnotationCustomPatroniVersion)

			uid := int64(1001)
			cr2.Spec.InstanceSets[0].SecurityContext = &corev1.PodSecurityContext{
				RunAsUser: &uid,
			}
			cr2.Spec.InstanceSets[0].Affinity = &corev1.Affinity{
				NodeAffinity: &corev1.NodeAffinity{
					PreferredDuringSchedulingIgnoredDuringExecution: []corev1.PreferredSchedulingTerm{
						{
							Weight: int32(1),
						},
					},
				},
			}
			cr2.Spec.ImagePullSecrets = []corev1.LocalObjectReference{
				{Name: "test-pull-secret"},
			}

			cr2.Status.Patroni.Version = "3.1.0"
			cr2.Status.PatroniVersion = "3.1.0"
			cr2.Status.Postgres.ImageID = "some-image-id"
			cr2.Annotations[pNaming.AnnotationPatroniVersion] = "3.1.0"

			status := cr2.Status
			Expect(k8sClient.Create(ctx, cr2)).Should(Succeed())
			cr2.Status = status
			Expect(k8sClient.Status().Update(ctx, cr2)).Should(Succeed())

			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      cr2.Name + "-instance-pod",
					Namespace: cr2.Namespace,
					Labels: map[string]string{
						"postgres-operator.crunchydata.com/cluster":  cr2.Name,
						"postgres-operator.crunchydata.com/instance": "instance",
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "database",
							Image: "postgres:16",
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, pod)).Should(Succeed())

			pod.Status = corev1.PodStatus{
				Phase: corev1.PodRunning,
				ContainerStatuses: []corev1.ContainerStatus{
					{
						Name:    "database",
						ImageID: "postgres:16",
					},
				},
			}
			Expect(k8sClient.Status().Update(ctx, pod)).Should(Succeed())
		})

		It("should create patroni version check pod and return errPatroniVersionCheckWait", func() {
			reconcilerInstance := reconciler(cr2)
			err := reconcilerInstance.reconcilePatroniVersion(ctx, cr2)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("waiting for pod to initialize"))
		})

		It("should have created patroni version check pod with correct configuration", func() {
			podName := cr2.Name + "-patroni-version-check"
			pod := &corev1.Pod{}
			err = k8sClient.Get(ctx, types.NamespacedName{Name: podName, Namespace: cr2.Namespace}, pod)
			Expect(err).NotTo(HaveOccurred())

			Expect(pod.Spec.Containers).To(HaveLen(1))
			Expect(pod.Spec.Containers[0].Name).To(Equal(pNaming.ContainerPatroniVersionCheck))
			Expect(pod.Spec.Containers[0].Image).To(Equal(cr2.Spec.Image))
			Expect(pod.Spec.Containers[0].Command).To(Equal([]string{"bash"}))
			Expect(pod.Spec.Containers[0].Args).To(Equal([]string{"-c", "sleep 60"}))
			Expect(pod.Spec.Containers[0].Resources).To(Equal(corev1.ResourceRequirements{
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("100m"),
					corev1.ResourceMemory: resource.MustParse("64Mi"),
				},
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("50m"),
					corev1.ResourceMemory: resource.MustParse("32Mi"),
				},
			}))
			Expect(pod.Spec.Resources).To(Equal(&corev1.ResourceRequirements{
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("100m"),
					corev1.ResourceMemory: resource.MustParse("64Mi"),
				},
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("50m"),
					corev1.ResourceMemory: resource.MustParse("32Mi"),
				},
			}))

			uid := int64(1001)
			expectedSecurityContext := &corev1.PodSecurityContext{
				RunAsUser: &uid,
			}
			expectedImagePullSecrets := []corev1.LocalObjectReference{
				{Name: "test-pull-secret"},
			}
			expectedAffinity := &corev1.Affinity{
				NodeAffinity: &corev1.NodeAffinity{
					PreferredDuringSchedulingIgnoredDuringExecution: []corev1.PreferredSchedulingTerm{
						{
							Weight: int32(1),
						},
					},
				},
			}

			Expect(pod.Spec.SecurityContext).To(Equal(expectedSecurityContext))
			Expect(pod.Spec.TerminationGracePeriodSeconds).To(Equal(ptr.To(int64(5))))
			Expect(pod.Spec.ImagePullSecrets).To(Equal(expectedImagePullSecrets))
			Expect(pod.Spec.Affinity).To(Equal(expectedAffinity))
		})

		It("should preserve existing patroni version in annotation", func() {
			updatedCR := &v2.PerconaPGCluster{}
			Expect(k8sClient.Get(ctx, crNamespacedName2, updatedCR)).Should(Succeed())

			Expect(updatedCR.Status.Patroni.Version).To(Equal("3.1.0"))
			Expect(updatedCR.Status.PatroniVersion).To(Equal("3.1.0"))
			Expect(updatedCR.Annotations[pNaming.AnnotationPatroniVersion]).To(Equal("3.1.0"))
		})
	})
})
