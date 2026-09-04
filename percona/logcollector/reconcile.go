package logcollector

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/percona/percona-postgresql-operator/v3/internal/naming"
	"github.com/percona/percona-postgresql-operator/v3/percona/logcollector/logrotate"
	pNaming "github.com/percona/percona-postgresql-operator/v3/percona/naming"
	v2 "github.com/percona/percona-postgresql-operator/v3/pkg/apis/pgv2.percona.com/v2"
	crunchyv1beta1 "github.com/percona/percona-postgresql-operator/v3/pkg/apis/upstream.pgv2.percona.com/v1beta1"
)

// Reconcile wires the log collector sidecars, volumes, and backing ConfigMaps for the given cluster.
func Reconcile(ctx context.Context, c client.Client, cr *v2.PerconaPGCluster) error {
	if cr.CompareVersion("3.1.0") < 0 {
		return nil
	}

	if err := resolveDefaultEnabled(ctx, c, cr); err != nil {
		return errors.Wrap(err, "resolve log collector default")
	}

	if err := wireSidecars(ctx, c, cr); err != nil {
		return errors.Wrap(err, "wire log collector sidecars")
	}

	if err := reconcileConfigMaps(ctx, c, cr); err != nil {
		return errors.Wrap(err, "reconcile log collector config maps")
	}

	return nil
}

// resolveDefaultEnabled defaults an unset Enabled to on for new clusters and off
// for existing ones, keyed on whether the crunchy PostgresCluster already exists.
func resolveDefaultEnabled(ctx context.Context, c client.Client, cr *v2.PerconaPGCluster) error {
	if cr.Spec.LogCollector == nil || cr.Spec.LogCollector.Enabled != nil {
		return nil
	}

	existing := &crunchyv1beta1.PostgresCluster{}
	err := c.Get(ctx, types.NamespacedName{Name: cr.Name, Namespace: cr.Namespace}, existing)
	if err != nil && !k8serrors.IsNotFound(err) {
		return errors.Wrap(err, "get postgres cluster")
	}

	var enabled bool
	if k8serrors.IsNotFound(err) {
		enabled = true
	} else {
		// preserve the existing state
		enabled = hasLogCollectorSidecar(existing)
	}
	cr.Spec.LogCollector.Enabled = &enabled
	return nil
}

func hasLogCollectorSidecar(pg *crunchyv1beta1.PostgresCluster) bool {
	for i := range pg.Spec.InstanceSets {
		for _, c := range pg.Spec.InstanceSets[i].Containers {
			if c.Name == containerName {
				return true
			}
		}
	}
	return false
}

func wireSidecars(ctx context.Context, c client.Client, cr *v2.PerconaPGCluster) error {
	if !cr.LogCollectorEnabled() {
		return nil
	}

	containers, err := instanceContainers(cr)
	if err != nil {
		return errors.Wrap(err, "build instance containers")
	}

	initContainers := instanceInitContainers(cr)

	volumes := volumes(cr)

	// The config is delivered through ConfigMaps mounted by a stable name, so a
	// content change does not alter the pod template by itself. Stamping its hash
	// onto the instance metadata rolls the pods when the config changes.
	hash, err := configHash(ctx, c, cr)
	if err != nil {
		return errors.Wrap(err, "compute config hash")
	}

	for i := range cr.Spec.InstanceSets {
		set := &cr.Spec.InstanceSets[i]
		set.Sidecars = append(set.Sidecars, containers...)
		set.SidecarVolumes = append(set.SidecarVolumes, volumes...)
		set.InitContainers = append(set.InitContainers, initContainers...)

		if set.Metadata == nil {
			set.Metadata = &crunchyv1beta1.Metadata{}
		}
		if set.Metadata.Annotations == nil {
			set.Metadata.Annotations = make(map[string]string)
		}
		set.Metadata.Annotations[pNaming.AnnotationLogCollectorConfigHash] = hash
	}

	return nil
}

func configHash(ctx context.Context, c client.Client, cr *v2.PerconaPGCluster) (string, error) {
	payload := struct {
		FluentBit       string                       `json:"fluentBit"`
		LogRotate       string                       `json:"logRotate"`
		ExtraConfigData map[string]map[string]string `json:"extraConfigData"`
	}{
		FluentBit:       cr.Spec.LogCollector.Configuration,
		ExtraConfigData: make(map[string]map[string]string),
	}
	if lr := cr.Spec.LogCollector.LogRotate; lr != nil {
		payload.LogRotate = lr.Configuration
	}

	// Fold the referenced ConfigMaps' contents into the hash so edits to them
	// roll the pods; the mount name alone stays stable across edits.
	for _, name := range cr.LogRotateExtraConfigMaps() {
		cm := &corev1.ConfigMap{}
		err := c.Get(ctx, types.NamespacedName{Name: name, Namespace: cr.Namespace}, cm)
		if err != nil && !k8serrors.IsNotFound(err) {
			return "", errors.Wrapf(err, "get extra config map %s", name)
		}
		payload.ExtraConfigData[name] = cm.Data
	}

	data, _ := json.Marshal(payload)
	return fmt.Sprintf("%x", sha256.Sum256(data)), nil
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

	return createOrUpdateConfigMap(ctx, c, cr, &corev1.ConfigMap{
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

	return createOrUpdateConfigMap(ctx, c, cr, &corev1.ConfigMap{
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

func createOrUpdateConfigMap(ctx context.Context, c client.Client, cr *v2.PerconaPGCluster, desired *corev1.ConfigMap) error {
	// Own the ConfigMap so it is garbage collected when the cluster is deleted.
	if err := controllerutil.SetControllerReference(cr, desired, c.Scheme()); err != nil {
		return errors.Wrap(err, "set controller reference")
	}

	existing := &corev1.ConfigMap{}
	err := c.Get(ctx, types.NamespacedName{Name: desired.Name, Namespace: desired.Namespace}, existing)
	if k8serrors.IsNotFound(err) {
		return c.Create(ctx, desired)
	}
	if err != nil {
		return err
	}
	if reflect.DeepEqual(existing.Data, desired.Data) &&
		reflect.DeepEqual(existing.Labels, desired.Labels) &&
		reflect.DeepEqual(existing.OwnerReferences, desired.OwnerReferences) {
		return nil
	}
	orig := existing.DeepCopy()
	existing.Data = desired.Data
	existing.Labels = desired.Labels
	existing.OwnerReferences = desired.OwnerReferences
	return c.Patch(ctx, existing, client.MergeFrom(orig))
}
