#!/usr/bin/env bash
# shellcheck disable=SC2016
# vim: set noexpandtab :
set -eu

DISTRIBUTION="$1"

cd "${BASH_SOURCE[0]%/*}"

bundle_directory="bundles/${DISTRIBUTION}"
project_directory="projects/${DISTRIBUTION}"
go_api_directory=$(cd ../../pkg/apis && pwd)

# The 'operators.operatorframework.io.bundle.package.v1' package name for each
# bundle (updated for the 'certified' and 'marketplace' bundles).
package_name='percona-postgresql-operator'

# The project name used by operator-sdk for initial bundle generation.
project_name='percona-postgresql-operator'

# The prefix for the 'clusterserviceversion.yaml' file.
# Per OLM guidance, the filename for the clusterserviceversion.yaml must be prefixed
# with the Operator's package name for the 'redhat' and 'marketplace' bundles.
# https://github.com/redhat-openshift-ecosystem/certification-releases/blob/main/4.9/ga/troubleshooting.md#get-supported-versions
file_name='percona-postgresql-operator'

operator_yamls=$(kubectl kustomize "config/${DISTRIBUTION}")
operator_crds=$(yq <<<"${operator_yamls}" --slurp --yaml-roundtrip 'map(select(.kind == "CustomResourceDefinition"))')
operator_deployments=$(yq <<<"${operator_yamls}" --slurp --yaml-roundtrip 'map(select(.kind == "Deployment"))')
operator_accounts=$(yq <<<"${operator_yamls}" --slurp --yaml-roundtrip 'map(select(.kind == "ServiceAccount"))')
operator_roles=$(yq <<<"${operator_yamls}" --slurp --yaml-roundtrip 'map(select(.kind == "Role"))')

# Recreate the Operator SDK project.
[ ! -d "${project_directory}" ] || rm -r "${project_directory}"
install -d "${project_directory}"
(
	cd "${project_directory}"
	operator-sdk init --fetch-deps='false' --project-name=${project_name}
	rm ./*.go go.*

	# Generate CRD descriptions from Go markers.
	# https://sdk.operatorframework.io/docs/building-operators/golang/references/markers/
	crd_gvks=$(yq <<<"${operator_crds}" 'map({
		group: .spec.group, kind: .spec.names.kind, version: .spec.versions[].name
	})')
	yq --in-place --yaml-roundtrip --argjson resources "${crd_gvks}" \
		'.multigroup = true | .resources = $resources | .' ./PROJECT

	ln -s "${go_api_directory}" .
	operator-sdk generate kustomize manifests --interactive='false'
)

# Recreate the OLM bundle.
[ ! -d "${bundle_directory}" ] || rm -r "${bundle_directory}"
install -d \
	"${bundle_directory}/manifests" \
	"${bundle_directory}/metadata" \
	"${bundle_directory}/tests/scorecard"

# `echo "${operator_yamls}" | operator-sdk generate bundle` includes the ServiceAccount which cannot
# be upgraded: https://github.com/operator-framework/operator-lifecycle-manager/issues/2193

# Include Operator SDK scorecard tests.
# https://sdk.operatorframework.io/docs/advanced-topics/scorecard/scorecard/
kubectl kustomize "${project_directory}/config/scorecard" \
	>"${bundle_directory}/tests/scorecard/config.yaml"

# Render bundle annotations and strip comments.
# Per Red Hat we should not include the org.opencontainers annotations in the
# 'redhat' & 'marketplace' annotations.yaml file, so only add them for 'community'.
# - https://coreos.slack.com/team/UP1LZCC1Y
if [ ${DISTRIBUTION} == 'community' ]; then
	yq --yaml-roundtrip \
		--arg package "${package_name}" \
		--arg package_channel "${PACKAGE_CHANNEL}" \
		--arg openshift_supported_versions "${OPENSHIFT_VERSIONS}" \
		'
	.annotations["operators.operatorframework.io.bundle.package.v1"] = $package |
	.annotations["org.opencontainers.image.authors"] = "info@percona.com" |
	.annotations["org.opencontainers.image.url"] = "https://percona.com" |
	.annotations["org.opencontainers.image.vendor"] = "Percona" |
	.annotations["operators.operatorframework.io.bundle.channels.v1"] = $package_channel |
	.annotations["operators.operatorframework.io.bundle.channel.default.v1"] = $package_channel |
	.annotations["com.redhat.openshift.versions"] = $openshift_supported_versions |
.' <bundle.annotations.yaml >"${bundle_directory}/metadata/annotations.yaml"
else
	yq --yaml-roundtrip \
		--arg package "${package_name}" \
		--arg package_channel "${PACKAGE_CHANNEL}" \
		--arg openshift_supported_versions "${OPENSHIFT_VERSIONS}" \
		'
	.annotations["operators.operatorframework.io.bundle.package.v1"] = $package |
	.annotations["operators.operatorframework.io.bundle.channels.v1"] = $package_channel |
	.annotations["operators.operatorframework.io.bundle.channel.default.v1"] = $package_channel |
	.annotations["com.redhat.openshift.versions"] = $openshift_supported_versions |
.' <bundle.annotations.yaml >"${bundle_directory}/metadata/annotations.yaml"
fi

# Copy annotations into Dockerfile LABELs.
labels=$(yq --raw-output \
	'.annotations | to_entries | map(.key +"="+ (.value | tojson)) | join(" \\\n\t")' <"${bundle_directory}/metadata/annotations.yaml")
ANNOTATIONS="${labels}" envsubst '$ANNOTATIONS' <bundle.Dockerfile >"${bundle_directory}/Dockerfile"

# Include CRDs as manifests.
crd_names=$(yq --raw-output 'to_entries[] | [.key, .value.metadata.name] | @tsv' <<<"${operator_crds}")
while IFS=$'\t' read -r index name; do
	yq --yaml-roundtrip ".[${index}]" <<<"${operator_crds}" >"${bundle_directory}/manifests/${name}.crd.yaml"
done <<<"${crd_names}"

abort() {
	echo >&2 "$@"
	exit 1
}
dump() { yq --color-output; }

yq >/dev/null <<<"${operator_deployments}" --exit-status 'length == 1' \
	|| abort "too many deployments!" $'\n'"$(dump <<<"${operator_deployments}")"

yq >/dev/null <<<"${operator_accounts}" --exit-status 'length == 1' \
	|| abort "too many service accounts!" $'\n'"$(dump <<<"${operator_accounts}")"

yq >/dev/null <<<"${operator_roles}" --exit-status 'length == 1' \
	|| abort "too many roles!" $'\n'"$(dump <<<"${operator_roles}")"

# Render bundle CSV and strip comments.

csv_stem=$(yq --raw-output '.projectName' "${project_directory}/PROJECT")

crd_descriptions=$(yq '.spec.customresourcedefinitions.owned' \
	"${project_directory}/config/manifests/bases/${csv_stem}.clusterserviceversion.yaml")

crd_gvks=$(yq <<<"${operator_crds}" 'map({
	group: .spec.group, kind: .spec.names.kind, version: .spec.versions[].name
} | {
	apiVersion: "\(.group)/\(.version)", kind
})')
crd_examples=$(yq <<<"${operator_yamls}" --slurp --argjson gvks "${crd_gvks}" 'map(select(
	IN({ apiVersion, kind }; $gvks | .[])
))')

yq --yaml-roundtrip \
	--argjson deployment "$(yq <<<"${operator_deployments}" 'first')" \
	--argjson account "$(yq <<<"${operator_accounts}" 'first | .metadata.name')" \
	--argjson rules "$(yq <<<"${operator_roles}" 'first | .rules')" \
	--argjson crds "${crd_descriptions}" \
	--arg examples "${crd_examples}" \
	--arg version "${VERSION}" \
	--arg minKubeVer "${MIN_KUBE_VERSION}" \
	--arg description "$(<description.md)" \
	--arg icon "$(base64 -i ../icon.png | tr -d '\n')" \
	--arg stem "${csv_stem}" \
	--arg timestamp "$(date -u '+%FT%T%Z')" \
	'
	.metadata.annotations["alm-examples"] = $examples |
	.metadata.annotations["containerImage"] = ($deployment.spec.template.spec.containers[0].image) |
	.metadata.annotations["olm.skipRange"] = ">=2.0.0 <v\($version)" |
	.metadata.annotations["createdAt"] = $timestamp |

	.metadata.name = "\($stem).v\($version)" |
	.spec.version = $version |
	.spec.minKubeVersion = $minKubeVer |

	.spec.customresourcedefinitions.owned = $crds |
	.spec.description = $description |
	.spec.icon = [{ mediatype: "image/png", base64data: $icon }] |

	.spec.install.spec.permissions = [{ serviceAccountName: $account, rules: $rules }] |
	.spec.install.spec.deployments = [( $deployment | { name: .metadata.name, spec } )] |
.' <bundle.csv.yaml >"${bundle_directory}/manifests/${file_name}.clusterserviceversion.yaml"

case "${DISTRIBUTION}" in
	'redhat')
		# https://redhat-connect.gitbook.io/certified-operator-guide/appendix/what-if-ive-already-published-a-community-operator
		yq --in-place --yaml-roundtrip \
			'
			.metadata.annotations.certified = "true" |
			.metadata.annotations["containerImage"] = "registry.connect.redhat.com/percona/percona-postgresql-operator@sha256:<update_operator_SHA_value>" |
			.metadata.annotations["containerImage"] = "registry.connect.redhat.com/percona/percona-postgresql-operator@sha256:<update_operator_SHA_value>" |
		.' \
			"${bundle_directory}/manifests/${file_name}.clusterserviceversion.yaml"
		;;
	'marketplace')
		# Annotations needed when targeting Red Hat Marketplace
		# https://github.com/redhat-openshift-ecosystem/certification-releases/blob/main/4.9/ga/ci-pipeline.md#bundle-structure
		yq --in-place --yaml-roundtrip \
			--arg package_url "https://marketplace.redhat.com/en-us/operators/${file_name}" \
			'
				.metadata.annotations["containerImage"] = "registry.connect.redhat.com/percona/percona-postgresql-operator@sha256:<update_operator_SHA_value>" |
				.metadata.annotations["marketplace.openshift.io/remote-workflow"] =
						"\($package_url)/pricing?utm_source=openshift_console" |
				.metadata.annotations["marketplace.openshift.io/support-workflow"] =
						"\($package_url)/support?utm_source=openshift_console" |
		.' \
			"${bundle_directory}/manifests/${file_name}.clusterserviceversion.yaml"
		;;
esac

if >/dev/null command -v tree; then tree -C "${bundle_directory}"; fi
