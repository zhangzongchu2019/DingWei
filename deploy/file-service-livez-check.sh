#!/usr/bin/env bash
set -euo pipefail

service_name=${FS_LIVEZ_SERVICE_NAME:-file-service.service}
url=${FS_LIVEZ_URL:-http://127.0.0.1:8794/readyz}
failures_required=${FS_LIVEZ_FAILURES:-3}
state_file=${FS_LIVEZ_STATE_FILE:-/run/file-service-livez.failures}

case "$failures_required" in
  ''|*[!0-9]*|0) failures_required=3 ;;
esac

if curl --fail --silent --show-error --max-time 6 "$url" >/dev/null; then
  rm -f "$state_file"
  exit 0
fi

failures=0
if [ -r "$state_file" ]; then
  read -r failures < "$state_file" || failures=0
fi
case "$failures" in
  ''|*[!0-9]*) failures=0 ;;
esac
failures=$((failures + 1))
printf '%s\n' "$failures" > "$state_file"

if [ "$failures" -lt "$failures_required" ]; then
  exit 0
fi

rm -f "$state_file"
logger -t file-service-livez "readyz failed ${failures} consecutive checks; restarting ${service_name}"
systemctl restart "$service_name"
