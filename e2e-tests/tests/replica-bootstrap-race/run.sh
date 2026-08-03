#!/bin/bash

# =============================================================================
# Reproduction script for: "_crunchyrepl" role does not exist during replica bootstrap
#
# Issue: When a cluster with multiple instance sets and createReplicaMethods
# override (pgbackrest + basebackup) is created, the replica instance may try
# to bootstrap from the primary before Patroni has created the _crunchyrepl
# replication role. The pgbackrest method also fails with "invalid option
# '--scope=...'" due to Patroni's built-in pgbackrest integration being used
# instead of the operator's custom command (since no backup exists yet).
#
# Root Cause:
#   1. K8SPG-704 allows overriding createReplicaMethods, but when "pgbackrest"
#      is specified and no backup exists yet, the pgbackrest method section is
#      not configured in the Patroni YAML. Patroni falls back to its built-in
#      pgbackrest which uses the removed --scope option.
#   2. All instance sets (primary + replicas) are scaled up simultaneously,
#      so replicas may attempt pg_basebackup before the primary creates _crunchyrepl.
#   3. The liveness probe may kill the replica pod before retries succeed.
#
# Expected behavior: The replica should eventually succeed in bootstrapping.
# Actual behavior: The cluster gets stuck; replica fails repeatedly.
#
# Usage:
#   export NAMESPACE=replica-race-test
#   ./run.sh [iterations]
#
# The script will:
#   1. Deploy the operator
#   2. Create a cluster with 2 instance sets + createReplicaMethods override
#   3. Monitor for the "_crunchyrepl" error in replica pod logs
#   4. Report success/failure timing
#   5. Optionally repeat N times to measure failure rate
# =============================================================================

set -o errexit
set -o pipefail

ROOT_REPO=${ROOT_REPO:-$(realpath "$(dirname "$0")/../../..")}
test_name="replica-bootstrap-race"
source "${ROOT_REPO}/e2e-tests/vars.sh"
source "${ROOT_REPO}/e2e-tests/functions"

NAMESPACE="${NAMESPACE:-replica-race-test}"
CLUSTER_NAME="race-test"
ITERATIONS="${1:-5}"
TIMEOUT_SECONDS=180  # How long to wait before declaring the cluster stuck
LOG_DIR="${TEMP_DIR:-/tmp/kuttl/pg/${test_name}}"

mkdir -p "$LOG_DIR"

# Counters
total_runs=0
failures=0
successes=0
timing_log="$LOG_DIR/timing.log"

echo "============================================================"
echo " Replica Bootstrap Race Condition Reproducer"
echo "============================================================"
echo " Iterations:  $ITERATIONS"
echo " Namespace:   $NAMESPACE"
echo " Cluster:     $CLUSTER_NAME"
echo " Timeout:     ${TIMEOUT_SECONDS}s per attempt"
echo " Log dir:     $LOG_DIR"
echo " Operator:    $IMAGE"
echo " PG Image:    $IMAGE_POSTGRESQL"
echo " PG Version:  $PG_VER"
echo "============================================================"
echo ""

# -------------------------------------------------------------------
# Generate the PerconaPGCluster CR that triggers the race condition.
# Key elements:
#   - Two separate instance sets (i1 and i2) with 1 replica each
#   - createReplicaMethods: [pgbackrest, basebackup] (K8SPG-704 override)
#   - Volume-based pgbackrest repo (no S3 needed)
# -------------------------------------------------------------------
generate_cluster_cr() {
    cat <<EOF
apiVersion: pgv2.percona.com/v2
kind: PerconaPGCluster
metadata:
  name: ${CLUSTER_NAME}
  namespace: ${NAMESPACE}
spec:
  crVersion: "${CR_VERSION:-3.1.0}"

  image: ${IMAGE_POSTGRESQL}
  imagePullPolicy: IfNotPresent
  postgresVersion: ${PG_VER}

  users:
    - name: testuser
      databases:
        - testdb
      options: "LOGIN"
      password:
        type: AlphaNumeric
    - name: postgres
      password:
        type: AlphaNumeric

  instances:
    # Primary instance set
    - name: i1
      replicas: 1
      dataVolumeClaimSpec:
        accessModes:
          - ReadWriteOnce
        resources:
          requests:
            storage: 1Gi

    # Replica instance set - this is the one that races
    - name: i2
      replicas: 1
      dataVolumeClaimSpec:
        accessModes:
          - ReadWriteOnce
        resources:
          requests:
            storage: 1Gi

  proxy:
    pgBouncer:
      replicas: 1
      image: ${IMAGE_PGBOUNCER}

  backups:
    pgbackrest:
      image: ${IMAGE_BACKREST}
      repoHost:
        affinity:
          podAntiAffinity:
            preferredDuringSchedulingIgnoredDuringExecution:
              - weight: 1
                podAffinityTerm:
                  labelSelector:
                    matchLabels:
                      postgres-operator.crunchydata.com/data: pgbackrest
                  topologyKey: kubernetes.io/hostname
      manual:
        repoName: repo1
        options:
          - --type=full
      repos:
        - name: repo1
          volume:
            volumeClaimSpec:
              accessModes:
                - ReadWriteOnce
              resources:
                requests:
                  storage: 2Gi

  # THIS IS THE KEY TRIGGER: overriding createReplicaMethods via K8SPG-704
  # When no backup exists, the "pgbackrest" method has no configured command
  # in the Patroni YAML, causing Patroni to use its built-in pgbackrest
  # integration which passes the removed --scope option.
  patroni:
    createReplicaMethods:
      - pgbackrest
      - basebackup
EOF
}

# -------------------------------------------------------------------
# Check if the _crunchyrepl error appears in any pod's logs
# -------------------------------------------------------------------
check_for_crunchyrepl_error() {
    local found=false
    local pods

    pods=$(kubectl -n "$NAMESPACE" get pods \
        -l "postgres-operator.crunchydata.com/cluster=${CLUSTER_NAME},postgres-operator.crunchydata.com/data=postgres" \
        -o jsonpath='{.items[*].metadata.name}' 2>/dev/null || echo "")

    for pod in $pods; do
        if kubectl -n "$NAMESPACE" logs "$pod" -c database 2>/dev/null | grep -q 'role "_crunchyrepl" does not exist'; then
            found=true
            echo "$pod"
            break
        fi
    done

    $found
}

# -------------------------------------------------------------------
# Check if the --scope error appears (pgbackrest built-in fallback)
# -------------------------------------------------------------------
check_for_scope_error() {
    local pods

    pods=$(kubectl -n "$NAMESPACE" get pods \
        -l "postgres-operator.crunchydata.com/cluster=${CLUSTER_NAME},postgres-operator.crunchydata.com/data=postgres" \
        -o jsonpath='{.items[*].metadata.name}' 2>/dev/null || echo "")

    for pod in $pods; do
        if kubectl -n "$NAMESPACE" logs "$pod" -c database 2>/dev/null | grep -q "invalid option '--scope="; then
            echo "$pod"
            return 0
        fi
    done

    return 1
}

# -------------------------------------------------------------------
# Wait for the cluster to become ready, or timeout
# Returns 0 if ready, 1 if timeout
# -------------------------------------------------------------------
wait_for_cluster_or_timeout() {
    local elapsed=0
    local interval=5

    while [[ $elapsed -lt $TIMEOUT_SECONDS ]]; do
        local state
        state=$(kubectl -n "$NAMESPACE" get pg "$CLUSTER_NAME" -o jsonpath='{.status.state}' 2>/dev/null || echo "unknown")

        if [[ "$state" == "ready" ]]; then
            # Verify all StatefulSets are rolled out
            local all_ready=true
            local sts_list
            sts_list=$(kubectl -n "$NAMESPACE" get sts \
                -l "postgres-operator.crunchydata.com/cluster=${CLUSTER_NAME}" \
                -o jsonpath='{.items[*].metadata.name}' 2>/dev/null || echo "")

            for sts in $sts_list; do
                local desired ready
                desired=$(kubectl -n "$NAMESPACE" get sts "$sts" -o jsonpath='{.spec.replicas}' 2>/dev/null || echo "0")
                ready=$(kubectl -n "$NAMESPACE" get sts "$sts" -o jsonpath='{.status.readyReplicas}' 2>/dev/null || echo "0")
                if [[ "$desired" != "$ready" ]]; then
                    all_ready=false
                    break
                fi
            done

            if $all_ready; then
                return 0
            fi
        fi

        sleep $interval
        elapsed=$((elapsed + interval))

        # Check for the error while waiting
        if check_for_crunchyrepl_error >/dev/null 2>&1; then
            echo "  [${elapsed}s] ⚠️  _crunchyrepl error detected!"
        fi
    done

    return 1
}

# -------------------------------------------------------------------
# Collect diagnostic information
# -------------------------------------------------------------------
collect_diagnostics() {
    local iteration=$1
    local diag_dir="$LOG_DIR/iteration-${iteration}"
    mkdir -p "$diag_dir"

    echo "  Collecting diagnostics to $diag_dir..."

    # Pod states
    kubectl -n "$NAMESPACE" get pods -o wide > "$diag_dir/pods.txt" 2>&1 || true

    # Cluster status
    kubectl -n "$NAMESPACE" get pg "$CLUSTER_NAME" -o yaml > "$diag_dir/cluster-status.yaml" 2>&1 || true

    # Pod logs
    local pods
    pods=$(kubectl -n "$NAMESPACE" get pods \
        -l "postgres-operator.crunchydata.com/cluster=${CLUSTER_NAME},postgres-operator.crunchydata.com/data=postgres" \
        -o jsonpath='{.items[*].metadata.name}' 2>/dev/null || echo "")

    for pod in $pods; do
        kubectl -n "$NAMESPACE" logs "$pod" -c database --timestamps > "$diag_dir/${pod}-database.log" 2>&1 || true
        kubectl -n "$NAMESPACE" logs "$pod" -c database --previous --timestamps > "$diag_dir/${pod}-database-previous.log" 2>&1 || true
    done

    # Operator logs
    local op_pod
    op_pod=$(kubectl get pods -n "${OPERATOR_NS:-$NAMESPACE}" \
        --selector=app.kubernetes.io/name=percona-postgresql-operator \
        -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || echo "")
    if [[ -n "$op_pod" ]]; then
        kubectl -n "${OPERATOR_NS:-$NAMESPACE}" logs "$op_pod" -c operator --tail=500 > "$diag_dir/operator.log" 2>&1 || true
    fi

    # Patroni ConfigMaps
    kubectl -n "$NAMESPACE" get configmap \
        -l "postgres-operator.crunchydata.com/cluster=${CLUSTER_NAME}" \
        -o yaml > "$diag_dir/configmaps.yaml" 2>&1 || true

    # Events
    kubectl -n "$NAMESPACE" get events --sort-by='.lastTimestamp' > "$diag_dir/events.txt" 2>&1 || true

    # Check pod restarts
    echo "  Pod restart counts:"
    kubectl -n "$NAMESPACE" get pods \
        -l "postgres-operator.crunchydata.com/cluster=${CLUSTER_NAME}" \
        -o custom-columns='NAME:.metadata.name,RESTARTS:.status.containerStatuses[*].restartCount,STATUS:.status.phase' 2>/dev/null || true
}

# -------------------------------------------------------------------
# Cleanup cluster
# -------------------------------------------------------------------
cleanup_cluster() {
    echo "  Cleaning up cluster..."
    kubectl -n "$NAMESPACE" delete perconapgcluster "$CLUSTER_NAME" --wait=false 2>/dev/null || true

    # Wait for pods to be deleted
    local wait_time=0
    while [[ $wait_time -lt 60 ]]; do
        local remaining
        remaining=$(kubectl -n "$NAMESPACE" get pods \
            -l "postgres-operator.crunchydata.com/cluster=${CLUSTER_NAME}" \
            --no-headers 2>/dev/null | wc -l || echo "0")
        if [[ "$remaining" -eq 0 ]]; then
            break
        fi
        sleep 5
        wait_time=$((wait_time + 5))
    done

    # Force delete PVCs
    kubectl -n "$NAMESPACE" delete pvc \
        -l "postgres-operator.crunchydata.com/cluster=${CLUSTER_NAME}" \
        --wait=false 2>/dev/null || true

    sleep 5
}

# -------------------------------------------------------------------
# Run a single iteration
# -------------------------------------------------------------------
run_iteration() {
    local iteration=$1
    local start_time
    start_time=$(date +%s)

    echo ""
    echo "------------------------------------------------------------"
    echo " Iteration $iteration / $ITERATIONS"
    echo "------------------------------------------------------------"
    echo "  $(date '+%Y-%m-%d %H:%M:%S') - Creating cluster..."

    # Apply the CR
    generate_cluster_cr | kubectl -n "$NAMESPACE" apply -f -

    echo "  Waiting for cluster to become ready (timeout: ${TIMEOUT_SECONDS}s)..."

    # Wait and check
    if wait_for_cluster_or_timeout; then
        local end_time
        end_time=$(date +%s)
        local duration=$((end_time - start_time))

        echo "  ✅ Cluster became ready in ${duration}s"

        # Even if ready, check if the error appeared during bootstrap
        if check_for_crunchyrepl_error >/dev/null 2>&1; then
            echo "  ⚠️  _crunchyrepl error DID appear but cluster recovered"
            echo "ITERATION $iteration: RECOVERED (${duration}s) - error appeared but cluster recovered" >> "$timing_log"
        else
            echo "  ✅ No _crunchyrepl error detected"
            echo "ITERATION $iteration: SUCCESS (${duration}s) - no error" >> "$timing_log"
        fi

        # Check for the --scope error (pgbackrest built-in fallback)
        local scope_pod
        if scope_pod=$(check_for_scope_error); then
            echo "  ⚠️  --scope error detected in $scope_pod (pgbackrest built-in used instead of operator command)"
        fi

        successes=$((successes + 1))
    else
        local end_time
        end_time=$(date +%s)
        local duration=$((end_time - start_time))

        echo "  ❌ FAILED: Cluster did not become ready within ${TIMEOUT_SECONDS}s"

        # Check which specific error occurred
        local error_pod
        if error_pod=$(check_for_crunchyrepl_error); then
            echo "  🔍 _crunchyrepl error found in pod: $error_pod"
            echo ""
            echo "  --- Relevant log lines ---"
            kubectl -n "$NAMESPACE" logs "$error_pod" -c database 2>/dev/null \
                | grep -E "(crunchyrepl|scope|bootstrap|basebackup|replica)" \
                | tail -20
            echo "  --- End log lines ---"
        fi

        if scope_pod=$(check_for_scope_error); then
            echo "  🔍 --scope error found in pod: $scope_pod"
        fi

        echo "ITERATION $iteration: FAILED (${duration}s)" >> "$timing_log"
        failures=$((failures + 1))

        collect_diagnostics "$iteration"
    fi

    total_runs=$((total_runs + 1))

    # Cleanup for next iteration
    cleanup_cluster
}

# ===================================================================
# MAIN
# ===================================================================

echo ""
echo "Step 1: Setting up namespace..."
create_namespace "$NAMESPACE"

echo ""
echo "Step 2: Deploying operator..."
deploy_operator
echo "  Waiting for operator to be ready..."
kubectl -n "${OPERATOR_NS:-$NAMESPACE}" wait deployment percona-postgresql-operator \
    --for=condition=available --timeout=120s

echo ""
echo "Step 3: Running $ITERATIONS iterations..."
echo "  (Creating and destroying the cluster each time to test the race)"
echo ""

> "$timing_log"  # Clear timing log

for i in $(seq 1 "$ITERATIONS"); do
    run_iteration "$i"
done

# ===================================================================
# Summary
# ===================================================================
echo ""
echo "============================================================"
echo " RESULTS SUMMARY"
echo "============================================================"
echo " Total runs:     $total_runs"
echo " Successes:      $successes"
echo " Failures:       $failures"
echo " Failure rate:   $(( failures * 100 / total_runs ))%"
echo ""
echo " Timing log:"
cat "$timing_log"
echo ""
echo " Diagnostics:    $LOG_DIR"
echo "============================================================"

if [[ $failures -gt 0 ]]; then
    echo ""
    echo "🐛 Bug reproduced! $failures out of $total_runs attempts failed."
    echo ""
    echo "The issue is caused by:"
    echo "  1. createReplicaMethods override (K8SPG-704) lists 'pgbackrest' but"
    echo "     no pgbackrest command section is configured (no backup exists yet)."
    echo "     Patroni uses its built-in pgbackrest which passes --scope (removed"
    echo "     in newer pgbackrest versions)."
    echo "  2. Fallback to 'basebackup' hits the _crunchyrepl race because all"
    echo "     instance sets are scaled up before the primary finishes bootstrap."
    echo "  3. Liveness probe may kill the replica pod before retries succeed."
    echo ""
    echo "Relevant code:"
    echo "  - internal/patroni/config.go:601-607 (K8SPG-704 override)"
    echo "  - internal/patroni/config.go:555-598 (pgbackrest command only set if backup exists)"
    echo "  - internal/controller/postgrescluster/instance.go:653 (all sets scaled simultaneously)"
    exit 1
else
    echo ""
    echo "✅ Bug was NOT reproduced in $total_runs attempts."
    echo "   Try increasing iterations or running on a busier cluster."
    exit 0
fi



