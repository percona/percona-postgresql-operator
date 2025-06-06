//go:build envtest
// +build envtest

package pgcluster

import (
	"context"
	"crypto/md5" //nolint:gosec
	"fmt"
	"strconv"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	gs "github.com/onsi/gomega/gstruct"
	"github.com/pkg/errors"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/percona/percona-postgresql-operator/internal/controller/postgrescluster"
	"github.com/percona/percona-postgresql-operator/internal/feature"
	"github.com/percona/percona-postgresql-operator/internal/naming"
	perconaController "github.com/percona/percona-postgresql-operator/percona/controller"
	pNaming "github.com/percona/percona-postgresql-operator/percona/naming"
	"github.com/percona/percona-postgresql-operator/percona/runtime"
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
		status := cr.Status
		Expect(k8sClient.Create(ctx, cr)).Should(Succeed())
		cr.Status = status
		Expect(k8sClient.Status().Update(ctx, cr)).Should(Succeed())
	})

	Context("Reconcile controller", func() {
		It("Controller should reconcile", func() {
			_, err := reconciler(cr).Reconcile(ctx, ctrl.Request{NamespacedName: crNamespacedName})
			Expect(err).NotTo(HaveOccurred())
		})
	})
})

var _ = Describe("Annotations", Ordered, func() {
	ctx := context.Background()

	const crName = "annotations-test"
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

	It("should create PerconaPGCluster with annotations", func() {
		cr.Annotations["pgv2.percona.com/trigger-switchover"] = "true"
		cr.Annotations["egedemo.com/test"] = "true"

		status := cr.Status
		Expect(k8sClient.Create(ctx, cr)).Should(Succeed())
		cr.Status = status
		Expect(k8sClient.Status().Update(ctx, cr)).Should(Succeed())
	})

	Context("Reconcile controller", func() {
		It("Controller should reconcile", func() {
			_, err := reconciler(cr).Reconcile(ctx, ctrl.Request{NamespacedName: crNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			_, err = crunchyReconciler().Reconcile(ctx, ctrl.Request{NamespacedName: crNamespacedName})
			Expect(err).NotTo(HaveOccurred())
		})
	})

	crunchyCr := v1beta1.PostgresCluster{}

	It("should get PostgresCluster", func() {
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(cr), &crunchyCr)).Should(Succeed())
	})

	It("should have annotations", func() {
		_, ok := crunchyCr.Annotations["postgres-operator.crunchydata.com/trigger-switchover"]
		Expect(ok).To(BeTrue())

		_, ok = crunchyCr.Annotations["egedemo.com/test"]
		Expect(ok).To(BeTrue())
	})
})

var _ = Describe("PMM sidecar", Ordered, func() {
	gate := feature.NewGate()
	err := gate.SetFromMap(map[string]bool{
		feature.InstanceSidecars: true,
	})
	Expect(err).NotTo(HaveOccurred())

	ctx := feature.NewContext(context.Background(), gate)

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
		status := cr.Status
		Expect(k8sClient.Create(ctx, cr)).Should(Succeed())
		cr.Status = status
		Expect(k8sClient.Status().Update(ctx, cr)).Should(Succeed())
	})

	Context("Reconcile controller", func() {
		It("Controller should reconcile", func() {
			_, err := reconciler(cr).Reconcile(ctx, ctrl.Request{NamespacedName: crNamespacedName})
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

				_, err := reconciler(cr).Reconcile(ctx, ctrl.Request{NamespacedName: crNamespacedName})
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

				_, err := reconciler(cr).Reconcile(ctx, ctrl.Request{NamespacedName: crNamespacedName})
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
				Expect(sts.Spec.Template.ObjectMeta.Annotations).To(HaveKey(pNaming.AnnotationPMMSecretHash))
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

				_, err := reconciler(cr).Reconcile(ctx, ctrl.Request{NamespacedName: crNamespacedName})
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
		status := cr.Status
		Expect(k8sClient.Create(ctx, cr)).Should(Succeed())
		cr.Status = status
		Expect(k8sClient.Status().Update(ctx, cr)).Should(Succeed())
	})

	It("controller should reconcile", func() {
		_, err := reconciler(cr).Reconcile(ctx, ctrl.Request{NamespacedName: crNamespacedName})
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
			_, err := reconciler(cr).Reconcile(ctx, ctrl.Request{NamespacedName: crNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			_, err = crunchyReconciler().Reconcile(ctx, ctrl.Request{NamespacedName: crNamespacedName})
			Expect(err).NotTo(HaveOccurred())
		})

		It("Instance sets should have monitor user secret hash annotation", func() {
			nn := types.NamespacedName{Namespace: ns, Name: cr.Name + "-" + naming.RolePostgresUser + "-" + v2.UserMonitoring}
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
					"Annotations": HaveKeyWithValue(pNaming.AnnotationMonitorUserSecretHash, currentHash),
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
			_, err := reconciler(cr).Reconcile(ctx, ctrl.Request{NamespacedName: crNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			_, err = crunchyReconciler().Reconcile(ctx, ctrl.Request{NamespacedName: crNamespacedName})
			Expect(err).NotTo(HaveOccurred())
		})

		It("Instance sets should have updated user secret hash annotation", func() {
			nn := types.NamespacedName{Namespace: ns, Name: cr.Name + "-" + naming.RolePostgresUser + "-" + v2.UserMonitoring}
			Expect(k8sClient.Get(ctx, nn, monitorUserSecret)).NotTo(HaveOccurred())

			secretString := fmt.Sprintln(monitorUserSecret.Data)
			// #nosec G401
			currentHash := fmt.Sprintf("%x", md5.Sum([]byte(secretString)))

			err = k8sClient.List(ctx, stsList, client.InNamespace(cr.Namespace), client.MatchingLabels(labels))
			Expect(err).NotTo(HaveOccurred())
			Expect(stsList.Items).NotTo(BeEmpty())

			Expect(stsList.Items).Should(ContainElement(gs.MatchFields(gs.IgnoreExtras, gs.Fields{
				"ObjectMeta": gs.MatchFields(gs.IgnoreExtras, gs.Fields{
					"Annotations": HaveKeyWithValue(pNaming.AnnotationMonitorUserSecretHash, currentHash),
				}),
			})))
		})
	})
})

// tracerWithCounter is a tracer that counts the number of times the Reconcile is called. It should be used for crunchy reconciler.
type tracerWithCounter struct {
	noop.Tracer
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
	r := reconciler(&v2.PerconaPGCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      crName,
			Namespace: ns,
		},
	})

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
		Expect(err).NotTo(HaveOccurred())

		gate := feature.NewGate()
		err = gate.SetFromMap(map[string]bool{})
		Expect(err).NotTo(HaveOccurred())

		Expect(err).To(Not(HaveOccurred()))
		mgr, err := runtime.CreateRuntimeManager(namespace.Name, cfg, true, true, gate)
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
	for i := range cr.Spec.Backups.PGBackRest.Repos {
		cr.Spec.Backups.PGBackRest.Repos[i].BackupSchedules = nil
	}

	reconcileCount := 0
	Context("Create cluster and wait until Reconcile stops", func() {
		It("should create PerconaPGCluster and PostgresCluster", func() {
			status := cr.Status
			Expect(k8sClient.Create(ctx, cr)).Should(Succeed())
			cr.Status = status
			Expect(k8sClient.Status().Update(ctx, cr)).Should(Succeed())

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
			}, time.Second*60, time.Millisecond*250).Should(Equal(true))
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

			status := cr.Status
			Expect(k8sClient.Create(ctx, cr)).Should(Succeed())
			cr.Status = status
			Expect(k8sClient.Status().Update(ctx, cr)).Should(Succeed())
		})

		It("should add default user", func() {
			_, err := reconciler(cr).Reconcile(ctx, ctrl.Request{NamespacedName: crNamespacedName})
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
				_, err := reconciler(cr).Reconcile(ctx, ctrl.Request{NamespacedName: crNamespacedName})
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
							"Name": Equal(cr.Name + "-" + naming.RolePostgresUser + "-" + v2.UserMonitoring),
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

			Eventually(func() error {
				_, err = reconciler(cr).Reconcile(ctx, ctrl.Request{NamespacedName: crNamespacedName})
				if err != nil {
					return err
				}

				_, err = crunchyReconciler().Reconcile(ctx, ctrl.Request{NamespacedName: crNamespacedName})
				if err != nil {
					return err
				}

				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(cr), new(v2.PerconaPGCluster))
				if k8serrors.IsNotFound(err) {
					return nil
				}

				return errors.New("cluster not deleted")
			}, time.Second*15, time.Millisecond*250).Should(BeNil())
		})
	})

	When("Cluster with PMM is created", Ordered, func() {
		cr, err := readDefaultCR(crName, ns)
		It("should read defautl cr.yaml and create PerconaPGCluster with PMM enabled", func() {
			Expect(err).NotTo(HaveOccurred())

			cr.Spec.PMM.Enabled = true
			status := cr.Status
			Expect(k8sClient.Create(ctx, cr)).Should(Succeed())
			cr.Status = status
			Expect(k8sClient.Status().Update(ctx, cr)).Should(Succeed())
		})

		It("should create defaul and monitor user", func() {
			_, err := reconciler(cr).Reconcile(ctx, ctrl.Request{NamespacedName: crNamespacedName})
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
						"Name": Equal(cr.Name + "-" + naming.RolePostgresUser + "-" + v2.UserMonitoring),
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

var _ = Describe("Version labels", Ordered, func() {
	ctx := context.Background()

	const crName = "ver-labels"
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

	It("should create PerconaPGCluster", func() {
		status := cr.Status
		Expect(k8sClient.Create(ctx, cr)).Should(Succeed())
		cr.Status = status
		Expect(k8sClient.Status().Update(ctx, cr)).Should(Succeed())
	})

	It("should reconcile", func() {
		// Run multiple reconcile cycles to ensure all resources are created
		for i := 0; i < 3; i++ {
			_, err := reconciler(cr).Reconcile(ctx, ctrl.Request{NamespacedName: crNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			_, err = crunchyReconciler().Reconcile(ctx, ctrl.Request{NamespacedName: crNamespacedName})
			Expect(err).NotTo(HaveOccurred())
		}
	})

	It("should label PostgreSQL statefulsets", func() {
		stsList := &appsv1.StatefulSetList{}
		labels := map[string]string{
			"postgres-operator.crunchydata.com/data":         "postgres",
			"postgres-operator.crunchydata.com/instance-set": "instance1",
			"postgres-operator.crunchydata.com/cluster":      crName,
		}
		err = k8sClient.List(ctx, stsList, client.InNamespace(cr.Namespace), client.MatchingLabels(labels))
		Expect(err).NotTo(HaveOccurred())
		Expect(stsList.Items).NotTo(BeEmpty())

		Expect(stsList.Items).Should(ContainElement(gs.MatchFields(gs.IgnoreExtras, gs.Fields{
			"ObjectMeta": gs.MatchFields(gs.IgnoreExtras, gs.Fields{
				"Labels": HaveKeyWithValue(v2.LabelOperatorVersion, cr.Spec.CRVersion),
			}),
		})))
	})

	It("should label PGBouncer deployments", func() {
		depList := &appsv1.DeploymentList{}
		labels := map[string]string{
			"postgres-operator.crunchydata.com/role":    "pgbouncer",
			"postgres-operator.crunchydata.com/cluster": crName,
		}
		err = k8sClient.List(ctx, depList, client.InNamespace(cr.Namespace), client.MatchingLabels(labels))
		Expect(err).NotTo(HaveOccurred())
		Expect(depList.Items).NotTo(BeEmpty())

		Expect(depList.Items).Should(ContainElement(gs.MatchFields(gs.IgnoreExtras, gs.Fields{
			"ObjectMeta": gs.MatchFields(gs.IgnoreExtras, gs.Fields{
				"Labels": HaveKeyWithValue(v2.LabelOperatorVersion, cr.Spec.CRVersion),
			}),
		})))
	})

	It("should label PGBackRest statefulsets", func() {
		stsList := &appsv1.StatefulSetList{}
		labels := map[string]string{
			"postgres-operator.crunchydata.com/data":    "pgbackrest",
			"postgres-operator.crunchydata.com/cluster": crName,
		}

		// Add a retry loop to give time for the StatefulSets to be created
		Eventually(func() bool {
			err := k8sClient.List(ctx, stsList, client.InNamespace(cr.Namespace), client.MatchingLabels(labels))
			if err != nil {
				GinkgoWriter.Printf("Error listing StatefulSets: %v\n", err)
				return false
			}

			if len(stsList.Items) == 0 {
				// List all StatefulSets to debug what's available
				allStsList := &appsv1.StatefulSetList{}
				err := k8sClient.List(ctx, allStsList, client.InNamespace(cr.Namespace))
				if err == nil {
					GinkgoWriter.Printf("Available StatefulSets in namespace %s:\n", cr.Namespace)
					for _, sts := range allStsList.Items {
						GinkgoWriter.Printf("  - %s (labels: %v)\n", sts.Name, sts.Labels)
					}
				}
				return false
			}

			return true
		}, time.Second*30, time.Millisecond*500).Should(BeTrue())

		Expect(stsList.Items).Should(ContainElement(gs.MatchFields(gs.IgnoreExtras, gs.Fields{
			"ObjectMeta": gs.MatchFields(gs.IgnoreExtras, gs.Fields{
				"Labels": HaveKeyWithValue(v2.LabelOperatorVersion, cr.Spec.CRVersion),
			}),
		})))
	})
})

var _ = Describe("Services with LoadBalancerSourceRanges", Ordered, func() {
	ctx := context.Background()

	const crName = "lb-source-ranges"
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

	It("should create PerconaPGCluster with service exposed with loadBalancerSourceRanges", func() {
		cr.Spec.Expose = &v2.ServiceExpose{
			Type:                     "LoadBalancer",
			LoadBalancerSourceRanges: []string{"10.10.10.10/16"},
		}
		status := cr.Status
		Expect(k8sClient.Create(ctx, cr)).Should(Succeed())
		cr.Status = status
		Expect(k8sClient.Status().Update(ctx, cr)).Should(Succeed())
	})

	It("should reconcile", func() {
		_, err := reconciler(cr).Reconcile(ctx, ctrl.Request{NamespacedName: crNamespacedName})
		Expect(err).NotTo(HaveOccurred())
		_, err = crunchyReconciler().Reconcile(ctx, ctrl.Request{NamespacedName: crNamespacedName})
		Expect(err).NotTo(HaveOccurred())
	})

	It("should create services with loadBalancerSourceRanges ", func() {
		haService := &corev1.Service{}
		err := k8sClient.Get(ctx, types.NamespacedName{Namespace: cr.Namespace, Name: cr.Name + "-ha"}, haService)
		Expect(err).NotTo(HaveOccurred())
		Expect(haService.Spec.LoadBalancerSourceRanges).To(Equal(cr.Spec.Expose.LoadBalancerSourceRanges))
	})
})

var _ = Describe("Pause with backup", Ordered, func() {
	ctx := context.Background()

	const crName = "backup-pause"
	const backupName = crName + "-backup"
	const ns = crName
	crNamespacedName := types.NamespacedName{Name: crName, Namespace: ns}
	backupNamespacedName := types.NamespacedName{Name: backupName, Namespace: ns}

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

	cr.Spec.Backups.PGBackRest.Manual = nil

	It("should create PerconaPGCluster", func() {
		status := cr.Status
		Expect(k8sClient.Create(ctx, cr)).Should(Succeed())
		cr.Status = status
		Expect(k8sClient.Status().Update(ctx, cr)).Should(Succeed())
	})

	pgBackup := &v2.PerconaPGBackup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      backupName,
			Namespace: ns,
		},
		Spec: v2.PerconaPGBackupSpec{
			PGCluster: crName,
			RepoName:  "repo1",
		},
	}

	It("should create PerconaPGBackup", func() {
		Expect(k8sClient.Create(ctx, pgBackup)).Should(Succeed())
	})

	It("should reconcile", func() {
		_, err := reconciler(cr).Reconcile(ctx, ctrl.Request{NamespacedName: crNamespacedName})
		Expect(err).NotTo(HaveOccurred())
		_, err = crunchyReconciler().Reconcile(ctx, ctrl.Request{NamespacedName: crNamespacedName})
		Expect(err).NotTo(HaveOccurred())
	})

	When("cluster is paused", func() {
		Context("pause cluster", func() {
			It("should pause cluster", func() {
				Expect(k8sClient.Get(ctx, crNamespacedName, cr)).To(Succeed())
				t := true
				cr.Spec.Pause = &t
				Expect(k8sClient.Update(ctx, cr)).To(Succeed())
			})
			It("should reconcile", func() {
				_, err := reconciler(cr).Reconcile(ctx, ctrl.Request{NamespacedName: crNamespacedName})
				Expect(err).NotTo(HaveOccurred())
				_, err = crunchyReconciler().Reconcile(ctx, ctrl.Request{NamespacedName: crNamespacedName})
				Expect(err).NotTo(HaveOccurred())
			})
			It("should be paused crunchy cluster", func() {
				postgresCluster := &v1beta1.PostgresCluster{}
				Expect(k8sClient.Get(ctx, crNamespacedName, postgresCluster)).To(Succeed())
				Expect(*postgresCluster.Spec.Shutdown).To(BeTrue())
			})
		})

		It("should reconcile backup", func() {
			_, err := backupReconciler().Reconcile(ctx, ctrl.Request{NamespacedName: backupNamespacedName})
			Expect(err).To(Succeed())
		})

		It("should have new backup state", func() {
			Expect(k8sClient.Get(ctx, backupNamespacedName, pgBackup)).To(Succeed())
			Expect(pgBackup.Status.State).To(Equal(v2.BackupNew))
		})
	})

	When("backup is running", func() {
		Context("unpause cluster", func() {
			It("should unpause cluster", func() {
				Expect(k8sClient.Get(ctx, crNamespacedName, cr)).To(Succeed())
				t := false
				cr.Spec.Pause = &t
				Expect(k8sClient.Update(ctx, cr)).To(Succeed())
			})
			It("should reconcile", func() {
				_, err := reconciler(cr).Reconcile(ctx, ctrl.Request{NamespacedName: crNamespacedName})
				Expect(err).NotTo(HaveOccurred())
				_, err = crunchyReconciler().Reconcile(ctx, ctrl.Request{NamespacedName: crNamespacedName})
				Expect(err).NotTo(HaveOccurred())
			})
			It("should be running crunchy cluster", func() {
				postgresCluster := &v1beta1.PostgresCluster{}
				Expect(k8sClient.Get(ctx, crNamespacedName, postgresCluster)).To(Succeed())
				Expect(*postgresCluster.Spec.Shutdown).To(BeFalse())
			})
		})

		It("should reconcile backup", func() {
			_, err := backupReconciler().Reconcile(ctx, ctrl.Request{NamespacedName: backupNamespacedName})
			Expect(err).To(Succeed())
		})
		It("should have starting backup state", func() {
			Expect(k8sClient.Get(ctx, backupNamespacedName, pgBackup)).To(Succeed())
			Expect(pgBackup.Status.State).To(Equal(v2.BackupStarting))
		})

		Context("try to pause cluster", func() {
			It("should pause cluster", func() {
				Expect(k8sClient.Get(ctx, crNamespacedName, cr)).To(Succeed())
				t := true
				cr.Spec.Pause = &t
				Expect(k8sClient.Update(ctx, cr)).To(Succeed())
			})
			It("should reconcile", func() {
				_, err := reconciler(cr).Reconcile(ctx, ctrl.Request{NamespacedName: crNamespacedName})
				Expect(err).NotTo(HaveOccurred())
				_, err = crunchyReconciler().Reconcile(ctx, ctrl.Request{NamespacedName: crNamespacedName})
				Expect(err).NotTo(HaveOccurred())
			})
			It("should be running crunchy cluster", func() {
				postgresCluster := &v1beta1.PostgresCluster{}
				Expect(k8sClient.Get(ctx, crNamespacedName, postgresCluster)).To(Succeed())
				Expect(*postgresCluster.Spec.Shutdown).To(BeFalse())
			})
		})
	})
})

var _ = Describe("Security context", Ordered, func() {
	ctx := context.Background()

	const crName = "security-context"
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

	el := int64(11)
	podSecContext := &corev1.PodSecurityContext{
		RunAsUser:  &el,
		RunAsGroup: &el,
	}

	It("should create PerconaPGCluster", func() {
		for i := range cr.Spec.InstanceSets {
			i := &cr.Spec.InstanceSets[i]
			i.SecurityContext = podSecContext
		}
		cr.Spec.Proxy.PGBouncer.SecurityContext = podSecContext
		cr.Spec.Backups.PGBackRest.RepoHost = &v1beta1.PGBackRestRepoHost{
			SecurityContext: podSecContext,
		}
		status := cr.Status
		Expect(k8sClient.Create(ctx, cr)).Should(Succeed())
		cr.Status = status
		Expect(k8sClient.Status().Update(ctx, cr)).Should(Succeed())
	})

	It("should reconcile", func() {
		// Run multiple reconcile cycles to ensure all resources are created
		for i := 0; i < 3; i++ {
			_, err := reconciler(cr).Reconcile(ctx, ctrl.Request{NamespacedName: crNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			_, err = crunchyReconciler().Reconcile(ctx, ctrl.Request{NamespacedName: crNamespacedName})
			Expect(err).NotTo(HaveOccurred())
		}
	})

	It("Instances should have security context", func() {
		stsList := &appsv1.StatefulSetList{}
		labels := map[string]string{
			"postgres-operator.crunchydata.com/data":    "postgres",
			"postgres-operator.crunchydata.com/cluster": crName,
		}
		err = k8sClient.List(ctx, stsList, client.InNamespace(cr.Namespace), client.MatchingLabels(labels))
		Expect(err).NotTo(HaveOccurred())
		Expect(stsList.Items).NotTo(BeEmpty())

		for _, sts := range stsList.Items {
			Expect(sts.Spec.Template.Spec.SecurityContext).To(Equal(podSecContext))
		}
	})

	It("PgBouncer should have security context", func() {
		deployment := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      crName + "-pgbouncer",
				Namespace: cr.Namespace,
			},
		}
		err = k8sClient.Get(ctx, client.ObjectKeyFromObject(deployment), deployment)
		Expect(err).NotTo(HaveOccurred())
		Expect(deployment.Spec.Template.Spec.SecurityContext).To(Equal(podSecContext))
	})

	It("PgBackrest Repo should have security context", func() {
		// Wait for the StatefulSet to be created before checking it
		sts := &appsv1.StatefulSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      crName + "-repo-host",
				Namespace: cr.Namespace,
			},
		}

		Eventually(func() error {
			return k8sClient.Get(ctx, client.ObjectKeyFromObject(sts), sts)
		}, time.Second*30, time.Millisecond*500).Should(Succeed())

		Expect(sts.Spec.Template.Spec.SecurityContext).To(Equal(podSecContext))
	})
})

var _ = Describe("Operator-created sidecar container resources", Ordered, func() {
	ctx := context.Background()

	const crName = "sidecar-resources"
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

	cr, err := readTestCR(crName, ns, "sidecar-resources-cr.yaml")
	It("should read defautl cr.yaml", func() {
		Expect(err).NotTo(HaveOccurred())
	})

	It("should create PerconaPGCluster", func() {
		status := cr.Status
		Expect(k8sClient.Create(ctx, cr)).Should(Succeed())
		cr.Status = status
		Expect(k8sClient.Status().Update(ctx, cr)).Should(Succeed())
	})

	It("should reconcile", func() {
		_, err := reconciler(cr).Reconcile(ctx, ctrl.Request{NamespacedName: crNamespacedName})
		Expect(err).NotTo(HaveOccurred())
		_, err = crunchyReconciler().Reconcile(ctx, ctrl.Request{NamespacedName: crNamespacedName})
		Expect(err).NotTo(HaveOccurred())
	})

	It("should apply resources to pgbackrest, pgbackrest-config and replication-cert-copy sidecar containers in instance pods", func() {
		stsList := &appsv1.StatefulSetList{}
		labels := map[string]string{
			"postgres-operator.crunchydata.com/data":    "postgres",
			"postgres-operator.crunchydata.com/cluster": crName,
		}
		err = k8sClient.List(ctx, stsList, client.InNamespace(cr.Namespace), client.MatchingLabels(labels))
		Expect(err).NotTo(HaveOccurred())
		Expect(stsList.Items).NotTo(BeEmpty())

		for _, sts := range stsList.Items {
			for _, c := range sts.Spec.Template.Spec.Containers {
				if c.Name == "replication-cert-copy" {
					Expect(c.Resources.Limits.Cpu()).Should(Equal(cr.Spec.InstanceSets[0].Containers.ReplicaCertCopy.Resources.Limits.Cpu()))
					Expect(c.Resources.Limits.Memory()).Should(Equal(cr.Spec.InstanceSets[0].Containers.ReplicaCertCopy.Resources.Limits.Memory()))
				}
				if c.Name == "pgbackrest" {
					Expect(c.Resources.Limits.Cpu()).Should(Equal(cr.Spec.Backups.PGBackRest.Containers.PGBackRest.Resources.Limits.Cpu()))
					Expect(c.Resources.Limits.Memory()).Should(Equal(cr.Spec.Backups.PGBackRest.Containers.PGBackRest.Resources.Limits.Memory()))
				}
				if c.Name == "pgbackrest-config" {
					Expect(c.Resources.Limits.Cpu()).Should(Equal(cr.Spec.Backups.PGBackRest.Containers.PGBackRestConfig.Resources.Limits.Cpu()))
					Expect(c.Resources.Limits.Memory()).Should(Equal(cr.Spec.Backups.PGBackRest.Containers.PGBackRestConfig.Resources.Limits.Memory()))
				}
			}
		}
	})

	It("should apply resources to pgbouncer-config sidecar container in pgbouncer pods", func() {
		deployment := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      crName + "-pgbouncer",
				Namespace: cr.Namespace,
			},
		}
		err = k8sClient.Get(ctx, client.ObjectKeyFromObject(deployment), deployment)
		Expect(err).NotTo(HaveOccurred())

		for _, c := range deployment.Spec.Template.Spec.Containers {
			if c.Name == "pgbouncer-config" {
				Expect(c.Resources.Limits.Cpu()).Should(Equal(cr.Spec.Proxy.PGBouncer.Containers.PGBouncerConfig.Resources.Limits.Cpu()))
				Expect(c.Resources.Limits.Memory()).Should(Equal(cr.Spec.Proxy.PGBouncer.Containers.PGBouncerConfig.Resources.Limits.Memory()))
			}
		}
	})
})

var _ = Describe("Validate TLS", Ordered, func() {
	ctx := context.Background()

	const crName = "validate-tls"
	const ns = crName

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
	It("should read default cr.yaml", func() {
		Expect(err).NotTo(HaveOccurred())
	})

	cr.Default()

	It("should create PerconaPGCluster", func() {
		status := cr.Status
		Expect(k8sClient.Create(ctx, cr)).Should(Succeed())
		cr.Status = status
		Expect(k8sClient.Status().Update(ctx, cr)).Should(Succeed())
	})

	checkSecretProjection := func(cr *v2.PerconaPGCluster, projection *corev1.SecretProjection, secretName string, neededKeys []string) {
		GinkgoHelper()
		It("should fail if secret doesn't exist", func() {
			projection.Name = secretName

			err := reconciler(cr).validateTLS(ctx, cr)
			Expect(err).To(HaveOccurred())
		})
		It("should fail if secret doesn't have needed data", func() {
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      secretName,
					Namespace: cr.Namespace,
				},
			}
			Expect(k8sClient.Create(ctx, secret)).NotTo(HaveOccurred())

			err := reconciler(cr).validateTLS(ctx, cr)
			Expect(err).To(HaveOccurred())
		})

		It("should not fail if needed keys specified in the secret", func() {
			secret := new(corev1.Secret)
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      secretName,
				Namespace: cr.Namespace,
			}, secret)).NotTo(HaveOccurred())
			secret.Data = make(map[string][]byte)
			for _, v := range neededKeys {
				secret.Data[v] = []byte("some-data")
			}
			Expect(k8sClient.Update(ctx, secret)).NotTo(HaveOccurred())

			err := reconciler(cr).validateTLS(ctx, cr)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should not fail if wrong items.path are specified but needed key exist in secrets", func() {
			projection.Items = []corev1.KeyToPath{}
			for i, v := range neededKeys {
				projection.Items = append(projection.Items, corev1.KeyToPath{
					Key:  v,
					Path: "wrong-path" + "-" + strconv.Itoa(i),
				})
			}

			err := reconciler(cr).validateTLS(ctx, cr)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should fail if items.key are specified which don't exist in the secret", func() {
			projection.Items = []corev1.KeyToPath{}
			for i, v := range neededKeys {
				projection.Items = append(projection.Items, corev1.KeyToPath{
					Key:  "non-existent-key" + strconv.Itoa(i),
					Path: v,
				})
			}
			err := reconciler(cr).validateTLS(ctx, cr)
			Expect(err).To(HaveOccurred())
		})

		It("should not fail if wrong items.path are specified but needed key exist in secrets", func() {
			projection.Items = []corev1.KeyToPath{}
			for _, v := range neededKeys {
				projection.Items = append(projection.Items, corev1.KeyToPath{
					Key:  v,
					Path: v + "-wrong",
				})
			}

			err := reconciler(cr).validateTLS(ctx, cr)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should not fail with custom data keys in the secret", func() {
			secret := new(corev1.Secret)
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      secretName,
				Namespace: cr.Namespace,
			}, secret)).NotTo(HaveOccurred())

			secret.Data = map[string][]byte{}
			for _, v := range neededKeys {
				secret.Data[v+"-custom"] = []byte("some-data")
			}
			Expect(k8sClient.Update(ctx, secret)).NotTo(HaveOccurred())
			projection.Items = []corev1.KeyToPath{}
			for _, v := range neededKeys {
				projection.Items = append(projection.Items, corev1.KeyToPath{
					Key:  v + "-custom",
					Path: v,
				})
			}

			err := reconciler(cr).validateTLS(ctx, cr)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should fail if items.key are specified but not paths", func() {
			projection.Items = []corev1.KeyToPath{}
			for _, v := range neededKeys {
				projection.Items = append(projection.Items, corev1.KeyToPath{
					Key: v,
				})
			}

			err := reconciler(cr).validateTLS(ctx, cr)
			Expect(err).To(HaveOccurred())
		})
	}

	Context("checking validation", func() {
		Describe("should check validation for cr.Spec.Secrets.CustomRootCATLSSecret", func() {
			cr := cr.DeepCopy()
			neededKeys := []string{
				"root.crt",
				"root.key",
			}
			cr.Spec.Secrets.CustomRootCATLSSecret = new(corev1.SecretProjection)
			checkSecretProjection(cr, cr.Spec.Secrets.CustomRootCATLSSecret, "root-ca", neededKeys)
			It("should not fail if the section was not specified", func() {
				cr.Spec.Secrets.CustomRootCATLSSecret = nil
				err := reconciler(cr).validateTLS(ctx, cr)
				Expect(err).NotTo(HaveOccurred())
			})
		})
		Describe("should check validation for cr.Spec.Secrets.CustomTLSSecret", func() {
			cr := cr.DeepCopy()
			neededKeys := []string{
				"ca.crt",
				"tls.crt",
				"tls.key",
			}
			cr.Spec.Secrets.CustomTLSSecret = new(corev1.SecretProjection)
			checkSecretProjection(cr, cr.Spec.Secrets.CustomTLSSecret, "tls-secret", neededKeys)
			It("should not fail if the section was not specified", func() {
				cr.Spec.Secrets.CustomTLSSecret = nil
				err := reconciler(cr).validateTLS(ctx, cr)
				Expect(err).NotTo(HaveOccurred())
			})
		})
		Describe("should check validation for cr.Spec.Secrets.CustomReplicationClientTLSSecret", func() {
			cr := cr.DeepCopy()
			neededKeys := []string{
				"ca.crt",
				"tls.crt",
				"tls.key",
			}
			cr.Spec.Secrets.CustomReplicationClientTLSSecret = new(corev1.SecretProjection)
			checkSecretProjection(cr, cr.Spec.Secrets.CustomReplicationClientTLSSecret, "repl-tls-secret", neededKeys)
			It("should not fail if the section was not specified", func() {
				cr.Spec.Secrets.CustomReplicationClientTLSSecret = nil
				err := reconciler(cr).validateTLS(ctx, cr)
				Expect(err).NotTo(HaveOccurred())
			})
		})
	})

	checkSecretProjectionWithCA := func(cr *v2.PerconaPGCluster, projection *corev1.SecretProjection, secretName string) {
		GinkgoHelper()
		neededKeys := []string{
			"tls.crt",
			"tls.key",
		}
		projection.Name = secretName
		It("should create secret", func() {
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      secretName,
					Namespace: cr.Namespace,
				},
			}
			secret.Data = map[string][]byte{}
			for _, v := range neededKeys {
				secret.Data[v] = []byte("some-data")
			}
			Expect(k8sClient.Create(ctx, secret)).NotTo(HaveOccurred())
		})
		It("should fail when CA is not specified", func() {
			err := reconciler(cr).validateTLS(ctx, cr)
			Expect(err).To(HaveOccurred())
		})
		It("should not fail when CA is specified", func() {
			secretName := secretName + "-ca"
			neededKeys := []string{
				"root.crt",
				"root.key",
			}
			cr.Spec.Secrets.CustomRootCATLSSecret = new(corev1.SecretProjection)
			projection := cr.Spec.Secrets.CustomRootCATLSSecret
			projection.Name = secretName
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      secretName,
					Namespace: cr.Namespace,
				},
			}
			secret.Data = make(map[string][]byte)
			for _, v := range neededKeys {
				secret.Data[v] = []byte("some-data")
			}
			Expect(k8sClient.Create(ctx, secret)).NotTo(HaveOccurred())

			err := reconciler(cr).validateTLS(ctx, cr)
			Expect(err).NotTo(HaveOccurred())
		})
	}
	Context("check validation for cr.Spec.Secrets.CustomTLSSecret when cr.Spec.Secrets.CustomRootCATLSSecret is specified", func() {
		cr := cr.DeepCopy()
		secretName := "custom-tls-secret-with-ca" //nolint:gosec
		cr.Spec.Secrets.CustomTLSSecret = new(corev1.SecretProjection)
		checkSecretProjectionWithCA(cr, cr.Spec.Secrets.CustomTLSSecret, secretName)
	})
	Context("should check validation for cr.Spec.Secrets.CustomReplicationClientTLSSecret when cr.Spec.Secrets.CustomRootCATLSSecret is specified", func() {
		cr := cr.DeepCopy()
		secretName := "custom-replication-tls-secret-with-ca" //nolint:gosec
		cr.Spec.Secrets.CustomReplicationClientTLSSecret = new(corev1.SecretProjection)
		checkSecretProjectionWithCA(cr, cr.Spec.Secrets.CustomReplicationClientTLSSecret, secretName)
	})
})
