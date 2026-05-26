#!/usr/bin/env bash

set -euo pipefail

operator_version=""
release_versions_file=""

usage() {
	echo "usage: $0 [--operator-version VERSION] e2e-tests/release_versions" >&2
}

parse_args() {
	while [[ $# -gt 0 ]]; do
		case "$1" in
			--operator-version)
				operator_version="$2"
				shift 2
				;;
			-h|--help)
				usage
				exit 0
				;;
			-*)
				echo "unknown option: $1" >&2
				usage
				exit 2
				;;
			*)
				release_versions_file="$1"
				shift
				;;
		esac
	done

	if [[ -z "$release_versions_file" ]]; then
		usage
		exit 2
	fi
}

require_tools() {
	command -v curl >/dev/null || { echo "curl is required" >&2; exit 1; }
	command -v jq >/dev/null || { echo "jq is required" >&2; exit 1; }
}

repo_for() {
	case "$1" in
		IMAGE_OPERATOR) echo "percona/percona-postgresql-operator" ;;
		IMAGE_POSTGRESQL*) echo "percona/percona-distribution-postgresql" ;;
		IMAGE_PGBOUNCER*) echo "percona/percona-pgbouncer" ;;
		IMAGE_POSTGIS*) echo "percona/percona-distribution-postgresql-with-postgis" ;;
		IMAGE_BACKREST*) echo "percona/percona-pgbackrest" ;;
		IMAGE_UPGRADE) echo "percona/percona-distribution-postgresql-upgrade" ;;
		IMAGE_PMM_CLIENT|IMAGE_PMM3_CLIENT) echo "percona/pmm-client" ;;
		IMAGE_PMM_SERVER|IMAGE_PMM3_SERVER) echo "percona/pmm-server" ;;
		*) echo "$2" ;;
	esac
}

docker_tags() {
	local repo="$1" url payload
	url="https://hub.docker.com/v2/repositories/${repo#docker.io/}/tags?page_size=100"

	while [[ -n "$url" ]]; do
		payload="$(curl -fsSL "$url")"
		jq -r '.results[].name' <<<"$payload"
		url="$(jq -r '.next // ""' <<<"$payload")"
	done
}

latest_tag() {
	local repo="$1" pattern="$2" current_tag="$3"
	local tag

	tag="$(
		docker_tags "$repo" |
			grep -Ev -- '-(amd64|arm64|arm|ppc64le|s390x)$' |
			grep -E "$pattern" |
			sort -V |
			tail -n 1 || true
	)"

	if [[ -z "$tag" ]]; then
		echo "warning: no tag matching ${pattern} found for ${repo}" >&2
		echo "$current_tag"
		return
	fi

	echo "$tag"
}

major_from_tag() {
	[[ "$1" =~ ^([0-9]+)\. ]] && echo "${BASH_REMATCH[1]}"
}

pg_major_from_name() {
	[[ "$1" =~ ([0-9]+)$ ]] && echo "${BASH_REMATCH[1]}"
}

numeric_major_pattern() {
	local tag="$1" major
	major="$(major_from_tag "$tag" || true)"

	if [[ -n "$major" ]]; then
		echo "^${major}\\.[0-9]+(\\.[0-9]+)?(-[0-9]+)?$"
	else
		echo '^[0-9]+\.[0-9]+(\.[0-9]+)?(-[0-9]+)?$'
	fi
}

pg_pattern() {
	local name="$1" major
	major="$(pg_major_from_name "$name")"
	echo "^${major}\\.[0-9]+-[0-9]+$"
}

latest_pg_version() {
	local major="$1" tag
	tag="$(latest_tag "percona/percona-distribution-postgresql" "^${major}\\.[0-9]+-[0-9]+$" "")"
	grep -Eo '^[0-9]+\.[0-9]+' <<<"$tag"
}

upgrade_tag() {
	local current_tag="$1"
	local versions="" major pattern

	for major in $(grep -Eo '^IMAGE_POSTGRESQL[0-9]+' "$release_versions_file" | grep -Eo '[0-9]+' | sort -rn); do
		versions="${versions:+${versions}-}$(latest_pg_version "$major")"
	done

	pattern="^${versions//./\\.}-[0-9]+$"
	latest_tag "percona/percona-distribution-postgresql-upgrade" "$pattern" "$current_tag"
}

new_tag_for() {
	local name="$1" repo="$2" tag="$3"

	case "$name" in
		IMAGE_OPERATOR)
			[[ "$operator_version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] && echo "$operator_version" || echo "$tag"
			;;
		IMAGE_UPGRADE)
			upgrade_tag "$tag"
			;;
		IMAGE_POSTGRESQL*|IMAGE_POSTGIS*)
			latest_tag "$repo" "$(pg_pattern "$name")" "$tag"
			;;
		IMAGE_PMM3_CLIENT|IMAGE_PMM3_SERVER)
			latest_tag "$repo" '^3\.[0-9]+(\.[0-9]+)?(-[0-9]+)?$' "$tag"
			;;
		IMAGE_PMM_CLIENT|IMAGE_PMM_SERVER)
			latest_tag "$repo" '^2\.[0-9]+(\.[0-9]+)?(-[0-9]+)?$' "$tag"
			;;
		IMAGE_*)
			latest_tag "$repo" "$(numeric_major_pattern "$tag")" "$tag"
			;;
	esac
}

update_release_versions_line() {
	local line="$1" output_file="$2"
	local name old_repo old_tag suffix repo new_tag old_value new_value

	if [[ ! "$line" =~ ^(IMAGE_[A-Z0-9_]+)=([^:#[:space:]]+):([^#[:space:]]+)(.*)$ ]]; then
		printf '%s\n' "$line" >>"$output_file"
		return
	fi

	name="${BASH_REMATCH[1]}"
	old_repo="${BASH_REMATCH[2]}"
	old_tag="${BASH_REMATCH[3]}"
	suffix="${BASH_REMATCH[4]}"

	repo="$(repo_for "$name" "$old_repo")"
	new_tag="$(new_tag_for "$name" "$repo" "$old_tag")"

	printf '%s=%s:%s%s\n' "$name" "$repo" "$new_tag" "$suffix" >>"$output_file"

	if [[ "${old_repo}:${old_tag}" != "${repo}:${new_tag}" ]]; then
		echo "${name}: ${old_repo}:${old_tag} -> ${repo}:${new_tag}"
	fi
}

main() {
	local output_file line

	parse_args "$@"
	require_tools

	output_file="$(mktemp)"

	while IFS= read -r line || [[ -n "$line" ]]; do
		update_release_versions_line "$line" "$output_file"
	done <"$release_versions_file"

	mv "$output_file" "$release_versions_file"
}

main "$@"
