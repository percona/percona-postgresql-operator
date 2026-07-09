package logcollector

import (
	"context"
	"reflect"

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/percona/percona-postgresql-operator/v2/internal/naming"
	"github.com/percona/percona-postgresql-operator/v2/percona/logcollector/logrotate"
	v2 "github.com/percona/percona-postgresql-operator/v2/pkg/apis/pgv2.percona.com/v2"
)

// Reconcile wires the log collector sidecars, volumes, and backing ConfigMaps for the given cluster.
func Reconcile(ctx context.Context, c client.Client, cr *v2.PerconaPGCluster) error {
	if cr.CompareVersion("3.1.0") < 0 {
		return nil
	}

	if err := wireSidecars(cr); err != nil {
		return errors.Wrap(err, "wire log collector sidecars")
	}

	if err := reconcileConfigMaps(ctx, c, cr); err != nil {
		return errors.Wrap(err, "reconcile log collector config maps")
	}

	return nil
}

func wireSidecars(cr *v2.PerconaPGCluster) error {
	if !cr.LogCollectorEnabled() {
		return nil
	}

	containers, err := instanceContainers(cr)
	if err != nil {
		return errors.Wrap(err, "build instance containers")
	}

	volumes := volumes(cr)

	// The log collector currently runs only on PostgreSQL instance pods. These
	// collect Postgres server logs and pgBackRest client/archive logs
	// (/pgdata/pgbackrest/log). Collecting pgBackRest server logs from the
	// dedicated repo host needs additional volume wiring on that pod and is not
	// yet supported.
	for i := range cr.Spec.InstanceSets {
		cr.Spec.InstanceSets[i].Sidecars = append(cr.Spec.InstanceSets[i].Sidecars, containers...)
		cr.Spec.InstanceSets[i].SidecarVolumes = append(cr.Spec.InstanceSets[i].SidecarVolumes, volumes...)
	}

	return nil
}

func reconcileConfigMaps(ctx context.Context, c client.Client, cr *v2.PerconaPGCluster) error {
	if err := reconcileFluentBitConfigMap(ctx, c, cr); err != nil {
		return errors.Wrap(err, "fluent-bit config map")
	}
	if err := reconcileLogRotateConfigMap(ctx, c, cr); err != nil {
		return errors.Wrap(err, "logrotate config map")
	}
	return nil
}

func reconcileFluentBitConfigMap(ctx context.Context, c client.Client, cr *v2.PerconaPGCluster) error {
	name := configMapName(cr.Name)

	if !cr.LogCollectorEnabled() || cr.Spec.LogCollector.Configuration == "" {
		return deleteConfigMapIfExists(ctx, c, cr.Namespace, name)
	}

	return createOrUpdateConfigMap(ctx, c, &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: cr.Namespace,
			Labels:    map[string]string{naming.LabelCluster: cr.Name},
		},
		Data: map[string]string{
			fluentBitCustomConfigurationFile: cr.Spec.LogCollector.Configuration,
		},
	})
}

func reconcileLogRotateConfigMap(ctx context.Context, c client.Client, cr *v2.PerconaPGCluster) error {
	name := logrotate.ConfigMapName(cr.Name)

	if !cr.LogCollectorEnabled() ||
		cr.Spec.LogCollector.LogRotate == nil ||
		cr.Spec.LogCollector.LogRotate.Configuration == "" {
		return deleteConfigMapIfExists(ctx, c, cr.Namespace, name)
	}

	return createOrUpdateConfigMap(ctx, c, &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: cr.Namespace,
			Labels:    map[string]string{naming.LabelCluster: cr.Name},
		},
		Data: map[string]string{
			logrotate.PostgresConfig: cr.Spec.LogCollector.LogRotate.Configuration,
		},
	})
}

func deleteConfigMapIfExists(ctx context.Context, c client.Client, namespace, name string) error {
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
	}
	if err := c.Delete(ctx, cm); err != nil && !k8serrors.IsNotFound(err) {
		return errors.Wrapf(err, "delete config map %s/%s", namespace, name)
	}
	return nil
}

func createOrUpdateConfigMap(ctx context.Context, c client.Client, desired *corev1.ConfigMap) error {
	existing := &corev1.ConfigMap{}
	err := c.Get(ctx, types.NamespacedName{Name: desired.Name, Namespace: desired.Namespace}, existing)
	if k8serrors.IsNotFound(err) {
		return c.Create(ctx, desired)
	}
	if err != nil {
		return err
	}
	if reflect.DeepEqual(existing.Data, desired.Data) && reflect.DeepEqual(existing.Labels, desired.Labels) {
		return nil
	}
	existing.Data = desired.Data
	existing.Labels = desired.Labels
	return c.Update(ctx, existing)
}
