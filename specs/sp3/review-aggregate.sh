#!/bin/sh
set -eu
cd "$(dirname "$0")/../.."
python3 specs/sp3/review-aggregate.py "$@"
