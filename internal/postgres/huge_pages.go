// Copyright 2021 - 2024 Crunchy Data Solutions, Inc.
//
// SPDX-License-Identifier: Apache-2.0

package postgres

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/percona/percona-postgresql-operator/pkg/apis/postgres-operator.crunchydata.com/v1beta1"
)

// This function looks for a valid huge_pages resource request. If it finds one,
// it sets the PostgreSQL parameter "huge_pages" to "try". If it doesn't find
// one, it sets "huge_pages" to "off".
func SetHugePages(cluster *v1beta1.PostgresCluster, pgParameters *Parameters) {
	if HugePagesRequested(cluster) {
		pgParameters.Default.Add("huge_pages", "try")
	} else {
		pgParameters.Default.Add("huge_pages", "off")
	}
}

// This helper function checks to see if a hugepages-2Mi value greater than zero has
// been set in any of the PostgresCluster's instances' resource specs
func HugePages2MiRequested(cluster *v1beta1.PostgresCluster) bool {
	for _, instance := range cluster.Spec.InstanceSets {
		for resourceName := range instance.Resources.Limits {
			if resourceName == corev1.ResourceHugePagesPrefix+"2Mi" {
				resourceQuantity := instance.Resources.Limits.Name(resourceName, resource.BinarySI)

				if resourceQuantity != nil && resourceQuantity.Value() > 0 {
					return true
				}
			}
		}
	}

	return false
}

// This helper function checks to see if a hugepages-1Gi value greater than zero has
// been set in any of the PostgresCluster's instances' resource specs
func HugePages1GiRequested(cluster *v1beta1.PostgresCluster) bool {
	for _, instance := range cluster.Spec.InstanceSets {
		for resourceName := range instance.Resources.Limits {
			if resourceName == corev1.ResourceHugePagesPrefix+"1Gi" {
				resourceQuantity := instance.Resources.Limits.Name(resourceName, resource.BinarySI)

				if resourceQuantity != nil && resourceQuantity.Value() > 0 {
					return true
				}
			}
		}
	}

	return false
}

// This helper function checks to see if a huge_pages value greater than zero has
// been set in any of the PostgresCluster's instances' resource specs
func HugePagesRequested(cluster *v1beta1.PostgresCluster) bool {
	return HugePages2MiRequested(cluster) || HugePages1GiRequested(cluster)
}
