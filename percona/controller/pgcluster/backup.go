package pgcluster

import (
	"context"

	"github.com/pkg/errors"
	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/percona/percona-postgresql-operator/internal/naming"
	v2 "github.com/percona/percona-postgresql-operator/pkg/apis/pgv2.percona.com/v2"
)

func (r *PGClusterReconciler) reconcileBackupJobs(ctx context.Context, cr *v2.PerconaPGCluster) error {
	for _, repo := range cr.Spec.Backups.PGBackRest.Repos {
		backupJobs, err := listBackupJobs(ctx, r.Client, cr, repo.Name)
		if err != nil {
			return errors.Wrap(err, "failed to list backup jobs")
		}

		for _, job := range backupJobs.Items {
			if err := reconcileBackupJob(ctx, r.Client, cr, job, repo.Name); err != nil {
				return errors.Wrap(err, "failed to reconcile backup job")
			}
		}
	}
	return nil
}

func reconcileBackupJob(ctx context.Context, cl client.Client, cr *v2.PerconaPGCluster, job batchv1.Job, repoName string) error {
	pb, err := findPGBackup(ctx, cl, cr, job)
	if err != nil {
		return errors.Wrapf(err, "failed to find PerconaPGBackup for job %s", job.Name)
	}

	if pb == nil {
		if job.Labels[naming.LabelPGBackRestBackup] == string(naming.BackupManual) {
			// we shouldn't create pg-backup for manual backup jobs and should wait until it's pg-backup will have `.status.jobName`
			return nil
		}
		// Create PerconaPGBackup resource for backup job,
		// which was created without this resource.
		pb = &v2.PerconaPGBackup{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: job.Name + "-",
				Namespace:    job.Namespace,
				Annotations: map[string]string{
					v2.AnnotationPGBackrestBackupJobName: job.Name,
				},
			},
			Spec: v2.PerconaPGBackupSpec{
				PGCluster: cr.Name,
				RepoName:  repoName,
			},
		}

		err = cl.Create(ctx, pb)
		if err != nil {
			return errors.Wrapf(err, "failed to create PerconaPGBackup for job %s", job.Name)
		}
	}

	err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
		j := new(batchv1.Job)
		err := cl.Get(ctx, types.NamespacedName{Name: job.Name, Namespace: job.Namespace}, j)
		if err != nil {
			return err
		}
		j.OwnerReferences = nil
		if err := controllerutil.SetControllerReference(pb, j, cl.Scheme()); err != nil {
			return errors.Wrap(err, "failed to set controller reference")
		}

		return cl.Update(ctx, j)
	})
	if err != nil {
		return errors.Wrap(err, "failed to update backup job")
	}

	return nil
}

func findPGBackup(ctx context.Context, cl client.Reader, cr *v2.PerconaPGCluster, job batchv1.Job) (*v2.PerconaPGBackup, error) {
	pbList, err := listPGBackups(ctx, cl, cr)
	if err != nil {
		return nil, errors.Wrap(err, "failed to list backup jobs")
	}

	for _, pb := range pbList {
		pb := pb
		if pb.GetAnnotations()[v2.AnnotationPGBackrestBackupJobName] == job.Name || pb.Status.JobName == job.Name {
			return &pb, nil
		}
	}
	return nil, nil
}

func listPGBackups(ctx context.Context, cl client.Reader, cr *v2.PerconaPGCluster) ([]v2.PerconaPGBackup, error) {
	pbList := new(v2.PerconaPGBackupList)
	err := cl.List(ctx, pbList, &client.ListOptions{
		Namespace: cr.Namespace,
	})
	if err != nil {
		return nil, errors.Wrap(err, "failed to list backup jobs")
	}

	// we should not filter by label, because the user can create the resource without the label
	list := []v2.PerconaPGBackup{}
	for _, pgBackup := range pbList.Items {
		if pgBackup.Spec.PGCluster != cr.Name {
			continue
		}
		list = append(list, pgBackup)
	}

	return list, nil
}

func listBackupJobs(ctx context.Context, cl client.Reader, cr *v2.PerconaPGCluster, repoName string) (*batchv1.JobList, error) {
	backupJobs := new(batchv1.JobList)

	ls := naming.PGBackRestBackupJobLabels(cr.Name, repoName, "")
	delete(ls, naming.LabelPGBackRestBackup)

	err := cl.List(ctx, backupJobs, &client.ListOptions{
		Namespace:     cr.Namespace,
		LabelSelector: ls.AsSelector(),
	})
	if err != nil {
		return nil, errors.Wrap(err, "failed to list backup jobs")
	}

	return backupJobs, nil
}
