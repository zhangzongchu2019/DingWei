#!/bin/sh
# Read-only owner-selected Mac app metadata. No Python, login, app start or state writes.
set -eu
if [ "$(/usr/bin/uname -s)" != Darwin ]; then
    echo 'Run this read-only probe on the actual Mac.' >&2
    exit 1
fi
if [ "$#" -ne 1 ]; then
    echo 'Usage: sh probe-desktop-metadata.sh /absolute/selected/Codex.app' >&2
    exit 1
fi
task_app="$1"
case "$task_app" in
    /*/Codex.app|/*/ChatGPT.app) ;;
    *) echo 'Select the actual installed app using an absolute path.' >&2; exit 1 ;;
esac
task_plist="$task_app/Contents/Info.plist"
if [ -L "$task_app" ] || [ ! -d "$task_app" ] || [ -L "$task_plist" ] || [ ! -f "$task_plist" ]; then
    echo 'Selected app metadata is unavailable; do not infer readiness.' >&2
    exit 1
fi
# Extract only the three whitelisted values, never dump the full plist.
for task_key in CFBundleIdentifier CFBundleShortVersionString CFBundleVersion; do
    task_value="$(/usr/libexec/PlistBuddy -c "Print :$task_key" "$task_plist" 2>/dev/null)" || {
        echo 'Selected app lacks required version metadata.' >&2; exit 1;
    }
    if [ -z "$task_value" ] || [ "${#task_value}" -gt 128 ] ||
        [ "$(printf '%s' "$task_value" | LC_ALL=C tr -d '[:cntrl:]')" != "$task_value" ]; then
        echo 'Selected app metadata is invalid.' >&2; exit 1
    fi
    case "$task_key" in
        CFBundleIdentifier) task_bundle_id="$task_value" ;;
        CFBundleShortVersionString) task_version="$task_value" ;;
        CFBundleVersion) task_build="$task_value" ;;
    esac
done
printf 'platform=Darwin\n'
printf 'macos_version=%s\n' "$(/usr/bin/sw_vers -productVersion)"
printf 'process_architecture=%s\n' "$(/usr/bin/uname -m)"
printf 'application=%s\n' "${task_app##*/}"
printf 'bundle_id=%s\nversion=%s\nbuild=%s\n' "$task_bundle_id" "$task_version" "$task_build"
printf 'qualification=metadata_only_not_GUI_model_or_idle_wakeup_proof\n'
