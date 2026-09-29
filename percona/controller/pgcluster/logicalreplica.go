package pgcluster

import (
	"context"
	"fmt"
	"io"
	"maps"
	"path"
	"slices"
	"strconv"
	"strings"

	"github.com/pkg/errors"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/percona/percona-postgresql-operator/v3/internal/controller/postgrescluster"
	"github.com/percona/percona-postgresql-operator/v3/internal/initialize"
	"github.com/percona/percona-postgresql-operator/v3/internal/logging"
	"github.com/percona/percona-postgresql-operator/v3/internal/logicalreplica"
	"github.com/percona/percona-postgresql-operator/v3/internal/naming"
	"github.com/percona/percona-postgresql-operator/v3/internal/patroni"
	"github.com/percona/percona-postgresql-operator/v3/internal/pgbackrest"
	"github.com/percona/percona-postgresql-operator/v3/internal/postgres"
	perconaController "github.com/percona/percona-postgresql-operator/v3/percona/controller"
	"github.com/percona/percona-postgresql-operator/v3/percona/k8s"
	pNaming "github.com/percona/percona-postgresql-operator/v3/percona/naming"
	perconaPG "github.com/percona/percona-postgresql-operator/v3/percona/postgres"
	v2 "github.com/percona/percona-postgresql-operator/v3/pkg/apis/pgv2.percona.com/v2"
	"github.com/percona/percona-postgresql-operator/v3/pkg/apis/upstream.pgv2.percona.com/v1beta1"
)

const (
	// logicalReplicaConfigMountPath is deliberately not naming.ConfigMountPath,
	// which spec.config.files owns.
	logicalReplicaConfigMountPath = "/etc/logical-replica"

	logicalReplicaConfigFile = "postgresql.conf"

	// logicalReplicaBootstrapConfigFile is the postgresql.conf that
	// pg_createsubscriber runs the target with during the conversion.
	logicalReplicaBootstrapConfigFile = "bootstrap.conf"

	logicalReplicaConfigVolume = "logical-replica-config"

	// logicalReplicaSocketVolume holds the directory Postgres binds its socket
	// in. It is a volume of its own so that nss_wrapper can have the rest of
	// /tmp: Postgres creates the socket but not the directory holding it, and
	// the kubelet creates a mounted one with the Pod's filesystem group.
	logicalReplicaSocketVolume = "postgres-socket"

	logicalReplicaBootstrapContainer = "logical-replica-bootstrap"
	logicalReplicaComponent          = "logical-replica"

	// logicalReplicaRecoveryTimeout bounds how long pg_createsubscriber waits
	// for the freshly seeded standby to replay up to the conversion LSN.
	logicalReplicaRecoveryTimeout = 3600

	// logicalReplicaPGCtlTimeout bounds how long the bootstrap Job waits for the
	// converted target to start and to stop again.
	logicalReplicaPGCtlTimeout = 300

	// logicalReplicaWorkerHeadroom is added on top of one worker per database
	// when sizing max_worker_processes, which must be strictly greater than the
	// number of databases.
	logicalReplicaWorkerHeadroom = 8
)

func logicalReplicaName(cr *v2.PerconaPGCluster, replica string) string {
	return cr.Name + "-lr-" + replica
}

func logicalReplicaObjectName(cr *v2.PerconaPGCluster, replica string) string {
	return naming.SafeDNSUniqueName(logicalReplicaName(cr, replica))
}

func logicalReplicaPVCName(cr *v2.PerconaPGCluster, replica string) string {
	return naming.SafeDNSUniqueName(logicalReplicaName(cr, replica) + "-pgdata")
}

func logicalReplicaJobName(cr *v2.PerconaPGCluster, replica string) string {
	return naming.SafeDNSUniqueName(logicalReplicaName(cr, replica) + "-bootstrap")
}

func logicalReplicaConfigMapName(cr *v2.PerconaPGCluster, replica string) string {
	return naming.SafeDNSUniqueName(logicalReplicaName(cr, replica) + "-config")
}

func logicalReplicaUserSecretName(cr *v2.PerconaPGCluster) string {
	return cr.Name + "-" + naming.RolePostgresUser + "-" + v2.UserLogicalReplication
}

func logicalReplicaSelector(cr *v2.PerconaPGCluster, replica string) map[string]string {
	return map[string]string{
		naming.LabelCluster:         cr.Name,
		pNaming.LabelLogicalReplica: replica,
	}
}

func logicalReplicaLabels(cr *v2.PerconaPGCluster, replica string) map[string]string {
	return naming.WithPerconaLabels(naming.Merge(
		logicalReplicaSelector(cr, replica),
		map[string]string{pNaming.LabelOperatorVersion: cr.Spec.CRVersion},
	), cr.Name, logicalReplicaComponent, cr.Spec.CRVersion)
}

// reconcileLogicalReplicas brings every logical replica in the spec to life and
// tears down the ones that were removed from it. It reports whether any replica
// is still working towards being ready.
func (r *PGClusterReconciler) reconcileLogicalReplicas(
	ctx context.Context, cr *v2.PerconaPGCluster, crunchyCR *v1beta1.PostgresCluster,
) (bool, error) {
	if cr.CompareVersion("3.1.0") < 0 {
		return false, nil
	}

	if len(cr.Spec.LogicalReplicas) == 0 && len(cr.Status.LogicalReplicas) == 0 {
		return false, r.updateLogicalReplicaStatus(ctx, cr, cr.Status.LogicalReplicas, nil)
	}

	suspension, err := r.shouldSuspendLogicalReplicas(ctx, cr)
	if err != nil {
		return false, errors.Wrap(err, "observe restore")
	}
	if suspension.Needed {
		return false, r.suspendLogicalReplicas(ctx, cr, suspension)
	}

	// Runs even when the section is empty: that is the case where the last
	// replica was just removed.
	deferred, err := r.cleanupRemovedLogicalReplicas(ctx, cr)
	if err != nil {
		return false, errors.Wrap(err, "clean up removed logical replicas")
	}

	if len(cr.Spec.LogicalReplicas) == 0 {
		if len(deferred) > 0 {
			// Nothing else will bring the primary back into view, so this is
			// the one case where the teardown has to be polled for.
			return true, r.updateLogicalReplicaStatus(ctx, cr, deferred, nil)
		}

		// The condition goes with the last replica, so the next reconcile can
		// take the shortcut above with nothing stale left behind.
		return false, r.updateLogicalReplicaStatus(ctx, cr, nil, nil)
	}

	// Everything below talks to the primary. No requeue: updateStatus writes the
	// state just before this runs, so the cluster becoming ready is itself a
	// status change that wakes this controller.
	if cr.Status.State != v2.AppStateReady {
		return len(deferred) > 0, nil
	}

	log := logging.FromContext(ctx).WithName("LogicalReplication")
	ctx = logging.NewContext(ctx, log)

	readiness := r.observePrimaryReadiness(ctx, cr)

	// Replicas whose teardown could not be finished keep their status entry:
	// forgetting one leaks the logical slots it left on the primary.
	statuses := make([]v2.LogicalReplicaStatus, 0, len(cr.Spec.LogicalReplicas)+len(deferred))
	statuses = append(statuses, deferred...)
	requeue := len(deferred) > 0

	for i := range cr.Spec.LogicalReplicas {
		spec := &cr.Spec.LogicalReplicas[i]

		status, err := r.reconcileLogicalReplica(ctx, cr, crunchyCR, spec,
			readiness.Status == metav1.ConditionTrue)
		if err != nil {
			// One broken replica must not stall the others or the rest of the
			// cluster: record it and carry on. Every error path below hands back
			// the status it was working on, so this is never nil.
			log.Error(err, "reconcile logical replica", "logicalReplica", spec.Name)

			status.State = v2.LogicalReplicaStateBroken
			status.Message = err.Error()
		}

		statuses = append(statuses, *status)

		// bootstrap logical replicas one by one
		// otherwise we might think primary has enough free slots
		// even when it hasn't to accommodate all pending replicas
		if status.State == v2.LogicalReplicaStateBootstrapping {
			return true, r.updateLogicalReplicaStatus(ctx, cr, statuses, &readiness)
		}

		if !logicalReplicaSettled(status) {
			requeue = true
		}
	}

	return requeue, r.updateLogicalReplicaStatus(ctx, cr, statuses, &readiness)
}

func logicalReplicaSettled(status *v2.LogicalReplicaStatus) bool {
	if status.State == v2.LogicalReplicaStateReady {
		return true
	}
	if status.State != v2.LogicalReplicaStateBroken {
		return false
	}

	switch status.Reason {
	case v2.LogicalReplicaReasonSourceUpgraded:
		// Also unrecoverable, but the slots the upgrade carried over to the new
		// cluster still have to be dropped, and the database list is what names the
		// ones still to go.
		return len(status.Databases) == 0

	case v2.LogicalReplicaReasonSourceRestored,
		v2.LogicalReplicaReasonSourceSlotMissing,
		v2.LogicalReplicaReasonBootstrapFailed:
		// All three mean the same thing: the data on this replica cannot be
		// reconciled with the primary, and only seeding it again fixes that.
		return true
	}

	return false
}

// observePrimaryReadiness reports whether the primary carries everything a
// logical replica bootstrap needs, all of it written by the PostgresCluster
// controller, which reconciles independently of this one.
func (r *PGClusterReconciler) observePrimaryReadiness(
	ctx context.Context, cr *v2.PerconaPGCluster,
) metav1.Condition {
	// LastTransitionTime left zero on purpose: meta.SetStatusCondition only
	// stamps it when Status changes, so a message that moves on its own does not
	// look like a transition.
	condition := metav1.Condition{
		Type:   pNaming.ConditionReadyForLogicalReplication,
		Status: metav1.ConditionFalse,
	}

	primary, err := perconaPG.GetPrimaryPod(ctx, r.Client, cr)
	if err != nil {
		condition.Reason = "PrimaryPodNotFound"
		condition.Message = err.Error()
		return condition
	}

	// The Job reads the password from this Secret through a secretKeyRef, and a
	// pod stuck on a missing one says nothing about why.
	secret := &corev1.Secret{}
	key := client.ObjectKey{Name: logicalReplicaUserSecretName(cr), Namespace: cr.Namespace}
	if err := r.Client.Get(ctx, key, secret); err != nil || len(secret.Data["password"]) == 0 {
		condition.Reason = "ReplicationSecretMissing"
		condition.Message = "waiting for the " + key.Name + " secret"
		return condition
	}

	// The exact signal handlePatroniRestarts acts on. The query below sees a
	// pending restart sooner and reports it under the same reason.
	if patroni.PodRequiresRestart(primary) {
		condition.Reason = logicalreplica.ReasonRestartPending
		condition.Message = logicalreplica.PrimaryReadinessMessage(logicalreplica.ReasonRestartPending)
		return condition
	}

	stdout, err := r.execOnPod(ctx, primary, "", logicalreplica.PrimaryReadinessQuery())
	if err != nil {
		logging.FromContext(ctx).V(1).Info("cannot query the primary for logical replication readiness",
			"error", err.Error())

		condition.Status = metav1.ConditionUnknown
		condition.Reason = "PrimaryUnreachable"
		condition.Message = err.Error()
		return condition
	}

	if reasons := logicalreplica.ParsePrimaryReadinessReasons(stdout); len(reasons) > 0 {
		messages := make([]string, 0, len(reasons))
		for _, reason := range reasons {
			messages = append(messages, logicalreplica.PrimaryReadinessMessage(reason))
		}

		condition.Reason = reasons[0]
		condition.Message = strings.Join(messages, "; ")
		return condition
	}

	condition.Status = metav1.ConditionTrue
	condition.Reason = "PrimaryReady"
	condition.Message = "the primary is ready for logical replica bootstrap"
	return condition
}

const logicalReplicaReseedInstructions = "remove it from spec.logicalReplicas, " +
	"wait for its entry to leave status.logicalReplicas, and add it back to seed it again"

// logicalReplicaInvalidation reports why a replica can no longer be reconciled
// with the cluster.
func logicalReplicaInvalidation(
	cr *v2.PerconaPGCluster, status *v2.LogicalReplicaStatus,
) (reason, message, cause string) {
	if status.PostgresVersion != 0 && status.PostgresVersion != cr.Spec.PostgresVersion {
		return v2.LogicalReplicaReasonSourceUpgraded,
			fmt.Sprintf("this replica holds a PostgreSQL %d data directory and the cluster now runs "+
				"%d; the pg_upgrade job only rewrites the data directories of the instance sets, so "+
				"this one cannot be started again; "+logicalReplicaReseedInstructions,
				status.PostgresVersion, cr.Spec.PostgresVersion),
			fmt.Sprintf("the cluster was upgraded from PostgreSQL %d to %d after it was seeded",
				status.PostgresVersion, cr.Spec.PostgresVersion)
	}

	return v2.LogicalReplicaReasonSourceRestored,
		"the cluster was restored in place after this replica was seeded, so its data " +
			"can no longer be reconciled with the primary; " + logicalReplicaReseedInstructions,
		"the cluster was restored in place after it was seeded"
}

// recordedLogicalReplicaReason reports whether the persisted status of a replica
// already carries reason, so callers can log and emit an event once rather than
// on every pass.
func recordedLogicalReplicaReason(cr *v2.PerconaPGCluster, replica, reason string) bool {
	for i := range cr.Status.LogicalReplicas {
		if cr.Status.LogicalReplicas[i].Name == replica {
			return cr.Status.LogicalReplicas[i].Reason == reason
		}
	}

	return false
}

// logicalReplicaStatusFor returns a copy of the recorded status of a replica, or
// a fresh one if it has none yet.
func logicalReplicaStatusFor(cr *v2.PerconaPGCluster, replica string) *v2.LogicalReplicaStatus {
	for i := range cr.Status.LogicalReplicas {
		if cr.Status.LogicalReplicas[i].Name == replica {
			status := cr.Status.LogicalReplicas[i].DeepCopy()
			// Reason and message are re-derived on every pass.
			status.Reason = ""
			status.Message = ""
			return status
		}
	}

	return &v2.LogicalReplicaStatus{
		Name:  replica,
		State: v2.LogicalReplicaStateBootstrapping,
	}
}

// updateLogicalReplicaStatus writes the logical replica statuses and the
// ReadyForLogicalReplication condition in one update, so the two can never
// disagree. A nil readiness removes the condition, which is what happens when
// the last replica leaves the spec.
func (r *PGClusterReconciler) updateLogicalReplicaStatus(
	ctx context.Context, cr *v2.PerconaPGCluster,
	statuses []v2.LogicalReplicaStatus, readiness *metav1.Condition,
) error {
	return errors.Wrap(retry.RetryOnConflict(retry.DefaultRetry, func() error {
		cluster := &v2.PerconaPGCluster{}
		if err := r.Client.Get(ctx, types.NamespacedName{
			Name:      cr.Name,
			Namespace: cr.Namespace,
		}, cluster); err != nil {
			return errors.Wrap(err, "get PerconaPGCluster")
		}

		cluster.Status.LogicalReplicas = statuses

		if readiness == nil {
			meta.RemoveStatusCondition(&cluster.Status.Conditions,
				pNaming.ConditionReadyForLogicalReplication)
		} else {
			condition := *readiness
			condition.ObservedGeneration = cluster.Generation
			meta.SetStatusCondition(&cluster.Status.Conditions, condition)
		}

		return r.Client.Status().Update(ctx, cluster)
	}), "update logical replica status")
}

// reconcileLogicalReplica drives a single replica through its lifecycle:
// resolve databases, seed and convert with a one-shot Job, then run it.
func (r *PGClusterReconciler) reconcileLogicalReplica(
	ctx context.Context, cr *v2.PerconaPGCluster, crunchyCR *v1beta1.PostgresCluster,
	spec *v2.LogicalReplicaSpec, primaryReady bool,
) (*v2.LogicalReplicaStatus, error) {
	log := logging.FromContext(ctx).WithValues("logicalReplica", spec.Name)
	status := logicalReplicaStatusFor(cr, spec.Name)

	// The pg_upgrade job rewrites the data directories of the instance sets and
	// nothing else, so a major upgrade leaves this replica holding /pgdata/pg<old>
	// while reconcileLogicalReplicaStatefulSet would now render
	// "postgres -D /pgdata/pg<new>" with the new image. Stop before that: the pod
	// could never start, and nothing would say why.
	if status.InvalidatedAt == nil && status.SeededAt != nil &&
		status.PostgresVersion != 0 && status.PostgresVersion != cr.Spec.PostgresVersion {
		status.InvalidatedAt = new(metav1.Now())
	}

	// See v2.LogicalReplicaReasonSourceRestored and
	// v2.LogicalReplicaReasonSourceUpgraded: neither is recoverable in place, and
	// the system identifier is unchanged, so starting it again would have it
	// serve data the cluster no longer has.
	if status.InvalidatedAt != nil {
		if err := r.scaleLogicalReplica(ctx, cr, spec.Name, 0); err != nil {
			return status, errors.Wrap(err, "stop invalidated replica")
		}

		reason, message, cause := logicalReplicaInvalidation(cr, status)

		if !recordedLogicalReplicaReason(cr, spec.Name, reason) {
			log.Info("logical replica needs to be seeded again",
				"reason", reason, "invalidatedAt", status.InvalidatedAt)
			r.Recorder.Eventf(cr, corev1.EventTypeWarning, "LogicalReplicaInvalidated",
				"Logical replica %q must be seeded again: %s", spec.Name, cause)
		}

		// pg_upgrade carries the logical slots over to the new cluster, where nothing
		// will ever read from them again, and Patroni's ignore_slots keeps it from
		// reaping them. Drop them rather than pin WAL on the primary until someone
		// gets around to seeding this replica again. Clearing the database list is
		// what records that they are gone, so a failure here is retried.
		if reason == v2.LogicalReplicaReasonSourceUpgraded && len(status.Databases) > 0 {
			if err := r.dropLogicalReplicaObjects(ctx, cr, spec.Name, status.Databases); err != nil {
				log.Error(err, "could not drop the replication objects of an invalidated replica")
			} else {
				status.Databases = nil
			}
		}

		status.State = v2.LogicalReplicaStateBroken
		status.Reason = reason
		status.Message = message
		return status, nil
	}

	// A bootstrap that starts too early cannot be retried: pg_createsubscriber
	// leaves the data volume unusable. Replicas already bootstrapped skip this
	// deliberately, so an unrelated pending restart never stops managing the ones
	// that are running.
	if status.SeededAt == nil && !primaryReady {
		status.State = v2.LogicalReplicaStateBootstrapping
		status.Reason = v2.LogicalReplicaReasonPrimaryNotReady
		status.Message = "waiting for the primary; see the " +
			pNaming.ConditionReadyForLogicalReplication + " condition"
		return status, nil
	}

	// Resolved exactly once and then frozen: the publications, subscriptions and
	// slots are all named after this list, so it must not drift when databases
	// are created or dropped later. That is what makes the waits below worth
	// having - a half-resolved list would be just as permanent as a complete one.
	if len(status.Databases) == 0 {
		// DatabaseRevision is written only once the PostgresCluster controller's
		// whole create pass has succeeded. No requeue: this reconciler owns that
		// CR, so the status write that sets the revision wakes it.
		if crunchyCR.Status.DatabaseRevision == "" {
			status.State = v2.LogicalReplicaStateBootstrapping
			status.Reason = v2.LogicalReplicaReasonWaitingForDatabases
			status.Message = "waiting for the operator to create the databases of the cluster"
			return status, nil
		}

		databases, missing, err := r.resolveLogicalReplicaDatabases(ctx, cr, spec)
		if err != nil {
			return status, errors.Wrap(err, "resolve databases")
		}
		if len(missing) > 0 {
			// pg_createsubscriber cannot subscribe to a database that is not
			// there, and the Job gets exactly one attempt.
			status.State = v2.LogicalReplicaStateBootstrapping
			status.Reason = v2.LogicalReplicaReasonWaitingForDatabases
			status.Message = "waiting for these databases of spec.logicalReplicas[].databases " +
				"to be created: " + strings.Join(missing, ", ")
			return status, nil
		}
		if len(databases) == 0 {
			status.State = v2.LogicalReplicaStateBootstrapping
			status.Reason = v2.LogicalReplicaReasonWaitingForDatabases
			status.Message = "the cluster has no databases to replicate; create one, or name the " +
				"databases this replica covers in spec.logicalReplicas[].databases"
			return status, nil
		}

		status.Databases = databases
		status.State = v2.LogicalReplicaStateBootstrapping

		// Persist the resolved list before anything acts on it.
		return status, nil
	}

	ready, err := r.reconcileLogicalReplicaPVC(ctx, cr, spec)
	if err != nil {
		return status, errors.Wrap(err, "reconcile pvc")
	}
	if !ready {
		// See reconcileLogicalReplicaPVC: the claim of an earlier incarnation is
		// still on its way out.
		status.State = v2.LogicalReplicaStateBootstrapping
		status.Reason = v2.LogicalReplicaReasonWaitingForDataVolume
		status.Message = "waiting for the " + logicalReplicaPVCName(cr, spec.Name) + " volume to be deleted"
		return status, nil
	}

	// The ConfigMap has to exist before the bootstrap Job: pg_createsubscriber
	// runs the target server with it, which is what keeps the inherited
	// pgBackRest archive_command from firing during the conversion.
	if err := r.reconcileLogicalReplicaConfigMap(ctx, cr, crunchyCR, spec, status); err != nil {
		return status, errors.Wrap(err, "reconcile configmap")
	}

	if status.SeededAt == nil {
		bootstrapped, err := r.reconcileLogicalReplicaBootstrap(ctx, cr, crunchyCR, spec, status)
		if err != nil {
			return status, err
		}
		if !bootstrapped {
			return status, nil
		}

		if status.SeededAt == nil {
			status.SeededAt = new(metav1.Now())
		}
		// The data directory is named after the major version, see
		// postgres.DataDirectory. Record which one this volume holds.
		status.PostgresVersion = cr.Spec.PostgresVersion

		log.Info("logical replica bootstrapped", "databases", status.Databases,
			"seededAt", status.SeededAt, "postgresVersion", status.PostgresVersion)
	}

	// Asserts one replica, which is also what starts one again after a restore
	// that was abandoned before it replaced the data directory.
	if err := r.reconcileLogicalReplicaStatefulSet(ctx, cr, crunchyCR, spec, status); err != nil {
		return status, errors.Wrap(err, "reconcile statefulset")
	}
	if err := r.reconcileLogicalReplicaService(ctx, cr, spec); err != nil {
		return status, errors.Wrap(err, "reconcile service")
	}

	return r.checkLogicalReplicaHealth(ctx, cr, spec, status)
}

// execOnPrimary runs sql on the primary and returns its trimmed output.
func (r *PGClusterReconciler) execOnPrimary(ctx context.Context, cr *v2.PerconaPGCluster, database, sql string) (string, error) {
	primary, err := perconaPG.GetPrimaryPod(ctx, r.Client, cr)
	if err != nil {
		return "", errors.Wrap(ErrPrimaryPodNotFound, err.Error())
	}

	return r.execOnPod(ctx, primary, database, sql)
}

// resolveLogicalReplicaDatabases returns the databases the replica covers, and
// the ones it was told to cover that do not exist on the primary. An empty
// spec.databases means every database a user could connect to.
func (r *PGClusterReconciler) resolveLogicalReplicaDatabases(
	ctx context.Context, cr *v2.PerconaPGCluster, spec *v2.LogicalReplicaSpec,
) (databases, missing []string, err error) {
	primary, err := perconaPG.GetPrimaryPod(ctx, r.Client, cr)
	if err != nil {
		return nil, nil, errors.Wrap(ErrPrimaryPodNotFound, err.Error())
	}

	if len(spec.Databases) == 0 {
		databases, err = r.queryDatabases(ctx, primary,
			`datallowconn AND NOT datistemplate AND datname <> 'postgres'`)

		return databases, nil, err
	}

	present, err := r.presentDatabases(ctx, primary)
	if err != nil {
		return nil, nil, err
	}

	databases = make([]string, 0, len(spec.Databases))
	for _, db := range spec.Databases {
		databases = append(databases, string(db))
		if !slices.Contains(present, string(db)) {
			missing = append(missing, string(db))
		}
	}

	return databases, missing, nil
}

// checkPrimaryCapacity verifies the primary can host the replication slots and
// WAL senders this replica needs.
func (r *PGClusterReconciler) checkPrimaryCapacity(
	ctx context.Context, cr *v2.PerconaPGCluster, neededSlots, neededSenders int,
) error {
	const sql = `SELECT current_setting('max_replication_slots')::int - (SELECT count(*) FROM pg_catalog.pg_replication_slots), ` +
		`current_setting('max_wal_senders')::int - (SELECT count(*) FROM pg_catalog.pg_stat_replication);`

	stdout, err := r.execOnPrimary(ctx, cr, "", sql)
	if err != nil {
		return err
	}

	// One entry per column of the query above, in order.
	limits := []struct {
		noun, parameter string
		needed          int
	}{
		{"replication slots", "max_replication_slots", neededSlots},
		{"WAL senders", "max_wal_senders", neededSenders},
	}

	fields := strings.Split(stdout, "|")
	if len(fields) != len(limits) {
		return errors.Errorf("unexpected capacity query output: %q", stdout)
	}

	for i, limit := range limits {
		free, err := strconv.Atoi(strings.TrimSpace(fields[i]))
		if err != nil {
			return errors.Wrapf(err, "parse free %s: %q", limit.noun, fields[i])
		}
		if free < limit.needed {
			return errors.Errorf(
				"primary has %d free %s but %d are needed; raise %s via spec.patroni.dynamicConfiguration",
				free, limit.noun, limit.needed, limit.parameter)
		}
	}

	return nil
}

// logicalReplicaCapacity returns how many replication slots and WAL senders the
// primary has to have free before this replica can be bootstrapped.
func logicalReplicaCapacity(spec *v2.LogicalReplicaSpec, databases int) (slots, senders int) {
	slots, senders = databases, databases

	if spec.BootstrapMethodOrDefault() == v2.LogicalReplicaBootstrapMethodPGBaseBackup {
		// "--wal-method=stream" holds two WAL senders and a temporary slot at
		// once. The primary drops all of it well before the per-database logical
		// slots are created, so this is a floor rather than an addition.
		slots, senders = max(slots, 1), max(senders, 2)
	}

	return slots, senders
}

// reconcileLogicalReplicaPVC creates or updates the data volume of a replica
// and reports whether it is usable.
func (r *PGClusterReconciler) reconcileLogicalReplicaPVC(
	ctx context.Context, cr *v2.PerconaPGCluster, spec *v2.LogicalReplicaSpec,
) (bool, error) {
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      logicalReplicaPVCName(cr, spec.Name),
			Namespace: cr.Namespace,
		},
	}

	// A claim on its way out must not be adopted: CreateOrUpdate would update it
	// and report success, and the bootstrap Job would then mount a volume that
	// disappears from under it - or the old data, which the Job refuses to seed
	// over. A replica removed from the spec and added straight back, or a
	// canceled bootstrap, opens that window.
	existing := &corev1.PersistentVolumeClaim{}
	switch err := r.Client.Get(ctx, client.ObjectKeyFromObject(pvc), existing); {
	case err == nil && existing.DeletionTimestamp != nil:
		return false, nil
	case err != nil && !apierrors.IsNotFound(err):
		return false, errors.Wrap(err, "get pvc")
	}

	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, pvc, func() error {
		if pvc.CreationTimestamp.IsZero() {
			// The claim spec is immutable apart from resources, so it is only
			// set on creation.
			pvc.Spec = spec.DataVolumeClaimSpec
		} else {
			pvc.Spec.Resources = spec.DataVolumeClaimSpec.Resources
		}
		pvc.Labels = logicalReplicaLabels(cr, spec.Name)

		return controllerutil.SetControllerReference(cr, pvc, r.Client.Scheme())
	})

	return err == nil, errors.Wrap(err, "create or update pvc")
}

// reconcileLogicalReplicaBootstrap runs the one-shot Job that seeds the data
// volume from the primary and converts it into a logical subscriber. It reports
// whether the conversion has completed.
func (r *PGClusterReconciler) reconcileLogicalReplicaBootstrap(
	ctx context.Context, cr *v2.PerconaPGCluster, crunchyCR *v1beta1.PostgresCluster, spec *v2.LogicalReplicaSpec, status *v2.LogicalReplicaStatus,
) (bool, error) {
	job := &batchv1.Job{}
	key := client.ObjectKey{Name: logicalReplicaJobName(cr, spec.Name), Namespace: cr.Namespace}

	err := r.Client.Get(ctx, key, job)
	switch {
	case err != nil && !apierrors.IsNotFound(err):
		return false, errors.Wrap(err, "get bootstrap job")

	case err == nil && perconaController.JobCompleted(job):
		// The Job holds the data volume open; drop it so the StatefulSet can
		// take over.
		return true, r.deleteLogicalReplicaJob(ctx, cr, spec.Name)

	case err == nil && perconaController.JobFailed(job):
		status.State = v2.LogicalReplicaStateBroken
		status.Reason = v2.LogicalReplicaReasonBootstrapFailed
		status.Message = "bootstrap job failed: " + jobFailedMessage(job) +
			"; inspect the " + job.Name + " job, then delete the logical replica and recreate it"
		return false, nil

	case err == nil:
		status.State = v2.LogicalReplicaStateBootstrapping
		return false, nil
	}

	// No Job, and as far as the status is concerned this replica has never been
	// bootstrapped. A StatefulSet says otherwise and is the more trustworthy of
	// the two: it only ever exists after a completed bootstrap, and it outlives
	// the Job. Seeding again would run over a replica that is already
	// replicating.
	sts, err := r.logicalReplicaStatefulSet(ctx, cr, spec.Name)
	if err != nil {
		return false, err
	}
	if sts != nil {
		logging.FromContext(ctx).Info(
			"logical replica has a StatefulSet but no record of being bootstrapped; adopting it rather than seeding again",
			"statefulSet", sts.Name)

		status.SeededAt = sts.CreationTimestamp.DeepCopy()
		return true, nil
	}

	// Fail fast if the primary cannot host the slots we need, rather than after
	// a full base backup.
	neededSlots, neededSenders := logicalReplicaCapacity(spec, len(status.Databases))
	if err := r.checkPrimaryCapacity(ctx, cr, neededSlots, neededSenders); err != nil {
		status.State = v2.LogicalReplicaStateBroken
		status.Message = err.Error()
		// Only the user can raise these settings, so retrying with backoff would
		// never make it true.
		//nolint:nilerr
		return false, nil
	}

	job, err = r.generateLogicalReplicaBootstrapJob(ctx, cr, crunchyCR, spec, status)
	if err != nil {
		return false, errors.Wrap(err, "generate bootstrap job")
	}
	if err := r.Client.Create(ctx, job); err != nil {
		return false, errors.Wrap(err, "create bootstrap job")
	}

	status.State = v2.LogicalReplicaStateBootstrapping
	return false, nil
}

// deleteLogicalReplicaJob deletes the bootstrap Job of a replica. Foreground, so
// the pod is gone before the claim it holds can be deleted.
func (r *PGClusterReconciler) deleteLogicalReplicaJob(
	ctx context.Context, cr *v2.PerconaPGCluster, replica string,
) error {
	job := &batchv1.Job{ObjectMeta: metav1.ObjectMeta{
		Name:      logicalReplicaJobName(cr, replica),
		Namespace: cr.Namespace,
	}}

	err := r.Client.Delete(ctx, job, &client.DeleteOptions{
		PropagationPolicy: new(metav1.DeletePropagationForeground),
	})

	return errors.Wrap(client.IgnoreNotFound(err), "delete bootstrap job")
}

// logicalReplicaStatefulSet returns the StatefulSet of a replica, or nil when
// there is none. One only ever exists after a completed bootstrap.
func (r *PGClusterReconciler) logicalReplicaStatefulSet(
	ctx context.Context, cr *v2.PerconaPGCluster, replica string,
) (*appsv1.StatefulSet, error) {
	sts := &appsv1.StatefulSet{}
	key := client.ObjectKey{Name: logicalReplicaObjectName(cr, replica), Namespace: cr.Namespace}

	switch err := r.Client.Get(ctx, key, sts); {
	case apierrors.IsNotFound(err):
		return nil, nil
	case err != nil:
		return nil, errors.Wrap(err, "get statefulset")
	}

	return sts, nil
}

// jobFailedMessage returns the message Kubernetes recorded on the JobFailed
// condition, which says whether the Job hit its backoff limit or its deadline.
func jobFailedMessage(job *batchv1.Job) string {
	for i := range job.Status.Conditions {
		if job.Status.Conditions[i].Type == batchv1.JobFailed {
			return job.Status.Conditions[i].Message
		}
	}

	return ""
}

// logicalReplicaBootstrapScript is the body of the bootstrap Job: seed a
// physical standby with logicalReplicaSeedCommand, then convert it.
//
// It never starts Postgres before the conversion. Finishing recovery here, the
// way the in-place restore Job does, ends recovery and promotes, and
// pg_createsubscriber refuses a target that is no longer a standby.
func logicalReplicaBootstrapScript(
	dataDir, primaryHost string, port int32, databases []string, replica, seed string,
) string {
	args := []string{
		"  pg_createsubscriber",
		"--verbose",
		"--pgdata=" + shellQuote(dataDir),
		"--publisher-server=\"${PUBLISHER_CONNINFO}\"",
		// Where the bootstrap config below tells the target to put its socket.
		"--socketdir=" + shellQuote(postgres.SocketDirectory),
		"--recovery-timeout=" + strconv.Itoa(logicalReplicaRecoveryTimeout),
		// Otherwise the target runs with the postgresql.conf the seed brought
		// back from the primary, which carries archive_mode=on and the pgBackRest
		// archive_command: the conversion promotes before it resets the system
		// identifier, so the target would archive WAL into the source cluster's
		// stanza on a diverged timeline. It is also how the target gets the slot
		// and worker settings the conversion checks for.
		"--config-file=" + shellQuote(logicalReplicaConfigMountPath+"/"+logicalReplicaBootstrapConfigFile),
	}

	for _, db := range databases {
		args = append(args,
			"--database="+shellQuote(db),
			"--publication="+logicalreplica.PublicationName(replica, db),
			"--subscription="+logicalreplica.SubscriptionName(replica, db),
			"--replication-slot="+logicalreplica.SlotName(replica, db),
		)
	}
	args = append(args, `"$@"`)

	return strings.Join([]string{
		`set -euo pipefail`,
		``,
		// Carries no credential, and must not grow one: pg_createsubscriber
		// stores it verbatim in pg_subscription.subconninfo. The password comes
		// from PGPASSWORD instead - see logicalReplicaEnvironment.
		`PUBLISHER_CONNINFO="host=` + primaryHost + ` port=` + strconv.Itoa(int(port)) +
			` user=` + logicalreplica.ReplicationUser + ` dbname=postgres sslmode=verify-ca sslrootcert=` +
			naming.CertMountPath + `/ca.crt"`,
		``,
		`createsubscriber() {`,
		strings.Join(args, " \\\n    "),
		`}`,
		``,
		`if [ -s ` + shellQuote(path.Join(dataDir, "PG_VERSION")) + ` ]; then`,
		`  echo "data directory is not empty, refusing to seed over it" >&2`,
		`  exit 1`,
		`fi`,
		``,
		`install --directory --mode=0700 ` + shellQuote(dataDir),
		``,
		// Postgres creates its socket but not the directory holding it, and the
		// Job mounts an empty volume over /tmp.
		`install --directory --mode=0700 ` + shellQuote(postgres.SocketDirectory),
		``,
		seed,
		``,
		// On the archive alone the target can only replay as far as the last
		// segment the primary happened to push, so it would sit out the whole
		// --recovery-timeout waiting for the one holding the conversion LSN.
		// Streaming closes that gap; a pg_basebackup seed has no restore_command
		// at all, so it is the only way that one ever catches up.
		//
		// Postgres reads postgresql.auto.conf whatever --config-file says, so this
		// survives into the conversion.
		`printf >> ` + shellQuote(path.Join(dataDir, "postgresql.auto.conf")) +
			` "primary_conninfo = '%s'\n" "${PUBLISHER_CONNINFO}"`,
		``,
		// Ignore any Patroni settings present in the backup: this replica is
		// configured from the operator-rendered ConfigMap instead.
		`rm -f ` + shellQuote(path.Join(dataDir, "patroni.dynamic.json")),
		``,
		`echo "validating prerequisites"`,
		`createsubscriber --dry-run`,
		``,
		`echo "converting to a logical replica"`,
		`createsubscriber`,
		``,
		`echo "disabling the new subscriptions on their first apply error"`,
		logicalReplicaDisableOnErrorCommand(dataDir, replica, databases),
		``,
		`echo "done"`,
	}, "\n")
}

// logicalReplicaDisableOnErrorCommand returns the shell command that sets
// "disable_on_error" on every subscription the conversion has just created.
func logicalReplicaDisableOnErrorCommand(dataDir, replica string, databases []string) string {
	options := []string{
		// Same reason pg_createsubscriber is given this file above.
		"-c config_file=" + logicalReplicaConfigMountPath + "/" + logicalReplicaBootstrapConfigFile,
		// The Job's pod carries the labels the replica's Service selects on, so
		// the config file's "listen_addresses = *" would put a half-finished
		// replica behind that Service for as long as this takes.
		"-c listen_addresses=''",
		// An apply worker started here would apply without the setting these
		// statements are here to make.
		"-c max_logical_replication_workers=0",
	}

	timeout := " --timeout=" + strconv.Itoa(logicalReplicaPGCtlTimeout)

	lines := []string{
		`pg_ctl start --wait` + timeout + ` --pgdata=` + shellQuote(dataDir) +
			` --options="` + strings.Join(options, " ") + `"`,
	}

	// A subscription only exists in the database it replicates, so each one takes
	// its own connection, over the local socket that pg_createsubscriber used too.
	for _, db := range databases {
		lines = append(lines, `psql --no-psqlrc --set=ON_ERROR_STOP=on`+
			` --dbname=`+shellQuote(db)+
			` --command=`+shellQuote(logicalreplica.DisableOnErrorSQL(replica, db)))
	}

	// Fast: nothing else is connected, and this leaves the data directory as
	// cleanly shut down as the conversion found it, so the replica's postmaster
	// starts without recovery.
	return strings.Join(append(lines,
		`pg_ctl stop --wait`+timeout+` --mode=fast --pgdata=`+shellQuote(dataDir)), "\n")
}

// logicalReplicaSeedCommand returns the shell command that fills the data
// directory with a physical copy of the primary. Whichever method produces it
// has to leave a directory that is still a standby: pg_createsubscriber converts
// nothing else.
func logicalReplicaSeedCommand(
	crunchyCR *v1beta1.PostgresCluster, spec *v2.LogicalReplicaSpec, dataDir string,
) (string, error) {
	switch method := spec.BootstrapMethodOrDefault(); method {
	case v2.LogicalReplicaBootstrapMethodPGBaseBackup:
		return logicalReplicaBaseBackupCommand(crunchyCR, dataDir), nil

	case v2.LogicalReplicaBootstrapMethodPGBackRest:
		return logicalReplicaRestoreCommand(crunchyCR, dataDir)

	default:
		return "", errors.Errorf("unknown logical replica bootstrap method %q", method)
	}
}

// logicalReplicaRestoreCommand seeds the volume from the cluster's own backups.
func logicalReplicaRestoreCommand(crunchyCR *v1beta1.PostgresCluster, dataDir string) (string, error) {
	opts, err := logicalReplicaRestoreOptions(crunchyCR, dataDir)
	if err != nil {
		return "", err
	}

	return `echo "restoring the data directory from pgBackRest"` + "\n" +
		"pgbackrest restore " + strings.Join(opts, " "), nil
}

// logicalReplicaBaseBackupCommand seeds the volume straight from the primary,
// which is the only way to do it when the cluster keeps no backups.
func logicalReplicaBaseBackupCommand(crunchyCR *v1beta1.PostgresCluster, dataDir string) string {
	// Patroni is told the same thing when it creates a physical replica: plain
	// pg_basebackup does not understand what pg_tde encrypts.
	command := "pg_basebackup"
	if crunchyCR.Spec.Extensions.PGTDE.Enabled {
		command = "pg_tde_basebackup"
	}

	args := []string{
		command,
		// pg_basebackup drops dbname and forces replication=true, so this is
		// matched by the primary's hostssl replication rule.
		`--dbname="${PUBLISHER_CONNINFO}"`,
		"--pgdata=" + shellQuote(dataDir),
		// Ships the WAL written while the backup runs. Unlike a pgBackRest restore
		// this leaves no restore_command behind.
		"--wal-method=stream",
		// Otherwise the backup waits out a spread checkpoint before copying a
		// single byte.
		"--checkpoint=fast",
		"--no-password",
		// The only diagnostic for a seed that takes hours.
		"--verbose",
		"--progress",
	}

	// No --write-recovery-conf: it derives primary_conninfo from libpq, which has
	// resolved PGPASSWORD by then, so it would write the password into
	// postgresql.auto.conf in plain text. The script appends its own conninfo.
	//
	// No --slot: without one "--wal-method=stream" uses a temporary slot that the
	// primary drops with the connection, whereas a named slot would survive a
	// failed Job and pin WAL for good.
	return `echo "streaming a base backup from the primary"` + "\n" +
		strings.Join(args, " \\\n    ") + "\n\n" +
		// What "pgbackrest restore --type=standby" writes on the other path.
		`touch ` + shellQuote(path.Join(dataDir, "standby.signal"))
}

// logicalReplicaRestoreOptions returns the pgBackRest options that seed a
// logical replica's volume from the cluster's own backups.
func logicalReplicaRestoreOptions(crunchyCR *v1beta1.PostgresCluster, dataDir string) ([]string, error) {
	repos := crunchyCR.Spec.Backups.PGBackRest.Repos
	if len(repos) == 0 {
		return nil, errors.New("cluster has no pgBackRest repository to restore from; " +
			"configure spec.backups.pgbackrest.repos or set " +
			"spec.logicalReplicas[].bootstrapMethod to pg_basebackup")
	}

	opts := []string{
		"--stanza=" + pgbackrest.DefaultStanzaName,
		"--pg1-path=" + dataDir,
		"--repo=" + strings.TrimPrefix(repos[0].Name, "repo"),

		// pg_createsubscriber converts a target that is still in recovery, and
		// this is what writes standby.signal.
		"--type=standby",
		"--log-level-file=off",
		"--log-level-console=info",
		"--log-level-stderr=info",
	}

	// A logical replica keeps its WAL on the data volume, so pg_wal has to be
	// remapped when the instance the backup came from kept its own elsewhere.
	for i := range crunchyCR.Spec.InstanceSets {
		if crunchyCR.Spec.InstanceSets[i].WALVolumeClaimSpec != nil {
			opts = append(opts, "--link-map=pg_wal="+path.Join(dataDir, "pg_wal"))
			break
		}
	}

	return opts, nil
}

// shellQuote wraps s in single quotes for safe interpolation into the bootstrap
// script: database names may contain almost anything.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func (r *PGClusterReconciler) generateLogicalReplicaBootstrapJob(
	ctx context.Context, cr *v2.PerconaPGCluster, crunchyCR *v1beta1.PostgresCluster, spec *v2.LogicalReplicaSpec, status *v2.LogicalReplicaStatus,
) (*batchv1.Job, error) {
	dataDir := postgres.DataDirectory(crunchyCR)
	primaryHost := naming.ClusterPrimaryService(crunchyCR).Name + "." + cr.Namespace + ".svc"

	seed, err := logicalReplicaSeedCommand(crunchyCR, spec, dataDir)
	if err != nil {
		return nil, err
	}

	script := logicalReplicaBootstrapScript(
		dataDir, primaryHost, *cr.Spec.Port, status.Databases, spec.Name, seed)

	initImage, err := k8s.InitImage(ctx, r.Client, crunchyCR, nil)
	if err != nil {
		return nil, errors.Wrap(err, "get init image")
	}

	container := corev1.Container{
		Name:            logicalReplicaBootstrapContainer,
		Image:           cr.PostgresImage(),
		ImagePullPolicy: cr.Spec.ImagePullPolicy,
		Command:         []string{"bash", "-c", script},
		Env: append(logicalReplicaEnvironment(cr, dataDir),
			postgrescluster.NSSWrapperPostgresEnv()...),
		Resources:       spec.Resources,
		SecurityContext: initialize.RestrictedSecurityContext(true),
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      "tmp",
				MountPath: "/tmp",
			},
			logicalReplicaCertVolumeMount(),
			postgres.DataVolumeMount(),
			{
				Name:      pNaming.CrunchyBinVolumeName,
				MountPath: pNaming.CrunchyBinVolumePath,
			},
			logicalReplicaConfigVolumeMount(),
		},
	}

	// Follows the repository, not the bootstrap method: the restore_command in the
	// inherited postgresql.conf is a useful WAL fallback either way, and a
	// non-optional projection with no ConfigMap leaves the Job unschedulable.
	hasPGBackRestRepo := len(crunchyCR.Spec.Backups.PGBackRest.Repos) > 0
	if hasPGBackRestRepo {
		container.VolumeMounts = append(container.VolumeMounts, pgbackrest.ConfigVolumeMount())
	}

	initContainer := k8s.InitContainer(
		crunchyCR,
		naming.ContainerDatabase,
		initImage,
		cr.Spec.ImagePullPolicy,
		initialize.RestrictedSecurityContext(true),
		container.Resources,
		nil)

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      logicalReplicaJobName(cr, spec.Name),
			Namespace: cr.Namespace,
			Labels:    logicalReplicaLabels(cr, spec.Name),
		},
		Spec: batchv1.JobSpec{
			BackoffLimit: new(int32(0)),
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: logicalReplicaLabels(cr, spec.Name),
				},
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyNever,
					InitContainers: []corev1.Container{
						initContainer,
						logicalReplicaNSSWrapperInitContainer(cr, container.Resources),
					},
					Containers:                   []corev1.Container{container},
					Volumes:                      logicalReplicaVolumes(cr, crunchyCR, spec),
					SecurityContext:              postgres.PodSecurityContext(crunchyCR),
					ImagePullSecrets:             cr.Spec.ImagePullSecrets,
					Affinity:                     spec.Affinity,
					Tolerations:                  spec.Tolerations,
					AutomountServiceAccountToken: new(false),
					EnableServiceLinks:           new(false),
				},
			},
		},
	}
	if spec.PriorityClassName != nil {
		job.Spec.Template.Spec.PriorityClassName = *spec.PriorityClassName
	}

	// The Job restores from the cluster's own repository, so it is both clusters
	// here. Gated with the mount above: a mount with no volume makes the Pod
	// invalid.
	if hasPGBackRestRepo {
		pgbackrest.AddConfigToRestorePod(crunchyCR, crunchyCR, &job.Spec.Template.Spec)
	}

	return job, controllerutil.SetControllerReference(cr, job, r.Client.Scheme())
}

func logicalReplicaCertVolumeMount() corev1.VolumeMount {
	return corev1.VolumeMount{
		Name:      naming.CertVolume,
		MountPath: naming.CertMountPath,
		ReadOnly:  true,
	}
}

func logicalReplicaConfigVolumeMount() corev1.VolumeMount {
	return corev1.VolumeMount{
		Name:      logicalReplicaConfigVolume,
		MountPath: logicalReplicaConfigMountPath,
		ReadOnly:  true,
	}
}

func defaultProbe(delay, period, failureThreshold, successThreshold int32) *corev1.Probe {
	return &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			Exec: &corev1.ExecAction{
				Command: []string{"pg_isready", "-h", postgres.SocketDirectory},
			},
		},
		InitialDelaySeconds: delay,
		PeriodSeconds:       period,
		FailureThreshold:    failureThreshold,
		SuccessThreshold:    successThreshold,
	}
}

func overrideProbe(probe, override *corev1.Probe) *corev1.Probe {
	if override == nil {
		return probe
	}

	if override.InitialDelaySeconds != 0 {
		probe.InitialDelaySeconds = override.InitialDelaySeconds
	}
	if override.PeriodSeconds != 0 {
		probe.PeriodSeconds = override.PeriodSeconds
	}
	if override.FailureThreshold != 0 {
		probe.FailureThreshold = override.FailureThreshold
	}
	if override.SuccessThreshold != 0 {
		probe.SuccessThreshold = override.SuccessThreshold
	}
	if override.TimeoutSeconds != 0 {
		probe.TimeoutSeconds = override.TimeoutSeconds
	}
	if override.Exec != nil {
		probe.Exec = override.Exec
	}

	return probe
}

func startupProbe(probe *corev1.Probe) *corev1.Probe {
	return overrideProbe(defaultProbe(5, 5, 30, 1), probe)
}

func livenessProbe(probe *corev1.Probe) *corev1.Probe {
	return overrideProbe(defaultProbe(30, 20, 6, 1), probe)
}

func readinessProbe(replica string, databases []string, probe *corev1.Probe) *corev1.Probe {
	readiness := defaultProbe(0, 5, 2, 1)

	// The StatefulSet is only reconciled once the replica is seeded, so there is
	// always at least one database here. An empty "IN ()" list is a syntax error.
	if len(databases) == 0 {
		return overrideProbe(readiness, probe)
	}

	readiness.Exec.Command = []string{"bash", "-c", strings.Join([]string{
		`set -euo pipefail`,
		`pg_isready -q -h ` + shellQuote(postgres.SocketDirectory),
		`psql -XAtqw --command=` +
			shellQuote(logicalreplica.SubscriptionsEnabledQuery(replica, databases)) +
			` | grep -qx t`,
	}, "\n")}

	return overrideProbe(readiness, probe)
}

// logicalReplicaNSSWrapperInitContainer writes the passwd and group files that
// let the Pod resolve its user ID to "postgres". OpenShift assigns an arbitrary
// UID and CRI-O names its passwd entry after the number, so without this both
// libpq and the backend's "peer" check see that number, and the
// `local all "postgres" peer` rule the seed brought back from the primary never
// matches.
func logicalReplicaNSSWrapperInitContainer(
	cr *v2.PerconaPGCluster, resources corev1.ResourceRequirements,
) corev1.Container {
	return corev1.Container{
		Name:            naming.ContainerNSSWrapperInit,
		Image:           cr.PostgresImage(),
		ImagePullPolicy: cr.Spec.ImagePullPolicy,
		Command:         []string{"bash", "-c", postgrescluster.NSSWrapperPostgresCommand()},
		Resources:       resources,
		SecurityContext: initialize.RestrictedSecurityContext(true),
		VolumeMounts:    []corev1.VolumeMount{{Name: "tmp", MountPath: "/tmp"}},
	}
}

// logicalReplicaEnvironment returns the environment shared by the bootstrap Job
// and the StatefulSet.
func logicalReplicaEnvironment(cr *v2.PerconaPGCluster, dataDir string) []corev1.EnvVar {
	return []corev1.EnvVar{
		{Name: "PGDATA", Value: dataDir},
		{Name: "PGHOST", Value: postgres.SocketDirectory},
		{Name: "PGPORT", Value: strconv.Itoa(int(*cr.Spec.Port))},
		{Name: "PGPASSWORD",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: logicalReplicaUserSecretName(cr),
					},
					Key: "password",
				},
			}},
	}
}

// logicalReplicaVolumes returns the volumes shared by the bootstrap Job and the StatefulSet.
func logicalReplicaVolumes(
	cr *v2.PerconaPGCluster, crunchyCR *v1beta1.PostgresCluster, spec *v2.LogicalReplicaSpec,
) []corev1.Volume {
	certSecret := naming.PostgresTLSSecret(crunchyCR).Name
	if cr.Spec.Secrets.CustomTLSSecret != nil && cr.Spec.Secrets.CustomTLSSecret.Name != "" {
		certSecret = cr.Spec.Secrets.CustomTLSSecret.Name
	}

	return []corev1.Volume{
		{
			Name: naming.CertVolume,
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: certSecret,
					// PostgreSQL refuses to start when the server key is
					// readable by anyone else.
					DefaultMode: new(int32(0o600)),
				},
			},
		},
		{
			Name: postgres.DataVolumeMount().Name,
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: logicalReplicaPVCName(cr, spec.Name),
				},
			},
		},
		{
			Name: "tmp",
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{
					Medium: corev1.StorageMediumMemory,
				},
			},
		},
		{
			Name: pNaming.CrunchyBinVolumeName,
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		},
		{
			Name: logicalReplicaConfigVolume,
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: logicalReplicaConfigMapName(cr, spec.Name),
					},
				},
			},
		},
	}
}

// logicalReplicaPostgresConfig renders the postgresql.conf of a logical replica.
func logicalReplicaPostgresConfig(cr *v2.PerconaPGCluster, crunchyCR *v1beta1.PostgresCluster, databases int, readOnly bool) string {
	inherited := path.Join(postgres.DataDirectory(crunchyCR), "postgresql.conf")

	lines := []string{
		"# Generated by percona-postgresql-operator. Do not edit.",
		"",
		"# The primary's own configuration, as it came back from the backup.",
		"# Settings below this line override it.",
		fmt.Sprintf("include_if_exists '%s'", inherited),
		"",
		"listen_addresses = '*'",
		fmt.Sprintf("port = %d", *cr.Spec.Port),
		fmt.Sprintf("unix_socket_directories = '%s'", postgres.SocketDirectory),
		"",
		"ssl = on",
		fmt.Sprintf("ssl_cert_file = '%s/tls.crt'", naming.CertMountPath),
		fmt.Sprintf("ssl_key_file = '%s/tls.key'", naming.CertMountPath),
		fmt.Sprintf("ssl_ca_file = '%s/ca.crt'", naming.CertMountPath),
		"",
		"# Without this the replica would inherit the pgBackRest archive_command and",
		"# push WAL from a diverged timeline into the source cluster's stanza.",
		"archive_mode = off",
		"archive_command = ''",
		"",
		"# One apply worker and one origin per subscribed database.",
		fmt.Sprintf("max_replication_slots = %d", databases),
		fmt.Sprintf("max_logical_replication_workers = %d", databases),
		fmt.Sprintf("max_worker_processes = %d", databases+logicalReplicaWorkerHeadroom),
		"",
		"password_encryption = 'scram-sha-256'",
	}

	if readOnly {
		lines = append(lines,
			"",
			"# Writing to a logical replica diverges it from the primary, and the",
			"# first conflicting row breaks apply for good. Replication itself keeps",
			"# working: this only disallows SQL commands, which the apply worker",
			"# bypasses.",
			"default_transaction_read_only = on",
		)
	}

	return strings.Join(lines, "\n") + "\n"
}

func (r *PGClusterReconciler) reconcileLogicalReplicaConfigMap(
	ctx context.Context, cr *v2.PerconaPGCluster, crunchyCR *v1beta1.PostgresCluster, spec *v2.LogicalReplicaSpec, status *v2.LogicalReplicaStatus,
) error {
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      logicalReplicaConfigMapName(cr, spec.Name),
			Namespace: cr.Namespace,
		},
	}

	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, cm, func() error {
		cm.Labels = logicalReplicaLabels(cr, spec.Name)
		cm.Data = map[string]string{
			logicalReplicaConfigFile:          logicalReplicaPostgresConfig(cr, crunchyCR, len(status.Databases), true /* read only */),
			logicalReplicaBootstrapConfigFile: logicalReplicaPostgresConfig(cr, crunchyCR, len(status.Databases), false /* read only */),
		}

		return controllerutil.SetControllerReference(cr, cm, r.Client.Scheme())
	})

	return errors.Wrap(err, "create or update configmap")
}

// reconcileLogicalReplicaStatefulSet renders the StatefulSet that runs the
// replica. Stopping one goes through scaleLogicalReplica instead.
func (r *PGClusterReconciler) reconcileLogicalReplicaStatefulSet(
	ctx context.Context, cr *v2.PerconaPGCluster, crunchyCR *v1beta1.PostgresCluster,
	spec *v2.LogicalReplicaSpec, status *v2.LogicalReplicaStatus,
) error {
	name := logicalReplicaObjectName(cr, spec.Name)
	dataDir := postgres.DataDirectory(crunchyCR)

	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: cr.Namespace},
	}

	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, sts, func() error {
		sts.Labels = logicalReplicaLabels(cr, spec.Name)
		sts.Spec.Replicas = new(int32(1))
		sts.Spec.ServiceName = name
		sts.Spec.Selector = &metav1.LabelSelector{MatchLabels: logicalReplicaSelector(cr, spec.Name)}
		sts.Spec.Template.Labels = naming.Merge(
			logicalReplicaLabels(cr, spec.Name), spec.Metadata.GetLabelsOrNil())
		sts.Spec.Template.Annotations = spec.Metadata.GetAnnotationsOrNil()

		container := corev1.Container{
			Name:            naming.ContainerDatabase,
			Image:           cr.PostgresImage(),
			ImagePullPolicy: cr.Spec.ImagePullPolicy,
			Command: []string{
				"postgres",
				"-D", dataDir,
				"-c", "config_file=" + logicalReplicaConfigMountPath + "/" + logicalReplicaConfigFile,
			},
			Env: append(logicalReplicaEnvironment(cr, dataDir),
				postgrescluster.NSSWrapperPostgresEnv()...),
			Resources: spec.Resources,
			Ports: []corev1.ContainerPort{{
				Name:          naming.PortPostgreSQL,
				ContainerPort: *cr.Spec.Port,
				Protocol:      corev1.ProtocolTCP,
			}},
			SecurityContext: initialize.RestrictedSecurityContext(true),
			VolumeMounts: []corev1.VolumeMount{
				logicalReplicaCertVolumeMount(),
				postgres.DataVolumeMount(),
				{Name: "tmp", MountPath: "/tmp"},
				{Name: logicalReplicaSocketVolume, MountPath: postgres.SocketDirectory},
				logicalReplicaConfigVolumeMount(),
			},
			StartupProbe:   startupProbe(spec.StartupProbe),
			LivenessProbe:  livenessProbe(spec.LivenessProbe),
			ReadinessProbe: readinessProbe(spec.Name, status.Databases, spec.ReadinessProbe),
		}

		sts.Spec.Template.Spec.InitContainers = []corev1.Container{
			logicalReplicaNSSWrapperInitContainer(cr, container.Resources),
		}
		sts.Spec.Template.Spec.Containers = []corev1.Container{container}
		sts.Spec.Template.Spec.Volumes = append(
			logicalReplicaVolumes(cr, crunchyCR, spec),
			corev1.Volume{
				Name: logicalReplicaSocketVolume,
				VolumeSource: corev1.VolumeSource{
					EmptyDir: &corev1.EmptyDirVolumeSource{
						Medium: corev1.StorageMediumMemory,
					},
				},
			})
		sts.Spec.Template.Spec.SecurityContext = postgres.PodSecurityContext(crunchyCR)
		sts.Spec.Template.Spec.ImagePullSecrets = cr.Spec.ImagePullSecrets
		sts.Spec.Template.Spec.Affinity = spec.Affinity
		sts.Spec.Template.Spec.Tolerations = spec.Tolerations
		sts.Spec.Template.Spec.EnableServiceLinks = new(false)
		if spec.PriorityClassName != nil {
			sts.Spec.Template.Spec.PriorityClassName = *spec.PriorityClassName
		}

		return controllerutil.SetControllerReference(cr, sts, r.Client.Scheme())
	})

	return errors.Wrap(err, "create or update statefulset")
}

// removeStaleExternalDNSAnnotations drops the external-dns annotations the
// operator wrote itself, so that the ones the CR no longer asks for are gone
// after they are rewritten from the spec. It is only needed here: every other
// service is written with server-side apply, which prunes what the operator
// stops emitting on its own.
//
// A service without the ownership marker was annotated by hand and is left
// alone, and external-dns keys the operator never writes (/target, /alias, ...)
// are never removed from any service.
func removeStaleExternalDNSAnnotations(annotations map[string]string) {
	if annotations[pNaming.AnnotationExternalDNSManaged] != "true" {
		return
	}

	delete(annotations, pNaming.AnnotationExternalDNSHostname)
	delete(annotations, pNaming.AnnotationExternalDNSTTL)
	delete(annotations, pNaming.AnnotationExternalDNSManaged)
}

func (r *PGClusterReconciler) reconcileLogicalReplicaService(
	ctx context.Context, cr *v2.PerconaPGCluster, spec *v2.LogicalReplicaSpec,
) error {
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      logicalReplicaObjectName(cr, spec.Name),
			Namespace: cr.Namespace,
		},
	}

	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, svc, func() error {
		svc.Labels = logicalReplicaLabels(cr, spec.Name)
		svc.Spec.Selector = logicalReplicaSelector(cr, spec.Name)
		svc.Spec.Ports = []corev1.ServicePort{{
			Name:       naming.PortPostgreSQL,
			Port:       *cr.Spec.Port,
			TargetPort: intstr.FromString(naming.PortPostgreSQL),
			Protocol:   corev1.ProtocolTCP,
		}}

		// Outside the guard below: dropping the whole expose block has to clear
		// the annotations too, not just dropping externalDNS from within it.
		removeStaleExternalDNSAnnotations(svc.Annotations)

		if spec.Expose != nil {
			svc.Spec.Type = corev1.ServiceType(spec.Expose.Type)
			svc.Spec.LoadBalancerSourceRanges = spec.Expose.LoadBalancerSourceRanges

			if annotations := spec.Expose.ServiceAnnotations(); len(annotations) > 0 {
				initialize.Annotations(svc)
				maps.Copy(svc.Annotations, annotations)
			}
			maps.Copy(svc.Labels, spec.Expose.Labels)
		}

		return controllerutil.SetControllerReference(cr, svc, r.Client.Scheme())
	})

	return errors.Wrap(err, "create or update service")
}

// checkLogicalReplicaHealth reports whether replication is still flowing.
func (r *PGClusterReconciler) checkLogicalReplicaHealth(
	ctx context.Context, cr *v2.PerconaPGCluster, spec *v2.LogicalReplicaSpec, status *v2.LogicalReplicaStatus,
) (*v2.LogicalReplicaStatus, error) {
	// Patroni's permanent-slot copying is gated behind use_slots, off by default,
	// so the slots only ever live on the primary that created them: losing them is
	// the expected outcome of a failover.
	names := make([]string, 0, len(status.Databases))
	for _, db := range status.Databases {
		names = append(names, postgres.QuoteLiteral(logicalreplica.SlotName(spec.Name, db)))
	}

	sql := "SELECT count(*) FROM pg_catalog.pg_replication_slots WHERE slot_name IN (" +
		strings.Join(names, ",") + ");"

	stdout, err := r.execOnPrimary(ctx, cr, "", sql)
	if err != nil {
		return status, errors.Wrap(err, "count replication slots")
	}

	present, err := strconv.Atoi(strings.TrimSpace(stdout))
	if err != nil {
		return status, errors.Wrapf(err, "parse replication slot count: %q", stdout)
	}

	if present < len(status.Databases) {
		status.State = v2.LogicalReplicaStateBroken
		status.Reason = v2.LogicalReplicaReasonSourceSlotMissing
		status.Message = fmt.Sprintf(
			"%d of %d replication slots are missing on the primary, most likely because it failed over; "+
				"this replica can no longer catch up, so "+logicalReplicaReseedInstructions,
			len(status.Databases)-present, len(status.Databases))
		return status, nil
	}

	// A live slot only proves the subscription was set up, not that it is running:
	// an apply worker that cannot connect exits and is restarted forever while the
	// slot sits there.
	pod, err := r.logicalReplicaPod(ctx, cr, spec.Name)
	if err != nil {
		status.State = v2.LogicalReplicaStateBootstrapping
		status.Reason = v2.LogicalReplicaReasonPodNotFound
		// The StatefulSet was only just created, or the pod is restarting.
		//nolint:nilerr
		return status, nil
	}

	for _, db := range status.Databases {
		subscription := logicalreplica.SubscriptionName(spec.Name, db)

		// pg_subscription.subenabled says it should be running,
		// pg_stat_subscription.pid says it actually is.
		sql := "SELECT s.subenabled, (st.pid IS NOT NULL) " +
			"FROM pg_catalog.pg_subscription s " +
			"LEFT JOIN pg_catalog.pg_stat_subscription st ON st.subid = s.oid AND st.relid IS NULL " +
			"WHERE s.subname = " + postgres.QuoteLiteral(subscription) + ";"

		stdout, err := r.execOnPod(ctx, pod, db, sql)
		if err != nil {
			return status, errors.Wrapf(err, "check subscription %q", subscription)
		}

		enabled, running, err := parseSubscriptionHealth(stdout)
		if err != nil {
			return status, errors.Wrapf(err, "check subscription %q", subscription)
		}

		switch {
		case !enabled:
			status.State = v2.LogicalReplicaStateBroken
			status.Reason = v2.LogicalReplicaReasonSubscriptionDisabled
			// The bootstrap sets disable_on_error, so this is what an apply error
			// looks like from here.
			status.Message = fmt.Sprintf(
				"subscription %q on database %q is disabled, most likely because applying a "+
					"change from the primary failed; check the logical replica's logs for the error",
				subscription, db)
			return status, nil

		case !running:
			status.State = v2.LogicalReplicaStateBroken
			status.Reason = v2.LogicalReplicaReasonApplyWorkerDown
			status.Message = fmt.Sprintf(
				"subscription %q on database %q is enabled but has no running apply worker; "+
					"check the logical replica's logs for why it cannot reach the primary",
				subscription, db)
			return status, nil
		}
	}

	status.State = v2.LogicalReplicaStateReady
	return status, nil
}

// parseSubscriptionHealth reads the "enabled|running" row that
// checkLogicalReplicaHealth queries. An empty result means the subscription is
// gone, which counts as neither.
func parseSubscriptionHealth(stdout string) (enabled, running bool, err error) {
	row := strings.TrimSpace(stdout)
	if row == "" {
		return false, false, nil
	}

	fields := strings.Split(row, "|")
	if len(fields) != 2 {
		return false, false, errors.Errorf("unexpected subscription query output: %q", stdout)
	}

	return strings.TrimSpace(fields[0]) == "t", strings.TrimSpace(fields[1]) == "t", nil
}

// cleanupRemovedLogicalReplicas tears down replicas that are no longer in the
// spec. Dropping the publications and the replication slots is not optional: an
// orphaned logical slot holds WAL on the primary forever.
//
// It returns the statuses of the replicas whose teardown could not be finished,
// which the caller has to keep: the recorded status is the only record of the
// objects that are still to be dropped.
func (r *PGClusterReconciler) cleanupRemovedLogicalReplicas(
	ctx context.Context, cr *v2.PerconaPGCluster,
) ([]v2.LogicalReplicaStatus, error) {
	wanted := make(map[string]struct{}, len(cr.Spec.LogicalReplicas))
	for _, replica := range cr.Spec.LogicalReplicas {
		wanted[replica.Name] = struct{}{}
	}

	var deferred []v2.LogicalReplicaStatus
	for i := range cr.Status.LogicalReplicas {
		recorded := cr.Status.LogicalReplicas[i]
		if _, ok := wanted[recorded.Name]; ok {
			continue
		}

		err := r.deleteLogicalReplica(ctx, cr, recorded.Name, recorded.Databases)
		switch {
		case errors.Is(err, ErrPrimaryPodNotFound):
			// Failing the reconcile over this would take the rest of the cluster
			// down with it. Keep the replica on the books and try again.
			logging.FromContext(ctx).Info("deferring logical replica teardown, the primary is unavailable",
				"logicalReplica", recorded.Name, "error", err.Error())

			held := *recorded.DeepCopy()
			held.State = v2.LogicalReplicaStateBroken
			held.Reason = v2.LogicalReplicaReasonAwaitingCleanup
			held.Message = "removed from the spec, waiting for a primary to drop its " +
				"replication slots and publications on"
			deferred = append(deferred, held)

		case err != nil:
			return deferred, errors.Wrapf(err, "delete logical replica %q", recorded.Name)
		}
	}

	return deferred, nil
}

// deleteLogicalReplica removes the replication objects on the primary and then
// every Kubernetes object belonging to the replica.
func (r *PGClusterReconciler) deleteLogicalReplica(
	ctx context.Context, cr *v2.PerconaPGCluster, replica string, databases []string,
) error {
	log := logging.FromContext(ctx).WithValues("logicalReplica", replica)

	if err := r.dropLogicalReplicaObjects(ctx, cr, replica, databases); err != nil {
		// Never delete the Kubernetes objects while slots may still be held on
		// the primary: retrying beats leaking WAL retention.
		return errors.Wrap(err, "drop replication objects")
	}

	if err := r.deleteLogicalReplicaJob(ctx, cr, replica); err != nil {
		return err
	}

	name := logicalReplicaObjectName(cr, replica)
	objects := []client.Object{
		&appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: cr.Namespace}},
		&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: cr.Namespace}},
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: logicalReplicaConfigMapName(cr, replica), Namespace: cr.Namespace}},
		&corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: logicalReplicaPVCName(cr, replica), Namespace: cr.Namespace}},
	}

	for _, object := range objects {
		if err := r.Client.Delete(ctx, object); client.IgnoreNotFound(err) != nil {
			return errors.Wrapf(err, "delete %T", object)
		}
	}

	log.Info("logical replica removed")

	return nil
}

// dropLogicalReplicaObjects drops the subscriptions on the replica and then the
// slots and publications on the primary.
func (r *PGClusterReconciler) dropLogicalReplicaObjects(
	ctx context.Context, cr *v2.PerconaPGCluster, replica string, databases []string,
) error {
	log := logging.FromContext(ctx).WithValues("logicalReplica", replica)

	// A replica removed before it ever resolved its databases has nothing on the
	// primary, so it must not need one to be torn down.
	if len(databases) == 0 {
		return nil
	}

	// Drop the subscriptions first so the slots go inactive. The replica may
	// already be gone, in which case they are inactive anyway.
	if pod, err := r.logicalReplicaPod(ctx, cr, replica); err != nil {
		log.Info("skipping subscription cleanup, replica pod is unavailable", "error", err.Error())
	} else {
		for _, db := range databases {
			subscription := logicalreplica.SubscriptionName(replica, db)
			sql := fmt.Sprintf(
				// Detaching the slot first keeps DROP SUBSCRIPTION from trying
				// to reach the primary, which may already be unreachable.
				"ALTER SUBSCRIPTION %q DISABLE; ALTER SUBSCRIPTION %q SET (slot_name = NONE); DROP SUBSCRIPTION %q;",
				subscription, subscription, subscription)

			if _, err := r.execOnPod(ctx, pod, db, sql); err != nil {
				log.Info("could not drop subscription", "subscription", subscription, "error", err.Error())
			}
		}
	}

	// Resolved once: the loop below runs three statements per database, and each
	// lookup lists every pod in the namespace.
	primary, err := perconaPG.GetPrimaryPod(ctx, r.Client, cr)
	if err != nil {
		return errors.Wrap(ErrPrimaryPodNotFound, err.Error())
	}

	// A database this replica covered may be gone - a point-in-time restore can
	// rewind the primary past its creation, and users drop databases. Its
	// publication went with it, but psql cannot connect to it at all, so the drop
	// below has to be skipped rather than fail the teardown forever.
	present, err := r.presentDatabases(ctx, primary)
	if err != nil {
		return errors.Wrap(err, "list databases")
	}

	for _, db := range databases {
		slot := logicalreplica.SlotName(replica, db)
		publication := logicalreplica.PublicationName(replica, db)

		log.Info("dropping replication objects", "slot", slot, "publication", publication)

		terminateSQL := fmt.Sprintf("SELECT pg_terminate_backend(active_pid) FROM pg_replication_slots WHERE slot_name = %s AND active = true;",
			postgres.QuoteLiteral(slot))
		if _, err := r.execOnPod(ctx, primary, "", terminateSQL); err != nil {
			return errors.Wrapf(err, "terminate backend on replication slot %q", slot)
		}

		sql := fmt.Sprintf(
			"SELECT pg_catalog.pg_drop_replication_slot(slot_name) FROM pg_catalog.pg_replication_slots WHERE slot_name = %s;",
			postgres.QuoteLiteral(slot))
		if _, err := r.execOnPod(ctx, primary, "", sql); err != nil {
			return errors.Wrapf(err, "drop replication slot %q", slot)
		}

		// The slot is cluster-wide and is dropped either way, which is the part
		// that matters: it is what pins WAL on the primary.
		if !slices.Contains(present, db) {
			log.Info("skipping publication, its database is gone from the primary",
				"database", db, "publication", publication)
			continue
		}

		if _, err := r.execOnPod(ctx, primary, db, fmt.Sprintf("DROP PUBLICATION IF EXISTS %q;", publication)); err != nil {
			return errors.Wrapf(err, "drop publication %q", publication)
		}
	}

	return nil
}

// queryDatabases returns the databases on pod that match where, ordered by name:
// the list is frozen for a replica's lifetime, so it has to be stable.
func (r *PGClusterReconciler) queryDatabases(
	ctx context.Context, pod *corev1.Pod, where string,
) ([]string, error) {
	stdout, err := r.execOnPod(ctx, pod, "",
		`SELECT datname FROM pg_catalog.pg_database WHERE `+where+` ORDER BY datname;`)
	if err != nil {
		return nil, err
	}

	var databases []string
	for line := range strings.SplitSeq(stdout, "\n") {
		if name := strings.TrimSpace(line); name != "" {
			databases = append(databases, name)
		}
	}

	return databases, nil
}

// presentDatabases returns the databases that exist on pod and can be connected
// to.
func (r *PGClusterReconciler) presentDatabases(ctx context.Context, pod *corev1.Pod) ([]string, error) {
	return r.queryDatabases(ctx, pod, "datallowconn")
}

func (r *PGClusterReconciler) logicalReplicaPod(ctx context.Context, cr *v2.PerconaPGCluster, replica string) (*corev1.Pod, error) {
	pods := &corev1.PodList{}
	if err := r.Client.List(ctx, pods, &client.ListOptions{
		Namespace:     cr.Namespace,
		LabelSelector: labels.SelectorFromSet(logicalReplicaSelector(cr, replica)),
	}); err != nil {
		return nil, errors.Wrap(err, "list pods")
	}

	for i := range pods.Items {
		pod := &pods.Items[i]
		if pod.DeletionTimestamp != nil || pod.Status.Phase != corev1.PodRunning {
			continue
		}

		// Running is not enough: psql fails against a postmaster that is not
		// accepting connections yet, and callers treat an exec failure as broken,
		// which would turn a few seconds of startup into a hard error.
		//
		// The startup probe is the signal this needs: true once the postmaster
		// has accepted connections, false again when the container restarts.
		if slices.ContainsFunc(pod.Status.ContainerStatuses, func(c corev1.ContainerStatus) bool {
			return c.Name == naming.ContainerDatabase &&
				c.State.Running != nil && c.Started != nil && *c.Started
		}) {
			return pod, nil
		}
	}

	return nil, errors.New("no running logical replica pod")
}

// execOnPod runs sql inside a logical replica pod and returns its trimmed
// output.
func (r *PGClusterReconciler) execOnPod(ctx context.Context, pod *corev1.Pod, database, sql string) (string, error) {
	exec := postgres.Executor(func(ctx context.Context, stdin io.Reader, stdout, stderr io.Writer, command ...string) error {
		return r.PodExec(ctx, pod.GetNamespace(), pod.GetName(), naming.ContainerDatabase, stdin, stdout, stderr, command...)
	})

	options := []string{"-t"}
	if database != "" {
		options = append(options, "--dbname="+database)
	}

	stdout, stderr, err := exec.Exec(ctx, strings.NewReader(sql), map[string]string{
		"ON_ERROR_STOP": "on",
		"QUIET":         "on",
	}, options)
	if err != nil {
		return "", errors.Wrapf(err, "execute query: stderr=%s", stderr)
	}

	return strings.TrimSpace(stdout), nil
}
