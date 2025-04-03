// Copyright 2021 - 2024 Crunchy Data Solutions, Inc.
//
// SPDX-License-Identifier: Apache-2.0

package patroni

import (
	"context"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	"github.com/percona/percona-postgresql-operator/internal/initialize"
	"github.com/percona/percona-postgresql-operator/internal/naming"
	"github.com/percona/percona-postgresql-operator/internal/pgbackrest"
	"github.com/percona/percona-postgresql-operator/internal/pki"
	"github.com/percona/percona-postgresql-operator/internal/postgres"
	"github.com/percona/percona-postgresql-operator/percona/k8s"
	pNaming "github.com/percona/percona-postgresql-operator/percona/naming"
	"github.com/percona/percona-postgresql-operator/pkg/apis/postgres-operator.crunchydata.com/v1beta1"
)

// ClusterBootstrapped returns a bool indicating whether or not Patroni has successfully
// bootstrapped the PostgresCluster
func ClusterBootstrapped(postgresCluster *v1beta1.PostgresCluster) bool {
	return postgresCluster.Status.Patroni.SystemIdentifier != ""
}

// ClusterConfigMap populates the shared ConfigMap with fields needed to run Patroni.
func ClusterConfigMap(ctx context.Context,
	inCluster *v1beta1.PostgresCluster,
	inHBAs postgres.HBAs,
	inParameters postgres.Parameters,
	outClusterConfigMap *corev1.ConfigMap,
) error {
	var err error

	initialize.Map(&outClusterConfigMap.Data)

	outClusterConfigMap.Data[configMapFileKey], err = clusterYAML(inCluster, inHBAs,
		inParameters)

	return err
}

// InstanceConfigMap populates the shared ConfigMap with fields needed to run Patroni.
func InstanceConfigMap(ctx context.Context,
	inCluster *v1beta1.PostgresCluster,
	inInstanceSpec *v1beta1.PostgresInstanceSetSpec,
	outInstanceConfigMap *corev1.ConfigMap,
) error {
	var err error

	initialize.Map(&outInstanceConfigMap.Data)

	command := pgbackrest.ReplicaCreateCommand(inCluster, inInstanceSpec)

	outInstanceConfigMap.Data[configMapFileKey], err = instanceYAML(
		inCluster, inInstanceSpec, command)

	return err
}

// InstanceCertificates populates the shared Secret with certificates needed to run Patroni.
func InstanceCertificates(ctx context.Context,
	inRoot pki.Certificate, inDNS pki.Certificate,
	inDNSKey pki.PrivateKey, outInstanceCertificates *corev1.Secret,
) error {
	initialize.Map(&outInstanceCertificates.Data)

	var err error
	outInstanceCertificates.Data[certAuthorityFileKey], err = certFile(inRoot)

	if err == nil {
		outInstanceCertificates.Data[certServerFileKey], err = certFile(inDNSKey, inDNS)
	}

	return err
}

// InstancePod populates a PodTemplateSpec with the fields needed to run Patroni.
// The database container must already be in the template.
func InstancePod(ctx context.Context,
	inCluster *v1beta1.PostgresCluster,
	inClusterConfigMap *corev1.ConfigMap,
	inClusterPodService *corev1.Service,
	inPatroniLeaderService *corev1.Service,
	inInstanceSpec *v1beta1.PostgresInstanceSetSpec,
	inInstanceCertificates *corev1.Secret,
	inInstanceConfigMap *corev1.ConfigMap,
	outInstancePod *corev1.PodTemplateSpec,
) error {
	initialize.Labels(outInstancePod)

	// When using Kubernetes for DCS, Patroni discovers members by listing Pods
	// that have the "scope" label. See the "kubernetes.scope_label" and
	// "kubernetes.labels" settings.
	outInstancePod.Labels[naming.LabelPatroni] = naming.PatroniScope(inCluster)

	var container *corev1.Container
	for i := range outInstancePod.Spec.Containers {
		if outInstancePod.Spec.Containers[i].Name == naming.ContainerDatabase {
			container = &outInstancePod.Spec.Containers[i]
		}
	}

	container.Command = []string{"patroni", configDirectory}
	// K8SPG-708 introduces a new entrypoint script in the percona-docker repository.
	if inCluster.CompareVersion("2.7.0") >= 0 {
		container.Command = []string{"/opt/crunchy/bin/postgres-entrypoint.sh", "patroni", configDirectory}
	}

	container.Env = append(container.Env,
		instanceEnvironment(inCluster, inClusterPodService, inPatroniLeaderService,
			outInstancePod.Spec.Containers)...)

	volume := corev1.Volume{Name: "patroni-config"}
	volume.Projected = new(corev1.ProjectedVolumeSource)

	// Add our projections after those specified in the CR. Items later in the
	// list take precedence over earlier items (that is, last write wins).
	// - https://kubernetes.io/docs/concepts/storage/volumes/#projected
	volume.Projected.Sources = append(append(volume.Projected.Sources,
		instanceConfigFiles(inClusterConfigMap, inInstanceConfigMap)...),
		instanceCertificates(inInstanceCertificates)...)

	outInstancePod.Spec.Volumes = append(outInstancePod.Spec.Volumes, volume)

	container.VolumeMounts = append(container.VolumeMounts, corev1.VolumeMount{
		Name:      volume.Name,
		MountPath: configDirectory,
		ReadOnly:  true,
	})

	instanceProbes(inCluster, container)

	// K8SPG-708
	if inCluster.CompareVersion("2.7.0") >= 0 {
		outInstancePod.Spec.InitContainers = []corev1.Container{
			k8s.InitContainer(
				naming.ContainerDatabase,
				inCluster.Spec.InitImage,
				inCluster.Spec.ImagePullPolicy,
				initialize.RestrictedSecurityContext(true),
				container.Resources,
			),
		}

		outInstancePod.Spec.Volumes = append(outInstancePod.Spec.Volumes, []corev1.Volume{
			{
				Name: pNaming.CrunchyBinVolumeName,
				VolumeSource: corev1.VolumeSource{
					EmptyDir: &corev1.EmptyDirVolumeSource{},
				},
			},
		}...)

		container.VolumeMounts = append(container.VolumeMounts, corev1.VolumeMount{
			Name:      pNaming.CrunchyBinVolumeName,
			MountPath: pNaming.CrunchyBinVolumePath,
		})
	}

	return nil
}

// instanceProbes adds Patroni liveness and readiness probes to container.
func instanceProbes(cluster *v1beta1.PostgresCluster, container *corev1.Container) {

	// Patroni uses a watchdog to ensure that PostgreSQL does not accept commits
	// after the leader lock expires, even if Patroni becomes unresponsive.
	// - https://github.com/zalando/patroni/blob/v2.0.1/docs/watchdog.rst
	//
	// Similar functionality is provided by a liveness probe. When the probe
	// finally fails, kubelet will send a SIGTERM to the Patroni process.
	// If the process does not stop, kubelet will send a SIGKILL after the pod's
	// TerminationGracePeriodSeconds.
	// - https://docs.k8s.io/concepts/workloads/pods/pod-lifecycle/
	//
	// TODO(cbandy): Consider TerminationGracePeriodSeconds' impact here.
	// TODO(cbandy): Consider if a PreStop hook is necessary.
	container.LivenessProbe = probeTiming(cluster.Spec.Patroni)
	container.LivenessProbe.InitialDelaySeconds = 3
	// Create the probe handler through a constructor for the liveness probe.
	// Introduced with K8SPG-708.
	container.LivenessProbe.ProbeHandler = livenessProbe(cluster)

	// Readiness is reflected in the controlling object's status (e.g. ReadyReplicas)
	// and allows our controller to react when Patroni bootstrap completes.
	//
	// When using Endpoints for DCS, this probe does not affect the availability
	// of the leader Pod in the leader Service.
	container.ReadinessProbe = probeTiming(cluster.Spec.Patroni)
	container.ReadinessProbe.InitialDelaySeconds = 3
	// Create the probe handler through a constructor for the readiness probe.
	// Introduced with K8SPG-708.
	container.ReadinessProbe.ProbeHandler = readinessProbe(cluster)
}

// livenessProbe is a custom constructor for the liveness probe.
// This allows for more sophisticated logic to determine whether
// the database container is considered "alive" beyond basic checks.
// Introduced with K8SPG-708.
func livenessProbe(cluster *v1beta1.PostgresCluster) corev1.ProbeHandler {
	if cluster.CompareVersion("2.7.0") >= 0 {
		return corev1.ProbeHandler{
			Exec: &corev1.ExecAction{
				Command: []string{"bash", "-c", "/opt/crunchy/bin/postgres-liveness-check.sh"},
			},
		}
	}
	return corev1.ProbeHandler{
		HTTPGet: &corev1.HTTPGetAction{
			Path:   "/liveness",
			Port:   intstr.FromInt(int(*cluster.Spec.Patroni.Port)),
			Scheme: corev1.URISchemeHTTPS,
		},
	}
}

// readinessProbe is a custom constructor for the liveness probe.
// This allows for more sophisticated logic to determine whether
// the database container is considered "alive" beyond basic checks.
// Introduced with K8SPG-708.
func readinessProbe(cluster *v1beta1.PostgresCluster) corev1.ProbeHandler {
	if cluster.CompareVersion("2.7.0") >= 0 {
		return corev1.ProbeHandler{
			Exec: &corev1.ExecAction{
				Command: []string{"bash", "-c", "/opt/crunchy/bin/postgres-readiness-check.sh"},
			},
		}
	}
	return corev1.ProbeHandler{
		HTTPGet: &corev1.HTTPGetAction{
			Path:   "/readiness",
			Port:   intstr.FromInt(int(*cluster.Spec.Patroni.Port)),
			Scheme: corev1.URISchemeHTTPS,
		},
	}
}

// PodIsPrimary returns whether or not pod is currently acting as the leader with
// the "master" role. This role will be called "primary" in the future, see:
// - https://github.com/zalando/patroni/blob/master/docs/releases.rst?plain=1#L213
func PodIsPrimary(pod metav1.Object) bool {
	if pod == nil {
		return false
	}

	// TODO(cbandy): This works only when using Kubernetes for DCS.

	// - https://github.com/zalando/patroni/blob/v3.1.1/patroni/ha.py#L296
	// - https://github.com/zalando/patroni/blob/v3.1.1/patroni/ha.py#L583
	// - https://github.com/zalando/patroni/blob/v3.1.1/patroni/ha.py#L782
	// - https://github.com/zalando/patroni/blob/v3.1.1/patroni/ha.py#L1574
	status := pod.GetAnnotations()["status"]
	// K8SPG-648: patroni v4.0.0 deprecated "master" role.
	//            We should use "primary" instead
	return strings.Contains(status, `"role":"master"`) || strings.Contains(status, `"role":"primary"`)
}

// PodIsStandbyLeader returns whether or not pod is currently acting as a "standby_leader".
func PodIsStandbyLeader(pod metav1.Object) bool {
	if pod == nil {
		return false
	}

	// TODO(cbandy): This works only when using Kubernetes for DCS.

	// - https://github.com/zalando/patroni/blob/v2.0.2/patroni/ha.py#L190
	// - https://github.com/zalando/patroni/blob/v2.0.2/patroni/ha.py#L294
	// - https://github.com/zalando/patroni/blob/v2.0.2/patroni/ha.py#L353
	status := pod.GetAnnotations()["status"]
	return strings.Contains(status, `"role":"standby_leader"`)
}

// PodRequiresRestart returns whether or not PostgreSQL inside pod has (pending)
// parameter changes that require a PostgreSQL restart.
func PodRequiresRestart(pod metav1.Object) bool {
	if pod == nil {
		return false
	}

	// TODO(cbandy): This works only when using Kubernetes for DCS.

	// - https://github.com/zalando/patroni/blob/v2.1.1/patroni/ha.py#L198
	// - https://github.com/zalando/patroni/blob/v2.1.1/patroni/postgresql/config.py#L977
	// - https://github.com/zalando/patroni/blob/v2.1.1/patroni/postgresql/config.py#L1007
	status := pod.GetAnnotations()["status"]
	return strings.Contains(status, `"pending_restart":true`)
}
