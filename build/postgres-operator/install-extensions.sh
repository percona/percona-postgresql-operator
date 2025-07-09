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

# Checking the STORAGE_DISABLE_SSL env for backwards compatibility before 2.8.0
if [[ -n $STORAGE_DISABLE_SSL ]]; then
	args+=(-disable-ssl "$STORAGE_DISABLE_SSL")
fi

# Checking the STORAGE_FORCE_PATH_STYLE env for backwards compatibility before 2.8.0
if [[ -n $STORAGE_FORCE_PATH_STYLE ]]; then
	args+=(-force-path-style "$STORAGE_FORCE_PATH_STYLE")
fi

for key in "${extensions[@]}"; do
	if [ -f "${PGDATA_EXTENSIONS}"/"${key}".installed ]; then
		echo "Extension ${key} already installed"
		continue
	fi

	echo "Installing extension: ${key}"
	/usr/local/bin/extension-installer \
		"${args[@]}" \
		-key "${key}" \
		-install
done

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
