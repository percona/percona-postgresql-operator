package utils

import (
	"context"

	"github.com/pkg/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/percona/percona-postgresql-operator/v2/internal/naming"
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

	if val, ok := r.pgCluster.GetAnnotations()[naming.PGBackRestRestore]; ok && val == r.pgRestore.Name {
		return nil // already started
	}

	if r.pgCluster.Annotations == nil {
		r.pgCluster.Annotations = make(map[string]string)
	}
	r.pgCluster.Annotations[naming.PGBackRestRestore] = r.pgRestore.Name

	postgresCluster := new(v1beta1.PostgresCluster)
	if err := r.Get(ctx, client.ObjectKeyFromObject(r.pgCluster), postgresCluster); err != nil {
		return errors.Wrap(err, "get PostgresCluster")
	}

	origPostgres := postgresCluster.DeepCopy()

	if postgresCluster.Status.PGBackRest == nil {
		postgresCluster.Status.PGBackRest = &v1beta1.PGBackRestStatus{}
	}

	postgresCluster.Status.PGBackRest.Restore = &v1beta1.PGBackRestJobStatus{}

	if err := r.Status().Patch(ctx, postgresCluster, client.MergeFrom(origPostgres)); err != nil {
		return errors.Wrap(err, "patch PostgresCluster status failed trying to initialize PGBackRest restore status")
	}

	if r.pgCluster.Spec.Backups.PGBackRest.Restore == nil {
		r.pgCluster.Spec.Backups.PGBackRest.Restore = &v1beta1.PGBackRestRestore{
			PostgresClusterDataSource: &v1beta1.PostgresClusterDataSource{},
		}
	}

	r.pgCluster.Spec.Backups.PGBackRest.Restore.Enabled = ptr.To(true)
	r.pgCluster.Spec.Backups.PGBackRest.Restore.RepoName = ptr.Deref(r.pgRestore.Spec.RepoName, "")
	r.pgCluster.Spec.Backups.PGBackRest.Restore.Options = r.pgRestore.Spec.Options

	if err := r.Patch(ctx, r.pgCluster, client.MergeFrom(orig)); err != nil {
		return errors.Wrap(err, "patch PostgresCluster status failed trying to start restore")
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
	delete(r.pgCluster.Annotations, naming.PGBackRestRestore)

	if err := r.Patch(ctx, r.pgCluster, client.MergeFrom(orig)); err != nil {
		return errors.Wrap(err, "patch PGCluster")
	}

	return nil
}

func (r *PGBackRestRestore) ObserveStatus(ctx context.Context) (v2.PGRestoreState, *metav1.Time, error) {
	cluster := &v2.PerconaPGCluster{}
	if err := r.Get(ctx, client.ObjectKeyFromObject(r.pgCluster), cluster); err != nil {
		return v2.RestoreStarting, nil, errors.Wrap(err, "get PerconaPGCluster")
	}

	if cluster.Status.PGBackRest == nil || cluster.Status.PGBackRest.Restore == nil {
		return v2.RestoreStarting, nil, nil
	}
	restoreStatus := cluster.Status.PGBackRest.Restore

	switch {
	case restoreStatus.Finished && restoreStatus.Succeeded > 0:
		return v2.RestoreSucceeded, restoreStatus.CompletionTime, nil
	case restoreStatus.Finished && restoreStatus.Failed > 0:
		return v2.RestoreFailed, nil, nil
	default:
		return v2.RestoreRunning, nil, nil
	}
}
