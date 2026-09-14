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
(cd "$T/repo" && "$TOOL" identify-online "$T/pkg" "$C2") | grep -q '^IDENTIFIED '

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
printf 'x' >>"$T/old/MANIFEST.sha256"
if (cd "$T/repo" && "$TOOL" verify-package "$T/old") >/dev/null; then exit 1; fi
echo "PASS view-service release core"
