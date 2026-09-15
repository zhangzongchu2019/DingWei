#!/usr/bin/env bash
# T-109: reproducible view-service package and online provenance verification.
set -euo pipefail

REPO_FILES=(
  view-service/renderer-canary-sse.html
  view-service/view_service.py
  view-service/view_page_store.py
)
FLAT_FILES=(renderer-canary-sse.html view_service.py view_page_store.py)

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
  for f in "${REPO_FILES[@]}"; do
    git cat-file -e "$commit:$f" 2>/dev/null || die "missing path at commit: $f"
    dst="$out/$f"; git cat-file blob "$commit:$f" >"$dst"
    printf '%s  %s\n' "$(sha_file "$dst")" "$f" >>"$manifest"
  done
  cat >"$out/PROVENANCE.json" <<EOF
{"commit":"$commit","tree":"$(git rev-parse "$commit^{tree}")","manifest_sha256":"$(sha_file "$manifest")"}
EOF
  echo "PACKAGED commit=$commit files=${#REPO_FILES[@]} output=$out"
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
    [ "$count" -le "${#REPO_FILES[@]}" ] && [ "$path" = "${REPO_FILES[$((count - 1))]}" ] || { echo "FAIL unexpected_path=$path"; return 1; }
    [ -f "$root/$path" ] || { echo "FAIL missing=$path"; return 1; }
    [ "$(sha_file "$root/$path")" = "$hash" ] || { echo "FAIL digest=$path"; return 1; }
  done <"$root/MANIFEST.sha256"
  [ "$count" -eq "${#REPO_FILES[@]}" ] || { echo "FAIL file_count=$count"; return 1; }
  if [ -n "$expected" ]; then
    full_commit "$expected" >/dev/null
    for path in "${REPO_FILES[@]}"; do
      git cat-file -e "$expected:$path" 2>/dev/null || { echo "FAIL expected_path=$path"; return 1; }
      [ "$(sha_file "$root/$path")" = "$(git cat-file blob "$expected:$path" | sha256sum | awk '{print $1}')" ] || {
        echo "FAIL expected_content=$path commit=$expected"; return 1;
      }
    done
  fi
  echo "VERIFIED files=$count${expected:+ expected_commit=$expected}"
}

identify_online() {
  local root flat=0 verbose=0 commits refs scanned=0 hit=0 c f online_path ok online candidate n matches
  local -a scan_refs=() online_files=()
  root="${1:-}"; shift || true
  [ -n "$root" ] || die "usage: identify-online <online-root> [--flat] [--verbose] [--all|ref ...]"
  while [ $# -gt 0 ]; do
    case "$1" in --flat) flat=1 ;; --verbose) verbose=1 ;; --all) scan_refs=(--all) ;; *) scan_refs+=("$1") ;; esac; shift
  done
  [ "${#scan_refs[@]}" -gt 0 ] || scan_refs=(--all)
  if [ "$flat" -eq 1 ]; then online_files=("${FLAT_FILES[@]}"); else online_files=("${REPO_FILES[@]}"); fi
  for online_path in "${online_files[@]}"; do [ -f "$root/$online_path" ] || die "online path missing: $online_path"; done
  if [ "${scan_refs[0]}" = --all ]; then
    refs="$(git for-each-ref --format='%(refname)')"
  else
    for f in "${scan_refs[@]}"; do git rev-parse --verify "$f^{commit}" >/dev/null 2>&1 || die "scan ref is not available: $f"; done
    refs="$(printf '%s\n' "${scan_refs[@]}")"
  fi
  # The commit range is derived from the exact ref list reported below. This
  # structurally prevents tags (or any future ref namespace) from being scanned
  # without also being counted in the evidence line.
  commits="$(git rev-list --stdin <<<"$refs")"
  [ -n "$commits" ] || die "empty scan range"
  echo "SCAN scope=${scan_refs[*]} refs=$(printf '%s\n' "$refs" | wc -l) commits=$(printf '%s\n' "$commits" | wc -l) flat=$flat"
  if [ "$verbose" -eq 1 ]; then
    while read -r f; do [ -n "$f" ] && echo "SCAN_REF $f"; done <<<"$refs"
  fi
  while read -r c; do
    [ -n "$c" ] || continue; scanned=$((scanned + 1)); ok=1
    for n in "${!REPO_FILES[@]}"; do
      f="${REPO_FILES[$n]}"; online_path="${online_files[$n]}"
      git cat-file -e "$c:$f" 2>/dev/null || { ok=0; break; }
      online="$(sha_file "$root/$online_path")"
      candidate="$(git cat-file blob "$c:$f" | sha256sum | awk '{print $1}')"
      [ "$online" = "$candidate" ] || { ok=0; break; }
    done
    if [ "$ok" -eq 1 ]; then echo "MATCH commit=$c"; hit=$((hit + 1)); fi
  done <<<"$commits"
  case "$hit" in
    0)
      echo "NOT_IN_REPO scanned=$scanned matches=0"
      for n in "${!REPO_FILES[@]}"; do
        f="${REPO_FILES[$n]}"; online_path="${online_files[$n]}"; online="$(sha_file "$root/$online_path")"; matches=()
        while read -r c; do
          git cat-file -e "$c:$f" 2>/dev/null || continue
          candidate="$(git cat-file blob "$c:$f" | sha256sum | awk '{print $1}')"
          [ "$candidate" = "$online" ] && matches+=("$c")
        done <<<"$commits"
        if [ "${#matches[@]}" -eq 0 ]; then echo "FILE_NOT_IN_REPO path=$f"; else
          echo "FILE_MATCH path=$f equivalent_commits=${#matches[@]} newest_equivalent=${matches[0]} attribution=AMBIGUOUS"
        fi
      done
      return 1 ;;
    1) echo "IDENTIFIED scanned=$scanned matches=1" ;;
    *) echo "AMBIGUOUS scanned=$scanned matches=$hit"; return 1 ;;
  esac
}

container_hash() { docker exec "$1" sha256sum "/app/vs/$2" | awk '{print $1}'; }
container_meta() { docker exec "$1" stat -c '%u:%g:%a' "/app/vs/$2"; }

wait_viewservice() {
  local container="$1" attempts="${VSR_HEALTH_ATTEMPTS:-30}" interval="${VSR_HEALTH_INTERVAL:-1}"
  local i started now status
  [[ "$attempts" =~ ^[1-9][0-9]*$ ]] || die "VSR_HEALTH_ATTEMPTS must be a positive integer"
  [[ "$interval" =~ ^[0-9]+$ ]] || die "VSR_HEALTH_INTERVAL must be a non-negative integer"
  started="$(date +%s)"
  for ((i=1; i<=attempts; i++)); do
    status="$(docker exec "$container" supervisorctl status viewservice 2>&1 || true)"
    if grep -q RUNNING <<<"$status" && docker exec "$container" curl -sf http://127.0.0.1:8791/healthz >/dev/null 2>&1; then
      now="$(date +%s)"; echo "HEALTHY attempts=$i waited_seconds=$((now - started))"; return 0
    fi
    [ "$i" -eq "$attempts" ] || sleep "$interval"
  done
  now="$(date +%s)"; echo "HEALTH_TIMEOUT attempts=$attempts waited_seconds=$((now - started)) last_status=$status" >&2
  return 1
}

check_container_layout() {
  local container="$1" entries entry pyc_count
  entries="$(docker exec "$container" find /app/vs -mindepth 1 -maxdepth 2 -printf '%P\n' | sort)"
  while read -r entry; do
    case "$entry" in
      renderer-canary-sse.html|view_service.py|view_page_store.py|__pycache__|__pycache__/view_page_store.cpython-*.pyc) ;;
      *) die "unexpected online path: $entry" ;;
    esac
  done <<<"$entries"
  for entry in "${FLAT_FILES[@]}"; do
    grep -Fxq "$entry" <<<"$entries" || die "required online path missing: $entry"
  done
  pyc_count="$(grep -c '^__pycache__/view_page_store\.cpython-.*\.pyc$' <<<"$entries" || true)"
  [ "$pyc_count" -le 1 ] || die "expected at most one view_page_store pyc"
}

restore_container() {
  local container="$1" backup="$2" restart="$3" n name meta uid gid mode want got
  local restore_rc=0 digest_rc=0 health_rc=0
  echo "ROLLBACK backup=$backup"
  for n in "${!FLAT_FILES[@]}"; do
    name="${FLAT_FILES[$n]}"; meta="$(cat "$backup/meta/$name")"
    IFS=: read -r uid gid mode <<<"$meta"
    docker cp "$backup/pre/$name" "$container:/app/vs/$name" || { echo "ROLLBACK_STEP_FAIL copy=$name" >&2; restore_rc=1; }
    docker exec "$container" chown "$uid:$gid" "/app/vs/$name" || { echo "ROLLBACK_STEP_FAIL chown=$name" >&2; restore_rc=1; }
    docker exec "$container" chmod "$mode" "/app/vs/$name" || { echo "ROLLBACK_STEP_FAIL chmod=$name" >&2; restore_rc=1; }
  done
  docker exec "$container" rm -rf /app/vs/__pycache__ || { echo "ROLLBACK_STEP_FAIL cache_remove" >&2; restore_rc=1; }
  if [ -d "$backup/pre/__pycache__" ]; then
    docker cp "$backup/pre/__pycache__" "$container:/app/vs/" || { echo "ROLLBACK_STEP_FAIL cache_restore" >&2; restore_rc=1; }
  fi
  for name in "${FLAT_FILES[@]}"; do
    want="$(sha_file "$backup/pre/$name")"
    if got="$(container_hash "$container" "$name" 2>/dev/null)" && [ "$want" = "$got" ]; then :; else
      echo "ROLLBACK_FAIL digest=$name expected=$want actual=${got:-UNREADABLE}" >&2; digest_rc=1
    fi
  done
  [ "$digest_rc" -ne 0 ] || echo "RESTORED_DIGESTS ok files=${#FLAT_FILES[@]}"
  if [ "$restart" -ne 0 ]; then
    docker exec "$container" supervisorctl restart viewservice || health_rc=1
    [ "$health_rc" -ne 0 ] || wait_viewservice "$container" || health_rc=1
  fi
  if [ "$restore_rc" -eq 0 ] && [ "$digest_rc" -eq 0 ] && [ "$health_rc" -eq 0 ]; then
    echo "ROLLED_BACK"; return 0
  fi
  echo "ROLLBACK_INCOMPLETE restore=$restore_rc digests=$digest_rc health=$health_rc" >&2
  return 1
}

install_container() {
  local package="${1:-}"; shift || true
  local expected="" container="" backup_root="" go=0 n name current target py_changed=0 backup stamp meta uid gid mode copy_ok identity container_id container_image extra
  local -a current_hashes=() target_hashes=()
  local -A write_only=()
  while [ $# -gt 0 ]; do
    case "$1" in
      --expected-commit) expected="${2:-}"; shift ;;
      --container) container="${2:-}"; shift ;;
      --backup-root) backup_root="${2:-}"; shift ;;
      --write-only)
        [ -n "${2:-}" ] || die "--write-only value required"
        case "$2" in renderer-canary-sse.html|view_service.py|view_page_store.py) write_only["$2"]=1 ;; *) die "invalid --write-only file: $2" ;; esac
        shift ;;
      --go) go=1 ;;
      *) die "unknown install option: $1" ;;
    esac; shift
  done
  [ -n "$package" ] && [ -n "$expected" ] && [ -n "$container" ] && [ -n "$backup_root" ] ||
    die "usage: install <package> --expected-commit <sha> --container <name> --backup-root <dir> --write-only <runtime-file> [--write-only <runtime-file> ...] [--go]"
  [ "${#write_only[@]}" -gt 0 ] || die "at least one --write-only is required"
  verify_package "$package" "$expected"
  identity="$(docker inspect --format '{{.Id}} {{.Config.Image}}' "$container")" || die "container inspect failed: $container"
  read -r container_id container_image extra <<<"$identity"
  [ -n "$container_id" ] && [ -n "$container_image" ] && [ -z "${extra:-}" ] || die "invalid container identity: $identity"
  echo "CONTAINER name=$container id=$container_id image=$container_image"
  check_container_layout "$container"
  echo "PREFLIGHT container=$container expected_commit=$expected files=${#FLAT_FILES[@]}"
  for n in "${!FLAT_FILES[@]}"; do
    name="${FLAT_FILES[$n]}"; current="$(container_hash "$container" "$name")"; target="$(sha_file "$package/${REPO_FILES[$n]}")"
    current_hashes[$n]="$current"; target_hashes[$n]="$target"
    meta="$(container_meta "$container" "$name")"
    echo "FILE name=$name current=$current target=$target metadata=$meta"
    if [ -z "${write_only[$name]:-}" ] && [ "$current" != "$target" ]; then
      die "UNAUTHORIZED_DRIFT name=$name current=$current target=$target writes=0"
    fi
    if [[ "$name" == *.py ]] && [ "$current" != "$target" ]; then py_changed=1; fi
  done
  if [ "$go" -eq 0 ]; then echo "PRECHECK_ONLY writes=0"; return 0; fi

  stamp="$(date -u +%Y%m%dT%H%M%SZ)-$(printf '%04x' "$RANDOM")"; backup="$backup_root/view-service-$expected-$stamp"
  mkdir -p "$backup/pre" "$backup/forward" "$backup/meta"
  cp "$package/MANIFEST.sha256" "$package/PROVENANCE.json" "$backup/forward/"
  for n in "${!FLAT_FILES[@]}"; do
    name="${FLAT_FILES[$n]}"
    docker cp "$container:/app/vs/$name" "$backup/pre/$name"
    cp "$package/${REPO_FILES[$n]}" "$backup/forward/$name"
    container_meta "$container" "$name" >"$backup/meta/$name"
  done
  if docker exec "$container" test -d /app/vs/__pycache__; then
    docker cp "$container:/app/vs/__pycache__" "$backup/pre/"
    echo "CACHE_BACKUP state=present"
  else
    echo "CACHE_BACKUP state=absent"
  fi

  if ! {
    copy_ok=1
    for n in "${!FLAT_FILES[@]}"; do
      name="${FLAT_FILES[$n]}"
      if [ -z "${write_only[$name]:-}" ]; then
        echo "SKIP name=$name reason=not-authorized-identical"
        continue
      fi
      if [ "${current_hashes[$n]}" = "${target_hashes[$n]}" ]; then
        echo "SKIP name=$name reason=identical"
        continue
      fi
      IFS=: read -r uid gid mode <"$backup/meta/$name"
      docker cp "$package/${REPO_FILES[$n]}" "$container:/tmp/t109-$expected-$name" &&
      docker exec "$container" mv "/tmp/t109-$expected-$name" "/app/vs/$name" &&
      docker exec "$container" chown "$uid:$gid" "/app/vs/$name" &&
      docker exec "$container" chmod "$mode" "/app/vs/$name" || { copy_ok=0; break; }
    done
    [ "$copy_ok" -eq 1 ] && { [ "$py_changed" -eq 0 ] || docker exec "$container" rm -rf /app/vs/__pycache__; } &&
    { [ "$py_changed" -eq 0 ] || { docker exec "$container" supervisorctl restart viewservice && wait_viewservice "$container"; }; } &&
    for n in "${!FLAT_FILES[@]}"; do
      name="${FLAT_FILES[$n]}"
      [ "$(container_hash "$container" "$name")" = "$(sha_file "$package/${REPO_FILES[$n]}")" ] || false
    done
  }; then
    restore_container "$container" "$backup" "$py_changed"
    return 1
  fi
  echo "INSTALLED commit=$expected backup=$backup restart=$py_changed files=${#FLAT_FILES[@]}"
}

cmd="${1:-}"; shift || true
case "$cmd" in
  package) package_release "$@" ;;
  verify-package) verify_package "${1:-}" "${2:-}" ;;
  identify-online) identify_online "$@" ;;
  install) install_container "$@" ;;
  *) die "commands: package | verify-package | identify-online | install" ;;
esac
