package pgcluster

import (
	"context"
	ll "log"
	"os"
	"strconv"

	"github.com/pkg/errors"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/percona/percona-postgresql-operator/internal/logging"
	"github.com/percona/percona-postgresql-operator/percona/k8s"
	"github.com/percona/percona-postgresql-operator/percona/version"
	v2 "github.com/percona/percona-postgresql-operator/pkg/apis/pgv2.percona.com/v2"
)

func (r *PGClusterReconciler) reconcileVersion(ctx context.Context, cr *v2.PerconaPGCluster) error {
	if !telemetryEnabled() {
		return nil
	}
	log := logging.FromContext(ctx)

	operatorDepl, err := r.getOperatorDeployment(ctx)
	if err != nil {
		return errors.Wrap(err, "failed to get operator deployment")
	}
	vm := r.getVersionMeta(cr, operatorDepl)

	if err := version.EnsureVersion(ctx, vm); err != nil {
		// we don't return here to not block execution just because we can't phone home
		log.Error(err, "ensure version")
	}

	return nil
}

func (r *PGClusterReconciler) getVersionMeta(cr *v2.PerconaPGCluster, operatorDepl *appsv1.Deployment) version.Meta {
	vm := version.Meta{
		Apply:           "disabled",
		OperatorVersion: v2.Version,
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

	if operatorDepl != nil {
		if _, ok := operatorDepl.Labels["helm.sh/chart"]; ok {
			vm.HelmDeployOperator = true
		}
	}

	return vm
}

func (r *PGClusterReconciler) getOperatorDeployment(ctx context.Context) (*appsv1.Deployment, error) {
	ns, err := k8s.GetOperatorNamespace()
	if err != nil {
		// if operator is running outside of k8s, this will fail to get namespace
		// but we don't want to fail everything because of that.
		//nolint:nilerr
		return nil, nil
	}
	name, err := os.Hostname()
	if err != nil {
		return nil, errors.Wrap(err, "failed to get operator hostname")
	}

	ll.Printf("AAAAAAAAAAA namespace: %s, hostname: %s", ns, name)

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

func telemetryEnabled() bool {
	value, ok := os.LookupEnv("DISABLE_TELEMETRY")
	if ok {
		return value != "true"
	}
	return true
}
