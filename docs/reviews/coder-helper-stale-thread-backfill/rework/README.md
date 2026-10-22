# REWORK — keep View backfill out of AMR

This is the same-branch follow-up to candidate `1ad6b349d9bd41e73f648df53c0738dd02229332` (tree `1c44b624adab3086496d25b6cbe0fd1c4fbf1e8f`). It addresses the review blocker that a View-only completed-item snapshot could recreate an AMR turn after Hub restart or expiry of the in-memory completed-turn table.

## Change

`internal/m8/amrProcessor.ingest` now checks the decoded event's `data.backfill` marker immediately after recording the observed event and before identity/replay handling or turn accumulator creation. A marked event increments both the general rejection counter and `view_backfill_dropped`, then returns. The heartbeat exports `view_backfill_dropped` so operators can count drops without logging event contents. No AMR turn is opened, accumulated, completed, routed, or sent through reply-origin handling.

`TestAMRBackfillAfterHubRestartIsDroppedBeforeAccumulator` models a fresh Hub processor with an empty volatile completed map. It feeds a backfilled assistant body containing an `@mention` and a backfilled completion event for the same delivery/turn. It asserts zero routes, zero route/reply queues, zero created turns/completed entries, unchanged route/reply counters, and two counted drops.

## Validation

- Focused regression: `go test ./internal/m8 -run '^TestAMRBackfillAfterHubRestartIsDroppedBeforeAccumulator$' -count=1`, rc 0.
- Withdrawal: removing the backfill guard made the regression fail at `AMR_BACKFILL_REOPENED_TURN: turns=0 completed=1`, rc 1. The raw output is `amr-backfill-guard-removed.log`.
- Full Go suite on this source: `go test ./... -count=1 -timeout=30m`, rc 0; 53 test packages passed and 13 packages had no test files. Raw output and command/rc are in `raw/go-full.log` and `raw/go-full.rc`.
- Canonical gate rerun: isolated HOME, locked helper venv, `BUILD_TESTS_OK python=57 shell=2`, rc 0. Raw output and command/rc are in `raw/canonical.log` and `raw/canonical.rc`.
- The helper/renderer source was unchanged from `1ad6b349`; the prior 817-test isolated helper run and three-assertion browser regression remain applicable to those unchanged inputs and are preserved in the parent evidence directory. This follow-up does not claim a new model-backed completed turn; the isolated Codex server's empty-thread `turns/list` limitation remains as documented in the parent README.
- `git diff --check` rc 0 before commit.

No production Hub/helper/renderer was changed, no service was restarted, and no live Hub event was injected.
