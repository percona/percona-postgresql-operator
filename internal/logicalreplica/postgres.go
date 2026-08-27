// Copyright 2021 - 2026 Crunchy Data Solutions, Inc.
//
// SPDX-License-Identifier: Apache-2.0

package logicalreplica

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/percona/percona-postgresql-operator/v3/internal/postgres"
	"github.com/percona/percona-postgresql-operator/v3/pkg/apis/upstream.pgv2.percona.com/v1beta1"
)

const (
	// ReplicationUser must match v2.UserLogicalReplication.
	ReplicationUser = "logicalrepl"

	outputPlugin = "pgoutput"

	// maxIdentifierLength is PostgreSQL's NAMEDATALEN-1.
	maxIdentifierLength = 63

	// hbaAuthMethod is what tells the rules in [PostgreSQLHBAs] apart from the
	// "reject" rule beside them, which spec.tlsOnly renders as "hostssl" too.
	hbaAuthMethod = "scram-sha-256"
	hbaOrigin     = "hostssl"
)

// Reasons reported by [PrimaryReadinessQuery], rendered by
// [PrimaryReadinessMessage].
const (
	reasonPrimaryInRecovery       = "PrimaryInRecovery"
	reasonWALLevelNotLogical      = "WALLevelNotLogical"
	reasonReplicationRoleNotReady = "ReplicationRoleNotReady"
	reasonReplicationHBAMissing   = "ReplicationHBAMissing"
	ReasonRestartPending          = "RestartPending"
)

// PrimaryReadinessQuery returns a query whose single column lists, comma
// separated, every prerequisite of a logical replica bootstrap that the primary
// does not yet satisfy. An empty result means it satisfies all of them.
//
// The pg_hba check is load-bearing beyond itself: patroni.DynamicConfiguration
// is the only writer of pg_hba.conf and it carries ignore_slots in the same map,
// so a rule on disk proves ignore_slots reached Patroni too.
func PrimaryReadinessQuery() string {
	return fmt.Sprintf(`SELECT coalesce(pg_catalog.string_agg(c.name, ',' ORDER BY c.n), '')
  FROM (VALUES
    (1, %[2]s, NOT pg_catalog.pg_is_in_recovery()),
    (2, %[3]s, pg_catalog.current_setting('wal_level') = 'logical'),
    (3, %[4]s, NOT EXISTS (
        SELECT 1 FROM pg_catalog.pg_settings WHERE pending_restart)),
    (4, %[5]s, EXISTS (
        SELECT 1 FROM pg_catalog.pg_authid
         WHERE rolname = %[1]s
           AND rolcanlogin AND rolreplication AND rolsuper
           AND rolpassword IS NOT NULL
           AND (rolvaliduntil IS NULL OR rolvaliduntil > pg_catalog.now()))),
    (5, %[6]s, EXISTS (
        SELECT 1 FROM pg_catalog.pg_hba_file_rules r
         WHERE r.error IS NULL AND r.type = %[7]s AND r.auth_method = %[8]s
           AND %[1]s = ANY (r.user_name) AND 'replication' = ANY (r.database))
      AND EXISTS (
        SELECT 1 FROM pg_catalog.pg_hba_file_rules r
         WHERE r.error IS NULL AND r.type = %[7]s AND r.auth_method = %[8]s
           AND %[1]s = ANY (r.user_name) AND 'all' = ANY (r.database)))
  ) AS c(n, name, satisfied)
 WHERE NOT c.satisfied;`,
		postgres.QuoteLiteral(ReplicationUser),
		postgres.QuoteLiteral(reasonPrimaryInRecovery),
		postgres.QuoteLiteral(reasonWALLevelNotLogical),
		postgres.QuoteLiteral(ReasonRestartPending),
		postgres.QuoteLiteral(reasonReplicationRoleNotReady),
		postgres.QuoteLiteral(reasonReplicationHBAMissing),
		postgres.QuoteLiteral(hbaOrigin),
		postgres.QuoteLiteral(hbaAuthMethod))
}

// ParsePrimaryReadinessReasons reads the output of [PrimaryReadinessQuery]. A
// nil result means the primary satisfies every prerequisite.
func ParsePrimaryReadinessReasons(stdout string) []string {
	var reasons []string

	for reason := range strings.SplitSeq(stdout, ",") {
		if reason = strings.TrimSpace(reason); reason != "" {
			reasons = append(reasons, reason)
		}
	}

	return reasons
}

// PrimaryReadinessMessage renders one of the reasons above as a sentence for a
// condition message.
func PrimaryReadinessMessage(reason string) string {
	switch reason {
	case reasonPrimaryInRecovery:
		return "the primary is in recovery; pg_createsubscriber needs a writable publisher"
	case reasonWALLevelNotLogical:
		return `"wal_level" is not "logical"; set it back via spec.patroni.dynamicConfiguration`
	case ReasonRestartPending:
		return "PostgreSQL has a parameter change pending a restart, which would cut off the bootstrap"
	case reasonReplicationRoleNotReady:
		return "the " + ReplicationUser + " role has not been created yet"
	case reasonReplicationHBAMissing:
		return "the pg_hba rules that let " + ReplicationUser + " reach the primary have not been written yet"
	default:
		return reason
	}
}

// Enabled returns whether the cluster has any logical replica configured.
func Enabled(inCluster *v1beta1.PostgresCluster) bool {
	return inCluster != nil && len(inCluster.Spec.LogicalReplicas) > 0
}

// PostgreSQLHBAs provides the Postgres HBA rules that let a logical replica and
// the tools that build it reach the primary as ReplicationUser.
func PostgreSQLHBAs(inCluster *v1beta1.PostgresCluster, outHBAs *postgres.HBAs) {
	if !Enabled(inCluster) {
		return
	}

	// Mandatory rather than Default: the default "hostssl all all all" rule is
	// dropped as soon as the user supplies any pg_hba rules of their own.
	outHBAs.Mandatory = append(outHBAs.Mandatory,
		// PrimaryReadinessQuery matches exactly these two rules; keep them in step.
		postgres.NewHBA().TLS().Users(ReplicationUser).Method(hbaAuthMethod).Replication(),
		postgres.NewHBA().TLS().Users(ReplicationUser).Method(hbaAuthMethod),
		// Never allow this superuser to authenticate without TLS.
		postgres.NewHBA().TCP().Users(ReplicationUser).Method("reject"))
}

// IgnoreSlotsMatchers returns the Patroni "ignore_slots" entries that keep it
// from deleting the logical replication slots backing the logical replicas.
// Patroni drops slots it does not know about regardless of "use_slots", so
// without this the slot pg_createsubscriber leaves behind dies on the next HA
// loop.
//
// The matcher deliberately carries no name: slot names are only known once the
// databases of a replica have been resolved, which happens after this
// configuration has been rendered. Matching every logical pgoutput slot is
// stable from the moment a logical replica appears in the spec.
func IgnoreSlotsMatchers(inCluster *v1beta1.PostgresCluster) []any {
	if !Enabled(inCluster) {
		return nil
	}

	return []any{map[string]any{
		"type":   "logical",
		"plugin": outputPlugin,
	}}
}

// Names of the replication objects backing database db of a logical replica.
func SlotName(replica, db string) string {
	return identifier("pgo_lr_slot", replica, db)
}

func PublicationName(replica, db string) string {
	return identifier("pgo_lr_pub", replica, db)
}

func SubscriptionName(replica, db string) string {
	return identifier("pgo_lr_sub", replica, db)
}

// DisableOnErrorSQL returns the statement that puts the subscription of database
// db on the given logical replica into "disable_on_error" mode.
//
// pg_createsubscriber has no option for this and it is a per-subscription option
// rather than a server setting. Without it a single conflicting row leaves the
// apply worker retrying forever while its slot pins WAL on the primary.
func DisableOnErrorSQL(replica, db string) string {
	return fmt.Sprintf("ALTER SUBSCRIPTION %q SET (disable_on_error = true);",
		SubscriptionName(replica, db))
}

// identifier folds the given parts to [a-z0-9_], caps the result at
// NAMEDATALEN-1, and always appends a hash so that names which sanitize or
// truncate alike stay distinct.
func identifier(prefix, replica, db string) string {
	sum := sha256.Sum256([]byte(replica + "\x00" + db))
	suffix := "_" + hex.EncodeToString(sum[:])[:8]

	name := prefix + "_" + sanitize(replica) + "_" + sanitize(db)
	if len(name) > maxIdentifierLength-len(suffix) {
		name = name[:maxIdentifierLength-len(suffix)]
	}

	return name + suffix
}

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
