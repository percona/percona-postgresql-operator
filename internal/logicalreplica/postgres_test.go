// Copyright 2021 - 2026 Crunchy Data Solutions, Inc.
//
// SPDX-License-Identifier: Apache-2.0

package logicalreplica

import (
	"strings"
	"testing"

	pg_query "github.com/pganalyze/pg_query_go/v6"
	"gotest.tools/v3/assert"

	"github.com/percona/percona-postgresql-operator/v3/internal/postgres"
	"github.com/percona/percona-postgresql-operator/v3/pkg/apis/upstream.pgv2.percona.com/v1beta1"
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

		// A replication connection for the replica itself and for pg_basebackup
		// when it is the bootstrap method, a regular one for
		// pg_createsubscriber, and a reject for anything not using TLS.
		assert.Assert(t, strings.Contains(all, `hostssl replication "logicalrepl" all scram-sha-256`), all)
		assert.Assert(t, strings.Contains(all, `hostssl all "logicalrepl" all scram-sha-256`), all)
		assert.Assert(t, strings.Contains(all, `host all "logicalrepl" all reject`), all)

		// Nothing was added to Default, which users can displace.
		assert.Equal(t, len(hbas.Default), defaults)
	})
}

func TestPrimaryReadinessQuery(t *testing.T) {
	query := PrimaryReadinessQuery()

	// The query runs on a live primary, where a syntax error would surface as an
	// unreachable primary rather than as anything obviously wrong. Parse it with
	// PostgreSQL's own grammar instead.
	tree, err := pg_query.Parse(query)
	assert.NilError(t, err)
	assert.Equal(t, len(tree.GetStmts()), 1, query)

	// Every reason the parser can report has to be one the caller knows how to
	// render, and each has to be reachable.
	for _, reason := range []string{
		reasonPrimaryInRecovery, reasonWALLevelNotLogical, ReasonRestartPending,
		reasonReplicationRoleNotReady, reasonReplicationHBAMissing,
	} {
		assert.Equal(t, strings.Count(query, postgres.QuoteLiteral(reason)), 1, reason)
		assert.Assert(t, PrimaryReadinessMessage(reason) != reason, reason)
	}

	assert.Assert(t, strings.Contains(query, postgres.QuoteLiteral(ReplicationUser)), query)
}

// TestPrimaryReadinessQueryMatchesHBAs pins the query to the rules it looks for.
// Nothing at runtime would report a mismatch: a query that cannot find the rules
// leaves the condition False forever, and the bootstrap simply never starts.
func TestPrimaryReadinessQueryMatchesHBAs(t *testing.T) {
	query := PrimaryReadinessQuery()

	// Both of the rules PostgreSQLHBAs adds for the replication user, and only
	// those: the "reject" rule alongside them must not satisfy the check.
	assert.Assert(t, strings.Contains(query, postgres.QuoteLiteral(hbaOrigin)), query)
	assert.Assert(t, strings.Contains(query, postgres.QuoteLiteral(hbaAuthMethod)), query)
	assert.Assert(t, strings.Contains(query, `'replication' = ANY (r.database)`), query)
	assert.Assert(t, strings.Contains(query, `'all' = ANY (r.database)`), query)

	rendered := func(tlsOnly bool) string {
		hbas := postgres.NewHBAs()
		PostgreSQLHBAs(clusterWithReplicas("analytics"), &hbas)

		lines := make([]string, 0, len(hbas.Mandatory))
		for _, hba := range hbas.Mandatory {
			if tlsOnly {
				hba = hba.TLSOnly()
			}
			lines = append(lines, hba.String())
		}
		return strings.Join(lines, "\n")
	}

	for _, tlsOnly := range []bool{false, true} {
		all := rendered(tlsOnly)

		// pg_hba_file_rules strips the quoting around the user name, and reports
		// the keywords as written.
		assert.Assert(t, strings.Contains(all,
			hbaOrigin+` replication "`+ReplicationUser+`" all `+hbaAuthMethod), all)
		assert.Assert(t, strings.Contains(all,
			hbaOrigin+` all "`+ReplicationUser+`" all `+hbaAuthMethod), all)
	}

	// Under spec.tlsOnly the reject rule becomes a "hostssl" record too, which is
	// why the query has to match on the method and not just the type.
	assert.Assert(t, strings.Contains(rendered(true),
		hbaOrigin+` all "`+ReplicationUser+`" all reject`), rendered(true))
}

func TestParsePrimaryReadinessReasons(t *testing.T) {
	// An empty result is the whole point: it means the primary is ready.
	assert.Assert(t, ParsePrimaryReadinessReasons("") == nil)
	assert.Assert(t, ParsePrimaryReadinessReasons("  \n ") == nil)

	assert.DeepEqual(t, ParsePrimaryReadinessReasons(" RestartPending \n"),
		[]string{"RestartPending"})

	// psql -t pads its output; the order is the query's, and the caller reports
	// the first as the condition reason.
	assert.DeepEqual(t, ParsePrimaryReadinessReasons(" RestartPending,ReplicationHBAMissing \n"),
		[]string{"RestartPending", "ReplicationHBAMissing"})

	// Unknown reasons pass through rather than being dropped, so a newer operator
	// reading an older reason still says something.
	assert.Equal(t, PrimaryReadinessMessage("Whatever"), "Whatever")
}

func TestIgnoreSlotsMatchers(t *testing.T) {
	assert.Assert(t, IgnoreSlotsMatchers(new(v1beta1.PostgresCluster)) == nil)

	matchers := IgnoreSlotsMatchers(clusterWithReplicas("analytics", "reporting"))

	// The matcher must not depend on the replica set or on database names:
	// those are only known after the operator queries the primary, which would
	// leave a window where a slot exists but Patroni does not know to skip it.
	assert.Equal(t, len(matchers), 1)
	matcher, ok := matchers[0].(map[string]any)
	assert.Assert(t, ok)
	assert.Equal(t, matcher["type"], "logical")
	assert.Equal(t, matcher["plugin"], outputPlugin)
	_, hasName := matcher["name"]
	assert.Assert(t, !hasName)
}

func TestDisableOnErrorSQL(t *testing.T) {
	sql := DisableOnErrorSQL("analytics", `we-ird."DB`)

	// The bootstrap Job hands this to psql, where a syntax error would surface as
	// a failed bootstrap and a data volume that has to be seeded again. Parse it
	// with PostgreSQL's own grammar instead.
	tree, err := pg_query.Parse(sql)
	assert.NilError(t, err)
	assert.Equal(t, len(tree.GetStmts()), 1, sql)

	alter := tree.GetStmts()[0].GetStmt().GetAlterSubscriptionStmt()
	assert.Assert(t, alter != nil, sql)
	assert.Equal(t, alter.GetSubname(), SubscriptionName("analytics", `we-ird."DB`))

	// Only the one option, and nothing that would touch the connection: the
	// target reaches the publisher again the moment the replica starts, and a
	// REFRESH here would try it from the Job.
	assert.Equal(t, len(alter.GetOptions()), 1, sql)
	assert.Equal(t, alter.GetOptions()[0].GetDefElem().GetDefname(), "disable_on_error")

	// The statement is passed to psql as one single-quoted shell word.
	assert.Assert(t, !strings.Contains(sql, "'"), sql)
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
