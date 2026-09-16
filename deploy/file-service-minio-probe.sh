#!/usr/bin/env bash
set -euo pipefail
umask 077

: "${FS_INTERNAL_ISSUER_TOKEN:?FS_INTERNAL_ISSUER_TOKEN is required}"
internal_base=${FS_PROBE_INTERNAL_BASE:-http://127.0.0.1:8794}
public_base=${FS_PROBE_PUBLIC_BASE:-http://127.0.0.1:8793}
owner=${FS_PROBE_OWNER:-deployment-probe}
session=${FS_PROBE_SESSION:-deployment-probe}

tmp=$(mktemp -d)
trap 'rm -rf -- "$tmp"' EXIT
body="dingwei-file-service-minio-probe-$(date -u +%s)-$$"
printf '%s' "$body" >"$tmp/body"
size=$(wc -c <"$tmp/body" | tr -d ' ')
sum=$(sha256sum "$tmp/body" | cut -d' ' -f1)

python3 - "$owner" "$session" "$size" >"$tmp/issue-upload.json" <<'PY'
import json, sys
print(json.dumps({"owner_key": sys.argv[1], "session_name": sys.argv[2],
                  "max_bytes": int(sys.argv[3]), "ttl_seconds": 60}))
PY
curl --fail --silent --show-error \
  -H "Authorization: Bearer ${FS_INTERNAL_ISSUER_TOKEN}" \
  -H 'Content-Type: application/json' --data-binary @"$tmp/issue-upload.json" \
  "$internal_base/internal/files/issue/upload" >"$tmp/upload-ticket.json"
upload_token=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["token"])' "$tmp/upload-ticket.json")

curl --fail --silent --show-error \
  -H "Authorization: Bearer ${upload_token}" \
  -H "X-DingWei-Session: ${session}" -H 'X-File-Name: deployment-probe.txt' \
  -H 'Content-Type: text/plain' -H "X-Content-SHA256: ${sum}" \
  --data-binary @"$tmp/body" "$public_base/files/upload" >"$tmp/upload.json"
file_id=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["file_ref"].removeprefix("dwfile:"))' "$tmp/upload.json")
range="bytes=0-$((size - 1))"

python3 - "$owner" "$session" "$file_id" "$range" >"$tmp/issue-get.json" <<'PY'
import json, sys
print(json.dumps({"subject": sys.argv[1] + "/" + sys.argv[2], "file_id": sys.argv[3],
                  "method": "GET", "range": sys.argv[4], "ttl_seconds": 60}))
PY
curl --fail --silent --show-error \
  -H "Authorization: Bearer ${FS_INTERNAL_ISSUER_TOKEN}" \
  -H 'Content-Type: application/json' --data-binary @"$tmp/issue-get.json" \
  "$internal_base/internal/files/issue/retrieval" >"$tmp/get-ticket.json"
get_token=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["token"])' "$tmp/get-ticket.json")
curl --fail --silent --show-error \
  -H "Authorization: Bearer ${get_token}" -H "Range: ${range}" \
  "$public_base/files/ref/$file_id" >"$tmp/got"
cmp "$tmp/body" "$tmp/got"
printf 'FILE_SERVICE_MINIO_OK file_id=%s bytes=%s sha256=%s\n' "$file_id" "$size" "$sum"
