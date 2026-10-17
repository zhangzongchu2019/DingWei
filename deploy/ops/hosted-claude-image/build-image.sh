#!/bin/sh
# Explicitly approved isolated Engine only; offline inputs, no fetch or push.
set -eu
: "${HOSTED_BUILD_ENGINE:?approved isolated Docker unix socket required}"
: "${HOSTED_BUILD_ENGINE_ID:?approved isolated daemon ID from independent inventory required}"
: "${HOSTED_IMAGE_TAG:?local candidate tag required}"
: "${CLAUDE_OFFLINE_LOCK:?reviewed offline Claude artifact lock required}"
: "${CLAUDE_OFFLINE_TAR:?reviewed offline Claude package required}"
: "${CLAUDE_NATIVE_TAR:?reviewed offline Claude artifact required}"
: "${CLAUDE_NODE_RUNTIME:?reviewed offline Node runtime required}"
: "${CLAUDE_BASE_CONTEXT:?verified H3 base context required}"
: "${CLAUDE_SOURCE_COMMIT:?exact reviewed Git source commit required}"
: "${HOSTED_BUILD_CONTEXT:?new absolute output context required}"
case "$HOSTED_BUILD_ENGINE" in unix:///*) ;; *) echo 'isolated unix Engine required' >&2; exit 2;; esac
# Metadata only: reject lexical/symlink aliases, require an existing socket, and
# use the same resolved endpoint for every subsequent Docker command.
engine_socket=$(python3 - "$HOSTED_BUILD_ENGINE" <<'PY_ENGINE'
import stat
import sys
from pathlib import Path
try:
    path = Path(sys.argv[1][len('unix://'):]).resolve()
    if path in (Path('/var/run/docker.sock').resolve(), Path('/run/docker.sock').resolve()):
        raise SystemExit('default host Engine alias forbidden')
    info = path.stat()
except (OSError, RuntimeError):
    raise SystemExit('isolated Engine socket missing or unreadable')
if not stat.S_ISSOCK(info.st_mode):
    raise SystemExit('isolated Engine path is not a socket')
print(path)
PY_ENGINE
) || exit 2
engine="unix://$engine_socket"
HERE=$(CDPATH= cd -- "$(dirname "$0")" && pwd)
REPO=$(CDPATH= cd -- "$HERE/../../.." && pwd)
# Preserve the packager's pre-Docker pollution refusal. No unchecked source tree
# or network input is handed to Docker, including its read-only identity probe.
python3 -B "$HERE/prepare_context.py" --repo "$REPO" --commit "$CLAUDE_SOURCE_COMMIT" --lock "$CLAUDE_OFFLINE_LOCK" --claude-tar "$CLAUDE_OFFLINE_TAR" --native-tar "$CLAUDE_NATIVE_TAR" --node-runtime "$CLAUDE_NODE_RUNTIME" --base-context "$CLAUDE_BASE_CONTEXT" --output "$HOSTED_BUILD_CONTEXT"
actual_engine_id=$(docker --host "$engine" info --format '{{.ID}}')
if [ -z "$actual_engine_id" ] || [ "$actual_engine_id" != "$HOSTED_BUILD_ENGINE_ID" ]; then
  echo 'isolated Engine identity mismatch' >&2
  exit 2
fi
docker --host "$engine" build --platform=linux/amd64 --network=none --pull=false \
  --build-arg "SOURCE_COMMIT=$CLAUDE_SOURCE_COMMIT" --tag "$HOSTED_IMAGE_TAG" "$HOSTED_BUILD_CONTEXT"
# Emit the local immutable ID for later pinning; no registry push/registration.
image_id=$(docker --host "$engine" image inspect --format '{{.Id}}' "$HOSTED_IMAGE_TAG")
case "$image_id" in sha256:*) ;; *) echo 'invalid inspected image ID' >&2; exit 2;; esac
image_hash=${image_id#sha256:}
case "$image_hash" in ''|*[!0-9a-f]*) echo 'invalid inspected image ID' >&2; exit 2;; esac
if [ "${#image_hash}" -ne 64 ]; then
  echo 'invalid inspected image ID' >&2
  exit 2
fi
printf '%s\n' "$image_id"
