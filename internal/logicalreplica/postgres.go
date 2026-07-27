// Copyright 2021 - 2026 Crunchy Data Solutions, Inc.
//
// SPDX-License-Identifier: Apache-2.0

// Package logicalreplica renders the PostgreSQL-side configuration that logical
// replicas need on the source cluster. The replicas themselves are created and
// managed by the Percona layer; this package only covers what has to be baked
// into the primary's pg_hba.conf and Patroni configuration. K8SPG-784
package logicalreplica

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/percona/percona-postgresql-operator/v2/internal/postgres"
	"github.com/percona/percona-postgresql-operator/v2/pkg/apis/upstream.pgv2.percona.com/v1beta1"
)

const (
	// ReplicationUser is the reserved superuser that pg_createsubscriber
	// connects to the primary as. It must match v2.UserLogicalReplication.
	ReplicationUser = "logicalrepl"

	// OutputPlugin is the logical decoding plugin that pg_createsubscriber uses.
	OutputPlugin = "pgoutput"

	// maxIdentifierLength is PostgreSQL's NAMEDATALEN-1. Replication slot,
	// publication and subscription names are all bound by it.
	maxIdentifierLength = 63
)

// Enabled returns whether the cluster has any logical replica configured.
func Enabled(inCluster *v1beta1.PostgresCluster) bool {
	return inCluster != nil && len(inCluster.Spec.LogicalReplicas) > 0
}

// PostgreSQLHBAs provides the Postgres HBA rules that let pg_basebackup and
// pg_createsubscriber reach the primary as ReplicationUser.
func PostgreSQLHBAs(inCluster *v1beta1.PostgresCluster, outHBAs *postgres.HBAs) {
	if !Enabled(inCluster) {
		return
	}

	// These have to be Mandatory rather than Default: the default
	// "hostssl all all all scram-sha-256" rule is dropped as soon as the user
	// supplies any pg_hba rules of their own, which would otherwise silently
	// break bootstrapping of every logical replica.
	outHBAs.Mandatory = append(outHBAs.Mandatory,
		// pg_basebackup opens a replication connection, pg_createsubscriber a
		// regular one to each database being converted.
		postgres.NewHBA().TLS().Users(ReplicationUser).Method("scram-sha-256").Replication(),
		postgres.NewHBA().TLS().Users(ReplicationUser).Method("scram-sha-256"),
		// Never allow this superuser to authenticate without TLS.
		postgres.NewHBA().TCP().Users(ReplicationUser).Method("reject"))
}

// IgnoreSlotsMatchers returns the Patroni "ignore_slots" entries that keep it
// from deleting the logical replication slots backing the logical replicas.
//
// Patroni drops every slot it does not know about, and that is not gated on
// "use_slots": with use_slots disabled its set of expected slots is empty, so a
// slot left behind by pg_createsubscriber would be dropped on the next HA loop
// and replication would stop for good.
//
// The matcher deliberately carries no name. Slot names are only known after the
// operator has resolved which databases a replica covers, which happens after
// the Patroni configuration has already been rendered; matching on name would
// leave a window in which the slot exists but its matcher has not reached
// Patroni yet. Matching every logical pgoutput slot instead is stable from the
// moment a logical replica appears in the spec. It is broader than strictly
// necessary - user-created logical slots on this cluster are protected too -
// but that only applies to clusters that opted into logical replication, and it
// is the behaviour those users would want anyway.
func IgnoreSlotsMatchers(inCluster *v1beta1.PostgresCluster) []map[string]any {
	if !Enabled(inCluster) {
		return nil
	}

	return []map[string]any{{
		"type":   "logical",
		"plugin": OutputPlugin,
	}}
}

// SlotName returns the name of the replication slot on the primary that feeds
// database db of the given logical replica.
func SlotName(replica, db string) string {
	return identifier("pgo_lr_slot", replica, db)
}

// PublicationName returns the name of the publication on the primary that feeds
// database db of the given logical replica.
func PublicationName(replica, db string) string {
	return identifier("pgo_lr_pub", replica, db)
}

// SubscriptionName returns the name of the subscription created in database db
// on the given logical replica.
func SubscriptionName(replica, db string) string {
	return identifier("pgo_lr_sub", replica, db)
}

// identifier builds a deterministic, lower-case PostgreSQL identifier from the
// given parts. Database names are far less constrained than slot names - they
// may contain any character and be up to 63 bytes on their own - so every part
// is folded to [a-z0-9_] and the result is capped at NAMEDATALEN-1. A hash of
// the original parts is always appended so that two names that sanitise or
// truncate to the same string stay distinct.
func identifier(prefix, replica, db string) string {
	sum := sha256.Sum256([]byte(replica + "\x00" + db))
	suffix := "_" + hex.EncodeToString(sum[:])[:8]

	name := prefix + "_" + sanitize(replica) + "_" + sanitize(db)
	if len(name) > maxIdentifierLength-len(suffix) {
		name = name[:maxIdentifierLength-len(suffix)]
	}

	return name + suffix
}

// sanitize folds s to characters that are always safe in an unquoted PostgreSQL
// identifier.
func sanitize(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			return r
		case r >= 'A' && r <= 'Z':
			return r + ('a' - 'A')
		default:
			return '_'
		}
	}, s)
}
