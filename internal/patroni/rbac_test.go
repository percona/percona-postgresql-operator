// Copyright 2021 - 2024 Crunchy Data Solutions, Inc.
//
// SPDX-License-Identifier: Apache-2.0

package patroni

import (
	"context"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/percona/percona-postgresql-operator/v3/internal/testing/cmp"
	"github.com/percona/percona-postgresql-operator/v3/pkg/apis/upstream.pgv2.percona.com/v1beta1"
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

// TestPermissions covers the generic RBAC rules Patroni needs regardless of
// DCS backend. See internal/patroni/dcs for backend-specific rules.
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
  - get
  - list
  - patch
  - watch
	`))
}
