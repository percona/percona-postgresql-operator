#!/usr/bin/env bash

set -euo pipefail

release_versions_file=""

GKE_STABLE_URL="https://docs.cloud.google.com/kubernetes-engine/docs/release-notes-stable"
EKS_VERSIONS_URL="https://docs.aws.amazon.com/eks/latest/userguide/kubernetes-versions.html"
AKS_VERSIONS_URL="https://learn.microsoft.com/en-us/azure/aks/supported-kubernetes-versions"
KUBERNETES_STABLE_URL="https://dl.k8s.io/release/stable.txt"

usage() {
	echo "usage: $0 e2e-tests/release_versions" >&2
}

parse_args() {
	if [[ $# -ne 1 ]]; then
		usage
		exit 2
	fi

	release_versions_file="$1"
}

require_tools() {
	command -v curl >/dev/null || { echo "curl is required" >&2; exit 1; }
	command -v jq >/dev/null || { echo "jq is required" >&2; exit 1; }
}

fetch_url() {
	curl -fsSL "$1"
}

kubernetes_stable_version() {
	fetch_url "$KUBERNETES_STABLE_URL" | sed 's/^v//'
}

minor_min() {
	sort -Vu | head -n 1
}

minor_max() {
	sort -Vu | tail -n 1
}

minor_minus() {
	local minor="$1" diff="$2"
	echo "1.$((${minor#1.} - diff))"
}

gke_stable_minors() {
	fetch_url "$GKE_STABLE_URL" |
		awk '
			done { next }
			/The following versions are now available in the Stable channel:/ { found = 1; next }
			found && /<\/ul><\/li>/ { done = 1; found = 0; next }
			found { print }
		' |
		grep -Eo '1\.[0-9]+\.[0-9]+-gke\.[0-9]+' |
		sed -E 's/^(1\.[0-9]+).*/\1/' |
		sort -Vu
}

eks_standard_minors() {
	fetch_url "$EKS_VERSIONS_URL" |
		awk '
			/Available versions on standard support/ { found = 1 }
			/Available versions on extended support/ { found = 0 }
			found { print }
		' |
		grep -Eo '1\.[0-9]+' |
		sort -Vu
}

aks_supported_minors() {
	local current_month version ga_month

	current_month="$(date +%Y%m)"

	while IFS='|' read -r version ga_month; do
		if [[ "$ga_month" -le "$current_month" ]]; then
			echo "$version"
		fi
	done < <(
		fetch_url "$AKS_VERSIONS_URL" |
			sed -n '/<h2 id="kubernetes-versions"/,/<\/table>/p' |
			grep -E '<td>(1\.[0-9]+|[A-Z][a-z]{2} [0-9]{4})</td>' |
			sed -E 's/.*<td>([^<]+)<\/td>.*/\1/' |
			awk '
				/^1\.[0-9]+$/ { version = $0; column = 0; next }
				version != "" {
					column++
					if (column == 3) {
						print version "|" $0
						version = ""
					}
				}
			' |
			while IFS='|' read -r version ga_date; do
				printf '%s|%s\n' "$version" "$(year_month "$ga_date")"
			done
	) | sort -Vu | tail -n 3
}

month_number() {
	case "$1" in
		Jan) echo "01" ;;
		Feb) echo "02" ;;
		Mar) echo "03" ;;
		Apr) echo "04" ;;
		May) echo "05" ;;
		Jun) echo "06" ;;
		Jul) echo "07" ;;
		Aug) echo "08" ;;
		Sep) echo "09" ;;
		Oct) echo "10" ;;
		Nov) echo "11" ;;
		Dec) echo "12" ;;
	esac
}

year_month() {
	local month year

	month="$(awk '{ print $1 }' <<<"$1")"
	year="$(awk '{ print $2 }' <<<"$1")"
	printf '%s%s\n' "$year" "$(month_number "$month")"
}

openshift_minor_from_version() {
	local version="$1"
	[[ "$version" =~ ^4\.([0-9]+)\. ]] && echo "${BASH_REMATCH[1]}"
}

openshift_latest_for_minor() {
	local minor="$1"
	fetch_url "https://api.openshift.com/api/upgrades_info/v1/graph?channel=stable-4.${minor}&arch=amd64" |
		jq -r '.nodes[].version' |
		grep -E "^4\\.${minor}\\." |
		sort -V |
		tail -n 1
}

latest_openshift_minor() {
	local current_max_minor minor version

	current_max_minor="$(openshift_minor_from_version "$(grep '^OPENSHIFT_MAX=' "$release_versions_file" | cut -d= -f2)" || true)"
	for minor in $(seq $((current_max_minor + 2)) -1 "$current_max_minor"); do
		version="$(openshift_latest_for_minor "$minor" || true)"
		if [[ -n "$version" ]]; then
			echo "$minor"
			return
		fi
	done

	echo "$current_max_minor"
}

platform_value() {
	local name="$1" current_value="$2"
	local openshift_minor openshift_version

	case "$name" in
		GKE_MIN) gke_stable_minors | minor_min ;;
		GKE_MAX) gke_stable_minors | minor_max ;;
		EKS_MIN) eks_standard_minors | minor_min ;;
		EKS_MAX) eks_standard_minors | minor_max ;;
		AKS_MIN) aks_supported_minors | minor_min ;;
		AKS_MAX) aks_supported_minors | minor_max ;;
		MINIKUBE_MAX) kubernetes_stable_version ;;
		OPENSHIFT_MIN)
			openshift_minor="$(latest_openshift_minor)"
			openshift_version="$(openshift_latest_for_minor "$((openshift_minor - 3))" || true)"
			echo "${openshift_version:-$current_value}"
			;;
		OPENSHIFT_MAX)
			openshift_minor="$(latest_openshift_minor)"
			openshift_version="$(openshift_latest_for_minor "$openshift_minor" || true)"
			echo "${openshift_version:-$current_value}"
			;;
	esac
}

update_platform_versions_line() {
	local line="$1" output_file="$2"
	local name old_value new_value

	if [[ ! "$line" =~ ^(GKE_MIN|GKE_MAX|EKS_MIN|EKS_MAX|AKS_MIN|AKS_MAX|OPENSHIFT_MIN|OPENSHIFT_MAX|MINIKUBE_MAX)=(.*)$ ]]; then
		printf '%s\n' "$line" >>"$output_file"
		return
	fi

	name="${BASH_REMATCH[1]}"
	old_value="${BASH_REMATCH[2]}"
	new_value="$(platform_value "$name" "$old_value")"

	printf '%s=%s\n' "$name" "$new_value" >>"$output_file"
	if [[ "$old_value" != "$new_value" ]]; then
		echo "${name}: ${old_value} -> ${new_value}"
	fi
}

main() {
	local output_file line

	parse_args "$@"
	require_tools

	output_file="$(mktemp)"

	while IFS= read -r line || [[ -n "$line" ]]; do
		update_platform_versions_line "$line" "$output_file"
	done <"$release_versions_file"

	mv "$output_file" "$release_versions_file"
}

main "$@"
