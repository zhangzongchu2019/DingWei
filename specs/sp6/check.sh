#!/bin/sh
set -eu
cd "$(dirname "$0")/../.."
python3 specs/sp6/check.py "$@"
