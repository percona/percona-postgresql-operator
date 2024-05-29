package pgbackup

import (
	"context"
	"path"
	"time"

	"github.com/pkg/errors"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	"github.com/percona/percona-postgresql-operator/internal/logging"
	"github.com/percona/percona-postgresql-operator/internal/naming"
	"github.com/percona/percona-postgresql-operator/percona/clientcmd"
	"github.com/percona/percona-postgresql-operator/percona/controller"
	"github.com/percona/percona-postgresql-operator/percona/pgbackrest"
	"github.com/percona/percona-postgresql-operator/percona/watcher"
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

	ExternalChan chan event.GenericEvent
	PodExec      func(
		namespace, pod, container string,
		stdin io.Reader, stdout, stderr io.Writer, command ...string,
	) error
}

// SetupWithManager adds the PerconaPGBackup controller to the provided runtime manager
func (r *PGBackupReconciler) SetupWithManager(mgr manager.Manager) error {
	return (builder.ControllerManagedBy(mgr).
		For(&v2.PerconaPGBackup{}).
		WatchesRawSource(source.Channel(r.ExternalChan, &handler.EnqueueRequestForObject{})).
		Complete(r))
}

// +kubebuilder:rbac:groups=pgv2.percona.com,resources=perconapgbackups,verbs=create;get;list;watch;update
// +kubebuilder:rbac:groups=pgv2.percona.com,resources=perconapgbackups/status,verbs=create;patch;update
// +kubebuilder:rbac:groups=pgv2.percona.com,resources=perconapgclusters,verbs=get;list;create;update;patch;watch
// +kubebuilder:rbac:groups=pgv2.percona.com,resources=perconapgbackups/finalizers,verbs=update
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

	pgBackup.Default()

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

		// start backup only if backup job doesn't exist
		_, err := findBackupJob(ctx, r.Client, pgCluster, pgBackup)
		if err != nil {
			if !errors.Is(err, ErrBackupJobNotFound) {
				return reconcile.Result{}, errors.Wrap(err, "find backup job")
			}
			if a := pgCluster.Annotations[v2.AnnotationBackupInProgress]; a != "" && a != pgBackup.Name {
				log.Info("Can't start backup. Previous backup is still in progress", "pg-backup", pgBackup.Name, "cluster", pgCluster.Name)
				return reconcile.Result{RequeueAfter: time.Second * 5}, nil
			}
			if err := startBackup(ctx, r.Client, pgBackup); err != nil {
				return reconcile.Result{}, errors.Wrap(err, "failed to start backup")
			}
		}

		pgBackup.Status.Destination = getDestination(pgCluster, pgBackup)
		pgBackup.Status.Image = pgCluster.Spec.Backups.PGBackRest.Image

		repo := getRepo(pgCluster, pgBackup)
		if repo == nil {
			return reconcile.Result{}, errors.Errorf("%s repo not defined", pgBackup.Spec.RepoName)
		}

		pgBackup.Status.Repo = repo
		switch {
		case repo.S3 != nil:
			pgBackup.Status.StorageType = v2.PGBackupStorageTypeS3
		case repo.Azure != nil:
			pgBackup.Status.StorageType = v2.PGBackupStorageTypeAzure
		case repo.GCS != nil:
			pgBackup.Status.StorageType = v2.PGBackupStorageTypeGCS
		default:
			pgBackup.Status.StorageType = v2.PGBackupStorageTypeFilesystem
		}

		pgBackup.Status.State = v2.BackupStarting
		if err := r.Client.Status().Update(ctx, pgBackup); err != nil {
			return reconcile.Result{}, errors.Wrap(err, "update PGBackup status")
		}

		log.Info("Backup is starting", "backup", pgBackup.Name, "cluster", pgCluster.Name)
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

		if err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
			bcp := new(v2.PerconaPGBackup)
			if err := r.Client.Get(ctx, types.NamespacedName{Name: pgBackup.Name, Namespace: pgBackup.Namespace}, bcp); err != nil {
				return errors.Wrap(err, "get PGBackup")
			}

			switch job.Labels[naming.LabelPGBackRestBackup] {
			case string(naming.BackupReplicaCreate), string(naming.BackupManual):
				if bcp.Annotations == nil {
					bcp.Annotations = make(map[string]string)
				}
				bcp.Annotations[v2.AnnotationPGBackrestBackupJobType] = job.Labels[naming.LabelPGBackRestBackup]
			default:
				return errors.Errorf("unknown value for %s label: %s", naming.LabelPGBackRestBackup, job.Labels[naming.LabelPGBackRestBackup])
			}

			return r.Client.Update(ctx, bcp)
		}); err != nil {
			return reconcile.Result{}, errors.Wrap(err, "update PGBackup")
		}

		if err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
			bcp := new(v2.PerconaPGBackup)
			if err := r.Client.Get(ctx, types.NamespacedName{Name: pgBackup.Name, Namespace: pgBackup.Namespace}, bcp); err != nil {
				return errors.Wrap(err, "get PGBackup")
			}

			bcp.Status.State = v2.BackupRunning
			bcp.Status.JobName = job.Name

			return r.Client.Status().Update(ctx, bcp)
		}); err != nil {
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
		rr, err := finishBackup(ctx, r.Client, pgBackup, job)
		if err != nil {
			return reconcile.Result{}, err
		}
		if rr != nil {
			log.Info("Waiting for crunchy reconciler to finish")
			return *rr, nil
		}

		if err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
			bcp := new(v2.PerconaPGBackup)
			if err := r.Client.Get(ctx, types.NamespacedName{Name: pgBackup.Name, Namespace: pgBackup.Namespace}, bcp); err != nil {
				return errors.Wrap(err, "get PGBackup")
			}

			bcp.Status.CompletedAt = job.Status.CompletionTime
			bcp.Status.State = status

			return r.Client.Status().Update(ctx, bcp)
		}); err != nil {
			return reconcile.Result{}, errors.Wrap(err, "update PGBackup status")
		}

		return reconcile.Result{}, nil
	case v2.BackupSucceeded:
		execCli, err := clientcmd.NewClient()
		if err != nil {
			return reconcile.Result{}, errors.Wrap(err, "failed to create exec client")
		}

		latestRestorableTime, err := watcher.GetLatestCommitTimestamp(ctx, r.Client, execCli, pgCluster)
		if err == nil {
			log.Info("Got latest restorable timestamp", "timestamp", latestRestorableTime)
			pgBackup.Status.LatestRestorableTime.Time = latestRestorableTime
		}

		if err := r.Client.Status().Update(ctx, pgBackup); err != nil {
			return reconcile.Result{}, errors.Wrap(err, "update PGBackup status")
		}

		return reconcile.Result{}, nil
	default:
		return reconcile.Result{}, nil
	}
}

func getRepo(pg *v2.PerconaPGCluster, pb *v2.PerconaPGBackup) *v1beta1.PGBackRestRepo {
	repoName := pb.Spec.RepoName
	for i, r := range pg.Spec.Backups.PGBackRest.Repos {
		if repoName == r.Name {
			return &pg.Spec.Backups.PGBackRest.Repos[i]
		}
	}
	return nil
}

func getDestination(pg *v2.PerconaPGCluster, pb *v2.PerconaPGBackup) string {
	destination := ""
	repo := getRepo(pg, pb)
	if repo == nil {
		return ""
	}

	repoPath := pg.Spec.Backups.PGBackRest.Global[repo.Name+"-path"]

	switch {
	case repo.S3 != nil:
		destination = "s3://" + path.Join(repo.S3.Bucket, repoPath)
	case repo.GCS != nil:
		destination = "gs://" + path.Join(repo.GCS.Bucket, repoPath)
	case repo.Azure != nil:
		destination = "azure://" + path.Join(repo.Azure.Container, repoPath)
	default:
		return ""
	}

	return destination
}

func getReadyInstancePod(ctx context.Context, c client.Client, pgBackup *v2.PerconaPGBackup) (*corev1.Pod, error) {
	pods := &corev1.PodList{}
	selector, err := naming.AsSelector(naming.ClusterInstances(pgBackup.Spec.PGCluster))
	if err != nil {
		return nil, err
	}
	if err := c.List(ctx, pods, client.InNamespace(pgBackup.Namespace), client.MatchingLabelsSelector{Selector: selector}); err != nil {
		return nil, errors.Wrap(err, "list pods")
	}
	for _, pod := range pods.Items {
		if pod.Status.Phase != corev1.PodRunning {
			continue
		}
		return &pod, nil
	}
	return nil, errors.New("no running instance found")
}

func updatePGBackrestInfo(ctx context.Context, c client.Client, pod *corev1.Pod, pgBackup *v2.PerconaPGBackup) error {
	info, err := pgbackrest.GetInfo(ctx, pod, pgBackup.Spec.RepoName)
	if err != nil {
		return errors.Wrap(err, "get pgBackRest info")
	}

	stanzaName := ""
	for _, info := range info {
		if stanzaName != "" {
			break
		}
		for _, backup := range info.Backup {
			if len(backup.Annotation) == 0 {
				continue
			}

			// We mark completed backups with the AnnotationJobName.
			// We should look for a backup without an AnnotationJobName that matches the AnnotationBackupName we're interested in.
			if v := backup.Annotation[v2.PGBackrestAnnotationJobName]; v != "" && v != pgBackup.Status.JobName {
				continue
			}

			backupType := backup.Annotation[v2.PGBackrestAnnotationJobType]
			if (backupType != pgBackup.Annotations[v2.AnnotationPGBackrestBackupJobType] ||
				backupType != string(naming.BackupReplicaCreate)) && backup.Annotation[v2.PGBackrestAnnotationBackupName] != pgBackup.Name {
				continue
			}

			stanzaName = info.Name
			if pgBackup.Status.BackupName == "" {
				if err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
					bcp := new(v2.PerconaPGBackup)
					if err := c.Get(ctx, types.NamespacedName{Name: pgBackup.Name, Namespace: pgBackup.Namespace}, bcp); err != nil {
						return errors.Wrap(err, "get PGBackup")
					}

					bcp.Status.BackupName = backup.Label
					bcp.Status.BackupType = backup.Type

					return c.Status().Update(ctx, bcp)
				}); err != nil {
					return errors.Wrap(err, "update PGBackup status")
				}
			}

			if err := pgbackrest.SetAnnotationsToBackup(ctx, pod, stanzaName, backup.Label, pgBackup.Spec.RepoName, map[string]string{
				v2.PGBackrestAnnotationJobName: pgBackup.Status.JobName,
			}); err != nil {
				return errors.Wrap(err, "set annotations to backup")
			}
			return nil
		}
	}
	return errors.New("backup annotations are not found in pgbackrest")
}

func finishBackup(ctx context.Context, c client.Client, pgBackup *v2.PerconaPGBackup, job *batchv1.Job) (*reconcile.Result, error) {
	readyPod, err := getReadyInstancePod(ctx, c, pgBackup)
	if err != nil {
		return nil, errors.Wrap(err, "get ready instance pod")
	}

	if err := updatePGBackrestInfo(ctx, c, readyPod, pgBackup); err != nil {
		return nil, errors.Wrap(err, "update pgbackrest info")
	}

	deleteAnnotation := func(annotation string) (bool, error) {
		deleted := true
		return deleted, retry.RetryOnConflict(retry.DefaultBackoff, func() error {
			pgCluster := new(v2.PerconaPGCluster)
			err := c.Get(ctx, types.NamespacedName{Name: pgBackup.Spec.PGCluster, Namespace: pgBackup.Namespace}, pgCluster)
			if err != nil {
				return errors.Wrap(err, "get PostgresCluster")
			}
			_, ok := pgCluster.Annotations[annotation]
			if !ok {
				deleted = false
				return nil
			}
			delete(pgCluster.Annotations, annotation)
			if err := c.Update(ctx, pgCluster); err != nil {
				return err
			}
			return nil
		})
	}

	if job.Labels[naming.LabelPGBackRestBackup] == string(naming.BackupManual) {
		deleted, err := deleteAnnotation(naming.PGBackRestBackup)
		if err != nil {
			return nil, errors.Wrapf(err, "delete %s annotation", naming.PGBackRestBackup)
		}
		if deleted {
			// We should wait until the crunchy reconciler is finished.
			// If we delete the job labels without waiting for the reconcile to finish, the Crunchy reconciler will
			// receive the pgcluster with the "naming.PGBackRestBackup" annotation, but will not find the manual backup job.
			// It will attempt to create a new job with the same name, failing and resulting in a scary error in the logs.
			return &reconcile.Result{RequeueAfter: time.Second * 5}, nil
		}

		// Remove PGBackRest labels to prevent the job from being included in
		// repoResources.manualBackupJobs and deleted by the cleanupRepoResources
		// or reconcileManualBackup methods.
		for k := range naming.PGBackRestLabels(pgBackup.Spec.PGCluster) {
			delete(job.Labels, k)
		}

		if err := c.Update(ctx, job); err != nil {
			return nil, errors.Wrap(err, "update backup job annotation")
		}

		pgCluster := new(v1beta1.PostgresCluster)
		if err := c.Get(ctx, types.NamespacedName{Name: pgBackup.Spec.PGCluster, Namespace: pgBackup.Namespace}, pgCluster); err != nil {
			return nil, errors.Wrap(err, "get PostgresCluster")
		}
		origPGCluster := pgCluster.DeepCopy()
		pgCluster.Status.PGBackRest.ManualBackup = nil
		if err := c.Status().Patch(ctx, pgCluster, client.MergeFrom(origPGCluster)); err != nil {
			return nil, errors.Wrap(err, "failed to patch PostgresCluster")
		}

		if _, err := deleteAnnotation(v2.AnnotationBackupInProgress); err != nil {
			return nil, errors.Wrapf(err, "delete %s annotation", v2.AnnotationBackupInProgress)
		}
	}

	return nil, nil
}

func startBackup(ctx context.Context, c client.Client, pb *v2.PerconaPGBackup) error {
	if err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		pg := &v2.PerconaPGCluster{}
		err := c.Get(ctx, types.NamespacedName{Name: pb.Spec.PGCluster, Namespace: pb.Namespace}, pg)
		if err != nil {
			return err
		}
		if a := pg.Annotations[v2.AnnotationBackupInProgress]; a != "" && a != pb.Name {
			return errors.Errorf("backup %s already in progress", a)
		}
		if pg.Annotations == nil {
			pg.Annotations = make(map[string]string)
		}
		pg.Annotations[naming.PGBackRestBackup] = pb.Name
		pg.Annotations[v2.AnnotationBackupInProgress] = pb.Name

		if pg.Spec.Backups.PGBackRest.Manual == nil {
			pg.Spec.Backups.PGBackRest.Manual = new(v1beta1.PGBackRestManualBackup)
		}

		pg.Spec.Backups.PGBackRest.Manual.RepoName = pb.Spec.RepoName
		pg.Spec.Backups.PGBackRest.Manual.Options = pb.Spec.Options

		return c.Update(ctx, pg)
	}); err != nil {
		return errors.Wrap(err, "update PostgresCluster")
	}

	return nil
}

func findBackupJob(ctx context.Context, c client.Client, pg *v2.PerconaPGCluster, pb *v2.PerconaPGBackup) (*batchv1.Job, error) {
	if jobName := pb.GetAnnotations()[v2.AnnotationPGBackrestBackupJobName]; jobName != "" {
		job := new(batchv1.Job)
		err := c.Get(ctx, types.NamespacedName{Name: jobName, Namespace: pb.Namespace}, job)
		if err != nil {
			return nil, errors.Wrap(err, "get backup job by .status.jobName")
		}
		return job, nil
	}

	jobList := &batchv1.JobList{}
	err := c.List(ctx, jobList,
		client.InNamespace(pg.Namespace),
		client.MatchingLabelsSelector{
			Selector: naming.PGBackRestBackupJobSelector(pg.Name, pb.Spec.RepoName, naming.BackupManual),
		})
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
