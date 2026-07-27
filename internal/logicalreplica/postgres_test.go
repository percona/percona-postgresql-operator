// Copyright 2021 - 2026 Crunchy Data Solutions, Inc.
//
// SPDX-License-Identifier: Apache-2.0

package logicalreplica

import (
	"strings"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/percona/percona-postgresql-operator/v2/internal/postgres"
	"github.com/percona/percona-postgresql-operator/v2/pkg/apis/upstream.pgv2.percona.com/v1beta1"
)

func clusterWithReplicas(names ...string) *v1beta1.PostgresCluster {
	cluster := new(v1beta1.PostgresCluster)
	for _, name := range names {
		cluster.Spec.LogicalReplicas = append(cluster.Spec.LogicalReplicas,
			v1beta1.LogicalReplicaSpec{Name: name})
	}
	return cluster
}

func TestEnabled(t *testing.T) {
	assert.Assert(t, !Enabled(nil))
	assert.Assert(t, !Enabled(new(v1beta1.PostgresCluster)))
	assert.Assert(t, Enabled(clusterWithReplicas("analytics")))
}

func TestPostgreSQLHBAs(t *testing.T) {
	t.Run("no rules without logical replicas", func(t *testing.T) {
		hbas := postgres.NewHBAs()
		before := len(hbas.Mandatory)

		PostgreSQLHBAs(new(v1beta1.PostgresCluster), &hbas)

		assert.Equal(t, len(hbas.Mandatory), before)
	})

	t.Run("rules are mandatory so custom pg_hba cannot drop them", func(t *testing.T) {
		hbas := postgres.NewHBAs()
		defaults := len(hbas.Default)

		PostgreSQLHBAs(clusterWithReplicas("analytics"), &hbas)

		rendered := make([]string, 0, len(hbas.Mandatory))
		for _, hba := range hbas.Mandatory {
			rendered = append(rendered, hba.String())
		}
		all := strings.Join(rendered, "\n")

		// A replication connection for pg_basebackup, a regular one for
		// pg_createsubscriber, and a reject for anything not using TLS.
		assert.Assert(t, strings.Contains(all, `hostssl replication "logicalrepl" all scram-sha-256`), all)
		assert.Assert(t, strings.Contains(all, `hostssl all "logicalrepl" all scram-sha-256`), all)
		assert.Assert(t, strings.Contains(all, `host all "logicalrepl" all reject`), all)

		// Nothing was added to Default, which users can displace.
		assert.Equal(t, len(hbas.Default), defaults)
	})
}

func TestIgnoreSlotsMatchers(t *testing.T) {
	assert.Assert(t, IgnoreSlotsMatchers(new(v1beta1.PostgresCluster)) == nil)

	matchers := IgnoreSlotsMatchers(clusterWithReplicas("analytics", "reporting"))

	// The matcher must not depend on the replica set or on database names:
	// those are only known after the operator queries the primary, which would
	// leave a window where a slot exists but Patroni does not know to skip it.
	assert.Equal(t, len(matchers), 1)
	assert.Equal(t, matchers[0]["type"], "logical")
	assert.Equal(t, matchers[0]["plugin"], OutputPlugin)
	_, hasName := matchers[0]["name"]
	assert.Assert(t, !hasName)
}

func TestIdentifierNames(t *testing.T) {
	t.Run("distinct kinds do not collide", func(t *testing.T) {
		slot := SlotName("analytics", "cluster1")
		pub := PublicationName("analytics", "cluster1")
		sub := SubscriptionName("analytics", "cluster1")

		assert.Assert(t, slot != pub)
		assert.Assert(t, slot != sub)
		assert.Assert(t, pub != sub)
	})

	t.Run("deterministic", func(t *testing.T) {
		assert.Equal(t, SlotName("analytics", "cluster1"), SlotName("analytics", "cluster1"))
	})

	t.Run("unsafe characters are folded", func(t *testing.T) {
		name := SlotName("analytics", `we-ird."DB`)
		for _, r := range name {
			ok := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_'
			assert.Assert(t, ok, "unexpected character %q in %q", r, name)
		}
	})

	t.Run("bounded by NAMEDATALEN", func(t *testing.T) {
		name := SlotName(strings.Repeat("r", 40), strings.Repeat("d", 63))
		assert.Assert(t, len(name) <= maxIdentifierLength, "%d: %q", len(name), name)
	})

	t.Run("names that sanitise alike stay distinct", func(t *testing.T) {
		// Both fold to "a_b", so only the hash keeps them apart.
		assert.Assert(t, SlotName("r", "a.b") != SlotName("r", "a-b"))
	})

	t.Run("names that truncate alike stay distinct", func(t *testing.T) {
		long := strings.Repeat("d", 80)
		assert.Assert(t, SlotName("r", long+"one") != SlotName("r", long+"two"))
	})
}
