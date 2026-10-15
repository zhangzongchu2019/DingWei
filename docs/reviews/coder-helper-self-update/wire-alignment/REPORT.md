# Helper health/lock/retry alignment — bounded author receipt

Final runtime/test source `8f2adda26ba4e3adb6034b8d0d8fc46ac24d1f88`, tree `4ec5a5a706563c8c462b84fd635ee25515626986`. It follows helper health/receipt commit `8751be3d2787e2a6903aaf81c44f387455ea7951` and normal docs-only merge e49731e preserving leader authoritative R3 amendment3134574. An initial push8751 was rejected because that amendment advanced the remote; corrected by normal merge, no force/reset. My premature pushed-wording message was corrected immediately; final source push/readback matched8f2.

## Runtime delta

- Health uses a fresh helper nonce and mounted immutable runtime-lock SHA. Ordinary receipts carry the validated lock; rollback receipts derive it from that stable mounted file.
- Prepare fsyncs a private slot-local binding of the six canonical job fields and verified lock before arming. Reopened health checks this against pending tuple and mounted lock. Missing/changed/wrong-job metadata or changed/unreadable lock is lock_changed. Guard journal and package format are unchanged; generated marker is private runtime state, not a package source input.
- Confirm requires trusted source, latest nonce, canonical tuple/runtime version, echoed lock, and still-matching slot/job/mounted binding before guard confirmation.
- Healthy pending retries every5 seconds with fresh nonces, including after dropped confirmation; a new updater on reconnect keeps the same durable pending job and supersedes old nonces. No missing-job rollback on manual factory start.

## Author checks on final source

- Isolated `PYTHONPATH=tools/sessionhelper locked-python -B -m unittest -v tools/sessionhelper/tests/test_self_update_offer.py`:19 tests, rc0,12.074s. Includes dropped confirm/retry/reopened updater, stale nonce, all tuple fields, wrong lock, lock changed after health, missing/wrong-job slot binding and rollback receipt lock. This uses loopback HTTP/archive plus controlled async send fixtures, not a real Hub or Docker.
- Isolated `locked-python -B -m unittest discover -s tools/sessionhelper/tests -v`:695 tests, rc0,352.270s on exact final source.
- Isolated canonical `bash scripts/run-build-tests.sh`:rc0, BUILD_TESTS_OK python=42 shell=2.
- Surgical local module overlays: remove confirm lock check => intended unittest assertions rc1; replace periodic retry with indefinite wait => bounded awaited second-health TimeoutError rc1; remove pending job-lock check => UpdateRejected-not-raised AssertionError rc1. No compile-error substitutes. Product files restored/unchanged during overlays.

Environment uses locked websockets15.0.1/pexpect4.9.0 venv, fresh HOME and short TMPDIR, seat SH_/DW_/DINGWEI_ variables stripped, existing Go caches reused. No cache purge.

## Failures and exact attribution retained

The first nonisolated targeted run executed18 successful cases but module teardown detected a live real-seat inbox change; whole command rc1, not PASS. A prospective slot-binding run failed1 because the old assertion expected confirmation_mismatch while stricter send-time enforcement correctly reported lock_changed; final explicit assertion now pins lock_changed. Prior8751 full694 tests rc0/352.097s and canonical0 remain prior-object evidence, not substitutes for final8f2 gates.

## Scope

This is a helper-only author delta, not R3/R4 closure or whole-feature acceptance. Core must use post-INSTALL/activation new-connection boundary, reject offered/downloaded and terminal failures, and idempotently echo a confirmed same job with fresh nonce/lock without another mutation/audit/activation; capability required on every offer/replay. Actual helper serializer-to-final-Hub and actual download/install/restart/confirm plus real Linux Docker gates remain OPEN. No package upload, deployment, service restart or real-Agent action. Frozen design untouched.
