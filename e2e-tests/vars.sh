#!/bin/bash

export ROOT_REPO=${ROOT_REPO:-${PWD}}

export DEPLOY_DIR="${DEPLOY_DIR:-${ROOT_REPO}/deploy}"
export TESTS_DIR="${TESTS_DIR:-${ROOT_REPO}/e2e-tests}"
export TESTS_CONFIG_DIR="${TESTS_CONFIG_DIR:-${TESTS_DIR}/conf}"
# shellcheck disable=SC2154
export TEMP_DIR="/tmp/kuttl/pg/${test_name}"

# shellcheck disable=SC2155
export GIT_BRANCH=$(git rev-parse --abbrev-ref HEAD)
export VERSION=${VERSION:-$(echo "${GIT_BRANCH}" | sed -e 's^/^-^g; s^[.]^-^g;' | tr '[:upper:]' '[:lower:]')}

if command -v oc &>/dev/null; then
	if oc get projects; then
		export OPENSHIFT=4
	fi
fi

export IMAGE_BASE=${IMAGE_BASE:-"perconalab/percona-postgresql-operator"}
export IMAGE=${IMAGE:-"${IMAGE_BASE}:${VERSION}"}
export PG_VER="${PG_VER:-17}"
export IMAGE_PGBOUNCER=${IMAGE_PGBOUNCER:-"${IMAGE_BASE}:main-pgbouncer$PG_VER"}
export IMAGE_POSTGRESQL=${IMAGE_POSTGRESQL:-"${IMAGE_BASE}:main-ppg$PG_VER-postgres"}
export IMAGE_BACKREST=${IMAGE_BACKREST:-"${IMAGE_BASE}:main-pgbackrest$PG_VER"}
export IMAGE_UPGRADE=${IMAGE_UPGRADE:-"${IMAGE_BASE}:main-upgrade"}
export BUCKET=${BUCKET:-"pg-operator-testing"}
export PMM_SERVER_VERSION=${PMM_SERVER_VERSION:-"9.9.9"}
export IMAGE_PMM_CLIENT=${IMAGE_PMM_CLIENT:-"perconalab/pmm-client:dev-latest"}
export IMAGE_PMM_SERVER=${IMAGE_PMM_SERVER:-"perconalab/pmm-server:dev-latest"}
export IMAGE_PMM3_CLIENT=${IMAGE_PMM3_CLIENT:-"perconalab/pmm-client:3-dev-latest"}
export IMAGE_PMM3_SERVER=${IMAGE_PMM3_SERVER:-"perconalab/pmm-server:3-dev-latest"}
export PGOV1_TAG=${PGOV1_TAG:-"1.4.0"}
export PGOV1_VER=${PGOV1_VER:-"14"}

if [[ $OPENSHIFT ]]; then
	REGISTRY='docker.io/'
	echo "Appending 'docker.io/' to image variables for OpenShift..."

	for var in $(printenv | grep -E '^IMAGE' | awk -F'=' '{print $1}'); do
		var_value=$(eval "echo \$$var")
		new_value="${REGISTRY}${var_value}"
		export "$var=$new_value"
		echo "$var=$new_value"
	done
fi

# shellcheck disable=SC2034
date=$(which gdate || which date)
# shellcheck disable=SC2034
sed=$(which gsed || which sed)

