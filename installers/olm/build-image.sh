#!/bin/bash

BUNDLE_REPO="${BUNDLE_REPO:-perconalab/percona-postgresql-operator}"

build_image() {
	local container="$1" directory="$2" distro="$3" version="$4"
	directory=$(cd "${directory}" && pwd)

    local tag="${version}-${distro}-bundle"
    local image="${BUNDLE_REPO}:${tag}"

    pushd ${directory}

    "${container}" build -t "${image}" .
    "${container}" push "${image}"

    popd
}

build_image "$@"