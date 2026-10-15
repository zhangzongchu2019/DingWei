# DW123 helper update — first core implementation slice

Base r12 `6c4600478a4c7c769cb6adaa1024e4504208f39b`; frozen design `fc3e4b4539566d0e2dfeacc015264c51ef2cc70f`.

This slice adds the Git-object source packager, source-bootstrap installer/preflight and stable stdlib guard/launcher. It does not yet replace the running helper's old provision action or bump the shipped version; offer integration/version0.2.3.1 and full Hub E2E are subsequent work. It is not a complete release candidate.

- Packager exports only selected immutable commit blobs, rejects a dirty helper source input, forbids tracked private-state files even outside the release allowlist, scans content and creates deterministic archive/manifest. Package SHA is outside its own manifest; runtime-lock hash and every member's SHA/size are inside. No source package upload.
- Stable root imports only stdlib guard, launches candidate in a separate process/import path. Writable private update journal contains current/previous/pending and terminal tuple pins; two starts/120s are durable, pending outranks retained current, confirmation promotes, failure preserves current. Manual no-job start selects factory and does not enter update timeout. Auxiliary invocations do not consume trial budget.
- Bootstrap validates the release directory, existing read-only mount/restart policy and pinned dependency versions. Owner-authorized preflight writes a non-secret container-state marker, performs a restart only with explicit flag, checks survival/owner and leaves a ten-minute container+manifest-bound proof for install. Host source replacement backs up only replaced source/launcher files, never a seat tree/private state. It does not change Docker entry. Backup remains local; restore is explicit.

## Executed evidence

Locked Python environment: `/data/ai-dev-data/helper-deps-lock-venv/bin/python`, websockets15.0.1/pexpect4.9.0.

`PYTHONPATH=tools/sessionhelper .../python -B -m unittest -v tools/sessionhelper/tests/test_update_release_core.py`: **16 tests rc0**. Real Git repositories/archives, real Python child launch and failed/hung candidates are exercised. Read-only filesystem mode is a local fixture; the preflight test executes real Python state/marker/dependency probes while Docker inspect/restart is controlled. Neither is a real Docker PASS.

`.../python -B tools/sessionhelper/tests/update_release_withdrawals.py --evidence-dir docs/reviews/coder-helper-self-update`: removes pending-first guard or secret-content check in disposable copies; both corresponding tests fail **rc1**. Original source remains intact.

Existing full helper discovery and canonical build were started separately under isolated HOME/TMPDIR. Their final results are not claimed in this first report until the logs finish; subsequent receipt records the exact executed state.

## Still required

Runtime trusted-offer/fetch/stage integration, version0.2.3.1, strict Hub health-confirmation hookup, actual Hub E2E, actual Linux Docker unless-stopped/entry/read-only mount/restart/rollback tests and final whole-suite gates. Local Docker API returns permission denied; no workaround, privileged access or production operation attempted. macOS is manual-only/deferred. No real Agent changes, deployment or artifact publication.
