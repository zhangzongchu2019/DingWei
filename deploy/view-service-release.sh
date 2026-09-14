#!/usr/bin/env bash
# T-109: reproducible view-service package and online provenance verification.
set -euo pipefail

FILES=(
  view-service/renderer-canary-sse.html
  view-service/view_service.py
  view-service/view_page_store.py
)

die() { echo "ERROR: $*" >&2; exit 2; }
sha_file() { sha256sum "$1" | awk '{print $1}'; }
full_commit() {
  [[ "$1" =~ ^[0-9a-f]{40}$ ]] || die "commit must be a full 40-character SHA"
  git rev-parse --verify "$1^{commit}" 2>/dev/null || die "commit is not available: $1"
}

package_release() {
  local commit out actual f dst manifest
  commit="${1:-}"; out="${2:-}"
  [ -n "$commit" ] && [ -n "$out" ] || die "usage: package <commit> <output-dir>"
  actual="$(full_commit "$commit")"; [ "$actual" = "$commit" ] || die "commit mismatch"
  [ ! -e "$out" ] || die "output already exists: $out"
  mkdir -p "$out/view-service"
  manifest="$out/MANIFEST.sha256"
  : >"$manifest"
  for f in "${FILES[@]}"; do
    git cat-file -e "$commit:$f" 2>/dev/null || die "missing path at commit: $f"
    dst="$out/$f"; git show "$commit:$f" >"$dst"
    printf '%s  %s\n' "$(sha_file "$dst")" "$f" >>"$manifest"
  done
  cat >"$out/PROVENANCE.json" <<EOF
{"commit":"$commit","tree":"$(git rev-parse "$commit^{tree}")","manifest_sha256":"$(sha_file "$manifest")"}
EOF
  echo "PACKAGED commit=$commit files=${#FILES[@]} output=$out"
}

verify_package() {
  local root expected hash path count=0 provenance_manifest actual_manifest
  root="${1:-}"; expected="${2:-}"
  [ -f "$root/MANIFEST.sha256" ] || die "manifest missing"
  [ -f "$root/PROVENANCE.json" ] || die "provenance missing"
  provenance_manifest="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["manifest_sha256"])' "$root/PROVENANCE.json")" || die "invalid provenance"
  actual_manifest="$(sha_file "$root/MANIFEST.sha256")"
  [ "$provenance_manifest" = "$actual_manifest" ] || { echo "FAIL manifest_digest"; return 1; }
  while read -r hash path; do
    [ -n "$hash" ] || continue; count=$((count + 1))
    [ "$count" -le "${#FILES[@]}" ] && [ "$path" = "${FILES[$((count - 1))]}" ] || { echo "FAIL unexpected_path=$path"; return 1; }
    [ -f "$root/$path" ] || { echo "FAIL missing=$path"; return 1; }
    [ "$(sha_file "$root/$path")" = "$hash" ] || { echo "FAIL digest=$path"; return 1; }
  done <"$root/MANIFEST.sha256"
  [ "$count" -eq "${#FILES[@]}" ] || { echo "FAIL file_count=$count"; return 1; }
  if [ -n "$expected" ]; then
    full_commit "$expected" >/dev/null
    grep -Fq '"commit":"'"$expected"'"' "$root/PROVENANCE.json" || { echo "FAIL expected_commit=$expected"; return 1; }
  fi
  echo "VERIFIED files=$count${expected:+ expected_commit=$expected}"
}

identify_online() {
  local root ref commits scanned=0 hit=0 c f ok online candidate
  root="${1:-}"; ref="${2:-}"
  [ -n "$root" ] && [ -n "$ref" ] || die "usage: identify-online <online-root> <scan-ref>"
  git rev-parse --verify "$ref^{commit}" >/dev/null 2>&1 || die "scan ref is not available: $ref"
  for f in "${FILES[@]}"; do [ -f "$root/$f" ] || die "online path missing: $f"; done
  commits="$(git rev-list "$ref")"; [ -n "$commits" ] || die "empty scan range"
  echo "SCAN ref=$ref tip=$(git rev-parse "$ref^{commit}") commits=$(printf '%s\n' "$commits" | wc -l)"
  while read -r c; do
    [ -n "$c" ] || continue; scanned=$((scanned + 1)); ok=1
    for f in "${FILES[@]}"; do
      git cat-file -e "$c:$f" 2>/dev/null || { ok=0; break; }
      online="$(sha_file "$root/$f")"
      candidate="$(git show "$c:$f" | sha256sum | awk '{print $1}')"
      [ "$online" = "$candidate" ] || { ok=0; break; }
    done
    if [ "$ok" -eq 1 ]; then echo "MATCH commit=$c"; hit=$((hit + 1)); fi
  done <<<"$commits"
  case "$hit" in
    0) echo "NOT_IN_REPO scanned=$scanned matches=0"; return 1 ;;
    1) echo "IDENTIFIED scanned=$scanned matches=1" ;;
    *) echo "AMBIGUOUS scanned=$scanned matches=$hit"; return 1 ;;
  esac
}

cmd="${1:-}"; shift || true
case "$cmd" in
  package) package_release "$@" ;;
  verify-package) verify_package "${1:-}" "${2:-}" ;;
  identify-online) identify_online "$@" ;;
  *) die "commands: package | verify-package | identify-online" ;;
esac
