package pgcluster

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/percona/percona-postgresql-operator/pkg/apis/pg.percona.com/v2beta1"
	"github.com/percona/percona-postgresql-operator/pkg/apis/postgres-operator.crunchydata.com/v1beta1"
)

var _ = FDescribe("PG Cluster status", Ordered, func() {
	ctx := context.Background()

	const crName = "pgcluster-status"
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

	Context("Update PG cluster status.state", func() {
		pgc := &v1beta1.PostgresCluster{}

		It("controller should reconclile and create PostgresCluster CR", func() {
			// This service is created by Cruncy PGO
			svc := &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      crName + "-pgbouncer",
					Namespace: ns,
				},
				Spec: corev1.ServiceSpec{
					Ports: []corev1.ServicePort{
						{
							Name: "pgbouncer",
							Port: 5588,
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, svc)).Should(Succeed())

			_, err := reconciler().Reconcile(ctx, ctrl.Request{NamespacedName: crNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			err = k8sClient.Get(ctx, crNamespacedName, pgc)
			Expect(err).NotTo(HaveOccurred())
		})

		When("PGBackRest RepoHost is not ready", func() {
			It("status should be initializing", func() {
				updatePerconaClusterCR(ctx, crNamespacedName, pgc, func(pgc *v1beta1.PostgresCluster) {
					pgc.Status.PGBackRest = &v1beta1.PGBackRestStatus{
						RepoHost: &v1beta1.RepoHostStatus{Ready: false},
					}
				})

				reconcileAndAssertState(ctx, crNamespacedName, cr, v2beta1.AppStateInit)
			})
		})

		When("PGBouncer ready replicas lower than specified replicas", func() {
			It("status should be initializing", func() {
				updatePerconaClusterCR(ctx, crNamespacedName, pgc, func(pgc *v1beta1.PostgresCluster) {
					pgc.Status.Proxy.PGBouncer.ReadyReplicas = 0
					pgc.Status.Proxy.PGBouncer.Replicas = 1
				})

				reconcileAndAssertState(ctx, crNamespacedName, cr, v2beta1.AppStateInit)
			})
		})

		When("PG running pods are lower than specified pods", func() {
			It("status should be initializing", func() {
				updatePerconaClusterCR(ctx, crNamespacedName, pgc, func(pgc *v1beta1.PostgresCluster) {
					pgc.Status.InstanceSets = append(pgc.Status.InstanceSets, v1beta1.PostgresInstanceSetStatus{
						ReadyReplicas: 0,
						Replicas:      1,
					})
				})

				reconcileAndAssertState(ctx, crNamespacedName, cr, v2beta1.AppStateInit)
			})
		})

		When("RepoHost is ready, PGBouncer and PG running pods are equal to specified number", func() {
			It("status should be ready", func() {
				updatePerconaClusterCR(ctx, crNamespacedName, pgc, func(pgc *v1beta1.PostgresCluster) {
					pgc.Status.PGBackRest.RepoHost.Ready = true
					pgc.Status.Proxy.PGBouncer.ReadyReplicas = 1
					pgc.Status.InstanceSets[0].ReadyReplicas = 1
				})

				reconcileAndAssertState(ctx, crNamespacedName, cr, v2beta1.AppStateReady)
			})
		})
	})
})

func reconcileAndAssertState(ctx context.Context, nn types.NamespacedName, cr *v2beta1.PerconaPGCluster, expectedState v2beta1.AppState) {
	_, err := reconciler().Reconcile(ctx, ctrl.Request{NamespacedName: nn})
	Expect(err).NotTo(HaveOccurred())

	Eventually(func() bool {
		err = k8sClient.Get(ctx, nn, cr)
		return err == nil
	}, time.Second*15, time.Millisecond*250).Should(BeTrue())

	Expect(cr.Status.State).Should(Equal(expectedState))
}

func updatePerconaClusterCR(ctx context.Context, nn types.NamespacedName, pgc *v1beta1.PostgresCluster, update func(*v1beta1.PostgresCluster)) {
	Eventually(func() bool {
		err := k8sClient.Get(ctx, nn, pgc)
		return err == nil
	}, time.Second*15, time.Millisecond*250).Should(BeTrue())

	update(pgc)

	Expect(k8sClient.Status().Update(ctx, pgc)).Should(Succeed())
}

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
