// Copyright 2021 - 2024 Crunchy Data Solutions, Inc.
//
// SPDX-License-Identifier: Apache-2.0

package patroni

import (
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"

	"github.com/percona/percona-postgresql-operator/v3/pkg/apis/upstream.pgv2.percona.com/v1beta1"
)

// "list", "patch", and "watch" are required. Include "get" for good measure.
// +kubebuilder:rbac:namespace=patroni,groups="",resources="pods",verbs={get}
// +kubebuilder:rbac:namespace=patroni,groups="",resources="pods",verbs={list,watch}
// +kubebuilder:rbac:namespace=patroni,groups="",resources="pods",verbs={patch}

// Permissions returns the RBAC rules Patroni needs for cluster, regardless
// of DCS backend. See internal/patroni/dcs for backend-specific rules.
func Permissions(*v1beta1.PostgresCluster) []rbacv1.PolicyRule {
	return []rbacv1.PolicyRule{{
		APIGroups: []string{corev1.SchemeGroupVersion.Group},
		Resources: []string{"pods"},
		Verbs:     []string{"get", "list", "patch", "watch"},
	}}
}
