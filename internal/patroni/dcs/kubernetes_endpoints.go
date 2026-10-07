// Copyright 2021 - 2024 Crunchy Data Solutions, Inc.
//
// SPDX-License-Identifier: Apache-2.0

package dcs

import (
	"context"
	"time"

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	"github.com/percona/percona-postgresql-operator/v3/internal/initialize"
	"github.com/percona/percona-postgresql-operator/v3/internal/logging"
	"github.com/percona/percona-postgresql-operator/v3/internal/naming"
	"github.com/percona/percona-postgresql-operator/v3/pkg/apis/upstream.pgv2.percona.com/v1beta1"
)

// kubernetesEndpointsBackend uses Kubernetes Endpoints as Patroni's DCS.
// This is distinct from a future ConfigMaps-based Kubernetes backend, which
// Patroni also supports.
type kubernetesEndpointsBackend struct{}

func (kubernetesEndpointsBackend) ClusterYAML(cluster *v1beta1.PostgresCluster) map[string]any {
	labels := map[string]string{naming.LabelCluster: cluster.Name}
	if cluster.CompareVersion("2.9.0") >= 0 {
		labels = naming.Merge(cluster.Spec.Metadata.GetLabelsOrNil(), labels)
	}

	return map[string]any{
		// Use Kubernetes Endpoints for the distributed configuration store (DCS).
		// These values cannot change during the cluster's lifetime.
		//
		// NOTE(cbandy): It *might* be possible to *carefully* change the role and
		// scope labels, but there is no way to reconfigure all instances at once.
		"kubernetes": map[string]any{
			"namespace":     cluster.Namespace,
			"role_label":    naming.LabelRole,
			"scope_label":   naming.LabelPatroni,
			"use_endpoints": true,

			// In addition to "scope_label" above, Patroni will add the following to
			// every object it creates. It will also use these as filters when doing
			// any lookups.
			"labels": labels,
		},
	}
}

func (kubernetesEndpointsBackend) InstanceYAML(*v1beta1.PostgresCluster) map[string]any {
	return nil
}

func (kubernetesEndpointsBackend) InstanceEnvVars(
	_ *v1beta1.PostgresCluster, leaderService *corev1.Service, podContainers []corev1.Container,
) []corev1.EnvVar {
	// "kubernetes.pod_ip" and "kubernetes.ports" cannot be known until the
	// instance Pod is created, so they aren't set in InstanceYAML. Instead
	// they're injected using the downward API via the
	// PATRONI_KUBERNETES_POD_IP and PATRONI_KUBERNETES_PORTS env vars below.
	// Gather Endpoint ports for any Container ports that match the leader
	// Service definition.
	ports := []corev1.EndpointPort{}
	for _, sp := range leaderService.Spec.Ports {
		for i := range podContainers {
			for _, cp := range podContainers[i].Ports {
				if sp.TargetPort.StrVal == cp.Name {
					ports = append(ports, corev1.EndpointPort{
						Name:     sp.Name,
						Port:     cp.ContainerPort,
						Protocol: cp.Protocol,
					})
				}
			}
		}
	}
	portsYAML, _ := yaml.Marshal(ports)

	return []corev1.EnvVar{
		// Set "kubernetes.pod_ip" to the v1.Pod's primary IP address.
		// Patroni must be restarted when changing this value.
		{
			Name: "PATRONI_KUBERNETES_POD_IP",
			ValueFrom: &corev1.EnvVarSource{FieldRef: &corev1.ObjectFieldSelector{
				APIVersion: "v1",
				FieldPath:  "status.podIP",
			}},
		},

		// When using Endpoints for DCS, Patroni needs to replicate the leader
		// ServicePort definitions. Set "kubernetes.ports" to the YAML of this
		// Pod's equivalent EndpointPort definitions.
		//
		// This is connascent with PATRONI_POSTGRESQL_CONNECT_ADDRESS.
		// Patroni must be restarted when changing this value.
		{
			Name:  "PATRONI_KUBERNETES_PORTS",
			Value: string(portsYAML),
		},
	}
}

// When using Endpoints for DCS, "create", "list", "patch", and "watch" are
// required. Include "get" for good measure. The `patronictl scaffold` and
// `patronictl remove` commands require "deletecollection".
// +kubebuilder:rbac:namespace=patroni,groups="",resources="endpoints",verbs={get}
// +kubebuilder:rbac:namespace=patroni,groups="",resources="endpoints",verbs={create,deletecollection}
// +kubebuilder:rbac:namespace=patroni,groups="",resources="endpoints",verbs={list,watch}
// +kubebuilder:rbac:namespace=patroni,groups="",resources="endpoints",verbs={patch}
// +kubebuilder:rbac:namespace=patroni,groups="",resources="services",verbs={create}

// The OpenShift RestrictedEndpointsAdmission plugin requires special
// authorization to create Endpoints that contain Pod IPs.
// - https://github.com/openshift/origin/pull/9383
// +kubebuilder:rbac:namespace=patroni,groups="",resources="endpoints/restricted",verbs={create}

func (kubernetesEndpointsBackend) Permissions(cluster *v1beta1.PostgresCluster) []rbacv1.PolicyRule {
	rules := make([]rbacv1.PolicyRule, 0, 3)

	rules = append(rules, rbacv1.PolicyRule{
		APIGroups: []string{corev1.SchemeGroupVersion.Group},
		Resources: []string{"endpoints"},
		Verbs:     []string{"create", "deletecollection", "get", "list", "patch", "watch"},
	})

	if cluster.Spec.OpenShift != nil && *cluster.Spec.OpenShift {
		rules = append(rules, rbacv1.PolicyRule{
			APIGroups: []string{corev1.SchemeGroupVersion.Group},
			Resources: []string{"endpoints/restricted"},
			Verbs:     []string{"create"},
		})
	}

	// When using Endpoints for DCS, Patroni tries to create the "{scope}-config" service.
	// NOTE(cbandy): The PostgresCluster controller already creates this Service;
	// it might be possible to eliminate this permission if it also created the
	// Endpoints.
	rules = append(rules, rbacv1.PolicyRule{
		APIGroups: []string{corev1.SchemeGroupVersion.Group},
		Resources: []string{"services"},
		Verbs:     []string{"create"},
	})

	return rules
}

func (kubernetesEndpointsBackend) DistributedConfigurationService(cluster *v1beta1.PostgresCluster) *corev1.Service {
	// When using Endpoints for DCS, Patroni needs a Service to ensure that the
	// Endpoints object is not removed by Kubernetes at startup. Patroni will
	// create this object if it has permission to do so, but it won't set any
	// ownership.
	// - https://releases.k8s.io/v1.16.0/pkg/controller/endpoint/endpoints_controller.go#L547
	// - https://releases.k8s.io/v1.20.0/pkg/controller/endpoint/endpoints_controller.go#L580
	// - https://github.com/zalando/patroni/blob/v2.0.1/patroni/dcs/kubernetes.py#L865-L881
	service := &corev1.Service{ObjectMeta: naming.PatroniDistributedConfiguration(cluster)}
	service.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("Service"))

	// Allocate no IP address (headless) and create no Endpoints.
	// - https://docs.k8s.io/concepts/services-networking/service/#headless-services
	service.Spec.ClusterIP = corev1.ClusterIPNone
	service.Spec.Selector = nil

	return service
}

func (kubernetesEndpointsBackend) LeaderLeaseService(
	cluster *v1beta1.PostgresCluster, recorder record.EventRecorder,
) (*corev1.Service, error) {
	service := &corev1.Service{ObjectMeta: naming.PatroniLeaderEndpoints(cluster)}
	service.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("Service"))

	service.Annotations = naming.Merge(
		cluster.Spec.Metadata.GetAnnotationsOrNil(),
	)
	service.Labels = naming.Merge(
		cluster.Spec.Metadata.GetLabelsOrNil(),
	)

	if spec := cluster.Spec.Service; spec != nil {
		service.Annotations = naming.Merge(service.Annotations,
			spec.Metadata.GetAnnotationsOrNil())
		service.Labels = naming.Merge(service.Labels,
			spec.Metadata.GetLabelsOrNil())
	}

	// add our labels last so they aren't overwritten
	service.Labels = naming.Merge(service.Labels,
		naming.WithPerconaLabels(map[string]string{ // K8SPG-430
			naming.LabelCluster: cluster.Name,
			naming.LabelPatroni: naming.PatroniScope(cluster),
		}, cluster.Name, "", cluster.Labels[naming.LabelVersion]))

	// Allocate an IP address and/or node port and let Patroni manage the Endpoints.
	// Patroni will ensure that they always route to the elected leader.
	// - https://docs.k8s.io/concepts/services-networking/service/#services-without-selectors
	service.Spec.Selector = nil

	// The TargetPort must be the name (not the number) of the PostgreSQL
	// ContainerPort. This name allows the port number to differ between
	// instances, which can happen during a rolling update.
	servicePort := corev1.ServicePort{
		Name:       naming.PortPostgreSQL,
		Port:       *cluster.Spec.Port,
		Protocol:   corev1.ProtocolTCP,
		TargetPort: intstr.FromString(naming.PortPostgreSQL),
	}

	if spec := cluster.Spec.Service; spec == nil {
		service.Spec.Type = corev1.ServiceTypeClusterIP
	} else {
		service.Spec.Type = corev1.ServiceType(spec.Type)
		// K8SPG-389
		service.Spec.LoadBalancerSourceRanges = spec.LoadBalancerSourceRanges

		if spec.NodePort != nil {
			if service.Spec.Type == corev1.ServiceTypeClusterIP {
				// The NodePort can only be set when the Service type is NodePort or
				// LoadBalancer. However, due to a known issue prior to Kubernetes
				// 1.20, we clear these errors during our apply. To preserve the
				// appropriate behavior, we log an Event and return an error.
				// TODO(tjmoore4): Once Validation Rules are available, this check
				// and event could potentially be removed in favor of that validation
				recorder.Eventf(cluster, corev1.EventTypeWarning, "MisconfiguredClusterIP",
					"NodePort cannot be set with type ClusterIP on Service %q", service.Name)
				return nil, errors.Errorf("NodePort cannot be set with type ClusterIP on Service %q", service.Name)
			}
			servicePort.NodePort = *spec.NodePort
		}
		service.Spec.ExternalTrafficPolicy = initialize.FromPointer(spec.ExternalTrafficPolicy)
		service.Spec.InternalTrafficPolicy = spec.InternalTrafficPolicy
	}
	service.Spec.Ports = []corev1.ServicePort{servicePort}

	return service, nil
}

func (kubernetesEndpointsBackend) PrimaryService(
	cluster *v1beta1.PostgresCluster, leader *corev1.Service,
) (corev1.ServiceSpec, *corev1.EndpointSubset, error) {
	// We want to name and label our primary Service consistently. When Patroni is
	// using Endpoints for its DCS, however, they and any Service that uses them
	// must use the same name as the Patroni "scope" which has its own constraints.
	//
	// To stay free from those constraints, our primary Service resolves to the
	// ClusterIP of the Service created in Reconciler.reconcilePatroniLeaderLease
	// when Patroni is using Endpoints.
	if leader == nil {
		return corev1.ServiceSpec{}, nil, errors.New("Patroni leader Service is not available yet")
	}

	// Allocate no IP address (headless) and manage the Endpoints ourselves.
	// - https://docs.k8s.io/concepts/services-networking/service/#headless-services
	// - https://docs.k8s.io/concepts/services-networking/service/#services-without-selectors
	spec := corev1.ServiceSpec{
		ClusterIP: corev1.ClusterIPNone,
		Selector:  nil,
		Ports: []corev1.ServicePort{{
			Name:       naming.PortPostgreSQL,
			Port:       *cluster.Spec.Port,
			Protocol:   corev1.ProtocolTCP,
			TargetPort: intstr.FromString(naming.PortPostgreSQL),
		}},
	}

	// Resolve to the ClusterIP for which Patroni has configured the Endpoints.
	subset := &corev1.EndpointSubset{
		Addresses: []corev1.EndpointAddress{{IP: leader.Spec.ClusterIP}},
	}

	// Copy the EndpointPorts from the ServicePorts.
	for _, sp := range spec.Ports {
		subset.Ports = append(subset.Ports, corev1.EndpointPort{
			Name:     sp.Name,
			Port:     sp.Port,
			Protocol: sp.Protocol,
		})
	}

	return spec, subset, nil
}

func (kubernetesEndpointsBackend) Observe(
	ctx context.Context, cli client.Client, cluster *v1beta1.PostgresCluster, readyInstance bool,
) (Observation, error) {
	var observation Observation

	dcs := &corev1.Endpoints{ObjectMeta: naming.PatroniDistributedConfiguration(cluster)}
	err := errors.WithStack(client.IgnoreNotFound(
		cli.Get(ctx, client.ObjectKeyFromObject(dcs), dcs),
	))

	if err == nil {
		if dcs.Annotations["initialize"] != "" {
			// After bootstrap, Patroni writes the cluster system identifier to DCS.
			observation.SystemIdentifier = dcs.Annotations["initialize"]
		} else if readyInstance {
			// While we typically expect a value for the initialize key to be present in the
			// Endpoints above by the time the StatefulSet for any instance indicates "ready"
			// (since Patroni writes this value after successful cluster bootstrap, at which time
			// the initial primary should transition to "ready"), sometimes this is not the case
			// and the "initialize" key is not yet present.  Therefore, if a "ready" instance
			// is detected in the cluster we assume this is the case, and simply log a message and
			// requeue in order to try again until the expected value is found.
			logging.FromContext(ctx).Info("detected ready instance but no initialize value")
			observation.RequeueAfter = time.Second
		}
	}

	return observation, err
}

func (kubernetesEndpointsBackend) Delete(ctx context.Context, cli client.Client, cluster *v1beta1.PostgresCluster) error {
	// TODO(cbandy): This could also be accomplished by adopting the Endpoints
	// as Patroni creates them. Would their events cause too many reconciles?
	// Foreground deletion may force us to adopt and set finalizers anyway.
	selector, err := naming.AsSelector(naming.ClusterPatronis(cluster))
	if err == nil {
		err = errors.WithStack(
			cli.DeleteAllOf(
				ctx, &corev1.Endpoints{},
				client.InNamespace(cluster.Namespace),
				client.MatchingLabelsSelector{Selector: selector},
			),
		)
	}

	return err
}
