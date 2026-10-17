#!/bin/sh
# Future isolated builder only. It intentionally refuses until a reviewed,
# hash-pinned offline Claude artifact and context packager exist.
set -eu
: "${HOSTED_BUILD_ENGINE:?approved isolated Docker unix socket required}"
: "${CLAUDE_OFFLINE_LOCK:?reviewed offline Claude artifact lock required}"
: "${CLAUDE_OFFLINE_TAR:?reviewed offline Claude package required}"
: "${CLAUDE_NATIVE_TAR:?reviewed offline Claude artifact required}"
: "${CLAUDE_NODE_RUNTIME:?reviewed offline Node runtime required}"
: "${CLAUDE_BASE_CONTEXT:?verified H3 base context required}"
: "${HOSTED_BUILD_CONTEXT:?new absolute output context required}"
case "$HOSTED_BUILD_ENGINE" in unix:///*) ;; *) echo 'isolated unix Engine required' >&2; exit 2;; esac
case "$HOSTED_BUILD_ENGINE" in unix:///var/run/docker.sock|unix:///run/docker.sock)
  echo 'default host Engine forbidden' >&2; exit 2 ;;
esac
HERE=$(CDPATH= cd -- "$(dirname "$0")" && pwd)
python3 "$HERE/prepare_context.py" --lock "$CLAUDE_OFFLINE_LOCK" --claude-tar "$CLAUDE_OFFLINE_TAR" --native-tar "$CLAUDE_NATIVE_TAR" --node-runtime "$CLAUDE_NODE_RUNTIME" --base-context "$CLAUDE_BASE_CONTEXT" --output "$HOSTED_BUILD_CONTEXT"
echo 'Claude context packager is not approved; refusing before Docker access' >&2
exit 2
