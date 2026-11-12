<!-- Last verified: 2026-11-03 -->
# Percona Operator for PostgreSQL: notes for coding agents

Kubernetes operator (Go, controller-runtime) for PostgreSQL, built on
[Crunchy Data PGO](https://github.com/CrunchyData/postgres-operator). These
notes are for any coding agent (Codex, Cursor, Copilot, Claude Code, Gemini
CLI and others) and for humans. Agents read the nearest `AGENTS.md`, so rules
for a subtree live next to it:

| Area | File |
| ---- | ---- |
| End-to-end tests (kuttl) | [`e2e-tests/AGENTS.md`](e2e-tests/AGENTS.md) |

Contribution rules (CLA, Jira, branches, review) are in
[`CONTRIBUTING.md`](CONTRIBUTING.md); this file does not repeat them.

## Layout

- `pkg/apis/pgv2.percona.com/v2`: Percona CRD types (`PerconaPGCluster`,
  `PerconaPGBackup`, `PerconaPGRestore`, `PerconaPGUpgrade`).
  `pkg/apis/upstream.pgv2.percona.com/v1beta1`: upstream types
  (`PostgresCluster`, `PGUpgrade`, `PGAdmin`, `CrunchyBridgeCluster`).
- `percona/`: Percona code. `percona/controller/<kind>/` holds the reconciler
  for each Percona CRD.
- `internal/`: upstream PGO code plus Percona additions; upstream-origin
  files keep the Crunchy Data copyright header.
  `internal/controller/postgrescluster/` is the upstream reconciler.
- `cmd/postgres-operator/`: the operator entrypoint. The other `cmd/`
  directories are helper binaries built into the operator image; the init
  container copies them into the database pods.
- `config/`: kustomize sources of the manifests. `deploy/`: the rendered
  manifests (see "Generated files") and the hand-written examples `cr.yaml`,
  `backup.yaml`, `restore.yaml`, `upgrade.yaml`, `secrets.yaml`. `crVersion`
  in `deploy/cr.yaml` is the version e2e tests treat as current.
- `percona/version/version.txt`: the operator version.
- `build/postgres-operator/Dockerfile`: the operator image; `make build`
  builds and pushes it through `e2e-tests/build`.
- `e2e-tests/`: kuttl tests against a real cluster.
- `installers/olm/`: OLM bundle tooling for OpenShift.
- `hack/`: generator scripts used by `make generate`.

## How it fits together

- A `PerconaPGCluster` is the user-facing API. Its reconciler converts it with
  `ToCrunchy` into a `PostgresCluster` that the upstream controller reconciles.
  Put Percona behavior in `percona/`; change `internal/` only when the upstream
  code itself must behave differently, and say so in the PR.
- Each PostgreSQL pod has its own StatefulSet with one replica. An instance set
  with three replicas is three StatefulSets, not one with three replicas.
- Patroni runs HA inside the PostgreSQL pods, pgBackRest does backups (a
  dedicated repo-host StatefulSet plus Jobs), PgBouncer is a Deployment.
- RBAC comes from `+kubebuilder:rbac` markers; add one above a reconciler that
  touches a new resource type, then regenerate.
- Before adding a helper, look for an existing one in `percona/` and
  `internal/`. Reuse or merge duplicates instead of adding another.
- Clean up the code you touch: small fixes go in the same PR, large
  refactors in a separate one.

## Generated files

`make generate` renders `deploy/crd.yaml`, `rbac.yaml`, `cw-rbac.yaml`,
`operator.yaml`, `cw-operator.yaml`, `bundle.yaml`, `cw-bundle.yaml`, the
deepcopy functions, and inside `config/`: `crd/bases/`, `rbac/*/role.yaml`,
the version-label patches and the image in the kustomization files. Never
edit those by hand: change the Go types, markers or the kustomize sources,
run `make generate VERSION=main` (on a `release-*` branch:
`VERSION=$(cat percona/version/version.txt)
IMAGE_TAG_BASE=percona/percona-postgresql-operator`), and commit the result.
CI fails when the rendered files differ from the sources.

## Checks to run before pushing

These are the checks CI runs on every PR. Run the ones that match your change.

| Change | Command |
| ------ | ------- |
| Go code | `make check` (unit tests), `ENVTEST_K8S_VERSION=1.35 make check-envtest` (controller tests; the variable is required), `make lint` (CI reports only issues your change adds) |
| Go formatting | `gofmt -s`, `goimports-reviser -imports-order std,general,company,project -company-prefixes github.com/percona` |
| CRD types, RBAC markers or `config/` | `make generate VERSION=main`, then commit the generated files |
| Shell scripts | `shellcheck`, and `shfmt -bn -ci -s` (the CI formatter flags) |
| Any text | `misspell` (US spelling) |

E2e tests run on Jenkins for every PR, from the list in
`e2e-tests/run-pr.csv`; a rebuild of the same commit reruns only the tests
that did not pass. Paths in `.e2eignore` do not trigger a run, and a push
that touches only ignored paths inherits the previous build's result.

## Go conventions

Enforced by `.golangci.yaml`; the list here is what is easy to get wrong.

- Errors: `github.com/pkg/errors` (`errors.Wrap`, `errors.Errorf`).
  `fmt.Errorf` is forbidden, and the standard `errors` package outside tests.
- Logging: `logging.FromContext(ctx)` from `internal/logging`, with
  structured key-value pairs.
- Imports: `k8s.io/api` and `k8s.io/apimachinery/pkg/apis` packages use the
  `<group><version>` alias (`corev1`, `appsv1`, `metav1`); order is standard,
  third party, then `github.com/percona`.
- YAML: `sigs.k8s.io/yaml`, not `gopkg.in/yaml`.
- Tests: match the surrounding package. `percona/` uses `testify`
  (`assert`, `require`), `internal/` uses `gotest.tools/v3`. Table-driven
  tests; controller tests under the `envtest` build tag.

## Conventions

- Commit messages and PR titles start with the Jira key, for example
  `K8SPG-1245: Add AGENTS.md for coding agents`.
- Fill in `.github/pull_request_template.md`; say which checklist items do not
  apply instead of deleting them.
- Never commit credentials. Test secrets come from `e2e-tests/conf/`, and
  `e2e-tests/conf/cloud-secret.yml` is gitignored.
- Scripts must run on Linux CI and on macOS: GNU make 3.81, bash 3.2 and BSD
  `sed`/`awk`/`date` are what a Mac has by default. `e2e-tests/vars.sh`
  picks `gsed`/`gdate` when present and the Makefile `gsed`; do the same, or
  avoid GNU-only features.

## Documentation

- User docs: https://docs.percona.com/percona-operator-for-postgresql/.
  Sources live in [k8spg-docs](https://github.com/percona/k8spg-docs).
- CRD reference: `docs/content/references/crd.md`.
