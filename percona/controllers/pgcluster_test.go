package controllers

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("PG Cluster", Ordered, func() {
	ctx := context.Background()

	const crName = "pgcluster"
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
		// TODO(user): Attention if you improve this code by adding other context test you MUST
		// be aware of the current delete namespace limitations. More info: https://book.kubebuilder.io/reference/envtest.html#testing-considerations
		By("Deleting the Namespace to perform the tests")
		_ = k8sClient.Delete(ctx, namespace)
	})

	cr, err := readDefaultCR(crName, ns)
	It("should read defautl cr.yaml", func() {
		Expect(err).NotTo(HaveOccurred())
	})

	It("should create PerconaPGCluster", func() {
		Expect(k8sClient.Create(ctx, cr)).Should(Succeed())
	})

	Context("Reconcile controller", func() {
		It("Controller should reconcile", func() {
			_, err := reconciler().Reconcile(ctx, ctrl.Request{NamespacedName: crNamespacedName})
			Expect(err).NotTo(HaveOccurred())
		})
	})
})

var _ = Describe("PMM sidecar", Ordered, func() {
	ctx := context.Background()

	const crName = "pmm-test"
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

	cr, err := readDefaultCR(crName, ns)
	It("should read defautl cr.yaml", func() {
		Expect(err).NotTo(HaveOccurred())
	})

	It("should create PerconaPGCluster with pmm enabled", func() {
		cr.Spec.PMM.Enabled = true
		Expect(k8sClient.Create(ctx, cr)).Should(Succeed())
	})

	Context("Reconcile controller", func() {
		It("Controller should reconcile", func() {
			_, err := reconciler().Reconcile(ctx, ctrl.Request{NamespacedName: crNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			_, err = crunchyReconciler().Reconcile(ctx, ctrl.Request{NamespacedName: crNamespacedName})
			Expect(err).NotTo(HaveOccurred())
		})
	})

	sts := appsv1.StatefulSet{}
	Context("sts should not have pmm sidecar", func() {
		It("should get sts", func() {
			stsList := &appsv1.StatefulSetList{}
			labels := map[string]string{
				"postgres-operator.crunchydata.com/data":         "postgres",
				"postgres-operator.crunchydata.com/instance-set": "instance1",
				"postgres-operator.crunchydata.com/cluster":      crName,
			}
			err = k8sClient.List(ctx, stsList, client.InNamespace(cr.Namespace), client.MatchingLabels(labels))
			Expect(err).NotTo(HaveOccurred())

			Expect(stsList.Items).NotTo(BeEmpty())
			sts = stsList.Items[0]
		})
		It("should not have pmm container", func() {
			Expect(havePMMSidecar(sts)).To(BeFalse())
		})
	})

	Context("secret with no data", func() {
		It("should create pmm secret with no data", func() {
			Expect(k8sClient.Create(ctx, &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cluster1-pmm-secret",
					Namespace: ns,
				},
				Data: map[string][]byte{
					"PMM_SERVER_KEY": {},
				},
			})).Should(Succeed())
		})
		It("Controller should reconcile", func() {
			_, err := reconciler().Reconcile(ctx, ctrl.Request{NamespacedName: crNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			_, err = crunchyReconciler().Reconcile(ctx, ctrl.Request{NamespacedName: crNamespacedName})
			Expect(err).NotTo(HaveOccurred())
		})
		It("should get updated sts", func() {
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(&sts), &sts)).Should(Succeed())
		})
		It("should not have pmm container", func() {
			Expect(havePMMSidecar(sts)).To(BeFalse())
		})
	})

	Context("secret with data", func() {
		It("should fill pmm secret with data", func() {
			Expect(k8sClient.Update(ctx, &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cluster1-pmm-secret",
					Namespace: ns,
				},
				Data: map[string][]byte{
					"PMM_SERVER_KEY": []byte("some-data"),
				},
			})).Should(Succeed())
		})
		It("Controller should reconcile", func() {
			_, err := reconciler().Reconcile(ctx, ctrl.Request{NamespacedName: crNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			_, err = crunchyReconciler().Reconcile(ctx, ctrl.Request{NamespacedName: crNamespacedName})
			Expect(err).NotTo(HaveOccurred())
		})
		It("should get updated sts", func() {
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(&sts), &sts)).Should(Succeed())
		})
		It("should have pmm container", func() {
			Expect(havePMMSidecar(sts)).To(BeTrue())
		})
	})

	Context("cr with disabled pmm", func() {
		It("should disable pmm", func() {
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(cr), cr)).Should(Succeed())
			cr.Spec.PMM.Enabled = false
			Expect(k8sClient.Update(ctx, cr)).Should(Succeed())
		})
		It("Controller should reconcile", func() {
			_, err := reconciler().Reconcile(ctx, ctrl.Request{NamespacedName: crNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			_, err = crunchyReconciler().Reconcile(ctx, ctrl.Request{NamespacedName: crNamespacedName})
			Expect(err).NotTo(HaveOccurred())
		})
		It("should get updated sts", func() {
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(&sts), &sts)).Should(Succeed())
		})
		It("should not have pmm container", func() {
			Expect(havePMMSidecar(sts)).To(BeFalse())
		})
	})
})

func havePMMSidecar(sts appsv1.StatefulSet) bool {
	containers := sts.Spec.Template.Spec.Containers
	for _, container := range containers {
		if container.Name == "pmm-client" {
			return true
		}
	}
	return false
}
