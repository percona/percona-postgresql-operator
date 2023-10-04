package pgcluster

import (
	"context"

	"github.com/pkg/errors"
	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/percona/percona-postgresql-operator/internal/naming"
	v2 "github.com/percona/percona-postgresql-operator/pkg/apis/pgv2.percona.com/v2"
)

func (r *PGClusterReconciler) reconcileBackupJobs(ctx context.Context, cr *v2.PerconaPGCluster) error {
	pbList := new(v2.PerconaPGBackupList)
	err := r.Client.List(ctx, pbList, client.InNamespace(cr.Namespace))
	if err != nil {
		return errors.Wrap(err, "failed to list backup jobs")
	}
	canCreatePB := true
	for _, pb := range pbList.Items {
		// We can't create resource if there is at least one in new or starting state.
		// These states are the only ones in which the resource doesn't have `.status.jobName`.
		if pb.Status.State == v2.BackupNew || pb.Status.State == v2.BackupStarting {
			canCreatePB = false
			break
		}
	}
	for _, repo := range cr.Spec.Backups.PGBackRest.Repos {
		backupJobs, err := listBackupJobs(ctx, r.Client, cr, repo.Name)
		if err != nil {
			return errors.Wrap(err, "failed to list backup jobs")
		}

		shouldCreatePB := canCreatePB
		for _, job := range backupJobs.Items {
			job := job
			for _, pb := range pbList.Items {
				pb := pb
				if pb.GetAnnotations()[v2.AnnotationPGBackrestBackupJobName] == job.Name || pb.Status.JobName == job.Name {
					shouldCreatePB = false

					if err := controllerutil.SetControllerReference(&pb, &job,
						r.Client.Scheme()); err != nil {
						return errors.WithStack(err)
					}
					break
				}
			}

			if shouldCreatePB {
				// Create PerconaPGBackup resource for backup job,
				// which was created without this resource.
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
				if err := controllerutil.SetControllerReference(pb, &job,
					r.Client.Scheme()); err != nil {
					return errors.WithStack(err)
				}
			}

			err := r.Client.Update(ctx, &job)
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
