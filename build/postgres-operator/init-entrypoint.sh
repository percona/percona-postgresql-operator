#!/usr/bin/env bash

set -o errexit
set -o xtrace

CRUNCHY_BINDIR="/opt/crunchy"

install -o "$(id -u)" -g "$(id -g)" -m 0755 -D "/usr/local/bin/pgbackrest" "${CRUNCHY_BINDIR}/bin/pgbackrest"
install -o "$(id -u)" -g "$(id -g)" -m 0755 -D "/usr/local/bin/postgres-entrypoint.sh" "${CRUNCHY_BINDIR}/bin/postgres-entrypoint.sh"
install -o "$(id -u)" -g "$(id -g)" -m 0755 -D "/usr/local/bin/postgres-liveness-check.sh" "${CRUNCHY_BINDIR}/bin/postgres-liveness-check.sh"
install -o "$(id -u)" -g "$(id -g)" -m 0755 -D "/usr/local/bin/postgres-readiness-check.sh" "${CRUNCHY_BINDIR}/bin/postgres-readiness-check.sh"
install -o "$(id -u)" -g "$(id -g)" -m 0755 -D "/usr/local/bin/relocate-extensions.sh" "${CRUNCHY_BINDIR}/bin/relocate-extensions.sh"
install -o "$(id -u)" -g "$(id -g)" -m 0755 -D "/usr/local/bin/restore-command-wrapper.sh" "${CRUNCHY_BINDIR}/bin/restore-command-wrapper.sh"

# Distribute the log collector assets (entrypoint + fluent-bit/logrotate config)
# into the shared bin volume so the log collector sidecars can run them without
# baking them into the fluent-bit image. The assets are only present when the
# operator image ships them, so guard the copy to avoid failing the init
# container (and thus the whole pod) on images without them.
if [ -d /logcollector ]; then
	cp -a "/logcollector" "${CRUNCHY_BINDIR}/"
	chown -R "$(id -u)":"$(id -g)" "${CRUNCHY_BINDIR}/logcollector"
	chmod -R 0755 "${CRUNCHY_BINDIR}/logcollector"
fi
