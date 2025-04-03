#!/bin/bash

# /pgdata/ is already mounted to the pg database container with rw permissions
recovery_file='/pgdata/sleep-forever'
if [[ -f ${recovery_file} ]]; then
	set +o xtrace
	echo "The $recovery_file file is detected, node entered an infinite sleep"
	echo "If you want to exit from the infinite sleep, remove the $recovery_file file"
	while [ -f "${recovery_file}" ]; do
		sleep 3
	done
	exit 0
fi

exec "$@"
