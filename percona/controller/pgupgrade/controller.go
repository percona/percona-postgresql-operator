package pgupgrade

import (
	"context"
	"slices"
	"time"

	"github.com/pkg/errors"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/percona/percona-postgresql-operator/internal/logging"
	"github.com/percona/percona-postgresql-operator/internal/naming"
	pgv2 "github.com/percona/percona-postgresql-operator/pkg/apis/pgv2.percona.com/v2"
	v2 "github.com/percona/percona-postgresql-operator/pkg/apis/pgv2.percona.com/v2"
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
	return builder.ControllerManagedBy(mgr).For(&v2.PerconaPGUpgrade{}).Complete(r)
}

// +kubebuilder:rbac:groups="apps",resources="statefulsets",verbs=get;list;create;update;patch;watch
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
		return reconcile.Result{}, errors.Wrapf(
			err, "get PerconaPGCluster %s/%s",
			perconaPGUpgrade.Namespace,
			perconaPGUpgrade.Spec.PostgresClusterName,
		)
	}

	if pgCluster.Status.State != pgv2.AppStateReady {
		return reconcile.Result{RequeueAfter: 5 * time.Second}, nil
	}

	if err := r.doReconcile(ctx, pgCluster, perconaPGUpgrade); err != nil {
		return reconcile.Result{}, err
	}

	log.Info("PerconaPGUpgrade status", "status", perconaPGUpgrade.Status)

	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		local := &pgv2.PerconaPGUpgrade{}
		if err := r.Client.Get(ctx, request.NamespacedName, local); err != nil {
			return err
		}

		local.Status = perconaPGUpgrade.Status

		return r.Client.Status().Update(ctx, local)
	})
	if err != nil {
		return reconcile.Result{}, errors.Wrap(err, "update status")
	}

	return reconcile.Result{}, nil
}

func (r PGUpgradeReconciler) doReconcile(ctx context.Context, cluster *pgv2.PerconaPGCluster, cr *pgv2.PerconaPGUpgrade) error {
	if meta.FindStatusCondition(cr.Status.Conditions, "ReplicaCreated") == nil {
		if err := r.createReplica(ctx, cluster); err != nil {
			return errors.Wrap(err, "create new replica")
		}

		if err := r.waitForInstanceToBeReady(ctx, cluster, "upgrade"); err != nil {
			return errors.Wrap(err, "wait for instance to be ready")
		}

		meta.SetStatusCondition(&cr.Status.Conditions, metav1.Condition{
			Type:               "ReplicaCreated",
			Reason:             "ReplicaCreated",
			Status:             metav1.ConditionTrue,
			Message:            "Replica for upgrade created and ready",
			ObservedGeneration: cr.ObjectMeta.Generation,
			LastTransitionTime: metav1.Now(),
		})

		return nil
	}

	if meta.FindStatusCondition(cr.Status.Conditions, "InstanceUpdated") == nil {
		if err := r.updateInstance(ctx, cluster, cr); err != nil {
			return errors.Wrap(err, "update instance")
		}

		if err := r.waitForInstanceToBeReady(ctx, cluster, "upgrade"); err != nil {
			return errors.Wrap(err, "wait for instance to be ready")
		}

		meta.SetStatusCondition(&cr.Status.Conditions, metav1.Condition{
			Type:               "InstanceUpdated",
			Reason:             "InstanceUpdated",
			Status:             metav1.ConditionTrue,
			Message:            "Replica for upgrade updated with upgrade image",
			ObservedGeneration: cr.ObjectMeta.Generation,
			LastTransitionTime: metav1.Now(),
		})
	}

	return nil
}

var errStsNotFound = errors.New("statefulset not found")

func (r *PGUpgradeReconciler) getStatefulSetForInstance(
	ctx context.Context,
	cluster *pgv2.PerconaPGCluster,
	name string,
) (*appsv1.StatefulSet, error) {
	stsList := &appsv1.StatefulSetList{}
	err := r.Client.List(ctx, stsList, &client.ListOptions{
		Namespace: cluster.Namespace,
		LabelSelector: labels.SelectorFromSet(map[string]string{
			naming.LabelCluster:     cluster.Name,
			naming.LabelInstanceSet: name,
		}),
	})
	if err != nil {
		return nil, err
	}

	if len(stsList.Items) == 0 {
		return nil, errStsNotFound
	}

	if len(stsList.Items) != 1 {
		return nil, errors.Errorf("expected 1 statefulset, got %d", len(stsList.Items))
	}

	sts := &stsList.Items[0]

	err = r.Client.Get(ctx, types.NamespacedName{
		Name:      sts.Name,
		Namespace: sts.Namespace,
	}, sts)
	if err != nil {
		return nil, err
	}

	logging.FromContext(ctx).Info("Got statefulset",
		"sts", sts.Name,
		"image", sts.Spec.Template.Spec.Containers[0].Image,
		"command", sts.Spec.Template.Spec.Containers[0].Command,
		"liveness", sts.Spec.Template.Spec.Containers[0].LivenessProbe,
		"readiness", sts.Spec.Template.Spec.Containers[0].ReadinessProbe,
	)

	return sts, nil
}

func (r *PGUpgradeReconciler) waitForInstanceToBeReady(ctx context.Context, cluster *pgv2.PerconaPGCluster, name string) error {
	for {
		sts, err := r.getStatefulSetForInstance(ctx, cluster, name)
		if err != nil {
			if errors.Is(err, errStsNotFound) {
				continue
			}
			return errors.Wrap(err, "get statefulset")
		}

		if sts.Status.UpdateRevision != sts.Status.CurrentRevision {
			err = r.Client.DeleteAllOf(ctx, &corev1.Pod{}, &client.DeleteAllOfOptions{
				ListOptions: client.ListOptions{
					LabelSelector: labels.SelectorFromSet(map[string]string{
						naming.LabelCluster:  cluster.Name,
						naming.LabelInstance: sts.Name,
					}),
				},
			})
			if err != nil {
				return errors.Wrap(err, "delete all pods")
			}

			continue
		}

		if sts.Status.UpdatedReplicas != sts.Status.CurrentReplicas {
			continue
		}

		if sts.Status.CurrentReplicas != 1 {
			continue
		}

		break
	}

	logging.FromContext(ctx).Info("Statefulset is ready", "cluster", cluster.Name, "instance", name)

	return nil
}

func (r *PGUpgradeReconciler) createReplica(ctx context.Context, cluster *pgv2.PerconaPGCluster) error {
	orig := cluster.DeepCopy()

	instanceSet := cluster.Spec.InstanceSets[0]
	instanceSet.Name = "upgrade"
	instanceSet.Replicas = ptr.To(int32(1))

	cluster.Spec.InstanceSets = append(cluster.Spec.InstanceSets, instanceSet)

	err := r.Client.Patch(ctx, cluster, client.MergeFrom(orig))
	if err != nil {
		return errors.Wrap(err, "patch pgCluster")
	}

	return nil
}

func (r *PGUpgradeReconciler) updateInstance(
	ctx context.Context,
	cluster *pgv2.PerconaPGCluster,
	cr *pgv2.PerconaPGUpgrade,
) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		sts, err := r.getStatefulSetForInstance(ctx, cluster, "upgrade")
		if err != nil {
			return errors.Wrap(err, "get statefulset")
		}

		if sts.Annotations == nil {
			sts.Annotations = make(map[string]string)
		}
		sts.Annotations[naming.SkipReconciliationAnnotation] = "true"

		i := slices.IndexFunc(sts.Spec.Template.Spec.Containers, func(c corev1.Container) bool {
			return c.Name == "database"
		})
		dbContainer := sts.Spec.Template.Spec.Containers[i]

		dbContainer.Image = *cr.Spec.Image
		dbContainer.LivenessProbe = nil
		dbContainer.ReadinessProbe = nil
		dbContainer.Command = []string{"sleep", "infinity"}

		sts.Spec.Template.Spec.Containers = []corev1.Container{dbContainer}
		err = r.Client.Update(ctx, sts)
		if err != nil {
			return err
		}

		logging.FromContext(ctx).Info("StatefulSet updated", "sts", sts.Name)

		return nil
	})
}
