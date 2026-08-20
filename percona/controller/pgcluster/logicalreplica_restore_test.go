package pgcluster

import (
	"context"
	"fmt"
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/percona/percona-postgresql-operator/v3/internal/controller/postgrescluster"
	"github.com/percona/percona-postgresql-operator/v3/internal/naming"
	pNaming "github.com/percona/percona-postgresql-operator/v3/percona/naming"
	v2 "github.com/percona/percona-postgresql-operator/v3/pkg/apis/pgv2.percona.com/v2"
	crunchyv1beta1 "github.com/percona/percona-postgresql-operator/v3/pkg/apis/upstream.pgv2.percona.com/v1beta1"
)

// restoreTestCluster is a cluster that uses the logical replica feature, with no
// restore of any kind in progress.
func restoreTestCluster(t *testing.T, replicas ...v2.LogicalReplicaSpec) *v2.PerconaPGCluster {
	t.Helper()

	cr, err := readDefaultCR("cluster1", "pg")
	require.NoError(t, err)
	cr.Default()
	cr.Spec.CRVersion = "3.1.0"
	cr.Spec.Port = new(int32(5432))
	cr.Spec.LogicalReplicas = replicas
	cr.Status.State = v2.AppStateReady

	return cr
}

func pgRestoreFor(cr *v2.PerconaPGCluster, name string, state v2.PGRestoreState) *v2.PerconaPGRestore {
	return &v2.PerconaPGRestore{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: cr.Namespace},
		Spec:       v2.PerconaPGRestoreSpec{PGCluster: cr.Name, RepoName: new("repo1")},
		Status:     v2.PerconaPGRestoreStatus{State: state},
	}
}

func TestShouldSuspendLogicalReplicas(t *testing.T) {
	for _, tt := range []struct {
		name       string
		mutate     func(*v2.PerconaPGCluster)
		restores   []*v2.PerconaPGRestore
		needed     bool
		invalidate bool
		reason     string
		message    string
	}{
		{
			name: "nothing in progress",
		},
		{
			// A paused cluster has no primary to replicate from, but nothing
			// has touched the data directory, so the replicas stay valid.
			name: "a paused cluster",
			mutate: func(cr *v2.PerconaPGCluster) {
				cr.Spec.Pause = new(true)
			},
			needed:  true,
			reason:  v2.LogicalReplicaReasonClusterPaused,
			message: "the cluster is paused",
		},
		{
			name: "an unpaused cluster",
			mutate: func(cr *v2.PerconaPGCluster) {
				cr.Spec.Pause = new(false)
			},
		},
		{
			name: "the spec flag alone",
			// Set by PGBackRestRestore.Start before anything is torn down, so
			// this is what stops the replicas earliest.
			mutate: func(cr *v2.PerconaPGCluster) {
				cr.Spec.Backups.PGBackRest.Restore = &crunchyv1beta1.PGBackRestRestore{
					Enabled: new(true),
				}
			},
			needed:  true,
			reason:  v2.LogicalReplicaReasonSourceRestoring,
			message: "the cluster is being restored in place",
		},
		{
			name: "a disabled spec flag is not a restore",
			mutate: func(cr *v2.PerconaPGCluster) {
				cr.Spec.Backups.PGBackRest.Restore = &crunchyv1beta1.PGBackRestRestore{
					Enabled: new(false),
				}
			},
		},
		{
			name: "the crunchy annotation alone",
			mutate: func(cr *v2.PerconaPGCluster) {
				cr.Annotations = map[string]string{naming.PGBackRestRestore: "restore1"}
			},
			needed:  true,
			reason:  v2.LogicalReplicaReasonSourceRestoring,
			message: "the cluster is being restored in place",
		},
		{
			// With no restore left in flight this is a restore that failed:
			// nothing removes the condition then, and that is right, because a
			// half-restored data directory invalidates a replica just as
			// thoroughly as a finished one.
			name: "the progressing condition means the data was replaced",
			mutate: func(cr *v2.PerconaPGCluster) {
				cr.Status.Conditions = []metav1.Condition{{
					Type:   postgrescluster.ConditionPGBackRestRestoreProgressing,
					Status: metav1.ConditionTrue,
					Reason: "ReadyForRestore",
				}}
			},
			invalidate: true,
			reason:     v2.LogicalReplicaReasonSourceRestored,
			message:    "logical replica invalidated by a restore of the cluster",
		},
		{
			// The only signal that covers a snapshot restore with no
			// point-in-time recovery, which never sets the two above.
			name:     "a starting restore",
			restores: []*v2.PerconaPGRestore{{}},
			needed:   true,
			reason:   v2.LogicalReplicaReasonSourceRestoring,
			message:  "the cluster is being restored in place",
		},
		{
			name:       "a running restore",
			restores:   []*v2.PerconaPGRestore{{}},
			needed:     true,
			invalidate: true,
			reason:     v2.LogicalReplicaReasonSourceRestored,
			message:    "logical replica invalidated by a restore of the cluster",
		},
		{
			// A restore of a paused cluster: the restore is the more specific
			// answer, and the only one that invalidates.
			name: "a running restore of a paused cluster",
			mutate: func(cr *v2.PerconaPGCluster) {
				cr.Spec.Pause = new(true)
			},
			restores:   []*v2.PerconaPGRestore{{}},
			needed:     true,
			invalidate: true,
			reason:     v2.LogicalReplicaReasonSourceRestored,
			message:    "logical replica invalidated by a restore of the cluster",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cr := restoreTestCluster(t)
			if tt.mutate != nil {
				tt.mutate(cr)
			}

			objs := []client.Object{
				// A restore of another cluster, which must be ignored.
				&v2.PerconaPGRestore{
					ObjectMeta: metav1.ObjectMeta{Name: "other", Namespace: cr.Namespace},
					Spec:       v2.PerconaPGRestoreSpec{PGCluster: "cluster2", RepoName: new("repo1")},
					Status:     v2.PerconaPGRestoreStatus{State: v2.RestoreRunning},
				},
				// A finished restore of this one, which must be ignored too.
				pgRestoreFor(cr, "done", v2.RestoreSucceeded),
			}
			if len(tt.restores) > 0 {
				state := v2.RestoreStarting
				if tt.invalidate {
					state = v2.RestoreRunning
				}
				objs = append(objs, pgRestoreFor(cr, "current", state))
			}

			cl, err := buildFakeClient(t.Context(), cr, objs...)
			require.NoError(t, err)
			r := &PGClusterReconciler{Client: cl}

			s, err := r.shouldSuspendLogicalReplicas(t.Context(), cr)
			require.NoError(t, err)

			assert.Equal(t, tt.needed, s.Needed, "Needed")
			assert.Equal(t, tt.invalidate, s.Invalidate, "Invalidate")
			assert.Equal(t, tt.reason, s.Reason, "Reason")
			assert.Equal(t, tt.message, s.Message, "Message")
		})
	}
}

func TestSuspendLogicalReplicas(t *testing.T) {
	spec := v2.LogicalReplicaSpec{Name: "analytics"}

	// What shouldSuspendLogicalReplicas reports for a restore that has started
	// but has not yet touched the data directory.
	restoring := suspension{
		Needed:  true,
		Reason:  v2.LogicalReplicaReasonSourceRestoring,
		Message: "the cluster is being restored in place",
	}

	// ... and once it has, which is what invalidates a replica.
	restored := suspension{
		Needed:     true,
		Invalidate: true,
		Reason:     v2.LogicalReplicaReasonSourceRestored,
		Message:    "logical replica invalidated by a restore of the cluster",
	}

	statefulSet := func(cr *v2.PerconaPGCluster) *appsv1.StatefulSet {
		return &appsv1.StatefulSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      logicalReplicaObjectName(cr, spec.Name),
				Namespace: cr.Namespace,
			},
			Spec: appsv1.StatefulSetSpec{Replicas: new(int32(1))},
		}
	}

	dataVolume := func(cr *v2.PerconaPGCluster) *corev1.PersistentVolumeClaim {
		return &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{
			Name:      logicalReplicaPVCName(cr, spec.Name),
			Namespace: cr.Namespace,
		}}
	}

	recorded := func(t *testing.T, cl client.Client, cr *v2.PerconaPGCluster) v2.LogicalReplicaStatus {
		t.Helper()

		updated := new(v2.PerconaPGCluster)
		require.NoError(t, cl.Get(t.Context(),
			client.ObjectKey{Name: cr.Name, Namespace: cr.Namespace}, updated))
		require.Len(t, updated.Status.LogicalReplicas, 1)

		return updated.Status.LogicalReplicas[0]
	}

	t.Run("a seeded replica is stopped but kept", func(t *testing.T) {
		cr := restoreTestCluster(t, spec)
		cr.Status.LogicalReplicas = []v2.LogicalReplicaStatus{{
			Name:      spec.Name,
			State:     v2.LogicalReplicaStateReady,
			Databases: []string{"cluster1"},
			SeededAt:  new(metav1.Now()),
		}}

		cl, err := buildFakeClient(t.Context(), cr, statefulSet(cr), dataVolume(cr))
		require.NoError(t, err)
		r := &PGClusterReconciler{Client: cl}

		require.NoError(t, r.suspendLogicalReplicas(t.Context(), cr, restoring))

		sts := new(appsv1.StatefulSet)
		require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(statefulSet(cr)), sts))
		assert.Equal(t, int32(0), *sts.Spec.Replicas)

		// Nothing is destroyed: a restore that fails before it touches the data
		// directory leaves this replica perfectly valid.
		require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(dataVolume(cr)),
			new(corev1.PersistentVolumeClaim)))

		status := recorded(t, cl, cr)
		assert.Equal(t, v2.LogicalReplicaStateSuspended, status.State)
		assert.Equal(t, v2.LogicalReplicaReasonSourceRestoring, status.Reason)
		assert.Nil(t, status.InvalidatedAt, "the data directory has not been replaced yet")
		assert.NotNil(t, status.SeededAt)
		assert.Equal(t, []string{"cluster1"}, status.Databases)
	})

	t.Run("a paused cluster suspends without invalidating", func(t *testing.T) {
		// Pausing takes the primary away just as a restore does, but it leaves
		// the data directory alone, so unpausing brings the replica back.
		paused := suspension{
			Needed:  true,
			Reason:  v2.LogicalReplicaReasonClusterPaused,
			Message: "the cluster is paused",
		}

		cr := restoreTestCluster(t, spec)
		cr.Spec.Pause = new(true)
		cr.Status.LogicalReplicas = []v2.LogicalReplicaStatus{{
			Name:      spec.Name,
			State:     v2.LogicalReplicaStateReady,
			Databases: []string{"cluster1"},
			SeededAt:  new(metav1.Now()),
		}}

		cl, err := buildFakeClient(t.Context(), cr, statefulSet(cr), dataVolume(cr))
		require.NoError(t, err)
		r := &PGClusterReconciler{Client: cl}

		require.NoError(t, r.suspendLogicalReplicas(t.Context(), cr, paused))

		sts := new(appsv1.StatefulSet)
		require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(statefulSet(cr)), sts))
		assert.Equal(t, int32(0), *sts.Spec.Replicas)

		status := recorded(t, cl, cr)
		assert.Equal(t, v2.LogicalReplicaStateSuspended, status.State)
		assert.Equal(t, v2.LogicalReplicaReasonClusterPaused, status.Reason)
		assert.Equal(t, paused.Message, status.Message)
		assert.Nil(t, status.InvalidatedAt, "a pause destroys nothing")
		assert.NotNil(t, status.SeededAt)

		// The condition says why, so unpausing is all it takes to undo this.
		updated := new(v2.PerconaPGCluster)
		require.NoError(t, cl.Get(t.Context(),
			client.ObjectKey{Name: cr.Name, Namespace: cr.Namespace}, updated))

		condition := meta.FindStatusCondition(updated.Status.Conditions,
			pNaming.ConditionReadyForLogicalReplication)
		require.NotNil(t, condition)
		assert.Equal(t, metav1.ConditionFalse, condition.Status)
		assert.Equal(t, v2.LogicalReplicaReasonClusterPaused, condition.Reason)
		assert.Equal(t, paused.Message, condition.Message)
	})

	t.Run("a seeded replica is invalidated once the data is replaced", func(t *testing.T) {
		cr := restoreTestCluster(t, spec)
		cr.Status.LogicalReplicas = []v2.LogicalReplicaStatus{{
			Name:      spec.Name,
			State:     v2.LogicalReplicaStateReady,
			Databases: []string{"cluster1"},
			SeededAt:  new(metav1.Now()),
		}}

		cl, err := buildFakeClient(t.Context(), cr, statefulSet(cr))
		require.NoError(t, err)
		r := &PGClusterReconciler{Client: cl}

		require.NoError(t, r.suspendLogicalReplicas(t.Context(), cr, restored))

		assert.NotNil(t, recorded(t, cl, cr).InvalidatedAt)
	})

	t.Run("an interrupted bootstrap is thrown away", func(t *testing.T) {
		// The Job has a backoff limit of zero, so one the restore interrupts can
		// never succeed, and the half-written data directory it leaves is what
		// the next attempt refuses to seed over.
		cr := restoreTestCluster(t, spec)
		cr.Status.LogicalReplicas = []v2.LogicalReplicaStatus{{
			Name:      spec.Name,
			State:     v2.LogicalReplicaStateBootstrapping,
			Databases: []string{"cluster1"},
		}}

		job := &batchv1.Job{ObjectMeta: metav1.ObjectMeta{
			Name:      logicalReplicaJobName(cr, spec.Name),
			Namespace: cr.Namespace,
		}}

		cl, err := buildFakeClient(t.Context(), cr, job, dataVolume(cr))
		require.NoError(t, err)
		r := &PGClusterReconciler{Client: cl}

		require.NoError(t, r.suspendLogicalReplicas(t.Context(), cr, restored))

		err = cl.Get(t.Context(), client.ObjectKeyFromObject(job), new(batchv1.Job))
		assert.True(t, apierrors.IsNotFound(err), "the bootstrap job must be deleted: %v", err)

		err = cl.Get(t.Context(), client.ObjectKeyFromObject(dataVolume(cr)),
			new(corev1.PersistentVolumeClaim))
		assert.True(t, apierrors.IsNotFound(err), "the partial data volume must be deleted: %v", err)

		status := recorded(t, cl, cr)
		assert.Equal(t, v2.LogicalReplicaStateBootstrapping, status.State)
		assert.Nil(t, status.SeededAt)
		assert.Nil(t, status.Databases, "the frozen list is re-resolved against the restored cluster")
		assert.Nil(t, status.InvalidatedAt, "an unseeded replica has no data to invalidate")
	})

	t.Run("a replica with a statefulset keeps its data volume", func(t *testing.T) {
		// A StatefulSet only ever exists once a bootstrap has completed, so it
		// is the more trustworthy record: the volume holds a replica rather than
		// a partial copy, whatever the status says.
		cr := restoreTestCluster(t, spec)
		cr.Status.LogicalReplicas = []v2.LogicalReplicaStatus{{
			Name:  spec.Name,
			State: v2.LogicalReplicaStateBootstrapping,
		}}

		job := &batchv1.Job{ObjectMeta: metav1.ObjectMeta{
			Name:      logicalReplicaJobName(cr, spec.Name),
			Namespace: cr.Namespace,
		}}

		cl, err := buildFakeClient(t.Context(), cr, job, statefulSet(cr), dataVolume(cr))
		require.NoError(t, err)
		r := &PGClusterReconciler{Client: cl}

		require.NoError(t, r.suspendLogicalReplicas(t.Context(), cr, restored))

		require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(dataVolume(cr)),
			new(corev1.PersistentVolumeClaim)))

		sts := new(appsv1.StatefulSet)
		require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(statefulSet(cr)), sts))
		assert.Equal(t, int32(0), *sts.Spec.Replicas)
	})

	t.Run("a replica that never started is left alone", func(t *testing.T) {
		// No Job ever ran, so the claim is pristine and worth keeping: deleting
		// it would make the cluster provision storage again for no reason.
		cr := restoreTestCluster(t, spec)
		cr.Status.LogicalReplicas = []v2.LogicalReplicaStatus{{
			Name:  spec.Name,
			State: v2.LogicalReplicaStateBootstrapping,
		}}

		cl, err := buildFakeClient(t.Context(), cr, dataVolume(cr))
		require.NoError(t, err)
		r := &PGClusterReconciler{Client: cl}

		require.NoError(t, r.suspendLogicalReplicas(t.Context(), cr, restored))

		require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(dataVolume(cr)),
			new(corev1.PersistentVolumeClaim)))
	})

	t.Run("a replica removed from the spec keeps its status", func(t *testing.T) {
		// Its status entry is the only record of the slots and publications it
		// left on the primary. Dropping it here would leak them.
		cr := restoreTestCluster(t)
		cr.Status.LogicalReplicas = []v2.LogicalReplicaStatus{{
			Name:      spec.Name,
			State:     v2.LogicalReplicaStateReady,
			Databases: []string{"cluster1"},
			SeededAt:  new(metav1.Now()),
		}}

		cl, err := buildFakeClient(t.Context(), cr, statefulSet(cr))
		require.NoError(t, err)
		r := &PGClusterReconciler{Client: cl}

		require.NoError(t, r.suspendLogicalReplicas(t.Context(), cr, restoring))

		assert.Equal(t, []string{"cluster1"}, recorded(t, cl, cr).Databases)
	})
}

// TestRestoreDoesNotErrorWhenReplicaRemoved covers the reconcile that used to
// fail: the teardown needs the primary, and a restore has taken it away.
func TestRestoreDoesNotErrorWhenReplicaRemoved(t *testing.T) {
	cr := restoreTestCluster(t)
	cr.Spec.Backups.PGBackRest.Restore = &crunchyv1beta1.PGBackRestRestore{Enabled: new(true)}
	cr.Status.State = v2.AppStateInit
	cr.Status.LogicalReplicas = []v2.LogicalReplicaStatus{{
		Name:      "analytics",
		State:     v2.LogicalReplicaStateReady,
		Databases: []string{"cluster1"},
		SeededAt:  new(metav1.Now()),
	}}

	cl, err := buildFakeClient(t.Context(), cr)
	require.NoError(t, err)
	// PodExec is nil: that nothing tried to reach the primary is the assertion.
	r := &PGClusterReconciler{Client: cl}

	requeue, err := r.reconcileLogicalReplicas(t.Context(), cr, new(crunchyv1beta1.PostgresCluster))
	require.NoError(t, err)
	assert.False(t, requeue, "both ends of a restore wake this controller on their own")

	updated := new(v2.PerconaPGCluster)
	require.NoError(t, cl.Get(t.Context(),
		client.ObjectKey{Name: cr.Name, Namespace: cr.Namespace}, updated))
	require.Len(t, updated.Status.LogicalReplicas, 1,
		"the teardown is deferred, not forgotten")

	// The spec flag is the earliest restore signal and the only one set here, so
	// it has to name a reason of its own: the API server rejects an empty one.
	condition := meta.FindStatusCondition(updated.Status.Conditions,
		pNaming.ConditionReadyForLogicalReplication)
	require.NotNil(t, condition)
	assert.Equal(t, v2.LogicalReplicaReasonSourceRestoring, condition.Reason)
	assert.NotEmpty(t, condition.Message)
}

// TestCleanupDefersWithoutPrimary covers the WAL leak: a replica whose teardown
// could not be finished used to be dropped from the status, which is the only
// record of the slots it left behind.
func TestCleanupDefersWithoutPrimary(t *testing.T) {
	cr := restoreTestCluster(t)
	cr.Status.LogicalReplicas = []v2.LogicalReplicaStatus{{
		Name:      "analytics",
		State:     v2.LogicalReplicaStateReady,
		Databases: []string{"cluster1"},
		SeededAt:  new(metav1.Now()),
	}}

	cl, err := buildFakeClient(t.Context(), cr)
	require.NoError(t, err)
	r := &PGClusterReconciler{Client: cl}

	requeue, err := r.reconcileLogicalReplicas(t.Context(), cr, new(crunchyv1beta1.PostgresCluster))
	require.NoError(t, err, "one unreachable primary must not fail the whole cluster reconcile")
	assert.True(t, requeue, "nothing else brings the primary back into view")

	updated := new(v2.PerconaPGCluster)
	require.NoError(t, cl.Get(t.Context(),
		client.ObjectKey{Name: cr.Name, Namespace: cr.Namespace}, updated))

	require.Len(t, updated.Status.LogicalReplicas, 1)
	status := updated.Status.LogicalReplicas[0]
	assert.Equal(t, v2.LogicalReplicaReasonAwaitingCleanup, status.Reason)
	assert.Equal(t, []string{"cluster1"}, status.Databases,
		"the database list is what names the slots still to be dropped")
}

func TestInvalidatedReplicaStaysStopped(t *testing.T) {
	spec := v2.LogicalReplicaSpec{Name: "analytics"}

	cr := restoreTestCluster(t, spec)
	cr.Status.LogicalReplicas = []v2.LogicalReplicaStatus{{
		Name:          spec.Name,
		State:         v2.LogicalReplicaStateSuspended,
		Reason:        v2.LogicalReplicaReasonSourceRestoring,
		Databases:     []string{"cluster1"},
		SeededAt:      new(metav1.Now()),
		InvalidatedAt: new(metav1.Now()),
	}}

	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      logicalReplicaObjectName(cr, spec.Name),
			Namespace: cr.Namespace,
		},
		// As if something else had started it again.
		Spec: appsv1.StatefulSetSpec{Replicas: new(int32(1))},
	}

	cl, err := buildFakeClient(t.Context(), cr, sts,
		primaryPodForCluster(cr), logicalReplicaUserSecret(cr))
	require.NoError(t, err)

	recorder := record.NewFakeRecorder(10)
	r := &PGClusterReconciler{Client: cl, Recorder: recorder}
	r.PodExec = func(_ context.Context, _, _, _ string,
		_ io.Reader, out, _ io.Writer, _ ...string,
	) error {
		// Only the cluster-wide readiness probe may run: the replica itself must
		// not be queried at all.
		_, err := fmt.Fprintln(out, "")
		return err
	}

	requeue, err := r.reconcileLogicalReplicas(t.Context(), cr, new(crunchyv1beta1.PostgresCluster))
	require.NoError(t, err)
	assert.False(t, requeue, "this replica is waiting for a person, not for the controller")

	require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(sts), sts))
	assert.Equal(t, int32(0), *sts.Spec.Replicas)

	updated := new(v2.PerconaPGCluster)
	require.NoError(t, cl.Get(t.Context(),
		client.ObjectKey{Name: cr.Name, Namespace: cr.Namespace}, updated))

	require.Len(t, updated.Status.LogicalReplicas, 1)
	status := updated.Status.LogicalReplicas[0]
	assert.Equal(t, v2.LogicalReplicaStateBroken, status.State)
	assert.Equal(t, v2.LogicalReplicaReasonSourceRestored, status.Reason)
	assert.Contains(t, status.Message, "remove it from spec.logicalReplicas")
	assert.NotNil(t, status.InvalidatedAt)

	select {
	case event := <-recorder.Events:
		assert.Contains(t, event, "LogicalReplicaInvalidated")
	default:
		t.Error("expected an event on the pass that establishes the replica is invalid")
	}
}

// TestFailedRestoreResumesReplica covers a restore abandoned before it touched
// the data directory: the replica is still valid, so it is simply started again.
func TestFailedRestoreResumesReplica(t *testing.T) {
	spec := v2.LogicalReplicaSpec{Name: "analytics"}

	cr := restoreTestCluster(t, spec)
	cr.Status.LogicalReplicas = []v2.LogicalReplicaStatus{{
		Name:      spec.Name,
		State:     v2.LogicalReplicaStateSuspended,
		Reason:    v2.LogicalReplicaReasonSourceRestoring,
		Databases: []string{"cluster1"},
		SeededAt:  new(metav1.Now()),
	}}

	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      logicalReplicaObjectName(cr, spec.Name),
			Namespace: cr.Namespace,
		},
		Spec: appsv1.StatefulSetSpec{Replicas: new(int32(0))},
	}

	crunchyCR := &crunchyv1beta1.PostgresCluster{
		ObjectMeta: metav1.ObjectMeta{Name: cr.Name, Namespace: cr.Namespace},
	}
	crunchyCR.Spec.PostgresVersion = 17

	cl, err := buildFakeClient(t.Context(), cr, sts,
		primaryPodForCluster(cr), logicalReplicaUserSecret(cr))
	require.NoError(t, err)

	r := &PGClusterReconciler{Client: cl}
	r.PodExec = func(_ context.Context, _, _, _ string,
		stdin io.Reader, out, _ io.Writer, _ ...string,
	) error {
		sql, err := io.ReadAll(stdin)
		require.NoError(t, err)
		if strings.Contains(string(sql), "pg_hba_file_rules") {
			_, err = fmt.Fprintln(out, "")
			return err
		}
		// The health check runs, which is the point: the replica is managed
		// again rather than held down.
		return errors.New("FATAL: the database system is starting up")
	}

	_, err = r.reconcileLogicalReplicas(t.Context(), cr, crunchyCR)
	require.NoError(t, err)

	require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(sts), sts))
	assert.Equal(t, int32(1), *sts.Spec.Replicas, "an abandoned restore costs the replica nothing")
}

func TestReconcileLogicalReplicaPVCWaitsForDeletion(t *testing.T) {
	spec := &v2.LogicalReplicaSpec{Name: "analytics"}

	cr := restoreTestCluster(t, *spec)

	// A finalizer is what keeps the claim around long enough to be observed
	// Terminating, which is exactly what happens in the cluster: the claim
	// cannot go before the pod holding it does.
	pvc := &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{
		Name:       logicalReplicaPVCName(cr, spec.Name),
		Namespace:  cr.Namespace,
		Finalizers: []string{"kubernetes.io/pvc-protection"},
	}}

	cl, err := buildFakeClient(t.Context(), cr, pvc)
	require.NoError(t, err)
	require.NoError(t, cl.Delete(t.Context(), pvc))

	r := &PGClusterReconciler{Client: cl}

	ready, err := r.reconcileLogicalReplicaPVC(t.Context(), cr, spec)
	require.NoError(t, err)
	assert.False(t, ready, "a claim on its way out must not be adopted")

	crunchyCR := &crunchyv1beta1.PostgresCluster{
		ObjectMeta: metav1.ObjectMeta{Name: cr.Name, Namespace: cr.Namespace},
	}
	crunchyCR.Spec.PostgresVersion = 17

	status := &v2.LogicalReplicaStatus{
		Name:      spec.Name,
		Databases: []string{"cluster1"},
	}
	cr.Status.LogicalReplicas = []v2.LogicalReplicaStatus{*status}

	got, err := r.reconcileLogicalReplica(t.Context(), cr, crunchyCR, spec, true)
	require.NoError(t, err)
	assert.Equal(t, v2.LogicalReplicaReasonWaitingForDataVolume, got.Reason)

	err = cl.Get(t.Context(), client.ObjectKey{
		Name: logicalReplicaJobName(cr, spec.Name), Namespace: cr.Namespace}, new(batchv1.Job))
	assert.True(t, apierrors.IsNotFound(err),
		"no bootstrap job may be created against a dying volume: %v", err)
}

// TestDropLogicalReplicaObjectsSkipsMissingDatabase covers a point-in-time
// restore that rewound the primary past the creation of a database the replica
// covered: psql cannot connect to it, so the publication drop has to be skipped
// rather than allowed to fail the teardown forever.
func TestDropLogicalReplicaObjectsSkipsMissingDatabase(t *testing.T) {
	cr := restoreTestCluster(t)

	cl, err := buildFakeClient(t.Context(), cr, primaryPodForCluster(cr))
	require.NoError(t, err)

	statements := make([]string, 0)
	r := &PGClusterReconciler{Client: cl}
	r.PodExec = func(_ context.Context, _, _, _ string,
		stdin io.Reader, out, _ io.Writer, command ...string,
	) error {
		sql, err := io.ReadAll(stdin)
		require.NoError(t, err)
		statements = append(statements, string(sql))

		if strings.Contains(string(sql), "datallowconn") {
			_, err = fmt.Fprintln(out, "cluster1")
			return err
		}
		if slices.Contains(command, "--dbname=gone") {
			return errors.New(`FATAL: database "gone" does not exist`)
		}

		_, err = fmt.Fprintln(out, "")
		return err
	}

	err = r.dropLogicalReplicaObjects(t.Context(), cr, "analytics", []string{"cluster1", "gone"})
	require.NoError(t, err)

	joined := strings.Join(statements, "\n")
	// The slot is cluster-wide and is dropped either way. That is the part that
	// matters: it is what pins WAL on the primary.
	assert.Contains(t, joined, "pg_drop_replication_slot")
	assert.Equal(t, 1, strings.Count(joined, "DROP PUBLICATION"),
		"only the database that still exists may have its publication dropped")
}

func TestLogicalReplicaSettled(t *testing.T) {
	for _, tt := range []struct {
		state   v2.LogicalReplicaState
		reason  string
		settled bool
	}{
		{state: v2.LogicalReplicaStateReady, settled: true},
		{state: v2.LogicalReplicaStateBootstrapping},
		{state: v2.LogicalReplicaStateBootstrapping, reason: v2.LogicalReplicaReasonPodNotFound},
		{state: v2.LogicalReplicaStateSuspended, reason: v2.LogicalReplicaReasonSourceRestoring},
		{state: v2.LogicalReplicaStateBroken, reason: v2.LogicalReplicaReasonApplyWorkerDown},
		{state: v2.LogicalReplicaStateBroken, reason: v2.LogicalReplicaReasonAwaitingCleanup},
		// All three mean the replica has to be seeded again, which only a person
		// can ask for, and the edit that asks wakes this controller anyway.
		{state: v2.LogicalReplicaStateBroken, reason: v2.LogicalReplicaReasonSourceRestored, settled: true},
		{state: v2.LogicalReplicaStateBroken, reason: v2.LogicalReplicaReasonSourceSlotMissing, settled: true},
		{state: v2.LogicalReplicaStateBroken, reason: v2.LogicalReplicaReasonBootstrapFailed, settled: true},
	} {
		t.Run(string(tt.state)+"/"+tt.reason, func(t *testing.T) {
			assert.Equal(t, tt.settled, logicalReplicaSettled(&v2.LogicalReplicaStatus{
				State: tt.state, Reason: tt.reason,
			}))
		})
	}
}
