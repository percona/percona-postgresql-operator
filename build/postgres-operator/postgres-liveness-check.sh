#!/bin/bash

PATRONI_PORT=8008
PATRONI_HOST=localhost

# /pgdata/ is already mounted to the pg database container with rw permissions
recovery_file='/pgdata/sleep-forever'
if [ -f "${recovery_file}" ]; then
	set +o xtrace
	echo "The $recovery_file file is detected, node entered an infinite sleep"
	echo "If you want to exit from the infinite sleep, remove the $recovery_file file"
	exit 0
fi

response=$(curl -s -o /dev/null -w "%{http_code}" -k "https://${PATRONI_HOST}:${PATRONI_PORT}/liveness")

if [[ $response -eq 200 ]]; then
	exit 0
fi
exit 1
