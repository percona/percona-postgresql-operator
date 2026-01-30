#!/bin/bash
set -e

if [[ -f $PGDATA/restored-from-snapshot ]]; then
    rm -f $PGDATA/restored-from-snapshot
    exit 1
fi

pgbackrest --stanza=db archive-get %f "%p"