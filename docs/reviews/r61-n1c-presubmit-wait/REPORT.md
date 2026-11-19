# S-R61-N1c presubmit wait

## Scope and anchors

- Branch: `togpt60lunazzc/r61-n1c-presubmit-wait`.
- Base: N-1b `bf0b405c146b6bd2513c9e80ba1eee9feaa29842` (tree `f2241bab` as reported by the lead).
- Frozen implementation source: `25af4ce1300dfd55ecd9b8869fecb593a003076e` (tree `dd82c264115af9fc439a3138b613c622b24dc275`); linear commits are based directly on the N-1b tip.
- Required main anchor for outbound self-check: `206d41b26dab646c0e50a111eed5a45ccf7c72f6`; `origin/preprod` remains at `0.2.1.103` and is not a target.
- No Hub product files, migrations, VERSION, main, deployment, or live environment were changed.

## Behavior

The Codex driver now retries only pre-submit read-only validation transport/server errors: timeout, recognized network errno, or server RPC errors on initialize/thread/read/thread/resume. Authentication, thread/cwd/direct-input, daemon identity, private endpoint, and other recognized validation mismatches fail closed, remain at the FIFO head with an operator alert, and do not rotate the bound thread. The retry loop uses capped 1/2/5/10/30 second backoff without a retry deadline. A thread/start or turn/start error is not treated as a retryable read-only validation result.

Before waiting, the existing `BusyAdmission` hook must persist `busy_wait_started_at`; inability to persist leaves the input queued locally and does not write a terminal failed receipt. The same command stays at the FIFO head. The existing submission CAS persists `submission_may_have_started_at` before `turn/start`; after restart, marker-null busy waits restore, while a set submission marker restores as `admission_unknown` and is never automatically submitted again.

After the durable wait marker is present, a thread-safe helper callback queues ACK v2 `busy`, `retryable=true`, `reason=presubmit_validation_wait` for the websocket ACK loop. This is not an ACK outbox terminal phase and does not alter terminal ACK validation. Hub handling follows the receipt boundary: before `DeliveredAt`, busy uses the existing busy→queued path; after `DeliveredAt`, Hub keeps `delivered` and does not schedule another attempt. In the latter case, the helper's durable FIFO owns continued waiting and resumes the same delivery after recovery. The adapter watchdog recognizes the explicit validation hold even when driver health is false, so it does not convert a known mismatch into terminal rejection or release later FIFO items. Operator resolution requires a safe helper restart/revalidation.

On the first Codex delivery, `DriverAdapter.handle_delivery()` now persists/enqueues the command before starting driver initialization. The command worker reaches `CodexProtocolDriver.deliver()`, where transient initialize failures enter the same durable pre-submit wait as later read-only validation failures. A healthy RPC socket is not considered initialized after a failed initialize response. This closes the prior `start()`-before-enqueue path that let `delivery_worker_loop` call `reject_unstarted` and terminalize the delivery.

## Verification

Locked interpreter: `/tmp/lockvenv/bin/python3`; environment was locally isolated for focused helper runs.

| Check | Result |
| --- | --- |
| Focused regression set | 157 tests, 0 failures after final hold and configured-thread retry changes. Raw log: `evidence/focused-final-hold-regressions.log` (SHA256 `82afd4ead8fb0cb2736bbc56d20c8e13bf6133099d7d45be3d2a45e3e62fe534`). |
| Frozen-source build gate | `SOURCE_COMMIT=25af4ce1300dfd55ecd9b8869fecb593a003076e`, isolated `env -i`, Go 1.22.2, locked Python 3.12.3; `BUILD_TESTS_OK helper_profiles=2 python=27 shell=2`, inventory 97. Claude and Codex profiles each 1281/1281, rc=0. Toolchain record: `evidence/toolchain.txt`; raw log: `evidence/build-test-only-final-rerun.log` (SHA256 in `SHA256SUMS`). |
| m8 + store, PG strict | `go test -p 1 -count=3 -timeout=45m -v ./internal/m8 ./internal/store`, `HUB_TEST_PG_STRICT=1`, rc=0. `internal/m8` 968.209s; `internal/store` 251.692s. PostgreSQL integration reported isolated `server_version_num=180003`. All three repetitions passed; no `interrupted (9)` and no failures. Skip records and conditions: `SKIPS.md`. Raw log: `evidence/m8-store-count3-final.log` (SHA256 in `SHA256SUMS`). |
| OpenSpec strict | 44 passed / 0 failed. Raw log: `evidence/openspec-strict-final.log` (SHA256 `00deedb25054eaeed804dcc843071547cc54a415fbd49e52ffe1de9e006e2ad4`). |
| G1–G4 mutations | All four killed by intended business assertions; no compile error or panic. See `evidence/mutations/*-final.log` and `SHA256SUMS`. An exploratory mutation at the adapter callback seam (which the direct driver test bypasses) survived; it is retained as `evidence/mutations/G4-wrong-seam-survived.log`. The actual G4 removes the durable lifecycle marker write and is killed at the missing `submission_may_have_started_at` assertion. |
| R1 first-delivery initialize regression | Baseline restored adapter failed both synthetic transient initialize cases at `N1C_STARTUP_TRANSIENT_MISSING_DURABLE_WAIT`; fixed source passes both. The test uses the real SessionHelper delivery worker, DriverAdapter, SQLite lifecycle, and fake RPC. Fixed log SHA256 `4d600ca4af8cae2f790a7126549756b4028843c32c2e6f3bcde696c1a3860312`; baseline log SHA256 `5ee55fdd2ae76deb8f55373d04d61cba7dc7bbb200e7e9a0c39bc6c6f1633095`; files under `evidence/r1/`. |
| Focused helper regression | `test_codex_stale_thread` + `test_dispatch_lifecycle`: 101 tests, rc=0. Raw log: `evidence/r1/focused-modules.log`. |
| G1 retry classification mutation | KILLED; `test_auth_stays_fail_closed_and_temporary_read_errors_retry_same_thread` failed on the retry-state assertion; `test_transient_validation_wait_is_durable_fifo_and_submits_once_after_recovery` also reported `N1C_RETRY_NOT_ENTERED`. |
| G2 transient validation terminalization mutation | KILLED; durable retry test failed at `N1C_RETRY_NOT_ENTERED`. |
| G3 busy-marker removal mutation | KILLED; lifecycle test failed at `N1C_BUSY_MARKER_NOT_PERSISTED`; Codex and Claude busy wait marker assertions also failed. |
| G4 submission-boundary persistence removal | KILLED; durable retry test failed because `submission_may_have_started_at` stayed null. |

Mutation logs are in `evidence/mutations/`; each mutation was applied in place for one isolated unittest process and restored immediately. No mutation produced a compile error or panic.

## Compatibility and remaining scope

- The earlier ACK-fixture compatibility check at the pre-R2 anchor is superseded for the post-`DeliveredAt` case; it does not model the normal queued-ACK-before-busy ordering and is not acceptance evidence for that case.
- Independent real old Hub `501e5b8a` plus real helper process trace at coder56 review tip `7bf230f1dfa6e2a8836ac7bf40a3ab093847f15b`, file `docs/reviews/r61-n1c-coder56/evidence/compat-result-trace.json`: queued ACK completed and `DeliveredAt` was recorded before busy; busy left Hub state `delivered` with attempts 1; after recovery the same delivery issued exactly one `turn/start` and sent injected ACK. This matches the corrected contract. The trace explicitly uses synthetic thread/read RPC and does not claim model completion.
- The four N-1b ACK compatibility probes remain in the accepted ancestor report; as before, those are helper-side probes with capability presence modeled in the connection. The additional old-Hub cell above exercises the old Hub websocket receiver.
- No Hub product code, migration, VERSION, main, preprod, deployment, or live endpoint was changed. The revoked N-3 review is excluded.

## Non-final gate history retained for context

- An earlier run at intermediate source `4af0091a...` was interrupted after review found that definitive identity mismatches needed to remain at the FIFO head. Its partial output is retained as `evidence/build-test-only-interrupted-4af0091a.log`; it is not a gate result.
- A full helper run at intermediate source `5dd67f96...` completed rc=1 with 17 failures in legacy assertions expecting terminal rejection for known pre-submit mismatches. The corrected fail-closed FIFO behavior and assertions pass the final focused and full helper gates. Raw failing log: `evidence/build-test-only-failed-5dd67f96.log` (SHA256 `4466b47efb54035b2e3f474d20284eaaa4bad257fe91418637595bc86b628c0f`).
- A `25af4ce1...` build attempt verified both helper profiles (1281/1281 each) but failed in Go integration tests because the isolated temporary filesystem ran out of space. The final rerun used the larger local volume for TMP while retaining isolated HOME; the rerun passed. Raw attempt: `evidence/build-test-only-no-space-25af.log` (SHA256 `6ef57f217e2d16b50ba7c294f3e48bf2ac428740c0daaf22752109be56b0627c`).


## Post-merge verification (2026-11-18)

The repaired N-1b source `b81737d0897c7edfea171371dba8ead6c1ef8f28` was merged into the original N-1c branch at `c22c4268016bd0c28e812028f4109613cd462728` (tree `65d669869260fa3c1228d5e0726398e8fddf36a2`). Merge parents are the originally received N-1c tip `6feca61c3055419238f05a01df33c7fa1150727c` and N-1b `b81737d0897c7edfea171371dba8ead6c1ef8f28`; the N-1b remote tip is an ancestor. The run used the merged tree, isolated `env -i`, Go 1.22.2, `/tmp/lockvenv/bin/python3`, separate HOME/TMP/GOCACHE, and no live Hub.

| Check | Result |
| --- | --- |
| `SOURCE_COMMIT=c22c4268016bd0c28e812028f4109613cd462728 bash build.sh --test-only` | rc=0; `BUILD_TESTS_OK helper_profiles=2 python=27 shell=2`; inventory 97. Claude and Codex each 1281/1281 (groups 1272+9), rc=0. Raw log: `evidence/post-merge-build-c22c426.log`. |
| m8 + store, PG strict | `go test -p 1 -count=3 -timeout=45m -v ./internal/m8 ./internal/store`, `HUB_TEST_PG_STRICT=1`; overall rc=1. m8 ran 981.951s with one failure in three repetitions: `TestSessionNameReconnectKeepsNameAndReleaseReuses`, `hub_test.go:3505`, `interrupted (9)` (first repetition only; second and third passed). This is on the Z-3 permitted list. No other m8 failure. Store passed all three repetitions, 252.535s. Aggregate top-level counts: m8 2975 PASS / 1 FAIL / 33 SKIP; store 1203 PASS / 0 FAIL / 15 SKIP. The skip set and conditions match `SKIPS.md` (48 top-level, 54 nested skip occurrences total). Raw log: `evidence/post-merge-m8-store-count3-c22c426.log`. |
| OpenSpec strict | 44 passed / 0 failed; rc=0. Raw log: `evidence/post-merge-openspec-strict-c22c426.log`. |

This is not an all-green m8 gate: the permitted flaky failure occurred once, so the aggregate Go command correctly returned rc=1. No failure outside the named Z-3 set occurred. The repaired N-1b merge does not change the N-1c helper source or the N-1c behavior under test.

## R1/R2 rework (2026-11-18)

R1 fixes the real first-delivery path: Codex initialization is deferred until after the durable command is enqueued and the adapter worker starts. The two-case regression covers `TimeoutError` and JSON-RPC `-32603`; on the pre-fix adapter both cases failed at the durable busy-wait assertion, while the fixed code passed and observed one `turn/start` per delivery, no `thread/start`, and no terminal failed ACK. Evidence is under `evidence/r1/`.

R2 supersedes the prior blanket busy→queued promise. Coder56's independently captured real-process trace at review tip `7bf230f1dfa6e2a8836ac7bf40a3ab093847f15b` demonstrates that the helper's queued ACK is confirmed first, so `DeliveredAt` is already non-null by the time the helper emits busy. The old Hub correctly preserves `delivered`; helper FIFO then resumes the same delivery and emits one `turn/start`. The change specification now covers both receipt states and requires no loss plus exactly one submission after recovery. No Hub behavior is changed.

The R1/R2 anchor and final gate results are recorded in the follow-up section below after rerunning against the frozen source.

## R1/R2 frozen-source verification (2026-11-18)

- Frozen source: `d8711290bf39c5f5b1305ab33663bf1e638474a9` (tree `92079421d08e0d97392dd7f24320903f067f27f2`). R1 product changes are limited to the helper adapter/driver path and its tests; R2 changes the specification only. `internal/m8`, Hub, and store product code are untouched.
- Build gate at this source: `SOURCE_COMMIT=d8711290bf39c5f5b1305ab33663bf1e638474a9 bash build.sh --test-only`, isolated `env -i`, Go 1.22.2, locked Python 3.12.3, explicit desktop integration interpreter. `BUILD_TESTS_OK helper_profiles=2 python=27 shell=2`; Claude 1282/1282 and Codex 1282/1282, inventory 97. Raw log `evidence/r1/build-test-only-d8711290.log`, SHA256 `eb8f3c018118096ef280837c2cb1b6446006469f4155bb12cb00519246a737c6`.
- Focused R1 suite: 7 tests pass, including both transient initialize error classes and the non-Codex synchronous startup check; raw log `evidence/r1/rework-focused-final.log`, SHA256 `ffb2e1fe7316fd4f4febebe9dc9b979111889297e7023e9c2866e21c3d4571e5`. The pre-fix adapter mutation failed both startup cases at `N1C_STARTUP_TRANSIENT_MISSING_DURABLE_WAIT`; the fixed log and gzip-preserved raw baseline are in `evidence/r1/`.
- `go test -p 1 -count=3 -timeout=45m -v ./internal/m8 ./internal/store`, `HUB_TEST_PG_STRICT=1`: aggregate rc=1. The only failure was one occurrence of the permitted Z-3 case `TestSessionNameReconnectKeepsNameAndReleaseReuses`, `hub_test.go:3505`, `interrupted (9)` in the first m8 repetition. The other two repetitions had no such failure and there were no other m8 failures. Across the three repetitions m8 recorded 2927 top-level PASS, 1 FAIL, 81 SKIP; store recorded 1203 PASS, 0 FAIL, 15 SKIP and completed in 246.563s. Raw log `evidence/r1/m8-store-count3-d8711290.log`, SHA256 `67634abfd22b5b89d2baa6c70d12b9f3836be31713ac759ad9f480edc47f29d6`. This is an allowed known flake, not an all-green command; the failure is retained without retrying away the result.
- OpenSpec 1.16.0 strict: 44 passed / 0 failed, rc=0; raw log `evidence/r1/openspec-strict.log`, SHA256 `b73d64a3f20d879b0a60482c549efa64807ef5d4804e86efc69bf5edfdce251c`.
- G1–G4 mutation evidence remains the independently recorded final set under `evidence/mutations/*-final.log`; those mutation targets were not changed by the R1 adapter startup fix. R1's additional before/after mutation is separately recorded above. The exploratory wrong-seam survivor remains disclosed and is not counted as a killed mutation.
- R2 compatibility contract remains verified by coder56's real old-Hub/helper trace at `review/coder56solzzc/r61-n1c-review-20261118` tip `7bf230f1dfa6e2a8836ac7bf40a3ab093847f15b`: before `DeliveredAt`, existing Hub busy→queued behavior applies; after `DeliveredAt`, busy leaves the state `delivered` and helper FIFO resumes the same delivery with exactly one `turn/start`, no loss. No Hub code was changed.

## R3/F1 supplement (2026-11-18)

- Source: `ba69949734dfc9ef93d8c29bb092f4aa1db73c46` (tree `d57aa17757f6459f0fb8df0a34ee76cd4f006e95`), appended after the R1/R2 anchor without rewriting history. It covers the recheck request at `6c386d9be64b`: repeated safe pre-submit initialize failures must not pin the durable FIFO, and a connected Codex transport whose initialize previously failed must not bypass initialization on a later `start()`.
- The adapter recovery worker now retries only classified pre-submit read-only failures while the durable `submission_may_have_started_at` marker is null. It uses capped 1/2/5/10/30-second delays, and only signals recovery after a later initialize succeeds. Definitive auth/identity failures stay quarantined. A set submission marker remains manual/unknown and is never auto-retried. The FIFO worker continues holding the same head while recovery is pending; the event bridge resumes only after recovery signals readiness.
- F1 regression `LiveInitializeRetry` holds the RPC socket healthy while the first initialize returns a transient timeout, then verifies a subsequent `start()` performs initialize again and reaches the initialized state. Removing the `self._initialized` guard is killed by the business assertion `N1C_LIVE_TRANSPORT_SKIPPED_INITIALIZE_RETRY`.
- R3 regressions inject consecutive timeout and initialize `-32603` responses, then successful recovery; they verify FIFO order and one `turn/start` per item. Separate cases assert auth rejection stays quarantined and a persisted submission marker cannot be recovered automatically. Three isolated mutation runs were killed by the intended assertions: remove the initialized guard (`N1C_LIVE_TRANSPORT_SKIPPED_INITIALIZE_RETRY`), retry auth failures (`N1C_AUTH_REJECTION_WAS_RETRIED_AS_TRANSIENT`), and ignore the submission marker (`N1C_UNKNOWN_SUBMISSION_WAS_AUTOMATICALLY_RECOVERED`). No mutation failed from compilation or panic. Logs are under `evidence/r3/`.
- Focused set: 6 tests, rc=0; raw log `evidence/r3/focused-r3-f1.log` (SHA256 in `SHA256SUMS`). OpenSpec 1.16.0 strict: 44 passed / 0 failed, rc=0; raw log `evidence/r3/openspec-strict-ba699497.log`.
- Canonical helper build gate at this source: isolated `env -i`, Go 1.22.2, locked Python 3.12.3 explicitly set for desktop integration; `BUILD_TESTS_OK helper_profiles=2 python=27 shell=2`, rc=0. Raw log `evidence/r3/run-build-tests-ba699497.log`, SHA256 in `SHA256SUMS`.
- m8/store gate: `go test -p 1 -count=3 -timeout=45m -v ./internal/m8 ./internal/store`, `HUB_TEST_PG_STRICT=1`, rc=0. m8 passed in 765.552s and store passed in 255.271s. No failures and no `interrupted (9)` occurrences. Raw log `evidence/r3/m8-store-count3-ba699497.log`, SHA256 in `SHA256SUMS`.
- No Hub/store product source, migration, VERSION, or live service was changed for this supplement.


## R1 repeated-startup-error recheck (2026-11-18)

- Reviewer checkpoint: `review/coder56solzzc/r61-n1c-review-20261118` `e8dd48e59c4679f84a95c62edde4af6d8f6430d9`. The defect was reproduced there with the real helper delivery worker, adapter, and SQLite lifecycle: after two classified transient initialize failures, the queue head remained `processing`, the durable busy marker remained, and no submission marker/admission/recovery event appeared.
- Regression strengthened at `tools/sessionhelper/tests/test_codex_stale_thread.py`, `FirstDeliveryInitializeWait`: each subcase now supplies an initial initialize timeout followed by either another timeout or JSON-RPC initialize `-32603`; the scripted RPC then succeeds. The real worker path reaches the same delivery's submission boundary and exactly one `turn/start`; no `thread/start` or failed ACK is emitted. Focused `test_codex_stale_thread` plus `test_dispatch_lifecycle`: 106 tests, rc=0. Log: `evidence/r1/recheck-20261118/focused-modules.log` (SHA256 `c28c1fff3f76a25d21d3b6bfd2131d37d34386c5f05410e3bfa10d33be67311b`).
- Retry-loop deletion mutation: both repeated-timeout and timeout-then-`-32603` subcases fail at the intended business assertion `N1C_STARTUP_RECOVERY_NEVER_REACHED_SUBMISSION` (rc=1; no compile error or panic). Log: `evidence/r1/recheck-20261118/retry-loop-removed.log` (SHA256 `533871d5ab6ecd23e620c488962e193143aa345f8002f8f4187d7415c55ddbe4`).
- Product behavior was already present in source `ba69949734dfc9ef93d8c29bb092f4aa1db73c46`: recovery retries only classified transient errors while the durable submission marker is null, and signals recovery only after initialize succeeds. Definitive identity/authentication/daemon errors and any non-null submission marker remain quarantined. This checkpoint adds the missing real-worker repeated-error coverage; no product file changed in this recheck. The affected helper modules and the full canonical build runner were rerun after this test-only addition; product source remains unchanged. Full gate used isolated `env -i`, Go 1.22.2, locked Python 3.12.3, and explicit `DW_DESKTOP_INTEGRATION_PYTHON`; it ended `BUILD_TESTS_OK helper_profiles=2 python=27 shell=2`, rc=0. Raw log: `evidence/r1/recheck-20261118/build-test-gate.log` (SHA256 recorded in `SHA256SUMS`).

## R3 supplement (2026-11-18)

- This append addresses group-lead instruction `c5dce2da3103`. Source/test anchor: `bc235f9fb604a8d677d8b921487be7d3075806bc`. Recovery is now gated consistently at three points: the startup recovery worker retries only classified transient pre-submit failures with a null submission marker; the command worker retains the deferred command at the FIFO head; and the event bridge does not subscribe to the replaced driver's stream until the recovery event is set. The prior driver's events cannot be consumed while its replacement is still initializing.
- The real helper worker / adapter / SQLite regression now enqueues a later delivery behind the recovering head. Both the timeout→timeout and timeout→initialize `-32603` cases wait for completion events and assert each delivery has exactly one `turn/start`, in FIFO order. The event-bridge test asserts no subscription occurs before recovery. Six focused tests pass, including authentication quarantine and the submitted-marker guard; raw log `evidence/r3-supplement/focused-r3-f1-fifo.log`.
- Two isolated mutations were killed at business assertions: changing `defer_current` from `appendleft` to `append` fails `N1C_STARTUP_RECOVERY_LATER_FIFO_ORDER_OR_EXACTLY_ONCE_BROKEN`; removing the event-bridge recovery gate fails `N1C_EVENT_BRIDGE_SUBSCRIBED_BEFORE_RECOVERY`. Logs are `evidence/r3-supplement/mutation-fifo-append-tail.log` and `evidence/r3-supplement/mutation-event-bridge-skip.log`.
- The in-repository `LiveInitializeRetry.test_healthy_transport_after_initialize_timeout_still_initializes` is the S4 probe. The supplied earlier-joined probe log is preserved at `evidence/r3-supplement/S4-live-initialize-earlier-joined.txt`; its failure is at `N1C_LIVE_TRANSPORT_SKIPPED_INITIALIZE_RETRY`. The corresponding in-repository mutation log remains `evidence/r3/mutation-remove-initialized-guard.log`.
- Focused R3/F1/FIFO set: 6 tests, rc=0. The canonical helper build gate at source/test anchor `bc235f9fb604a8d677d8b921487be7d3075806bc` used isolated `env -i`, Go 1.22.2, locked Python 3.12.3, and explicit desktop integration interpreter. Both profiles passed 1287/1287 tests (groups 1278 + 9); inventory 97; `BUILD_TESTS_OK helper_profiles=2 python=27 shell=2`, rc=0. Raw log `evidence/r3-supplement/run-build-tests-bc235f9.log`.
- The original `SHA256SUMS` entry for `REPORT.md` was stale because it included an older report version. The manifest is regenerated after this supplement is finalized.

## F2 event-bridge reconnect regression (2026-11-18)

- Added `test_set_recovery_event_does_not_mask_unhealthy_driver_reconnect`. It keeps the recovery Event set and the recovery object attached, starts the bridge with an unhealthy driver, and requires one `driver.start()` reconnect before normal resubscription. The event iterator and sleep are bounded so a regression fails without hanging.
- The isolated mutation changed the post-subscription recovery guard to `if recovery is not None`. The new test fails at the business assertion `N1C_SET_RECOVERY_MASKED_UNHEALTHY_DRIVER_RECONNECT` (zero reconnect starts); no compile error or panic. The restored source passes all five `PresubmitRecoveryStartRetryTest` cases. Logs: `evidence/r3-supplement/mutation-event-bridge-set-event-mask.log` and `evidence/r3-supplement/focused-f2-event-bridge.log`.
- F2 changes tests and report evidence only; no product code changed. Full canonical helper gates remain the R3/F1 result at source `bc235f9fb604a8d677d8b921487be7d3075806bc`.

## R4 FIFO interleaving recheck (2026-11-18)

- FIFO repair is the queue-level `defer_current` change already included in source anchor `bc235f9fb604a8d677d8b921487be7d3075806bc`: deferred command goes to `_pending.appendleft`. We chose this over only checking `_transport_recovery` before `start_next`, because the Codex local-queue paths at `driver_adapter.py:628` and `:667` defer the head before `_recover_transport` creates the recovery Event. A dispatch-loop guard alone would leave that first rotation window open.
- R5 refines the queue policy: recovery and pre-submit waits at `driver_adapter.py:364`, `:466`, `:628`, and `:667` preserve the FIFO head; transient blocker authorization/read failure at `:433` rotates to the tail so an unavailable read cannot starve ordinary deliveries. The `test_dispatch_lifecycle.py` direct test caller explicitly chooses head preservation. See the R5 section below for the five-caller inventory and rationale.
- The real helper-worker / adapter / Codex driver / SQLite regression at `FirstDeliveryInitializeWait` now pauses immediately after the real `defer_current` releases its lock, waits for recovery to finish and the later same-session delivery to enqueue, then resumes the command worker. Both timeout→timeout and timeout→initialize `-32603` subcases assert the original head receives `turn/start` first and both items are each submitted once. This is the deterministic interleaving from coder56's `probe_fifo_interleaving.py`, embedded in the resident helper test; no queue ordering, dispatch, or RPC implementation is bypassed.
- The append-to-tail mutation reproduces `R3_FIFO_HEAD_OVERTAKEN_AFTER_SAFE_RECOVERY` in both subcases: follower `turn/start` appears before the original head, and the final expected-order assertion fails. Restored code passes the seven focused R4/F2/auth-boundary tests, rc=0. Logs: `evidence/r4-focused.log` and `evidence/r4-fifo-append-tail-mutation.log`.
- The F2 event-bridge case remains in the same focused set. The separate set-Event/unhealthy-driver mutation remains recorded at `evidence/r3-supplement/mutation-event-bridge-set-event-mask.log` and fails at `N1C_SET_RECOVERY_MASKED_UNHEALTHY_DRIVER_RECONNECT`.
- The reviewer-noted `test_handle_delivery_turn_auth_failure` case passed locally in isolation and in the focused R4 set; this environment did not reproduce that one-off canonical profile failure.
- R4 adds test/report/evidence only; no product code changed from the already-pushed FIFO repair.

## R4 auth-failure canonical comparison (2026-11-18)

The requested auth-boundary comparison was run with the same full canonical `SOURCE_COMMIT=<sha> bash build.sh --test-only` command on both trees: baseline `d6e56a8b13bcb622eb0610e4a7cd129f834d9ff3` (without N-1c) and R4 source `057f1da733b405e61a285f24813e32df0e8cf09c`. Each tree was run three times, sequentially. Every invocation used `env -i`, Go 1.22.2, `/tmp/lockvenv/bin/python3` (3.12.3), a distinct HOME, explicit `DW_DESKTOP_INTEGRATION_PYTHON`, and the same Go module/build caches. The full build test gate passed all six invocations.

| Source | Round | Claude profile | Codex profile | `test_handle_delivery_turn_auth_failure` | Canonical result |
| --- | ---: | ---: | ---: | --- | --- |
| `d6e56a8b` | 1 | 1277/1277 | 1277/1277 | pass in both profiles | rc 0 |
| `d6e56a8b` | 2 | 1277/1277 | 1277/1277 | pass in both profiles | rc 0 |
| `d6e56a8b` | 3 | 1277/1277 | 1277/1277 | pass in both profiles | rc 0 |
| `057f1da7` | 1 | 1288/1288 | 1288/1288 | pass in both profiles | rc 0 |
| `057f1da7` | 2 | 1288/1288 | 1288/1288 | pass in both profiles | rc 0 |
| `057f1da7` | 3 | 1288/1288 | 1288/1288 | pass in both profiles | rc 0 |

Thus the reported one-off failure was not reproduced in either source across three canonical runs (six profile executions per source); these samples do not establish its root cause or a source-specific probability change. No repair was made for this auth case. Raw full logs are under `evidence/r4-auth/`.

The evidence checksum manifest was repaired at this delivery source: all existing listed log files were force-added to Git, including the twelve previously present locally but absent from the tracked tree; the six canonical comparison logs were also force-added. The final manifest has 63 entries, all 63 pass `sha256sum -c`, and every referenced path is tracked and available from Git at the delivery tip.

## R5 blocker fairness and auth-boundary comparison (2026-11-18)

- `UnifiedCommandQueue.defer_current(delivery_id, *, preserve_fifo_head)` now requires every caller to select the deferral policy. The four recovery/pre-submit paths at `driver_adapter.py:364` (recovery event pending), `:466` (regular command sees pending recovery), `:628` (pre-submit transport recovery before event creation), and `:667` (local queue result proven not submitted) use `True`: their durable command remains the FIFO head. The blocker authorization/read failure path at `:433` uses `False`: it returns that blocker wake to the tail after a transient read failure, allowing ordinary queued deliveries to progress. The direct unit-test caller at `test_dispatch_lifecycle.py:386` explicitly uses `True`. This prevents global tail rotation from reintroducing R4 FIFO overtaking while preventing a transient blocker read from monopolizing the lane.
- Added permanent real-helper / `_command_worker` / Codex driver / SQLite coverage for the blocker case in `FirstDeliveryInitializeWait.test_blocker_authorization_read_failure_rotates_behind_ordinary_delivery`. It uses the actual `prepare_wake` validation path and a synthetic HTTP 503 response; the ordinary delivery is persisted first and must reach `turn/start` within two seconds. The old head-rotation mutation fails at `R4_BLOCKER_AUTH_READ_FAILURE_STARVED_ORDINARY_DELIVERY`. The external coder56 probe (`8ec08df8`, `recheck-bc235f9f/checkpoint-evidence/probe_blocker_head.py`) independently showed the old behavior: 11 authorization retries in 5.2 seconds and no ordinary start, versus an ordinary start at about 0.55 seconds on the tail-rotation source.
- The two R4 FIFO subcases remain permanent. Mutating recovery deferral to tail rotation fails both consecutive-initialize-error subcases at `N1C_STARTUP_RECOVERY_LATER_FIFO_ORDER_OR_EXACTLY_ONCE_BROKEN`; blocker head rotation fails at the business assertion above. Restored focused set (`FirstDeliveryInitializeWait` plus `test_dispatch_lifecycle`) passed 65 tests, 0 failures/errors. Raw focused and mutation logs are recorded under `evidence/r5/`.
- R4 supplement A was rerun at the final R5 product/test source `52a3f8a7041d5fdcf7f3eac6f1e118c1a277adf3` with three full canonical `build.sh --test-only` invocations. Baseline `d6e56a8b13bcb622eb0610e4a7cd129f834d9ff3` retains its three previously recorded full canonical passes. The auth test passed in all six R5 profile executions. Round 1 had one unrelated Codex-profile failure (`test_handle_delivery_turn_failed_event_surfaces`, assertion expecting `turn_completed` in an event blob); rounds 2 and 3 ended with `BUILD_TESTS_OK helper_profiles=2 python=27 shell=2`. Every invocation used `env -i`, Go 1.22.2, locked Python 3.12.3, a distinct HOME, and explicit `DW_DESKTOP_INTEGRATION_PYTHON`.

| Source | Round | Claude profile | Codex profile | `test_handle_delivery_turn_auth_failure` | Canonical helper/build result |
| --- | ---: | ---: | ---: | --- | --- |
| `52a3f8a7` | 1 | 1289/1289, rc 0 | 1289 cases, rc 1 (one unrelated failure) | pass / pass | rc 1 |
| `52a3f8a7` | 2 | 1289/1289, rc 0 | 1289/1289, rc 0 | pass / pass | BUILD_TESTS_OK, rc 0 |
| `52a3f8a7` | 3 | 1289/1289, rc 0 | 1289/1289, rc 0 | pass / pass | BUILD_TESTS_OK, rc 0 |

Round 1's unrelated failure is recorded verbatim in `evidence/r5-auth/52a3f8a-round1.log`; it does not change the auth-case outcome. The three baseline runs and their six passing auth observations are the `d6e56a8b` rows in the preceding R4 comparison table.
- The queue strategy change is included in source commit `52a3f8a7041d5fdcf7f3eac6f1e118c1a277adf3`; the following report/evidence commit is documentation only. R4 history through `d0a0cab5` is unchanged.

## R6 — initialize authentication rejection and first-start race (2026-11-19)

**Finding.** On N-1c source `52a3f8a7041d5fdcf7f3eac6f1e118c1a277adf3`, the requested `test_handle_delivery_turn_auth_failure` intermittently fails before it reaches the operation named by the test. In the traced failure, the fake app-server saw only `initialize`; there was no `thread/start` or `turn/start`. The adapter's watchdog can run in the small interval after `_bounded_deliver` launches the delivery worker but before `CodexProtocolDriver.start()` has established its first transport. `transport_recovery_required()` then sees an uninitialized driver as unhealthy and starts recovery against the still-running initial start. Recovery can quarantine the FIFO head before the definite authentication rejection is classified, leaving the queue without a `failed` terminal. This explains the empty state and the recovery alert in the captured failure. The comparison tree `d6e56a8b13bcb622eb0610e4a7cd129f834d9ff3` did not show this outcome in the same 200-run sample; that sample alone does not establish a general rate.

**Repair.** The exact gates are `driver_adapter.py:623-636` (skip recovery only before the first connection), `codexDriver.py:918-962` (publish startup failure under the start lock), `codexDriver.py:1207-1213` (definite initialize-auth rejection to failed), and `codexDriver.py:1857-1864` (do not race an active or already-failed start with the watchdog). The delivery worker owns the first start. Definite authentication rejection from `initialize` settles through the existing non-retryable failed receipt path. Authentication rejection during thread/read validation remains operator-held, and the existing post-submit/unknown protections are unchanged. A local-queue result caused by a classified transient startup failure can still invoke bounded recovery after `deliver()` returns. No Hub, store, ACK contract, or other helper profile behavior changed.

**Regression evidence.** `test_handle_delivery_initialize_auth_rejection_is_terminal` uses the real helper worker, adapter, Codex driver and SQLite queue with a fake JSON-RPC server. It blocks the first delivery call to hold the exact pre-transport watchdog window, supplies a deterministic `initialize` 401 rejection, and asserts: no premature recovery, terminal state `failed`, RPC trace exactly `initialize` (no thread/turn submission), no injected callback, and release of the FIFO head. The test passes on this source. Removing the `initial_start_pending` guard makes the same test fail at the business assertion `initial startup must not be preempted by the recovery watchdog`; the mutation log is `evidence/r6/mutation-initial-watchdog.log`. The deterministic green log is `evidence/r6/deterministic-auth-green.log`.

**Requested sampling.** Isolated, serial runs of the original test before the repair produced:

| Source | Runs | Failures | Failed iterations |
| --- | ---: | ---: | --- |
| N-1c `52a3f8a7` | 200 | 2 | 12, 159 |
| Baseline `d6e56a8b` | 200 | 0 | — |
| N-1c `52a3f8a7`, with RPC method trace enabled | 200 | 1 | 39 |
| Repaired source | 200 | 0 | — |

All loops used `env -i`, one test process at a time, isolated HOME/TMP, and the locked Python interpreter. The traced pre-fix failure contained only the `initialize` RPC before recovery/quarantine; raw failure logs and per-loop results are under `evidence/r6/`. The repaired run's 200 individual logs are preserved in `postfix-200-raw.tar.gz`.

**Coverage.** The focused regression set (`test_codex_stale_thread`, `test_dispatch_lifecycle`, `test_driver_failure_faces`) passed 117 tests with zero failures after the final test changes (`evidence/r6/focused-117.log`). The canonical `scripts/run-build-tests.sh` gate passed under `env -i` with `DW_DESKTOP_INTEGRATION_PYTHON=/tmp/lockvenv/bin/python3`: `BUILD_TESTS_OK helper_profiles=2 python=27 shell=2` (`evidence/r6/canonical-build.log`). After tightening the deterministic test probe to only report the no-RPC initial-start window, the two-profile helper gate was rerun against that final test fixture: both Claude and Codex reported 1290/1290 cases, groups `[1281, 9]`, inventory 97, rc=0 (`evidence/r6/helper-two-profile-final.log`). R3–R5 focused cases remain included in the 117-test set.
