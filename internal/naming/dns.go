// Copyright 2021 - 2024 Crunchy Data Solutions, Inc.
//
// SPDX-License-Identifier: Apache-2.0

package naming

import (
	"context"
	"net"
	"strings"

	"github.com/pkg/errors"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
)

// InstancePodDNSNames returns the possible DNS names for instance. The first
// name is the fully qualified domain name (FQDN).
func InstancePodDNSNames(ctx context.Context, instance *appsv1.StatefulSet, dnsSuffix string) []string {
	var (
		domain    = KubernetesClusterDomain(ctx, dnsSuffix)
		namespace = instance.Namespace
		name      = instance.Name + "-0." + instance.Spec.ServiceName
	)

	// We configure our instances with a subdomain so that Pods get stable DNS
	// names in the form "{pod}.{service}.{namespace}.svc.{cluster-domain}".
	// - https://docs.k8s.io/concepts/services-networking/dns-pod-service/#pods
	return []string{
		name + "." + namespace + ".svc." + domain,
		name + "." + namespace + ".svc",
		name + "." + namespace,
		name,
	}
}

// RepoHostPodDNSNames returns the possible DNS names for a pgBackRest repository host Pod.
// The first name is the fully qualified domain name (FQDN).
func RepoHostPodDNSNames(ctx context.Context, repoHost *appsv1.StatefulSet, dnsSuffix string) ([]string, error) {
	if repoHost.Namespace == "" {
		return nil, errors.New("repoHost.Namespace is empty")
	}
	if repoHost.Name == "" {
		return nil, errors.New("repoHost.Name is empty")
	}
	if repoHost.Spec.ServiceName == "" {
		return nil, errors.New("repoHost.Spec.ServiceName is empty")
	}

	var (
		domain    = KubernetesClusterDomain(ctx, dnsSuffix)
		namespace = repoHost.Namespace
		name      = repoHost.Name + "-0." + repoHost.Spec.ServiceName
	)

	// We configure our repository hosts with a subdomain so that Pods get stable
	// DNS names in the form "{pod}.{service}.{namespace}.svc.{cluster-domain}".
	// - https://docs.k8s.io/concepts/services-networking/dns-pod-service/#pods
	return []string{
		name + "." + namespace + ".svc." + domain,
		name + "." + namespace + ".svc",
		name + "." + namespace,
		name,
	}, nil
}

// ServiceDNSNames returns the possible DNS names for service. The first name
// is the fully qualified domain name (FQDN).
func ServiceDNSNames(ctx context.Context, service *corev1.Service, dnsSuffix string) ([]string, error) {
	if service.Name == "" {
		return nil, errors.New("service.Name is empty")
	}

	if service.Namespace == "" {
		return nil, errors.New("service.Namespace is empty")
	}

	domain := KubernetesClusterDomain(ctx, dnsSuffix)

	return []string{
		service.Name + "." + service.Namespace + ".svc." + domain,
		service.Name + "." + service.Namespace + ".svc",
		service.Name + "." + service.Namespace,
		service.Name,
	}, nil
}

// KubernetesClusterDomain looks up the Kubernetes cluster domain name.
// K8SPG-694: If the override parameter is provided, it is returned without performing any operations
func KubernetesClusterDomain(ctx context.Context, override string) string {
	// K8SPG-694
	if override != "" {
		return override
	}

	ctx, span := tracer.Start(ctx, "kubernetes-domain-lookup")
	defer span.End()

	// Lookup an existing Service to determine its fully qualified domain name.
	// This is inexpensive because the "net" package uses OS-level DNS caching.
	// - https://golang.org/issue/24796
	api := "kubernetes.default.svc"
	cname, err := net.DefaultResolver.LookupCNAME(ctx, api)

	if err == nil {
		// The cname returned from the LookupCNAME can be `kubernetes.default.svc.cluster.local.`
		// Since go stdlib validates and rejects DNS with the dot suffix, the operator has to trim it.
		return strings.TrimSuffix(strings.TrimPrefix(cname, api+"."), ".")
	}

	span.RecordError(err)
	// The kubeadm default is "cluster.local" and is adequate when not running
	// in an actual Kubernetes cluster.
	return "cluster.local"
}
