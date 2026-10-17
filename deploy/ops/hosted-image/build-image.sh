#!/bin/sh
# Future isolated builder only. No default Engine, no fetch, no push, no credentials.
set -eu
: "${SOURCE_COMMIT:?exact clean source commit required}"
: "${HOSTED_BUILD_CONTEXT:?new prepared context path required}"
: "${HOSTED_BUILD_ENGINE:?approved isolated Docker unix socket required}"
: "${HOSTED_IMAGE_TAG:?local candidate tag required}"
case "$HOSTED_BUILD_ENGINE" in unix:///*) ;; *) echo 'isolated unix Engine required' >&2; exit 2;; esac
case "$HOSTED_BUILD_ENGINE" in unix:///var/run/docker.sock) echo 'default host Engine forbidden' >&2; exit 2;; esac
HERE=$(CDPATH= cd -- "$(dirname "$0")" && pwd)
python3 "$HERE/prepare_context.py" "$@" --commit "$SOURCE_COMMIT" --output "$HOSTED_BUILD_CONTEXT"
docker --host "$HOSTED_BUILD_ENGINE" build --platform=linux/amd64 --network=none --pull=false \
  --build-arg "SOURCE_COMMIT=$SOURCE_COMMIT" --tag "$HOSTED_IMAGE_TAG" "$HOSTED_BUILD_CONTEXT"
# Record local immutable image ID. Registry push/digest registration is separately authorized.
docker --host "$HOSTED_BUILD_ENGINE" image inspect --format '{{.Id}}' "$HOSTED_IMAGE_TAG"
