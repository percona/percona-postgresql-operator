package pgcluster

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/pkg/errors"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/percona/percona-postgresql-operator/v2/internal/initialize"
	"github.com/percona/percona-postgresql-operator/v2/internal/logging"
	"github.com/percona/percona-postgresql-operator/v2/internal/logicalreplica"
	"github.com/percona/percona-postgresql-operator/v2/internal/naming"
	"github.com/percona/percona-postgresql-operator/v2/internal/postgres"
	"github.com/percona/percona-postgresql-operator/v2/percona/k8s"
	pNaming "github.com/percona/percona-postgresql-operator/v2/percona/naming"
	perconaPG "github.com/percona/percona-postgresql-operator/v2/percona/postgres"
	v2 "github.com/percona/percona-postgresql-operator/v2/pkg/apis/pgv2.percona.com/v2"
	"github.com/percona/percona-postgresql-operator/v2/pkg/apis/upstream.pgv2.percona.com/v1beta1"
)

const (
	// logicalReplicaConfigMountPath is where the operator-rendered
	// postgresql.conf of a logical replica is mounted. It is deliberately not
	// naming.ConfigMountPath, which spec.config.files already owns.
	logicalReplicaConfigMountPath = "/etc/logical-replica"

	// logicalReplicaConfigFile is the postgresql.conf key in the ConfigMap.
	logicalReplicaConfigFile = "postgresql.conf"

	// logicalReplicaConfigVolume is the name of the config volume and mount.
	logicalReplicaConfigVolume = "logical-replica-config"

	// logicalReplicaBootstrapContainer is the name of the container in the
	// bootstrap Job.
	logicalReplicaBootstrapContainer = "logical-replica-bootstrap"

	// logicalReplicaRecoveryTimeout bounds how long pg_createsubscriber waits
	// for the freshly seeded standby to replay up to the conversion LSN.
	logicalReplicaRecoveryTimeout = 3600

	// logicalReplicaWorkerHeadroom is added on top of one logical replication
	// worker per database when sizing max_worker_processes, which must be
	// strictly greater than the number of databases and also covers autovacuum
	// and parallel workers.
	logicalReplicaWorkerHeadroom = 8
)

// logicalReplicaObjectName is the name shared by every object that makes up a
// logical replica. The "-lr-" infix keeps it from colliding with the instance
// StatefulSets, which are named "<cluster>-<instance-set>-<hash>".
func logicalReplicaObjectName(cr *v2.PerconaPGCluster, replica string) string {
	return cr.Name + "-lr-" + replica
}

func logicalReplicaPVCName(cr *v2.PerconaPGCluster, replica string) string {
	return logicalReplicaObjectName(cr, replica) + "-pgdata"
}

func logicalReplicaJobName(cr *v2.PerconaPGCluster, replica string) string {
	return logicalReplicaObjectName(cr, replica) + "-bootstrap"
}

func logicalReplicaConfigMapName(cr *v2.PerconaPGCluster, replica string) string {
	return logicalReplicaObjectName(cr, replica) + "-config"
}

func logicalReplicaLabels(cr *v2.PerconaPGCluster, replica string) map[string]string {
	return map[string]string{
		naming.LabelCluster:           cr.Name,
		pNaming.LabelLogicalReplica:   replica,
		pNaming.LabelOperatorVersion:  cr.Spec.CRVersion,
		"app.kubernetes.io/instance":  cr.Name,
		"app.kubernetes.io/component": "logical-replica",
	}
}

// reconcileLogicalReplicas brings every logical replica in the spec to life and
// tears down the ones that were removed from it. K8SPG-784
func (r *PGClusterReconciler) reconcileLogicalReplicas(ctx context.Context, cr *v2.PerconaPGCluster) error {
	if cr.CompareVersion("3.1.0") < 0 {
		return nil
	}

	// Clusters that never used the feature must not pay for it with an API call
	// on every reconcile.
	if len(cr.Spec.LogicalReplicas) == 0 && len(cr.Status.LogicalReplicas) == 0 {
		return nil
	}

	// Run the teardown even when the section is empty: that is exactly the case
	// where the last replica was just removed.
	if err := r.cleanupRemovedLogicalReplicas(ctx, cr); err != nil {
		return errors.Wrap(err, "clean up removed logical replicas")
	}

	if len(cr.Spec.LogicalReplicas) == 0 {
		return r.updateLogicalReplicaStatus(ctx, cr, nil)
	}

	// Everything below talks to the primary, so there has to be one.
	if cr.Status.State != v2.AppStateReady {
		return nil
	}

	log := logging.FromContext(ctx)

	statuses := make([]v2.LogicalReplicaStatus, 0, len(cr.Spec.LogicalReplicas))
	for i := range cr.Spec.LogicalReplicas {
		spec := &cr.Spec.LogicalReplicas[i]

		status, err := r.reconcileLogicalReplica(ctx, cr, spec)
		if err != nil {
			// One broken replica must not stall the others or the rest of the
			// cluster: record it and carry on.
			log.Error(err, "reconcile logical replica", "logicalReplica", spec.Name)

			status = logicalReplicaStatusFor(cr, spec.Name)
			status.State = v2.LogicalReplicaStateBroken
			status.Message = err.Error()
		}

		statuses = append(statuses, *status)
	}

	return r.updateLogicalReplicaStatus(ctx, cr, statuses)
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

func (r *PGClusterReconciler) updateLogicalReplicaStatus(ctx context.Context, cr *v2.PerconaPGCluster, statuses []v2.LogicalReplicaStatus) error {
	return errors.Wrap(retry.RetryOnConflict(retry.DefaultRetry, func() error {
		cluster := &v2.PerconaPGCluster{}
		if err := r.Client.Get(ctx, types.NamespacedName{
			Name:      cr.Name,
			Namespace: cr.Namespace,
		}, cluster); err != nil {
			return errors.Wrap(err, "get PerconaPGCluster")
		}

		cluster.Status.LogicalReplicas = statuses

		return r.Client.Status().Update(ctx, cluster)
	}), "update logical replica status")
}

// reconcileLogicalReplica drives a single replica through its lifecycle:
// resolve databases, seed and convert with a one-shot Job, then run it.
func (r *PGClusterReconciler) reconcileLogicalReplica(
	ctx context.Context, cr *v2.PerconaPGCluster, spec *v2.LogicalReplicaSpec,
) (*v2.LogicalReplicaStatus, error) {
	log := logging.FromContext(ctx).WithValues("logicalReplica", spec.Name)
	status := logicalReplicaStatusFor(cr, spec.Name)

	// The set of databases is resolved exactly once and then frozen in the
	// status: the publications, subscriptions and slots are all named after it,
	// so it must not drift when databases are created or dropped later.
	if len(status.Databases) == 0 {
		databases, err := r.resolveLogicalReplicaDatabases(ctx, cr, spec)
		if err != nil {
			return nil, errors.Wrap(err, "resolve databases")
		}
		if len(databases) == 0 {
			status.State = v2.LogicalReplicaStateBroken
			status.Message = "no databases to replicate"
			return status, nil
		}

		status.Databases = databases
		status.State = v2.LogicalReplicaStateBootstrapping

		// Persist the resolved list before anything acts on it.
		return status, nil
	}

	if err := r.reconcileLogicalReplicaPVC(ctx, cr, spec); err != nil {
		return nil, errors.Wrap(err, "reconcile pvc")
	}

	if status.ConvertedAt == nil {
		converted, err := r.reconcileLogicalReplicaBootstrap(ctx, cr, spec, status)
		if err != nil {
			return nil, err
		}
		if !converted {
			return status, nil
		}

		now := metav1.Now()
		status.ConvertedAt = &now
		log.Info("logical replica converted", "databases", status.Databases)
	}

	if err := r.reconcileLogicalReplicaConfigMap(ctx, cr, spec, status); err != nil {
		return nil, errors.Wrap(err, "reconcile configmap")
	}
	if err := r.reconcileLogicalReplicaStatefulSet(ctx, cr, spec); err != nil {
		return nil, errors.Wrap(err, "reconcile statefulset")
	}
	if err := r.reconcileLogicalReplicaService(ctx, cr, spec); err != nil {
		return nil, errors.Wrap(err, "reconcile service")
	}

	return r.checkLogicalReplicaHealth(ctx, cr, spec, status)
}

// primaryExecutor returns an Executor that runs psql inside the primary pod.
func (r *PGClusterReconciler) primaryExecutor(ctx context.Context, cr *v2.PerconaPGCluster) (postgres.Executor, error) {
	primary, err := perconaPG.GetPrimaryPod(ctx, r.Client, cr)
	if err != nil {
		return nil, errors.Wrap(ErrPrimaryPodNotFound, err.Error())
	}

	return postgres.Executor(func(ctx context.Context, stdin io.Reader, stdout, stderr io.Writer, command ...string) error {
		return r.PodExec(ctx, primary.GetNamespace(), primary.GetName(), naming.ContainerDatabase, stdin, stdout, stderr, command...)
	}), nil
}

// execOnPrimary runs sql on the primary and returns its trimmed output.
func (r *PGClusterReconciler) execOnPrimary(ctx context.Context, cr *v2.PerconaPGCluster, database, sql string) (string, error) {
	exec, err := r.primaryExecutor(ctx, cr)
	if err != nil {
		return "", err
	}

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

// resolveLogicalReplicaDatabases returns the databases the replica covers. An
// empty spec.databases means every database a user could connect to, which is
// what "replicate everything" has to mean: templates cannot be subscribed and
// "postgres" is a maintenance database.
func (r *PGClusterReconciler) resolveLogicalReplicaDatabases(
	ctx context.Context, cr *v2.PerconaPGCluster, spec *v2.LogicalReplicaSpec,
) ([]string, error) {
	if len(spec.Databases) > 0 {
		databases := make([]string, 0, len(spec.Databases))
		for _, db := range spec.Databases {
			databases = append(databases, string(db))
		}
		return databases, nil
	}

	const sql = `SELECT datname FROM pg_catalog.pg_database ` +
		`WHERE datallowconn AND NOT datistemplate AND datname <> 'postgres' ORDER BY datname;`

	stdout, err := r.execOnPrimary(ctx, cr, "", sql)
	if err != nil {
		return nil, err
	}

	databases := make([]string, 0)
	for _, line := range strings.Split(stdout, "\n") {
		if name := strings.TrimSpace(line); name != "" {
			databases = append(databases, name)
		}
	}

	return databases, nil
}

// checkPrimaryCapacity verifies the primary can host the replication slots and
// WAL senders this replica needs.
//
// The operator deliberately does not raise max_replication_slots or
// max_wal_senders itself: both need a server restart, and silently bouncing the
// primary because someone added a logical replica would be a nasty surprise.
// pg_createsubscriber checks this too, but only after a full base backup has
// already been taken, so it is worth catching up front.
func (r *PGClusterReconciler) checkPrimaryCapacity(ctx context.Context, cr *v2.PerconaPGCluster, needed int) error {
	const sql = `SELECT current_setting('max_replication_slots')::int - (SELECT count(*) FROM pg_catalog.pg_replication_slots), ` +
		`current_setting('max_wal_senders')::int - (SELECT count(*) FROM pg_catalog.pg_stat_replication);`

	stdout, err := r.execOnPrimary(ctx, cr, "", sql)
	if err != nil {
		return err
	}

	fields := strings.Split(stdout, "|")
	if len(fields) != 2 {
		return errors.Errorf("unexpected capacity query output: %q", stdout)
	}

	freeSlots, err := strconv.Atoi(strings.TrimSpace(fields[0]))
	if err != nil {
		return errors.Wrapf(err, "parse free replication slots: %q", fields[0])
	}
	freeSenders, err := strconv.Atoi(strings.TrimSpace(fields[1]))
	if err != nil {
		return errors.Wrapf(err, "parse free WAL senders: %q", fields[1])
	}

	if freeSlots < needed {
		return errors.Errorf(
			"primary has %d free replication slots but %d are needed; raise max_replication_slots via spec.patroni.dynamicConfiguration",
			freeSlots, needed)
	}
	if freeSenders < needed {
		return errors.Errorf(
			"primary has %d free WAL senders but %d are needed; raise max_wal_senders via spec.patroni.dynamicConfiguration",
			freeSenders, needed)
	}

	return nil
}

func (r *PGClusterReconciler) reconcileLogicalReplicaPVC(
	ctx context.Context, cr *v2.PerconaPGCluster, spec *v2.LogicalReplicaSpec,
) error {
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      logicalReplicaPVCName(cr, spec.Name),
			Namespace: cr.Namespace,
		},
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

	return errors.Wrap(err, "create or update pvc")
}

// reconcileLogicalReplicaBootstrap runs the one-shot Job that seeds the data
// volume from the primary and converts it into a logical subscriber. It reports
// whether the conversion has completed.
func (r *PGClusterReconciler) reconcileLogicalReplicaBootstrap(
	ctx context.Context, cr *v2.PerconaPGCluster, spec *v2.LogicalReplicaSpec, status *v2.LogicalReplicaStatus,
) (bool, error) {
	job := &batchv1.Job{}
	key := client.ObjectKey{Name: logicalReplicaJobName(cr, spec.Name), Namespace: cr.Namespace}

	err := r.Client.Get(ctx, key, job)
	switch {
	case err == nil:
		for _, condition := range job.Status.Conditions {
			if condition.Status != corev1.ConditionTrue {
				continue
			}
			switch condition.Type {
			case batchv1.JobComplete:
				// The Job holds the data volume open; drop it so the
				// StatefulSet can take over.
				propagation := metav1.DeletePropagationForeground
				if err := r.Client.Delete(ctx, job, &client.DeleteOptions{
					PropagationPolicy: &propagation,
				}); client.IgnoreNotFound(err) != nil {
					return false, errors.Wrap(err, "delete completed bootstrap job")
				}
				return true, nil

			case batchv1.JobFailed:
				status.State = v2.LogicalReplicaStateBroken
				status.Reason = v2.LogicalReplicaReasonBootstrapFailed
				status.Message = "bootstrap job failed: " + condition.Message +
					"; inspect the " + job.Name + " job, then delete the logical replica and recreate it"
				return false, nil

			default:
				// Still running, suspended or on its way to one of the above.
			}
		}

		status.State = v2.LogicalReplicaStateBootstrapping
		return false, nil

	case !apierrors.IsNotFound(err):
		return false, errors.Wrap(err, "get bootstrap job")
	}

	// No Job yet. Fail fast if the primary cannot host the slots we need,
	// rather than after a full base backup.
	if err := r.checkPrimaryCapacity(ctx, cr, len(status.Databases)); err != nil {
		status.State = v2.LogicalReplicaStateBroken
		status.Message = err.Error()
		// Not a controller error: only the user can raise these settings, and
		// retrying with backoff would never make it true. Report and wait.
		//nolint:nilerr
		return false, nil
	}

	job, err = r.generateLogicalReplicaBootstrapJob(cr, spec, status)
	if err != nil {
		return false, errors.Wrap(err, "generate bootstrap job")
	}
	if err := r.Client.Create(ctx, job); err != nil {
		return false, errors.Wrap(err, "create bootstrap job")
	}

	status.State = v2.LogicalReplicaStateBootstrapping
	return false, nil
}

// logicalReplicaBootstrapScript is the body of the bootstrap Job.
//
// pg_createsubscriber only converts an existing physical standby, so the volume
// is first seeded with pg_basebackup -R, which writes both standby.signal and
// primary_conninfo. The conversion then promotes the result into a standalone
// primary carrying the subscriptions.
func logicalReplicaBootstrapScript(dataDir, primaryHost string, port int32, databases []string, replica string) string {
	args := []string{
		"pg_createsubscriber",
		"--verbose",
		"--pgdata=" + dataDir,
		"--publisher-server=\"${PUBLISHER_CONNINFO}\"",
		"--socketdir=/tmp",
		"--recovery-timeout=" + strconv.Itoa(logicalReplicaRecoveryTimeout),
	}
	for _, db := range databases {
		args = append(args,
			"--database="+shellQuote(db),
			"--publication="+logicalreplica.PublicationName(replica, db),
			"--subscription="+logicalreplica.SubscriptionName(replica, db),
			"--replication-slot="+logicalreplica.SlotName(replica, db),
		)
	}

	return strings.Join([]string{
		`set -euo pipefail`,
		``,
		`PUBLISHER_CONNINFO="host=` + primaryHost + ` port=` + strconv.Itoa(int(port)) +
			` user=` + logicalreplica.ReplicationUser + ` dbname=postgres sslmode=verify-ca sslrootcert=` +
			naming.CertMountPath + `/ca.crt"`,
		``,
		`if [ -s ` + shellQuote(dataDir+"/PG_VERSION") + ` ]; then`,
		`  echo "data directory is not empty, refusing to seed over it" >&2`,
		`  exit 1`,
		`fi`,
		``,
		`install --directory --mode=0700 ` + shellQuote(dataDir),
		``,
		`echo "seeding from ` + primaryHost + `"`,
		`pg_basebackup --pgdata=` + shellQuote(dataDir) +
			` --host=` + primaryHost + ` --port=` + strconv.Itoa(int(port)) +
			` --username=` + logicalreplica.ReplicationUser +
			` --write-recovery-conf --wal-method=stream --checkpoint=fast --progress --no-password`,
		``,
		`echo "converting to a logical replica"`,
		strings.Join(args, " \\\n  "),
		``,
		`echo "done"`,
	}, "\n")
}

// shellQuote wraps s in single quotes for safe interpolation into the bootstrap
// script. Database names may contain almost anything.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func (r *PGClusterReconciler) generateLogicalReplicaBootstrapJob(
	cr *v2.PerconaPGCluster, spec *v2.LogicalReplicaSpec, status *v2.LogicalReplicaStatus,
) (*batchv1.Job, error) {
	crunchyCluster := logicalReplicaCrunchyShim(cr)
	dataDir := postgres.DataDirectory(crunchyCluster)
	primaryHost := naming.ClusterPrimaryService(crunchyCluster).Name + "." + cr.Namespace + ".svc"

	script := logicalReplicaBootstrapScript(dataDir, primaryHost, *cr.Spec.Port, status.Databases, spec.Name)

	container := corev1.Container{
		Name:            logicalReplicaBootstrapContainer,
		Image:           cr.PostgresImage(),
		ImagePullPolicy: cr.Spec.ImagePullPolicy,
		Command:         []string{"bash", "-c", script},
		Env: append(logicalReplicaEnvironment(cr), corev1.EnvVar{
			Name: "PGPASSWORD",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: cr.Name + "-" + naming.RolePostgresUser + "-" + v2.UserLogicalReplication,
					},
					Key: "password",
				},
			},
		}),
		Resources:       spec.Resources,
		SecurityContext: initialize.RestrictedSecurityContext(true),
		VolumeMounts: []corev1.VolumeMount{
			corev1.VolumeMount{
				Name:      "tmp",
				MountPath: "/tmp",
			},
			logicalReplicaCertVolumeMount(),
			postgres.DataVolumeMount(),
		},
	}

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      logicalReplicaJobName(cr, spec.Name),
			Namespace: cr.Namespace,
			Labels:    logicalReplicaLabels(cr, spec.Name),
			Annotations: map[string]string{
				pNaming.AnnotationLogicalReplicaDatabases: strings.Join(status.Databases, ","),
			},
		},
		Spec: batchv1.JobSpec{
			BackoffLimit: new(int32(0)),
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: logicalReplicaLabels(cr, spec.Name),
				},
				Spec: corev1.PodSpec{
					RestartPolicy:                corev1.RestartPolicyNever,
					InitContainers:               []corev1.Container{},
					Containers:                   []corev1.Container{container},
					Volumes:                      logicalReplicaVolumes(cr, spec, false),
					SecurityContext:              postgres.PodSecurityContext(crunchyCluster),
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

	return job, controllerutil.SetControllerReference(cr, job, r.Client.Scheme())
}

// logicalReplicaCrunchyShim builds the minimal PostgresCluster that the shared
// internal/postgres helpers need to derive paths and security contexts.
func logicalReplicaCrunchyShim(cr *v2.PerconaPGCluster) *v1beta1.PostgresCluster {
	return &v1beta1.PostgresCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cr.Name,
			Namespace: cr.Namespace,
			Labels:    map[string]string{v1beta1.LabelVersion: cr.Spec.CRVersion},
		},
		Spec: v1beta1.PostgresClusterSpec{
			PostgresVersion: cr.Spec.PostgresVersion,
			Port:            cr.Spec.Port,
		},
	}
}

func logicalReplicaCertVolumeMount() corev1.VolumeMount {
	return corev1.VolumeMount{
		Name:      naming.CertVolume,
		MountPath: naming.CertMountPath,
		ReadOnly:  true,
	}
}

func logicalReplicaEnvironment(cr *v2.PerconaPGCluster) []corev1.EnvVar {
	return []corev1.EnvVar{
		{Name: "PGDATA", Value: postgres.DataDirectory(logicalReplicaCrunchyShim(cr))},
		{Name: "PGHOST", Value: postgres.SocketDirectory},
		{Name: "PGPORT", Value: strconv.Itoa(int(*cr.Spec.Port))},
	}
}

// logicalReplicaVolumes returns the volumes shared by the bootstrap Job and the
// StatefulSet. The Job runs before the ConfigMap matters, so it opts out of it.
func logicalReplicaVolumes(cr *v2.PerconaPGCluster, spec *v2.LogicalReplicaSpec, withConfig bool) []corev1.Volume {
	certSecret := cr.Name + "-cluster-cert"
	if cr.Spec.Secrets.CustomTLSSecret != nil && cr.Spec.Secrets.CustomTLSSecret.Name != "" {
		certSecret = cr.Spec.Secrets.CustomTLSSecret.Name
	}

	volumes := []corev1.Volume{
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
			// PostgreSQL needs a writable directory for its unix socket.
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
	}

	if withConfig {
		volumes = append(volumes, corev1.Volume{
			Name: logicalReplicaConfigVolume,
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: logicalReplicaConfigMapName(cr, spec.Name),
					},
				},
			},
		})
	}

	return volumes
}

// logicalReplicaPostgresConfig renders the postgresql.conf of a logical replica.
//
// pg_hba.conf is intentionally left alone: pg_basebackup copies the primary's,
// which already carries the operator's rules and the users' credentials, so
// clients authenticate against the replica exactly as they do against the
// primary.
func logicalReplicaPostgresConfig(cr *v2.PerconaPGCluster, databases int) string {
	workers := databases + logicalReplicaWorkerHeadroom

	lines := []string{
		"# Generated by percona-postgresql-operator. Do not edit.",
		"listen_addresses = '*'",
		fmt.Sprintf("port = %d", *cr.Spec.Port),
		fmt.Sprintf("unix_socket_directories = '%s'", postgres.SocketDirectory),
		"",
		"ssl = on",
		fmt.Sprintf("ssl_cert_file = '%s/tls.crt'", naming.CertMountPath),
		fmt.Sprintf("ssl_key_file = '%s/tls.key'", naming.CertMountPath),
		fmt.Sprintf("ssl_ca_file = '%s/ca.crt'", naming.CertMountPath),
		"",
		"# One apply worker and one origin per subscribed database.",
		fmt.Sprintf("max_replication_slots = %d", databases),
		fmt.Sprintf("max_logical_replication_workers = %d", databases),
		fmt.Sprintf("max_worker_processes = %d", workers),
		"",
		"password_encryption = 'scram-sha-256'",
	}

	return strings.Join(lines, "\n") + "\n"
}

func (r *PGClusterReconciler) reconcileLogicalReplicaConfigMap(
	ctx context.Context, cr *v2.PerconaPGCluster, spec *v2.LogicalReplicaSpec, status *v2.LogicalReplicaStatus,
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
			logicalReplicaConfigFile: logicalReplicaPostgresConfig(cr, len(status.Databases)),
		}

		return controllerutil.SetControllerReference(cr, cm, r.Client.Scheme())
	})

	return errors.Wrap(err, "create or update configmap")
}

func (r *PGClusterReconciler) reconcileLogicalReplicaStatefulSet(
	ctx context.Context, cr *v2.PerconaPGCluster, spec *v2.LogicalReplicaSpec,
) error {
	name := logicalReplicaObjectName(cr, spec.Name)
	crunchyCluster := logicalReplicaCrunchyShim(cr)
	dataDir := postgres.DataDirectory(crunchyCluster)

	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: cr.Namespace},
	}

	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, sts, func() error {
		selector := map[string]string{
			naming.LabelCluster:         cr.Name,
			pNaming.LabelLogicalReplica: spec.Name,
		}

		podLabels := logicalReplicaLabels(cr, spec.Name)
		podAnnotations := map[string]string{}
		if spec.Metadata != nil {
			for k, v := range spec.Metadata.Labels {
				podLabels[k] = v
			}
			for k, v := range spec.Metadata.Annotations {
				podAnnotations[k] = v
			}
		}

		sts.Labels = logicalReplicaLabels(cr, spec.Name)
		sts.Spec.Replicas = new(int32(1))
		sts.Spec.ServiceName = name
		sts.Spec.Selector = &metav1.LabelSelector{MatchLabels: selector}
		sts.Spec.Template.Labels = podLabels
		sts.Spec.Template.Annotations = podAnnotations

		container := corev1.Container{
			Name:            naming.ContainerDatabase,
			Image:           cr.PostgresImage(),
			ImagePullPolicy: cr.Spec.ImagePullPolicy,
			Command: []string{
				"postgres",
				"-D", dataDir,
				"-c", "config_file=" + logicalReplicaConfigMountPath + "/" + logicalReplicaConfigFile,
			},
			Env:       logicalReplicaEnvironment(cr),
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
				{Name: "tmp", MountPath: postgres.SocketDirectory},
				{Name: logicalReplicaConfigVolume, MountPath: logicalReplicaConfigMountPath, ReadOnly: true},
			},
			ReadinessProbe: &corev1.Probe{
				ProbeHandler: corev1.ProbeHandler{
					Exec: &corev1.ExecAction{
						Command: []string{"pg_isready", "-h", postgres.SocketDirectory},
					},
				},
				InitialDelaySeconds: 5,
				PeriodSeconds:       10,
			},
			LivenessProbe: &corev1.Probe{
				ProbeHandler: corev1.ProbeHandler{
					Exec: &corev1.ExecAction{
						Command: []string{"pg_isready", "-h", postgres.SocketDirectory},
					},
				},
				InitialDelaySeconds: 30,
				PeriodSeconds:       20,
				FailureThreshold:    6,
			},
		}

		sts.Spec.Template.Spec.Containers = []corev1.Container{container}
		sts.Spec.Template.Spec.Volumes = logicalReplicaVolumes(cr, spec, true)
		sts.Spec.Template.Spec.SecurityContext = postgres.PodSecurityContext(crunchyCluster)
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
		svc.Spec.Selector = map[string]string{
			naming.LabelCluster:         cr.Name,
			pNaming.LabelLogicalReplica: spec.Name,
		}
		svc.Spec.Ports = []corev1.ServicePort{{
			Name:       naming.PortPostgreSQL,
			Port:       *cr.Spec.Port,
			TargetPort: intstr.FromString(naming.PortPostgreSQL),
			Protocol:   corev1.ProtocolTCP,
		}}

		if spec.Expose != nil {
			svc.Spec.Type = corev1.ServiceType(spec.Expose.Type)
			svc.Spec.LoadBalancerSourceRanges = spec.Expose.LoadBalancerSourceRanges
			for k, v := range spec.Expose.Annotations {
				if svc.Annotations == nil {
					svc.Annotations = map[string]string{}
				}
				svc.Annotations[k] = v
			}
			for k, v := range spec.Expose.Labels {
				svc.Labels[k] = v
			}
		}

		return controllerutil.SetControllerReference(cr, svc, r.Client.Scheme())
	})

	return errors.Wrap(err, "create or update service")
}

// checkLogicalReplicaHealth reports whether replication is still flowing.
func (r *PGClusterReconciler) checkLogicalReplicaHealth(
	ctx context.Context, cr *v2.PerconaPGCluster, spec *v2.LogicalReplicaSpec, status *v2.LogicalReplicaStatus,
) (*v2.LogicalReplicaStatus, error) {
	// The slots live on the primary and, because Patroni's permanent-slot
	// copying is gated behind use_slots, only on the one that created them.
	// Losing them is the expected outcome of a failover.
	names := make([]string, 0, len(status.Databases))
	for _, db := range status.Databases {
		names = append(names, "'"+logicalreplica.SlotName(spec.Name, db)+"'")
	}

	sql := "SELECT count(*) FROM pg_catalog.pg_replication_slots WHERE slot_name IN (" +
		strings.Join(names, ",") + ");"

	stdout, err := r.execOnPrimary(ctx, cr, "", sql)
	if err != nil {
		return nil, errors.Wrap(err, "count replication slots")
	}

	present, err := strconv.Atoi(strings.TrimSpace(stdout))
	if err != nil {
		return nil, errors.Wrapf(err, "parse replication slot count: %q", stdout)
	}

	if present < len(status.Databases) {
		status.State = v2.LogicalReplicaStateBroken
		status.Reason = v2.LogicalReplicaReasonSourceSlotMissing
		status.Message = fmt.Sprintf(
			"%d of %d replication slots are missing on the primary, most likely because it failed over; "+
				"recreate this logical replica to re-seed it",
			len(status.Databases)-present, len(status.Databases))
		return status, nil
	}

	status.State = v2.LogicalReplicaStateReady
	return status, nil
}

// cleanupRemovedLogicalReplicas tears down replicas that are no longer in the
// spec. Dropping the publications and the replication slots is not optional: an
// orphaned logical slot holds WAL on the primary forever and will eventually
// fill its volume.
func (r *PGClusterReconciler) cleanupRemovedLogicalReplicas(ctx context.Context, cr *v2.PerconaPGCluster) error {
	wanted := make(map[string]struct{}, len(cr.Spec.LogicalReplicas))
	for _, replica := range cr.Spec.LogicalReplicas {
		wanted[replica.Name] = struct{}{}
	}

	for i := range cr.Status.LogicalReplicas {
		recorded := cr.Status.LogicalReplicas[i]
		if _, ok := wanted[recorded.Name]; ok {
			continue
		}

		if err := r.deleteLogicalReplica(ctx, cr, recorded.Name, recorded.Databases); err != nil {
			return errors.Wrapf(err, "delete logical replica %q", recorded.Name)
		}
	}

	return nil
}

// deleteLogicalReplica removes the replication objects on the primary and then
// every Kubernetes object belonging to the replica.
func (r *PGClusterReconciler) deleteLogicalReplica(
	ctx context.Context, cr *v2.PerconaPGCluster, replica string, databases []string,
) error {
	log := logging.FromContext(ctx).WithValues("logicalReplica", replica)

	if err := r.dropLogicalReplicaObjects(ctx, cr, replica, databases); err != nil {
		// Never delete the Kubernetes objects while slots may still be held on
		// the primary: retrying is far better than leaking WAL retention.
		return errors.Wrap(err, "drop replication objects")
	}

	name := logicalReplicaObjectName(cr, replica)
	objects := []client.Object{
		&appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: cr.Namespace}},
		&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: cr.Namespace}},
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: logicalReplicaConfigMapName(cr, replica), Namespace: cr.Namespace}},
		&corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: logicalReplicaPVCName(cr, replica), Namespace: cr.Namespace}},
	}

	propagation := metav1.DeletePropagationForeground
	job := &batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: logicalReplicaJobName(cr, replica), Namespace: cr.Namespace}}
	if err := r.Client.Delete(ctx, job, &client.DeleteOptions{PropagationPolicy: &propagation}); client.IgnoreNotFound(err) != nil {
		return errors.Wrap(err, "delete bootstrap job")
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

	// Disable and drop the subscriptions first so the slots go inactive. The
	// replica may already be gone, in which case the slots are simply inactive
	// and dropping them below still works.
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

			if err := r.execOnPod(ctx, pod, db, sql); err != nil {
				log.Info("could not drop subscription", "subscription", subscription, "error", err.Error())
			}
		}
	}

	for _, db := range databases {
		slot := logicalreplica.SlotName(replica, db)
		publication := logicalreplica.PublicationName(replica, db)

		sql := fmt.Sprintf(
			"SELECT pg_catalog.pg_drop_replication_slot(slot_name) FROM pg_catalog.pg_replication_slots WHERE slot_name = %s;",
			quoteLiteral(slot))
		if _, err := r.execOnPrimary(ctx, cr, "", sql); err != nil {
			return errors.Wrapf(err, "drop replication slot %q", slot)
		}

		if _, err := r.execOnPrimary(ctx, cr, db, fmt.Sprintf("DROP PUBLICATION IF EXISTS %q;", publication)); err != nil {
			return errors.Wrapf(err, "drop publication %q", publication)
		}
	}

	return nil
}

func quoteLiteral(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

func (r *PGClusterReconciler) logicalReplicaPod(ctx context.Context, cr *v2.PerconaPGCluster, replica string) (*corev1.Pod, error) {
	pods := &corev1.PodList{}
	if err := r.Client.List(ctx, pods, &client.ListOptions{
		Namespace: cr.Namespace,
		LabelSelector: labels.SelectorFromSet(map[string]string{
			naming.LabelCluster:         cr.Name,
			pNaming.LabelLogicalReplica: replica,
		}),
	}); err != nil {
		return nil, errors.Wrap(err, "list pods")
	}

	for i := range pods.Items {
		pod := &pods.Items[i]
		if pod.DeletionTimestamp == nil && pod.Status.Phase == corev1.PodRunning {
			return pod, nil
		}
	}

	return nil, errors.New("no running logical replica pod")
}

func (r *PGClusterReconciler) execOnPod(ctx context.Context, pod *corev1.Pod, database, sql string) error {
	exec := postgres.Executor(func(ctx context.Context, stdin io.Reader, stdout, stderr io.Writer, command ...string) error {
		return r.PodExec(ctx, pod.GetNamespace(), pod.GetName(), naming.ContainerDatabase, stdin, stdout, stderr, command...)
	})

	options := []string{"-t"}
	if database != "" {
		options = append(options, "--dbname="+database)
	}

	_, stderr, err := exec.Exec(ctx, strings.NewReader(sql), map[string]string{
		"ON_ERROR_STOP": "on",
		"QUIET":         "on",
	}, options)

	return errors.Wrapf(err, "execute query: stderr=%s", stderr)
}
