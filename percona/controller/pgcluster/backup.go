package pgcluster

import (
	"context"

	"github.com/pkg/errors"
	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/percona/percona-postgresql-operator/internal/naming"
	"github.com/percona/percona-postgresql-operator/percona/controller"
	v2 "github.com/percona/percona-postgresql-operator/pkg/apis/pgv2.percona.com/v2"
)

// reconcileBackupJobs creates PerconaPGBackup resource for backup jobs created without this resource
func (r *PGClusterReconciler) reconcileBackupJobs(ctx context.Context, cr *v2.PerconaPGCluster) error {
	pbList := new(v2.PerconaPGBackupList)
	err := r.Client.List(ctx, pbList, client.InNamespace(cr.Namespace))
	if err != nil {
		return errors.Wrap(err, "failed to list backup jobs")
	}
	for _, pb := range pbList.Items {
		// We can't create resource if there is at least one in new or starting state.
		// These states are the only ones in which the resource doesn't have `.status.jobName`.
		if pb.Status.State == v2.BackupNew || pb.Status.State == v2.BackupStarting {
			return nil
		}
	}
	for _, repo := range cr.Spec.Backups.PGBackRest.Repos {
		backupJobs, err := listBackupJobs(ctx, r.Client, cr, repo.Name)
		if err != nil {
			return errors.Wrap(err, "failed to list backup jobs")
		}

		for _, job := range backupJobs.Items {
			job := job
			if controller.JobCompleted(&job) || controller.JobFailed(&job) {
				continue
			}
			hasPBResource := false
			for _, pb := range pbList.Items {
				if pb.GetAnnotations()[v2.AnnotationPGBackrestBackupJobName] == job.Name || pb.Status.JobName == job.Name {
					hasPBResource = true
					break
				}
			}
			if hasPBResource {
				continue
			}

			pb := &v2.PerconaPGBackup{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: job.Name + "-",
					Namespace:    job.Namespace,
					Annotations: map[string]string{
						v2.AnnotationPGBackrestBackupJobName: job.Name,
					},
				},
				Spec: v2.PerconaPGBackupSpec{
					PGCluster: cr.Name,
					RepoName:  repo.Name,
				},
			}

			err := r.Client.Create(ctx, pb)
			if err != nil {
				return errors.Wrapf(err, "failed to create PerconaPGBackup for job %s", job.Name)
			}
		}
	}
	return nil
}

func listBackupJobs(ctx context.Context, cl client.Reader, cr *v2.PerconaPGCluster, repoName string) (*batchv1.JobList, error) {
	backupJobs := new(batchv1.JobList)

	ls := naming.PGBackRestBackupJobLabels(cr.Name, repoName, "")
	delete(ls, naming.LabelPGBackRestBackup)

	err := cl.List(ctx, backupJobs, client.InNamespace(cr.Namespace), client.MatchingLabelsSelector{Selector: ls.AsSelector()})
	if err != nil {
		return nil, errors.Wrap(err, "failed to list backup jobs")
	}

	return backupJobs, nil
}
