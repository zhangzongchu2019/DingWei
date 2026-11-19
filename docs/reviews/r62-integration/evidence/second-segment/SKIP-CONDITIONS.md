# Second-segment skip conditions

The PG gate ran with isolated environment variables. Each listed m8/store test was skipped three times, once per `-count=3` iteration. These are the concrete gate/opt-in conditions, not a generic “existing test” classification.

## `internal/m8` (45 unique names)

| Test | Skip condition |
|---|---|
| `TestBlockerAcrossRealHTTPHelperIPCMCP` | Requires `DW_DESKTOP_INTEGRATION_PYTHON` to run the real helper IPC/MCP integration. |
| `TestChangeEvidenceConsoleBrowser` | Requires `CHANGEFLOW_PLAYWRIGHT` to point to the installed Playwright module. |
| `TestConsolePlatformPayloadDump` | Runs only when the browser payload-extraction gate sets `DW_CONSOLE_PLATFORM_PAYLOAD_OUT`. |
| `TestConsoleTaskBoardStartedTaskRestrictionsPendingCaseRecordModel` | Deliberate K13 deferral: case-record model is absent, so started state cannot yet be derived. No environment opt-in exists. |
| `TestDeliveryACKActualCLIAdmissionCrashWindows/claude/dispatch_sent_receipt_unread` | Requires `DW_ACK_V2_E2E_PYTHON` and the actual CLI/Hub acceptance fixture. |
| `TestDeliveryACKActualCLIAdmissionCrashWindows/claude/receipt_before_admitted_persist` | Requires `DW_ACK_V2_E2E_PYTHON` and the actual CLI/Hub acceptance fixture. |
| `TestDeliveryACKActualCLIAdmissionCrashWindows/codex/dispatch_sent_receipt_unread` | Requires `DW_ACK_V2_E2E_PYTHON` and the actual CLI/Hub acceptance fixture. |
| `TestDeliveryACKActualCLIAdmissionCrashWindows/codex/receipt_before_admitted_persist` | Requires `DW_ACK_V2_E2E_PYTHON` and the actual CLI/Hub acceptance fixture. |
| `TestDeliveryACKActualCLICancelAtomicBoundary/claude` | Requires `DW_ACK_V2_E2E_PYTHON` and the actual CLI/Hub acceptance fixture. |
| `TestDeliveryACKActualCLICancelAtomicBoundary/codex` | Requires `DW_ACK_V2_E2E_PYTHON` and the actual CLI/Hub acceptance fixture. |
| `TestDeliveryACKActualCLILateWireTerminal` | Requires `DW_ACK_V2_E2E_PYTHON` and the actual CLI/Hub acceptance fixture. |
| `TestDeliveryACKActualCLILegacyUpgradeKeepsPerDeliveryProvenance/claude` | Requires `DW_ACK_V2_E2E_PYTHON` and the actual CLI/Hub acceptance fixture. |
| `TestDeliveryACKActualCLILegacyUpgradeKeepsPerDeliveryProvenance/codex` | Requires `DW_ACK_V2_E2E_PYTHON` and the actual CLI/Hub acceptance fixture. |
| `TestDeliveryACKActualCLIQueuedAdmissionAndTerminal/claude` | Requires `DW_ACK_V2_E2E_PYTHON` and the actual CLI/Hub acceptance fixture. |
| `TestDeliveryACKActualCLIQueuedAdmissionAndTerminal/claude_explicit` | Requires `DW_ACK_V2_E2E_PYTHON` and the actual CLI/Hub acceptance fixture. |
| `TestDeliveryACKActualCLIQueuedAdmissionAndTerminal/codex` | Requires `DW_ACK_V2_E2E_PYTHON` and the actual CLI/Hub acceptance fixture. |
| `TestDeliveryACKActualCLIReplaysPredecessorAcrossDisconnect/claude/drop_injected` | Requires `DW_ACK_V2_E2E_PYTHON` and the actual CLI/Hub acceptance fixture. |
| `TestDeliveryACKActualCLIReplaysPredecessorAcrossDisconnect/claude/drop_injected_confirm` | Requires `DW_ACK_V2_E2E_PYTHON` and the actual CLI/Hub acceptance fixture. |
| `TestDeliveryACKActualCLIReplaysPredecessorAcrossDisconnect/codex/drop_injected` | Requires `DW_ACK_V2_E2E_PYTHON` and the actual CLI/Hub acceptance fixture. |
| `TestDeliveryACKActualCLIReplaysPredecessorAcrossDisconnect/codex/drop_injected_confirm` | Requires `DW_ACK_V2_E2E_PYTHON` and the actual CLI/Hub acceptance fixture. |
| `TestDesktopMCPRoundTripAcrossRealHub` | Requires `DW_DESKTOP_INTEGRATION_PYTHON` / the isolated desktop Hub round-trip gate. |
| `TestDeviceClaimAcrossActualClientAndHub` | Requires `DW_DESKTOP_INTEGRATION_PYTHON` / the isolated desktop Hub round-trip gate. |
| `TestDeviceClaimAcrossActualClientAndPostgres` | Requires `DW_DESKTOP_INTEGRATION_PYTHON` / the isolated desktop PostgreSQL enrollment gate. |
| `TestEnvelopeReadTimeoutEnvironmentChild` | Child-process-only test; exercised by parent `TestEnvelopeReadTimeoutDerivesFromEnvironment`, which sets `M8_TIMEOUT_CHILD=1`. |
| `TestFailureNoticeDriverTurnEndToEnd` | Requires `DW_FAILURE_NOTICE_PYTHON` to select the locked helper runtime. |
| `TestGroupReplyMCP` | Requires `DW_SPEC_E2E_PYTHON` for the locked helper runtime. |
| `TestHelper0232MixedVersionEndToEnd` | Requires pinned `DW_HELPER_E2E_RELEASE_COMMIT`, `DW_HELPER_E2E_FACTORY_COMMIT`, and `DW_HELPER_E2E_HUB_BINARY` for the immutable old-release compatibility fixture. |
| `TestHelperSelfUpdateEndToEnd/install_restart_confirm` | Requires immutable release pins and `DW_HELPER_E2E_PYTHON` (or `DW_SPEC_E2E_PYTHON`) plus the isolated self-update fixture. |
| `TestHelperSelfUpdateEndToEnd/new_layout_rejected_keeps_factory_and_projects_reason` | Requires immutable release pins and `DW_HELPER_E2E_PYTHON` (or `DW_SPEC_E2E_PYTHON`) plus the isolated self-update fixture. |
| `TestHelperSelfUpdateEndToEnd/sha_mismatch_keeps_factory` | Requires immutable release pins and `DW_HELPER_E2E_PYTHON` (or `DW_SPEC_E2E_PYTHON`) plus the isolated self-update fixture. |
| `TestHostedBodySendAcrossRealHub` | Requires `DW_DESKTOP_INTEGRATION_PYTHON` for the isolated real-Hub sender fixture. |
| `TestHostedBodySendSameRequestIDAcrossCLIProcesses` | Requires `DW_DESKTOP_INTEGRATION_PYTHON` for the isolated multi-process sender fixture. |
| `TestLegacyIngressBurstKeepsFIFOAndCommittedACK` | Requires `DW_DESKTOP_INTEGRATION_PYTHON` for a real legacy helper process. |
| `TestLegacyIngressDefaultCycleLongBusinessKeepsPong` | Requires `DW_DESKTOP_INTEGRATION_PYTHON` for a real legacy helper process. |
| `TestLegacyIngressLimitedWirePongCompetition` | Requires `DW_DESKTOP_INTEGRATION_PYTHON` for a real legacy helper process. |
| `TestLegacyIngressReadPauseResumesWithoutDisconnect` | Requires `DW_DESKTOP_INTEGRATION_PYTHON` for a real legacy helper process. |
| `TestLegacyIngressSmallControlBlockedPhysicalWrite` | Requires `DW_DESKTOP_INTEGRATION_PYTHON` for a real legacy helper process. |
| `TestLegacyIngressSmallControlReadPause` | Requires `DW_DESKTOP_INTEGRATION_PYTHON` for a real legacy helper process. |
| `TestMuxProductHTTP1EightSeats` | Requires `MUX_PUPPETEER` to point to installed `puppeteer-core`. |
| `TestPilotQuestionAcrossRealHTTPHelperIPCMCP` | Requires `DW_DESKTOP_INTEGRATION_PYTHON` for the isolated loopback helper/MCP fixture. |
| `TestPilotTemplateAcrossRealHTTPHelperIPCMCP` | Requires `DW_DESKTOP_INTEGRATION_PYTHON` for the isolated loopback helper/MCP fixture. |
| `TestProjectContextAcrossRealHTTPHelperIPCMCP` | Requires `DW_DESKTOP_INTEGRATION_PYTHON` for the isolated loopback helper/MCP fixture. |
| `TestR60OutboundControlBackpressureRecoversWithSendStatus` | Requires `DW_DESKTOP_INTEGRATION_PYTHON` for the isolated helper IPC integration. |
| `TestSessionHistoryTwentyAgentPublishP99` | Opt-in performance gate; requires `DW_SESSION_HISTORY_PERF=1`. |
| `TestSpecMCPEndToEnd` | Requires `DW_SPEC_E2E_PYTHON` for actual helper/MCP subprocesses. |

## `internal/store/...` (5 unique names)

| Test | Skip condition |
|---|---|
| `TestConsoleTaskLegacyDODMigrationPendingProductionCount` | Deliberate policy deferral: requires production legacy-row count before deciding whether migration 0049 is safe. No environment opt-in exists. |
| `TestConsoleTaskStartRequiresAcceptanceLinkPendingStartGateRuling` | Deliberate K7 deferral pending the start-gate ruling; creation remains allowed until timing is decided. No environment opt-in exists. |
| `TestFeishuStickyOldHubMigrationRollbackCompatibility` | Requires `R14_OLD_HUB_BINARY`, the exact r48 compatibility binary. |
| `TestR10OldR48HubUpgradeAndRollbackOnIsolatedCopy` | Requires `R10_OLD_HUB_BINARY` or fallback `R14_OLD_HUB_BINARY`, the exact r48 binary. |
| `TestR10RestartChildAuthority` | Child-process-only probe; parent `TestR10SeparateProcessRestartPreservesAuthorityAndUnknownNotice` supplies `R10_RESTART_DB`, `R10_RESTART_KEY`, `R10_RESTART_NOW`, and related child variables. |
