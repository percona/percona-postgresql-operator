<!-- Last verified: 2026-11-03 -->
# End-to-end tests: notes for coding agents

Read the root [`AGENTS.md`](../AGENTS.md) first. These rules cover
`e2e-tests/`. The tests are [kuttl](https://kuttl.dev) suites: each test is a
directory of numbered steps, and the steps call bash helpers.

## Layout

- One directory per test: `tests/<name>/`. `NN-<step>.yaml` is a `TestStep`
  (a script that sources `../../functions`), `NN-assert.yaml` is a
  `TestAssert` (objects kuttl waits for, or a script for checks kuttl cannot
  express). Steps with the same `NN` run together.
- `functions` holds the shared helpers (`deploy_operator`, `get_cr`,
  `run_psql`, `wait_pg_condition`, `check_logs_for_errors`, ...) and sources
  `vars.sh`, which sets images, `PG_VER` (default `18`) and detects the
  platform.
- `get_cr [cr-name]` renders the cluster CR from `deploy/cr.yaml`, merged
  with `tests/<test>/conf/<cr-name>.yaml` when it exists (`cr-name` defaults
  to the test name; `<cr-name>-<platform>.yaml` wins on that platform). It
  also sets the name, users, `postgresVersion` and every image, including
  `initContainer.image` to the operator image under test. Use it; do not
  keep full CRs in test steps.
- `kuttl.yaml` is the suite config (step timeout 600 s). The PR job runs the
  tests in `run-pr.csv`; `run-release.csv`, `run-community.csv` and
  `run-minikube.csv` feed the other jobs. A test directory that is not in a
  list never runs in CI. Leaving it out is right only for a test that needs
  special infrastructure; say so in the PR.
- Version strings in asserts (`sidecars`, `init-deploy`,
  `upgrade-consistency`) are rewritten by `make release` and
  `make after-release`; keep new ones in the same form.

## Running locally

- `kubectl kuttl test --config e2e-tests/kuttl.yaml --test '^<name>$'
  --skip-delete` runs one test from the repo root. It uses the current
  kubectl context; check `kubectl config current-context` before every run.
- `deploy_operator` applies the CRDs server-side and recreates the operator
  in the test namespace. With `OPERATOR_NS` set, it installs the
  cluster-wide variant (`deploy/cw-*.yaml`) into that namespace instead.
- Images default to `perconalab/percona-postgresql-operator:<branch>` for the
  operator and `main-*` tags for PostgreSQL, pgBackRest and PgBouncer; see
  `vars.sh` for the current defaults and any temporary pins. Override with
  `IMAGE`, `IMAGE_POSTGRESQL`, `IMAGE_PGBOUNCER`, `IMAGE_BACKREST`, `PG_VER`,
  or `PG_DISTRIBUTION=community` (all `main-*-community` tags).
- Cloud backup tests need `conf/cloud-secret.yml` (gitignored).
- In-cluster S3 is SeaweedFS, deployed by `deploy_s3_storage` with
  `conf/seaweedfs-values.yaml` (plus `seaweedfs-tls-values.yaml` when a TLS
  secret is passed). The `migration-from-crunchy-*`, extension, logcollection
  and upgrade tests use it; do not call the chart with its defaults.

## Rules for test code

A test is useful only if it fails when the operator is broken. Review every
check against that.

- **Do not assert in e2e what a unit test can check.** Pure logic and
  rendering (how a CR field becomes a StatefulSet, Service or container spec,
  defaults, validation, version parsing) belong in Go unit tests (`make check`,
  envtest for reconcile logic), which run in seconds. Keep e2e for what needs a
  real cluster: PostgreSQL behavior, data, backups and restores, upgrades,
  failover, networking, and integration with Kubernetes and external systems.
- **Assert aggregate state on the `PerconaPGCluster` status**
  (`status.postgres.ready`, `status.pgbouncer.ready`, `status.state: ready`).
  Each PostgreSQL pod has its own StatefulSet with a generated name, so match
  StatefulSets by labels, never by name, and expect `replicas: 1` on each.
- **Every wait has a ceiling** and fails with what it expected and the last
  value it saw. Give every step and assert a `timeout:`; no unbounded `until`
  loops in scripts.
- **Fail fast on terminal states.** When the watched object reports an error
  state, stop with its message instead of waiting out the timeout.
- **No sleeps for readiness.** Poll the condition. Sleeps that encode real
  time (a soak, a PITR window, a known upstream bug workaround) stay, with a
  comment saying why.
- **Assertions must be able to fail.** A command whose result nothing checks
  is not a check; a commented-out check is a bug to fix or remove.
- **Check data, not only status.** After a restore, failover or upgrade,
  read the data back with `run_psql`; do not stop at pods being `Running`.
- **Test the version you mean to test.** Before testing an upgrade or a
  built artifact, assert it is the version under test (for example against
  `crVersion` in `deploy/cr.yaml`); a stale image passes otherwise.
- **"Nothing changed" needs a before/after snapshot.** To prove no restart or
  rollout, use `snapshot_workload_revisions` with `assert_no_operator_rollout`
  and `snapshot_tls_fingerprints` with `assert_tls_unchanged`, not the current
  state alone.
- **Check again at the end of a soak.** Problems often show up on a later
  reconcile: repeat the restart and error-log checks (`check_logs_for_errors`)
  after the observation window, not only at its start.
- **Keep allowlists narrow.** Allowed log lines get one entry each, matched as
  specifically as possible, with a comment on why they are expected.
- **Clean up what you create**, cluster-scoped objects included (CRDs,
  ClusterRoles, Helm releases), and use the `${NAMESPACE}` kuttl creates.
- **Bash:** every step script starts with `set -o errexit` and
  `set -o xtrace`, quotes variables, and reuses helpers from `functions`
  instead of copying them. `shellcheck` and `shfmt -bn -ci -s` must pass.
