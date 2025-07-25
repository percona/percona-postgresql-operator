//go:build envtest
// +build envtest

package pgcluster

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	gs "github.com/onsi/gomega/gstruct"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/percona/percona-postgresql-operator/internal/controller/postgrescluster"
	pNaming "github.com/percona/percona-postgresql-operator/percona/naming"
	v2 "github.com/percona/percona-postgresql-operator/pkg/apis/pgv2.percona.com/v2"
	"github.com/percona/percona-postgresql-operator/pkg/apis/postgres-operator.crunchydata.com/v1beta1"
)

var _ = Describe("PG Cluster status", Ordered, func() {
	ctx := context.Background()

	const ns = "pgcluster-status"

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

	Context("Updated PG cluster status.postgres", func() {
		crName := ns + "-postgres"
		crNamespacedName := types.NamespacedName{Name: crName, Namespace: ns}

		cr, err := readDefaultCR(crName, ns)
		It("should read defautl cr.yaml and create PerconaPGCluster", func() {
			Expect(err).NotTo(HaveOccurred())
			status := cr.Status
			Expect(k8sClient.Create(ctx, cr)).Should(Succeed())
			cr.Status = status
			Expect(k8sClient.Status().Update(ctx, cr)).Should(Succeed())
		})

		It("should reconcile and create Crunchy PostgreCluster", func() {
			_, err := reconciler(cr).Reconcile(ctx, ctrl.Request{NamespacedName: crNamespacedName})
			Expect(err).NotTo(HaveOccurred())
		})

		It("status.postgres should reflect Crunchy instanceSets status", func() {
			updateCrunchyPGClusterStatus(ctx, crNamespacedName, func(pgc *v1beta1.PostgresCluster) {
				pgc.Status.InstanceSets = append(pgc.Status.InstanceSets,
					v1beta1.PostgresInstanceSetStatus{
						Name:          "instance1",
						ReadyReplicas: 0,
						Replicas:      1,
					},
					v1beta1.PostgresInstanceSetStatus{
						Name:          "instance2",
						ReadyReplicas: 3,
						Replicas:      3,
					},
				)
			})

			_, err := reconciler(cr).Reconcile(ctx, ctrl.Request{NamespacedName: crNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			Eventually(func() bool {
				err = k8sClient.Get(ctx, crNamespacedName, cr)
				return err == nil
			}, time.Second*15, time.Millisecond*250).Should(BeTrue())

			Expect(cr.Status.Postgres.Ready).Should(Equal(int32(3)))
			Expect(cr.Status.Postgres.Size).Should(Equal(int32(4)))
			Expect(cr.Status.Postgres.InstanceSets).Should(HaveLen(2))
			Expect(cr.Status.ObservedGeneration).Should(Equal(int64(1)))
			Expect(cr.Status.Postgres.InstanceSets).Should(ContainElement(gs.MatchFields(gs.IgnoreExtras, gs.Fields{
				"Name":  Equal("instance1"),
				"Ready": Equal(int32(0)),
				"Size":  Equal(int32(1)),
			})))
			Expect(cr.Status.Postgres.InstanceSets).Should(ContainElement(gs.MatchFields(gs.IgnoreExtras, gs.Fields{
				"Name":  Equal("instance2"),
				"Ready": Equal(int32(3)),
				"Size":  Equal(int32(3)),
			})))
		})
	})

	Context("Updated PG cluster status.pgbouncer", func() {
		crName := ns + "-pgbouncer"
		crNamespacedName := types.NamespacedName{Name: crName, Namespace: ns}

		cr, err := readDefaultCR(crName, ns)
		It("should read defautl cr.yaml and create PerconaPGCluster", func() {
			Expect(err).NotTo(HaveOccurred())
			status := cr.Status
			Expect(k8sClient.Create(ctx, cr)).Should(Succeed())
			cr.Status = status
			Expect(k8sClient.Status().Update(ctx, cr)).Should(Succeed())
		})

		It("should reconcile and create Crunchy PostgreCluster", func() {
			_, err := reconciler(cr).Reconcile(ctx, ctrl.Request{NamespacedName: crNamespacedName})
			Expect(err).NotTo(HaveOccurred())
		})

		It("status.pgbouncer should match Crunchy pgbouncer status", func() {
			updateCrunchyPGClusterStatus(ctx, crNamespacedName, func(pgc *v1beta1.PostgresCluster) {
				pgc.Status.Proxy.PGBouncer.ReadyReplicas = 0
				pgc.Status.Proxy.PGBouncer.Replicas = 1
			})

			_, err := reconciler(cr).Reconcile(ctx, ctrl.Request{NamespacedName: crNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			Eventually(func() bool {
				err = k8sClient.Get(ctx, crNamespacedName, cr)
				return err == nil
			}, time.Second*15, time.Millisecond*250).Should(BeTrue())

			Expect(cr.Status.ObservedGeneration).Should(Equal(int64(1)))
			Expect(cr.Status.PGBouncer.Ready).Should(Equal(int32(0)))
			Expect(cr.Status.PGBouncer.Size).Should(Equal(int32(1)))
		})
	})

	Context("Update PG cluster status.state", Ordered, func() {
		crName := ns + "-state"
		crNamespacedName := types.NamespacedName{Name: crName, Namespace: ns}

		cr, err := readDefaultCR(crName, ns)
		It("should read defautl cr.yaml and create PerconaPGCluster", func() {
			Expect(err).NotTo(HaveOccurred())
			status := cr.Status
			Expect(k8sClient.Create(ctx, cr)).Should(Succeed())
			cr.Status = status
			Expect(k8sClient.Status().Update(ctx, cr)).Should(Succeed())
		})

		It("should reconcile and create Crunchy PostgreCluster", func() {
			_, err := reconciler(cr).Reconcile(ctx, ctrl.Request{NamespacedName: crNamespacedName})
			Expect(err).NotTo(HaveOccurred())
		})

		When("PGBackRest RepoHost is not ready", func() {
			It("state should be initializing", func() {
				updateCrunchyPGClusterStatus(ctx, crNamespacedName, func(pgc *v1beta1.PostgresCluster) {
					pgc.Status.PGBackRest = &v1beta1.PGBackRestStatus{
						RepoHost: &v1beta1.RepoHostStatus{Ready: false},
					}
				})

				reconcileAndAssertState(ctx, crNamespacedName, cr, v2.AppStateInit)
			})
		})

		When("PGBouncer ready replicas lower than specified replicas", func() {
			It("state should be initializing", func() {
				updateCrunchyPGClusterStatus(ctx, crNamespacedName, func(pgc *v1beta1.PostgresCluster) {
					pgc.Status.Proxy.PGBouncer.ReadyReplicas = 0
					pgc.Status.Proxy.PGBouncer.Replicas = 1
				})

				reconcileAndAssertState(ctx, crNamespacedName, cr, v2.AppStateInit)
			})
		})

		When("The cluster is paused", Ordered, func() {
			It("should pause the cluster", func() {
				updatePerconaPGClusterCR(ctx, crNamespacedName, func(cr *v2.PerconaPGCluster) {
					t := true
					cr.Spec.Pause = &t
				})
			})

			When("And PG running pods are lower than specified pods", func() {
				It("state should be stopping", func() {
					updateCrunchyPGClusterStatus(ctx, crNamespacedName, func(pgc *v1beta1.PostgresCluster) {
						pgc.Status.InstanceSets = append(pgc.Status.InstanceSets, v1beta1.PostgresInstanceSetStatus{
							ReadyReplicas: 1,
							Replicas:      0,
						})
					})

					reconcileAndAssertState(ctx, crNamespacedName, cr, v2.AppStateStopping)
				})
			})

			When("And PG running pods are lower than specified pods", func() {
				It("state should be paused", func() {
					updateCrunchyPGClusterStatus(ctx, crNamespacedName, func(pgc *v1beta1.PostgresCluster) {
						pgc.Status.InstanceSets[0].ReadyReplicas = 0
					})

					reconcileAndAssertState(ctx, crNamespacedName, cr, v2.AppStatePaused)
				})
			})

			It("should unpause the cluster", func() {
				updatePerconaPGClusterCR(ctx, crNamespacedName, func(cr *v2.PerconaPGCluster) {
					t := false
					cr.Spec.Pause = &t
				})
			})
		})

		When("PG running pods are lower than specified pods", func() {
			It("state should be initializing", func() {
				updateCrunchyPGClusterStatus(ctx, crNamespacedName, func(pgc *v1beta1.PostgresCluster) {
					pgc.Status.InstanceSets[0].Replicas = 1
					pgc.Status.InstanceSets[0].ReadyReplicas = 0
				})

				reconcileAndAssertState(ctx, crNamespacedName, cr, v2.AppStateInit)
			})
		})

		When("RepoHost is ready, PGBouncer and PG running pods are equal to specified number", func() {
			It("state should be ready", func() {
				updateCrunchyPGClusterStatus(ctx, crNamespacedName, func(pgc *v1beta1.PostgresCluster) {
					pgc.Status.PGBackRest.RepoHost.Ready = true
					pgc.Status.Proxy.PGBouncer.ReadyReplicas = 1
					pgc.Status.InstanceSets[0].ReadyReplicas = 1
					pgc.Status.InstanceSets[0].UpdatedReplicas = 1
				})

				reconcileAndAssertState(ctx, crNamespacedName, cr, v2.AppStateReady)
			})
		})
	})

	Context("Update PG cluster status.host", Ordered, func() {
		crName := ns + "-host"
		crNamespacedName := types.NamespacedName{Name: crName, Namespace: ns}

		cr, err := readDefaultCR(crName, ns)
		It("should read defautl cr.yaml and create PerconaPGCluster", func() {
			Expect(err).NotTo(HaveOccurred())
			status := cr.Status
			Expect(k8sClient.Create(ctx, cr)).Should(Succeed())
			cr.Status = status
			Expect(k8sClient.Status().Update(ctx, cr)).Should(Succeed())
		})

		It("should reconcile and create Crunchy PostgreCluster", func() {
			_, err = reconciler(cr).Reconcile(ctx, ctrl.Request{NamespacedName: crNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			_, err = crunchyReconciler().Reconcile(ctx, ctrl.Request{NamespacedName: crNamespacedName})
			Expect(err).NotTo(HaveOccurred())
		})

		pgBouncerSVC := &corev1.Service{}
		It("should retrieve pbbouncer service", func() {
			Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: crName + "-pgbouncer", Namespace: ns}, pgBouncerSVC)
				return err == nil
			}, time.Second*15, time.Millisecond*250).Should(BeTrue())
		})

		When("PGBouncer expose type is not LoadBalancer", func() {
			It("status host should be <pgbouncer-svc>.namespace", func() {
				updatePerconaPGClusterCR(ctx, crNamespacedName, func(cr *v2.PerconaPGCluster) {
					cr.Spec.Proxy.PGBouncer.ServiceExpose = &v2.ServiceExpose{
						Type: string(corev1.ServiceTypeClusterIP),
					}
				})

				_, err = reconciler(cr).Reconcile(ctx, ctrl.Request{NamespacedName: crNamespacedName})
				Expect(err).NotTo(HaveOccurred())
				_, err = crunchyReconciler().Reconcile(ctx, ctrl.Request{NamespacedName: crNamespacedName})
				Expect(err).NotTo(HaveOccurred())

				Eventually(func() bool {
					err := k8sClient.Get(ctx, crNamespacedName, cr)
					return err == nil
				}, time.Second*15, time.Millisecond*250).Should(BeTrue())
				Expect(cr.Status.Host).Should(Equal(pgBouncerSVC.Name + "." + ns + ".svc"))
				Expect(cr.Status.ObservedGeneration).Should(Equal(int64(2)))
			})
		})

		When("PGBouncer expose type is LoadBalancer", func() {
			It("status host should be LoadBalancer Ingress endpoint", func() {
				updatePerconaPGClusterCR(ctx, crNamespacedName, func(cr *v2.PerconaPGCluster) {
					cr.Spec.Proxy.PGBouncer.ServiceExpose = &v2.ServiceExpose{
						Type: string(corev1.ServiceTypeLoadBalancer),
					}
				})

				_, err := reconciler(cr).Reconcile(ctx, ctrl.Request{NamespacedName: crNamespacedName})
				Expect(err).NotTo(HaveOccurred())
				_, err = crunchyReconciler().Reconcile(ctx, ctrl.Request{NamespacedName: crNamespacedName})
				Expect(err).NotTo(HaveOccurred())

				// Update PGBouncer service status so it contains ingress IP
				Eventually(func() bool {
					err := k8sClient.Get(ctx, client.ObjectKeyFromObject(pgBouncerSVC), pgBouncerSVC)
					return err == nil
				}, time.Second*15, time.Millisecond*250).Should(BeTrue())
				pgBouncerSVC.Status.LoadBalancer.Ingress = append(pgBouncerSVC.Status.LoadBalancer.Ingress, corev1.LoadBalancerIngress{
					IP: "22.22.22.22",
				})
				Expect(k8sClient.Status().Update(ctx, pgBouncerSVC)).Should(Succeed())

				_, err = reconciler(cr).Reconcile(ctx, ctrl.Request{NamespacedName: crNamespacedName})
				Expect(err).NotTo(HaveOccurred())
				_, err = crunchyReconciler().Reconcile(ctx, ctrl.Request{NamespacedName: crNamespacedName})
				Expect(err).NotTo(HaveOccurred())

				Eventually(func() bool {
					err := k8sClient.Get(ctx, crNamespacedName, cr)
					return err == nil
				}, time.Second*15, time.Millisecond*250).Should(BeTrue())
				Expect(cr.Status.Host).Should(Equal("22.22.22.22"))

				Expect(cr.Status.ObservedGeneration).Should(Equal(int64(3)))
			})
		})
	})

	Context("Update PG cluster status.conditions", Ordered, func() {
		crName := ns + "-conditions"
		crNamespacedName := types.NamespacedName{Name: crName, Namespace: ns}

		cr, err := readDefaultCR(crName, ns)
		It("should read default cr.yaml and create PerconaPGCluster", func() {
			Expect(err).NotTo(HaveOccurred())
			status := cr.Status
			Expect(k8sClient.Create(ctx, cr)).Should(Succeed())
			cr.Status = status
			Expect(k8sClient.Status().Update(ctx, cr)).Should(Succeed())
		})

		It("should reconcile and create Crunchy PostgreCluster", func() {
			_, err = reconciler(cr).Reconcile(ctx, ctrl.Request{NamespacedName: crNamespacedName})
			Expect(err).NotTo(HaveOccurred())
		})

		When("conditions are not set", func() {
			It("should reconcile and update conditions", func() {
				_, err = reconciler(cr).Reconcile(ctx, ctrl.Request{NamespacedName: crNamespacedName})
				Expect(err).NotTo(HaveOccurred())
			})
			It("ReadyForBackup condition should be False", func() {
				Eventually(func() bool {
					err := k8sClient.Get(ctx, crNamespacedName, cr)
					return err == nil
				}, time.Second*15, time.Millisecond*250).Should(BeTrue())

				condition := meta.FindStatusCondition(cr.Status.Conditions, pNaming.ConditionClusterIsReadyForBackup)
				Expect(condition).Should(Not(BeNil()))
				Expect(condition.Status).Should(Equal(metav1.ConditionFalse))
			})
		})

		When("ConditionRepoHostReady is false", func() {
			It("ReadyForBackup condition should be False", func() {
				updateCrunchyPGClusterStatus(ctx, crNamespacedName, func(pgc *v1beta1.PostgresCluster) {
					_ = meta.SetStatusCondition(&pgc.Status.Conditions, metav1.Condition{
						Type:   postgrescluster.ConditionRepoHostReady,
						Status: metav1.ConditionFalse,
						Reason: "test",
					})
					_ = meta.SetStatusCondition(&pgc.Status.Conditions, metav1.Condition{
						Type:   postgrescluster.ConditionReplicaCreate,
						Status: metav1.ConditionTrue,
						Reason: "test",
					})
				})
				_, err = reconciler(cr).Reconcile(ctx, ctrl.Request{NamespacedName: crNamespacedName})
				Expect(err).NotTo(HaveOccurred())

				Eventually(func() bool {
					err := k8sClient.Get(ctx, crNamespacedName, cr)
					return err == nil
				}, time.Second*15, time.Millisecond*250).Should(BeTrue())

				condition := meta.FindStatusCondition(cr.Status.Conditions, pNaming.ConditionClusterIsReadyForBackup)
				Expect(condition.Status).Should(Equal(metav1.ConditionFalse))
				Expect(cr.Status.ObservedGeneration).Should(Equal(int64(1)))
			})
		})

		When("ConditionReplicaCreate is false", func() {
			It("ReadyForBackup condition should be False", func() {
				updateCrunchyPGClusterStatus(ctx, crNamespacedName, func(pgc *v1beta1.PostgresCluster) {
					_ = meta.SetStatusCondition(&pgc.Status.Conditions, metav1.Condition{
						Type:   postgrescluster.ConditionRepoHostReady,
						Status: metav1.ConditionTrue,
						Reason: "test",
					})
					_ = meta.SetStatusCondition(&pgc.Status.Conditions, metav1.Condition{
						Type:   postgrescluster.ConditionReplicaCreate,
						Status: metav1.ConditionFalse,
						Reason: "test",
					})
				})
				_, err = reconciler(cr).Reconcile(ctx, ctrl.Request{NamespacedName: crNamespacedName})
				Expect(err).NotTo(HaveOccurred())

				Eventually(func() bool {
					err := k8sClient.Get(ctx, crNamespacedName, cr)
					return err == nil
				}, time.Second*15, time.Millisecond*250).Should(BeTrue())

				condition := meta.FindStatusCondition(cr.Status.Conditions, pNaming.ConditionClusterIsReadyForBackup)
				Expect(condition.Status).Should(Equal(metav1.ConditionFalse))
			})
		})

		When("both are true", func() {
			It("ReadyForBackup condition should be True", func() {
				updateCrunchyPGClusterStatus(ctx, crNamespacedName, func(pgc *v1beta1.PostgresCluster) {
					_ = meta.SetStatusCondition(&pgc.Status.Conditions, metav1.Condition{
						Type:   postgrescluster.ConditionRepoHostReady,
						Status: metav1.ConditionTrue,
						Reason: "test",
					})
					_ = meta.SetStatusCondition(&pgc.Status.Conditions, metav1.Condition{
						Type:   postgrescluster.ConditionReplicaCreate,
						Status: metav1.ConditionTrue,
						Reason: "test",
					})
				})
				_, err = reconciler(cr).Reconcile(ctx, ctrl.Request{NamespacedName: crNamespacedName})
				Expect(err).NotTo(HaveOccurred())

				Eventually(func() bool {
					err := k8sClient.Get(ctx, crNamespacedName, cr)
					return err == nil
				}, time.Second*15, time.Millisecond*250).Should(BeTrue())

				condition := meta.FindStatusCondition(cr.Status.Conditions, pNaming.ConditionClusterIsReadyForBackup)
				Expect(condition.Status).Should(Equal(metav1.ConditionTrue))
			})
		})
	})
})

func reconcileAndAssertState(ctx context.Context, nn types.NamespacedName, cr *v2.PerconaPGCluster, expectedState v2.AppState) {
	_, err := reconciler(cr).Reconcile(ctx, ctrl.Request{NamespacedName: nn})
	Expect(err).NotTo(HaveOccurred())

	Eventually(func() bool {
		err = k8sClient.Get(ctx, nn, cr)
		return err == nil
	}, time.Second*15, time.Millisecond*250).Should(BeTrue())

	Expect(cr.Status.State).Should(Equal(expectedState))
}
