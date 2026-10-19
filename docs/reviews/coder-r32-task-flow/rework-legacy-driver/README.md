# R9 REWORK follow-up: both task-list read surfaces

This is the author follow-up to the independent REWORK of candidate `6754f7d343301d4743b641f8feee263bbccd1807`. It is not an independent PASS. R8 remains unstarted and no deployment or production action is included.

Frozen source/test anchor: `d23ed782c775febeaf1ca40c12700dff7a87ef47`, tree `0c788a9751c9eeaec9fa42128b0f4526c37e87bd`. Runtime fixes finish at parent `d1a1f06543570270ab740acb95d73a83e76ae071`; d23 adds only a post-selection state-error test and withdrawal. Subsequent delivery changes are evidence/docs only. Branch: `review/coder/r32-task-flow`, private GongFeng `wego-ai/dingwei`.

The original report and manifests one directory above describe the historical 6754f7d3/b08add98 candidate. They are retained unchanged. **Use this directory's source/evidence manifests and gate ledger for the current follow-up**, and the original candidate's helper/browser receipts only within the byte-identity reuse limits below.

## Implementation response to findings

Review inputs: `54135761b14b7235f975b20873ac53ea541ea447`, supplemented by `1d0b4359a5362cb693b0ad3ff5cbaf656d1d8f2e`, and `20c412496276069748a6c2c079860ed74b5e9fb3` on review/togpt60lunazzc/hosted-enable-r31. The leader required both list surfaces, errors and reassignment races, in one follow-up.

| Surface | Fix | Deterministic regression / withdrawal |
|---|---|---|
| Legacy capability task-driver | Any state-read error fails closed before response formation. A missing/deleted row is also rejected; it is not proof that a stale selected task is legacy. Actual group-less tasks have a readable state row and retain their behavior. | `TestR32LegacyTaskDriverStateReadFailureCloses`: initial and locked storage failure plus missing-row; withdrawal `legacy_driver_state_fail_closed` must hit `LEGACY_DRIVER_STATE_READ_FAILED_OPEN`. |
| Legacy current assignment | Require the requesting Agent to remain current developer/reviewer; acquire existing PG task membership locks, then reread SQLite state and require current role, current version/group, not archived, not needs-reassignment. | `TestR32LegacyTaskDriverReassignmentBetweenReads`: real HTTP with dedicated bearer/capability, goroutine barriers after initial list and after first state read. Reassign in SQLite before resuming. Real PG task authorization locks; HTTP credential/capability sessions stay the original Mem fixture. Withdrawals `legacy_driver_current_actor` and `legacy_driver_locked_snapshot`, marker `LEGACY_DRIVER_REASSIGNMENT_LEAKED`. |
| New store list | Initial task selection is not continuing authorization. The production projection stage rechecks the requested Agent against current roles and propagates state lookup errors. Public ListConsoleTaskFlow directly invokes that projection stage. | `TestR32TaskListReassignmentBetweenSelectionAndProjection`: run the same two production stages with a real SQLite reassignment at a barrier; no old Agent items, new assignee positive. `TestR32TaskListStateReadFailurePropagates`: close the actual SQLite DB after initial selection; projection must error and return no items. Withdrawals `store_list_current_actor` and `store_list_state_fail_closed`; designated markers `STORE_TASK_LIST_REASSIGNED_ACTOR_LEAKED` and `STORE_TASK_LIST_STATE_ERROR_NOT_PROPAGATED`. |
| New HTTP list | Independently require current actor/active task state inside the existing PG authorization callback, retaining the version check. Propagate list/projection and post-selection state-read errors. | Mixed-snapshot `TestR32MainTaskListRechecksCurrentActorAfterReassignment` models the former store race at the HTTP adapter boundary. `TestR32MainTaskListStateReadFailureCloses` injects projection error. **`TestR32MainTaskListCurrentStateReadFailureCloses` injects the separate ConsoleTaskFlowState read after selection**, returning initialized state plus non-nil error so that dropping the error check really returns task data instead of turning red at another version guard. Withdrawals `main_list_current_actor`, `main_list_state_fail_closed`, `main_list_current_state_fail_closed`, each with its named marker in WITHDRAWALS.json. |

`TestR32LegacyTaskDriverUnchangedAndGrouplessControls` retains a valid current R32 task and genuine group-less legacy task as positive controls. Role rejection does not pretend that the reassigned task's new pair is invalid. No schema, identity lock protocol, helper protocol or version changes are introduced.

Store regressions use the actual production selection method and its extracted projection stage, rather than a production test hook or a scheduler timing guess. New HTTP mixed-snapshot and fault cases deliberately test an adapter returning a bad/failed snapshot; they are defense-layer checks, while the store tests exercise the production SQLite projection. Legacy route tests use a real httptest HTTP server; new API handler tests run the actual bearer middleware with httptest requests/recorders.

## Current gates and evidence

Author gates completed 2026-10-19T10:32:16Z: default-parallel full Go rc0, 52 tested packages passed and 13 packages without tests; vet rc0; canonical Python50/shell2; targeted race store/m8 rc0; 30 designated withdrawals green/red/restored-green; 11 compatibility readings. Exact receipts are in `GATES.json`, `GO-PACKAGE-LEDGER.json` and raw/. Non-verbose package logs do not establish a count of skipped individual cases. `withdrawals/WITHDRAWALS.json` binds **30 actual isolated source withdrawals** to the exact frozen anchor, each green → designated red → restored green, with per-phase stdout/stderr/rc. The original 22 plus 8 follow-up withdrawals run together. The runtime source is unchanged while gates run; only this evidence directory is written.

Reproduction:

```sh
go test ./... -count=1 -timeout=30m
go vet ./...
go test -race ./internal/store ./internal/m8 -run '^TestR32TaskList|TestR32LegacyTaskDriver|TestR32MainTaskList' -count=1 -timeout=5m
PATH=/tmp/coder-0232-locked-venv/bin:$PATH bash scripts/run-build-tests.sh
python3 tests/r32-task-flow/withdrawals.py --anchor d23ed782c775febeaf1ca40c12700dff7a87ef47 --output /tmp/r32-review-rework
```

The withdrawal runner now respects caller-supplied NODE_PATH and PW_CHANNEL, so reviewers can point to their own Playwright install. Its fallback local path is recorded in the script. Supplying the environment is not a browser PASS; browser availability and executed rc remain required.

The original helper 736-test and browser receipts are reused only after proving their source producer/fixture bytes unchanged; see `REUSED-GATES.json`. Store compatibility was rerun after the projection fix, including migration/old-Hub reading/old-helper capability checks. This is compatibility evidence, not permission for writable rollback. The original rollback write fence remains required.

Original 6754f7d3 was REWORK, and intermediate abb2ec1f/2a934de8/d1a1f065 handoffs did not start final review or become an acceptance claim. Failed first-stage fixtures/build attempts are retained separately; they are not replaced by final green receipts. In particular, the first race fixture copied identity rows into PG but not live capability sessions; it failed before reaching its barrier and was corrected to keep HTTP sessions in Mem while using PG for task authorization. An interrupted local helper-extraction edit initially produced an undefined-helper build error; that is retained as a fixture/edit failure, not counted as a designated mutation red.

This follow-up awaits independent final review. It performs no deployment, host43/dw123 changes, R8 work, helper upgrade, or external file-service source transfer.

## Independent interim readings

Reviewer branch review/togpt60lunazzc/hosted-enable-r31: commit `c928043ccefd9eb036f2d35d1f7b8c8ea647e89d` records d1a1f065 store targeted rc0 and m8 targeted rc0 (13.039s); `40096000fb7b12a3ac456c5b5a84341bc37f0d3a` records d23ed782's new post-selection HTTP error case rc0 (0.168s) and source inspection of its matching withdrawal. Both reports were independently fetched and read. These are interim source/targeted readings; neither claims the full Go/30-withdrawal gate or final R9 PASS. Latest-source dev-env evidence, when available, is recorded separately.

Latest-source dev-env supplementary evidence: `fd814190c54eadfdd25b9a69142aa70051f1de97`, tree `90fd41b53f642306f181471d4986a56ed4003eb2`, directory `docs/reviews/togpt60lunazzc-r32-task-flow/dev-env-d23ed782-20261019T102134Z/` in the reviewer branch. Exact d23ed782, ubuntu UID1000, umask0002, Go1.22.2; store/m8 targeted groups rc0 (0.246s / 7.193s), with original stdout/stderr/rc and environment. This report was fetched and read independently. It is the second-environment targeted supplement, not a full-repository run or final independent PASS.

A predecessor full-suite run started at 2a934de8 returned rc1: hosted TestBackupPipelineEndToEnd failed opening its temporary PG connection with unexpected EOF. The hosted/pgtest sources were not modified in this follow-up. Its raw failed receipt is retained; the root cause was not established and the failed result is not rewritten as passed. The final frozen-source full-suite receipt is recorded separately.
