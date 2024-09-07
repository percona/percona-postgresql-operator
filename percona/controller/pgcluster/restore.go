package pgcluster

import (
	"context"
	"strings"

	"github.com/pkg/errors"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	v2 "github.com/percona/percona-postgresql-operator/pkg/apis/pgv2.percona.com/v2"
	"github.com/percona/percona-postgresql-operator/pkg/apis/postgres-operator.crunchydata.com/v1beta1"
)

// createBootstrapRestoreObject creates a PerconaPGRestore object for the bootstrap restore
func (r *PGClusterReconciler) createBootstrapRestoreObject(ctx context.Context, cr *v2.PerconaPGCluster) error {
	if cr.Spec.DataSource == nil || (cr.Spec.DataSource.PGBackRest == nil &&
		cr.Spec.DataSource.PostgresCluster == nil &&
		cr.Spec.DataSource.Volumes == nil) {
		return nil
	}

	if cr.Status.State != v2.AppStateInit {
		return nil
	}

	pgc := &v1beta1.PostgresCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cr.Name,
			Namespace: cr.Namespace,
		},
	}
	err := r.Client.Get(ctx, types.NamespacedName{Name: cr.Name, Namespace: cr.Namespace}, pgc)
	if err == nil {
		return nil
	}

	if pgc.Status.PGBackRest == nil || pgc.Status.PGBackRest.Restore == nil {
		return nil
	}

	if !strings.Contains(pgc.Status.PGBackRest.Restore.ID, "bootstrap") || pgc.Status.PGBackRest.Restore.Finished {
		return nil
	}

	pgr := &v2.PerconaPGRestore{}
	pgrName := cr.Name + "-bootstrap"

	err = r.Client.Get(ctx, types.NamespacedName{Name: pgrName, Namespace: cr.Namespace}, pgr)
	if err != nil && !k8serrors.IsNotFound(err) {
		return errors.Wrapf(err, "failed to get PgRestore %s", pgrName)
	}

	// means pgr is found, no need to create
	if err == nil {
		return nil
	}

	repoName := ""

	if cr.Spec.DataSource.PGBackRest != nil {
		repoName = cr.Spec.DataSource.PGBackRest.Repo.Name
	}
	if cr.Spec.DataSource.PostgresCluster != nil {
		repoName = cr.Spec.DataSource.PostgresCluster.RepoName
	}

	pgr = &v2.PerconaPGRestore{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pgrName,
			Namespace: cr.Namespace,
		},
		Spec: v2.PerconaPGRestoreSpec{
			PGCluster: cr.Name,
			RepoName:  repoName,
		},
		Status: v2.PerconaPGRestoreStatus{
			State: v2.RestoreStarting,
		},
	}

	err = r.Client.Create(ctx, pgr)
	if err != nil {
		return errors.Wrap(err, "failed to create PerconaPGRestore for bootstrap restore")
	}

	return nil
}
