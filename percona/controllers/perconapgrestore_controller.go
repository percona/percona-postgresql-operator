package controllers

import (
	"context"
	"time"

	"github.com/pkg/errors"
	"go.opentelemetry.io/otel/trace"
	batchv1 "k8s.io/api/batch/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/percona/percona-postgresql-operator/internal/logging"
	"github.com/percona/percona-postgresql-operator/internal/naming"
	"github.com/percona/percona-postgresql-operator/pkg/apis/pg.percona.com/v2beta1"
	"github.com/percona/percona-postgresql-operator/pkg/apis/postgres-operator.crunchydata.com/v1beta1"
)

const (
	// ControllerName is the name of the perconapgrestore controller
	PGRestoreControllerName = "perconapgrestore-controller"
)

// Reconciler holds resources for the PerconaPGRestore reconciler
type PGRestoreReconciler struct {
	Client   client.Client
	Owner    client.FieldOwner
	Recorder record.EventRecorder
	Tracer   trace.Tracer
}

// SetupWithManager adds the perconapgrestore controller to the provided runtime manager
func (r *PGRestoreReconciler) SetupWithManager(mgr manager.Manager) error {
	return builder.ControllerManagedBy(mgr).For(&v2beta1.PerconaPGRestore{}).Complete(r)
}

// +kubebuilder:rbac:groups=pg.percona.com,resources=perconapgrestores,verbs=get;list;watch
// +kubebuilder:rbac:groups=pg.percona.com,resources=perconapgrestores/status,verbs=patch;update
// +kubebuilder:rbac:groups=pg.percona.com,resources=perconapgclusters,verbs=get;list;create;update;patch;watch
// +kubebuilder:rbac:groups=postgres-operator.crunchydata.com,resources=postgresclusters,verbs=get;list;create;update;patch;watch
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch

func (r *PGRestoreReconciler) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	log := logging.FromContext(ctx).WithValues("request", request)

	pgRestore := &v2beta1.PerconaPGRestore{}
	if err := r.Client.Get(ctx, request.NamespacedName, pgRestore); err != nil {
		// NotFound cannot be fixed by requeuing so ignore it. During background
		// deletion, we receive delete events from cluster's dependents after
		// cluster is deleted.
		if err = client.IgnoreNotFound(err); err != nil {
			log.Error(err, "unable to fetch perconapgrestore")
		}
		return reconcile.Result{}, err
	}

	if pgRestore.Status.State == v2beta1.RestoreSucceeded || pgRestore.Status.State == v2beta1.RestoreFailed {
		return reconcile.Result{}, nil
	}

	pgCluster := &v2beta1.PerconaPGCluster{}
	err := r.Client.Get(ctx, types.NamespacedName{Name: pgRestore.Spec.PGCluster, Namespace: request.Namespace}, pgCluster)
	if err != nil {
		return reconcile.Result{}, errors.Wrap(err, "get PostgresCluster")
	}

	switch pgRestore.Status.State {
	case v2beta1.RestoreNew:
		if restore := pgCluster.Spec.Backups.PGBackRest.Restore; restore != nil && *restore.Enabled {
			log.Info("Waiting for another restore to finish")
			return reconcile.Result{RequeueAfter: time.Second * 5}, nil
		}

		pgRestore.Status.State = v2beta1.RestoreStarting
		if err := r.Client.Status().Update(ctx, pgRestore); err != nil {
			return reconcile.Result{}, errors.Wrap(err, "update PGRestore status")
		}

		if err := startRestore(ctx, r.Client, pgCluster, pgRestore); err != nil {
			return reconcile.Result{}, errors.Wrap(err, "start restore")
		}

		log.Info("Restore is starting")
		return reconcile.Result{}, nil
	case v2beta1.RestoreStarting:
		job := &batchv1.Job{}
		err := r.Client.Get(ctx, types.NamespacedName{Name: pgCluster.Name + "-pgbackrest-restore", Namespace: pgCluster.Namespace}, job)
		if err != nil {
			if k8serrors.IsNotFound(err) {
				log.Info("Waiting for restore to start")
				return reconcile.Result{RequeueAfter: time.Second * 5}, nil
			}
			return reconcile.Result{}, errors.Wrap(err, "get restore job")
		}

		pgRestore.Status.State = v2beta1.RestoreRunning
		if err := r.Client.Status().Update(ctx, pgRestore); err != nil {
			return reconcile.Result{}, errors.Wrap(err, "update PGRestore status")
		}

		return reconcile.Result{}, nil
	case v2beta1.RestoreRunning:
		job := &batchv1.Job{}
		err := r.Client.Get(ctx, types.NamespacedName{Name: pgCluster.Name + "-pgbackrest-restore", Namespace: pgCluster.Namespace}, job)
		if err != nil {
			return reconcile.Result{}, errors.Wrap(err, "get restore job")
		}

		status := checkRestoreJob(job)
		switch status {
		case v2beta1.RestoreFailed:
			log.Info("Restore failed")
		case v2beta1.RestoreSucceeded:
			log.Info("Restore succeeded")
		default:
			log.Info("Waiting for restore to complete")
			return reconcile.Result{RequeueAfter: time.Second * 5}, nil
		}

		pgRestore.Status.State = status
		if err := r.Client.Status().Update(ctx, pgRestore); err != nil {
			return reconcile.Result{}, errors.Wrap(err, "update pgRestore status")
		}

		if err := disableRestore(ctx, r.Client, pgCluster); err != nil {
			return reconcile.Result{}, errors.Wrap(err, "disable restore")
		}

		return reconcile.Result{}, nil
	default:
		return reconcile.Result{}, nil
	}
}

func startRestore(ctx context.Context, c client.Client, pg *v2beta1.PerconaPGCluster, pr *v2beta1.PerconaPGRestore) error {
	orig := pg.DeepCopy()

	if pg.Annotations == nil {
		pg.Annotations = make(map[string]string)
	}
	pg.Annotations[naming.PGBackRestRestore] = pr.Name

	if pg.Spec.Backups.PGBackRest.Restore == nil {
		pg.Spec.Backups.PGBackRest.Restore = &v1beta1.PGBackRestRestore{
			PostgresClusterDataSource: &v1beta1.PostgresClusterDataSource{},
		}
	}

	tvar := true
	pg.Spec.Backups.PGBackRest.Restore.Enabled = &tvar
	pg.Spec.Backups.PGBackRest.Restore.RepoName = pr.Spec.RepoName
	pg.Spec.Backups.PGBackRest.Restore.Options = pr.Spec.Options

	if err := c.Patch(ctx, pg, client.MergeFrom(orig)); err != nil {
		return errors.Wrap(err, "patch PGCluster")
	}

	return nil
}

func disableRestore(ctx context.Context, c client.Client, pg *v2beta1.PerconaPGCluster) error {
	orig := pg.DeepCopy()

	if pg.Spec.Backups.PGBackRest.Restore == nil {
		pg.Spec.Backups.PGBackRest.Restore = &v1beta1.PGBackRestRestore{
			PostgresClusterDataSource: &v1beta1.PostgresClusterDataSource{},
		}
	}

	fvar := false
	pg.Spec.Backups.PGBackRest.Restore.Enabled = &fvar

	if err := c.Patch(ctx, pg, client.MergeFrom(orig)); err != nil {
		return errors.Wrap(err, "patch PGCluster")
	}

	return nil
}

func checkRestoreJob(job *batchv1.Job) v2beta1.PGRestoreState {
	switch {
	case jobCompleted(job):
		return v2beta1.RestoreSucceeded
	case jobFailed(job):
		return v2beta1.RestoreFailed
	default:
		return v2beta1.RestoreRunning
	}
}
