package pgupgrade

import (
	"context"
	"strings"
	"time"

	"github.com/pkg/errors"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/percona/percona-postgresql-operator/v2/internal/logging"
	"github.com/percona/percona-postgresql-operator/v2/percona/extensions"
	pgv2 "github.com/percona/percona-postgresql-operator/v2/pkg/apis/pgv2.percona.com/v2"
	crunchyv1beta1 "github.com/percona/percona-postgresql-operator/v2/pkg/apis/postgres-operator.crunchydata.com/v1beta1"
)

const (
	// ControllerName is the name of the PerconaPerconaPGUpgrade controller
	PGUpgradeControllerName = "perconapgupgrade-controller"
)

// Reconciler holds resources for the PerconaPerconaPGUpgrade reconciler
type PGUpgradeReconciler struct {
	Client client.Client
}

// SetupWithManager adds the PerconaPerconaPGUpgrade controller to the provided runtime manager
func (r *PGUpgradeReconciler) SetupWithManager(mgr manager.Manager) error {
	return builder.ControllerManagedBy(mgr).For(&pgv2.PerconaPGUpgrade{}).Complete(r)
}

// +kubebuilder:rbac:groups=pgv2.percona.com,resources=perconapgupgrades,verbs=get;list;create;update;patch;watch
// +kubebuilder:rbac:groups=pgv2.percona.com,resources=perconapgupgrades/status,verbs=patch;update
// +kubebuilder:rbac:groups=pgv2.percona.com,resources=perconapgupgrades/finalizers,verbs=patch;update
// +kubebuilder:rbac:groups=pgv2.percona.com,resources=perconapgclusters,verbs=get;list;watch;patch;update
// +kubebuilder:rbac:groups=postgres-operator.crunchydata.com,resources=pgupgrades,verbs=get;list;create;update;patch;delete;watch

func (r *PGUpgradeReconciler) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	log := logging.FromContext(ctx).WithValues("request", request)

	perconaPGUpgrade := &pgv2.PerconaPGUpgrade{}
	if err := r.Client.Get(ctx, request.NamespacedName, perconaPGUpgrade); err != nil {
		// NotFound cannot be fixed by requeuing so ignore it. During background
		// deletion, we receive delete events from cluster's dependents after
		// cluster is deleted.
		if err = client.IgnoreNotFound(err); err != nil {
			log.Error(err, "unable to fetch PerconaPGUpgrade")
		}
		return reconcile.Result{}, err
	}

	pgCluster := &pgv2.PerconaPGCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      perconaPGUpgrade.Spec.PostgresClusterName,
			Namespace: perconaPGUpgrade.Namespace,
		},
	}
	if err := r.Client.Get(ctx, client.ObjectKeyFromObject(pgCluster), pgCluster); err != nil {
		return reconcile.Result{}, errors.Wrapf(err, "get PerconaPGCluster %s/%s", perconaPGUpgrade.Namespace, perconaPGUpgrade.Spec.PostgresClusterName)
	}

	pgUpgrade := &crunchyv1beta1.PGUpgrade{
		ObjectMeta: metav1.ObjectMeta{
			Name:      perconaPGUpgrade.Name,
			Namespace: perconaPGUpgrade.Namespace,
		},
	}
	if err := r.Client.Get(ctx, client.ObjectKeyFromObject(pgUpgrade), pgUpgrade); err != nil {
		if k8serrors.IsNotFound(err) {
			if err := controllerutil.SetControllerReference(perconaPGUpgrade, pgUpgrade, r.Client.Scheme()); err != nil {
				return reconcile.Result{}, errors.Wrap(err, "set controller reference")
			}

			if err := r.createPGUpgrade(ctx, pgCluster, pgUpgrade, perconaPGUpgrade); err != nil {
				return reconcile.Result{}, errors.Wrap(err, "create PGUpgrade")
			}

			log.Info("PGUpgrade created", "cluster", pgCluster.Name)

			return reconcile.Result{RequeueAfter: 5 * time.Second}, nil
		}

		return reconcile.Result{}, errors.Wrapf(err, "get PGUpgrade %s/%s", pgUpgrade.Namespace, pgUpgrade.Name)
	}

	defer func() {
		err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
			perconaPGUpgrade.Status.Conditions = pgUpgrade.Status.Conditions
			perconaPGUpgrade.Status.ObservedGeneration = perconaPGUpgrade.Generation

			return r.Client.Status().Update(ctx, perconaPGUpgrade)
		})
		if err != nil {
			log.Error(err, "update PerconaPGUpgrade status")
		}
	}()

	if cond := meta.FindStatusCondition(pgUpgrade.Status.Conditions, "Progressing"); cond != nil {
		log.Info("PGUpgrade progressing", "reason", cond.Reason, "message", cond.Message, "type", cond.Type, "status", cond.Status)
		if cond.Status == metav1.ConditionTrue {
			log.Info("PGUpgrade in progress", "cluster", pgCluster.Name)
			return reconcile.Result{RequeueAfter: 5 * time.Second}, nil
		}

		switch cond.Reason {
		case "PGUpgradeInvalidForCluster":
			log.Info("PGUpgrade invalid for cluster", "cluster", pgCluster.Name)
			return reconcile.Result{}, nil
		case "PGClusterNotShutdown":
			log.Info("Pausing PGCluster", "PGCluster", pgCluster.Name)
			if err := r.pauseCluster(ctx, pgCluster); err != nil {
				return reconcile.Result{}, errors.Wrap(err, "pause PGCluster")
			}
			return reconcile.Result{RequeueAfter: 5 * time.Second}, nil
		case "PGClusterMissingRequiredAnnotation":
			log.Info("Annotating PGCluster", "cluster", pgCluster.Name)
			if err := r.annotateCluster(ctx, pgCluster, perconaPGUpgrade); err != nil {
				return reconcile.Result{}, errors.Wrap(err, "annotate PGCluster")
			}
			return reconcile.Result{RequeueAfter: 5 * time.Second}, nil
		}
	}

	if cond := meta.FindStatusCondition(pgUpgrade.Status.Conditions, "Succeeded"); cond != nil {
		log.Info("PGUpgrade succeeded", "reason", cond.Reason, "message", cond.Message, "type", cond.Type, "status", cond.Status)
		switch cond.Reason {
		case "PGUpgradeFailed":
			log.Info("PGUpgrade failed", "cluster", pgCluster.Name)
			return reconcile.Result{}, nil
		case "PGUpgradeSucceeded":
			if err := r.finalizeUpgrade(ctx, pgCluster, perconaPGUpgrade); err != nil {
				return reconcile.Result{}, errors.Wrap(err, "finalize upgrade")
			}
			log.Info("Resuming PGCluster", "PGCluster", pgCluster.Name)
			if err := r.resumeCluster(ctx, pgCluster); err != nil {
				return reconcile.Result{}, errors.Wrap(err, "resume PGCluster")
			}
		}
	}

	return reconcile.Result{}, nil
}

func (r *PGUpgradeReconciler) createPGUpgrade(ctx context.Context, cluster *pgv2.PerconaPGCluster, pgUpgrade *crunchyv1beta1.PGUpgrade, perconaPGUpgrade *pgv2.PerconaPGUpgrade) error {
	pgUpgrade.Spec.Metadata = perconaPGUpgrade.Spec.Metadata
	pgUpgrade.Spec.PostgresClusterName = perconaPGUpgrade.Spec.PostgresClusterName

	pgUpgrade.Spec.Image = perconaPGUpgrade.Spec.Image
	pgUpgrade.Spec.ImagePullPolicy = perconaPGUpgrade.Spec.ImagePullPolicy
	pgUpgrade.Spec.ImagePullSecrets = perconaPGUpgrade.Spec.ImagePullSecrets

	pgUpgrade.Spec.FromPostgresVersion = perconaPGUpgrade.Spec.FromPostgresVersion
	pgUpgrade.Spec.ToPostgresVersion = perconaPGUpgrade.Spec.ToPostgresVersion
	pgUpgrade.Spec.ToPostgresImage = perconaPGUpgrade.Spec.ToPostgresImage

	pgUpgrade.Spec.Resources = perconaPGUpgrade.Spec.Resources
	pgUpgrade.Spec.Affinity = perconaPGUpgrade.Spec.Affinity
	pgUpgrade.Spec.PriorityClassName = perconaPGUpgrade.Spec.PriorityClassName
	pgUpgrade.Spec.Tolerations = perconaPGUpgrade.Spec.Tolerations
	pgUpgrade.Spec.InitContainers = perconaPGUpgrade.Spec.InitContainers

	if cluster.Spec.Extensions.Storage.Secret == nil {
		return r.Client.Create(ctx, pgUpgrade)
	}

	for _, pgVersion := range []int{perconaPGUpgrade.Spec.FromPostgresVersion, perconaPGUpgrade.Spec.ToPostgresVersion} {
		extensionKeys := make([]string, 0)

		for _, extension := range cluster.Spec.Extensions.Custom {
			key := extensions.GetExtensionKey(pgVersion, extension.Name, extension.Version)
			extensionKeys = append(extensionKeys, key)
		}

		pgUpgrade.Spec.InitContainers = append(pgUpgrade.Spec.InitContainers, extensions.RelocatorContainer(
			cluster, *perconaPGUpgrade.Spec.Image, cluster.Spec.ImagePullPolicy, pgVersion,
		))

		pgUpgrade.Spec.InitContainers = append(pgUpgrade.Spec.InitContainers, extensions.InstallerContainer(
			cluster,
			pgVersion,
			&cluster.Spec.Extensions,
			strings.Join(extensionKeys, ","),
			cluster.Spec.OpenShift,
		))
	}

	// we're only adding the volume mounts for target version since current volume mounts are already mounted
	pgUpgrade.Spec.VolumeMounts = append(pgUpgrade.Spec.VolumeMounts, extensions.VolumeMounts(
		perconaPGUpgrade.Spec.ToPostgresVersion)...,
	)

	return r.Client.Create(ctx, pgUpgrade)
}

func (r *PGUpgradeReconciler) pauseCluster(ctx context.Context, pgCluster *pgv2.PerconaPGCluster) error {
	orig := pgCluster.DeepCopy()

	t := true
	pgCluster.Spec.Pause = &t

	return r.Client.Patch(ctx, pgCluster.DeepCopy(), client.MergeFrom(orig))
}

func (r *PGUpgradeReconciler) resumeCluster(ctx context.Context, pgCluster *pgv2.PerconaPGCluster) error {
	orig := pgCluster.DeepCopy()

	pgCluster.Spec.Pause = nil

	return r.Client.Patch(ctx, pgCluster.DeepCopy(), client.MergeFrom(orig))
}

func (r *PGUpgradeReconciler) annotateCluster(ctx context.Context, pgCluster *pgv2.PerconaPGCluster, pgUpgrade *pgv2.PerconaPGUpgrade) error {
	orig := pgCluster.DeepCopy()

	if pgCluster.Annotations == nil {
		pgCluster.Annotations = make(map[string]string)
	}

	pgCluster.Annotations[pgv2.AnnotationAllowUpgrade] = pgUpgrade.Name

	return r.Client.Patch(ctx, pgCluster.DeepCopy(), client.MergeFrom(orig))
}

func (r *PGUpgradeReconciler) finalizeUpgrade(ctx context.Context, pgCluster *pgv2.PerconaPGCluster, pgUpgrade *pgv2.PerconaPGUpgrade) error {
	log := logging.FromContext(ctx)

	orig := pgCluster.DeepCopy()

	delete(pgCluster.Annotations, pgv2.AnnotationAllowUpgrade)

	pgCluster.Spec.PostgresVersion = pgUpgrade.Spec.ToPostgresVersion
	pgCluster.Spec.Image = pgUpgrade.Spec.ToPostgresImage
	pgCluster.Spec.Proxy.PGBouncer.Image = pgUpgrade.Spec.ToPgBouncerImage
	pgCluster.Spec.Backups.PGBackRest.Image = pgUpgrade.Spec.ToPgBackRestImage

	log.Info("Finalizing upgrade",
		"newPostgresVersion", pgCluster.Spec.PostgresVersion,
		"newPostgresImage", pgCluster.Spec.Image,
		"newPgBouncerImage", pgCluster.Spec.Proxy.PGBouncer.Image,
		"newPgBackRestImage", pgCluster.Spec.Backups.PGBackRest.Image,
	)

	return r.Client.Patch(ctx, pgCluster.DeepCopy(), client.MergeFrom(orig))
}
