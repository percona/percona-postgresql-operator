package pgbackup

import (
	"context"
	"path"
	"slices"
	"time"

	"github.com/pkg/errors"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	"github.com/percona/percona-postgresql-operator/v2/internal/logging"
	"github.com/percona/percona-postgresql-operator/v2/internal/naming"
	"github.com/percona/percona-postgresql-operator/v2/percona/clientcmd"
	"github.com/percona/percona-postgresql-operator/v2/percona/controller"
	pNaming "github.com/percona/percona-postgresql-operator/v2/percona/naming"
	"github.com/percona/percona-postgresql-operator/v2/percona/pgbackrest"
	"github.com/percona/percona-postgresql-operator/v2/percona/watcher"
	v2 "github.com/percona/percona-postgresql-operator/v2/pkg/apis/pgv2.percona.com/v2"
	"github.com/percona/percona-postgresql-operator/v2/pkg/apis/postgres-operator.crunchydata.com/v1beta1"
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
}

// SetupWithManager adds the PerconaPGBackup controller to the provided runtime manager
func (r *PGBackupReconciler) SetupWithManager(mgr manager.Manager) error {
	return (builder.ControllerManagedBy(mgr).
		For(&v2.PerconaPGBackup{}).
		WatchesRawSource(source.Channel(r.ExternalChan, &handler.EnqueueRequestForObject{})).
		Complete(r))
}

// +kubebuilder:rbac:groups=pgv2.percona.com,resources=perconapgbackups,verbs=create;get;list;watch;update;delete;patch
// +kubebuilder:rbac:groups=pgv2.percona.com,resources=perconapgbackups/status,verbs=create;patch;update
// +kubebuilder:rbac:groups=pgv2.percona.com,resources=perconapgclusters,verbs=get;list;create;update;patch;watch
// +kubebuilder:rbac:groups=pgv2.percona.com,resources=perconapgbackups/finalizers,verbs=update;patch
// +kubebuilder:rbac:groups=postgres-operator.crunchydata.com,resources=postgresclusters,verbs=get;list;create;update;patch;watch
// +kubebuilder:rbac:groups=postgres-operator.crunchydata.com,resources=postgresclusters/status,verbs=create;update;patch
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

	if !pgBackup.DeletionTimestamp.IsZero() || pgBackup.Status.State == v2.BackupFailed {
		if _, err := runFinalizers(ctx, r.Client, pgBackup); err != nil {
			return reconcile.Result{}, errors.Wrap(err, "failed to run finalizers")
		}
		return reconcile.Result{RequeueAfter: time.Second * 5}, nil
	}

	if pgBackup.Status.State != v2.BackupFailed && pgBackup.Status.State != v2.BackupSucceeded {
		if err := ensureFinalizers(ctx, r.Client, pgBackup); err != nil {
			return reconcile.Result{}, errors.Wrap(err, "ensure finalizers")
		}
	}

	pgCluster := new(v2.PerconaPGCluster)
	if err := r.Client.Get(ctx, types.NamespacedName{Name: pgBackup.Spec.PGCluster, Namespace: request.Namespace}, pgCluster); err != nil {
		if !k8serrors.IsNotFound(err) {
			return reconcile.Result{}, errors.Wrap(err, "get PostgresCluster")
		}
		pgCluster = nil
	}

	switch pgBackup.Status.State {
	case v2.BackupNew:
		if pgCluster == nil {
			return reconcile.Result{}, errors.Errorf("PostgresCluster %s is not found", pgBackup.Spec.PGCluster)
		}

		if !pgCluster.Spec.Backups.IsEnabled() {
			if err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
				bcp := new(v2.PerconaPGBackup)
				if err := r.Client.Get(ctx, client.ObjectKeyFromObject(pgBackup), bcp); err != nil {
					return errors.Wrap(err, "get PGBackup")
				}

				bcp.Status.State = v2.BackupFailed
				bcp.Status.Error = "Backups are not enabled in the PerconaPGCluster configuration"

				return r.Client.Status().Update(ctx, bcp)
			}); err != nil {
				return reconcile.Result{}, errors.Wrap(err, "update PGBackup status")
			}

			return reconcile.Result{}, nil
		}

		if pgCluster.Spec.Pause != nil && *pgCluster.Spec.Pause {
			log.Info("Can't start backup. PostgresCluster is paused", "pg-backup", pgBackup.Name, "cluster", pgCluster.Name)
			return reconcile.Result{RequeueAfter: time.Second * 5}, nil
		}

		// start backup only if backup job doesn't exist
		_, err := findBackupJob(ctx, r.Client, pgBackup)
		if err != nil {
			if !errors.Is(err, ErrBackupJobNotFound) {
				return reconcile.Result{}, errors.Wrap(err, "find backup job")
			}

			runningBackup, err := getBackupInProgress(ctx, r.Client, pgBackup.Spec.PGCluster, pgBackup.Namespace)
			if err != nil {
				return reconcile.Result{}, errors.Wrap(err, "get backup in progress")
			}
			if runningBackup != "" && runningBackup != pgBackup.Name {
				log.Info("Can't start backup. Previous backup is still in progress", "pg-backup", pgBackup.Name, "cluster", pgCluster.Name)
				return reconcile.Result{RequeueAfter: time.Second * 5}, nil
			}
			if err := startBackup(ctx, r.Client, pgBackup); err != nil {
				return reconcile.Result{}, errors.Wrap(err, "failed to start backup")
			}
		}

		repo := getRepo(pgCluster, pgBackup)
		if repo == nil {
			return reconcile.Result{}, errors.Errorf("%s repo not defined", pgBackup.Spec.RepoName)
		}

		if err := updateStatus(ctx, r.Client, pgBackup, func(bcp *v2.PerconaPGBackup) {
			bcp.Status.Destination = getDestination(pgCluster, pgBackup)
			bcp.Status.Image = pgCluster.Spec.Backups.PGBackRest.Image
			bcp.Status.Repo = repo
			bcp.Status.CRVersion = pgCluster.Spec.CRVersion

			switch {
			case repo.S3 != nil:
				bcp.Status.StorageType = v2.PGBackupStorageTypeS3
			case repo.Azure != nil:
				bcp.Status.StorageType = v2.PGBackupStorageTypeAzure
			case repo.GCS != nil:
				bcp.Status.StorageType = v2.PGBackupStorageTypeGCS
			default:
				bcp.Status.StorageType = v2.PGBackupStorageTypeFilesystem
			}

			bcp.Status.State = v2.BackupStarting
		}); err != nil {
			return reconcile.Result{}, errors.Wrap(err, "update PGBackup status")
		}

		log.Info("Backup is starting", "backup", pgBackup.Name, "cluster", pgCluster.Name)
		return reconcile.Result{}, nil
	case v2.BackupStarting:
		if pgCluster == nil {
			return reconcile.Result{}, errors.Errorf("PostgresCluster %s is not found", pgBackup.Spec.PGCluster)
		}

		job, err := findBackupJob(ctx, r.Client, pgBackup)
		if err != nil {
			if errors.Is(err, ErrBackupJobNotFound) {
				log.Info("Waiting for backup to start")

				if err := failIfClusterIsNotReady(ctx, r.Client, pgCluster, pgBackup); err != nil {
					return reconcile.Result{}, errors.Wrap(err, "fail if cluster is not ready for backup")
				}

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
				bcp.Annotations[pNaming.AnnotationPGBackrestBackupJobType] = job.Labels[naming.LabelPGBackRestBackup]
			default:
				return errors.Errorf("unknown value for %s label: %s", naming.LabelPGBackRestBackup, job.Labels[naming.LabelPGBackRestBackup])
			}

			return r.Client.Update(ctx, bcp)
		}); err != nil {
			return reconcile.Result{}, errors.Wrap(err, "update PGBackup")
		}

		if err := updateStatus(ctx, r.Client, pgBackup, func(bcp *v2.PerconaPGBackup) {
			bcp.Status.State = v2.BackupRunning
			bcp.Status.JobName = job.Name
		}); err != nil {
			return reconcile.Result{}, errors.Wrap(err, "update PGBackup status")
		}

		return reconcile.Result{}, nil
	case v2.BackupRunning:
		job := &batchv1.Job{}
		err := r.Client.Get(ctx, types.NamespacedName{Name: pgBackup.Status.JobName, Namespace: pgBackup.Namespace}, job)
		if err != nil {
			// If something has deleted the job even with the finalizer, we should fail the backup.
			if k8serrors.IsNotFound(err) {
				if err := updateStatus(ctx, r.Client, pgBackup, func(bcp *v2.PerconaPGBackup) {
					bcp.Status.State = v2.BackupFailed
				}); err != nil {
					return reconcile.Result{}, errors.Wrap(err, "update PGBackup status")
				}
				return reconcile.Result{}, nil
			}
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

		// We need to perform the same steps as in the delete-backup finalizer once the backup has finished or failed.
		// After that, the finalizer is no longer needed, that's why the RunFinalizer function is used here.
		done, err := controller.RunFinalizer(ctx, r.Client, pgBackup, pNaming.FinalizerDeleteBackup, deleteBackupFinalizer(r.Client, pgCluster))
		if err != nil {
			return reconcile.Result{}, errors.Wrap(err, "failed to run delete-backup finalizer")
		}
		if !done {
			log.Info("Waiting for crunchy reconciler to finish")
			return reconcile.Result{RequeueAfter: time.Second * 5}, nil
		}

		if err := updateStatus(ctx, r.Client, pgBackup, func(bcp *v2.PerconaPGBackup) {
			bcp.Status.CompletedAt = job.Status.CompletionTime
			bcp.Status.State = status
		}); err != nil {
			return reconcile.Result{}, errors.Wrap(err, "update PGBackup status")
		}

		return reconcile.Result{}, nil
	case v2.BackupSucceeded:
		job, err := findBackupJob(ctx, r.Client, pgBackup)
		if err == nil && slices.Contains(job.Finalizers, pNaming.FinalizerKeepJob) {
			if err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
				j := new(batchv1.Job)
				if err := r.Client.Get(ctx, client.ObjectKeyFromObject(job), j); err != nil {
					return errors.Wrap(err, "get job")
				}
				j.Finalizers = slices.DeleteFunc(j.Finalizers, func(s string) bool { return s == pNaming.FinalizerKeepJob })

				return r.Client.Update(ctx, j)
			}); err != nil {
				return reconcile.Result{}, errors.Wrap(err, "update PGBackup status")
			}
		}

		if pgCluster == nil {
			return reconcile.Result{}, nil
		}

		execCli, err := clientcmd.NewClient()
		if err != nil {
			return reconcile.Result{}, errors.Wrap(err, "failed to create exec client")
		}
		latestRestorableTime, err := watcher.GetLatestCommitTimestamp(ctx, r.Client, execCli, pgCluster, pgBackup)
		if err == nil {
			log.Info("Got latest restorable timestamp", "timestamp", latestRestorableTime)

			if err := updateStatus(ctx, r.Client, pgBackup, func(bcp *v2.PerconaPGBackup) {
				bcp.Status.LatestRestorableTime.Time = latestRestorableTime
			}); err != nil {
				return reconcile.Result{}, errors.Wrap(err, "update PGBackup status")
			}
		}

		return reconcile.Result{}, nil
	default:
		return reconcile.Result{}, nil
	}
}

func ensureFinalizers(ctx context.Context, cl client.Client, pgBackup *v2.PerconaPGBackup) error {
	orig := pgBackup.DeepCopy()

	finalizers := []string{pNaming.FinalizerDeleteBackup}
	finalizersChanged := false
	for _, f := range finalizers {
		if controllerutil.AddFinalizer(pgBackup, f) {
			finalizersChanged = true
		}
	}
	if !finalizersChanged {
		return nil
	}

	if err := cl.Patch(ctx, pgBackup.DeepCopy(), client.MergeFrom(orig)); err != nil {
		return errors.Wrap(err, "remove finalizers")
	}
	return nil
}

func deleteBackupFinalizer(c client.Client, pg *v2.PerconaPGCluster) func(ctx context.Context, pgBackup *v2.PerconaPGBackup) error {
	return func(ctx context.Context, pgBackup *v2.PerconaPGBackup) error {
		if pg == nil {
			return nil
		}

		job := new(batchv1.Job)
		err := c.Get(ctx, types.NamespacedName{Name: pgBackup.Status.JobName, Namespace: pgBackup.Namespace}, job)
		if client.IgnoreNotFound(err) != nil {
			return errors.Wrap(err, "get backup job")
		}
		if k8serrors.IsNotFound(err) {
			job = nil
		}

		rr, err := finishBackup(ctx, c, pgBackup, job)
		if err != nil {
			return errors.Wrap(err, "failed to finish backup")
		}

		if rr != nil && rr.RequeueAfter != 0 {
			return controller.ErrFinalizerPending
		}
		return nil
	}
}

func runFinalizers(ctx context.Context, c client.Client, pgBackup *v2.PerconaPGBackup) (bool, error) {
	pg := new(v2.PerconaPGCluster)
	if err := c.Get(ctx, types.NamespacedName{Name: pgBackup.Spec.PGCluster, Namespace: pgBackup.Namespace}, pg); err != nil {
		if !k8serrors.IsNotFound(err) {
			return false, errors.Wrap(err, "get PostgresCluster")
		}

		pg = nil
	}

	finalizers := map[string]controller.FinalizerFunc[*v2.PerconaPGBackup]{
		pNaming.FinalizerDeleteBackup: deleteBackupFinalizer(c, pg),
	}

	finished := true
	for finalizer, f := range finalizers {
		done, err := controller.RunFinalizer(ctx, c, pgBackup, finalizer, f)
		if err != nil {
			return false, errors.Wrapf(err, "run finalizer %s", finalizer)
		}
		if !done {
			finished = false
		}
	}
	return finished, nil
}

func getBackupInProgress(ctx context.Context, c client.Client, clusterName, ns string) (string, error) {
	pgCluster := &v2.PerconaPGCluster{}
	err := c.Get(ctx, types.NamespacedName{Name: clusterName, Namespace: ns}, pgCluster)
	if err != nil {
		return "", errors.Wrap(err, "get PostgresCluster")
	}

	crunchyCluster := new(v1beta1.PostgresCluster)
	if err := c.Get(ctx, types.NamespacedName{Name: clusterName, Namespace: ns}, crunchyCluster); err != nil {
		return "", errors.Wrap(err, "get PostgresCluster")
	}

	annotation := pNaming.AnnotationBackupInProgress
	crunchyAnnotation := pNaming.ToCrunchyAnnotation(annotation)

	if crunchyCluster.Annotations[crunchyAnnotation] != "" {
		return crunchyCluster.Annotations[crunchyAnnotation], nil
	}

	return pgCluster.Annotations[annotation], nil
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
			if (backupType != pgBackup.Annotations[pNaming.AnnotationPGBackrestBackupJobType] ||
				backupType != string(naming.BackupReplicaCreate)) && backup.Annotation[v2.PGBackrestAnnotationBackupName] != pgBackup.Name {
				continue
			}

			stanzaName = info.Name
			if pgBackup.Status.BackupName == "" {
				if err := updateStatus(ctx, c, pgBackup, func(bcp *v2.PerconaPGBackup) {
					bcp.Status.BackupName = backup.Label
					bcp.Status.BackupType = backup.Type
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
	log := logging.FromContext(ctx)
	// We should log error here instead of returning it
	// to allow deletion of the backup in the Starting/Running state
	log.Error(nil, "backup annotations are not found in pgbackrest")
	return nil
}

func finishBackup(ctx context.Context, c client.Client, pgBackup *v2.PerconaPGBackup, job *batchv1.Job) (*reconcile.Result, error) {
	if job != nil && checkBackupJob(job) == v2.BackupSucceeded {
		readyPod, err := controller.GetReadyInstancePod(ctx, c, pgBackup.Spec.PGCluster, pgBackup.Namespace)
		if err != nil {
			return nil, errors.Wrap(err, "get ready instance pod")
		}

		if err := updatePGBackrestInfo(ctx, c, readyPod, pgBackup); err != nil {
			return nil, errors.Wrap(err, "update pgbackrest info")
		}
	}

	if job != nil && job.Labels[naming.LabelPGBackRestBackup] != string(naming.BackupManual) {
		return nil, nil
	}

	runningBackup, err := getBackupInProgress(ctx, c, pgBackup.Spec.PGCluster, pgBackup.Namespace)
	if err != nil {
		return nil, errors.Wrap(err, "get backup in progress")
	}

	if runningBackup != pgBackup.Name {
		// This block only runs after all finalizer operations are complete.
		// Or, it runs when the user deletes a backup object that never started.
		// In both cases, treat this as the function's exit point.

		return nil, nil
	}

	// deleteAnnotation deletes the annotation from the PerconaPGCluster
	// and returns true when it's deleted from crunchy PostgresCluster.
	deleteAnnotation := func(annotation string) (bool, error) {
		pgCluster := new(v1beta1.PostgresCluster)
		if err := c.Get(ctx, types.NamespacedName{Name: pgBackup.Spec.PGCluster, Namespace: pgBackup.Namespace}, pgCluster); err != nil {
			return false, errors.Wrap(err, "get PostgresCluster")
		}

		crunchyAnnotation := pNaming.ToCrunchyAnnotation(annotation)
		// We should be sure that annotation is deleted from crunchy's cluster
		_, ok := pgCluster.Annotations[crunchyAnnotation]
		if !ok {
			return true, nil
		}

		return false, retry.RetryOnConflict(retry.DefaultBackoff, func() error {
			// If the annotation is present, we should delete the annotation in our cluster.
			// The annotation will be deleted in crunchy's cluster in `(*PGClusterReconciler) Reconcile` method call
			perconaPGCluster := new(v2.PerconaPGCluster)
			err := c.Get(ctx, types.NamespacedName{Name: pgBackup.Spec.PGCluster, Namespace: pgBackup.Namespace}, perconaPGCluster)
			if err != nil {
				return errors.Wrap(err, "get PerconaPGCluster")
			}

			_, ok = perconaPGCluster.Annotations[annotation]
			if !ok {
				return nil
			}
			delete(perconaPGCluster.Annotations, annotation)

			return c.Update(ctx, perconaPGCluster)
		})
	}

	deleted, err := deleteAnnotation(naming.PGBackRestBackup)
	if err != nil {
		return nil, errors.Wrapf(err, "delete %s annotation", naming.PGBackRestBackup)
	}
	if !deleted {
		// We should wait until the annotation is deleted from crunchy cluster.
		return &reconcile.Result{RequeueAfter: time.Second * 5}, nil
	}

	// Remove PGBackRest labels to prevent the job from being
	// deleted by the cleanupRepoResources method and included in
	// repoResources.manualBackupJobs used in reconcileManualBackup method
	if job != nil {
		if err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
			j := new(batchv1.Job)
			if err := c.Get(ctx, client.ObjectKeyFromObject(job), j); err != nil {
				return errors.Wrap(err, "get job")
			}

			for k := range naming.PGBackRestLabels(pgBackup.Spec.PGCluster) {
				delete(j.Labels, k)
			}

			return c.Update(ctx, j)
		}); err != nil {
			return nil, errors.Wrap(err, "update backup job labels")
		}
	}

	// We should delete the pgbackrest status to be able to recreate backups with the same name.
	if err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		pgCluster := new(v1beta1.PostgresCluster)
		if err := c.Get(ctx, types.NamespacedName{Name: pgBackup.Spec.PGCluster, Namespace: pgBackup.Namespace}, pgCluster); err != nil {
			return errors.Wrap(err, "get PostgresCluster")
		}
		pgCluster.Status.PGBackRest.ManualBackup = nil

		return c.Status().Update(ctx, pgCluster)
	}); err != nil {
		return nil, errors.Wrap(err, "update postgrescluster")
	}

	_, err = deleteAnnotation(pNaming.AnnotationBackupInProgress)
	if err != nil {
		return nil, errors.Wrapf(err, "delete %s annotation", pNaming.AnnotationBackupInProgress)
	}
	// Do not add any code after this comment.
	//
	// The code after the comment may or may not execute, depending on whether the
	// crunchy cluster removes the AnnotationBackupInProgress annotation in time.
	//
	// Once AnnotationBackupInProgress is deleted, the successful return of finalizer must happen inside the
	// `if runningBackup != pgBackup.Name { ... }` block.

	return &reconcile.Result{RequeueAfter: time.Second * 5}, nil
}

func startBackup(ctx context.Context, c client.Client, pb *v2.PerconaPGBackup) error {
	if err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		pg := &v2.PerconaPGCluster{}
		err := c.Get(ctx, types.NamespacedName{Name: pb.Spec.PGCluster, Namespace: pb.Namespace}, pg)
		if err != nil {
			return err
		}
		if a := pg.Annotations[pNaming.AnnotationBackupInProgress]; a != "" && a != pb.Name {
			return errors.Errorf("backup %s already in progress", a)
		}
		if pg.Annotations == nil {
			pg.Annotations = make(map[string]string)
		}
		pg.Annotations[naming.PGBackRestBackup] = pb.Name
		pg.Annotations[pNaming.AnnotationBackupInProgress] = pb.Name

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

func findBackupJob(ctx context.Context, c client.Client, pb *v2.PerconaPGBackup) (*batchv1.Job, error) {
	if jobName := pb.GetAnnotations()[pNaming.AnnotationPGBackrestBackupJobName]; jobName != "" || pb.Status.JobName != "" {
		if jobName == "" {
			jobName = pb.Status.JobName
		}
		job := new(batchv1.Job)
		err := c.Get(ctx, types.NamespacedName{Name: jobName, Namespace: pb.Namespace}, job)
		if err != nil {
			return nil, errors.Wrap(err, "get backup job by .status.jobName")
		}
		return job, nil
	}

	jobList := &batchv1.JobList{}
	err := c.List(ctx, jobList,
		client.InNamespace(pb.Namespace),
		client.MatchingLabelsSelector{
			Selector: naming.PGBackRestBackupJobSelector(pb.Spec.PGCluster, pb.Spec.RepoName, naming.BackupManual),
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

func failIfClusterIsNotReady(ctx context.Context, cl client.Client, pgCluster *v2.PerconaPGCluster, pgBackup *v2.PerconaPGBackup) error {
	log := logging.FromContext(ctx)

	condition := meta.FindStatusCondition(pgCluster.Status.Conditions, pNaming.ConditionClusterIsReadyForBackup)
	if condition == nil || condition.Status == metav1.ConditionTrue || condition.LastTransitionTime.Time.Add(2*time.Minute).After(time.Now()) {
		return nil
	}

	crunchyCluster := new(v1beta1.PostgresCluster)
	if err := cl.Get(ctx, client.ObjectKeyFromObject(pgCluster), crunchyCluster); err != nil {
		return errors.Wrap(err, "get PostgresCluster")
	}

	// Waiting for the crunchy cluster to receive the annotations added by the startBackup function.
	_, ok := crunchyCluster.Annotations[pNaming.ToCrunchyAnnotation(naming.PGBackRestBackup)]
	if !ok {
		return nil
	}
	_, ok = crunchyCluster.Annotations[pNaming.ToCrunchyAnnotation(pNaming.AnnotationBackupInProgress)]
	if !ok {
		return nil
	}

	log.Info("Cluster is not ready for backup for too long. Setting it's state to Failed")

	if err := updateStatus(ctx, cl, pgBackup, func(bcp *v2.PerconaPGBackup) {
		bcp.Status.State = v2.BackupFailed
	}); err != nil {
		return errors.Wrap(err, "update PGBackup status")
	}
	return nil
}

func updateStatus(ctx context.Context, cl client.Client, pgBackup *v2.PerconaPGBackup, updateFunc func(bcp *v2.PerconaPGBackup)) error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		bcp := new(v2.PerconaPGBackup)
		if err := cl.Get(ctx, client.ObjectKeyFromObject(pgBackup), bcp); err != nil {
			return errors.Wrap(err, "get PGBackup")
		}

		updateFunc(bcp)

		return cl.Status().Update(ctx, bcp)
	})
}
