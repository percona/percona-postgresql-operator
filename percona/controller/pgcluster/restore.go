package pgcluster

import (
	"context"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	"github.com/percona/percona-postgresql-operator/v2/internal/naming"
	pNaming "github.com/percona/percona-postgresql-operator/v2/percona/naming"
	v2 "github.com/percona/percona-postgresql-operator/v2/pkg/apis/pgv2.percona.com/v2"
)

// createBootstrapRestoreObject creates a PerconaPGRestore object for the bootstrap restore
func (r *PGClusterReconciler) createBootstrapRestoreObject(ctx context.Context, cr *v2.PerconaPGCluster) error {
	if cr.Spec.DataSource == nil || (cr.Spec.DataSource.PGBackRest == nil &&
		cr.Spec.DataSource.PostgresCluster == nil &&
		cr.Spec.DataSource.Volumes == nil) {
		return nil
	}

	pgrName := cr.Name + "-bootstrap"
	repoName := ""

	if cr.Spec.DataSource.PGBackRest != nil {
		repoName = cr.Spec.DataSource.PGBackRest.Repo.Name
	}
	if cr.Spec.DataSource.PostgresCluster != nil {
		repoName = cr.Spec.DataSource.PostgresCluster.RepoName
	}

	pgr := &v2.PerconaPGRestore{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pgrName,
			Namespace: cr.Namespace,
			Annotations: map[string]string{
				pNaming.AnnotationClusterBootstrapRestore: "true",
			},
		},
		Spec: v2.PerconaPGRestoreSpec{
			PGCluster: cr.Name,
			RepoName:  ptr.To(repoName),
		},
	}
	if cr.CompareVersion("2.6.0") >= 0 && cr.Spec.Metadata != nil {
		pgr.Annotations = naming.Merge(cr.Spec.Metadata.Annotations, pgr.Annotations)
		pgr.Labels = cr.Spec.Metadata.Labels
	}

	err := r.Client.Create(ctx, pgr)
	if err != nil && !k8serrors.IsAlreadyExists(err) {
		return err
	}

	return nil
}
