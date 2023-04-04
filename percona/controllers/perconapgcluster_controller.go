package controllers

import (
	"context"
	"os"
	"strconv"

	"github.com/pkg/errors"
	"go.opentelemetry.io/otel/trace"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/percona/percona-postgresql-operator/internal/logging"
	"github.com/percona/percona-postgresql-operator/internal/naming"
	"github.com/percona/percona-postgresql-operator/percona/k8s"
	"github.com/percona/percona-postgresql-operator/percona/pmm"
	"github.com/percona/percona-postgresql-operator/percona/version"
	"github.com/percona/percona-postgresql-operator/pkg/apis/pg.percona.com/v2beta1"
	"github.com/percona/percona-postgresql-operator/pkg/apis/postgres-operator.crunchydata.com/v1beta1"
)

const (
	// ControllerName is the name of the PerconaPGCluster controller
	PGClusterControllerName = "perconapgcluster-controller"
)

// Reconciler holds resources for the PerconaPGCluster reconciler
type PGClusterReconciler struct {
	Client      client.Client
	Owner       client.FieldOwner
	Recorder    record.EventRecorder
	Tracer      trace.Tracer
	Platform    string
	KubeVersion string
}

// SetupWithManager adds the PerconaPGCluster controller to the provided runtime manager
func (r *PGClusterReconciler) SetupWithManager(mgr manager.Manager) error {
	return builder.ControllerManagedBy(mgr).For(&v2beta1.PerconaPGCluster{}).Owns(&v1beta1.PostgresCluster{}).Complete(r)
}

// +kubebuilder:rbac:groups=pg.percona.com,resources=perconapgclusters,verbs=get;list;watch
// +kubebuilder:rbac:groups=pg.percona.com,resources=perconapgclusters/status,verbs=patch;update
// +kubebuilder:rbac:groups=postgres-operator.crunchydata.com,resources=postgresclusters,verbs=get;list;create;update;patch;watch
// +kubebuilder:rbac:groups=apps,resources=replicasets,verbs=create;delete;get;list;patch;watch

func (r *PGClusterReconciler) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	log := logging.FromContext(ctx)

	perconaPGCluster := &v2beta1.PerconaPGCluster{}
	if err := r.Client.Get(ctx, request.NamespacedName, perconaPGCluster); err != nil {
		// NotFound cannot be fixed by requeuing so ignore it. During background
		// deletion, we receive delete events from cluster's dependents after
		// cluster is deleted.
		if err = client.IgnoreNotFound(err); err != nil {
			log.Error(err, "unable to fetch PerconaPGCluster")
		}
		return reconcile.Result{}, err
	}

	vm, err := r.getVersionMeta(ctx, perconaPGCluster)
	if err != nil {
		return reconcile.Result{}, errors.Wrap(err, "get version meta")
	}

	if err := version.EnsureVersion(ctx, vm); err != nil {
		return reconcile.Result{}, errors.Wrap(err, "ensure versions")
	}

	postgresCluster := &v1beta1.PostgresCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      perconaPGCluster.Name,
			Namespace: perconaPGCluster.Namespace,
		},
	}

	if err := controllerutil.SetControllerReference(perconaPGCluster, postgresCluster, r.Client.Scheme()); err != nil {
		return reconcile.Result{}, err
	}

	_, err = controllerutil.CreateOrUpdate(ctx, r.Client, postgresCluster, func() error {
		postgresCluster.Default()

		annotations := make(map[string]string)
		for k, v := range perconaPGCluster.Annotations {
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
		postgresCluster.Labels = perconaPGCluster.Labels

		postgresCluster.Spec.Image = perconaPGCluster.Spec.Image
		postgresCluster.Spec.ImagePullPolicy = perconaPGCluster.Spec.ImagePullPolicy
		postgresCluster.Spec.ImagePullSecrets = perconaPGCluster.Spec.ImagePullSecrets

		postgresCluster.Spec.PostgresVersion = perconaPGCluster.Spec.PostgresVersion
		postgresCluster.Spec.Port = perconaPGCluster.Spec.Port
		postgresCluster.Spec.OpenShift = perconaPGCluster.Spec.OpenShift
		postgresCluster.Spec.Paused = perconaPGCluster.Spec.Paused
		postgresCluster.Spec.Shutdown = perconaPGCluster.Spec.Shutdown
		postgresCluster.Spec.Standby = perconaPGCluster.Spec.Standby
		postgresCluster.Spec.Service = perconaPGCluster.Spec.Expose.ToCrunchy()

		postgresCluster.Spec.CustomReplicationClientTLSSecret = perconaPGCluster.Spec.Secrets.CustomReplicationClientTLSSecret
		postgresCluster.Spec.CustomTLSSecret = perconaPGCluster.Spec.Secrets.CustomTLSSecret

		postgresCluster.Spec.Backups = perconaPGCluster.Spec.Backups
		postgresCluster.Spec.DataSource = perconaPGCluster.Spec.DataSource
		postgresCluster.Spec.DatabaseInitSQL = perconaPGCluster.Spec.DatabaseInitSQL
		postgresCluster.Spec.Patroni = perconaPGCluster.Spec.Patroni

		if perconaPGCluster.Spec.PMM != nil && perconaPGCluster.Spec.PMM.Enabled {
			// TODO: Check if PMM secret exists
			for i := 0; i < len(perconaPGCluster.Spec.InstanceSets); i++ {
				set := &perconaPGCluster.Spec.InstanceSets[i]
				set.Sidecars = append(set.Sidecars, pmm.SidecarContainer(perconaPGCluster))
			}
		}

		postgresCluster.Spec.InstanceSets = perconaPGCluster.Spec.InstanceSets.ToCrunchy()
		postgresCluster.Spec.Users = perconaPGCluster.Spec.Users
		postgresCluster.Spec.Proxy = perconaPGCluster.Spec.Proxy.ToCrunchy()

		return nil
	})

	if err := r.Client.Get(ctx, client.ObjectKeyFromObject(postgresCluster), postgresCluster); err != nil {
		return reconcile.Result{}, errors.Wrap(err, "get PostgresCluster")
	}

	perconaPGCluster.Status = v2beta1.PerconaPGClusterStatus{
		PostgresClusterStatus: postgresCluster.Status,
	}

	if err := r.Client.Status().Update(ctx, perconaPGCluster); err != nil {
		log.Error(err, "failed to update status")
	}

	return reconcile.Result{}, err
}

func (r *PGClusterReconciler) getVersionMeta(ctx context.Context, cr *v2beta1.PerconaPGCluster) (version.Meta, error) {
	vm := version.Meta{
		Apply:           "disabled",
		OperatorVersion: version.Version,
		CRUID:           string(cr.GetUID()),
		KubeVersion:     r.KubeVersion,
		Platform:        r.Platform,
		PGVersion:       strconv.Itoa(cr.Spec.PostgresVersion),
		BackupVersion:   "",
		PMMVersion:      "",
		PMMEnabled:      cr.Spec.PMM != nil && cr.Spec.PMM.Enabled,
	}
	if _, ok := cr.Labels["helm.sh/chart"]; ok {
		vm.HelmDeployCR = true
	}
	for _, set := range cr.Spec.InstanceSets {
		if len(set.Sidecars) > 0 {
			vm.SidecarsUsed = true
			break
		}
	}
	operatorDepl, err := r.getOperatorDeployment(ctx)
	if err != nil {
		return version.Meta{}, errors.Wrap(err, "failed to get operator deployment")
	}
	if _, ok := operatorDepl.Labels["helm.sh/chart"]; ok {
		vm.HelmDeployOperator = true
	}
	return vm, nil
}

func (r *PGClusterReconciler) getOperatorDeployment(ctx context.Context) (*appsv1.Deployment, error) {
	ns, err := k8s.GetOperatorNamespace()
	if err != nil {
		return nil, errors.Wrap(err, "failed to get operator namespace")
	}
	name, err := os.Hostname()
	if err != nil {
		return nil, errors.Wrap(err, "failed to get operator hostname")
	}

	pod := new(corev1.Pod)
	err = r.Client.Get(ctx, types.NamespacedName{Namespace: ns, Name: name}, pod)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get operator pod")
	}
	if len(pod.OwnerReferences) == 0 {
		return nil, errors.New("operator pod has no owner reference")
	}

	rs := new(appsv1.ReplicaSet)
	err = r.Client.Get(ctx, types.NamespacedName{Namespace: pod.Namespace, Name: pod.OwnerReferences[0].Name}, rs)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get operator replicaset")
	}
	if len(rs.OwnerReferences) == 0 {
		return nil, errors.New("operator replicaset has no owner reference")
	}

	depl := new(appsv1.Deployment)
	err = r.Client.Get(ctx, types.NamespacedName{Namespace: pod.Namespace, Name: rs.OwnerReferences[0].Name}, depl)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get operator deployment")
	}

	return depl, nil
}
