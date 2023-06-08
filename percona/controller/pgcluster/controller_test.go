package pgcluster

import (
	"context"
	// #nosec G501
	"crypto/md5"
	"fmt"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	gs "github.com/onsi/gomega/gstruct"
	"go.opentelemetry.io/otel/trace"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/percona/percona-postgresql-operator/internal/controller/postgrescluster"
	"github.com/percona/percona-postgresql-operator/internal/controller/runtime"
	"github.com/percona/percona-postgresql-operator/internal/naming"
	perconaController "github.com/percona/percona-postgresql-operator/percona/controller"
	"github.com/percona/percona-postgresql-operator/percona/pmm"
	v2 "github.com/percona/percona-postgresql-operator/pkg/apis/pgv2.percona.com/v2"
	"github.com/percona/percona-postgresql-operator/pkg/apis/postgres-operator.crunchydata.com/v1beta1"
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

	It("should get instance-set sts", func() {
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

	Context("Instance-set statefulset", func() {
		When("sts doesn't have pmm sidecar", func() {
			It("should not have pmm container", func() {
				Expect(havePMMSidecar(sts)).To(BeFalse())
			})
		})

		When("pmm secret has no data", func() {
			BeforeAll(func() {
				Expect(k8sClient.Create(ctx, &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "cluster1-pmm-secret",
						Namespace: ns,
					},
					Data: map[string][]byte{
						"PMM_SERVER_KEY": {},
					},
				})).Should(Succeed())

				_, err := reconciler().Reconcile(ctx, ctrl.Request{NamespacedName: crNamespacedName})
				Expect(err).NotTo(HaveOccurred())
				_, err = crunchyReconciler().Reconcile(ctx, ctrl.Request{NamespacedName: crNamespacedName})
				Expect(err).NotTo(HaveOccurred())
			})

			It("should not have pmm container", func() {
				Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(&sts), &sts)).Should(Succeed())

				Expect(havePMMSidecar(sts)).To(BeFalse())
			})
		})

		When("pmm secret has data", func() {
			BeforeAll(func() {
				Expect(k8sClient.Update(ctx, &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "cluster1-pmm-secret",
						Namespace: ns,
					},
					Data: map[string][]byte{
						"PMM_SERVER_KEY": []byte("some-data"),
					},
				})).Should(Succeed())

				_, err := reconciler().Reconcile(ctx, ctrl.Request{NamespacedName: crNamespacedName})
				Expect(err).NotTo(HaveOccurred())
				_, err = crunchyReconciler().Reconcile(ctx, ctrl.Request{NamespacedName: crNamespacedName})
				Expect(err).NotTo(HaveOccurred())
			})

			It("should have pmm container", func() {
				Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(&sts), &sts)).Should(Succeed())

				Expect(havePMMSidecar(sts)).To(BeTrue())
			})

			It("should have PMM secret hash", func() {
				Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(&sts), &sts)).Should(Succeed())
				Expect(sts.Spec.Template.ObjectMeta.Annotations).To(HaveKey(v2.AnnotationPMMSecretHash))
			})

			It("should label PMM secret", func() {
				secret := &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "cluster1-pmm-secret",
						Namespace: ns,
					},
				}
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(secret), secret)
				Expect(err).NotTo(HaveOccurred())

				Expect(secret.Labels).To(HaveKeyWithValue(v2.LabelPMMSecret, "true"))
				Expect(secret.Labels).To(HaveKeyWithValue(naming.LabelCluster, crName))
			})
		})

		When("cr has disabled pmm", func() {
			BeforeAll(func() {
				Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(cr), cr)).Should(Succeed())
				cr.Spec.PMM.Enabled = false
				Expect(k8sClient.Update(ctx, cr)).Should(Succeed())

				_, err := reconciler().Reconcile(ctx, ctrl.Request{NamespacedName: crNamespacedName})
				Expect(err).NotTo(HaveOccurred())
				_, err = crunchyReconciler().Reconcile(ctx, ctrl.Request{NamespacedName: crNamespacedName})
				Expect(err).NotTo(HaveOccurred())
			})

			It("should not have pmm container", func() {
				Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(&sts), &sts)).Should(Succeed())

				Expect(havePMMSidecar(sts)).To(BeFalse())
			})
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

var _ = Describe("Monitor user password change", Ordered, func() {
	ctx := context.Background()

	const crName = "monitor-pass-user-change-test"
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

	It("controller should reconcile", func() {
		_, err := reconciler().Reconcile(ctx, ctrl.Request{NamespacedName: crNamespacedName})
		Expect(err).NotTo(HaveOccurred())
		_, err = crunchyReconciler().Reconcile(ctx, ctrl.Request{NamespacedName: crNamespacedName})
		Expect(err).NotTo(HaveOccurred())
	})

	monitorUserSecret := &corev1.Secret{}

	stsList := &appsv1.StatefulSetList{}
	labels := map[string]string{
		"postgres-operator.crunchydata.com/data":    "postgres",
		"postgres-operator.crunchydata.com/cluster": crName,
	}

	When("PMM is enabled", Ordered, func() {
		It("should reconcile", func() {
			_, err := reconciler().Reconcile(ctx, ctrl.Request{NamespacedName: crNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			_, err = crunchyReconciler().Reconcile(ctx, ctrl.Request{NamespacedName: crNamespacedName})
			Expect(err).NotTo(HaveOccurred())
		})

		It("Instance sets should have monitor user secret hash annotation", func() {
			nn := types.NamespacedName{Namespace: ns, Name: cr.Name + "-" + naming.RolePostgresUser + "-" + pmm.MonitoringUser}
			Expect(k8sClient.Get(ctx, nn, monitorUserSecret)).NotTo(HaveOccurred())

			secretString := fmt.Sprintln(monitorUserSecret.Data)
			// #nosec G401
			currentHash := fmt.Sprintf("%x", md5.Sum([]byte(secretString)))

			stsList := &appsv1.StatefulSetList{}
			labels := map[string]string{
				"postgres-operator.crunchydata.com/data":    "postgres",
				"postgres-operator.crunchydata.com/cluster": crName,
			}
			err = k8sClient.List(ctx, stsList, client.InNamespace(cr.Namespace), client.MatchingLabels(labels))
			Expect(err).NotTo(HaveOccurred())
			Expect(stsList.Items).NotTo(BeEmpty())

			Expect(stsList.Items).Should(ContainElement(gs.MatchFields(gs.IgnoreExtras, gs.Fields{
				"ObjectMeta": gs.MatchFields(gs.IgnoreExtras, gs.Fields{
					"Annotations": HaveKeyWithValue(v2.AnnotationMonitorUserSecretHash, currentHash),
				}),
			})))

		})
	})

	When("Monitor user password is updated", Ordered, func() {
		It("should update secret data", func() {
			monitorUserSecret.Data["password"] = []byte("some-new-pas")
			Expect(k8sClient.Update(ctx, monitorUserSecret)).To(Succeed())
		})

		It("should reconcile", func() {
			_, err := reconciler().Reconcile(ctx, ctrl.Request{NamespacedName: crNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			_, err = crunchyReconciler().Reconcile(ctx, ctrl.Request{NamespacedName: crNamespacedName})
			Expect(err).NotTo(HaveOccurred())
		})

		It("Instance sets should have updated user secret hash annotation", func() {
			nn := types.NamespacedName{Namespace: ns, Name: cr.Name + "-" + naming.RolePostgresUser + "-" + pmm.MonitoringUser}
			Expect(k8sClient.Get(ctx, nn, monitorUserSecret)).NotTo(HaveOccurred())

			secretString := fmt.Sprintln(monitorUserSecret.Data)
			// #nosec G401
			currentHash := fmt.Sprintf("%x", md5.Sum([]byte(secretString)))

			err = k8sClient.List(ctx, stsList, client.InNamespace(cr.Namespace), client.MatchingLabels(labels))
			Expect(err).NotTo(HaveOccurred())
			Expect(stsList.Items).NotTo(BeEmpty())

			Expect(stsList.Items).Should(ContainElement(gs.MatchFields(gs.IgnoreExtras, gs.Fields{
				"ObjectMeta": gs.MatchFields(gs.IgnoreExtras, gs.Fields{
					"Annotations": HaveKeyWithValue(v2.AnnotationMonitorUserSecretHash, currentHash),
				}),
			})))
		})
	})
})

// tracerWithCounter is a tracer that counts the number of times the Reconcile is called. It should be used for crunchy reconciler.
type tracerWithCounter struct {
	counter int
	t       trace.Tracer
}

func (t *tracerWithCounter) Start(ctx context.Context, spanName string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	ctx, span := t.t.Start(ctx, spanName, opts...)
	if spanName == "Reconcile" {
		t.counter++
	}
	return ctx, span
}

func getReconcileCount(r *postgrescluster.Reconciler) int {
	return r.Tracer.(*tracerWithCounter).counter
}

var _ = Describe("Watching secrets", Ordered, func() {
	ctx := context.Background()

	const crName = "watch-secret-test"
	const ns = crName

	crunchyR := crunchyReconciler()
	r := reconciler()

	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:      crName,
			Namespace: ns,
		},
	}

	mgrCtx, cancel := context.WithCancel(ctx)
	wg := sync.WaitGroup{}

	BeforeAll(func() {
		By("Creating the Namespace to perform the tests")
		err := k8sClient.Create(ctx, namespace)

		Expect(err).To(Not(HaveOccurred()))
		mgr, err := runtime.CreateRuntimeManager(ns, cfg, true)
		Expect(err).To(Succeed())
		Expect(v2.AddToScheme(mgr.GetScheme())).To(Succeed())

		r.Client = mgr.GetClient()
		crunchyR.Client = mgr.GetClient()
		crunchyR.Tracer = &tracerWithCounter{t: crunchyR.Tracer}

		cm := &perconaController.CustomManager{Manager: mgr}
		Expect(crunchyR.SetupWithManager(cm)).To(Succeed())

		Expect(cm.Controller()).NotTo(BeNil())
		r.CrunchyController = cm.Controller()
		Expect(r.SetupWithManager(mgr)).To(Succeed())

		wg.Add(1)
		go func() {
			Expect(mgr.Start(mgrCtx)).To(Succeed())
			wg.Done()
		}()
	})

	AfterAll(func() {
		By("Stopping manager")
		cancel()
		wg.Wait()

		By("Deleting the Namespace to perform the tests")
		_ = k8sClient.Delete(ctx, namespace)
	})

	cr, err := readDefaultCR(crName, ns)
	It("should read default cr.yaml", func() {
		Expect(err).NotTo(HaveOccurred())
	})

	reconcileCount := 0
	Context("Create cluster and wait until Reconcile stops", func() {
		It("should create PerconaPGCluster and PostgresCluster", func() {
			Expect(k8sClient.Create(ctx, cr)).Should(Succeed())

			Eventually(func() error {
				return k8sClient.Get(ctx, client.ObjectKeyFromObject(cr), new(v2.PerconaPGCluster))
			}, time.Second*15, time.Millisecond*250).Should(BeNil())

			Eventually(func() error {
				return k8sClient.Get(ctx, client.ObjectKeyFromObject(cr), new(v1beta1.PostgresCluster))
			}, time.Second*15, time.Millisecond*250).Should(BeNil())
		})

		It("should wait until PostgresCluster will stop to Reconcile multiple times", func() {
			Eventually(func() bool {
				pgCluster := new(v1beta1.PostgresCluster)
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(cr), pgCluster)
				if err != nil {
					return false
				}
				// When ManagedFields get field with `status` subresource, crunchy's Reconcile stops being called
				for _, f := range pgCluster.ManagedFields {
					if f.Manager == postgrescluster.ControllerName && f.Subresource == "status" {
						return true
					}
				}

				return false
			}, time.Second*30, time.Millisecond*250).Should(Equal(true))
			reconcileCount = getReconcileCount(crunchyR)
		})
	})

	var secret *corev1.Secret
	Context("Create secret", func() {
		secret = &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "some-secret",
				Namespace: ns,
				Labels: map[string]string{
					naming.LabelCluster: cr.Name,
				},
			},
			Data: map[string][]byte{
				"some-data": []byte("data"),
			},
		}
		It("should create secret", func() {
			Expect(k8sClient.Create(ctx, secret)).To(Succeed())
			Eventually(func() error {
				return k8sClient.Get(ctx, client.ObjectKeyFromObject(secret), new(corev1.Secret))
			}, time.Second*15, time.Millisecond*250).Should(BeNil())
		})

		It("should reconcile 0 times", func() {
			Eventually(func() int { return getReconcileCount(crunchyR) }, time.Second*15, time.Millisecond*250).
				Should(Equal(reconcileCount))
		})
	})

	Context("Update secret data", func() {
		It("should update secret data", func() {
			secret.Data["some-data"] = []byte("updated-data")
			Expect(k8sClient.Update(ctx, secret)).To(Succeed())
		})

		It("should wait until secret is updated", func() {
			Eventually(func() bool {
				newSecret := new(corev1.Secret)
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(secret), newSecret)
				if err != nil {
					return false
				}
				return string(newSecret.Data["some-data"]) == "updated-data"
			}, time.Second*15, time.Millisecond*250).Should(BeTrue())
		})

		It("should reconcile 1 time", func() {
			Eventually(func() int { return getReconcileCount(crunchyR) }, time.Second*15, time.Millisecond*250).
				Should(Equal(reconcileCount + 1))
		})

		It("should update secret data", func() {
			secret.Data["some-data"] = []byte("updated-data-2")
			Expect(k8sClient.Update(ctx, secret)).To(Succeed())
		})

		It("should wait until secret is updated", func() {
			Eventually(func() bool {
				newSecret := new(corev1.Secret)
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(secret), newSecret)
				if err != nil {
					return false
				}
				return string(newSecret.Data["some-data"]) == "updated-data-2"
			}, time.Second*15, time.Millisecond*250).Should(BeTrue())
		})

		It("should reconcile 2 times", func() {
			Eventually(func() int { return getReconcileCount(crunchyR) }, time.Second*15, time.Millisecond*250).
				Should(Equal(reconcileCount + 2))
		})
	})

	Context("Update secret data and remove labels", func() {
		It("should remove cluster label and update data", func() {
			secret.Labels = make(map[string]string)
			secret.Data["some-data"] = []byte("updated-data-3")
			Expect(k8sClient.Update(ctx, secret)).To(Succeed())
		})

		It("should wait until secret is updated", func() {
			Eventually(func() bool {
				newSecret := new(corev1.Secret)
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(secret), newSecret)
				if err != nil {
					return false
				}
				return string(newSecret.Data["some-data"]) == "updated-data-3"
			}, time.Second*15, time.Millisecond*250).Should(BeTrue())
		})

		It("should reconcile 2 times", func() {
			Eventually(func() int { return getReconcileCount(crunchyR) }, time.Second*15, time.Millisecond*250).
				Should(Equal(reconcileCount + 2))
		})
	})
})

var _ = Describe("Users", Ordered, func() {
	ctx := context.Background()

	const crName = "users-test"
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

	When("Cluster without PMM is created", Ordered, func() {
		cr, err := readDefaultCR(crName, ns)
		It("should read defautl cr.yaml and create PerconaPGCluster without PMM", func() {
			Expect(err).NotTo(HaveOccurred())

			Expect(k8sClient.Create(ctx, cr)).Should(Succeed())
		})

		It("should add default user", func() {
			_, err := reconciler().Reconcile(ctx, ctrl.Request{NamespacedName: crNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			_, err = crunchyReconciler().Reconcile(ctx, ctrl.Request{NamespacedName: crNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			secList := &corev1.SecretList{}

			labels := map[string]string{
				"postgres-operator.crunchydata.com/cluster": cr.Name,
				"postgres-operator.crunchydata.com/role":    naming.RolePostgresUser,
			}
			err = k8sClient.List(ctx, secList, client.InNamespace(cr.Namespace), client.MatchingLabels(labels))
			Expect(err).NotTo(HaveOccurred())
			Expect(secList.Items).NotTo(BeEmpty())
			Expect(secList.Items).Should(ContainElement(gs.MatchFields(gs.IgnoreExtras, gs.Fields{
				"ObjectMeta": gs.MatchFields(gs.IgnoreExtras, gs.Fields{
					"Name": Equal(cr.Name + "-" + naming.RolePostgresUser + "-" + cr.Name),
				}),
			})))
		})

		When("PMM is enabled on a running cluster", func() {
			It("should enable PMM and update the cluster", func() {
				Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(cr), cr)).Should(Succeed())
				cr.Spec.PMM.Enabled = true
				Expect(k8sClient.Update(ctx, cr)).Should(Succeed())
			})

			It("should add monitor user along side the default user", func() {
				_, err := reconciler().Reconcile(ctx, ctrl.Request{NamespacedName: crNamespacedName})
				Expect(err).NotTo(HaveOccurred())
				_, err = crunchyReconciler().Reconcile(ctx, ctrl.Request{NamespacedName: crNamespacedName})
				Expect(err).NotTo(HaveOccurred())

				secList := &corev1.SecretList{}

				labels := map[string]string{
					"postgres-operator.crunchydata.com/cluster": cr.Name,
					"postgres-operator.crunchydata.com/role":    naming.RolePostgresUser,
				}
				err = k8sClient.List(ctx, secList, client.InNamespace(cr.Namespace), client.MatchingLabels(labels))
				Expect(err).NotTo(HaveOccurred())
				Expect(secList.Items).NotTo(BeEmpty())
				Expect(secList.Items).Should(ContainElements(
					gs.MatchFields(gs.IgnoreExtras, gs.Fields{
						"ObjectMeta": gs.MatchFields(gs.IgnoreExtras, gs.Fields{
							"Name": Equal(cr.Name + "-" + naming.RolePostgresUser + "-" + pmm.MonitoringUser),
						}),
					}),
					gs.MatchFields(gs.IgnoreExtras, gs.Fields{
						"ObjectMeta": gs.MatchFields(gs.IgnoreExtras, gs.Fields{
							"Name": Equal(cr.Name + "-" + naming.RolePostgresUser + "-" + cr.Name),
						}),
					}),
				))
			})
		})

		It("should delete cr and all secrets", func() {
			Expect(k8sClient.Delete(ctx, cr)).Should(Succeed())
			Expect(k8sClient.DeleteAllOf(context.Background(), &corev1.Secret{}, client.InNamespace(ns)))
		})
	})

	When("Cluster with PMM is created", Ordered, func() {
		cr, err := readDefaultCR(crName, ns)
		It("should read defautl cr.yaml and create PerconaPGCluster with PMM enabled", func() {
			Expect(err).NotTo(HaveOccurred())

			cr.Spec.PMM.Enabled = true
			Expect(k8sClient.Create(ctx, cr)).Should(Succeed())
		})

		It("should create defaul and monitor user", func() {
			_, err := reconciler().Reconcile(ctx, ctrl.Request{NamespacedName: crNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			_, err = crunchyReconciler().Reconcile(ctx, ctrl.Request{NamespacedName: crNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			secList := &corev1.SecretList{}

			labels := map[string]string{
				"postgres-operator.crunchydata.com/cluster": cr.Name,
				"postgres-operator.crunchydata.com/role":    naming.RolePostgresUser,
			}
			err = k8sClient.List(ctx, secList, client.InNamespace(cr.Namespace), client.MatchingLabels(labels))
			Expect(err).NotTo(HaveOccurred())
			Expect(secList.Items).NotTo(BeEmpty())
			Expect(secList.Items).Should(ContainElements(
				gs.MatchFields(gs.IgnoreExtras, gs.Fields{
					"ObjectMeta": gs.MatchFields(gs.IgnoreExtras, gs.Fields{
						"Name": Equal(cr.Name + "-" + naming.RolePostgresUser + "-" + pmm.MonitoringUser),
					}),
				}),
				gs.MatchFields(gs.IgnoreExtras, gs.Fields{
					"ObjectMeta": gs.MatchFields(gs.IgnoreExtras, gs.Fields{
						"Name": Equal(cr.Name + "-" + naming.RolePostgresUser + "-" + cr.Name),
					}),
				}),
			))
		})
	})
})
