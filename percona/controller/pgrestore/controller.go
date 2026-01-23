package pgrestore

import (
	"context"
	"time"

	"github.com/pkg/errors"
	"go.opentelemetry.io/otel/trace"
	batchv1 "k8s.io/api/batch/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/percona/percona-postgresql-operator/v2/internal/logging"
	"github.com/percona/percona-postgresql-operator/v2/internal/naming"
	"github.com/percona/percona-postgresql-operator/v2/percona/controller"
	"github.com/percona/percona-postgresql-operator/v2/percona/controller/pgrestore/snapshot"
	pNaming "github.com/percona/percona-postgresql-operator/v2/percona/naming"
	v2 "github.com/percona/percona-postgresql-operator/v2/pkg/apis/pgv2.percona.com/v2"
	"github.com/percona/percona-postgresql-operator/v2/pkg/apis/postgres-operator.crunchydata.com/v1beta1"
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
	return builder.ControllerManagedBy(mgr).For(&v2.PerconaPGRestore{}).Complete(r)
}

// +kubebuilder:rbac:groups=pgv2.percona.com,resources=perconapgrestores,verbs=get;list;watch;create;patch
// +kubebuilder:rbac:groups=pgv2.percona.com,resources=perconapgrestores/status,verbs=patch;update
// +kubebuilder:rbac:groups=pgv2.percona.com,resources=perconapgclusters,verbs=get;list;create;update;patch;watch
// +kubebuilder:rbac:groups=postgres-operator.crunchydata.com,resources=postgresclusters,verbs=get;list;create;update;patch;watch
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch

func (r *PGRestoreReconciler) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	log := logging.FromContext(ctx).WithValues("request", request)

	pgRestore := &v2.PerconaPGRestore{}
	if err := r.Client.Get(ctx, request.NamespacedName, pgRestore); err != nil {
		// NotFound cannot be fixed by requeuing so ignore it. During background
		// deletion, we receive delete events from cluster's dependents after
		// cluster is deleted.
		if err = client.IgnoreNotFound(err); err != nil {
			log.Error(err, "unable to fetch perconapgrestore")
		}
		return reconcile.Result{}, err
	}

	pgCluster := &v2.PerconaPGCluster{}
	err := r.Client.Get(ctx, types.NamespacedName{Name: pgRestore.Spec.PGCluster, Namespace: request.Namespace}, pgCluster)
	if err != nil {
		return reconcile.Result{}, errors.Wrap(err, "get PostgresCluster")
	}

	if pgRestore.Spec.VolumeSnapshotName != "" {
		// Delegate to snapshot restore reconciliation
		return snapshot.Reconcile(ctx, r.Client, pgCluster, pgRestore)
	}

	if pgRestore.DeletionTimestamp != nil {
		if err := runFinalizers(ctx, r.Client, pgRestore); err != nil {
			return reconcile.Result{}, errors.Wrap(err, "failed to run finalizers")
		}
		return reconcile.Result{}, nil
	}

	if pgRestore.Status.State == v2.RestoreSucceeded || pgRestore.Status.State == v2.RestoreFailed {
		return reconcile.Result{}, nil
	}

	switch pgRestore.Status.State {
	case v2.RestoreNew:
		if restore := pgCluster.Spec.Backups.PGBackRest.Restore; restore != nil && *restore.Enabled {
			log.Info("Waiting for another restore to finish")
			return reconcile.Result{RequeueAfter: time.Second * 5}, nil
		}

		if _, ok := pgRestore.Annotations[pNaming.AnnotationClusterBootstrapRestore]; !ok {
			if err := startRestore(ctx, r.Client, pgCluster, pgRestore); err != nil {
				return reconcile.Result{}, errors.Wrap(err, "start restore")
			}
		}

		pgRestore.Status.State = v2.RestoreStarting
		if err := r.Client.Status().Update(ctx, pgRestore); err != nil {
			return reconcile.Result{}, errors.Wrap(err, "update PGRestore status")
		}

		return reconcile.Result{}, nil
	case v2.RestoreStarting:
		job := &batchv1.Job{}
		err := r.Client.Get(ctx, types.NamespacedName{Name: pgCluster.Name + "-pgbackrest-restore", Namespace: pgCluster.Namespace}, job)
		if err != nil {
			if k8serrors.IsNotFound(err) {
				log.Info("Waiting for restore to start")
				return reconcile.Result{RequeueAfter: time.Second * 5}, nil
			}
			return reconcile.Result{}, errors.Wrap(err, "get restore job")
		}

		pgRestore.Status.State = v2.RestoreRunning
		if err := r.Client.Status().Update(ctx, pgRestore); err != nil {
			return reconcile.Result{}, errors.Wrap(err, "update PGRestore status")
		}

		return reconcile.Result{}, nil
	case v2.RestoreRunning:
		job := &batchv1.Job{}
		err := r.Client.Get(ctx, types.NamespacedName{Name: pgCluster.Name + "-pgbackrest-restore", Namespace: pgCluster.Namespace}, job)
		if err != nil {
			return reconcile.Result{}, errors.Wrap(err, "get restore job")
		}

		status := checkRestoreJob(job)
		switch status {
		case v2.RestoreFailed:
			log.Info("Restore failed")
		case v2.RestoreSucceeded:
			log.Info("Restore succeeded")
			pgRestore.Status.CompletedAt = job.Status.CompletionTime
		default:
			log.Info("Waiting for restore to complete")
			return reconcile.Result{RequeueAfter: time.Second * 5}, nil
		}

		if _, ok := pgRestore.Annotations[pNaming.AnnotationClusterBootstrapRestore]; !ok {
			if err := disableRestore(ctx, r.Client, pgCluster, pgRestore); err != nil {
				return reconcile.Result{}, errors.Wrap(err, "disable restore")
			}
		}

		// Don't add code after the status update.
		// Otherwise, it's possible to get a problem like this: https://perconadev.atlassian.net/browse/K8SPG-509
		pgRestore.Status.State = status
		if err := r.Client.Status().Update(ctx, pgRestore); err != nil {
			return reconcile.Result{}, errors.Wrap(err, "update pgRestore status")
		}
		return reconcile.Result{}, nil
	default:
		return reconcile.Result{}, nil
	}
}

func runFinalizers(ctx context.Context, c client.Client, pr *v2.PerconaPGRestore) error {
	pg := new(v2.PerconaPGCluster)
	if err := c.Get(ctx, types.NamespacedName{Name: pr.Spec.PGCluster, Namespace: pr.Namespace}, pg); err != nil {
		if k8serrors.IsNotFound(err) {
			pg = nil
		} else {
			return errors.Wrap(err, "get PostgresCluster")
		}
	}

	finalizers := map[string]controller.FinalizerFunc[*v2.PerconaPGRestore]{
		pNaming.FinalizerDeleteRestore: func(ctx context.Context, pr *v2.PerconaPGRestore) error {
			if pg == nil {
				return nil
			}
			return disableRestore(ctx, c, pg, pr)
		},
	}

	for finalizer, f := range finalizers {
		if _, err := controller.RunFinalizer(ctx, c, pr, finalizer, f); err != nil {
			return errors.Wrapf(err, "run finalizer %s", finalizer)
		}
	}
	return nil
}

func ensureFinalizers(ctx context.Context, cl client.Client, pr *v2.PerconaPGRestore) error {
	orig := pr.DeepCopy()

	finalizers := []string{pNaming.FinalizerDeleteRestore}
	finalizersChanged := false
	for _, f := range finalizers {
		if controllerutil.AddFinalizer(pr, f) {
			finalizersChanged = true
		}
	}
	if !finalizersChanged {
		return nil
	}

	if err := cl.Patch(ctx, pr, client.MergeFrom(orig)); err != nil {
		return errors.Wrap(err, "remove finalizers")
	}
	return nil
}

func startRestore(ctx context.Context, c client.Client, pg *v2.PerconaPGCluster, pr *v2.PerconaPGRestore) error {
	orig := pg.DeepCopy()

	if pg.Annotations == nil {
		pg.Annotations = make(map[string]string)
	}
	pg.Annotations[naming.PGBackRestRestore] = pr.Name

	postgresCluster := new(v1beta1.PostgresCluster)
	if err := c.Get(ctx, client.ObjectKeyFromObject(pg), postgresCluster); err != nil {
		return errors.Wrap(err, "get PostgresCluster")
	}

	origPostgres := postgresCluster.DeepCopy()

	postgresCluster.Status.PGBackRest.Restore = new(v1beta1.PGBackRestJobStatus)

	if err := c.Status().Patch(ctx, postgresCluster, client.MergeFrom(origPostgres)); err != nil {
		return errors.Wrap(err, "patch PGCluster")
	}

	if pg.Spec.Backups.PGBackRest.Restore == nil {
		pg.Spec.Backups.PGBackRest.Restore = &v1beta1.PGBackRestRestore{
			PostgresClusterDataSource: &v1beta1.PostgresClusterDataSource{},
		}
	}

	tvar := true
	pg.Spec.Backups.PGBackRest.Restore.Enabled = &tvar
	pg.Spec.Backups.PGBackRest.Restore.RepoName = ptr.Deref(pr.Spec.RepoName, "")
	pg.Spec.Backups.PGBackRest.Restore.Options = pr.Spec.Options

	if err := c.Patch(ctx, pg, client.MergeFrom(orig)); err != nil {
		return errors.Wrap(err, "patch PGCluster")
	}

	if err := ensureFinalizers(ctx, c, pr); err != nil {
		return errors.Wrap(err, "ensure restore finalizers")
	}

	return nil
}

func disableRestore(ctx context.Context, c client.Client, pg *v2.PerconaPGCluster, pr *v2.PerconaPGRestore) error {
	if pr.Status.State == v2.RestoreSucceeded || pr.Status.State == v2.RestoreFailed {
		return nil
	}

	orig := pg.DeepCopy()

	if pg.Spec.Backups.PGBackRest.Restore == nil {
		pg.Spec.Backups.PGBackRest.Restore = &v1beta1.PGBackRestRestore{
			PostgresClusterDataSource: &v1beta1.PostgresClusterDataSource{},
		}
	}

	fvar := false
	pg.Spec.Backups.PGBackRest.Restore.Enabled = &fvar

	delete(pg.Annotations, naming.LabelPGBackRestRestore)

	if err := c.Patch(ctx, pg, client.MergeFrom(orig)); err != nil {
		return errors.Wrap(err, "patch PGCluster")
	}

	return nil
}

func checkRestoreJob(job *batchv1.Job) v2.PGRestoreState {
	switch {
	case controller.JobCompleted(job):
		return v2.RestoreSucceeded
	case controller.JobFailed(job):
		return v2.RestoreFailed
	default:
		return v2.RestoreRunning
	}
}
