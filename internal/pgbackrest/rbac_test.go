// Copyright 2021 - 2024 Crunchy Data Solutions, Inc.
//
// SPDX-License-Identifier: Apache-2.0

package pgbackrest

import (
	"context"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/percona/percona-postgresql-operator/v2/internal/testing/cmp"
	"github.com/percona/percona-postgresql-operator/v2/pkg/apis/postgres-operator.crunchydata.com/v1beta1"
)

func isUniqueAndSorted(slice []string) bool {
	if len(slice) > 1 {
		previous := slice[0]
		for _, next := range slice[1:] {
			if next <= previous {
				return false
			}
			previous = next
		}
	}
	return true
}

func TestPermissions(t *testing.T) {
	cluster := new(v1beta1.PostgresCluster)
	err := cluster.Default(context.Background(), nil)
	assert.NilError(t, err)

	permissions := Permissions(cluster)
	for _, rule := range permissions {
		assert.Assert(t, isUniqueAndSorted(rule.APIGroups), "got %q", rule.APIGroups)
		assert.Assert(t, isUniqueAndSorted(rule.Resources), "got %q", rule.Resources)
		assert.Assert(t, isUniqueAndSorted(rule.Verbs), "got %q", rule.Verbs)
	}

	assert.Assert(t, cmp.MarshalMatches(permissions, `
- apiGroups:
  - ""
  resources:
  - pods
  verbs:
  - list
- apiGroups:
  - ""
  resources:
  - pods/exec
  verbs:
  - create
	`))
}
