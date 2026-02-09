#!/bin/bash
set -e


if [[ "${DISABLE_WAL_ARCHIVE_RECOVERY:-}" == "1" ]]; then
    exit 1
fi

exec "$@"
