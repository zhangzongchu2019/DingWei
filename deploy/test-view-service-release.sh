#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TOOL="$ROOT/deploy/view-service-release.sh"
T="$(mktemp -d)"; trap 'rm -rf "$T"' EXIT
git init -q "$T/repo"; git -C "$T/repo" config user.email test@example.invalid; git -C "$T/repo" config user.name test
mkdir -p "$T/repo/view-service"
for f in renderer-canary-sse.html view_service.py view_page_store.py; do printf 'v1-%s\n' "$f" >"$T/repo/view-service/$f"; done
git -C "$T/repo" add .; git -C "$T/repo" commit -qm v1; C1="$(git -C "$T/repo" rev-parse HEAD)"
printf 'v2\n' >"$T/repo/view-service/view_service.py"; git -C "$T/repo" commit -qam v2; C2="$(git -C "$T/repo" rev-parse HEAD)"

(cd "$T/repo" && "$TOOL" package "$C2" "$T/pkg" && "$TOOL" verify-package "$T/pkg" "$C2")
rm "$T/pkg/PROVENANCE.json"
OUT="$(cd "$T/repo" && "$TOOL" identify-online "$T/pkg" "$C2")"
grep -q '^IDENTIFIED ' <<<"$OUT"

# An unrelated commit leaves the deployable tuple unchanged: report every
# matching commit and refuse to guess which one was deployed.
printf 'unrelated\n' >"$T/repo/README"; git -C "$T/repo" add README; git -C "$T/repo" commit -qm unrelated
if (cd "$T/repo" && "$TOOL" identify-online "$T/pkg" HEAD) >"$T/ambiguous"; then exit 1; fi
grep -q '^AMBIGUOUS ' "$T/ambiguous"

printf 'manual bypass\n' >"$T/pkg/view-service/view_service.py"
if (cd "$T/repo" && "$TOOL" identify-online "$T/pkg" HEAD) >"$T/not-in-repo"; then exit 1; fi
grep -q '^NOT_IN_REPO ' "$T/not-in-repo"

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
if (cd "$T/repo" && "$TOOL" identify-online "$T/old" parallel) >"$T/wrong-ref"; then exit 1; fi
grep -q '^NOT_IN_REPO ' "$T/wrong-ref"
OUT="$(cd "$T/repo" && "$TOOL" identify-online "$T/old")"
grep -q '^IDENTIFIED ' <<<"$OUT"
if grep -q '^SCAN_REF ' <<<"$OUT"; then exit 1; fi
OUT="$(cd "$T/repo" && "$TOOL" identify-online "$T/old" --verbose)"
grep -q '^SCAN_REF ' <<<"$OUT"

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
if [ "$op" = exec ]; then
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
(cd "$T/repo" && "$TOOL" install "$T/install-pkg" --expected-commit "$C2" --container fixture --backup-root "$T/backups") | grep -q 'PRECHECK_ONLY writes=0'
AFTER="$(find "$T/container/app/vs" -type f -exec sha256sum {} + | sort | sha256sum)"
[ "$BEFORE" = "$AFTER" ]
echo 2 >"$T/container/health-delay"
OUT="$(cd "$T/repo" && "$TOOL" install "$T/install-pkg" --expected-commit "$C2" --container fixture --backup-root "$T/backups" --go)"
grep -q '^HEALTHY attempts=3 ' <<<"$OUT"
grep -q '^INSTALLED ' <<<"$OUT"
[ -e "$T/container/app/vs/__pycache__/view_page_store.cpython-311.pyc" ]
[ -s "$T/container/restarts" ]
BACKUP="$(find "$T/backups" -mindepth 1 -maxdepth 1 -type d)"
[ "$(find "$BACKUP/pre" -type f | wc -l)" -eq 4 ]
[ "$(find "$BACKUP/forward" -type f | wc -l)" -eq 5 ]

# Renderer-only changes are hot-updated without restarting viewservice.
RESTARTS="$(wc -l <"$T/container/restarts")"
printf 'renderer-v3\n' >"$T/repo/view-service/renderer-canary-sse.html"
git -C "$T/repo" commit -qam renderer-v3; C3="$(git -C "$T/repo" rev-parse HEAD)"
(cd "$T/repo" && "$TOOL" package "$C3" "$T/static-pkg")
(cd "$T/repo" && "$TOOL" install "$T/static-pkg" --expected-commit "$C3" --container fixture --backup-root "$T/backups" --go) | grep -q 'restart=0'
[ "$(wc -l <"$T/container/restarts")" -eq "$RESTARTS" ]

# A mid-copy failure restores all three pre-images and their cache snapshot.
BEFORE="$(find "$T/container/app/vs" -type f -exec sha256sum {} + | sort | sha256sum)"
printf 'python-v4\n' >"$T/repo/view-service/view_service.py"
git -C "$T/repo" commit -qam python-v4; C4="$(git -C "$T/repo" rev-parse HEAD)"
(cd "$T/repo" && "$TOOL" package "$C4" "$T/fail-pkg")
touch "$T/container/fail-once"
if (cd "$T/repo" && "$TOOL" install "$T/fail-pkg" --expected-commit "$C4" --container fixture --backup-root "$T/backups" --go) >/dev/null; then exit 1; fi
AFTER="$(find "$T/container/app/vs" -type f -exec sha256sum {} + | sort | sha256sum)"
[ "$BEFORE" = "$AFTER" ]

# Cache absence is a valid state created between removal and regeneration.
rm -rf "$T/container/app/vs/__pycache__"
OUT="$(cd "$T/repo" && "$TOOL" install "$T/static-pkg" --expected-commit "$C3" --container fixture --backup-root "$T/backups" --go)"
grep -q '^CACHE_BACKUP state=absent$' <<<"$OUT"
grep -q 'restart=0' <<<"$OUT"
echo "PASS view-service release core"
