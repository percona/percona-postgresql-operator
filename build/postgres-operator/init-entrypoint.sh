#!/usr/bin/env bash

set -o errexit
set -o xtrace

CRUNCHY_BINDIR="/opt/crunchy"

install -o "$(id -u)" -g "$(id -g)" -m 0755 -D "/usr/local/bin/pgbackrest" "${CRUNCHY_BINDIR}/bin/pgbackrest"
