#!/bin/bash

PATRONI_PORT=8008
PATRONI_HOST=localhost

response=$(curl -s -o /dev/null -w "%{http_code}" -k "https://${PATRONI_HOST}:${PATRONI_PORT}/readiness")

if [[ $response -eq 200 ]]; then
	exit 0
fi
exit 1
