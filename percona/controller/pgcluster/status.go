package pgcluster

import (
	"context"

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/percona/percona-postgresql-operator/pkg/apis/pg.percona.com/v2beta1"
	"github.com/percona/percona-postgresql-operator/pkg/apis/postgres-operator.crunchydata.com/v1beta1"
)

func (r *PGClusterReconciler) getHost(ctx context.Context, cr *v2beta1.PerconaPGCluster) (string, error) {
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

func (r *PGClusterReconciler) getState(cr *v2beta1.PerconaPGCluster, status *v1beta1.PostgresClusterStatus) v2beta1.AppState {
	var size, ready int
	for _, is := range status.InstanceSets {
		size = size + int(is.Replicas)
		ready = ready + int(is.ReadyReplicas)
	}

	if cr.Spec.Pause != nil && *cr.Spec.Pause {
		if ready > size {
			return v2beta1.AppStateStopping
		}

		return v2beta1.AppStatePaused
	}

	if status.PGBackRest != nil && status.PGBackRest.RepoHost != nil && !status.PGBackRest.RepoHost.Ready {
		return v2beta1.AppStateInit
	}

	if status.Proxy.PGBouncer.ReadyReplicas != status.Proxy.PGBouncer.Replicas {
		return v2beta1.AppStateInit
	}

	if ready < size {
		return v2beta1.AppStateInit
	}

	return v2beta1.AppStateReady
}

func (r *PGClusterReconciler) updateStatus(ctx context.Context, cr *v2beta1.PerconaPGCluster, status *v1beta1.PostgresClusterStatus) error {
	host, err := r.getHost(ctx, cr)
	if err != nil {
		return errors.Wrap(err, "get app host")
	}

	var size, ready int32
	for _, is := range status.InstanceSets {
		size = size + is.Replicas
		ready = ready + is.ReadyReplicas
	}

	pgStatusFromCruncy := func() []v2beta1.PostgresInstanceSetStatus {
		ss := make([]v2beta1.PostgresInstanceSetStatus, 0, len(status.InstanceSets))

		for _, is := range status.InstanceSets {
			ss = append(ss, v2beta1.PostgresInstanceSetStatus{
				Name:  is.Name,
				Size:  is.Replicas,
				Ready: is.ReadyReplicas,
			})
		}

		return ss
	}

	cr.Status = v2beta1.PerconaPGClusterStatus{
		Postgres: v2beta1.PostgresStatus{
			Size:         size,
			Ready:        ready,
			InstanceSets: pgStatusFromCruncy(),
		},
		PGBouncer: v2beta1.PGBouncerStatus{
			Size:  status.Proxy.PGBouncer.Replicas,
			Ready: status.Proxy.PGBouncer.ReadyReplicas,
		},
		State: r.getState(cr, status),
		Host:  host,
	}

	return r.Client.Status().Update(ctx, cr)
}
