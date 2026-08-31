#!/bin/sh
set -eu
exec python3 specs/prototype/check.py "$@"
