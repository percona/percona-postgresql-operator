# Replica Bootstrap Race Condition Test

## Issue

Since Percona PG Operator 2.6.0, clusters with multiple instance sets sometimes
fail to start replicas because the `_crunchyrepl` replication role does not exist
when `pg_basebackup` attempts to connect to the primary. The failure rate
increased to >50% in 2.8.2.

## Root Cause

Two interacting problems:

1. **K8SPG-704 `createReplicaMethods` override bug**: When a user specifies
   `createReplicaMethods: [pgbackrest, basebackup]`, the operator puts "pgbackrest"
   in the methods list but does NOT configure the `postgresql.pgbackrest` command
   section in Patroni's YAML (because no backup exists during initial cluster
   creation). Patroni then falls back to its **built-in** pgbackrest integration,
   which passes the `--scope` flag — removed in newer pgbackrest versions. This
   causes the pgbackrest method to fail permanently with
   `ERROR: [031]: invalid option '--scope=...'`.

2. **Simultaneous instance set scale-up**: All instance sets are scaled up in one
   pass (`internal/controller/postgrescluster/instance.go:653`). Replicas start
   Patroni immediately and try `pg_basebackup`, but the primary's Patroni hasn't
   finished its bootstrap (which creates `_crunchyrepl`). Combined with the broken
   pgbackrest method wasting time, and the liveness probe potentially killing the
   pod, the replica may never recover.

## How to Reproduce

### Option 1: Standalone script (recommended for repeated testing)

```bash
cd e2e-tests/tests/replica-bootstrap-race
export NAMESPACE=replica-race-test
./run.sh 10  # Run 10 iterations
```

### Option 2: KUTTL test

```bash
kubectl kuttl test --config e2e-tests/kuttl.yaml --test replica-bootstrap-race
```

## Key Configuration That Triggers the Bug

```yaml
spec:
  instances:
    - name: i1
      replicas: 1
    - name: i2       # separate instance set for replica
      replicas: 1
  patroni:
    createReplicaMethods:
      - pgbackrest   # broken: no command configured, uses built-in with --scope
      - basebackup   # races: _crunchyrepl may not exist yet
```

## What to Look For

1. In replica pod logs (`database` container):
   - `FATAL: role "_crunchyrepl" does not exist` — the race condition
   - `ERROR: [031]: invalid option '--scope=...'` — K8SPG-704 bug

2. Pod restarts — liveness probe killing the pod before retries succeed

3. Cluster never reaching `status.state: ready`

## Relevant Code Paths

| File | Lines | Description |
|------|-------|-------------|
| `internal/patroni/config.go` | 601-607 | K8SPG-704: override without configuring command |
| `internal/patroni/config.go` | 555-598 | pgbackrest command only set if backup exists |
| `internal/controller/postgrescluster/instance.go` | 653 | All sets scaled simultaneously |
| `internal/patroni/reconcile.go` | 193-194 | Liveness probe timing |
| `internal/pgbackrest/reconcile.go` | 457-462 | ReplicaCreateCommand returns nil if no backup |

