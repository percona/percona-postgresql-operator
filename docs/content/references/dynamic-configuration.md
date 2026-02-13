---
title: Dynamic Configuration Reference
draft: false
weight: 110
---

# Dynamic Configuration Reference

This document describes which PostgreSQL and Patroni settings can and cannot be configured by users via `spec.patroni.dynamicConfiguration` in the PerconaPGCluster cluster custom resource.

## Overview

The operator uses Patroni's dynamic configuration to manage PostgreSQL cluster settings. Users can override many settings through `dynamicConfiguration`, but some parameters are **mandatory** and always enforced by the operator for security, replication, backups, and monitoring.

## Configurable Settings

The following PostgreSQL parameters have operator-defined defaults but **can be overridden** via `spec.patroni.dynamicConfiguration.postgresql.parameters`:

| Parameter | Default | Description |
|-----------|---------|-------------|
| `wal_level` | `logical` | Enables logical replication, streaming replication, and WAL archiving |
| `jit` | `off` | Just-in-Time compilation for query execution |
| `password_encryption` | `scram-sha-256` | Password hashing algorithm |
| `archive_timeout` | `60s` | Forces a WAL file switch after this many seconds |
| `huge_pages` | `try` or `off` | Depends on instance resource requests |

**Example:**

```yaml
spec:
  patroni:
    dynamicConfiguration:
      postgresql:
        parameters:
          wal_level: "replica"
          shared_buffers: "256MB"
```

Users can also add any other PostgreSQL parameter not listed in the non-configurable sections below.

## Non-Configurable PostgreSQL Parameters

These parameters are always set by the operator and **cannot be overridden** via dynamicConfiguration:

### Core Security & Connectivity

| Parameter | Value | Purpose |
|-----------|-------|---------|
| `unix_socket_directories` | `/tmp/postgres` | UNIX socket location for local connections |
| `ssl` | `on` | TLS/SSL always enabled |
| `ssl_cert_file` | `/pgconf/tls/tls.crt` | Server certificate path |
| `ssl_key_file` | `/pgconf/tls/tls.key` | Server private key path |
| `ssl_ca_file` | `/pgconf/tls/ca.crt` | Certificate authority path |

### Backup & Recovery (pgBackRest)

| Parameter | Value | Purpose |
|-----------|-------|---------|
| `archive_mode` | `on` | WAL archiving required for backups |
| `archive_command` | pgBackRest command or `true` | How WAL files are shipped to archive |
| `restore_command` | pgBackRest command | How WAL files are fetched during recovery |

**Note:** On standby clusters with `spec.standby.repoName` configured, `restore_command` is always set by the operator. On primary clusters, users can override `restore_command` via dynamicConfiguration if needed.

### Optional: Track Restorable Time

| Parameter | Condition | Value |
|-----------|-----------|-------|
| `track_commit_timestamp` | When `spec.backups.trackLatestRestorableTime` is enabled, or operator version < 2.8.0 | `true` |

### Extensions & Monitoring

When the following extensions are enabled, their parameters cannot be removed or overridden:

| Extension | Non-Configurable Parameters |
|-----------|-----------------------------|
| pg_stat_statements | `shared_preload_libraries` (pg_stat_statements), `pg_stat_statements.track` = `all` |
| pg_stat_monitor | `shared_preload_libraries` (pg_stat_monitor), `pg_stat_monitor.pgsm_query_max_len` = `2048` |
| pgaudit | `shared_preload_libraries` (pgaudit) |
| Exporter (pgMonitor) | `shared_preload_libraries` (pg_stat_statements, pgnodemx), `pgnodemx.kdapi_path` |

**shared_preload_libraries:** Users can add custom libraries via dynamicConfiguration; they are prepended to the list. However, mandatory libraries required by enabled extensions are always appended and cannot be removed.

## Non-Configurable Patroni/DCS Settings

These Patroni settings are derived from the cluster spec or hardcoded and cannot be set via dynamicConfiguration:

| Setting | Source | Description |
|---------|--------|-------------|
| `ttl` | `spec.patroni.leaderLeaseDurationSeconds` | Leader lease TTL in DCS |
| `loop_wait` | `spec.patroni.syncPeriodSeconds` | How often Patroni checks DCS |
| `postgresql.use_slots` | Hardcoded | Always `false` |
| `postgresql.use_pg_rewind` | `spec.postgresVersion` | `true` when PostgreSQL version > 10 |
| `postgresql.bin_name.pg_rewind` | TDE configuration | Set to `/tmp/pg_rewind_tde.sh` when TDE is enabled |

## pg_hba Configuration

Users can add custom authentication rules via `spec.patroni.dynamicConfiguration.postgresql.pg_hba`. However, **mandatory rules** are always prepended by the operator and cannot be removed:

- Local `postgres` user (peer auth)
- Replication user (TLS certificate auth)
- Monitoring user rules (when exporter is enabled)
- PgBouncer and PMM rules (when those components are enabled)
- TLS-only rules (when `spec.tlsOnly` is true)

Custom rules from dynamicConfiguration are appended after the mandatory rules.

## Standby Clusters

For standby clusters (`spec.standby.enabled: true`), the `standby_cluster` section is populated by the operator. Critical fields such as `restore_command` are overridden when `spec.standby.repoName` is set. User-provided standby_cluster settings are merged where applicable, but operator-managed fields take precedence.

## Summary

- **Configurable:** Default parameters (`wal_level`, `jit`, `password_encryption`, `archive_timeout`, `huge_pages`) and any parameter not listed as mandatory.
- **Not configurable:** Security (SSL, sockets), backup/archive commands, extension parameters when extensions are enabled, and Patroni DCS settings (`ttl`, `loop_wait`, `use_slots`, `use_pg_rewind`).
- **Partially configurable:** `pg_hba` (add rules only), `shared_preload_libraries` (add libraries only), `restore_command` on primary clusters (overridden on standby with RepoName).
