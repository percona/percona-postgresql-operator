package pgbackup

import (
	"context"
	"time"

	"github.com/pkg/errors"
	batchv1 "k8s.io/api/batch/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/percona/percona-postgresql-operator/internal/logging"
	"github.com/percona/percona-postgresql-operator/internal/naming"
	"github.com/percona/percona-postgresql-operator/percona/controller"
	v2 "github.com/percona/percona-postgresql-operator/pkg/apis/pgv2.percona.com/v2"
	"github.com/percona/percona-postgresql-operator/pkg/apis/postgres-operator.crunchydata.com/v1beta1"
)

const (
	// ControllerName is the name of the PerconaPGBackup controller
	PGBackupControllerName = "perconapgbackup-controller"
)

var ErrBackupJobNotFound = errors.New("backup Job not found")

// Reconciler holds resources for the PerconaPGBackup reconciler
type PGBackupReconciler struct {
	Client client.Client
}

// SetupWithManager adds the PerconaPGBackup controller to the provided runtime manager
func (r *PGBackupReconciler) SetupWithManager(mgr manager.Manager) error {
	return builder.ControllerManagedBy(mgr).For(&v2.PerconaPGBackup{}).Complete(r)
}

// +kubebuilder:rbac:groups=pgv2.percona.com,resources=perconapgbackups,verbs=get;list;watch
// +kubebuilder:rbac:groups=pgv2.percona.com,resources=perconapgbackups/status,verbs=patch;update
// +kubebuilder:rbac:groups=pgv2.percona.com,resources=perconapgclusters,verbs=get;list;create;update;patch;watch
// +kubebuilder:rbac:groups=postgres-operator.crunchydata.com,resources=postgresclusters,verbs=get;list;create;update;patch;watch
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch

func (r *PGBackupReconciler) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	log := logging.FromContext(ctx).WithValues("request", request)

	pgBackup := &v2.PerconaPGBackup{}
	if err := r.Client.Get(ctx, request.NamespacedName, pgBackup); err != nil {
		// NotFound cannot be fixed by requeuing so ignore it. During background
		// deletion, we receive delete events from cluster's dependents after
		// cluster is deleted.
		if err = client.IgnoreNotFound(err); err != nil {
			log.Error(err, "unable to fetch PerconaPGBackup")
		}
		return reconcile.Result{}, err
	}

	if pgBackup.Status.State == v2.BackupSucceeded || pgBackup.Status.State == v2.BackupFailed {
		return reconcile.Result{}, nil
	}

	pgCluster := &v2.PerconaPGCluster{}
	err := r.Client.Get(ctx, types.NamespacedName{Name: pgBackup.Spec.PGCluster, Namespace: request.Namespace}, pgCluster)
	if err != nil {
		return reconcile.Result{}, errors.Wrap(err, "get PostgresCluster")
	}

	switch pgBackup.Status.State {
	case v2.BackupNew:
		if pgCluster.Spec.Pause != nil && *pgCluster.Spec.Pause {
			log.Info("Can't start backup. PostgresCluster is paused", "pg-backup", pgBackup.Name, "cluster", pgCluster.Name)
			return reconcile.Result{RequeueAfter: time.Second * 5}, nil
		}
		if err := startBackup(ctx, r.Client, pgCluster, pgBackup); err != nil {
			return reconcile.Result{}, errors.Wrap(err, "start backup")
		}

		pgBackup.Status.State = v2.BackupStarting
		if err := r.Client.Status().Update(ctx, pgBackup); err != nil {
			return reconcile.Result{}, errors.Wrap(err, "update PGBackup status")
		}

		log.Info("Backup is starting")
		return reconcile.Result{}, nil
	case v2.BackupStarting:
		job, err := findBackupJob(ctx, r.Client, pgCluster, pgBackup)
		if err != nil {
			if errors.Is(err, ErrBackupJobNotFound) {
				log.Info("Waiting for backup to start")

				return reconcile.Result{RequeueAfter: time.Second * 5}, nil
			}
			return reconcile.Result{}, errors.Wrap(err, "find backup job")
		}

		pgBackup.Status.State = v2.BackupRunning
		pgBackup.Status.JobName = job.Name

		if err := r.Client.Status().Update(ctx, pgBackup); err != nil {
			return reconcile.Result{}, errors.Wrap(err, "update PGBackup status")
		}

		return reconcile.Result{}, nil
	case v2.BackupRunning:
		job := &batchv1.Job{}
		err := r.Client.Get(ctx, types.NamespacedName{Name: pgBackup.Status.JobName, Namespace: pgBackup.Namespace}, job)
		if err != nil {
			return reconcile.Result{}, errors.Wrap(err, "get backup job")
		}

		status := checkBackupJob(job)
		switch status {
		case v2.BackupFailed:
			log.Info("Backup failed")
		case v2.BackupSucceeded:
			log.Info("Backup succeeded")
		default:
			log.Info("Waiting for backup to complete")
			return reconcile.Result{RequeueAfter: time.Second * 5}, nil
		}

		pgBackup.Status.State = status
		if err := r.Client.Status().Update(ctx, pgBackup); err != nil {
			return reconcile.Result{}, errors.Wrap(err, "update PGBackup status")
		}

		return reconcile.Result{}, nil
	default:
		return reconcile.Result{}, nil
	}
}

func startBackup(ctx context.Context, c client.Client, pg *v2.PerconaPGCluster, pb *v2.PerconaPGBackup) error {
	orig := pg.DeepCopy()

	if pg.Annotations == nil {
		pg.Annotations = make(map[string]string)
	}
	pg.Annotations[naming.PGBackRestBackup] = pb.Name

	if pg.Spec.Backups.PGBackRest.Manual == nil {
		pg.Spec.Backups.PGBackRest.Manual = new(v1beta1.PGBackRestManualBackup)
	}

	pg.Spec.Backups.PGBackRest.Manual.RepoName = pb.Spec.RepoName
	pg.Spec.Backups.PGBackRest.Manual.Options = pb.Spec.Options

	if err := c.Patch(ctx, pg, client.MergeFrom(orig)); err != nil {
		return errors.Wrap(err, "annotate PostgresCluster")
	}

	return nil
}

func findBackupJob(ctx context.Context, c client.Client, pg *v2.PerconaPGCluster, pb *v2.PerconaPGBackup) (*batchv1.Job, error) {
	jobList := &batchv1.JobList{}
	selector := labels.SelectorFromSet(map[string]string{
		naming.LabelCluster:          pg.Name,
		naming.LabelPGBackRestBackup: "manual",
		naming.LabelPGBackRestRepo:   pb.Spec.RepoName,
	})
	err := c.List(ctx, jobList, client.InNamespace(pg.Namespace), client.MatchingLabelsSelector{Selector: selector})
	if err != nil {
		return nil, errors.Wrap(err, "get backup jobs")
	}

	for _, job := range jobList.Items {
		val, ok := job.Annotations[naming.PGBackRestBackup]
		if !ok {
			continue
		}

		if val != pb.Name {
			continue
		}

		return &job, nil
	}

	return nil, ErrBackupJobNotFound
}

func checkBackupJob(job *batchv1.Job) v2.PGBackupState {
	switch {
	case controller.JobCompleted(job):
		return v2.BackupSucceeded
	case controller.JobFailed(job):
		return v2.BackupFailed
	default:
		return v2.BackupRunning
	}
}
