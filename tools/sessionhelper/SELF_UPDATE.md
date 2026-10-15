# Linux container helper source updates

This implementation follows frozen design `fc3e4b4539566d0e2dfeacc015264c51ef2cc70f`.
It is not yet a release approval: actual Hub interoperability and real Docker restart/rollback acceptance are required. No package has been uploaded and no real Agent has been changed by development tests.

## Build from Git

Use a clean, isolated checkout of the approved full commit. Run its packager:

```sh
python3 tools/sessionhelper/package_release.py --repo "$PWD" --commit '<approved full commit>' --output /private/build/helper.tgz
```

The command reads immutable Git objects, refuses dirty helper input and prints commit/tree, version, size, package SHA256 and lock SHA256. It neither installs dependencies nor uploads anything. Keep the independently published package SHA outside the archive. The archive contains `helper-release/` with factory Python source, a stable standard-library launcher/guard, verifier, bootstrap and lock/manifest. No PyInstaller/native build is used. Files must pass both the allowlist and private-state scan; key/credential/env/inbox/ledger data must never be included.

Only an authorized release publication may make this upgrade package and its instructions available through the configured file service. General source exchange remains Git-only. The Console selects an operations-registered catalog release; it does not accept an arbitrary URL.

## One manual bootstrap by the owner executor

The default is a file-service archive plus an independently supplied full SHA256. Git exact-commit build above is the alternative. Do not use the old helper's downloader for this first upgrade.

The existing container command remains `tini → bash → source /run/secrets/sessionhelper_env → /opt/dingwei/sessionhelper-venv/bin/python -m sessionhelper`. `PYTHONPATH=/opt/dingwei/sessionhelper-src`; that source mount is **read-only in the container**. Only the owner replaces its host-side contents. Upgraded source slots/current/previous/journal live under the resolved container `SH_STATE_DIR/update` (default expanded `~/.dingwei/update`). Never put private state in the source archive.

Fill the approved release URL/hash, actual container name and host mount path before executing. These are release-specific values, not guesses. Run on the container host with authorized Docker access:

```sh
set -eu
umask 077
UPGRADE_URL='<approved HTTPS upgrade-package URL>'
UPGRADE_SHA='<independently supplied 64-character SHA256>'
UPGRADE_CONTAINER='<actual container name>'
UPGRADE_MOUNT='<host source directory mounted at /opt/dingwei/sessionhelper-src>'
UPGRADE_BACKUP='<new private local source-backup directory outside the mount>'
UPGRADE_STAGE=$(mktemp -d)
curl --fail --silent --show-error --proto '=https' --max-redirs 0 --max-time 180 "$UPGRADE_URL" -o "$UPGRADE_STAGE/helper.tgz"
printf '%s  %s\n' "$UPGRADE_SHA" "$UPGRADE_STAGE/helper.tgz" | sha256sum --check --status
# This is the verified, operator-built archive with an allowlisted manifest.
tar -xzf "$UPGRADE_STAGE/helper.tgz" -C "$UPGRADE_STAGE" --no-same-owner
python3 "$UPGRADE_STAGE/helper-release/bootstrap.py" --preflight --check-restart-persistence --container "$UPGRADE_CONTAINER" --mount "$UPGRADE_MOUNT"
python3 "$UPGRADE_STAGE/helper-release/bootstrap.py" --install --container "$UPGRADE_CONTAINER" --mount "$UPGRADE_MOUNT" --backup "$UPGRADE_BACKUP"
docker restart "$UPGRADE_CONTAINER"
docker exec "$UPGRADE_CONTAINER" /opt/dingwei/sessionhelper-venv/bin/python -m sessionhelper --version
```

First ensure the Agent is idle and the source mount is not shared with unselected running Agents; stop and coordinate if it is shared. Preflight checks the existing read-only source mount and `unless-stopped` policy, pinned venv dependencies, writable state ownership/private mode and an actual marker surviving a restart. It prints the actual resolved state path for the execution receipt. A ten-minute container+manifest-bound proof allows the next install. Any mismatch stops with a fixed rejection; no container recreation, entry amendment, online pip or automatic short-path fallback occurs.

Install backs up only source/launcher files into the private local backup directory and records a restore receipt. Keys/env/inbox/ledger remain in their existing locations; never upload this backup or private state. It replaces the mounted source with stable launcher/guard plus factory code. Self-updates never replace that stable root. Initial factory startup has no pending job and does not wait for a nonexistent update confirmation.

Owner verifies all four manual acceptance points: authenticated reconnect self-reports **0.2.3.1**, MCP socket exists, a message roundtrip works, and A6 disk metrics are normal. Installed files or a version-only CLI command are not sufficient evidence.

If manual replacement must be undone:

```sh
python3 "$UPGRADE_STAGE/helper-release/bootstrap.py" --restore --receipt "$UPGRADE_BACKUP/receipt.json"
docker restart "$UPGRADE_CONTAINER"
```

Verify original version and messaging again. Restore is local only. macOS v1 remains manual source replacement using the approved exact Git commit and existing launchd procedure; Linux bootstrap.py/automatic update is not a macOS installer or acceptance claim.

## Subsequent updates

Only a validated local-Hub system offer may cause download. The helper pins the actual system From through authenticated update-capable WebSocket response headers, because enrolled clients' local key may only be a placeholder. It checks assigned Agent name, immutable job/Agent/owner/release/version/hash tuple, expiry, origin and lock hash. Download sends no Agent credential, ignores ambient proxy credentials, follows no redirects and checks SHA before source extraction. Dependency change is `lock_changed`; no online installation or wheelhouse.

Pending source is staged under the writable state directory. Guard selects an armed pending trial before current/previous/factory, durably records at most two starts and a 120-second deadline, and runs candidate only in a separate process. It preserves the original business cwd, isolating code selection through the import path. Matching Hub job+hash+fresh-session+nonce health confirmation promotes pending to current. Failed import, exit or timeout marks pending terminal and keeps current for the next Docker restart. Terminal jobs cannot rearm. Manual factory/no-job startup has no update deadline.

SHA failure rejects before activation. Lost receipt before a restart request can resend status/request restart without redownloading; once restart has been durably requested, a duplicate job never installs/restarts again. Hub receipts and fresh endpoint version, not an installed ACK alone, determine success.
