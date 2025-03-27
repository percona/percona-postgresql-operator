#!/bin/bash

export ROOT_REPO=${ROOT_REPO:-${PWD}}

export DEPLOY_DIR="${DEPLOY_DIR:-${ROOT_REPO}/deploy}"
export TESTS_DIR="${TESTS_DIR:-${ROOT_REPO}/e2e-tests}"
export TESTS_CONFIG_DIR="${TESTS_CONFIG_DIR:-${TESTS_DIR}/conf}"
export TEMP_DIR="/tmp/kuttl/pg/${test_name}"

export GIT_BRANCH=$(git rev-parse --abbrev-ref HEAD)
export VERSION=${VERSION:-$(echo "${GIT_BRANCH}" | sed -e 's^/^-^g; s^[.]^-^g;' | tr '[:upper:]' '[:lower:]')}

export IMAGE_BASE=${IMAGE_BASE:-"perconalab/percona-postgresql-operator"}
export IMAGE=${IMAGE:-"${IMAGE_BASE}:${VERSION}"}
export PG_VER="${PG_VER:-16}"
export IMAGE_PGBOUNCER=${IMAGE_PGBOUNCER:-"perconalab/percona-pgbouncer"}
export IMAGE_POSTGRESQL=${IMAGE_POSTGRESQL:-"${IMAGE_BASE}:main-ppg$PG_VER-postgres"}
export IMAGE_BACKREST=${IMAGE_BACKREST:-"${IMAGE_BASE}:main-ppg$PG_VER-pgbackrest"}
export IMAGE_UPGRADE=${IMAGE_UPGRADE:-"${IMAGE_BASE}:main-upgrade"}
export BUCKET=${BUCKET:-"pg-operator-testing"}
export PMM_SERVER_VERSION=${PMM_SERVER_VERSION:-"9.9.9"}
export IMAGE_PMM_CLIENT=${IMAGE_PMM_CLIENT:-"perconalab/pmm-client:dev-latest"}
export IMAGE_PMM_SERVER=${IMAGE_PMM_SERVER:-"perconalab/pmm-server:dev-latest"}
export PGOV1_TAG=${PGOV1_TAG:-"1.4.0"}
export PGOV1_VER=${PGOV1_VER:-"14"}

date=$(which gdate || which date)
sed=$(which gsed || which sed)

if command -v oc &>/dev/null; then
	if oc get projects; then
		export OPENSHIFT=4
	fi
fi
