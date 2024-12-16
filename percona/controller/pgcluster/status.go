package pgcluster

import (
	"context"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"

	v2 "github.com/percona/percona-postgresql-operator/pkg/apis/pgv2.percona.com/v2"
	"github.com/percona/percona-postgresql-operator/pkg/apis/postgres-operator.crunchydata.com/v1beta1"
)

func (r *PGClusterReconciler) getHost(ctx context.Context, cr *v2.PerconaPGCluster) (string, error) {
	svcName := cr.Name + "-pgbouncer"

	if cr.Spec.Proxy.PGBouncer.ServiceExpose == nil || cr.Spec.Proxy.PGBouncer.ServiceExpose.Type != string(corev1.ServiceTypeLoadBalancer) {
		return svcName + "." + cr.Namespace + ".svc", nil
	}

	svc := &corev1.Service{}
	err := r.Client.Get(ctx, types.NamespacedName{Namespace: cr.Namespace, Name: svcName}, svc)
	if err != nil {
		return "", errors.Wrapf(err, "get %s service", svcName)
	}

	var host string
	for _, i := range svc.Status.LoadBalancer.Ingress {
		host = i.IP
		if len(i.Hostname) > 0 {
			host = i.Hostname
		}
	}

	return host, nil
}

func (r *PGClusterReconciler) getState(cr *v2.PerconaPGCluster, status *v2.PerconaPGClusterStatus, crunchyStatus *v1beta1.PostgresClusterStatus) v2.AppState {
	if cr.Spec.Pause != nil && *cr.Spec.Pause {
		if status.Postgres.Ready > 0 {
			return v2.AppStateStopping
		}

		return v2.AppStatePaused
	}

	if crunchyStatus.PGBackRest != nil && crunchyStatus.PGBackRest.RepoHost != nil && !crunchyStatus.PGBackRest.RepoHost.Ready {
		return v2.AppStateInit
	}

	if status.PGBouncer.Ready != status.PGBouncer.Size {
		return v2.AppStateInit
	}

	if status.Postgres.Ready != status.Postgres.Size {
		return v2.AppStateInit
	}

	var updatedPods int32
	for _, is := range crunchyStatus.InstanceSets {
		updatedPods += is.UpdatedReplicas
	}
	if updatedPods != status.Postgres.Size {
		return v2.AppStateInit
	}

	if status.Postgres.Size == 0 {
		return v2.AppStateInit
	}

	return v2.AppStateReady
}

func (r *PGClusterReconciler) updateStatus(ctx context.Context, cr *v2.PerconaPGCluster, status *v1beta1.PostgresClusterStatus) error {
	host, err := r.getHost(ctx, cr)
	if err != nil {
		return errors.Wrap(err, "get app host")
	}

	installedCustomExtensions := make([]string, 0)
	for _, extension := range cr.Spec.Extensions.Custom {
		installedCustomExtensions = append(installedCustomExtensions, extension.Name)
	}

	var size, ready int32
	ss := make([]v2.PostgresInstanceSetStatus, 0, len(status.InstanceSets))
	for _, is := range status.InstanceSets {
		ss = append(ss, v2.PostgresInstanceSetStatus{
			Name:  is.Name,
			Size:  is.Replicas,
			Ready: is.ReadyReplicas,
		})

		size += is.Replicas
		ready += is.ReadyReplicas
	}

	if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		cluster := &v2.PerconaPGCluster{}
		if err := r.Client.Get(ctx, types.NamespacedName{
			Name:      cr.Name,
			Namespace: cr.Namespace,
		}, cluster); err != nil {
			return errors.Wrap(err, "get PerconaPGCluster")
		}

		cluster.Status = v2.PerconaPGClusterStatus{
			Postgres: v2.PostgresStatus{
				Size:         size,
				Ready:        ready,
				InstanceSets: ss,
			},
			PGBouncer: v2.PGBouncerStatus{
				Size:  status.Proxy.PGBouncer.Replicas,
				Ready: status.Proxy.PGBouncer.ReadyReplicas,
			},
			Host: host,

			InstalledCustomExtensions: installedCustomExtensions,
		}

		cluster.Status.State = r.getState(cr, &cluster.Status, status)

		return r.Client.Status().Update(ctx, cluster)
	}); err != nil {
		return errors.Wrap(err, "update PerconaPGCluster status")
	}

	return nil
}
