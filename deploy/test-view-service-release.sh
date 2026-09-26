#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TOOL="$ROOT/deploy/view-service-release.sh"
T="$(mktemp -d)"; trap 'rm -rf "$T"' EXIT
MUTATION=''
case "${1:-}" in
  '') ;;
  --mutate=*) MUTATION="${1#--mutate=}" ;;
  *) echo "usage: $0 [--mutate=identify-reject-member|--mutate=identify-accept-nonmember|--mutate=drop-cwd-guard]" >&2; exit 2 ;;
esac
if [ -n "$MUTATION" ]; then
  cp "$TOOL" "$T/view-service-release.sh"; TOOL="$T/view-service-release.sh"
  case "$MUTATION" in
    identify-reject-member)
      grep -Fq '[ "$expected_match_found" -eq 1 ]' "$TOOL" || { echo 'stale mutation probe: identify-reject-member' >&2; exit 2; }
      sed -i 's/\[ "$expected_match_found" -eq 1 \]/[ "$expected_match_found" -eq 0 ]/' "$TOOL" ;;
    identify-accept-nonmember)
      grep -A1 'EXPECTED_COMMIT_MISMATCH' "$TOOL" | grep -Fq 'return 1' || { echo 'stale mutation probe: identify-accept-nonmember' >&2; exit 2; }
      sed -i '/EXPECTED_COMMIT_MISMATCH/{n;s/return 1/return 0/;}' "$TOOL" ;;
    drop-cwd-guard)
      grep -Fq 'git rev-parse --is-inside-work-tree' "$TOOL" || { echo 'stale mutation probe: drop-cwd-guard' >&2; exit 2; }
      sed -i '/^require_git_worktree() {$/,/^}$/c\require_git_worktree() { :; }' "$TOOL" ;;
    *) echo "unknown mutation: $MUTATION" >&2; exit 2 ;;
  esac
fi
git init -q "$T/repo"; git -C "$T/repo" config user.email test@example.invalid; git -C "$T/repo" config user.name test
mkdir -p "$T/repo/view-service"
for f in renderer-canary-sse.html view_service.py view_page_store.py; do printf 'v1-%s\n' "$f" >"$T/repo/view-service/$f"; done
git -C "$T/repo" add .; git -C "$T/repo" commit -qm v1; C1="$(git -C "$T/repo" rev-parse HEAD)"
printf 'v2\n' >"$T/repo/view-service/view_service.py"; git -C "$T/repo" commit -qam v2; C2="$(git -C "$T/repo" rev-parse HEAD)"

# A wrong caller cwd is diagnosed before commit lookup can misdirect the
# operator toward the anchor or fetch state.
mkdir "$T/outside"
if (cd "$T/outside" && "$TOOL" package "$C2" "$T/outside-package") >"$T/outside-cwd" 2>&1; then exit 1; fi
grep -Fq '本脚本必须在仓内运行' "$T/outside-cwd" || {
  echo "cwd guard diagnostic missing: $(<"$T/outside-cwd")" >&2; exit 1
}
grep -Fq "current cwd=$T/outside" "$T/outside-cwd" || { echo 'cwd guard omitted current cwd' >&2; exit 1; }
grep -Fq '请先 cd 到该锚的 checkout' "$T/outside-cwd" || { echo 'cwd guard omitted checkout advice' >&2; exit 1; }
if grep -Fq 'commit is not available' "$T/outside-cwd"; then
  echo 'cwd guard fell through to misleading commit diagnostic' >&2; exit 1
fi

# Once cwd is valid, only a genuinely missing object gets the fetch advice.
MISSING_COMMIT=0000000000000000000000000000000000000000
if (cd "$T/repo" && "$TOOL" package "$MISSING_COMMIT" "$T/missing-package") >"$T/missing-commit" 2>&1; then exit 1; fi
grep -Fq "commit is not available: $MISSING_COMMIT" "$T/missing-commit"
grep -Fq 'git fetch <ref>' "$T/missing-commit"
if grep -Fq '本脚本必须在仓内运行' "$T/missing-commit"; then exit 1; fi

# Release orders cannot rely on the author remembering the view-service side.
printf '%s\n' '# order' >"$T/order-missing"
if (cd "$T/repo" && "$TOOL" check-order "$C1" "$C2" "$T/order-missing") >/dev/null 2>&1; then exit 1; fi
printf '%s\n' '- 是否触及 view-service 侧运行时文件：否' >"$T/order-no"
if (cd "$T/repo" && "$TOOL" check-order "$C1" "$C2" "$T/order-no") >/dev/null 2>&1; then exit 1; fi
printf '%s\n' '- 是否触及 view-service 侧运行时文件：是' >"$T/order-yes"
(cd "$T/repo" && "$TOOL" check-order "$C1" "$C2" "$T/order-yes") >/dev/null
(cd "$T/repo" && "$TOOL" check-order "$C2" "$C2" "$T/order-no") >/dev/null

(cd "$T/repo" && "$TOOL" package "$C2" "$T/pkg" && "$TOOL" verify-package "$T/pkg" "$C2")
rm "$T/pkg/PROVENANCE.json"
OUT="$(cd "$T/repo" && "$TOOL" identify-online "$T/pkg" "$C2")"
grep -q '^IDENTIFIED ' <<<"$OUT"

# An unrelated commit leaves the deployable tuple unchanged: report every
# matching commit and refuse to guess which one was deployed.
printf 'unrelated\n' >"$T/repo/README"; git -C "$T/repo" add README; git -C "$T/repo" commit -qm unrelated
if (cd "$T/repo" && "$TOOL" identify-online "$T/pkg" HEAD) >"$T/ambiguous"; then exit 1; fi
grep -q '^AMBIGUOUS ' "$T/ambiguous"
# With an explicit release anchor, ambiguity is informational: the audit passes
# exactly when that anchor is one of the commits with matching runtime bytes.
OUT="$(cd "$T/repo" && "$TOOL" identify-online "$T/pkg" --expect-commit "$C2" HEAD)"
grep -q '^AMBIGUOUS ' <<<"$OUT"
grep -q "^EXPECTED_COMMIT_MATCH expected=$C2 matches=2$" <<<"$OUT"
if (cd "$T/repo" && "$TOOL" identify-online "$T/pkg" --expect-commit "$C1" HEAD) >"$T/expected-mismatch" 2>&1; then
  echo "expected-commit negative unexpectedly passed: expected=$C1" >&2; exit 1
fi
grep -q "^EXPECTED_COMMIT_MISMATCH expected=$C1 actual_matches=.*$C2" "$T/expected-mismatch"

printf 'manual bypass\n' >"$T/pkg/view-service/view_service.py"
if (cd "$T/repo" && "$TOOL" identify-online "$T/pkg" HEAD) >"$T/not-in-repo"; then exit 1; fi
grep -q '^NOT_IN_REPO ' "$T/not-in-repo"
if (cd "$T/repo" && "$TOOL" identify-online "$T/pkg" --expect-commit "$C2" HEAD) >"$T/not-in-repo-expected"; then
  echo 'NOT_IN_REPO unexpectedly passed with --expect-commit' >&2; exit 1
fi
grep -q '^NOT_IN_REPO ' "$T/not-in-repo-expected"

(cd "$T/repo" && "$TOOL" package "$C1" "$T/old")
if (cd "$T/repo" && "$TOOL" verify-package "$T/old" "$C2") >/dev/null; then exit 1; fi
cp -R "$T/old" "$T/forged"
sed -i "s/$C1/$C2/" "$T/forged/PROVENANCE.json"
if (cd "$T/repo" && "$TOOL" verify-package "$T/forged" "$C2") >/dev/null; then exit 1; fi
printf 'x' >>"$T/old/MANIFEST.sha256"
if (cd "$T/repo" && "$TOOL" verify-package "$T/old") >/dev/null; then exit 1; fi

# A caller-selected parallel ref must not be mistaken for a repository-wide
# negative. The default --all scan still finds the package's source commit.
EMPTY_TREE="$(git -C "$T/repo" mktree </dev/null)"
PARALLEL="$(printf 'parallel\n' | git -C "$T/repo" commit-tree "$EMPTY_TREE")"
git -C "$T/repo" update-ref refs/heads/parallel "$PARALLEL"
TAG_ONLY="$(printf 'tag-only\n' | git -C "$T/repo" commit-tree "$EMPTY_TREE")"
git -C "$T/repo" tag tag-only "$TAG_ONLY"
if (cd "$T/repo" && "$TOOL" identify-online "$T/old" parallel) >"$T/wrong-ref"; then exit 1; fi
grep -q '^NOT_IN_REPO ' "$T/wrong-ref"
OUT="$(cd "$T/repo" && "$TOOL" identify-online "$T/old")"
grep -q '^IDENTIFIED ' <<<"$OUT"
if grep -q '^SCAN_REF ' <<<"$OUT"; then exit 1; fi
OUT="$(cd "$T/repo" && "$TOOL" identify-online "$T/old" --verbose)"
grep -q '^SCAN_REF ' <<<"$OUT"
grep -q '^SCAN_REF refs/tags/tag-only$' <<<"$OUT"

# Flat runtime layout uses an explicit repository-path to online-path mapping.
mkdir "$T/flat"
cp "$T/old/view-service/"* "$T/flat/"
OUT="$(cd "$T/repo" && "$TOOL" identify-online "$T/flat" --flat)"
grep -q '^IDENTIFIED ' <<<"$OUT"

# Container adapter fixture: exact five-entry topology, precheck with zero
# writes, then a three-file deployment with forward/pre-image backups.
(cd "$T/repo" && "$TOOL" package "$C2" "$T/install-pkg")
mkdir -p "$T/bin" "$T/container/app/vs/__pycache__" "$T/container/tmp" "$T/backups"
cp "$T/old/view-service/"* "$T/container/app/vs/"
printf pyc >"$T/container/app/vs/__pycache__/view_page_store.cpython-311.pyc"
cat >"$T/bin/docker" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
root="$FAKE_CONTAINER_ROOT"; log="$root/docker.log"; op="$1"; shift
path() { printf '%s%s' "$root" "$1"; }
if [ "$op" = inspect ]; then
  shift 2; [ "$1" = fixture ] || exit 1
  echo 'sha256:fixture-id example/view-service:test'
elif [ "$op" = exec ]; then
  container="$1"; shift; cmd="$1"; shift; echo "exec $cmd $*" >>"$log"
  case "$cmd" in
    find) find "$(path /app/vs)" -mindepth 1 -maxdepth 2 -printf '%P\n' ;;
    sha256sum) sha256sum "$(path "$1")" ;;
    stat) shift 2; stat -c '%u:%g:%a' "$(path "$1")" ;;
    chown) : ;;
    chmod) chmod "$1" "$(path "$2")" ;;
    rm) rm -rf "$(path "${@: -1}")" ;;
    mv) mv "$(path "$1")" "$(path "$2")" ;;
    test) test "$1" "$(path "$2")" ;;
    supervisorctl)
      if [ "$1" = restart ]; then echo restart >>"$root/restarts"; mkdir -p "$root/app/vs/__pycache__"; printf pyc >"$root/app/vs/__pycache__/view_page_store.cpython-311.pyc"
      elif [ -s "$root/health-delay" ]; then
        n="$(cat "$root/health-delay")"; if [ "$n" -gt 0 ]; then echo $((n - 1)) >"$root/health-delay"; echo 'viewservice STARTING'; else echo 'viewservice RUNNING pid 123, uptime 0:00:01'; fi
      else echo 'viewservice RUNNING pid 123, uptime 0:00:01'; fi ;;
    curl) : ;;
    *) exit 2 ;;
  esac
elif [ "$op" = cp ]; then
  src="$1"; dst="$2"; echo "cp $src $dst" >>"$log"
  if [ -e "$root/fail-once" ] && [[ "$dst" == *:/tmp/t109-*-view_service.py ]]; then rm "$root/fail-once"; exit 9; fi
  if [[ "$src" == *:* ]]; then cp -R "$(path "${src#*:}")" "$dst"; else cp -R "$src" "$(path "${dst#*:}")"; fi
else exit 2; fi
EOF
chmod +x "$T/bin/docker"
export FAKE_CONTAINER_ROOT="$T/container" PATH="$T/bin:$PATH" VSR_HEALTH_INTERVAL=0
BEFORE="$(find "$T/container/app/vs" -type f -exec sha256sum {} + | sort | sha256sum)"
if (cd "$T/repo" && "$TOOL" install "$T/install-pkg" --expected-commit "$C2" --container wrong --backup-root "$T/backups" --write-only view_service.py) >/dev/null 2>&1; then exit 1; fi
OUT="$(cd "$T/repo" && "$TOOL" install "$T/install-pkg" --expected-commit "$C2" --container fixture --backup-root "$T/backups" --write-only view_service.py)"
grep -q '^CONTAINER name=fixture id=sha256:fixture-id image=example/view-service:test$' <<<"$OUT"
grep -q 'PRECHECK_ONLY writes=0' <<<"$OUT"
AFTER="$(find "$T/container/app/vs" -type f -exec sha256sum {} + | sort | sha256sum)"
[ "$BEFORE" = "$AFTER" ]
echo 2 >"$T/container/health-delay"
OUT="$(cd "$T/repo" && "$TOOL" install "$T/install-pkg" --expected-commit "$C2" --container fixture --backup-root "$T/backups" --write-only view_service.py --go)"
grep -q '^HEALTHY attempts=3 ' <<<"$OUT"
grep -q '^INSTALLED ' <<<"$OUT"
[ "$(sha256sum "$T/install-pkg/MANIFEST.sha256" | awk '{print $1}')" = "$(sha256sum "$T/container/app/vs/MANIFEST.sha256" | awk '{print $1}')" ]
[ "$(sha256sum "$T/install-pkg/PROVENANCE.json" | awk '{print $1}')" = "$(sha256sum "$T/container/app/vs/PROVENANCE.json" | awk '{print $1}')" ]
[ "$(grep -c '^SKIP ' <<<"$OUT")" -eq 2 ]
grep -q '^SKIP name=renderer-canary-sse.html reason=not-authorized-identical$' <<<"$OUT"
grep -q '^SKIP name=view_page_store.py reason=not-authorized-identical$' <<<"$OUT"
[ -e "$T/container/app/vs/__pycache__/view_page_store.cpython-311.pyc" ]
[ -s "$T/container/restarts" ]
BACKUP="$(find "$T/backups" -mindepth 1 -maxdepth 1 -type d)"
[ "$(find "$BACKUP/pre" -type f | wc -l)" -eq 4 ]
[ "$(find "$BACKUP/forward" -type f | wc -l)" -eq 5 ]

# Renderer-only changes are hot-updated without restarting viewservice.
RESTARTS="$(wc -l <"$T/container/restarts")"
touch -d '2020-01-01 00:00:00 UTC' "$T/container/app/vs/view_service.py" "$T/container/app/vs/view_page_store.py"
PY_MTIME_BEFORE="$(stat -c '%y' "$T/container/app/vs/view_service.py")|$(stat -c '%y' "$T/container/app/vs/view_page_store.py")"
printf 'renderer-v3\n' >"$T/repo/view-service/renderer-canary-sse.html"
git -C "$T/repo" commit -qam renderer-v3; C3="$(git -C "$T/repo" rev-parse HEAD)"
(cd "$T/repo" && "$TOOL" package "$C3" "$T/static-pkg")
OUT="$(cd "$T/repo" && "$TOOL" install "$T/static-pkg" --expected-commit "$C3" --container fixture --backup-root "$T/backups" --write-only renderer-canary-sse.html --go)"
grep -q 'restart=0' <<<"$OUT"
[ "$(grep -c '^SKIP ' <<<"$OUT")" -eq 2 ]
grep -q '^SKIP name=view_service.py reason=not-authorized-identical$' <<<"$OUT"
grep -q '^SKIP name=view_page_store.py reason=not-authorized-identical$' <<<"$OUT"
[ "$PY_MTIME_BEFORE" = "$(stat -c '%y' "$T/container/app/vs/view_service.py")|$(stat -c '%y' "$T/container/app/vs/view_page_store.py")" ]
[ "$(wc -l <"$T/container/restarts")" -eq "$RESTARTS" ]

# Empty authorization is rejected before any write.
: >"$T/container/docker.log"; BACKUPS_BEFORE="$(find "$T/backups" -mindepth 1 -maxdepth 1 -type d | wc -l)"
if (cd "$T/repo" && "$TOOL" install "$T/static-pkg" --expected-commit "$C3" --container fixture --backup-root "$T/backups" --go) >/dev/null 2>&1; then exit 1; fi
[ ! -s "$T/container/docker.log" ]
[ "$BACKUPS_BEFORE" -eq "$(find "$T/backups" -mindepth 1 -maxdepth 1 -type d | wc -l)" ]

# Drift outside the order's write set aborts before backup or any container write.
printf 'unauthorized-drift\n' >"$T/container/app/vs/view_service.py"; : >"$T/container/docker.log"
if OUT="$(cd "$T/repo" && "$TOOL" install "$T/static-pkg" --expected-commit "$C3" --container fixture --backup-root "$T/backups" --write-only renderer-canary-sse.html --go 2>&1)"; then exit 1; fi
grep -q 'UNAUTHORIZED_DRIFT name=view_service.py .* writes=0' <<<"$OUT"
if grep -Eq '^(cp |exec (mv|chown|chmod|rm) )' "$T/container/docker.log"; then exit 1; fi
[ "$BACKUPS_BEFORE" -eq "$(find "$T/backups" -mindepth 1 -maxdepth 1 -type d | wc -l)" ]
cp "$T/static-pkg/view-service/view_service.py" "$T/container/app/vs/view_service.py"

# A two-file order writes exactly those two; the identical third file is untouched.
printf 'two-renderer\n' >"$T/repo/view-service/renderer-canary-sse.html"; printf 'two-python\n' >"$T/repo/view-service/view_service.py"
git -C "$T/repo" commit -qam two-files; C2W="$(git -C "$T/repo" rev-parse HEAD)"; (cd "$T/repo" && "$TOOL" package "$C2W" "$T/two-pkg")
: >"$T/container/docker.log"
OUT="$(cd "$T/repo" && "$TOOL" install "$T/two-pkg" --expected-commit "$C2W" --container fixture --backup-root "$T/backups" --write-only renderer-canary-sse.html --write-only view_service.py --go)"
grep -q '^SKIP name=view_page_store.py reason=not-authorized-identical$' <<<"$OUT"
[ "$(grep -c '^cp .*:/tmp/t109-' "$T/container/docker.log")" -eq 4 ]
OUT="$(cd "$T/repo" && "$TOOL" install "$T/static-pkg" --expected-commit "$C3" --container fixture --backup-root "$T/backups" --write-only renderer-canary-sse.html --write-only view_service.py --go)"

# All three different means no skip and three real staging writes. Restore C3 afterwards.
for f in renderer-canary-sse.html view_service.py view_page_store.py; do printf 'all-new-%s\n' "$f" >"$T/repo/view-service/$f"; done
git -C "$T/repo" commit -qam all-new; CALL="$(git -C "$T/repo" rev-parse HEAD)"
(cd "$T/repo" && "$TOOL" package "$CALL" "$T/all-pkg")
: >"$T/container/docker.log"
OUT="$(cd "$T/repo" && "$TOOL" install "$T/all-pkg" --expected-commit "$CALL" --container fixture --backup-root "$T/backups" --write-only renderer-canary-sse.html --write-only view_service.py --write-only view_page_store.py --go)"
if grep -q '^SKIP ' <<<"$OUT"; then exit 1; fi
[ "$(grep -c '^cp .*:/tmp/t109-' "$T/container/docker.log")" -eq 5 ]
OUT="$(cd "$T/repo" && "$TOOL" install "$T/static-pkg" --expected-commit "$C3" --container fixture --backup-root "$T/backups" --write-only renderer-canary-sse.html --write-only view_service.py --write-only view_page_store.py --go)"
if grep -q '^SKIP ' <<<"$OUT"; then exit 1; fi

# A mid-copy failure restores all three pre-images and their cache snapshot.
BEFORE="$(find "$T/container/app/vs" -type f -exec sha256sum {} + | sort | sha256sum)"
# Build a two-file delta: renderer is skipped, view_service then fails, while
# view_page_store is also different. Rollback must still restore the complete pre-image.
git -C "$T/repo" cat-file blob "$C3:view-service/renderer-canary-sse.html" >"$T/repo/view-service/renderer-canary-sse.html"
printf 'page-fail-v4\n' >"$T/repo/view-service/view_page_store.py"
printf 'python-v4\n' >"$T/repo/view-service/view_service.py"
git -C "$T/repo" commit -qam python-v4; C4="$(git -C "$T/repo" rev-parse HEAD)"
(cd "$T/repo" && "$TOOL" package "$C4" "$T/fail-pkg")
touch "$T/container/fail-once"
if OUT="$(cd "$T/repo" && "$TOOL" install "$T/fail-pkg" --expected-commit "$C4" --container fixture --backup-root "$T/backups" --write-only view_service.py --write-only view_page_store.py --go)"; then exit 1; fi
grep -q '^SKIP name=renderer-canary-sse.html reason=not-authorized-identical$' <<<"$OUT"
AFTER="$(find "$T/container/app/vs" -type f -exec sha256sum {} + | sort | sha256sum)"
[ "$BEFORE" = "$AFTER" ]

# Cache absence is a valid state created between removal and regeneration.
rm -rf "$T/container/app/vs/__pycache__"
OUT="$(cd "$T/repo" && "$TOOL" install "$T/static-pkg" --expected-commit "$C3" --container fixture --backup-root "$T/backups" --write-only renderer-canary-sse.html --go)"
grep -q '^CACHE_BACKUP state=absent$' <<<"$OUT"
grep -q 'restart=0' <<<"$OUT"

# A service that remains unhealthy must not hide successful file restoration.
BEFORE="$(cd "$T/container/app/vs" && sha256sum renderer-canary-sse.html view_service.py view_page_store.py | sha256sum)"
printf 'python-v5\n' >"$T/repo/view-service/view_service.py"
git -C "$T/repo" commit -qam python-v5; C5="$(git -C "$T/repo" rev-parse HEAD)"
(cd "$T/repo" && "$TOOL" package "$C5" "$T/unhealthy-pkg")
echo 999 >"$T/container/health-delay"
if OUT="$(cd "$T/repo" && VSR_HEALTH_ATTEMPTS=2 "$TOOL" install "$T/unhealthy-pkg" --expected-commit "$C5" --container fixture --backup-root "$T/backups" --write-only view_service.py --write-only view_page_store.py --go 2>&1)"; then exit 1; fi
grep -q '^RESTORED_DIGESTS ok files=3$' <<<"$OUT"
grep -q '^HEALTH_TIMEOUT ' <<<"$OUT"
if grep -q '^ROLLED_BACK$' <<<"$OUT"; then exit 1; fi
grep -q '^ROLLBACK_INCOMPLETE restore=0 digests=0 health=1$' <<<"$OUT"
AFTER="$(cd "$T/container/app/vs" && sha256sum renderer-canary-sse.html view_service.py view_page_store.py | sha256sum)"
[ "$BEFORE" = "$AFTER" ]
echo "PASS view-service release core"
