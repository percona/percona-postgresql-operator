#!/bin/bash
# Helpers for asserting external-dns annotations on Services.
#
# The operator writes them asynchronously, and removing them takes another
# reconcile, so every check polls instead of reading once.

# Exported because the kuttl steps that source this file use them too.
export ANNOTATION_HOSTNAME='external-dns.alpha.kubernetes.io/hostname'
export ANNOTATION_TTL='external-dns.alpha.kubernetes.io/ttl'
export ANNOTATION_MANAGED='percona.com/external-dns-managed'

get_annotation() {
	local svc=$1
	local key=$2

	kubectl -n "${NAMESPACE}" get service/"${svc}" -o json | jq -r ".metadata.annotations[\"${key}\"] // \"\""
}

# wait_annotation waits for an annotation to reach a value. Pass "" to wait for
# the annotation to be removed.
wait_annotation() {
	local svc=$1
	local key=$2
	local expected=$3

	echo -n "waiting for annotation ${key}=\"${expected}\" on service/${svc}"
	local timeout=0
	until [[ $(get_annotation "${svc}" "${key}") == "${expected}" ]]; do
		sleep 2
		timeout=$((timeout + 2))
		echo -n '.'
		if [[ ${timeout} -gt 240 ]]; then
			echo
			echo "Waiting timeout has been reached. Annotation ${key} on service/${svc} is \"$(get_annotation "${svc}" "${key}")\", expected \"${expected}\". Exiting..."
			exit 1
		fi
	done
	echo ".OK"
}

# check_annotation_stays fails if an annotation changes over a few reconcile
# loops. Used to prove the operator leaves annotations it does not own alone.
check_annotation_stays() {
	local svc=$1
	local key=$2
	local expected=$3

	sleep 30 # a few reconcile loops
	local actual
	actual=$(get_annotation "${svc}" "${key}")
	if [[ ${actual} != "${expected}" ]]; then
		echo "Annotation ${key} on service/${svc} is \"${actual}\", expected it to stay \"${expected}\". Exiting..."
		exit 1
	fi
	echo "annotation ${key}=\"${expected}\" on service/${svc} preserved.OK"
}

# wait_san waits for a certificate in a Secret to carry a SAN.
wait_san() {
	local secret=$1
	local key=$2
	local expected=$3

	# kubectl jsonpath needs the dots inside the key escaped, even in brackets.
	local path="{.data['${key//./\\.}']}"

	echo -n "waiting for SAN ${expected} in secret/${secret}"
	local timeout=0
	until kubectl -n "${NAMESPACE}" get secret/"${secret}" -o jsonpath="${path}" \
		| base64 -d \
		| openssl x509 -noout -ext subjectAltName 2>/dev/null \
		| grep -q "DNS:${expected}"; do
		sleep 2
		timeout=$((timeout + 2))
		echo -n '.'
		if [[ ${timeout} -gt 240 ]]; then
			echo
			echo "Waiting timeout has been reached. Secret ${secret} has no SAN ${expected}. Exiting..."
			exit 1
		fi
	done
	echo ".OK"
}
