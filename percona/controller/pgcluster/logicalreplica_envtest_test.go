//go:build envtest

package pgcluster

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/percona/percona-postgresql-operator/v3/internal/logicalreplica"
	pNaming "github.com/percona/percona-postgresql-operator/v3/percona/naming"
	v2 "github.com/percona/percona-postgresql-operator/v3/pkg/apis/pgv2.percona.com/v2"
)

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

	It("should default the bootstrap method to pgbackrest", func() {
		cr := cluster("lr-default-method", 17, replica())

		Expect(k8sClient.Create(ctx, cr)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, cr) })

		created := new(v2.PerconaPGCluster)
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(cr), created)).To(Succeed())
		Expect(created.Spec.LogicalReplicas[0].BootstrapMethod).
			To(Equal(v2.LogicalReplicaBootstrapMethodPGBackRest))
	})

	It("should reject a bootstrap method it does not know", func() {
		bad := replica()
		bad.BootstrapMethod = "pg_dump"

		err := k8sClient.Create(ctx, cluster("lr-badmethod", 17, bad))

		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("bootstrapMethod"))
	})

	// Without backups the cluster has no repository, so "pgbackrest" has nothing
	// to restore from and the replica would only ever report itself broken.
	It("should reject pgbackrest seeding without backups", func() {
		cr := cluster("lr-nobackups-pgbackrest", 17, replica())
		cr.Spec.Backups.Enabled = new(false)

		err := k8sClient.Create(ctx, cr)

		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring(
			"bootstrapMethod must be 'pg_basebackup' when spec.backups.enabled is false"))
	})

	It("should accept pg_basebackup seeding without backups", func() {
		seeded := replica()
		seeded.BootstrapMethod = v2.LogicalReplicaBootstrapMethodPGBaseBackup

		cr := cluster("lr-nobackups-basebackup", 17, seeded)
		cr.Spec.Backups.Enabled = new(false)

		Expect(k8sClient.Create(ctx, cr)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, cr) })

		created := new(v2.PerconaPGCluster)
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(cr), created)).To(Succeed())
		Expect(created.Spec.LogicalReplicas[0].BootstrapMethod).
			To(Equal(v2.LogicalReplicaBootstrapMethodPGBaseBackup))
	})

	It("should reject an invalid replica name", func() {
		bad := replica()
		bad.Name = "Analytics_1"

		err := k8sClient.Create(ctx, cluster("lr-badname", 17, bad))

		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("spec.logicalReplicas"))
	})

	// The API server validates condition reasons against a pattern, and a reason
	// it rejects would fail the status update rather than the bootstrap, so every
	// reason the operator can report has to be checked against a real one.
	It("should accept every ReadyForLogicalReplication reason", func() {
		cr := cluster("lr-conditions", 17, replica())
		Expect(k8sClient.Create(ctx, cr)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, cr) })

		r := &PGClusterReconciler{Client: k8sClient}
		statuses := []v2.LogicalReplicaStatus{{
			Name:    "analytics",
			State:   v2.LogicalReplicaStateBootstrapping,
			Reason:  v2.LogicalReplicaReasonPrimaryNotReady,
			Message: "waiting for the primary",
		}}

		for _, reason := range []string{
			"PrimaryPodNotFound",
			"ReplicationSecretMissing",
			"PrimaryUnreachable",
			"PrimaryReady",
			"PrimaryInRecovery",
			"WALLevelNotLogical",
			"RestartPending",
			"ReplicationRoleNotReady",
			"ReplicationHBAMissing",
		} {
			Expect(r.updateLogicalReplicaStatus(ctx, cr, statuses, &metav1.Condition{
				Type:    pNaming.ConditionReadyForLogicalReplication,
				Status:  metav1.ConditionFalse,
				Reason:  reason,
				Message: logicalreplica.PrimaryReadinessMessage(reason),
			})).To(Succeed(), "reason %q", reason)

			updated := new(v2.PerconaPGCluster)
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(cr), updated)).To(Succeed())

			cond := meta.FindStatusCondition(updated.Status.Conditions,
				pNaming.ConditionReadyForLogicalReplication)
			Expect(cond).NotTo(BeNil())
			Expect(cond.Reason).To(Equal(reason))
			Expect(updated.Status.LogicalReplicas).To(HaveLen(1))
		}

		By("removing the condition along with the last replica")
		Expect(r.updateLogicalReplicaStatus(ctx, cr, nil, nil)).To(Succeed())

		updated := new(v2.PerconaPGCluster)
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(cr), updated)).To(Succeed())
		Expect(meta.FindStatusCondition(updated.Status.Conditions,
			pNaming.ConditionReadyForLogicalReplication)).To(BeNil())
		Expect(updated.Status.LogicalReplicas).To(BeEmpty())
	})

	// The state and the reason go into a status the API server validates against
	// the generated CRD, and the timestamps have to survive a round trip: they
	// are what keeps a replica invalidated by a restore from being started again.
	It("should accept every logical replica state, reason and timestamp", func() {
		cr := cluster("lr-states", 17, replica())
		Expect(k8sClient.Create(ctx, cr)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, cr) })

		r := &PGClusterReconciler{Client: k8sClient}
		seeded := metav1.NewTime(metav1.Now().Rfc3339Copy().Time)
		invalidated := metav1.NewTime(seeded.Add(time.Hour))

		for _, state := range []v2.LogicalReplicaState{
			v2.LogicalReplicaStateBootstrapping,
			v2.LogicalReplicaStateReady,
			v2.LogicalReplicaStateBroken,
			v2.LogicalReplicaStateSuspended,
		} {
			for _, reason := range []string{
				v2.LogicalReplicaReasonSourceSlotMissing,
				v2.LogicalReplicaReasonSubscriptionDisabled,
				v2.LogicalReplicaReasonApplyWorkerDown,
				v2.LogicalReplicaReasonBootstrapFailed,
				v2.LogicalReplicaReasonPodNotFound,
				v2.LogicalReplicaReasonPrimaryNotReady,
				v2.LogicalReplicaReasonSourceRestoring,
				v2.LogicalReplicaReasonSourceRestored,
				v2.LogicalReplicaReasonWaitingForDataVolume,
				v2.LogicalReplicaReasonAwaitingCleanup,
			} {
				Expect(r.updateLogicalReplicaStatus(ctx, cr, []v2.LogicalReplicaStatus{{
					Name:          "analytics",
					State:         state,
					Reason:        reason,
					Message:       "message",
					Databases:     []string{"cluster1"},
					SeededAt:      &seeded,
					InvalidatedAt: &invalidated,
				}}, nil)).To(Succeed(), "state %q reason %q", state, reason)

				updated := new(v2.PerconaPGCluster)
				Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(cr), updated)).To(Succeed())
				Expect(updated.Status.LogicalReplicas).To(HaveLen(1))

				recorded := updated.Status.LogicalReplicas[0]
				Expect(recorded.State).To(Equal(state))
				Expect(recorded.Reason).To(Equal(reason))
				Expect(recorded.SeededAt).NotTo(BeNil())
				Expect(recorded.InvalidatedAt).NotTo(BeNil())
				Expect(recorded.InvalidatedAt.After(recorded.SeededAt.Time)).To(BeTrue())
			}
		}
	})
})
