#!/bin/sh
# Future isolated builder only. No default Engine, no fetch, no push, no credentials.
set -eu
: "${SOURCE_COMMIT:?exact clean source commit required}"
: "${HOSTED_BUILD_CONTEXT:?new prepared context path required}"
: "${HOSTED_BUILD_ENGINE:?approved isolated Docker unix socket required}"
: "${HOSTED_IMAGE_TAG:?local candidate tag required}"
case "$HOSTED_BUILD_ENGINE" in unix:///*) ;; *) echo 'isolated unix Engine required' >&2; exit 2;; esac
case "$HOSTED_BUILD_ENGINE" in unix:///var/run/docker.sock) echo 'default host Engine forbidden' >&2; exit 2;; esac
# Reject aliases of the default Engine too; resolve metadata only, never connect.
python3 - "$HOSTED_BUILD_ENGINE" <<'PY_ENGINE'
import sys
import stat
from pathlib import Path
path = Path(sys.argv[1][len('unix://'):]).resolve()
if path == Path('/var/run/docker.sock').resolve() or path == Path('/run/docker.sock').resolve():
    raise SystemExit('default host Engine alias forbidden')
try:
    info = path.stat()
except OSError:
    raise SystemExit('isolated Engine socket missing or unreadable')
if not stat.S_ISSOCK(info.st_mode):
    raise SystemExit('isolated Engine path is not a socket')
PY_ENGINE
: "${HOSTED_BUILD_ENGINE_ID:?approved isolated daemon ID from independent inventory required}"
# Read-only daemon identity preflight; no context generation or build before identity match.
actual_engine_id=$(docker --host "$HOSTED_BUILD_ENGINE" info --format '{{.ID}}')
if [ -z "$actual_engine_id" ] || [ "$actual_engine_id" != "$HOSTED_BUILD_ENGINE_ID" ]; then
  echo 'isolated Engine identity mismatch' >&2
  exit 2
fi
HERE=$(CDPATH= cd -- "$(dirname "$0")" && pwd)
python3 "$HERE/prepare_context.py" "$@" --commit "$SOURCE_COMMIT" --output "$HOSTED_BUILD_CONTEXT"
docker --host "$HOSTED_BUILD_ENGINE" build --platform=linux/amd64 --network=none --pull=false \
  --build-arg "SOURCE_COMMIT=$SOURCE_COMMIT" --tag "$HOSTED_IMAGE_TAG" "$HOSTED_BUILD_CONTEXT"
# Record local immutable image ID. Registry push/digest registration is separately authorized.
docker --host "$HOSTED_BUILD_ENGINE" image inspect --format '{{.Id}}' "$HOSTED_IMAGE_TAG"
