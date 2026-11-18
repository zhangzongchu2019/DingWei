# Helper admission deadline and AA010 test flakiness

Base: `9818b979e9e20f0bcfeffa6b78045dda71b1bd79`. Product code is unchanged. Test-only commits: `c6f935096` and `5b96b215c`.

## Sampling

The focused matrix ran four exact cases per invocation under `/tmp/lockvenv/bin/python3` with separate `HOME`, explicit `SH_DRIVER`, and `PYTHONPATH`; Claude and Codex ran as concurrent processes for 30 pairs, then each ran alone 30 times. Exact logs and machine-readable totals are in `samples/`.

| Mode | Claude | Codex | Failures |
|---|---:|---:|---|
| Parallel, 30 runs each | 30/30 green | 29/30 green | Codex iteration 30: AA010 only |
| Serial, 30 runs each | 30/30 green | 30/30 green | none |

Each of the three targeted admission cases ran 30 times per profile/mode (120 executions per case across the full matrix) with no failures. The AA010 case ran 30 times per profile/mode and had one failure in parallel Codex, none in the other 90 invocations. Exact red:

```text
FAIL: test_aa010_feature_off_drains_without_native_or_capability (test_sub_agent_helper.StoreTests.test_aa010_feature_off_drains_without_native_or_capability)
AssertionError: False is not true : AA010_FEATURE_OFF_RETURN_CONFIRMED
```

An additional 30-pair run of the complete `test_dispatch_lifecycle.py` module had Claude 30/30 green and Codex 29/30 green. Its one error was a teardown-only `OSError: [Errno 39] Directory not empty` from `TemporaryDirectory.cleanup` in `test_claude_bad_transport_old_handling`; the test body passed. This ancillary error was not one of the four target cases. The prescribed build gate runs profiles sequentially and gives each a separate temporary home.

The prior same-base samples explain why the admission family still needs deterministic fixtures: Qwen's `browser-flake-windows` report/evidence records two failing build-gate runs and one clean run; isolated full-module reruns were 6/8 green, 2/8 red. Exact reds were:

| Evidence | Failure and assertion |
|---|---|
| `build-tests-run1.log`, Codex | `test_missing_accepted_uses_admission_deadline_and_recovers`: expected the recovered `('new', second)` call, observed only `('old', first)` (`Lists differ`). |
| `build-tests-run2.log`, Claude | `test_missing_accepted_uses_admission_deadline_and_recovers`: `entered.is_set()` false. |
| `build-tests-run2.log`, Claude and Codex | `test_admission_committed_before_deadline_wins`: expected `['queued','injected','processed']`, got `['queued','admission_unknown']`. |
| `dispatch-lifecycle-rerun6.log` | `test_blocked_deliver_accepted_after_deadline_is_not_admission`: expected `[did]`, got `[]` (`Lists differ`). |
| `dispatch-lifecycle-rerun7.log` | `test_missing_accepted_uses_admission_deadline_and_recovers`: `entered.is_set()` false. |

Those source logs remain on `origin/qwen38mvb4/browser-flake-windows`, under `docs/reviews/browser-flake-windows/evidence/`; the focused repetitions here did not independently hit an admission red.

## Cause

**AdmissionDeadline family — confirmed test scheduling sensitivity; no product regression demonstrated.** The deadline is based on the persisted dispatch intent, which is written before the background transport worker is scheduled (`driver_adapter.py`, `_dispatch_clock` and `_bounded_deliver`). Tests use a 100 ms cutoff and `sleep(0.12)`/a short fixed wait to arrange which side of the deadline wins. Under parallel load, the worker or first poll can be scheduled after the persisted-intent deadline. Then the fixture's intended call/receipt never occurs, or a valid receipt in the fixture arrives after the test has already exercised the timeout branch. This is why one baseline red sees `entered` false, another sees no first delivery call, and the early-admission case records `admission_unknown`. The scheduler can change the test's ordering before the assertion runs; increasing the cutoff or accepting either state would weaken the test and is not recommended.

**AA010 — confirmed observation-order race in the test.** In `tools/sessionhelper/sessionhelper/sub_agent_connection.py:98-104`, the helper awaits `sub_agent_return`, then schedules `store.return_result(..., 'returned')` on a worker thread. The test's fake `send` appends to `calls` before that durable state update finishes (`tools/sessionhelper/tests/test_sub_agent_helper.py`, former lines 126-138). The test polls until two calls exist and immediately calls `prepare_rollback()`. Thus `AA010_FEATURE_OFF_RETURN_CONFIRMED` can run in the gap between the fake transport observation and durable settlement. The product sequence is intentional; the test was observing the earlier event as if it proved the later one.

## Test-only repair

The new fixtures keep all state, ordering, ACK, and FIFO assertions:

1. For admission tests, wait on an event proving the fake `deliver` or `poll_receipt` has entered, then cross the deadline deterministically. The missing-accepted case uses a test-local elapsed clock that stays at zero until the worker-entry event and then advances beyond the 100 ms cutoff. The late-poll case retains the real persisted-intent clock: it blocks the already-entered poll, arms the short cutoff, waits until `_admission_elapsed(command) >= _admission_timeout`, then releases the receipt. This removes fixed sleeps as scheduling assumptions while retaining the late/early branch distinction.
2. For AA010, wrap the test store's `return_result`; set a `threading.Event` only after both underlying `state='returned'` writes have completed. Await that durable-state event before the existing exact per-mail return and `downgrade_ready` assertions. No product code or semantic assertion is relaxed.

Repaired focused matrix: 30 parallel pairs and 30 serial runs per profile, all 120 invocations green for both profiles. See `repaired-samples/RESULTS.json` and per-run logs.

## Product-bug mutations

Both mutations were applied in temporary worktrees, each run was restored to its original source afterward, and both tests failed on an assertion:

| Mutation | Red result |
|---|---|
| Admission: make `_transport_unknown(..., reason='admission_timeout')` return without persisting unknown or recovering transport | `test_missing_accepted_uses_admission_deadline_and_recovers` red at `独立admission截止必须越过阻塞deliver`; late-receipt cases also fail to settle. This is a real broken admission-timeout behavior, not a compile or fixture error. |
| AA010: change successful durable return state from `returned` to `undeliverable` | `test_aa010_feature_off_drains_without_native_or_capability` red at `AA010_FEATURE_OFF_DURABLE_RETURNS_SETTLED`; the accepted returns are no longer durably settled. |

Raw mutation output is in `raw/mutation-admission.log` and `raw/mutation-aa010.log`.

## N-1a impact

Compared `9818b979` with `origin/togpt60lunazzc/r61-n1-busy-wait` (tip `8107bc5a`). N-1a changes `DriverAdapter` admission timing only for a row with a persisted `busy_wait_started_at`: `_admission_timeout_for` then applies the busy-wait limit, and `_admission_elapsed` handles its submission boundary. The four target fixtures do not create that marker, and their target test bodies were not changed by N-1a. N-1a changes neither `sub_agent_connection.py` nor `test_sub_agent_helper.py`, so the AA010 ordering race is untouched. Static comparison therefore finds no direct change to either failure mechanism; any indirect change from added suite load is unmeasured. I did not claim a 30-run N-1a reproduction.

## Verdict

The test-only repair removes the fixed scheduling window and waits for the durable condition each assertion already requires. The baseline focused matrix independently reproduced AA010 once and did not reproduce an admission failure; the admission diagnosis is supported by Qwen's same-base build and isolated-module evidence. Both repaired areas killed a true-bug mutation. No product files changed.
