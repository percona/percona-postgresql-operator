#!/bin/bash
# Patroni on_role_change / on_start callback.
# Called by Patroni as: <script> <event> <role> <scope>
# With etcd DCS, Patroni does not update pod labels or annotations.
# This script patches both so that:
#   - Service selectors work (role label)
#   - IsWritable() works (status annotation read by the operator)

ROLE=${2}

NAMESPACE=$(cat /var/run/secrets/kubernetes.io/serviceaccount/namespace)
TOKEN=$(cat /var/run/secrets/kubernetes.io/serviceaccount/token)
CA=/var/run/secrets/kubernetes.io/serviceaccount/ca.crt
POD=${HOSTNAME}

case "${ROLE}" in
    master|primary)  LABEL="primary" ;;
    replica)         LABEL="replica" ;;
    *)               LABEL="${ROLE}" ;;
esac

PATCH_BODY="{\"metadata\":{\"labels\":{\"postgres-operator.crunchydata.com/role\":\"${LABEL}\"},\"annotations\":{\"status\":\"{\\\"role\\\":\\\"${LABEL}\\\"}\"}}}"

curl -sf -X PATCH \
    --cacert "${CA}" \
    -H "Authorization: Bearer ${TOKEN}" \
    -H "Content-Type: application/strategic-merge-patch+json" \
    "https://kubernetes.default.svc/api/v1/namespaces/${NAMESPACE}/pods/${POD}" \
    -d "${PATCH_BODY}"
