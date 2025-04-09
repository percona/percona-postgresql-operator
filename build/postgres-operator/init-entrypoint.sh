#!/usr/bin/env bash

set -o errexit
set -o xtrace

CRUNCHY_BINDIR="/opt/crunchy"

install -o "$(id -u)" -g "$(id -g)" -m 0755 -D "/usr/local/bin/pgbackrest" "${CRUNCHY_BINDIR}/bin/pgbackrest"
install -o "$(id -u)" -g "$(id -g)" -m 0755 -D "/usr/local/bin/postgres-entrypoint.sh" "${CRUNCHY_BINDIR}/bin/postgres-entrypoint.sh"
install -o "$(id -u)" -g "$(id -g)" -m 0755 -D "/usr/local/bin/postgres-liveness-check.sh" "${CRUNCHY_BINDIR}/bin/postgres-liveness-check.sh"
install -o "$(id -u)" -g "$(id -g)" -m 0755 -D "/usr/local/bin/postgres-readiness-check.sh" "${CRUNCHY_BINDIR}/bin/postgres-readiness-check.sh"
