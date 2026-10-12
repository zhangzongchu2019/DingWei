#!/bin/sh
# Build a native development candidate on the owner's Mac. No Hub/AI calls.
set -eu
if [ "$(uname -s)" != Darwin ]; then
    echo 'This entry must run on macOS; it does not cross-build a Mac binary on Linux.' >&2
    exit 1
fi
if [ "$#" -lt 1 ] || [ "$#" -gt 2 ]; then
    echo 'Usage: sh build-desktop-candidate.sh /absolute/new/private-output [selected-Codex.app]' >&2
    exit 1
fi
task_source_root="$(CDPATH= cd -- "$(dirname "$0")/../../../.." && pwd -P)"
task_output="$1"
task_desktop_app="${2:-}"
case "$task_output" in
    /*) ;;
    *) echo 'Output must be an absolute fresh directory.' >&2; exit 1 ;;
esac
case "$task_output" in
    /|"$task_source_root"|"$task_source_root"/*)
        echo 'Use a new output directory outside the source tree.' >&2; exit 1 ;;
esac
if [ -e "$task_output" ] || [ -L "$task_output" ]; then
    echo 'Output already exists; preserve it and choose a fresh directory.' >&2
    exit 1
fi
task_python=""
for task_name in python3.13 python3.12 python3.11 python3; do
    task_found="$(command -v "$task_name" || true)"
    if [ -n "$task_found" ] && "$task_found" -c 'import sys;sys.exit(0 if sys.version_info >= (3,11) else 1)'; then
        task_python="$task_found"
        break
    fi
done
if [ -z "$task_python" ]; then
    echo 'Python 3.11+ required. Install from https://www.python.org/downloads/macos/ and rerun.' >&2
    exit 1
fi
task_output="$("$task_python" -c 'from pathlib import Path;import sys;print(Path(sys.argv[1]).resolve())' "$task_output")"
case "$task_output" in
    /|"$task_source_root"|"$task_source_root"/*)
        echo 'Resolved output must stay outside the source tree.' >&2; exit 1 ;;
esac
if [ -e "$task_output" ] || [ -L "$task_output" ]; then
    echo 'Resolved output already exists; preserve it.' >&2; exit 1
fi
# A source archive is accepted only with its accompanying fixed member manifest.
# A Git checkout must be clean; do not build a dirty developer tree on the PM.
if [ -e "$task_source_root/.git" ]; then
    [ -z "$(git -C "$task_source_root" status --porcelain)" ] || { echo 'Source Git tree is dirty.' >&2; exit 1; }
    task_source_commit="$(git -C "$task_source_root" rev-parse HEAD)"
else
    "$task_python" -B "$task_source_root/scripts/verify-sessionhelper-source.py" "$task_source_root"
    task_source_commit="$("$task_python" -c 'import json,sys;from pathlib import Path;print(json.loads((Path(sys.argv[1])/"SOURCE-MEMBER-PINS.json").read_text())["source_commit"])' "$task_source_root")"
fi
# App selection/metadata must pass before creating output, downloading deps or
# compiling. Source was already verified; no desktop login/history is read.
if [ -n "$task_desktop_app" ]; then
    task_desktop_metadata="$(PYTHONPATH="$task_source_root/tools/sessionhelper" "$task_python" -B -m sessionhelper.desktop_host --require-app --desktop-app "$task_desktop_app")"
else
    task_desktop_metadata="$(PYTHONPATH="$task_source_root/tools/sessionhelper" "$task_python" -B -m sessionhelper.desktop_host --require-app)"
fi
umask 077
mkdir -p -m 700 "$task_output"
printf '%s\n' "$task_desktop_metadata" > "$task_output/desktop-metadata-before-build.json"
"$task_python" -m venv "$task_output/venv"
task_venv_python="$task_output/venv/bin/python"
"$task_venv_python" -m pip install -r "$task_source_root/tools/sessionhelper/requirements.txt" > "$task_output/dependencies-private.log" 2>&1
"$task_venv_python" -B -m PyInstaller --noconfirm --distpath "$task_output/bin" --workpath "$task_output/work" \
    "$task_source_root/tools/sessionhelper/sessionhelper.spec" > "$task_output/build-private.log" 2>&1
if [ -n "$task_desktop_app" ]; then
    "$task_venv_python" -B "$task_source_root/scripts/verify-sessionhelper-frozen.py" "$task_output/bin/sessionhelper" \
        --desktop-app "$task_desktop_app" > "$task_output/local-smoke.jsonl" 2>&1
    "$task_output/bin/sessionhelper" --desktop-probe --desktop-app "$task_desktop_app" > "$task_output/desktop-metadata.json"
else
    "$task_venv_python" -B "$task_source_root/scripts/verify-sessionhelper-frozen.py" "$task_output/bin/sessionhelper" \
        > "$task_output/local-smoke.jsonl" 2>&1
    "$task_output/bin/sessionhelper" --desktop-probe > "$task_output/desktop-metadata.json"
fi
"$task_venv_python" -B "$task_source_root/scripts/record-sessionhelper-macos-build.py" \
    --source-root "$task_source_root" --source-commit "$task_source_commit" --output "$task_output"
"$task_output/bin/sessionhelper" --version
printf 'Native candidate and private metadata: %s\n' "$task_output"
echo 'Local smoke is not native desktop notification verification. No Hub enrollment or model login was performed.'
