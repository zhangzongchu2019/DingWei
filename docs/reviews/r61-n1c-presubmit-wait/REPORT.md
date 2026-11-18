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

After the durable wait marker is present, a thread-safe helper callback queues ACK v2 `busy`, `retryable=true`, `reason=presubmit_validation_wait` for the websocket ACK loop. This is not an ACK outbox terminal phase and does not alter terminal ACK validation. It uses the Hub's existing busy→queued path. The adapter watchdog recognizes the explicit validation hold even when driver health is false, so it does not convert a known mismatch into terminal rejection or release later FIFO items. Operator resolution requires a safe helper restart/revalidation.

## Verification

Locked interpreter: `/tmp/lockvenv/bin/python3`; environment was locally isolated for focused helper runs.

| Check | Result |
| --- | --- |
| Focused regression set | 157 tests, 0 failures after final hold and configured-thread retry changes. Raw log: `evidence/focused-final-hold-regressions.log` (SHA256 `82afd4ead8fb0cb2736bbc56d20c8e13bf6133099d7d45be3d2a45e3e62fe534`). |
| Frozen-source build gate | `SOURCE_COMMIT=25af4ce1300dfd55ecd9b8869fecb593a003076e`, isolated `env -i`, Go 1.22.2, locked Python 3.12.3; `BUILD_TESTS_OK helper_profiles=2 python=27 shell=2`, inventory 97. Claude and Codex profiles each 1281/1281, rc=0. Toolchain record: `evidence/toolchain.txt`; raw log: `evidence/build-test-only-final-rerun.log` (SHA256 in `SHA256SUMS`). |
| m8 + store, PG strict | `go test -p 1 -count=3 -timeout=45m -v ./internal/m8 ./internal/store`, `HUB_TEST_PG_STRICT=1`, rc=0. `internal/m8` 968.209s; `internal/store` 251.692s. PostgreSQL integration reported isolated `server_version_num=180003`. All three repetitions passed; no `interrupted (9)` and no failures. Skip records and conditions: `SKIPS.md`. Raw log: `evidence/m8-store-count3-final.log` (SHA256 in `SHA256SUMS`). |
| OpenSpec strict | 44 passed / 0 failed. Raw log: `evidence/openspec-strict-final.log` (SHA256 `00deedb25054eaeed804dcc843071547cc54a415fbd49e52ffe1de9e006e2ad4`). |
| G1–G4 mutations | All four killed by intended business assertions; no compile error or panic. See `evidence/mutations/*-final.log` and `SHA256SUMS`. An exploratory mutation at the adapter callback seam (which the direct driver test bypasses) survived; it is retained as `evidence/mutations/G4-wrong-seam-survived.log`. The actual G4 removes the durable lifecycle marker write and is killed at the missing `submission_may_have_started_at` assertion. |
| Old Hub/helper wire compatibility | PASS: helper-generated ACK metadata fixture was consumed by a temporary detached sparse checkout of Hub `501e5b8a`; the real Hub websocket test `TestReliableSessionDeliveryBusyAckDefersWithoutBurningAttempts` observed the durable delivery return to `queued`, attempts reset to 0, and the original source remained pending. The temporary checkout changed only that test harness to inject the helper-generated metadata fixture and bind the row to the already negotiated v2 capability; no temporary change was made to Hub product code. Raw log: `evidence/compat/old-hub-busy-ack.log` (SHA256 `eb0cbba6c43ab36d81f449a554c1cc0cc88bf9c7593888c9b3f561059dda26de`). Fixture: `tools/sessionhelper/tests/fixtures/r61-n1c-busy-ack-meta.json` (SHA256 `41d7a8b66ff25fb4723bd7f28c80d7b6101be61f945c5fcfb6d111bf6e9a6aba`). |
| G1 retry classification mutation | KILLED; `test_auth_stays_fail_closed_and_temporary_read_errors_retry_same_thread` failed on the retry-state assertion; `test_transient_validation_wait_is_durable_fifo_and_submits_once_after_recovery` also reported `N1C_RETRY_NOT_ENTERED`. |
| G2 transient validation terminalization mutation | KILLED; durable retry test failed at `N1C_RETRY_NOT_ENTERED`. |
| G3 busy-marker removal mutation | KILLED; lifecycle test failed at `N1C_BUSY_MARKER_NOT_PERSISTED`; Codex and Claude busy wait marker assertions also failed. |
| G4 submission-boundary persistence removal | KILLED; durable retry test failed because `submission_may_have_started_at` stayed null. |

Mutation logs are in `evidence/mutations/`; each mutation was applied in place for one isolated unittest process and restored immediately. No mutation produced a compile error or panic.

## Compatibility and remaining scope

- Old Hub `501e5b8a` + this helper: PASS. The helper-generated busy ACK fixture was consumed by the actual old-Hub websocket test `TestReliableSessionDeliveryBusyAckDefersWithoutBurningAttempts`; result was `queued`, attempts reset to 0, and the original sender delivery remained pending. This is an isolated Hub test harness with a recorded helper ACK fixture, not two separate long-running processes. Raw log: `evidence/compat/old-hub-busy-ack.log` (SHA256 `eb0cbba6c43ab36d81f449a554c1cc0cc88bf9c7593888c9b3f561059dda26de`). Fixture SHA256: `41d7a8b66ff25fb4723bd7f28c80d7b6101be61f945c5fcfb6d111bf6e9a6aba`.
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
