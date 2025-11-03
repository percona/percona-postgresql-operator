package pgcluster

import (
	"context"
	"crypto/md5" //nolint:gosec
	"fmt"
	"strconv"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	gs "github.com/onsi/gomega/gstruct"
	"github.com/pkg/errors"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/percona/percona-postgresql-operator/v2/internal/feature"
	"github.com/percona/percona-postgresql-operator/v2/internal/naming"
	pNaming "github.com/percona/percona-postgresql-operator/v2/percona/naming"
	"github.com/percona/percona-postgresql-operator/v2/percona/version"
	v2 "github.com/percona/percona-postgresql-operator/v2/pkg/apis/pgv2.percona.com/v2"
	"github.com/percona/percona-postgresql-operator/v2/pkg/apis/postgres-operator.crunchydata.com/v1beta1"
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

		When("pmm secret has data for PMM_SERVER_KEY", func() {
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

		When("pmm secret has data for PMM_SERVER_TOKEN", func() {
			BeforeAll(func() {
				Expect(k8sClient.Update(ctx, &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "cluster1-pmm-secret",
						Namespace: ns,
					},
					Data: map[string][]byte{
						"PMM_SERVER_TOKEN": []byte("token"),
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

		It("should create default and monitor user", func() {
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
	It("should read default cr.yaml", func() {
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
		err = k8sClient.List(ctx, stsList, client.InNamespace(cr.Namespace), client.MatchingLabels(labels))
		Expect(err).NotTo(HaveOccurred())
		Expect(stsList.Items).NotTo(BeEmpty())

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
		_, err := reconciler(cr).Reconcile(ctx, ctrl.Request{NamespacedName: crNamespacedName})
		Expect(err).NotTo(HaveOccurred())
		_, err = crunchyReconciler().Reconcile(ctx, ctrl.Request{NamespacedName: crNamespacedName})
		Expect(err).NotTo(HaveOccurred())
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
		sts := &appsv1.StatefulSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      crName + "-repo-host",
				Namespace: cr.Namespace,
			},
		}
		err = k8sClient.Get(ctx, client.ObjectKeyFromObject(sts), sts)
		Expect(err).NotTo(HaveOccurred())
		Expect(sts.Spec.Template.Spec.SecurityContext).To(Equal(podSecContext))
	})
})

var _ = Describe("Envs", Ordered, func() {
	ctx := context.Background()

	const crName = "envs"
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

	instanceEnv := []corev1.EnvVar{
		{
			Name:  "INSTANCE_ENV",
			Value: "VALUE1",
		},
	}
	instanceEnvFrom := []corev1.EnvFromSource{
		{
			SecretRef: &corev1.SecretEnvSource{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: "secret-instance-env",
				},
			},
		},
	}
	instanceSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "secret-instance-env",
			Namespace: ns,
		},
		StringData: map[string]string{
			"instance": "test",
		},
	}

	pgbouncerEnv := []corev1.EnvVar{
		{
			Name:  "PGBOUNCER_ENV",
			Value: "VALUE2",
		},
	}
	pgbouncerEnvFrom := []corev1.EnvFromSource{
		{
			SecretRef: &corev1.SecretEnvSource{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: "secret-pgbouncer-env",
				},
			},
		},
	}
	pgbouncerSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "secret-pgbouncer-env",
			Namespace: ns,
		},
		StringData: map[string]string{
			"pgbouncer": "test",
		},
	}

	repoHostEnv := []corev1.EnvVar{
		{
			Name:  "REPOHOST_ENV",
			Value: "VALUE3",
		},
	}
	repoHostEnvFrom := []corev1.EnvFromSource{
		{
			SecretRef: &corev1.SecretEnvSource{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: "secret-pgbackrest-env",
				},
			},
		},
	}
	repoHostSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "secret-pgbackrest-env",
			Namespace: ns,
		},
		StringData: map[string]string{
			"pgbackrest": "test",
		},
	}

	It("should create PerconaPGCluster", func() {
		for i := range cr.Spec.InstanceSets {
			cr.Spec.InstanceSets[i].Env = []corev1.EnvVar{
				{
					Name:  "INSTANCE_ENV",
					Value: "VALUE1",
				},
			}

			cr.Spec.InstanceSets[i].EnvFrom = []corev1.EnvFromSource{
				{
					SecretRef: &corev1.SecretEnvSource{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: "secret-instance-env",
						},
					},
				},
			}
		}
		cr.Spec.Proxy.PGBouncer.Env = []corev1.EnvVar{
			{
				Name:  "PGBOUNCER_ENV",
				Value: "VALUE2",
			},
		}
		cr.Spec.Proxy.PGBouncer.EnvFrom = []corev1.EnvFromSource{
			{
				SecretRef: &corev1.SecretEnvSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: "secret-pgbouncer-env",
					},
				},
			},
		}
		cr.Spec.Backups.PGBackRest.Env = []corev1.EnvVar{
			{
				Name:  "REPOHOST_ENV",
				Value: "VALUE3",
			},
		}
		cr.Spec.Backups.PGBackRest.EnvFrom = []corev1.EnvFromSource{
			{
				SecretRef: &corev1.SecretEnvSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: "secret-pgbackrest-env",
					},
				},
			},
		}
		cr.Spec.CRVersion = "2.8.0"

		status := cr.Status
		Expect(k8sClient.Create(ctx, cr)).Should(Succeed())
		cr.Status = status
		Expect(k8sClient.Status().Update(ctx, cr)).Should(Succeed())

		Expect(k8sClient.Create(ctx, instanceSecret)).Should(Succeed())
		Expect(k8sClient.Create(ctx, pgbouncerSecret)).Should(Succeed())
		Expect(k8sClient.Create(ctx, repoHostSecret)).Should(Succeed())
	})

	It("should reconcile", func() {
		_, err := reconciler(cr).Reconcile(ctx, ctrl.Request{NamespacedName: crNamespacedName})
		Expect(err).NotTo(HaveOccurred())
		_, err = crunchyReconciler().Reconcile(ctx, ctrl.Request{NamespacedName: crNamespacedName})
		Expect(err).NotTo(HaveOccurred())
	})

	It("Instances should have envs", func() {
		stsList := &appsv1.StatefulSetList{}
		labels := map[string]string{
			"postgres-operator.crunchydata.com/data":    "postgres",
			"postgres-operator.crunchydata.com/cluster": crName,
		}
		err = k8sClient.List(ctx, stsList, client.InNamespace(cr.Namespace), client.MatchingLabels(labels))
		Expect(err).NotTo(HaveOccurred())
		Expect(stsList.Items).NotTo(BeEmpty())

		for _, sts := range stsList.Items {
			Expect(sts.Spec.Template.Annotations[pNaming.AnnotationEnvVarsSecretHash]).To(Equal("fadefc4ed7b5e5948dc8b03f2a3a71be"))
			for _, c := range sts.Spec.Template.Spec.Containers {
				Expect(c.Env).To(ContainElement(instanceEnv[0]))
				Expect(c.EnvFrom).To(Equal(instanceEnvFrom))
			}
		}
	})

	It("PgBouncer should have envs", func() {
		deployment := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      crName + "-pgbouncer",
				Namespace: cr.Namespace,
			},
		}
		err = k8sClient.Get(ctx, client.ObjectKeyFromObject(deployment), deployment)
		Expect(err).NotTo(HaveOccurred())
		Expect(deployment.Spec.Template.Annotations[pNaming.AnnotationEnvVarsSecretHash]).To(Equal("6bc7e8df0909c789be90c630c9edce14"))
		for _, c := range deployment.Spec.Template.Spec.Containers {
			Expect(c.Env).To(ContainElement(pgbouncerEnv[0]))
			Expect(c.EnvFrom).To(Equal(pgbouncerEnvFrom))
		}
	})

	It("PgBackrest Repo should have envs", func() {
		sts := &appsv1.StatefulSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      crName + "-repo-host",
				Namespace: cr.Namespace,
			},
		}
		err = k8sClient.Get(ctx, client.ObjectKeyFromObject(sts), sts)
		Expect(err).NotTo(HaveOccurred())
		Expect(sts.Spec.Template.Annotations[pNaming.AnnotationEnvVarsSecretHash]).To(Equal("22eb2683af3f48813c0cd53f905b67d0"))
		for _, c := range sts.Spec.Template.Spec.Containers {
			Expect(c.Env).To(ContainElement(repoHostEnv[0]))
			Expect(c.EnvFrom).To(Equal(repoHostEnvFrom))
		}
	})
})

var _ = Describe("Sidecars", Ordered, func() {
	gate := feature.NewGate()
	err := gate.SetFromMap(map[string]bool{
		feature.InstanceSidecars:           true,
		feature.PGBouncerSidecars:          true,
		feature.PGBackrestRepoHostSidecars: true,
	})
	Expect(err).NotTo(HaveOccurred())

	ctx := feature.NewContext(context.Background(), gate)

	const crName = "sidecars"
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
		for i := range cr.Spec.InstanceSets {
			i := &cr.Spec.InstanceSets[i]
			i.Sidecars = []corev1.Container{
				{
					Name:    "instance-sidecar",
					Command: []string{"instance-cmd"},
					Image:   "instance-image",
				},
			}
		}
		cr.Spec.Proxy.PGBouncer.Sidecars = []corev1.Container{
			{
				Name:    "pgbouncer-sidecar",
				Command: []string{"pgbouncer-cmd"},
				Image:   "pgbouncer-image",
			},
		}
		cr.Spec.Backups.PGBackRest.RepoHost.Sidecars = []corev1.Container{
			{
				Name:    "repohost-sidecar",
				Command: []string{"repohost-cmd"},
				Image:   "repohost-image",
			},
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

	getContainer := func(containers []corev1.Container, name string) *corev1.Container {
		for _, c := range containers {
			if c.Name == name {
				return &c
			}
		}
		return nil
	}

	It("Instances should have sidecar", func() {
		stsList := &appsv1.StatefulSetList{}
		labels := map[string]string{
			"postgres-operator.crunchydata.com/data":    "postgres",
			"postgres-operator.crunchydata.com/cluster": crName,
		}
		err = k8sClient.List(ctx, stsList, client.InNamespace(cr.Namespace), client.MatchingLabels(labels))
		Expect(err).NotTo(HaveOccurred())
		Expect(stsList.Items).NotTo(BeEmpty())

		for _, sts := range stsList.Items {
			sidecar := getContainer(sts.Spec.Template.Spec.Containers, "instance-sidecar")
			Expect(sidecar).NotTo(BeNil())
			Expect(sidecar.Command).To(Equal([]string{"instance-cmd"}))
			Expect(sidecar.Image).To(Equal("instance-image"))
		}
	})

	It("PgBouncer should have sidecar", func() {
		deployment := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      crName + "-pgbouncer",
				Namespace: cr.Namespace,
			},
		}
		err = k8sClient.Get(ctx, client.ObjectKeyFromObject(deployment), deployment)
		Expect(err).NotTo(HaveOccurred())
		sidecar := getContainer(deployment.Spec.Template.Spec.Containers, "pgbouncer-sidecar")
		Expect(sidecar).NotTo(BeNil())
		Expect(sidecar.Command).To(Equal([]string{"pgbouncer-cmd"}))
		Expect(sidecar.Image).To(Equal("pgbouncer-image"))
	})

	It("PgBackrest Repo should have sidecar", func() {
		sts := &appsv1.StatefulSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      crName + "-repo-host",
				Namespace: cr.Namespace,
			},
		}
		err = k8sClient.Get(ctx, client.ObjectKeyFromObject(sts), sts)
		Expect(err).NotTo(HaveOccurred())
		sidecar := getContainer(sts.Spec.Template.Spec.Containers, "repohost-sidecar")
		Expect(sidecar).NotTo(BeNil())
		Expect(sidecar.Command).To(Equal([]string{"repohost-cmd"}))
		Expect(sidecar.Image).To(Equal("repohost-image"))
	})

	It("should update PerconaPGCluster with multiple sidecars", func() {
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(cr), cr)).Should(Succeed())

		for i := range cr.Spec.InstanceSets {
			i := &cr.Spec.InstanceSets[i]
			i.Sidecars = []corev1.Container{
				{
					Name:    "instance-sidecar-2",
					Command: []string{"instance-cmd-2"},
					Image:   "instance-image-2",
				},
				{
					Name:    "instance-sidecar",
					Command: []string{"instance-cmd"},
					Image:   "instance-image",
				},
			}
		}
		cr.Spec.Proxy.PGBouncer.Sidecars = []corev1.Container{
			{
				Name:    "pgbouncer-sidecar",
				Command: []string{"pgbouncer-cmd"},
				Image:   "pgbouncer-image",
			},
			{
				Name:    "pgbouncer-sidecar-2",
				Command: []string{"pgbouncer-cmd-2"},
				Image:   "pgbouncer-image-2",
			},
			{
				Name:    "pgbouncer-sidecar-3",
				Command: []string{"pgbouncer-cmd-3"},
				Image:   "pgbouncer-image-3",
			},
		}
		cr.Spec.Backups.PGBackRest.RepoHost.Sidecars = []corev1.Container{
			{
				Name:    "repohost-sidecar-2",
				Command: []string{"repohost-cmd-2"},
				Image:   "repohost-image-2",
			},
			{
				Name:    "repohost-sidecar",
				Command: []string{"repohost-cmd"},
				Image:   "repohost-image",
			},
			{
				Name:    "repohost-sidecar-3",
				Command: []string{"repohost-cmd-3"},
				Image:   "repohost-image-3",
			},
		}
		Expect(k8sClient.Update(ctx, cr)).Should(Succeed())
	})

	It("should reconcile", func() {
		_, err := reconciler(cr).Reconcile(ctx, ctrl.Request{NamespacedName: crNamespacedName})
		Expect(err).NotTo(HaveOccurred())
		_, err = crunchyReconciler().Reconcile(ctx, ctrl.Request{NamespacedName: crNamespacedName})
		Expect(err).NotTo(HaveOccurred())
	})

	It("Instances should have multiple sidecars", func() {
		stsList := &appsv1.StatefulSetList{}
		labels := map[string]string{
			"postgres-operator.crunchydata.com/data":    "postgres",
			"postgres-operator.crunchydata.com/cluster": crName,
		}
		err = k8sClient.List(ctx, stsList, client.InNamespace(cr.Namespace), client.MatchingLabels(labels))
		Expect(err).NotTo(HaveOccurred())
		Expect(stsList.Items).NotTo(BeEmpty())

		for _, sts := range stsList.Items {
			l := len(sts.Spec.Template.Spec.Containers)
			sidecar := sts.Spec.Template.Spec.Containers[l-4]
			Expect(sidecar).NotTo(BeNil())
			Expect(sidecar.Name).To(Equal("instance-sidecar-2"))
			Expect(sidecar.Command).To(Equal([]string{"instance-cmd-2"}))
			Expect(sidecar.Image).To(Equal("instance-image-2"))

			sidecar = sts.Spec.Template.Spec.Containers[l-3]
			Expect(sidecar).NotTo(BeNil())
			Expect(sidecar.Name).To(Equal("instance-sidecar"))
			Expect(sidecar.Command).To(Equal([]string{"instance-cmd"}))
			Expect(sidecar.Image).To(Equal("instance-image"))
		}
	})

	It("PgBouncer should have multiple sidecars", func() {
		deployment := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      crName + "-pgbouncer",
				Namespace: cr.Namespace,
			},
		}
		err = k8sClient.Get(ctx, client.ObjectKeyFromObject(deployment), deployment)
		Expect(err).NotTo(HaveOccurred())

		l := len(deployment.Spec.Template.Spec.Containers)
		sidecar := deployment.Spec.Template.Spec.Containers[l-3]
		Expect(sidecar).NotTo(BeNil())
		Expect(sidecar.Name).To(Equal("pgbouncer-sidecar"))
		Expect(sidecar.Command).To(Equal([]string{"pgbouncer-cmd"}))
		Expect(sidecar.Image).To(Equal("pgbouncer-image"))

		sidecar = deployment.Spec.Template.Spec.Containers[l-2]
		Expect(sidecar).NotTo(BeNil())
		Expect(sidecar.Name).To(Equal("pgbouncer-sidecar-2"))
		Expect(sidecar.Command).To(Equal([]string{"pgbouncer-cmd-2"}))
		Expect(sidecar.Image).To(Equal("pgbouncer-image-2"))

		sidecar = deployment.Spec.Template.Spec.Containers[l-1]
		Expect(sidecar).NotTo(BeNil())
		Expect(sidecar.Name).To(Equal("pgbouncer-sidecar-3"))
		Expect(sidecar.Command).To(Equal([]string{"pgbouncer-cmd-3"}))
		Expect(sidecar.Image).To(Equal("pgbouncer-image-3"))
	})

	It("PgBackrest Repo should have multiple sidecars", func() {
		sts := &appsv1.StatefulSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      crName + "-repo-host",
				Namespace: cr.Namespace,
			},
		}
		err = k8sClient.Get(ctx, client.ObjectKeyFromObject(sts), sts)
		Expect(err).NotTo(HaveOccurred())

		l := len(sts.Spec.Template.Spec.Containers)
		sidecar := sts.Spec.Template.Spec.Containers[l-3]
		Expect(sidecar).NotTo(BeNil())
		Expect(sidecar.Name).To(Equal("repohost-sidecar-2"))
		Expect(sidecar.Command).To(Equal([]string{"repohost-cmd-2"}))
		Expect(sidecar.Image).To(Equal("repohost-image-2"))

		sidecar = sts.Spec.Template.Spec.Containers[l-2]
		Expect(sidecar).NotTo(BeNil())
		Expect(sidecar.Name).To(Equal("repohost-sidecar"))
		Expect(sidecar.Command).To(Equal([]string{"repohost-cmd"}))
		Expect(sidecar.Image).To(Equal("repohost-image"))

		sidecar = sts.Spec.Template.Spec.Containers[l-1]
		Expect(sidecar).NotTo(BeNil())
		Expect(sidecar.Name).To(Equal("repohost-sidecar-3"))
		Expect(sidecar.Command).To(Equal([]string{"repohost-cmd-3"}))
		Expect(sidecar.Image).To(Equal("repohost-image-3"))
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

type saTestClient struct {
	client.Client

	crName string
	ns     string
}

func (sc *saTestClient) checkObject(ctx context.Context, obj client.Object) error {
	sts, ok := obj.(*appsv1.StatefulSet)
	if !ok {
		return nil
	}
	serviceAccountName := sts.Spec.Template.Spec.ServiceAccountName
	if serviceAccountName == "" {
		return errors.New("it's not expected to have empty service account name")
	}

	if err := sc.Get(ctx, types.NamespacedName{
		Name:      serviceAccountName,
		Namespace: sts.Namespace,
	}, new(corev1.ServiceAccount)); err != nil {
		if k8serrors.IsNotFound(err) {
			return errors.Wrap(err, "test error: service account should be created before the statefulset")
		}
		return err
	}

	return nil
}

func (sc *saTestClient) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
	if err := sc.checkObject(ctx, obj); err != nil {
		panic(err) // should panic because reconciler can ignore the error
	}
	return sc.Client.Patch(ctx, obj, patch, opts...)
}

func (sc *saTestClient) Create(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
	if err := sc.checkObject(ctx, obj); err != nil {
		panic(err) // should panic because reconciler can ignore the error
	}
	return sc.Client.Create(ctx, obj, opts...)
}

// This test ensures that the ServiceAccount associated with a StatefulSet is created
// before the StatefulSet itself. (K8SPG-698)
// The saTestClient verifies the existence of the ServiceAccount during create, patch,
// or update operations on the StatefulSet.
var _ = Describe("ServiceAccount early creation", Ordered, func() {
	ctx := context.Background()

	const crName = "sa-timestamp"
	const ns = crName

	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:      crName,
			Namespace: ns,
		},
	}
	crNamespacedName := types.NamespacedName{Name: crName, Namespace: ns}

	cr, err := readDefaultCR(crName, ns)
	It("should read default cr.yaml", func() {
		Expect(err).NotTo(HaveOccurred())
	})

	cr.Default()
	reconciler := reconciler(cr)
	crunchyReconciler := crunchyReconciler()

	var cl client.Client

	BeforeAll(func() {
		cl = &saTestClient{
			Client: k8sClient,

			crName: crName,
			ns:     ns,
		}
		reconciler.Client = cl
		crunchyReconciler.Client = cl

		By("Creating the Namespace to perform the tests")
		err := cl.Create(ctx, namespace)
		Expect(err).To(Not(HaveOccurred()))
	})

	AfterAll(func() {
		By("Deleting the Namespace to perform the tests")
		_ = cl.Delete(ctx, namespace)
	})

	It("should create PerconaPGCluster", func() {
		status := cr.Status
		Expect(cl.Create(ctx, cr)).Should(Succeed())
		cr.Status = status
		Expect(cl.Status().Update(ctx, cr)).Should(Succeed())
	})

	It("Should reconcile", func() {
		_, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: crNamespacedName})
		Expect(err).NotTo(HaveOccurred())
		_, err = crunchyReconciler.Reconcile(ctx, ctrl.Request{NamespacedName: crNamespacedName})
		Expect(err).NotTo(HaveOccurred())
	})
})

var _ = Describe("CR Validations", Ordered, func() {
	ctx := context.Background()
	const crName = "cr-validation"
	const ns = crName
	t := true
	f := false
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
		By("Deleting the Namespace to clean up after the tests")
		_ = k8sClient.Delete(ctx, namespace)
	})

	Context("PostgresVersion and grantPublicSchemaAccess validations", Ordered, func() {
		When("creating a CR with valid configurations", func() {
			It("should accept version >=15 with public schema access", func() {
				cr, err := readDefaultCR("cr-validation-1", ns)
				Expect(err).NotTo(HaveOccurred())

				cr.Spec.PostgresVersion = 15
				cr.Spec.Users = []v1beta1.PostgresUserSpec{{
					Name:                    "test",
					GrantPublicSchemaAccess: &t,
				}}

				Expect(k8sClient.Create(ctx, cr)).Should(Succeed())
			})

			It("should accept version <15 without public schema access", func() {
				cr, err := readDefaultCR("cr-validation-2", ns)
				Expect(err).NotTo(HaveOccurred())

				cr.Spec.PostgresVersion = 14
				cr.Spec.Users = []v1beta1.PostgresUserSpec{{
					Name:                    "test",
					GrantPublicSchemaAccess: &f,
				}}

				Expect(k8sClient.Create(ctx, cr)).Should(Succeed())
			})

			It("should accept version <15 with omitted public schema access", func() {
				cr, err := readDefaultCR("cr-validation-3", ns)
				Expect(err).NotTo(HaveOccurred())

				cr.Spec.PostgresVersion = 14
				cr.Spec.Users = []v1beta1.PostgresUserSpec{{Name: "test"}}

				Expect(k8sClient.Create(ctx, cr)).Should(Succeed())
			})

			It("should accept when no users are specified", func() {
				cr, err := readDefaultCR("cr-validation-4", ns)
				Expect(err).NotTo(HaveOccurred())

				cr.Spec.PostgresVersion = 14
				cr.Spec.Users = nil // No users provided

				Expect(k8sClient.Create(ctx, cr)).Should(Succeed())
			})
		})

		When("creating a CR with invalid configurations", func() {
			It("should reject version <15 with public schema access", func() {
				cr, err := readDefaultCR("cr-validation-5", ns)
				Expect(err).NotTo(HaveOccurred())

				cr.Spec.PostgresVersion = 14
				cr.Spec.Users = []v1beta1.PostgresUserSpec{{
					Name:                    "test",
					GrantPublicSchemaAccess: &t,
				}}

				err = k8sClient.Create(ctx, cr)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring(
					"PostgresVersion must be >= 15 if grantPublicSchemaAccess exists and is true",
				))
			})

			It("should reject mixed access in multiple users", func() {
				cr, err := readDefaultCR("cr-validation-6", ns)
				Expect(err).NotTo(HaveOccurred())

				cr.Spec.PostgresVersion = 14
				cr.Spec.Users = []v1beta1.PostgresUserSpec{
					{Name: "test1", GrantPublicSchemaAccess: &f},
					{Name: "test2", GrantPublicSchemaAccess: &t},
				}

				err = k8sClient.Create(ctx, cr)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring(
					"PostgresVersion must be >= 15 if grantPublicSchemaAccess exists and is true",
				))
			})
		})
	})

	Context("Backup repository validations", Ordered, func() {
		When("creating a CR with valid backup configurations", func() {
			It("should accept backups disabled with no repositories", func() {
				cr, err := readDefaultCR("cr-validation-backup-1", ns)
				Expect(err).NotTo(HaveOccurred())

				cr.Spec.Backups.Enabled = &f
				cr.Spec.Backups.PGBackRest.Repos = []v1beta1.PGBackRestRepo{}

				Expect(k8sClient.Create(ctx, cr)).Should(Succeed())
			})

			It("should accept backups enabled with at least one repository", func() {
				cr, err := readDefaultCR("cr-validation-backup-2", ns)
				Expect(err).NotTo(HaveOccurred())

				cr.Spec.Backups.Enabled = &t
				cr.Spec.Backups.PGBackRest.Repos = []v1beta1.PGBackRestRepo{
					{Name: "repo1"},
				}

				Expect(k8sClient.Create(ctx, cr)).Should(Succeed())
			})

			It("should accept backups disabled with at least one repository", func() {
				cr, err := readDefaultCR("cr-validation-backup-3", ns)
				Expect(err).NotTo(HaveOccurred())

				cr.Spec.Backups.Enabled = &f
				cr.Spec.Backups.PGBackRest.Repos = []v1beta1.PGBackRestRepo{
					{Name: "repo1"},
				}

				Expect(k8sClient.Create(ctx, cr)).Should(Succeed())
			})

			It("should accept backups enabled (nil - default true) with repositories", func() {
				cr, err := readDefaultCR("cr-validation-backup-4", ns)
				Expect(err).NotTo(HaveOccurred())

				cr.Spec.Backups.Enabled = nil // defaults to enabled
				cr.Spec.Backups.PGBackRest.Repos = []v1beta1.PGBackRestRepo{
					{Name: "repo1"},
				}

				Expect(k8sClient.Create(ctx, cr)).Should(Succeed())
			})

			It("should accept backups disabled with repositories not existing (nil)", func() {
				cr, err := readDefaultCR("cr-validation-backup-5", ns)
				Expect(err).NotTo(HaveOccurred())

				cr.Spec.Backups.Enabled = &f
				cr.Spec.Backups.PGBackRest.Repos = nil

				Expect(k8sClient.Create(ctx, cr)).Should(Succeed())
			})
		})

		When("creating a CR with invalid backup configurations", func() {
			It("should reject backups enabled with no repositories", func() {
				cr, err := readDefaultCR("cr-validation-backup-1", ns)
				Expect(err).NotTo(HaveOccurred())

				cr.Spec.Backups.Enabled = &t
				cr.Spec.Backups.PGBackRest.Repos = []v1beta1.PGBackRestRepo{}

				err = k8sClient.Create(ctx, cr)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring(
					"At least one repository must be configured when backups are enabled",
				))
			})

			It("should reject backups enabled with no repositories (nil)", func() {
				cr, err := readDefaultCR("cr-validation-backup-2", ns)
				Expect(err).NotTo(HaveOccurred())

				cr.Spec.Backups.Enabled = &t
				cr.Spec.Backups.PGBackRest.Repos = nil

				err = k8sClient.Create(ctx, cr)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring(
					"At least one repository must be configured when backups are enabled",
				))
			})

			It("should reject backups enabled (nil - default true) with no repositories", func() {
				cr, err := readDefaultCR("cr-validation-backup-3", ns)
				Expect(err).NotTo(HaveOccurred())

				cr.Spec.Backups.Enabled = nil // defaults to enabled
				cr.Spec.Backups.PGBackRest.Repos = []v1beta1.PGBackRestRepo{}

				err = k8sClient.Create(ctx, cr)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring(
					"At least one repository must be configured when backups are enabled",
				))
			})
		})
	})
})

var _ = Describe("Init Container", Ordered, func() {
	ctx := context.Background()

	const crName = "init-container-test"
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

	It("Controller should reconcile", func() {
		_, err := reconciler(cr).Reconcile(ctx, ctrl.Request{NamespacedName: crNamespacedName})
		Expect(err).NotTo(HaveOccurred())
		_, err = crunchyReconciler().Reconcile(ctx, ctrl.Request{NamespacedName: crNamespacedName})
		Expect(err).NotTo(HaveOccurred())
	})
	It("should create fake pods for statefulsets", func() {
		stsList := &appsv1.StatefulSetList{}
		Expect(k8sClient.List(ctx, stsList, client.InNamespace(ns))).To(Succeed())
		Expect(len(stsList.Items)).To(Equal(4))
		Expect(createFakePodsForStatefulsets(ctx, k8sClient, stsList)).To(Succeed())
	})
	It("Controller should reconcile", func() {
		_, err := reconciler(cr).Reconcile(ctx, ctrl.Request{NamespacedName: crNamespacedName})
		Expect(err).NotTo(HaveOccurred())
		_, err = crunchyReconciler().Reconcile(ctx, ctrl.Request{NamespacedName: crNamespacedName})
		Expect(err).NotTo(HaveOccurred())
		_, err = crunchyReconciler().Reconcile(ctx, ctrl.Request{NamespacedName: crNamespacedName})
		Expect(err).NotTo(HaveOccurred())
	})

	Context("check init containers", func() {
		stsList := &appsv1.StatefulSetList{}
		var backupJob batchv1.Job
		It("should get statefulsets", func() {
			selector, err := naming.AsSelector(metav1.LabelSelector{MatchLabels: map[string]string{naming.LabelCluster: crName, naming.LabelInstanceSet: "instance1"}})
			Expect(err).To(Succeed())

			Expect(k8sClient.List(ctx, stsList, client.InNamespace(ns), client.MatchingLabelsSelector{Selector: selector})).To(Succeed())

			Expect(len(stsList.Items)).To(Equal(3))
		})
		It("should get replica-create backup job", func() {
			jobList := &batchv1.JobList{}
			Expect(k8sClient.List(ctx, jobList, client.InNamespace(ns))).To(Succeed())

			Expect(len(jobList.Items)).To(Equal(1))

			backupJob = jobList.Items[0]
		})

		It("should have default init container in the instances", func() {
			for _, sts := range stsList.Items {
				var initContainer *corev1.Container
				for _, c := range sts.Spec.Template.Spec.InitContainers {
					if c.Name == "database-init" {
						initContainer = &c
						break
					}
				}
				Expect(initContainer).NotTo(BeNil())

				Expect(initContainer.Image).To(Equal("some-image"))
				Expect(initContainer.ImagePullPolicy).To(Equal(corev1.PullAlways))
				Expect(initContainer.Command).To(Equal([]string{"/usr/local/bin/init-entrypoint.sh"}))
				Expect(initContainer.Resources).To(Equal(corev1.ResourceRequirements{}))
				Expect(initContainer.SecurityContext).To(Equal(&corev1.SecurityContext{
					Capabilities: &corev1.Capabilities{
						Drop: []corev1.Capability{
							"ALL",
						},
					},
					Privileged:               ptr.To(false),
					RunAsNonRoot:             ptr.To(true),
					ReadOnlyRootFilesystem:   ptr.To(true),
					AllowPrivilegeEscalation: ptr.To(false),
					SeccompProfile: &corev1.SeccompProfile{
						Type: corev1.SeccompProfileTypeRuntimeDefault,
					},
				}))
				Expect(initContainer.TerminationMessagePath).To(Equal("/dev/termination-log"))
				Expect(initContainer.TerminationMessagePolicy).To(Equal(corev1.TerminationMessageReadFile))
				Expect(initContainer.VolumeMounts).To(Equal([]corev1.VolumeMount{
					{
						Name:      "crunchy-bin",
						MountPath: "/opt/crunchy",
					},
					{
						Name:      "tmp",
						MountPath: "/tmp",
					},
				}))
			}
		})
		It("should have default init container in the backup job", func() {
			var initContainer *corev1.Container
			for _, c := range backupJob.Spec.Template.Spec.InitContainers {
				if c.Name == "pgbackrest-init" {
					initContainer = &c
					break
				}
			}
			Expect(initContainer).NotTo(BeNil())

			Expect(initContainer.Image).To(Equal("some-image"))
			Expect(initContainer.ImagePullPolicy).To(Equal(corev1.PullAlways))
			Expect(initContainer.Command).To(Equal([]string{"/usr/local/bin/init-entrypoint.sh"}))
			Expect(initContainer.Resources).To(Equal(corev1.ResourceRequirements{}))
			Expect(initContainer.SecurityContext).To(Equal(&corev1.SecurityContext{
				Capabilities: &corev1.Capabilities{
					Drop: []corev1.Capability{
						"ALL",
					},
				},
				Privileged:               ptr.To(false),
				RunAsNonRoot:             ptr.To(true),
				ReadOnlyRootFilesystem:   ptr.To(true),
				AllowPrivilegeEscalation: ptr.To(false),
				SeccompProfile: &corev1.SeccompProfile{
					Type: corev1.SeccompProfileTypeRuntimeDefault,
				},
			}))
			Expect(initContainer.TerminationMessagePath).To(Equal("/dev/termination-log"))
			Expect(initContainer.TerminationMessagePolicy).To(Equal(corev1.TerminationMessageReadFile))
			Expect(initContainer.VolumeMounts).To(Equal([]corev1.VolumeMount{
				{
					Name:      "crunchy-bin",
					MountPath: "/opt/crunchy",
				},
			}))
		})

		It("update global initContainer", func() {
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(cr), cr)).To(Succeed())

			cr.Spec.InitContainer.Image = "new-image"
			cr.Spec.InitContainer.ContainerSecurityContext = &corev1.SecurityContext{
				RunAsNonRoot: ptr.To(false),
			}
			cr.Spec.InitContainer.Resources = &corev1.ResourceRequirements{
				Limits: corev1.ResourceList{
					corev1.ResourceCPU: resource.MustParse("2"),
				},
			}
			Expect(k8sClient.Update(ctx, cr)).Should(Succeed())
		})
		It("should delete backup job labels and annotations", func() {
			// Deleting job labels and annotations to update the job during reconcile
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(&backupJob), &backupJob)).To(Succeed())
			backupJob.Labels = nil
			backupJob.Annotations = nil
			Expect(k8sClient.Update(ctx, &backupJob)).To(Succeed())
		})
		It("Controller should reconcile", func() {
			_, err := reconciler(cr).Reconcile(ctx, ctrl.Request{NamespacedName: crNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			_, err = crunchyReconciler().Reconcile(ctx, ctrl.Request{NamespacedName: crNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			_, err = crunchyReconciler().Reconcile(ctx, ctrl.Request{NamespacedName: crNamespacedName})
			Expect(err).NotTo(HaveOccurred())
		})
		It("should get statefulsets", func() {
			selector, err := naming.AsSelector(metav1.LabelSelector{MatchLabels: map[string]string{naming.LabelCluster: crName, naming.LabelInstanceSet: "instance1"}})
			Expect(err).To(Succeed())

			Expect(k8sClient.List(ctx, stsList, client.InNamespace(ns), client.MatchingLabelsSelector{Selector: selector})).To(Succeed())

			Expect(len(stsList.Items)).To(Equal(3))
		})
		It("should get replica-create backup job", func() {
			jobList := &batchv1.JobList{}
			Expect(k8sClient.List(ctx, jobList, client.InNamespace(ns))).To(Succeed())

			Expect(len(jobList.Items)).To(Equal(1))

			backupJob = jobList.Items[0]
		})

		It("should have updated init container in the instances", func() {
			for _, sts := range stsList.Items {
				var initContainer *corev1.Container
				for _, c := range sts.Spec.Template.Spec.InitContainers {
					if c.Name == "database-init" {
						initContainer = &c
						break
					}
				}
				Expect(initContainer).NotTo(BeNil())

				Expect(initContainer.Image).To(Equal("new-image"))
				Expect(initContainer.ImagePullPolicy).To(Equal(corev1.PullAlways))
				Expect(initContainer.Command).To(Equal([]string{"/usr/local/bin/init-entrypoint.sh"}))
				Expect(initContainer.Resources).To(Equal(corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						corev1.ResourceCPU: resource.MustParse("2"),
					},
				}))
				Expect(initContainer.SecurityContext).To(Equal(&corev1.SecurityContext{
					RunAsNonRoot: ptr.To(false),
				}))
				Expect(initContainer.TerminationMessagePath).To(Equal("/dev/termination-log"))
				Expect(initContainer.TerminationMessagePolicy).To(Equal(corev1.TerminationMessageReadFile))
				Expect(initContainer.VolumeMounts).To(Equal([]corev1.VolumeMount{
					{
						Name:      "crunchy-bin",
						MountPath: "/opt/crunchy",
					},
					{
						Name:      "tmp",
						MountPath: "/tmp",
					},
				}))
			}
		})
		It("should have updated init container in the backup job", func() {
			var initContainer *corev1.Container
			for _, c := range backupJob.Spec.Template.Spec.InitContainers {
				if c.Name == "pgbackrest-init" {
					initContainer = &c
					break
				}
			}
			Expect(initContainer).NotTo(BeNil())

			Expect(initContainer.Image).To(Equal("new-image"))
			Expect(initContainer.ImagePullPolicy).To(Equal(corev1.PullAlways))
			Expect(initContainer.Command).To(Equal([]string{"/usr/local/bin/init-entrypoint.sh"}))
			Expect(initContainer.Resources).To(Equal(corev1.ResourceRequirements{
				Limits: corev1.ResourceList{
					corev1.ResourceCPU: resource.MustParse("2"),
				},
			}))
			Expect(initContainer.SecurityContext).To(Equal(&corev1.SecurityContext{
				RunAsNonRoot: ptr.To(false),
			}))
			Expect(initContainer.TerminationMessagePath).To(Equal("/dev/termination-log"))
			Expect(initContainer.TerminationMessagePolicy).To(Equal(corev1.TerminationMessageReadFile))
			Expect(initContainer.VolumeMounts).To(Equal([]corev1.VolumeMount{
				{
					Name:      "crunchy-bin",
					MountPath: "/opt/crunchy",
				},
			}))
		})

		It("update initContainer of the instance set", func() {
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(cr), cr)).To(Succeed())

			cr.Spec.InstanceSets[0].InitContainer = new(v1beta1.InitContainerSpec)
			cr.Spec.InstanceSets[0].InitContainer.Image = "instance-image"
			cr.Spec.InstanceSets[0].InitContainer.ContainerSecurityContext = &corev1.SecurityContext{
				RunAsNonRoot: ptr.To(true),
			}
			cr.Spec.InstanceSets[0].InitContainer.Resources = &corev1.ResourceRequirements{
				Limits: corev1.ResourceList{
					corev1.ResourceCPU: resource.MustParse("3"),
				},
			}
			Expect(k8sClient.Update(ctx, cr)).Should(Succeed())
		})
		It("should delete backup job labels and annotations", func() {
			// Deleting job labels and annotations to update the job during reconcile
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(&backupJob), &backupJob)).To(Succeed())
			backupJob.Labels = nil
			backupJob.Annotations = nil
			Expect(k8sClient.Update(ctx, &backupJob)).To(Succeed())
		})
		It("Controller should reconcile", func() {
			_, err := reconciler(cr).Reconcile(ctx, ctrl.Request{NamespacedName: crNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			_, err = crunchyReconciler().Reconcile(ctx, ctrl.Request{NamespacedName: crNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			_, err = crunchyReconciler().Reconcile(ctx, ctrl.Request{NamespacedName: crNamespacedName})
			Expect(err).NotTo(HaveOccurred())
		})
		It("should get statefulsets", func() {
			selector, err := naming.AsSelector(metav1.LabelSelector{MatchLabels: map[string]string{naming.LabelCluster: crName, naming.LabelInstanceSet: "instance1"}})
			Expect(err).To(Succeed())

			Expect(k8sClient.List(ctx, stsList, client.InNamespace(ns), client.MatchingLabelsSelector{Selector: selector})).To(Succeed())

			Expect(len(stsList.Items)).To(Equal(3))
		})
		It("should get replica-create backup job", func() {
			jobList := &batchv1.JobList{}
			Expect(k8sClient.List(ctx, jobList, client.InNamespace(ns))).To(Succeed())

			Expect(len(jobList.Items)).To(Equal(1))

			backupJob = jobList.Items[0]
		})

		It("should have updated init container in the instances", func() {
			for _, sts := range stsList.Items {
				var initContainer *corev1.Container
				for _, c := range sts.Spec.Template.Spec.InitContainers {
					if c.Name == "database-init" {
						initContainer = &c
						break
					}
				}
				Expect(initContainer).NotTo(BeNil())

				Expect(initContainer.Image).To(Equal("instance-image"))
				Expect(initContainer.ImagePullPolicy).To(Equal(corev1.PullAlways))
				Expect(initContainer.Command).To(Equal([]string{"/usr/local/bin/init-entrypoint.sh"}))
				Expect(initContainer.Resources).To(Equal(corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						corev1.ResourceCPU: resource.MustParse("3"),
					},
				}))
				Expect(initContainer.SecurityContext).To(Equal(&corev1.SecurityContext{
					RunAsNonRoot: ptr.To(true),
				}))
				Expect(initContainer.TerminationMessagePath).To(Equal("/dev/termination-log"))
				Expect(initContainer.TerminationMessagePolicy).To(Equal(corev1.TerminationMessageReadFile))
				Expect(initContainer.VolumeMounts).To(Equal([]corev1.VolumeMount{
					{
						Name:      "crunchy-bin",
						MountPath: "/opt/crunchy",
					},
					{
						Name:      "tmp",
						MountPath: "/tmp",
					},
				}))
			}
		})
		It("should have the same init container in the backup job", func() {
			var initContainer *corev1.Container
			for _, c := range backupJob.Spec.Template.Spec.InitContainers {
				if c.Name == "pgbackrest-init" {
					initContainer = &c
					break
				}
			}
			Expect(initContainer).NotTo(BeNil())

			Expect(initContainer.Image).To(Equal("new-image"))
			Expect(initContainer.ImagePullPolicy).To(Equal(corev1.PullAlways))
			Expect(initContainer.Command).To(Equal([]string{"/usr/local/bin/init-entrypoint.sh"}))
			Expect(initContainer.Resources).To(Equal(corev1.ResourceRequirements{
				Limits: corev1.ResourceList{
					corev1.ResourceCPU: resource.MustParse("2"),
				},
			}))
			Expect(initContainer.SecurityContext).To(Equal(&corev1.SecurityContext{
				RunAsNonRoot: ptr.To(false),
			}))
			Expect(initContainer.TerminationMessagePath).To(Equal("/dev/termination-log"))
			Expect(initContainer.TerminationMessagePolicy).To(Equal(corev1.TerminationMessageReadFile))
			Expect(initContainer.VolumeMounts).To(Equal([]corev1.VolumeMount{
				{
					Name:      "crunchy-bin",
					MountPath: "/opt/crunchy",
				},
			}))
		})

		It("should delete backup job labels and annotations", func() {
			// Deleting job labels and annotations to update the job during reconcile
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(&backupJob), &backupJob)).To(Succeed())
			backupJob.Labels = nil
			backupJob.Annotations = nil
			Expect(k8sClient.Update(ctx, &backupJob)).To(Succeed())
		})
		It("update initContainer of the pgbackrest set", func() {
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(cr), cr)).To(Succeed())

			cr.Spec.Backups.PGBackRest.InitContainer = new(v1beta1.InitContainerSpec)
			cr.Spec.Backups.PGBackRest.InitContainer.Image = "pgbackrest-image"
			cr.Spec.Backups.PGBackRest.InitContainer.ContainerSecurityContext = &corev1.SecurityContext{
				RunAsNonRoot: ptr.To(false),
				Privileged:   ptr.To(true),
			}
			cr.Spec.Backups.PGBackRest.InitContainer.Resources = &corev1.ResourceRequirements{
				Limits: corev1.ResourceList{
					corev1.ResourceCPU: resource.MustParse("4"),
				},
			}
			Expect(k8sClient.Update(ctx, cr)).Should(Succeed())
		})
		It("Controller should reconcile", func() {
			_, err := reconciler(cr).Reconcile(ctx, ctrl.Request{NamespacedName: crNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			_, err = crunchyReconciler().Reconcile(ctx, ctrl.Request{NamespacedName: crNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			_, err = crunchyReconciler().Reconcile(ctx, ctrl.Request{NamespacedName: crNamespacedName})
			Expect(err).NotTo(HaveOccurred())
		})
		It("should get replica-create backup job", func() {
			jobList := &batchv1.JobList{}
			Expect(k8sClient.List(ctx, jobList, client.InNamespace(ns))).To(Succeed())

			Expect(len(jobList.Items)).To(Equal(1))

			backupJob = jobList.Items[0]
		})

		It("should have updated init container in the backup job", func() {
			var initContainer *corev1.Container
			for _, c := range backupJob.Spec.Template.Spec.InitContainers {
				if c.Name == "pgbackrest-init" {
					initContainer = &c
					break
				}
			}
			Expect(initContainer).NotTo(BeNil())

			Expect(initContainer.Image).To(Equal("pgbackrest-image"))
			Expect(initContainer.ImagePullPolicy).To(Equal(corev1.PullAlways))
			Expect(initContainer.Command).To(Equal([]string{"/usr/local/bin/init-entrypoint.sh"}))
			Expect(initContainer.Resources).To(Equal(corev1.ResourceRequirements{
				Limits: corev1.ResourceList{
					corev1.ResourceCPU: resource.MustParse("4"),
				},
			}))
			Expect(initContainer.SecurityContext).To(Equal(&corev1.SecurityContext{
				Privileged:   ptr.To(true),
				RunAsNonRoot: ptr.To(false),
			}))
			Expect(initContainer.TerminationMessagePath).To(Equal("/dev/termination-log"))
			Expect(initContainer.TerminationMessagePolicy).To(Equal(corev1.TerminationMessageReadFile))
			Expect(initContainer.VolumeMounts).To(Equal([]corev1.VolumeMount{
				{
					Name:      "crunchy-bin",
					MountPath: "/opt/crunchy",
				},
			}))
		})
	})
})

var _ = Describe("CR Version Management", Ordered, func() {
	ctx := context.Background()
	const crName = "cr-version"
	const ns = crName

	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:      crName,
			Namespace: ns,
		},
	}

	BeforeAll(func() {
		By("Creating the Namespace for CR version tests")
		err := k8sClient.Create(ctx, namespace)
		Expect(err).To(Not(HaveOccurred()))
	})

	AfterAll(func() {
		By("Deleting the Namespace after CR version tests")
		_ = k8sClient.Delete(ctx, namespace)
	})

	Context("setCRVersion logic", Ordered, func() {
		When("the CRVersion is already set", func() {
			It("should not change the CRVersion", func() {
				cr, err := readDefaultCR("cr-version-1", ns)
				Expect(err).NotTo(HaveOccurred())

				cr.Spec.CRVersion = "2.7.0"
				Expect(k8sClient.Create(ctx, cr)).Should(Succeed())

				reconciler := &PGClusterReconciler{Client: k8sClient}
				err = reconciler.setCRVersion(ctx, cr)
				Expect(err).NotTo(HaveOccurred())
				Expect(cr.Spec.CRVersion).To(Equal("2.7.0"))
			})
		})

		When("the CRVersion is empty", func() {
			It("should set CRVersion and patch the resource", func() {
				cr, err := readDefaultCR("cr-version-2", ns)
				Expect(err).NotTo(HaveOccurred())

				cr.Spec.CRVersion = ""
				Expect(k8sClient.Create(ctx, cr)).Should(Succeed())

				reconciler := &PGClusterReconciler{Client: k8sClient}
				err = reconciler.setCRVersion(ctx, cr)
				Expect(err).NotTo(HaveOccurred())

				// Fetch the CR again to verify the patch was applied in the cluster
				updated := &v2.PerconaPGCluster{}
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: cr.Name, Namespace: cr.Namespace}, updated)).Should(Succeed())
				Expect(updated.Spec.CRVersion).To(Equal(version.Version()))
			})
		})

		When("the patch operation fails", func() {
			It("should return an error", func() {
				cr, err := readDefaultCR("cr-version-3", ns)
				Expect(err).NotTo(HaveOccurred())
				cr.Spec.CRVersion = ""

				// Do NOT create the CR in k8s, so Patch will fail (object does not exist)
				reconciler := &PGClusterReconciler{Client: k8sClient}
				err = reconciler.setCRVersion(ctx, cr)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("patch CR"))
			})
		})
	})
})
