#!/bin/bash
set -e

PG_EXTENSIONS_DIR=/usr/pgsql-${PG_VERSION}/share/extension
PG_LIB_DIR=/usr/pgsql-${PG_VERSION}/lib

PGDATA_EXTENSIONS_DIR=/pgdata/extension/${PG_VERSION}/usr/pgsql-${PG_VERSION}/share/extension
PGDATA_LIB_DIR=/pgdata/extension/${PG_VERSION}/usr/pgsql-${PG_VERSION}/lib

mkdir -p "${PGDATA_EXTENSIONS_DIR}"
mkdir -p "${PGDATA_LIB_DIR}"

cp -r "${PG_EXTENSIONS_DIR}"/* "${PGDATA_EXTENSIONS_DIR}/"
cp -r "${PG_LIB_DIR}"/* "${PGDATA_LIB_DIR}/"
