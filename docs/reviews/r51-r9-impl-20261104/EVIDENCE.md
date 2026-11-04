# R51 R9 implementation evidence

## Source coordinates

- Base: `opus50tc2k/r50-integration-v2` at `eaf8f01c`.
- Design: `e229c671` (already in base).
- R51 test fixes merged before implementation: `76c72fbe`, merge commit `095768968`.
- Implementation branch: `review/togpt60lunazzc/r51-r9-impl-20261104`.
- Final source tip: `5cede10976b2d16f8a4534d9c62a00614e8bb279`; tree `c27baf4ed93c3a8768561730965adaa174cb1850`.
- R10 and R15 change directories are untouched. The final evidence tip adds only R9-off compatibility tests and this evidence update; it does not change runtime implementation.

## Existing-file attachment points and disabled-state compatibility

The following line coordinates are the zero-context additions in
`git diff --unified=0 eaf8f01c..693cd69c -- internal/m8/hub.go cmd/workpulse/console_routes.go cmd/workpulse/main.go`
(r50 base `eaf8f01c`, submitted R9 tree `693cd69c`). They are additive wiring:

| Existing file | Diff lines | R9 change | Why other input stays on the r50 path |
|---|---|---|---|
| `internal/m8/hub.go` | `+152`, `+430`, `+1606..1608` | Adds a dedicated R9 sender, starts its scheduler, and checks `dispatchR9PMReply` before the existing clarification and ordinary dispatch. | The sender and scheduler both require the exact `DW_PM_DECISION_REQUEST_ENABLED=1` flag. With the flag off, `dispatchR9PMReply` returns `(unmatched, false, nil)` before inspecting reply metadata or querying R9 tables. When on, an unrelated parent is still passed through. |
| `cmd/workpulse/console_routes.go` | `+34..39` | Adds six `/api/console/openspec-pm-*` routes; existing route registrations are not edited. Responsibility writes retain the platform-admin guard. | Each new handler checks the same exact feature flag and returns 404 while disabled, matching the r50 response for an unregistered route. Existing registrations and handlers remain unchanged. |
| `cmd/workpulse/main.go` | `+310` | Configures the R9 Feishu sender after the existing pilot-question sender setup. | `configureR9PMFeishu` delegates to `ConfigureR9PMFeishuSender`; the Hub starts no sender worker unless the exact flag is on and a real gateway is present. No existing sender or receipt callback is replaced. |

The off-state comparison is exercised directly, rather than inferred from the
enabled-path tests:

- `TestR9PMResponsibilityRegisteredRouteIsPlatformAdminOnly`: after confirming
  the enabled admin route, sets `DW_PM_DECISION_REQUEST_ENABLED=0` and sends
  GET/PUT/DELETE responsibility, GET/POST requests, and POST response requests
  as an authenticated platform admin. All six return HTTP 404, the old
  unregistered-route behavior; unauthorized and non-admin enabled reads remain
  401 and 403, and the enabled admin write remains 200.
- `TestR9PMFlagIsExactAndOffSkipsR9Lookup`: empty, `true`, ` 1 `, and `0` all
  leave R9 dispatch unmatched; the R9 repository lookup count is exactly 0.
- `TestR9PMDispatcherPassesNonR9ReplyThrough`: with R9 enabled, a known-shape
  personal Feishu reply whose parent is not an R9 request remains unmatched and
  is not accepted by the R9 store.
- `TestClarificationFeishuPlainReplySupplementsAndReplay` now explicitly sets
  the R9 flag to `0`, then exercises the existing authenticated clarification
  callback through `Hub.Dispatch`. It passes with two posts at floors 1 and 2,
  both receipts sent, exact receipt prefixes `已入第1条` / `已入第2条`, and replay
  creates no third post. This is the existing r50 clarification and receipt
  path under the R9-off setting.
- `TestNewPlatformRouteSweep` passed with 198 registered routes and 604 auth
  requests checked; `TestConsoleTasksCompatibilityRoute` passed, retaining the
  existing task-board 200 and legacy redirects (302). These cover the existing
  Console route/auth surface alongside the six disabled R9 endpoints.

Reproduction on this evidence tip:

```
$ go test ./cmd/workpulse -run '^(TestR9PMResponsibilityRegisteredRouteIsPlatformAdminOnly|TestNewPlatformRouteSweep|TestConsoleTasksCompatibilityRoute)$' -count=1 -v
PASS; cmd/workpulse 0.687s
TestNewPlatformRouteSweep: routes recorded=198 exempt=16 requests checked=604

$ go test ./internal/m8 -run '^(TestR9PMDispatcherPassesNonR9ReplyThrough|TestR9PMFlagIsExactAndOffSkipsR9Lookup|TestClarificationFeishuPlainReplySupplementsAndReplay)$' -count=1 -v
PASS; internal/m8 0.456s
```

## Gates

Times are UTC, captured by the invoking runner. All results below are from the final source tip unless stated otherwise.

| Gate | Command | UTC interval | rc / result |
|---|---|---|---|
| Full verbose strict package set | `HUB_TEST_PG_STRICT=1 go test -v ./internal/store ./internal/m8 ./cmd/workpulse ./internal/r9pm` | 2026-11-04 03:14:01.402–03:17:48.685 | rc 0; store 81.380s, m8 225.807s, cmd/workpulse 208.726s, r9pm 0.002s |
| Build tests | `PATH=/tmp/r9helper-venv/bin:$PATH scripts/run-build-tests.sh` | 2026-11-04 03:14:01.402–03:22:48.989 | rc 0; final line `BUILD_TESTS_OK helper_profiles=2 python=27 shell=2` |
| OpenSpec strict | `NODE_OPTIONS=--disable-warning=ExperimentalWarning /home/dev/.npm/_npx/2e6f94dbd680ce26/node_modules/.bin/openspec validate --all --strict --no-interactive` | 2026-11-04 04:40Z | rc 0; `Totals: 22 passed, 0 failed (22 items)` |
| Race matrix | `go test -race -count=3 -timeout=35m <package> -run <shard regex>` | 2026-11-04 03:26:27.842–04:38:48.704 | all 15 shards rc 0; shard inventory below |

### Race shard inventory

The runner built shards from `go test -list '^Test'`; the `-run` regex in each invocation is the exact set of listed tests. Counts sum to store 372, m8 887, cmd/workpulse 166, and r9pm 3.

| Package | Shard | Listed tests | UTC start–end | rc | Go final line |
|---|---:|---:|---|---:|---|
| store | 1/4 | 93 | 03:26:27.851–03:48:25.128 | 0 | `ok .../internal/store 1316.373s` |
| store | 2/4 | 93 | 03:26:27.851–03:48:04.315 | 0 | `ok .../internal/store 1295.570s` |
| store | 3/4 | 93 | 03:26:27.852–03:45:37.993 | 0 | `ok .../internal/store 1149.244s` |
| store | 4/4 | 93 | 03:26:27.852–03:55:02.630 | 0 | `ok .../internal/store 1713.857s` |
| m8 | 1/8 | 111 | 03:45:37.993–04:10:02.510 | 0 | `ok .../internal/m8 1440.023s` |
| m8 | 2/8 | 111 | 03:48:04.316–04:10:33.273 | 0 | `ok .../internal/m8 1347.222s` |
| m8 | 3/8 | 111 | 03:48:25.128–04:05:26.582 | 0 | `ok .../internal/m8 1019.723s` |
| m8 | 4/8 | 111 | 03:55:02.630–04:18:25.796 | 0 | `ok .../internal/m8 1401.415s` |
| m8 | 5/8 | 111 | 04:05:26.582–04:26:27.864 | 0 | `ok .../internal/m8 1259.567s` |
| m8 | 6/8 | 111 | 04:10:02.511–04:29:34.349 | 0 | `ok .../internal/m8 1170.073s` |
| m8 | 7/8 | 111 | 04:10:33.273–04:27:26.432 | 0 | `ok .../internal/m8 1011.385s` |
| m8 | 8/8 | 110 | 04:18:25.796–04:37:28.314 | 0 | `ok .../internal/m8 1140.713s` |
| cmd/workpulse | 1/2 | 83 | 04:26:27.865–04:37:05.425 | 0 | `ok .../cmd/workpulse 626.957s` |
| cmd/workpulse | 2/2 | 83 | 04:27:26.432–04:38:48.704 | 0 | `ok .../cmd/workpulse 680.665s` |
| internal/r9pm | 1/1 | 3 | 04:29:34.349–04:29:35.625 | 0 | `ok .../internal/r9pm 1.014s` |

### SKIP inventory

The strict verbose full-package run reported 48 skips. Each was an explicit environment/platform/deferred-scope gate; exact test and reason text follows.

- `--- SKIP: TestConsoleTaskLegacyDODMigrationPendingProductionCount (0.00s)` — console_task_lineage_test.go:135: K17 covers new writes only; legacy acceptance_json rows containing "DOD" await the production count before any 0049 migration decision
- `--- SKIP: TestConsoleTaskStartRequiresAcceptanceLinkPendingStartGateRuling (0.00s)` — console_task_lineage_test.go:139: future K7: enforce at the start gate using console_spec_task_acceptance, never acceptance_json; creation remains allowed with zero links until that timing is sealed
- `--- SKIP: TestFeishuStickyOldHubMigrationRollbackCompatibility (0.00s)` — feishu_sticky_compat_test.go:116: R14_OLD_HUB_BINARY not supplied; exact-r48 compatibility fixture is run by the R14 evidence gate
- `--- SKIP: TestR10OldR48HubUpgradeAndRollbackOnIsolatedCopy (0.00s)` — task_dispatch_compat_test.go:196: exact-r48 binary not provided; supplied by the isolated R10 compatibility evidence gate
- `--- SKIP: TestR10RestartChildAuthority (0.00s)` — task_dispatch_restart_test.go:22: subprocess-only probe, exercised by TestR10SeparateProcessRestartPreservesAuthorityAndUnknownNotice
- `--- SKIP: TestDeviceClaimAcrossActualClientAndHub (0.00s)` — claim_client_integration_test.go:33: run scripts/test-desktop-hub-roundtrip.sh for cross-runtime enrollment
- `--- SKIP: TestDeviceClaimAcrossActualClientAndPostgres (0.00s)` — claim_client_integration_test.go:45: run scripts/test-desktop-hub-roundtrip.sh for cross-runtime PostgreSQL enrollment
- `--- SKIP: TestChangeEvidenceConsoleBrowser (0.00s)` — console_change_evidence_test.go:128: set CHANGEFLOW_PLAYWRIGHT to installed Playwright for Chrome integration
- `--- SKIP: TestMuxProductHTTP1EightSeats (0.00s)` — console_mux_product_test.go:34: set MUX_PUPPETEER to installed puppeteer-core for actual Chrome product integration
- `--- SKIP: TestConsolePlatformPayloadDump (0.00s)` — console_platform_agentgroups_test.go:133: 仅在浏览器闸取真载荷时运行(设 DW_CONSOLE_PLATFORM_PAYLOAD_OUT)
- `--- SKIP: TestConsoleTaskBoardStartedTaskRestrictionsPendingCaseRecordModel (0.00s)` — console_task_board_page_test.go:122: K13 deferred: task case record model does not exist, so started state cannot be derived mechanically
- `--- SKIP: TestDeliveryACKActualCLIQueuedAdmissionAndTerminal/claude (0.16s)` — delivery_ack_real_cli_e2e_linux_test.go:373: INCONCLUSIVE: set DW_ACK_V2_E2E_PYTHON for actual CLI/Hub acceptance
- `--- SKIP: TestDeliveryACKActualCLIQueuedAdmissionAndTerminal/codex (0.15s)` — delivery_ack_real_cli_e2e_linux_test.go:373: INCONCLUSIVE: set DW_ACK_V2_E2E_PYTHON for actual CLI/Hub acceptance
- `--- SKIP: TestDeliveryACKActualCLIQueuedAdmissionAndTerminal/claude_explicit (0.16s)` — delivery_ack_real_cli_e2e_linux_test.go:373: INCONCLUSIVE: set DW_ACK_V2_E2E_PYTHON for actual CLI/Hub acceptance
- `--- SKIP: TestDeliveryACKActualCLIReplaysPredecessorAcrossDisconnect/claude/drop_injected (0.16s)` — delivery_ack_real_cli_e2e_linux_test.go:475: INCONCLUSIVE: set DW_ACK_V2_E2E_PYTHON for actual CLI/Hub acceptance
- `--- SKIP: TestDeliveryACKActualCLIReplaysPredecessorAcrossDisconnect/claude/drop_injected_confirm (0.14s)` — delivery_ack_real_cli_e2e_linux_test.go:475: INCONCLUSIVE: set DW_ACK_V2_E2E_PYTHON for actual CLI/Hub acceptance
- `--- SKIP: TestDeliveryACKActualCLIReplaysPredecessorAcrossDisconnect/codex/drop_injected (0.14s)` — delivery_ack_real_cli_e2e_linux_test.go:475: INCONCLUSIVE: set DW_ACK_V2_E2E_PYTHON for actual CLI/Hub acceptance
- `--- SKIP: TestDeliveryACKActualCLIReplaysPredecessorAcrossDisconnect/codex/drop_injected_confirm (0.15s)` — same parent test skip; no separate reason output
- `--- SKIP: TestDeliveryACKActualCLIAdmissionCrashWindows/claude/dispatch_sent_receipt_unread (0.15s)` — delivery_ack_real_cli_e2e_linux_test.go:527: INCONCLUSIVE: set DW_ACK_V2_E2E_PYTHON for actual CLI/Hub acceptance
- `--- SKIP: TestDeliveryACKActualCLIAdmissionCrashWindows/claude/receipt_before_admitted_persist (0.15s)` — delivery_ack_real_cli_e2e_linux_test.go:527: INCONCLUSIVE: set DW_ACK_V2_E2E_PYTHON for actual CLI/Hub acceptance
- `--- SKIP: TestDeliveryACKActualCLIAdmissionCrashWindows/codex/dispatch_sent_receipt_unread (0.17s)` — delivery_ack_real_cli_e2e_linux_test.go:527: INCONCLUSIVE: set DW_ACK_V2_E2E_PYTHON for actual CLI/Hub acceptance
- `--- SKIP: TestDeliveryACKActualCLIAdmissionCrashWindows/codex/receipt_before_admitted_persist (0.15s)` — same parent test skip; no separate reason output
- `--- SKIP: TestDeliveryACKActualCLICancelAtomicBoundary/claude (0.15s)` — delivery_ack_real_cli_e2e_linux_test.go:592: INCONCLUSIVE: set DW_ACK_V2_E2E_PYTHON for actual CLI/Hub acceptance
- `--- SKIP: TestDeliveryACKActualCLICancelAtomicBoundary/codex (0.14s)` — delivery_ack_real_cli_e2e_linux_test.go:592: INCONCLUSIVE: set DW_ACK_V2_E2E_PYTHON for actual CLI/Hub acceptance
- `--- SKIP: TestDeliveryACKActualCLILegacyUpgradeKeepsPerDeliveryProvenance/claude (0.27s)` — delivery_ack_real_cli_e2e_linux_test.go:675: INCONCLUSIVE: set DW_ACK_V2_E2E_PYTHON for actual CLI/Hub acceptance
- `--- SKIP: TestDeliveryACKActualCLILegacyUpgradeKeepsPerDeliveryProvenance/codex (0.26s)` — delivery_ack_real_cli_e2e_linux_test.go:675: INCONCLUSIVE: set DW_ACK_V2_E2E_PYTHON for actual CLI/Hub acceptance
- `--- SKIP: TestDeliveryACKActualCLILateWireTerminal (0.15s)` — delivery_ack_real_cli_e2e_linux_test.go:733: INCONCLUSIVE: set DW_ACK_V2_E2E_PYTHON for actual CLI/Hub acceptance
- `--- SKIP: TestDesktopMCPRoundTripAcrossRealHub (0.00s)` — desktop_mcp_roundtrip_test.go:25: run scripts/test-desktop-hub-roundtrip.sh for the cross-runtime gate
- `--- SKIP: TestFailureNoticeDriverTurnEndToEnd (0.00s)` — failure_notice_driver_e2e_test.go:24: set DW_FAILURE_NOTICE_PYTHON to locked helper runtime
- `--- SKIP: TestGroupReplyMCP (0.00s)` — group_reply_test.go:104: requires locked helper Python; set DW_SPEC_E2E_PYTHON
- `--- SKIP: TestHelperSelfUpdateEndToEnd/install_restart_confirm (0.00s)` — helper_update_e2e_test.go:75: set DW_HELPER_E2E_PYTHON to locked helper runtime Python
- `--- SKIP: TestHelperSelfUpdateEndToEnd/sha_mismatch_keeps_factory (0.00s)` — helper_update_e2e_test.go:75: set DW_HELPER_E2E_PYTHON to locked helper runtime Python
- `--- SKIP: TestHelperSelfUpdateEndToEnd/new_layout_rejected_keeps_factory_and_projects_reason (0.00s)` — helper_update_e2e_test.go:75: set DW_HELPER_E2E_PYTHON to locked helper runtime Python
- `--- SKIP: TestHelper0232MixedVersionEndToEnd (0.00s)` — helper_update_e2e_test.go:55: explicit immutable release pins required
- `--- SKIP: TestHostedBodySendAcrossRealHub (0.00s)` — hosted_body_send_test.go:23: set explicit isolated integration interpreter
- `--- SKIP: TestEnvelopeReadTimeoutEnvironmentChild (0.00s)` — hub_timeout_contract_test.go:50: subprocess probe only
- `--- SKIP: TestPilotQuestionAcrossRealHTTPHelperIPCMCP (0.00s)` — pilot_question_roundtrip_test.go:17: set locked helper Python; canonical runs this local integration
- `--- SKIP: TestPilotTemplateAcrossRealHTTPHelperIPCMCP (0.00s)` — pilot_template_roundtrip_test.go:17: set locked helper Python; canonical runs loopback integration
- `--- SKIP: TestProjectContextAcrossRealHTTPHelperIPCMCP (0.00s)` — project_context_roundtrip_test.go:17: set DW_DESKTOP_INTEGRATION_PYTHON to the locked helper test venv; canonical shell gate runs the cross-runtime set
- `--- SKIP: TestSessionHistoryTwentyAgentPublishP99 (0.00s)` — session_history_test.go:313: set DW_SESSION_HISTORY_PERF=1 for isolated 20-agent latency gate
- `--- SKIP: TestSpecMCPEndToEnd (0.00s)` — spec_mcp_e2e_test.go:211: set DW_SPEC_E2E_PYTHON for actual helper/MCP subprocesses
- `--- SKIP: TestBlockerAcrossRealHTTPHelperIPCMCP (0.00s)` — task_blocker_roundtrip_test.go:21: set locked helper Python; canonical runs this local integration
- `--- SKIP: TestUpgradeRotationKeepsNewCredentialOnSharedWork (0.01s)` — newplat_upgrade_cred_e2e_test.go:42: helper 端到端需要装有 websockets>=15 的 python（DW_HELPER_TEST_PYTHON="python3" 不满足：exit status 1 Traceback (most recent call last):
- `--- SKIP: TestR31OldBinarySchemaReadCompatibility (0.00s)` — r31_rollback_compat_test.go:23: requires independently built R30 binary via R31_R30_BINARY
- `--- SKIP: TestHLCFaultProcessMatrix (0.00s)` — shutdown_fault_process_test.go:94: isolated fault/watchdog runner tests/hlc/fault_matrix.py
- `--- SKIP: TestHLCBindFailure (0.00s)` — shutdown_fault_process_test.go:329: isolated startup failure runner
- `--- SKIP: TestHLCLoadMatrix (0.00s)` — shutdown_load_test.go:560: isolated actual-process matrix: tests/hlc/process_matrix.py
- `--- SKIP: TestHLCS007Full (0.00s)` — shutdown_mixed_fault_process_test.go:164: isolated full mixed fault: tests/hlc/s007_full.py

## Mutation evidence

The mutation suite was run against this source state; each target test failed at the intended semantic assertion. R9-005 was first attempted against the wrong duplicate SQL occurrence and was not red; the corrected mutation changed the proposal supersession update and was red at `r9_pm_migration_test.go:307` (`old response accepted=true err=<nil>`). Harness rc is 0 after checking that exact failure and restoring the file; mutated test rc is 1.

| Mutation | Result |
|---|---|
| R9-001 remove complete decision-point validation | red, rc 1 |
| R9-002 omit second channel outbox row | red, rc 1 |
| R9-003 accept response enum by substring | red, rc 1 |
| R9-004 remove first-response CAS | red, rc 1 |
| R9-005 do not supersede previous proposal | red at old-response acceptance assertion, test rc 1 |
| R9-006 do not advance due reminder slot | red, rc 1 |
| R9-007 default MR receipt/status to written | red, rc 1 |
| R9-008 coerce unknown provider receipt to sent | red, rc 1 |
| R9-009 remove platform-admin route guard | red, rc 1 |
| invert FCS RPC Feishu block | red, rc 1 |
| force R9 flag always on | red, rc 1 |

Corrected R9-005 verification: `go test ./internal/store -run TestR9PMNewContentSupersedesOldGenerationAndLeavesLateEvidence -count=1`; mutated test rc 1, exact failing line above. The unmodified test is covered by the full suite and race matrix.

## Migration binary evidence

The isolated executable probe used explicit temporary HOME, WP_DB_PATH, WP_DATA_DIR, WP_ADDR and WP_INTERNAL_ADDR, no external provider, and removed its temporary directory after capture. It established:

- r50 binary seeded a 72-row schema ledger and healthz returned 200.
- Current R9 binary upgraded a copy to 73 rows; all preceding version/digest/applied_at tuples were unchanged and only `0074_pm_decision_request.sql` was added.
- r50 binary started against the 73-row database; healthz returned 200 and the ledger remained unchanged.
- 0074 DDL SHA-256: `b7291160b56d38a7b4a8852d274477e4b5ab65d076edd110a547f8449d937d64`.
- r50 executable `/tmp/r9-r50-workpulse-vcs`, SHA-256 `738dd406d4065c31f41e0fd926bb119017464c6152b37594d9f3d5c5b10f8f7d`.
- R9 executable `/tmp/r9-current-workpulse`, SHA-256 `4497bb6f32c55cf8dc3fc03289ce2b8ea8ec97f655ec52f8c0dfb238c472d3a1`.
- Both `go version -m` outputs identify Go 1.22.2 and `cmd/workpulse`; build metadata did not include `vcs.revision`. They were built from the named source revisions recorded in the local build commands, not authenticated by embedded revision metadata.

The probe also checked full DDL present / migration ledger missing repairs the ledger, while a partial 0074 object set is rejected. Exact migration run output and database diff were inspected before cleanup.

## Log digests

- Full strict suite log: `/tmp/r9-full-suite.log`, SHA-256 `247c4d0662c2eb4674ebabfdc0a8b5e0a0049dccdbc43cc7ef311ee6c94f0e72`.
- Build-tests log: `/tmp/r9-build-tests-final.log`, SHA-256 `1f6cf95b13624ce5884cb4dd755bb2f3655fd82f114d27de9a07348716f8cd75`.
- Mutation matrix log before corrected R9-005: `/tmp/r9-mutations-final.log`, SHA-256 `08a40204e71dc21f517d30a438596aa87f6e530df30d5d6075018ec4de78760d`; corrected R9-005 output: `/tmp/r9-mut-005.log`, SHA-256 `3c3d21ce6c1a16324628785aefaa173675eb02aeccace431e6bba6f4bc49e9c7`.
- Race runner summary and per-shard logs: `/tmp/r9-race-final/summary.json` and `/tmp/r9-race-final/*.log`.

Independent acceptance has not been recorded by the implementer. No production GO is claimed here.
