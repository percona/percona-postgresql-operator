//go:build envtest
// +build envtest

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
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/percona/percona-postgresql-operator/internal/feature"
	"github.com/percona/percona-postgresql-operator/internal/naming"
	pNaming "github.com/percona/percona-postgresql-operator/percona/naming"
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
			err := reconcilerInstance.reconcilePatroniVersionCheck(ctx, cr)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should copy custom patroni version to status", func() {
			updatedCR := &v2.PerconaPGCluster{}
			Expect(k8sClient.Get(ctx, crNamespacedName, updatedCR)).Should(Succeed())

			Expect(updatedCR.Status.PatroniVersion).To(Equal("3.2.1"))
		})
	})

	Context("Without custom patroni version annotation", func() {
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
			cr2.Spec.ImagePullSecrets = []corev1.LocalObjectReference{
				{Name: "test-pull-secret"},
			}

			cr2.Status.PatroniVersion = "3.1.0"
			cr2.Status.Postgres.ImageID = "some-image-id"

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
			err := reconcilerInstance.reconcilePatroniVersionCheck(ctx, cr2)
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

			uid := int64(1001)
			expectedSecurityContext := &corev1.PodSecurityContext{
				RunAsUser: &uid,
			}
			expectedImagePullSecrets := []corev1.LocalObjectReference{
				{Name: "test-pull-secret"},
			}

			Expect(pod.Spec.SecurityContext).To(Equal(expectedSecurityContext))
			Expect(pod.Spec.TerminationGracePeriodSeconds).To(Equal(ptr.To(int64(5))))
			Expect(pod.Spec.ImagePullSecrets).To(Equal(expectedImagePullSecrets))
		})

		It("should preserve existing patroni version in annotation", func() {
			updatedCR := &v2.PerconaPGCluster{}
			Expect(k8sClient.Get(ctx, crNamespacedName2, updatedCR)).Should(Succeed())

			Expect(updatedCR.Status.PatroniVersion).To(Equal("3.1.0"))
		})
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
})
