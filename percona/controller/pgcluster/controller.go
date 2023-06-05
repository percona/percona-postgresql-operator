package pgcluster

import (
	"context"
	"fmt"
	"reflect"
	"time"

	"github.com/pkg/errors"
	"go.opentelemetry.io/otel/trace"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	"github.com/percona/percona-postgresql-operator/internal/logging"
	"github.com/percona/percona-postgresql-operator/internal/naming"
	"github.com/percona/percona-postgresql-operator/percona/k8s"
	"github.com/percona/percona-postgresql-operator/percona/pmm"
	"github.com/percona/percona-postgresql-operator/pkg/apis/pgv2.percona.com/v2beta1"
	"github.com/percona/percona-postgresql-operator/pkg/apis/postgres-operator.crunchydata.com/v1beta1"
)

const (
	// ControllerName is the name of the PerconaPGCluster controller
	PGClusterControllerName = "perconapgcluster-controller"
)

// Reconciler holds resources for the PerconaPGCluster reconciler
type PGClusterReconciler struct {
	Client            client.Client
	Owner             client.FieldOwner
	Recorder          record.EventRecorder
	Tracer            trace.Tracer
	Platform          string
	KubeVersion       string
	CrunchyController controller.Controller
}

// SetupWithManager adds the PerconaPGCluster controller to the provided runtime manager
func (r *PGClusterReconciler) SetupWithManager(mgr manager.Manager) error {
	if err := r.CrunchyController.Watch(&source.Kind{Type: &corev1.Secret{}}, r.watchSecrets()); err != nil {
		return errors.Wrap(err, "unable to watch secrets")
	}

	return builder.ControllerManagedBy(mgr).
		For(&v2beta1.PerconaPGCluster{}).
		Owns(&v1beta1.PostgresCluster{}).
		Watches(&source.Kind{Type: &corev1.Service{}}, r.watchServices()).
		Watches(&source.Kind{Type: &corev1.Secret{}}, r.watchSecrets()).
		Complete(r)
}

func (r *PGClusterReconciler) watchServices() handler.Funcs {
	return handler.Funcs{
		UpdateFunc: func(e event.UpdateEvent, q workqueue.RateLimitingInterface) {
			labels := e.ObjectNew.GetLabels()
			crName := labels[naming.LabelCluster]

			if e.ObjectNew.GetName() == crName+"-pgbouncer" {
				q.Add(reconcile.Request{NamespacedName: client.ObjectKey{
					Namespace: e.ObjectNew.GetNamespace(),
					Name:      crName,
				}})
			}
		},
	}
}

func (r *PGClusterReconciler) watchSecrets() handler.Funcs {
	return handler.Funcs{
		UpdateFunc: func(e event.UpdateEvent, q workqueue.RateLimitingInterface) {
			labels := e.ObjectNew.GetLabels()
			crName := labels[naming.LabelCluster]

			if len(crName) != 0 &&
				!reflect.DeepEqual(e.ObjectNew.(*corev1.Secret).Data, e.ObjectOld.(*corev1.Secret).Data) {
				q.Add(reconcile.Request{NamespacedName: client.ObjectKey{
					Namespace: e.ObjectNew.GetNamespace(),
					Name:      crName,
				}})
			}
		},
	}
}

// +kubebuilder:rbac:groups=pgv2.percona.com,resources=perconapgclusters,verbs=get;list;watch
// +kubebuilder:rbac:groups=pgv2.percona.com,resources=perconapgclusters/status,verbs=patch;update
// +kubebuilder:rbac:groups=postgres-operator.crunchydata.com,resources=postgresclusters,verbs=get;list;create;update;patch;delete;watch
// +kubebuilder:rbac:groups=apps,resources=replicasets,verbs=create;delete;get;list;patch;watch

func (r *PGClusterReconciler) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	log := logging.FromContext(ctx)

	cr := &v2beta1.PerconaPGCluster{}
	if err := r.Client.Get(ctx, request.NamespacedName, cr); err != nil {
		// NotFound cannot be fixed by requeuing so ignore it. During background
		// deletion, we receive delete events from cluster's dependents after
		// cluster is deleted.
		if err = client.IgnoreNotFound(err); err != nil {
			log.Error(err, "unable to fetch PerconaPGCluster")
		}
		return ctrl.Result{}, err
	}

	postgresCluster := &v1beta1.PostgresCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cr.Name,
			Namespace: cr.Namespace,
		},
	}

	if cr.DeletionTimestamp != nil {
		// We're deleting PostgresCluster explicitly to let Crunchy controller run its finalizers and not mess with us.
		if err := r.Client.Delete(ctx, postgresCluster); client.IgnoreNotFound(err) != nil {
			return ctrl.Result{RequeueAfter: 5 * time.Second}, errors.Wrap(err, "delete postgres cluster")
		}

		if err := r.Client.Get(ctx, client.ObjectKeyFromObject(postgresCluster), postgresCluster); err == nil {
			log.Info("Waiting for PostgresCluster to be deleted")
			return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
		}

		if err := r.runFinalizers(ctx, cr); err != nil {
			return reconcile.Result{RequeueAfter: 5 * time.Second}, err
		}

		return reconcile.Result{}, nil
	}

	if err := r.reconcileVersion(ctx, cr); err != nil {
		return reconcile.Result{}, errors.Wrap(err, "reconcile version")
	}

	if err := controllerutil.SetControllerReference(cr, postgresCluster, r.Client.Scheme()); err != nil {
		return ctrl.Result{}, err
	}

	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, postgresCluster, func() error {
		postgresCluster.Default()

		annotations := make(map[string]string)
		for k, v := range cr.Annotations {
			switch k {
			case v2beta1.AnnotationPGBackrestBackup:
				annotations[naming.PGBackRestBackup] = v
			case v2beta1.AnnotationPGBackRestRestore:
				annotations[naming.PGBackRestRestore] = v
			case corev1.LastAppliedConfigAnnotation:
				continue
			default:
				annotations[k] = v
			}
		}
		postgresCluster.Annotations = annotations
		postgresCluster.Labels = cr.Labels

		postgresCluster.Spec.Image = cr.Spec.Image
		postgresCluster.Spec.ImagePullPolicy = cr.Spec.ImagePullPolicy
		postgresCluster.Spec.ImagePullSecrets = cr.Spec.ImagePullSecrets

		postgresCluster.Spec.PostgresVersion = cr.Spec.PostgresVersion
		postgresCluster.Spec.Port = cr.Spec.Port
		postgresCluster.Spec.OpenShift = cr.Spec.OpenShift
		postgresCluster.Spec.Paused = cr.Spec.Unmanaged
		postgresCluster.Spec.Shutdown = cr.Spec.Pause
		postgresCluster.Spec.Standby = cr.Spec.Standby
		postgresCluster.Spec.Service = cr.Spec.Expose.ToCrunchy()

		postgresCluster.Spec.CustomReplicationClientTLSSecret = cr.Spec.Secrets.CustomReplicationClientTLSSecret
		postgresCluster.Spec.CustomTLSSecret = cr.Spec.Secrets.CustomTLSSecret

		postgresCluster.Spec.Backups = cr.Spec.Backups
		postgresCluster.Spec.DataSource = cr.Spec.DataSource
		postgresCluster.Spec.DatabaseInitSQL = cr.Spec.DatabaseInitSQL
		postgresCluster.Spec.Patroni = cr.Spec.Patroni

		users := make([]v1beta1.PostgresUserSpec, 0)

		for _, user := range cr.Spec.Users {
			un := string(user.Name)
			if un == pmm.MonitoringUser || un == cr.Name {
				log.Info(un + " user is reserved, it'll be ignored.")
				continue
			}
			users = append(users, user)
		}

		if cr.Spec.PMM.Enabled {
			users = append(cr.Spec.Users, v1beta1.PostgresUserSpec{
				Name:    pmm.MonitoringUser,
				Options: "SUPERUSER",
				Password: &v1beta1.PostgresPasswordSpec{
					Type: v1beta1.PostgresPasswordTypeAlphaNumeric,
				},
			})
		}

		postgresCluster.Spec.Users = users

		// Add default user: <cluster-name>-pguser-<cluster-name>
		postgresCluster.Spec.Users = append(postgresCluster.Spec.Users, v1beta1.PostgresUserSpec{
			Name: v1beta1.PostgresIdentifier(cr.Name),
			Databases: []v1beta1.PostgresIdentifier{
				v1beta1.PostgresIdentifier(cr.Name),
			},
			Password: &v1beta1.PostgresPasswordSpec{
				Type: v1beta1.PostgresPasswordTypeAlphaNumeric,
			},
		})

		if err := r.addPMMSidecar(ctx, cr); err != nil {
			return errors.Wrap(err, "failed to add pmm sidecar")
		}

		postgresCluster.Spec.InstanceSets = cr.Spec.InstanceSets.ToCrunchy()
		postgresCluster.Spec.Proxy = cr.Spec.Proxy.ToCrunchy()

		return nil
	})
	if err != nil {
		return ctrl.Result{}, errors.Wrap(err, "update/create PostgresCluster")
	}

	if err := r.Client.Get(ctx, client.ObjectKeyFromObject(postgresCluster), postgresCluster); err != nil {
		return ctrl.Result{}, errors.Wrap(err, "get PostgresCluster")
	}

	err = r.updateStatus(ctx, cr, &postgresCluster.Status)

	return ctrl.Result{}, err
}

func (r *PGClusterReconciler) addPMMSidecar(ctx context.Context, cr *v2beta1.PerconaPGCluster) error {
	if cr.Spec.PMM == nil || !cr.Spec.PMM.Enabled {
		return nil
	}

	log := logging.FromContext(ctx)

	if cr.Spec.PMM.Secret == "" {
		log.Info(fmt.Sprintf("Can't enable PMM: `.spec.pmm.secret` is empty in %s", cr.Name))
		return nil
	}

	pmmSecret := new(corev1.Secret)
	if err := r.Client.Get(ctx, types.NamespacedName{
		Name:      cr.Spec.PMM.Secret,
		Namespace: cr.Namespace,
	}, pmmSecret); err != nil {
		if k8serrors.IsNotFound(err) {
			log.Info(fmt.Sprintf("Can't enable PMM: %s secret doesn't exist", cr.Spec.PMM.Secret))
			return nil
		}
		return errors.Wrap(err, "failed to get pmm secret")
	}

	if v, ok := pmmSecret.Data[pmm.SecretKey]; !ok || len(v) == 0 {
		log.Info(fmt.Sprintf("Can't enable PMM: %s key doesn't exist in %s secret or empty", pmm.SecretKey, cr.Spec.PMM.Secret))
		return nil
	}

	if pmmSecret.Labels == nil {
		pmmSecret.Labels = make(map[string]string)
	}

	_, pmmSecretLabelOK := pmmSecret.Labels[v2beta1.LabelPMMSecret]
	_, clusterLabelOK := pmmSecret.Labels[naming.LabelCluster]
	if !pmmSecretLabelOK || !clusterLabelOK {
		orig := pmmSecret.DeepCopy()
		pmmSecret.Labels[v2beta1.LabelPMMSecret] = "true"
		pmmSecret.Labels[naming.LabelCluster] = cr.Name
		if err := r.Client.Patch(ctx, pmmSecret, client.MergeFrom(orig)); err != nil {
			return errors.Wrap(err, "label PMM secret")
		}
	}

	pmmSecretHash, err := k8s.ObjectHash(pmmSecret)
	if err != nil {
		return errors.Wrap(err, "hash PMM secret")
	}

	for i := 0; i < len(cr.Spec.InstanceSets); i++ {
		set := &cr.Spec.InstanceSets[i]

		if set.Metadata == nil {
			set.Metadata = &v1beta1.Metadata{}
		}
		if set.Metadata.Annotations == nil {
			set.Metadata.Annotations = make(map[string]string)
		}
		set.Metadata.Annotations[v2beta1.AnnotationPMMSecretHash] = pmmSecretHash

		set.Sidecars = append(set.Sidecars, pmm.SidecarContainer(cr))
	}

	return nil
}
