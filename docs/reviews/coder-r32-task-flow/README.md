# R32 R9 candidate: spec task flow

R9 W9.1–W9.5 is implemented against r31.1 `ef439b82b1550e9ca5c179edf61be78666c5fff0`. R8 is not implemented. This is an author validation report, not an independent acceptance or deployment authorization. The r31.1 production build source remains unchanged.

Runtime/test source: `b08add989500f000f9066249661c0310ebb7794c`, tree `01f27bc620bc208f0c44f0ac1abf5913ca109b8d`. It includes test-only successors expanding compatibility/browser checks and aligning the new-platform shim with owner confirmation, plus stable outbox pagination beyond waiting recipients. Subsequent changes in this delivery are documentation and evidence only. `SOURCE-MANIFEST.json` records the tested source bytes and Git blobs; `EVIDENCE-MANIFEST.json` records evidence hashes. The branch is `review/coder/r32-task-flow` in private GongFeng `wego-ai/dingwei`.

The design is `docs/designs/S-R32-SPEC-TASK-FLOW-R8-R9.md`. Leader's final ruling permits **different Agent IDs belonging to the same owner**, and rejects only the same Agent in both roles. The reviewer request for different owners was explicitly overruled; it is not an unresolved implementation condition.

## Acceptance and evidence map

| Requirement | Delivered behavior | Checks |
|---|---|---|
| W9.1 / A26 | Presplit stores candidates only. Human owner edits/confirms a complete batch before formal tasks, assignments, audit and notifications commit. Trusted full-name resolution, same current group, distinct Agents; same-owner and cross-user pairs supported. | `TestConsoleSpecPresplitAgentIdentityTasksAndFirstResult`, `TestR32AssignmentPairNamesGroupAndSameOwnerRuling`, candidate precommit test, browser candidate edit/confirm; withdrawals owner_confirmation_before_tasks, distinct_agents, current_group_membership. |
| W9.2 / A27 | Dedicated Agent bearer GET/PATCH; current assigned role and current PG identity/group checked on every access. Versioned state transitions, dependency gates, request-id dedup, atomic audit. Cookie/bearer mixing and public-spec task projection bypass denied. | HTTP auth/revocation test, state/dependency/idempotency tests, public-spec fence, actual MCP→Unix IPC→HTTP chain; withdrawals own_task_writer, dependency_done, precommit_pg_recheck, shared_spec_task_privacy. |
| W9.3 / A28 | Developer registers expected frozen cases; distinct reviewer confirms. Re-mark and reassignment clear old confirmations. All cases confirmed before done. Done reassignment requires explicit owner reopen; Agents cannot reopen. | State/cases, reassignment, explicit reopen tests, browser dismiss/accept confirmation; withdrawals review_before_done, developer_no_self_confirm, remark_resets_confirmation, owner_explicit_done_reopen. |
| W9.4 / A27 | SQLite transactional durable outbox; stable logical IDs, old/new role notices, reliable delivery capability negotiation. Offline/old helper waits, does not receive task text as a user instruction. Persistent startup and periodic reconciliation repairs PG-commit/SQLite-crash window without a user request. | Real WS capability/delivery test, PG-backed restart test, outbox rollback, actual old helper serializer; withdrawals outbox_atomicity, background_reconciliation, notification_not_executable, waiting_page_does_not_starve_delivery. |
| W9.5 / A29 | Five-second active-page refresh shows authoritative status, blocker, confirmed cases and reassignment state; hidden page pauses. | New browser 11 checks; withdrawal board_five_second_refresh. |
| Driver inbox / versions | Driver status/list/read/ack use local inbox and independent read markers; no delivery-state or Hub processed ACK changes. Hub/CLI/helper/MCP release 0.2.3.5. | Helper tests, actual IPC chain, release-pin tests; withdrawals driver_ack_local_only, http_503_outcome_unknown. |

`withdrawals/WITHDRAWALS.json` is authoritative for exact test commands, all **22** withdrawal names, markers, source hashes and return codes. Each runs a green positive, a single isolated source withdrawal that fails at its designated marker, then a green restored control; compile failures do not count. Raw stdout/stderr/rc are retained per phase.

## Authorization, consistency and failure semantics

PG group/Agent/active-owner locks are held in an ordinary **read/write SQL transaction with no business writes** while SQLite performs task state, dedup, audit and outbox updates. SQL READ ONLY is rejected and never silently downgraded to unlocked authorization. PG is rechecked immediately before SQLite commit. Actual PG tests observe membership-change lock waits and test a read-only connection.

SQLite is the state commit point; there is no cross-database two-phase commit. If PG release fails after SQLite commit, or helper update HTTP response is 5xx/transport failure, the outcome is unknown rather than falsely reported as rolled back. Stable request_id permits result reconciliation without a duplicate operation. Candidate staging also rechecks PG before commit. Tests inject both before-commit rollback and after-commit uncertainty.

Invalidated assignments lose read/write access immediately, including public-spec projections, before background bookkeeping succeeds. Startup and periodic reconciliation persist needs_reassignment and one logical owner notice after a crash. Ordinary PG outages reject access without falsely classifying a member as removed. Durable Console reassignment state represents the owner notice; no external chat service is implied. Outbox delivery scans stable-key pages beyond persistently queued offline/old-helper/owner rows; board pending projection also includes later pages. Regression coverage includes 203 stored rows and a real WebSocket notice behind a synthetic waiting page.

Old group-less tasks remain on the legacy path until the owner chooses a group and confirms an assignment. They do not gain new Agent channel authorization automatically. Human done-task reopen is explicit and resets previous results. Reassignment notification receipt is not task completion.

## Migration and rollback

Only SQLite ledger `0055_console_task_flow_r32.sql` is added. Identity PG schema is unchanged; R8's planned 0056 is not present. Helper inbox uses its existing local migration mechanism and an independent read-marker table.

`compatibility/COMPATIBILITY.json` records 11 actual scenarios: exact r31.1 store library seed→new upgrade/repeat; new database→old reader/repeat; injected ledger-insert failure fully rolls back added columns/tables and preserves old task data, followed by successful recovery; exact old helper 0.2.3.4 advertises ACK but not task_flow_v1; exact old Hub binary starts on loopback with fake transport and reads the upgraded spec/tasks over HTTP. Probe fixtures are shared, but old and new store libraries are independently built at exact Git anchors. Executables and database files remain local and are not included in Git evidence.

Old binary readability does **not** authorize writable downgrade. First close R32 human/Agent write routes and task drivers, resolve/cancel pending presplit, freeze the outbox, and retain new tables/dedup identifiers. Old binaries may only be used read-only, or use a compatible rollback build retaining the new gates. Frozen 0.2.3.4 container definitions have not been rebuilt or deployed.

## Validation ledger

Author gates completed at 2026-10-19T09:55:15Z: default-parallel full Go rc0, **52 tested packages passed / 13 packages without test files**; vet rc0; canonical Python50/shell2; helper736 rc0; 22 designated withdrawals; 11 compatibility scenarios; browser hosted32/console20/enroll20/vega12/specs220/workflow12/newR32 11 with zero reported assertion failures. Exact commands and return codes are in `GATES.json`; raw logs are under `raw/`. Full `go test` is non-verbose, so its package ledger alone does not establish a count of skipped individual test cases.

Reproduction from the source checkout:

```sh
go vet ./...
go test ./... -count=1 -timeout=30m
PATH=/tmp/coder-0232-locked-venv/bin:$PATH bash scripts/run-build-tests.sh
PYTHONPATH=tools/sessionhelper /tmp/coder-0232-locked-venv/bin/python -m unittest discover -s tools/sessionhelper/tests
python3 tests/r32-task-flow/withdrawals.py --anchor b08add989500f000f9066249661c0310ebb7794c --output /tmp/r32-review-withdrawals
python3 tests/r32-task-flow/compatibility.py --output /tmp/r32-review-compatibility
```

Browser runner: `tools/run-browser-regressions.py`, including new `r32-task-flow` suite; Playwright dependency path and Chrome availability are recorded in environment/raw logs. No production endpoint is used.

Independent dev-env targeted evidence is in review/togpt60lunazzc/hosted-enable-r31 commit `737ed1abdc981db5f110ef26296ce708b2e5ce26`, tree `76f5de6b4a29c869443fbf479263638793bc16ee`, directory `docs/reviews/togpt60lunazzc-r32-task-flow/dev-env-targeted-35558e9e/`. At 09:31:14–09:31:29Z, ubuntu UID1000, umask0002, Go1.22.2 passed store/identity/m8/cmd/workpulse/version selected tests rc0, clean checkout. This is a targeted independent reading, not full-repository or final review PASS. Earlier 2c06ccb9 four-package reading is a predecessor, not a claim about later changes. Latest b08add98 store/m8 targeted run also passed at 09:41:53–09:42:06Z in the same dev-env conditions; original evidence commit `9d9b6b9abb1ac1bec63142452c901431829a2981`, tree `76fcecc7e2a42cbacef59fdc066b0a31c5db68c5`, directory `dev-env-targeted-b08add98/` under that review folder. Docs-only correction `09b18a3e165d5166e34ac58308791032c31f5ac0` (tree `799ce5b952c1ecb8a1e8d940920a41bd1cebc111`) corrects the eight-file delta prose; independently verified diff changes only that report. Exact source coordinates and raw rc0 readings are unchanged.

## Retained failed runs and limits

Earlier full Go runs failed the old new-platform presplit shim (immediate tasks instead of owner confirmation), route-sweep fixtures and the initially omitted Go version synchronization. The product version omission was corrected, not dismissed as environment. Canonical's expected Python count and helper MCP/IPC fakes required updates for the added tests/tools. Initial vet flagged a preexisting test copying sessionClient's mutex; only that fixture was changed to construct a distinct pointer. Initial system-Python helper run lacked required packages; the locked test venv is used for final validation. Those logs remain under raw with their original rc.

The first 35558e9e helper suite executed 736 tests but returned one module teardown error: its live-home isolation guard observed the running seat's inbox database change. No individual business assertion failed in that run; the log is retained and a repeat is separately recorded, rather than rewritten. The first expanded browser reopen test checked its asynchronous dialog too early; the corrected test waits for either the dialog or an unexpected write, and then verifies both cancel and confirm paths. The failed receipt is retained.

No source, binary or private state was uploaded to file storage. No Hub deployment, helper upgrade, production database change, host43/dw123 action, or R8 implementation is included.
