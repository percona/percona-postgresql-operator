#!/bin/bash

set -e
set -o xtrace

IFS=',' read -ra extensions <<<"$INSTALL_EXTENSIONS"

declare -a args=(
	-type "${STORAGE_TYPE}"
	-region "${STORAGE_REGION}"
	-bucket "${STORAGE_BUCKET}"
	-extension-path "${PGDATA_EXTENSIONS}"
)

if [[ -n $STORAGE_ENDPOINT ]]; then
	args+=(-endpoint "$STORAGE_ENDPOINT")
fi

if [[ ${STORAGE_DISABLE_SSL} == "true" ]]; then
	args+=(-disable-ssl)
fi

if [[ ${STORAGE_FORCE_PATH_STYLE} == "true" ]]; then
	args+=(-force-path-style)
fi

for installed in "${PGDATA_EXTENSIONS}"/*.installed; do
	filename=$(basename -- "${installed}")
	key=${filename%.*}
	if [[ ${key} == "*" ]]; then
		continue
	fi

	if [[ ! ${extensions[*]} =~ ${key} ]]; then
		echo "Uninstalling extension: ${key}"
		/usr/local/bin/extension-installer \
			"${args[@]}" \
			-key "${key}" \
			-uninstall
		rm -f "${installed}"
	fi
done

for key in "${extensions[@]}"; do
	# do not skip when the .installed marker exists: relocate-extensions.sh runs
	# before this script on every pod start and overwrites the files an extension
	# shares with the postgres image (e.g. pg_cron is bundled in the image now),
	# so the custom build must be reinstalled on top of them every time
	if [ -f "${PGDATA_EXTENSIONS}"/"${key}".installed ]; then
		echo "Extension ${key} marker found, reinstalling over the relocated image files"
	fi

	echo "Installing extension: ${key}"
	/usr/local/bin/extension-installer \
		"${args[@]}" \
		-key "${key}" \
		-install
done
