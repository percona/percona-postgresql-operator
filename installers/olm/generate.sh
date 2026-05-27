#!/usr/bin/env bash

# shellcheck disable=SC2016,SC2155
set -euo pipefail

cd "${BASH_SOURCE[0]%/*}"

DISTRIBUTION="${1:?Distribution argument required (redhat|community)}"

repo_root="$(cd ../.. && pwd)"
bundle_directory="bundles/${DISTRIBUTION}"
project_directory="projects/${DISTRIBUTION}"
go_api_directory="$(cd ../../pkg/apis && pwd)"
icon_file="${repo_root}/kubernetes.svg"

bundle_package_name="percona-postgresql-operator"
bundle_project_name="percona-postgresql-operator"
bundle_filename="percona-postgresql-operator"
bundle_icon_mime="image/svg+xml"

icon_b64=""
disconnected="false"
csv_stem=""
related_images="[]"
skips=""
operator_image=""
distribution_images="{}"

log() {
	echo >&2 "[olm] $*"
}

abort() {
	echo >&2 "[olm] ERROR: $*"
	exit 1
}

require_env() {
	local name="$1"

	if [[ -z ${!name:-} ]]; then
		abort "${name} is required"
	fi
}

select_manifests() {
	local kind="$1"
	shift

	yq --slurp --yaml-roundtrip \
		--arg kind "${kind}" \
		'map(select(.kind == $kind))' \
		"$@"
}

require() {
	if [ $# -eq 1 ]; then
		command -v "$1" >/dev/null 2>&1 \
			|| abort "$1 not found in PATH"
	else
		"$@" >/dev/null 2>&1 \
			|| abort "Failed running: $*"
	fi
}

check_tools() {
	log "Checking required tools"

	require kubectl
	require operator-sdk
	require jq
	require bash -c "yq --help | grep -q -- '--yaml-roundtrip'"

	log "All tools available"
}

load_icon() {
	log "Loading icon from ${icon_file}"

	[[ -f ${icon_file} ]] \
		|| abort "OLM CSV requires an icon: add ${icon_file}"

	icon_b64="$(base64 <"${icon_file}" | tr -d '\n')"

	log "Icon loaded ($(stat -f%z "${icon_file}" 2>/dev/null || stat -c%s "${icon_file}") bytes)"
}

image_version() {
	printf '%s\n' "${1##*:}"
}

load_distribution_hooks() {
	local hook_file="distributions/${DISTRIBUTION}.sh"

	if [[ ! -f ${hook_file} ]]; then
		log "No distribution hooks for ${DISTRIBUTION}"
		return
	fi

	log "Loading distribution hooks from ${hook_file}"

	# shellcheck source=/dev/null
	source "${hook_file}"
}

prepare_distribution() {
	load_distribution_hooks

	if [[ ${DISTRIBUTION} == "redhat" ]]; then
		log "Loading Red Hat certified bundle metadata"
		require_env REDHAT_OPERATOR_IMAGE

		bundle_package_name="percona-postgresql-operator-certified"
		bundle_filename="percona-postgresql-operator-certified"
		disconnected="true"
		distribution_images="$(build_redhat_related_images)"
		operator_image="$(jq --raw-output '.operatorImage' <<<"${distribution_images}")"
		related_images="$(jq --compact-output '.relatedImages' <<<"${distribution_images}")"
		skips="$(prepare_redhat_csv_vars "${bundle_filename}")"

		log "Red Hat configuration completed"
	else
		require_env IMAGE
		operator_image="${IMAGE}"
	fi

	log "Operator image: ${operator_image}"
}

render_operator_manifests() {
	local operator_yamls

	log "Rendering operator manifests for ${DISTRIBUTION}"

	operator_yamls="$(kubectl kustomize "config/${DISTRIBUTION}")" \
		|| abort "Failed to kustomize config/${DISTRIBUTION}"

	operator_crds="$(select_manifests CustomResourceDefinition <(printf '%s\n' "${operator_yamls}"))" \
		|| abort "Failed to extract CRDs"
	operator_deployments="$(select_manifests Deployment <(printf '%s\n' "${operator_yamls}"))" \
		|| abort "Failed to extract Deployments"
	operator_accounts="$(select_manifests ServiceAccount ../../deploy/rbac.yaml)" \
		|| abort "Failed to extract ServiceAccounts"
	operator_roles="$(select_manifests Role ../../deploy/rbac.yaml)" \
		|| abort "Failed to extract Roles"
	operator_cluster_roles="$(select_manifests ClusterRole ../../deploy/cw-rbac.yaml)" \
		|| abort "Failed to extract ClusterRoles"

	log "Extracted manifests: CRDs, Deployments, ServiceAccounts, Roles, ClusterRoles"
}

create_sdk_workspace() {
	local crd_gvks

	log "Creating Operator SDK workspace for ${DISTRIBUTION}"

	rm -rf "${project_directory}"
	install -d "${project_directory}"

	(
		cd "${project_directory}"

		log "Initializing operator-sdk project"

		operator-sdk init \
			--fetch-deps='false' \
			--project-name="${bundle_project_name}" \
			|| abort "Failed to init operator-sdk"

		rm -f ./*.go go.*

		crd_gvks="$(
			yq \
				'map({
          group: .spec.group,
          kind: .spec.names.kind,
          version: .spec.versions[].name
        })' \
				<<<"${operator_crds}"
		)" || abort "Failed to extract CRD GVKs"

		yq --in-place --yaml-roundtrip \
			--argjson resources "${crd_gvks}" \
			'
        .multigroup = true
        | .resources = $resources
        | .
      ' \
			./PROJECT \
			|| abort "Failed to update PROJECT"

		ln -s "${go_api_directory}" .

		log "Generating kustomize manifests"

		operator-sdk generate kustomize manifests \
			--interactive='false' \
			|| abort "Failed to generate manifests"
	)

	log "SDK workspace created"
}

create_bundle_directory() {
	log "Creating OLM bundle directory ${bundle_directory}"

	rm -rf "${bundle_directory}"
	install -d \
		"${bundle_directory}/manifests" \
		"${bundle_directory}/metadata" \
		"${bundle_directory}/tests/scorecard"

	log "Bundle directory structure created"
}

render_scorecard_tests() {
	log "Rendering scorecard tests"

	kubectl kustomize "${project_directory}/config/scorecard" \
		>"${bundle_directory}/tests/scorecard/config.yaml" \
		|| abort "Failed to render scorecard tests"

	log "Scorecard tests rendered"
}

render_bundle_metadata() {
	log "Rendering bundle annotations for ${DISTRIBUTION}"

	require_env OPENSHIFT_VERSIONS

	yq --yaml-roundtrip \
		--arg distribution "${DISTRIBUTION}" \
		--arg package "${bundle_package_name}" \
		--arg openshift_supported_versions "${OPENSHIFT_VERSIONS}" \
		'
        .annotations["operators.operatorframework.io.bundle.package.v1"] = $package
        | .annotations["operators.operatorframework.io.bundle.channels.v1"] = "stable"
        | .annotations["operators.operatorframework.io.bundle.channel.default.v1"] = "stable"
        | .annotations["com.redhat.openshift.versions"] = $openshift_supported_versions
        | if $distribution == "community" then
            .annotations["org.opencontainers.image.authors"] = "info@percona.com"
            | .annotations["org.opencontainers.image.url"] = "https://percona.com"
            | .annotations["org.opencontainers.image.vendor"] = "Percona"
          else
            .
          end
      ' \
		<bundle.annotations.yaml \
		>"${bundle_directory}/metadata/annotations.yaml" \
		|| abort "Failed to render annotations"

	log "Bundle annotations written"
}

render_bundle_dockerfile() {
	local labels

	log "Rendering bundle Dockerfile labels"

	labels="$(
		yq --raw-output \
			'.annotations | to_entries | map(.key +"="+ (.value | tojson)) | join(" \\\n\t")' \
			<"${bundle_directory}/metadata/annotations.yaml"
	)" || abort "Failed to extract labels"

	export ANNOTATIONS="${labels}"

	envsubst '${ANNOTATIONS}' \
		<bundle.Dockerfile \
		>"${bundle_directory}/Dockerfile" \
		|| abort "Failed to render Dockerfile"

	log "Bundle Dockerfile rendered"
}

write_crd_manifests() {
	local crd_names
	local index
	local name

	log "Writing CRD manifests"

	crd_names="$(
		yq --raw-output \
			'to_entries[] | [.key, .value.metadata.name] | @tsv' \
			<<<"${operator_crds}"
	)" || abort "Failed to extract CRD names"

	while IFS=$'\t' read -r index name; do
		yq --yaml-roundtrip ".[${index}]" \
			<<<"${operator_crds}" \
			>"${bundle_directory}/manifests/${name}.crd.yaml" \
			|| abort "Failed to write CRD ${name}"
	done <<<"${crd_names}"

	log "CRD manifests written"
}

validate_single_manifest() {
	local value="$1"
	local name="$2"

	yq >/dev/null --exit-status 'length == 1' <<<"${value}" \
		|| abort "Expected exactly 1 ${name}, found $(yq 'length' <<<"${value}")"
}

validate_manifest_inputs() {
	log "Validating manifest inputs"

	validate_single_manifest "${operator_deployments}" "Deployment"
	validate_single_manifest "${operator_accounts}" "ServiceAccount"
	validate_single_manifest "${operator_roles}" "Role"
	validate_single_manifest "${operator_cluster_roles}" "ClusterRole"

	log "Manifest validation passed"
}

load_crd_descriptions() {
	log "Loading CRD descriptions"

	crd_descriptions="$(
		yq \
			'.spec.customresourcedefinitions.owned // []' \
			"${project_directory}/config/manifests/bases/${csv_stem}.clusterserviceversion.yaml"
	)" || abort "Failed to load existing CRD descriptions"

	if yq --exit-status 'length > 0' >/dev/null <<<"${crd_descriptions}"; then
		log "Using existing CRD descriptions from CSV"
		return
	fi

	log "Generating CRD descriptions from manifests"

	crd_descriptions="$(
		yq \
			'
        [
          .[] as $crd
          | $crd.spec.versions[]
          | {
              name: $crd.metadata.name,
              version: .name,
              kind: $crd.spec.names.kind,
              displayName: $crd.spec.names.kind,
              description: "\($crd.spec.names.kind) CRD"
            }
        ]
      ' \
			<<<"${operator_crds}"
	)" || abort "Failed to generate CRD descriptions"

	log "CRD descriptions generated"
}

build_crd_examples() {
	log "Building CRD examples"

	crd_examples="$(
		jq -s '.' \
			<(
				yq \
					'
            .spec.backups.volumeSnapshots = {
              "className": "default",
              "mode": "offline",
              "offlineConfig": {
                "checkpoint": {
                  "enabled": true,
                  "timeoutSeconds": 300
                }
              }
            }
            | .spec.backups.pgbackrest.restore = {
              "enabled": false,
              "repoName": "repo1"
            }
          ' \
					../../deploy/cr.yaml
			) \
			<(yq . ../../deploy/backup.yaml) \
			<(yq . ../../deploy/restore.yaml) \
			<(yq . ../../deploy/upgrade.yaml)
	)" || abort "Failed to build CRD examples"

	log "CRD examples built"
}

rewrite_crd_examples() {
	if [[ ${DISTRIBUTION} == "redhat" ]]; then
		log "Rewriting Red Hat alm-examples image references"

		rewrite_crd_examples_images crd_examples \
			"${distribution_images}" \
			|| abort "Failed to rewrite images"

		log "Image rewrite completed"
	fi
}

apply_operator_image_to_examples() {
	crd_examples="$(
		jq \
			--arg operator_image "${operator_image}" \
			'
        map(
          if .kind == "PerconaPGCluster" then
            .spec.initContainer = ((.spec.initContainer // {}) + { image: $operator_image })
          else
            .
          end
        )
      ' \
			<<<"${crd_examples}"
	)" || abort "Failed to apply operator image to alm-examples"
}

render_csv() {
	log "Rendering ClusterServiceVersion"

	csv_stem="$(
		yq --raw-output '.projectName' "${project_directory}/PROJECT"
	)" || abort "Failed to extract project name"

	log "CSV stem: ${csv_stem}"

	load_crd_descriptions
	build_crd_examples
	rewrite_crd_examples
	apply_operator_image_to_examples

	if [[ ${skips} == \[* ]]; then
		skips_arg="--argjson"
	else
		skips_arg="--arg"
	fi

	yq --yaml-roundtrip \
		--argjson deployment "$(yq 'first' <<<"${operator_deployments}")" \
		--arg account "$(yq --raw-output 'first | .metadata.name' <<<"${operator_accounts}")" \
		--argjson rules "$(yq 'first | .rules' <<<"${operator_roles}")" \
		--argjson cluster_rules "$(yq 'first | .rules' <<<"${operator_cluster_roles}")" \
		--argjson crds "${crd_descriptions}" \
		--arg examples "${crd_examples}" \
		--arg version "${VERSION}" \
		--arg description "$(<description.md)" \
		--arg icon "${icon_b64}" \
		--arg icon_mime "${bundle_icon_mime}" \
		--arg file_name "${bundle_filename}" \
		--arg disconnected "${disconnected}" \
		--arg operator_image "${operator_image}" \
		--argjson related_images "${related_images}" \
		"${skips_arg}" skips "${skips}" \
		--arg target_namespaces_field_path "metadata.annotations['olm.targetNamespaces']" \
		--arg timestamp "$(date -u '+%Y-%m-%dT%H:%M:%SZ')" \
		'
			.metadata.annotations["alm-examples"] = $examples
			| .metadata.annotations["containerImage"] = $operator_image
			| (
				if ($skips | type) == "array" then
					del(.metadata.annotations["olm.skipRange"])
					| .spec.skips = $skips

				elif ($skips | type) == "string" then
					del(.spec.skips)
					| .metadata.annotations["olm.skipRange"] = $skips

				else
					.
				end
			)
			| .metadata.annotations["createdAt"] = $timestamp
			| .metadata.annotations["features.operators.openshift.io/disconnected"] = $disconnected
			| .metadata.name = "\($file_name).v\($version)"
			| .spec.version = $version
			| .spec.customresourcedefinitions.owned = $crds
			| .spec.description = $description
			| .spec.icon = [
					{
						mediatype: $icon_mime,
						base64data: $icon
					}
				]
			| (
					if ($related_images | length) > 0 then
						.spec.relatedImages = $related_images
					else
						.
					end
				)
			| .spec.install.spec.permissions = [
					{
						serviceAccountName: $account,
						rules: $rules
					}
				]
			| .spec.install.spec.clusterPermissions = [
					{
						serviceAccountName: $account,
						rules: $cluster_rules
					}
				]
			| .spec.install.spec.deployments = [
					(
						$deployment
						| .spec.template.spec.containers[].image = $operator_image
						| (
								.spec.template.spec.containers[].env[]?
								| select(.valueFrom.fieldRef.fieldPath == "metadata.namespace")
								| .valueFrom.fieldRef.fieldPath
							) = $target_namespaces_field_path
						| {
								name: .metadata.name,
								spec
							}
					)
				]
			| .
		' \
		<bundle.csv.yaml \
		>"${bundle_directory}/manifests/${bundle_filename}.clusterserviceversion.yaml" \
		|| abort "Failed to render CSV"

	log "CSV rendered: ${bundle_directory}/manifests/${bundle_filename}.clusterserviceversion.yaml"
}

if ! declare -F apply_csv_overrides >/dev/null; then
	apply_csv_overrides() {
		log "No CSV overrides for ${DISTRIBUTION}"
	}
fi

print_bundle_tree() {
	if command -v tree >/dev/null 2>&1; then
		log "Bundle structure:"
		tree -C "${bundle_directory}"
		return
	fi

	log "Bundle structure (tree command not available):"
	find "${bundle_directory}" -type f | sort | sed 's|^|  |'
}

main() {
	log "======================================"
	log "OLM Bundle Generation for ${DISTRIBUTION}"
	log "======================================"
	log "Version: ${VERSION:-unknown}"
	log "Distribution: ${DISTRIBUTION}"
	log "======================================"

	require_env VERSION
	skips="<${VERSION}"

	check_tools
	load_icon
	prepare_distribution

	render_operator_manifests
	create_sdk_workspace
	create_bundle_directory

	render_scorecard_tests
	render_bundle_metadata
	render_bundle_dockerfile
	write_crd_manifests

	validate_manifest_inputs
	render_csv
	apply_csv_overrides
	print_bundle_tree

	log "======================================"
	log "Bundle generation completed successfully"
	log "======================================"
}

main "$@"
