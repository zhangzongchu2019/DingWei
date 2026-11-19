# R62 integration first segment — independent integration record

UTC window: 2026-11-19. Repository branch: `togpt60lunazzc/r62-integration`. This record covers the first-segment integration and local gates; it does not authorize main, deployment, or release artifacts.

## Anchors and merge order

- Base: `e07ad81ebea94ba41316b011df751ab400091cc1`.
- Final source anchor: `84d56286229ae24f63f4ec016c87a292caba6b3f`; tree `a83daed1be0a71b6b1e29288b5e410a81fac790e`.
- Ordered `--no-ff` merges: qwen `r62-mobile` `1055ec05` → coder56 `r62-extseat-ttl` `3639406c` → local `r62-cli-ready` `95892dc0` → qwen `r62-cli-view-b` `53b296be` → local `r62-extseat-name` `ceab869c` → coder56 `r62-extseat-groups` `4fd154f8` → local `r62-extseat-copy` `bd8d238b`.
- Excluded by design: `r62-extseat-ttl` comparison tip `97df7db9` and `r62-extseat-design` `124d24ee`.

## Conflict choices and integration fixes

- Mobile mutation whitelist: retained `restore-slimmed-entries` for the removed Specs menu entry and `silent-group-action-feedback` from R61. I ran both mutations: `restore-slimmed-entries` returned rc 1 at the owner-menu and slim-menu count assertions; `silent-group-action-feedback` returned rc 1 with group-action feedback assertions red. Its run also had a separate zero-group workflow timeout; it still failed at the targeted business feedback assertions, not compilation or panic. Both mutation tokens remain in the allowlist and fixture code.
- Name + TTL: kept Unicode display-name helpers alongside the seven-day lifetime comment; kept the R61 `CONDITION_WAIT_MS` / timeout reset and asserted canonical full names in the group selector.
- Groups + name: preserved display-name/principal checks, tenant scoping and nullable AG compatibility; maintained migration order `0112` then `0113`; updated migration fixtures, APIs, and browser suite registration.
- Copy + prior slices: preserved `0112` display name, `0113` groups and `0114` copy in PG fixtures, identity schema, E2E setup, migration ledger, and count assertions.
- Follow-up test-only commit `1d9dbd59`: corrected dual-track migration comparison (source DDL already includes `display_name`) and full-ledger replay count (19).
- Full browser run then caught a merge omission: group rendering called `ownedAgentLabel` without the name-slice helper. Commit `84d56286` restored the global helper while retaining group candidate and timeout behavior. Targeted groups/console tests and the final full runner pass after this fix.

## Gates

All Go gates used isolated `env -i`, Go 1.22 toolchain, `GOPROXY=off`, and `HUB_TEST_PG_STRICT=1` for PG-backed packages. Filtered outcome JSONL files preserve timestamps, package, test name, and pass/skip/fail action while omitting test output bodies and synthetic identifiers. `gate-results.json` records source-log hashes and full unique skip-name lists.

- Build: `scripts/run-build-tests.sh`, `SOURCE_COMMIT=84d56286229ae24f63f4ec016c87a292caba6b3f`; rc 0; `BUILD_TESTS_OK helper_profiles=2 python=27 shell=2`; inventory 97.
- OpenSpec: `openspec validate --all --strict`; rc 0, 50 passed / 0 failed.
- Full browser runner: rc 0, all 54 suites passed (`aggregate_rc=0`). Receipt in `evidence/browser-full-runner-receipt.txt`.
- `internal/store/... -count=3 -p=1`: rc 0, 2,358 pass events, 15 skip events over 5 unique skipped tests.
- `internal/extseat -count=3 -p=1`: rc 0, 222 pass events, no skips.
- `internal/extshared -count=3 -p=1`: rc 0, 33 pass events, no skips.
- `internal/tempgroup -count=3 -p=1`: rc 0, 69 pass events, no skips.
- `internal/m8 -count=3 -p=1`: rc 1 solely due to approved Z-3 cell `TestNewSessionNameNotifiesFeishuOnceAndReconnectFlappingStaySilent` returning SQLite `interrupted (9)` once. Across the three iterations: 4,949 pass events, 135 skip events (45 unique names), 1 fail event. No other failure. This is reported as an allowed known-cell interruption, not as a green package gate.

### Z-3 five-round comparison

Each round was a complete serial `go test -json -count=1 -p=1 -timeout=30m ./internal/m8` with PG strict. The same allowed set was used at both anchors: `TestNewSessionNameNotifiesFeishuOnceAndReconnectFlappingStaySilent`, `TestSessionNameReconnectKeepsNameAndReleaseReuses`, and `TestViewMentionRouteFallsBackToRegisteredACK`.

| Anchor | Round 1 | Round 2 | Round 3 | Round 4 | Round 5 | Full-green rounds |
|---|---|---|---|---|---|---:|
| 84d562862 | green | allowed interrupt: TestNewSessionNameNotifiesFeishuOnceAndReconnectFlappingStaySilent | green | green | green | 4/5 |
| e07ad81e | allowed interrupt: TestSessionNameReconnectKeepsNameAndReleaseReuses | green | green | green | green | 4/5 |

Both anchors have 4/5 full-green rounds; each non-green round failed only in an approved interrupt(9) cell. The additional focused same-cell sample ran each of the three tests 25 times: integration recorded 4/25 `TestSessionNameReconnect...` and 1/25 `TestNewSession...`; e07 recorded 1/25 and 2/25 respectively; `TestViewMentionRoute...` was 0/25 at both. These small samples do not establish a frequency change.

### R15 same-cell comparison

On both e07 and the final integration anchor, five serial pagination runs and one history-mutation run all returned rc 0. The three deletion mutations for history order, scroll-anchor correction and live eviction each returned rc 2 at their expected business assertions. Per-run outputs are in `gate-results.json`.

### Skip inventory

Unique skip names from the three m8 iterations (each appeared three times):

- `TestBlockerAcrossRealHTTPHelperIPCMCP`
- `TestChangeEvidenceConsoleBrowser`
- `TestConsolePlatformPayloadDump`
- `TestConsoleTaskBoardStartedTaskRestrictionsPendingCaseRecordModel`
- `TestDeliveryACKActualCLIAdmissionCrashWindows/claude/dispatch_sent_receipt_unread`
- `TestDeliveryACKActualCLIAdmissionCrashWindows/claude/receipt_before_admitted_persist`
- `TestDeliveryACKActualCLIAdmissionCrashWindows/codex/dispatch_sent_receipt_unread`
- `TestDeliveryACKActualCLIAdmissionCrashWindows/codex/receipt_before_admitted_persist`
- `TestDeliveryACKActualCLICancelAtomicBoundary/claude`
- `TestDeliveryACKActualCLICancelAtomicBoundary/codex`
- `TestDeliveryACKActualCLILateWireTerminal`
- `TestDeliveryACKActualCLILegacyUpgradeKeepsPerDeliveryProvenance/claude`
- `TestDeliveryACKActualCLILegacyUpgradeKeepsPerDeliveryProvenance/codex`
- `TestDeliveryACKActualCLIQueuedAdmissionAndTerminal/claude`
- `TestDeliveryACKActualCLIQueuedAdmissionAndTerminal/claude_explicit`
- `TestDeliveryACKActualCLIQueuedAdmissionAndTerminal/codex`
- `TestDeliveryACKActualCLIReplaysPredecessorAcrossDisconnect/claude/drop_injected`
- `TestDeliveryACKActualCLIReplaysPredecessorAcrossDisconnect/claude/drop_injected_confirm`
- `TestDeliveryACKActualCLIReplaysPredecessorAcrossDisconnect/codex/drop_injected`
- `TestDeliveryACKActualCLIReplaysPredecessorAcrossDisconnect/codex/drop_injected_confirm`
- `TestDesktopMCPRoundTripAcrossRealHub`
- `TestDeviceClaimAcrossActualClientAndHub`
- `TestDeviceClaimAcrossActualClientAndPostgres`
- `TestEnvelopeReadTimeoutEnvironmentChild`
- `TestFailureNoticeDriverTurnEndToEnd`
- `TestGroupReplyMCP`
- `TestHelper0232MixedVersionEndToEnd`
- `TestHelperSelfUpdateEndToEnd/install_restart_confirm`
- `TestHelperSelfUpdateEndToEnd/new_layout_rejected_keeps_factory_and_projects_reason`
- `TestHelperSelfUpdateEndToEnd/sha_mismatch_keeps_factory`
- `TestHostedBodySendAcrossRealHub`
- `TestHostedBodySendSameRequestIDAcrossCLIProcesses`
- `TestLegacyIngressBurstKeepsFIFOAndCommittedACK`
- `TestLegacyIngressDefaultCycleLongBusinessKeepsPong`
- `TestLegacyIngressLimitedWirePongCompetition`
- `TestLegacyIngressReadPauseResumesWithoutDisconnect`
- `TestLegacyIngressSmallControlBlockedPhysicalWrite`
- `TestLegacyIngressSmallControlReadPause`
- `TestMuxProductHTTP1EightSeats`
- `TestPilotQuestionAcrossRealHTTPHelperIPCMCP`
- `TestPilotTemplateAcrossRealHTTPHelperIPCMCP`
- `TestProjectContextAcrossRealHTTPHelperIPCMCP`
- `TestR60OutboundControlBackpressureRecoversWithSendStatus`
- `TestSessionHistoryTwentyAgentPublishP99`
- `TestSpecMCPEndToEnd`

Unique skip names from store count=3 (each appeared three times):

- `TestConsoleTaskLegacyDODMigrationPendingProductionCount`
- `TestConsoleTaskStartRequiresAcceptanceLinkPendingStartGateRuling`
- `TestFeishuStickyOldHubMigrationRollbackCompatibility`
- `TestR10OldR48HubUpgradeAndRollbackOnIsolatedCopy`
- `TestR10RestartChildAuthority`

## Evidence and scope

`evidence/gate-results.json` contains exact per-gate counts, round results, skip inventories, browser receipt metadata and source-log SHA256 values. The `*-outcomes.jsonl` files are filtered outcome streams; they intentionally omit raw application/test output.

No main branch update, deployment, or release build was performed.
