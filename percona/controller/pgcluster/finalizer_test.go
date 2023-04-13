package pgcluster

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/percona/percona-postgresql-operator/pkg/apis/pg.percona.com/v2beta1"
	"github.com/percona/percona-postgresql-operator/pkg/apis/postgres-operator.crunchydata.com/v1beta1"
)

var _ = Describe("Finalizers", Ordered, func() {
	ctx := context.Background()

	const ns = "pgcluster-finalizers"

	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ns,
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

	Context(v2beta1.FinalizerDeletePVC, Ordered, func() {
		crName := ns + "-with-delete-pvc"
		crNamespacedName := types.NamespacedName{Name: crName, Namespace: ns}
		When("with finalizer", func() {
			cr, err := readDefaultCR(crName, ns)
			It("should read defautl cr.yaml", func() {
				Expect(err).NotTo(HaveOccurred())
			})

			controllerutil.AddFinalizer(cr, v2beta1.FinalizerDeletePVC)

			It("should create PerconaPGCluster", func() {
				Expect(k8sClient.Create(ctx, cr)).Should(Succeed())
			})

			It("should create PVCs", func() {
				_, err = reconciler().Reconcile(ctx, ctrl.Request{NamespacedName: crNamespacedName})
				Expect(err).NotTo(HaveOccurred())
				_, err = crunchyReconciler().Reconcile(ctx, ctrl.Request{NamespacedName: crNamespacedName})
				Expect(err).NotTo(HaveOccurred())

				pvcList := corev1.PersistentVolumeClaimList{}
				Eventually(func() bool {
					err := k8sClient.List(ctx, &pvcList, &client.ListOptions{
						Namespace: cr.Namespace,
						LabelSelector: labels.SelectorFromSet(map[string]string{
							"postgres-operator.crunchydata.com/cluster": cr.Name,
						}),
					})
					return err == nil
				}, time.Second*15, time.Millisecond*250).Should(BeTrue())
				Expect(len(pvcList.Items)).Should(Equal(4))
			})

			It("should delete PerconaPGCluster", func() {
				Expect(k8sClient.Delete(ctx, cr)).Should(Succeed())
			})

			It("should delete PostgresCluster", func() {
				_, err = reconciler().Reconcile(ctx, ctrl.Request{NamespacedName: crNamespacedName})
				Expect(err).NotTo(HaveOccurred())
				_, err = crunchyReconciler().Reconcile(ctx, ctrl.Request{NamespacedName: crNamespacedName})
				Expect(err).NotTo(HaveOccurred())
			})

			It("should wait for PostgresCluster to be deleted", func() {
				postgresCluster := &v1beta1.PostgresCluster{
					ObjectMeta: metav1.ObjectMeta{
						Name:      cr.Name,
						Namespace: cr.Namespace,
					},
				}
				Eventually(func() bool {
					err := k8sClient.Get(ctx, client.ObjectKeyFromObject(postgresCluster), postgresCluster)
					return k8serrors.IsNotFound(err)
				}, time.Second*15, time.Millisecond*250).Should(BeTrue())
			})

			It("should run finalizer", func() {
				_, err = reconciler().Reconcile(ctx, ctrl.Request{NamespacedName: crNamespacedName})
				Expect(err).NotTo(HaveOccurred())
				_, err = crunchyReconciler().Reconcile(ctx, ctrl.Request{NamespacedName: crNamespacedName})
				Expect(err).NotTo(HaveOccurred())
			})

			It("should delete PVCs", func() {
				pvcList := corev1.PersistentVolumeClaimList{}
				Eventually(func() bool {
					err := k8sClient.List(ctx, &pvcList, &client.ListOptions{
						Namespace: cr.Namespace,
						LabelSelector: labels.SelectorFromSet(map[string]string{
							"postgres-operator.crunchydata.com/cluster": cr.Name,
						}),
					})
					return err == nil
				}, time.Second*15, time.Millisecond*250).Should(BeTrue())
				for _, pvc := range pvcList.Items {
					By(fmt.Sprintf("checking pvc/%s", pvc.Name))
					Expect(pvc.DeletionTimestamp).ShouldNot(BeNil())
				}
			})

			It("should delete user secrets", func() {
				secretList := corev1.SecretList{}
				Eventually(func() bool {
					err := k8sClient.List(ctx, &secretList, &client.ListOptions{
						Namespace: cr.Namespace,
						LabelSelector: labels.SelectorFromSet(map[string]string{
							"postgres-operator.crunchydata.com/cluster": cr.Name,
							"postgres-operator.crunchydata.com/role":    "pguser",
						}),
					})
					return err == nil
				}, time.Second*15, time.Millisecond*250).Should(BeTrue())
				Expect(len(secretList.Items)).Should(Equal(0))
			})
		})

		When("without finalizer", func() {
			crName := ns + "-without-delete-pvc"
			crNamespacedName := types.NamespacedName{Name: crName, Namespace: ns}

			cr, err := readDefaultCR(crName, ns)
			It("should read defautl cr.yaml", func() {
				Expect(err).NotTo(HaveOccurred())
			})

			It("should create PerconaPGCluster", func() {
				Expect(k8sClient.Create(ctx, cr)).Should(Succeed())
			})

			It("should reconcile PerconaPGCluster", func() {
				_, err = reconciler().Reconcile(ctx, ctrl.Request{NamespacedName: crNamespacedName})
				Expect(err).NotTo(HaveOccurred())
				_, err = crunchyReconciler().Reconcile(ctx, ctrl.Request{NamespacedName: crNamespacedName})
				Expect(err).NotTo(HaveOccurred())

				pvcList := corev1.PersistentVolumeClaimList{}
				Eventually(func() bool {
					err := k8sClient.List(ctx, &pvcList, &client.ListOptions{
						Namespace: cr.Namespace,
						LabelSelector: labels.SelectorFromSet(map[string]string{
							"postgres-operator.crunchydata.com/cluster": cr.Name,
						}),
					})
					return err == nil
				}, time.Second*15, time.Millisecond*250).Should(BeTrue())
				Expect(len(pvcList.Items)).Should(Equal(4))
			})

			It("should delete PerconaPGCluster", func() {
				Expect(k8sClient.Delete(ctx, cr)).Should(Succeed())
			})

			It("should reconcile PerconaPGCluster", func() {
				_, err = reconciler().Reconcile(ctx, ctrl.Request{NamespacedName: crNamespacedName})
				Expect(err).NotTo(HaveOccurred())
				_, err = crunchyReconciler().Reconcile(ctx, ctrl.Request{NamespacedName: crNamespacedName})
				Expect(err).NotTo(HaveOccurred())
			})

			It("should not delete PVCs", func() {
				pvcList := corev1.PersistentVolumeClaimList{}
				Eventually(func() bool {
					err := k8sClient.List(ctx, &pvcList, &client.ListOptions{
						Namespace: cr.Namespace,
						LabelSelector: labels.SelectorFromSet(map[string]string{
							"postgres-operator.crunchydata.com/cluster": cr.Name,
						}),
					})
					return err == nil
				}, time.Second*15, time.Millisecond*250).Should(BeTrue())
				for _, pvc := range pvcList.Items {
					By(fmt.Sprintf("checking pvc/%s", pvc.Name))
					Expect(pvc.DeletionTimestamp).Should(BeNil())
				}
			})

			It("should not delete user secrets", func() {
				secretList := corev1.SecretList{}
				Eventually(func() bool {
					err := k8sClient.List(ctx, &secretList, &client.ListOptions{
						Namespace: cr.Namespace,
						LabelSelector: labels.SelectorFromSet(map[string]string{
							"postgres-operator.crunchydata.com/cluster": cr.Name,
							"postgres-operator.crunchydata.com/role":    "pguser",
						}),
					})
					return err == nil
				}, time.Second*15, time.Millisecond*250).Should(BeTrue())
				Expect(len(secretList.Items)).Should(Equal(1))
			})
		})
	})
})
