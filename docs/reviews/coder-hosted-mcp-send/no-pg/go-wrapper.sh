#!/bin/sh
# Task boundary: preserve all selected tests except the existing PostgreSQL integration.
set -eu
if [ "${1:-}" = test ]; then
  exec "$DW_TEST_REAL_GO" "$@" -skip '^TestDeviceClaimAcrossActualClientAndPostgres$'
fi
exec "$DW_TEST_REAL_GO" "$@"
