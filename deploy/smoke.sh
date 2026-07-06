#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
WORKDIR=$(CDPATH= cd -- "$SCRIPT_DIR/.." && pwd)
ENV_FILE=${ENV_FILE:-"$WORKDIR/.env"}
SERVICE_NAME=${SERVICE_NAME:-workpulse}

env_value() {
  key="$1"
  file="$2"
  if [ ! -f "$file" ]; then
    return 0
  fi
  line=$(grep -E "^${key}=" "$file" | tail -1 || true)
  [ -n "$line" ] || return 0
  value=${line#*=}
  # Strip inline comments only when preceded by whitespace, preserving values
  # such as CLI commands with flags. Do not execute or shell-expand .env.
  value=$(printf '%s' "$value" | sed -E 's/[[:space:]]+#.*$//')
  case "$value" in
    \"*\") value=${value#\"}; value=${value%\"} ;;
    \'*\') value=${value#\'}; value=${value%\'} ;;
  esac
  printf '%s' "$value"
}

addr=${WP_ADDR:-$(env_value WP_ADDR "$ENV_FILE")}
addr=${addr:-127.0.0.1:8791}
case "$addr" in
  :*) base="http://127.0.0.1${addr}" ;;
  http://*|https://*) base="$addr" ;;
  *) base="http://${addr}" ;;
esac

admin_user=${WP_ADMIN_USER:-$(env_value WP_ADMIN_USER "$ENV_FILE")}
admin_user=${admin_user:-admin}
admin_pass=${WP_ADMIN_INIT_PASSWORD:-$(env_value WP_ADMIN_INIT_PASSWORD "$ENV_FILE")}
if [ "${SMOKE_PARSE_ONLY:-0}" = "1" ]; then
  printf 'base=%s\nadmin_user=%s\nadmin_pass_set=%s\n' "$base" "$admin_user" "$(if [ -n "$admin_pass" ]; then echo true; else echo false; fi)"
  exit 0
fi
cookie=$(mktemp)
body=$(mktemp)
trap 'rm -f "$cookie" "$body"' EXIT

pass() { printf 'PASS %s\n' "$1"; }
fail() { printf 'FAIL %s\n' "$1" >&2; exit 1; }

code=$(curl -sS -o "$body" -w '%{http_code}' "$base/healthz") || fail "healthz curl"
[ "$code" = "200" ] || fail "healthz expected 200 got $code"
pass "healthz=200"

code=$(curl -sS -o "$body" -w '%{http_code}' "$base/admin/login") || fail "admin login page curl"
[ "$code" = "200" ] || fail "admin/login expected 200 got $code"
pass "admin/login=200"

code=$(curl -sS -o "$body" -w '%{http_code}' "$base/admin") || fail "admin redirect curl"
[ "$code" = "303" ] || [ "$code" = "302" ] || fail "admin expected 302/303 got $code"
pass "admin unauth redirect=$code"

if [ -n "$admin_pass" ]; then
  code=$(curl -sS -c "$cookie" -b "$cookie" -o "$body" -w '%{http_code}' \
    -d "username=${admin_user}&password=${admin_pass}" "$base/admin/login") || fail "admin login submit curl"
  [ "$code" = "303" ] || [ "$code" = "302" ] || fail "admin login submit expected 302/303 got $code"
  pass "admin login submit=$code"

  before=$(curl -sS -b "$cookie" "$base/admin" | sed -n 's/.*done: \([0-9][0-9]*\).*/\1/p' | head -1)
  before=${before:-0}
  msg_id="smoke-$(date +%s)"
  code=$(curl -sS -o "$body" -w '%{http_code}' -X POST "$base/webhook/unifiedrobot" \
    -H 'Content-Type: application/json' \
    -d "{\"msg_id\":\"${msg_id}\",\"chat_type\":\"personal\",\"open_id\":\"smoke_user\",\"sender_open_id\":\"smoke_user\",\"name\":\"Smoke User\",\"text\":\"hello smoke\"}") || fail "webhook curl"
  [ "$code" = "202" ] || fail "webhook expected 202 got $code"
  sleep 1
  admin_html=$(curl -sS -b "$cookie" "$base/admin")
  after=$(printf '%s' "$admin_html" | sed -n 's/.*done: \([0-9][0-9]*\).*/\1/p' | head -1)
  after=${after:-0}
  queued=$(printf '%s' "$admin_html" | sed -n 's/.*queued: \([0-9][0-9]*\).*/\1/p' | head -1)
  queued=${queued:-0}
  if [ "$after" -lt "$before" ]; then
    fail "message done count decreased before=$before after=$after"
  fi
  [ "$queued" = "0" ] || fail "message queued expected 0 got $queued"
  pass "webhook accepted and queue drained"
else
  echo "WARN WP_ADMIN_INIT_PASSWORD empty; skip authenticated webhook/admin stats smoke" >&2
fi

if command -v journalctl >/dev/null 2>&1 && command -v systemctl >/dev/null 2>&1 && systemctl list-units --full -all | grep -q "^${SERVICE_NAME}.service"; then
  if journalctl -u "$SERVICE_NAME" --no-pager -n 200 2>/dev/null | grep -q "scheduler evidence cron configured"; then
    pass "scheduler evidence cron log found"
  else
    echo "WARN scheduler evidence cron log not found in recent journal" >&2
  fi
else
  echo "WARN systemd journal unavailable; skip scheduler cron log check" >&2
fi

echo "ALL PASS"
