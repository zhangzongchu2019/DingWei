#!/usr/bin/env bash
set -euo pipefail

service_name=${WP_LIVEZ_SERVICE_NAME:-workpulse.service}
url=${WP_LIVEZ_URL:-http://127.0.0.1:8791/livez}
failures_required=${WP_LIVEZ_FAILURES:-3}
state_file=${WP_LIVEZ_STATE_FILE:-/run/workpulse-livez.failures}

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
logger -t workpulse-livez "livez failed ${failures} consecutive checks; restarting ${service_name}"
systemctl restart "$service_name"
