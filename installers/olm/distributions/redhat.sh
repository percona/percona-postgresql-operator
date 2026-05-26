#!/usr/bin/env bash

# shellcheck disable=SC2016,SC2155
set -euo pipefail

redhat_release="${VERSION}"
redhat_registry="registry.connect.redhat.com"
redhat_catalog_api="${REDHAT_CATALOG_API:-https://catalog.redhat.com/api/containers/v1}"
redhat_catalog_curl_timeout="${REDHAT_CATALOG_CURL_TIMEOUT:-20}"
redhat_operator_repository="percona/percona-postgresql-operator"
redhat_containers_repository="percona/percona-postgresql-operator-containers"
redhat_operator_tag="${REDHAT_OPERATOR_TAG:-${redhat_release}}"
redhat_upgrade_tag="${REDHAT_UPGRADE_TAG:-${redhat_release}-upgrade}"

# Images
redhat_operator_image=""
redhat_postgres_image=""
redhat_pgbouncer_image=""
redhat_pgbackrest_image=""
redhat_pmm_image=""
redhat_upgrade_image=""
redhat_related_images="[]"

required_vars=(
	VERSION
	repo_root
	bundle_directory
	bundle_filename
)

for var in "${required_vars[@]}"; do
	: "${!var:?Environment variable "${var}" is required}"
done

digest_key() {
	printf '%s' "$1" \
		| sed -E 's/[^[:alnum:]]+/_/g' \
		| tr '[:lower:]' '[:upper:]'
}

catalog_digest() {
	local repository="$1"
	local tag="$2"
	local digest

	log "Resolving Red Hat digest for ${redhat_registry}/${repository}:${tag}"

	digest="$(
		curl -fsSL \
			--connect-timeout 5 \
			--max-time "${redhat_catalog_curl_timeout}" \
			"${redhat_catalog_api}/repositories/registry/${redhat_registry}/repository/${repository}/tag/${tag}" \
			| jq -er '.docker_image_digest // .data.docker_image_digest // .data[0].docker_image_digest' 2>/dev/null
	)" || digest="$(
		curl -fsSL \
			--connect-timeout 5 \
			--max-time "${redhat_catalog_curl_timeout}" \
			"${redhat_catalog_api}/repositories/registry/${redhat_registry}/repository/${repository}/images?page_size=500" \
			| jq -er \
				--arg tag "${tag}" \
				'first(.data[] | select(any(.repositories[]?.tags[]?; .name == $tag)) | .docker_image_digest)' \
				2>/dev/null
	)" || digest="<DIGEST>"

	if [[ -z ${digest} || ${digest} == "null" ]]; then
		digest="<DIGEST>"
	fi

	if [[ ${digest} == "<DIGEST>" ]]; then
		log "Unable to resolve digest for ${redhat_registry}/${repository}:${tag}; using <DIGEST> placeholder"
	elif [[ ${digest} != sha256:* ]]; then
		digest="sha256:${digest#sha256:}"
	fi

	printf '%s\n' "${digest}"
}

image_ref() {
	local name="$1"
	local repository="$2"
	local tag="$3"
	local digest_var="REDHAT_IMAGE_DIGEST_$(digest_key "${name}")"
	local digest="${!digest_var:-}"

	if [[ -z ${digest} ]]; then
		digest="$(catalog_digest "${repository}" "${tag}")"
	fi

	if [[ -z ${digest} ]]; then
		abort "empty Red Hat image digest for ${redhat_registry}/${repository}:${tag}"
	fi

	if [[ ${digest} != "<DIGEST>" && ${digest} != sha256:* ]]; then
		digest="sha256:${digest#sha256:}"
	fi

	printf '%s/%s@%s\n' "${redhat_registry}" "${repository}" "${digest}"
}

release_component_tag() {
	printf '%s-%s-%s\n' "${redhat_release}" "$1" "$2"
}

add_related_image() {
	local name="$1"
	local repository="$2"
	local tag="$3"
	local image

	image="$(image_ref "${name}" "${repository}" "${tag}")"

	log "Related image ${name}: ${image}"

	case "${name}" in
		operator) redhat_operator_image="${image}" ;;
		"postgres-${current_pg_major}") redhat_postgres_image="${image}" ;;
		"postgis-${current_pg_major}") redhat_postgres_image="${image}" ;;
		pgbouncer) redhat_pgbouncer_image="${image}" ;;
		pgbackrest) redhat_pgbackrest_image="${image}" ;;
		pmm3) redhat_pmm_image="${image}" ;;
		pmm2) redhat_pmm_image="${image}" ;;
		pgupgrade) redhat_upgrade_image="${image}" ;;
	esac

	redhat_related_images="$(
		jq -c \
			--arg name "${name}" \
			--arg image "${image}" \
			'. + [{ name: $name, image: $image }]' \
			<<<"${redhat_related_images}"
	)"
}

build_redhat_related_images() {
	local repo_root="${repo_root}"
	local pg_major
	local image_var
	local pg_versions

	log "Building Red Hat related images from release versions"

	# shellcheck source=/dev/null
	source "${repo_root}/e2e-tests/release_versions"

	pg_versions="$(
		compgen -A variable IMAGE_POSTGRESQL \
			| sed 's/^IMAGE_POSTGRESQL//' \
			| sort -rn
	)"

	current_pg_major="$(yq --raw-output '.spec.postgresVersion' ../../deploy/cr.yaml)"

	for pg_major in ${pg_versions}; do
		image_var="IMAGE_POSTGRESQL${pg_major}"
		add_related_image \
			"postgres-${pg_major}" \
			"${redhat_containers_repository}" \
			"$(release_component_tag pg "$(image_version "${!image_var}")")"

		image_var="IMAGE_POSTGIS${pg_major}"
		add_related_image \
			"postgis-${pg_major}" \
			"${redhat_containers_repository}" \
			"$(release_component_tag postgis "$(image_version "${!image_var}")")"
	done

	add_related_image "pgbackrest" "${redhat_containers_repository}" \
		"$(release_component_tag pgbackrest "$(image_version "${IMAGE_BACKREST18}")")"

	add_related_image "pgbouncer" "${redhat_containers_repository}" \
		"$(release_component_tag pgbouncer "$(image_version "${IMAGE_PGBOUNCER18}")")"

	add_related_image "pmm3" "${redhat_containers_repository}" "${redhat_release}-pmm3"
	add_related_image "pmm2" "${redhat_containers_repository}" "${redhat_release}-pmm2"
	add_related_image "operator" "${redhat_operator_repository}" "${redhat_operator_tag}"
	add_related_image "pgupgrade" "${redhat_containers_repository}" "${redhat_upgrade_tag}"
}

apply_csv_overrides() {
	local bundle_directory="${bundle_directory}"
	local bundle_filename="${bundle_filename}"
	local csv_file="${bundle_directory}/manifests/${bundle_filename}.clusterserviceversion.yaml"
	local override_file="redhat.csv.overrides.yaml"

	log "Applying Red Hat certified CSV overrides"

	yq --in-place --yaml-roundtrip \
		'
      .metadata.annotations.certified = "true"
    ' \
		"${csv_file}"

	if [[ ! -f ${override_file} ]]; then
		log "No Red Hat CSV override file found, skipping"
		return
	fi

	log "Applying Red Hat CSV overrides from ${override_file}"

	yq eval-all \
		'select(fileIndex == 0) * select(fileIndex == 1)' \
		"${csv_file}" \
		"${override_file}" >"${csv_file}.tmp" \
		|| abort "Failed to merge Red Hat CSV overrides"

	mv "${csv_file}.tmp" "${csv_file}"

	log "Red Hat CSV overrides applied"
}

prepare_redhat_csv_vars() {
	local file_name="$1"

	redhat_skips="$(
		jq -nc \
			--arg file_name "${file_name}" \
			'
        [
          "2.5.0",
          "2.6.0",
          "2.6.1",
          "2.7.0",
          "2.8.0",
          "2.8.1",
          "2.8.2",
          "2.9.0"
        ]
        | map("\($file_name).v\(.)")
      '
	)"

	printf '%s\n' "${redhat_skips}"
}

rewrite_crd_examples_images() {
	local var_name="$1"
	local ref
	local rewritten

	ref="${!var_name}"

	rewritten="$(
		jq \
			--arg operator_image "${redhat_operator_image}" \
			--arg postgres_image "${redhat_postgres_image}" \
			--arg pgbouncer_image "${redhat_pgbouncer_image}" \
			--arg pgbackrest_image "${redhat_pgbackrest_image}" \
			--arg pmm_image "${redhat_pmm_image}" \
			--arg upgrade_image "${redhat_upgrade_image}" \
			'
        map(
          if .kind == "PerconaPGCluster" then
            .spec = (
              {
                crVersion: .spec.crVersion,
                initContainer: {
                  image: $operator_image
                }
              } + (
                .spec
                | del(
                    .crVersion,
                    .initContainer
                  )
              )
            )
            | .spec.image = $postgres_image
            | .spec.proxy.pgBouncer.image = $pgbouncer_image
            | .spec.backups.pgbackrest.image = $pgbackrest_image
            | .spec.pmm.image = $pmm_image

          elif .kind == "PerconaPGUpgrade" then
            .spec.image = $upgrade_image
            | .spec.toPostgresImage = $postgres_image
            | .spec.toPgBouncerImage = $pgbouncer_image
            | .spec.toPgBackRestImage = $pgbackrest_image

          else
            .
          end
        )
      ' \
			<<<"${ref}"
	)"

	printf -v "${var_name}" '%s' "${rewritten}"
}
