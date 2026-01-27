package utils

import (
	"context"

	"github.com/pkg/errors"
	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/percona/percona-postgresql-operator/v2/internal/naming"
	"github.com/percona/percona-postgresql-operator/v2/percona/controller"
	v2 "github.com/percona/percona-postgresql-operator/v2/pkg/apis/pgv2.percona.com/v2"
	"github.com/percona/percona-postgresql-operator/v2/pkg/apis/postgres-operator.crunchydata.com/v1beta1"
)

type PGBackRestRestore struct {
	client.Client

	pgCluster *v2.PerconaPGCluster
	pgRestore *v2.PerconaPGRestore
}

func NewPGBackRestRestore(c client.Client, pgCluster *v2.PerconaPGCluster, pgRestore *v2.PerconaPGRestore) *PGBackRestRestore {
	return &PGBackRestRestore{
		Client:    c,
		pgCluster: pgCluster,
		pgRestore: pgRestore,
	}
}

func (r *PGBackRestRestore) Start(ctx context.Context) error {
	orig := r.pgCluster.DeepCopy()

	if r.pgCluster.Annotations == nil {
		r.pgCluster.Annotations = make(map[string]string)
	}
	r.pgCluster.Annotations[naming.PGBackRestRestore] = r.pgRestore.Name

	postgresCluster := new(v1beta1.PostgresCluster)
	if err := r.Client.Get(ctx, client.ObjectKeyFromObject(r.pgCluster), postgresCluster); err != nil {
		return errors.Wrap(err, "get PostgresCluster")
	}

	origPostgres := postgresCluster.DeepCopy()

	postgresCluster.Status.PGBackRest.Restore = new(v1beta1.PGBackRestJobStatus)

	if err := r.Client.Status().Patch(ctx, postgresCluster, client.MergeFrom(origPostgres)); err != nil {
		return errors.Wrap(err, "patch PGCluster")
	}

	if r.pgCluster.Spec.Backups.PGBackRest.Restore == nil {
		r.pgCluster.Spec.Backups.PGBackRest.Restore = &v1beta1.PGBackRestRestore{
			PostgresClusterDataSource: &v1beta1.PostgresClusterDataSource{},
		}
	}

	r.pgCluster.Spec.Backups.PGBackRest.Restore.Enabled = ptr.To(true)
	r.pgCluster.Spec.Backups.PGBackRest.Restore.RepoName = ptr.Deref(r.pgRestore.Spec.RepoName, "")
	r.pgCluster.Spec.Backups.PGBackRest.Restore.Options = r.pgRestore.Spec.Options

	if err := r.Client.Patch(ctx, r.pgCluster, client.MergeFrom(orig)); err != nil {
		return errors.Wrap(err, "patch PGCluster")
	}

	return nil
}

func (r *PGBackRestRestore) DisableRestore(ctx context.Context) error {
	if r.pgRestore.Status.State == v2.RestoreSucceeded || r.pgRestore.Status.State == v2.RestoreFailed {
		return nil
	}

	orig := r.pgCluster.DeepCopy()

	if r.pgCluster.Spec.Backups.PGBackRest.Restore == nil {
		r.pgCluster.Spec.Backups.PGBackRest.Restore = &v1beta1.PGBackRestRestore{
			PostgresClusterDataSource: &v1beta1.PostgresClusterDataSource{},
		}
	}

	r.pgCluster.Spec.Backups.PGBackRest.Restore.Enabled = ptr.To(false)
	delete(r.pgCluster.Annotations, naming.LabelPGBackRestRestore)

	if err := r.Client.Patch(ctx, r.pgCluster, client.MergeFrom(orig)); err != nil {
		return errors.Wrap(err, "patch PGCluster")
	}

	return nil
}

func (r *PGBackRestRestore) ObserveStatus(ctx context.Context) (v2.PGRestoreState, *metav1.Time, error) {
	job := &batchv1.Job{}
	err := r.Client.Get(ctx, types.NamespacedName{Name: r.pgCluster.Name + "-pgbackrest-restore", Namespace: r.pgCluster.Namespace}, job)
	if err != nil {
		return v2.RestoreNew, nil, errors.Wrap(err, "get restore job")
	}
	return checkRestoreJob(job), job.Status.CompletionTime, nil
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
