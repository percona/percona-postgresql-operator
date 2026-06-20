#!/bin/bash
# Patroni on_role_change callback.
# Called by Patroni as: <script> on_role_change <role> <scope>
# Patches this pod's role label so Kubernetes Services can route correctly
# when etcd is the DCS (Patroni does not update pod labels in this case).

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

curl -sf -X PATCH \
    --cacert "${CA}" \
    -H "Authorization: Bearer ${TOKEN}" \
    -H "Content-Type: application/strategic-merge-patch+json" \
    "https://kubernetes.default.svc/api/v1/namespaces/${NAMESPACE}/pods/${POD}" \
    -d "{\"metadata\":{\"labels\":{\"postgres-operator.crunchydata.com/role\":\"${LABEL}\"}}}"
