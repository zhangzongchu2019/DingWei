#!/bin/sh
set -eu
exec python3 specs/v0.2/check.py "$@"
