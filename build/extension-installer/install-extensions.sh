#!/bin/bash

set -e
set -o xtrace

IFS=',' read -ra extensions <<<"$INSTALL_EXTENSIONS"
for key in "${extensions[@]}"; do
	if [ -f ${PGDATA_EXTENSIONS}/${key}.installed ]; then
		echo "Extension ${key} already installed"
		continue
	fi

	echo "Installing extension: ${key}"
	/usr/local/bin/extension-installer \
		-type ${STORAGE_TYPE} \
		-region ${STORAGE_REGION} \
		-bucket ${STORAGE_BUCKET} \
		-extension-path ${PGDATA_EXTENSIONS} \
		-key ${key} \
		-install
done

for installed in ${PGDATA_EXTENSIONS}/*.installed; do
	filename=$(basename -- ${installed})
	key=${filename%.*}
	if [[ ${key} == "*" ]]; then
		continue
	fi

	if [[ ! ${extensions[*]} =~ ${key} ]]; then
		echo "Uninstalling extension: ${key}"
		/usr/local/bin/extension-installer \
			-type ${STORAGE_TYPE} \
			-region ${STORAGE_REGION} \
			-bucket ${STORAGE_BUCKET} \
			-extension-path ${PGDATA_EXTENSIONS} \
			-key ${key} \
			-uninstall
	fi
done