#!/bin/sh
set -e

# When this marker exists (e.g. after a snapshot restore), skip all WAL recovery by
# exiting non-zero. Do not remove the file so every restore_command call is skipped.
if [ -f "${PGDATA}/skip-wal-recovery" ]; then
	exit 1
fi

exec "$@"
