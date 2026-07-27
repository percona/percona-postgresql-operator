//go:build envtest

package pgcluster

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v2 "github.com/percona/percona-postgresql-operator/v2/pkg/apis/pgv2.percona.com/v2"
)

// K8SPG-784: the PostgreSQL version gate is a CEL rule on the CRD, so it is only
// really exercised against a live API server with the generated CRDs installed.
var _ = Describe("Logical replicas", Ordered, func() {
	ctx := context.Background()

	const crName = "logical-replicas"
	const ns = crName

	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: crName, Namespace: ns},
	}

	BeforeAll(func() {
		By("Creating the Namespace to perform the tests")
		Expect(k8sClient.Create(ctx, namespace)).To(Succeed())
	})

	AfterAll(func() {
		By("Deleting the Namespace to perform the tests")
		_ = k8sClient.Delete(ctx, namespace)
	})

	replica := func() v2.LogicalReplicaSpec {
		return v2.LogicalReplicaSpec{
			Name: "analytics",
			DataVolumeClaimSpec: corev1.PersistentVolumeClaimSpec{
				AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
				Resources: corev1.VolumeResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceStorage: resource.MustParse("1Gi"),
					},
				},
			},
		}
	}

	cluster := func(name string, postgresVersion int, replicas ...v2.LogicalReplicaSpec) *v2.PerconaPGCluster {
		cr, err := readDefaultCR(name, ns)
		Expect(err).NotTo(HaveOccurred())

		cr.Spec.PostgresVersion = postgresVersion
		cr.Spec.LogicalReplicas = replicas

		return cr
	}

	It("should reject logical replicas on PostgreSQL 16", func() {
		err := k8sClient.Create(ctx, cluster("lr-pg16", 16, replica()))

		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("spec.logicalReplicas requires spec.postgresVersion >= 17"))
	})

	It("should accept logical replicas on PostgreSQL 17", func() {
		cr := cluster("lr-pg17", 17, replica())

		Expect(k8sClient.Create(ctx, cr)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, cr) })

		created := new(v2.PerconaPGCluster)
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(cr), created)).To(Succeed())
		Expect(created.Spec.LogicalReplicas).To(HaveLen(1))
		Expect(created.Spec.LogicalReplicas[0].Name).To(Equal("analytics"))
		// Omitting databases means "replicate everything"; it must stay unset
		// so the operator resolves it against the primary.
		Expect(created.Spec.LogicalReplicas[0].Databases).To(BeEmpty())
	})

	It("should accept a cluster without logical replicas on PostgreSQL 16", func() {
		cr := cluster("lr-none", 16)

		Expect(k8sClient.Create(ctx, cr)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, cr) })
	})

	It("should reject an invalid replica name", func() {
		bad := replica()
		bad.Name = "Analytics_1"

		err := k8sClient.Create(ctx, cluster("lr-badname", 17, bad))

		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("spec.logicalReplicas"))
	})
})
