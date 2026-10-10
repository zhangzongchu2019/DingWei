# TS3 guard R3: worker wiring, pre-dispatch cancellation and fixed test gate

Recorded UTC: 2026-10-10T08:02:36.866492+00:00
Parent: c4c2ce19c37067dc68f888e10bb2f1b7237d87bb. R2 review 30b4156f028e7d8b98940433afe4fc1b7990e0d1 was independently fetched/read and verified: 8947B, SHA256 6d4e863e0622318802b4c7f79f12985eb6cf7633bf434ab7bb4858948da8d14e. R2 F1-F4 scoped code PASS; G1 tests REWORK, G2/G3 registered. No new candidate is approved by the old review.

## G1

The actual delivery_worker_loop is now extracted via AST and exercised with a fake inbox whose claim blocks in a synthetic thread. Cancellation before handoff must release the claim and never invoke process_inbound_message. Reverting the worker call to direct to_thread(claim_next) gives one FAIL, zero ERROR. This verifies real function wiring, not only a source-string check or the helper in isolation.

## G2

The pre-dispatch phase after claim handoff is enclosed in a CancelledError handler: mode/idle check, busy release, busy acknowledgement, and cooldown. Recovery creates/shields the exact inbox.release thread task before propagating cancellation. The handler ends before process_inbound_message is entered; cancellation in/after that call never triggers this automatic release. This avoids indiscriminate replay of potentially injected work.

Precision: successful busy release already changes processing to received; cancelling a subsequent acknowledgement/sleep does not itself restore processing. New recovery there is idempotent. The risky states are a cancelled idle check and an unfinished release. Existing release is scoped by session/ID and only affects processing.

New worker tests verify cancellation during blocked idle checking restores received without injection, and cancellation after entering process_inbound_message does not requeue. Removing pre-dispatch cancellation release is detected as FAIL, zero ERROR.

Limits: synchronous threads are not terminated. Claim/release completion may block cleanup; a second cancellation during recovery can interrupt the waiter, and storage failure remains first-error rather than guaranteed recovery. Injection/adapter side effects, actual SQLite/Hub redelivery behavior and event/view output remain unproved. No blanket inbox reset or production operation is authorized.

## G3

scripts/run-build-tests.sh explicitly lists all four AST helper regression files before existing deploy/view tests (20 Python files + one shell file). Canonical invocation is python3 -m unittest -q with explicit paths; it cannot omit the supervisor file merely because it sits outside tests/. All four files also have __main__ unittest entrypoints so direct execution runs tests. deploy/test_build_entrypoints.py checks each path is present exactly once and each standalone entry is wired. No helper startup module is imported.

Validation: helper 25 tests PASS; build-entrypoint 9 tests PASS. Complete canonical gate rc0, BUILD_TESTS_OK python=20 shell=1 (202 Python test cases total plus shell release-core fixtures). These are local test fixtures/subprocesses and local service tests; no real model, Hub, owner session, production PG or service deployment was used. Existing fixture/source ResourceWarnings did not fail tests and were not changed. All direct helper script invocations ran nonzero test counts. Worker-wiring and pre-dispatch-release reverse mutations each fail with zero errors. sh -n and git diff --check pass.

Review scope: only G1 test wiring, G2 pre-dispatch cancellation phase, G3 canonical runner/entrypoint tests and test __main__ additions. Original guide/reconnect/supervisor source is unchanged relative to R2. Root authored R3; independent per-artifact qualification and accepted delta findings remain required before integration. The previous R2 candidate archive stays historical. No root cause claim, deployment or runtime GO.
