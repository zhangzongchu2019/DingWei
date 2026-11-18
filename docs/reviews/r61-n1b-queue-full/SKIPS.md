# m8/store count=3 skipped cases

The final `-count=3` log has 96 skip records, representing these 32 unique tests (each skipped in all three repetitions). Conditions below are copied from the test skip text.

| Test | Skip condition |
| --- | --- |
| `TestBlockerAcrossRealHTTPHelperIPCMCP` | set locked helper Python; canonical runs this local integration |
| `TestChangeEvidenceConsoleBrowser` | set `CHANGEFLOW_PLAYWRIGHT` to installed Playwright for Chrome integration |
| `TestConsolePlatformPayloadDump` | 仅在浏览器闸取真载荷时运行（设 `DW_CONSOLE_PLATFORM_PAYLOAD_OUT`） |
| `TestConsoleTaskBoardStartedTaskRestrictionsPendingCaseRecordModel` | K13 deferred: task case record model does not exist, so started state cannot be derived mechanically |
| `TestConsoleTaskLegacyDODMigrationPendingProductionCount` | K17 covers new writes only; legacy acceptance_json rows containing “DOD” await production count before any 0049 migration decision |
| `TestConsoleTaskStartRequiresAcceptanceLinkPendingStartGateRuling` | future K7: enforce at the start gate using `console_spec_task_acceptance`, never `acceptance_json`; creation remains allowed with zero links until timing is sealed |
| `TestDeliveryACKActualCLILateWireTerminal` | INCONCLUSIVE: set `DW_ACK_V2_E2E_PYTHON` for actual CLI/Hub acceptance |
| `TestDesktopMCPRoundTripAcrossRealHub` | run `scripts/test-desktop-hub-roundtrip.sh` for the cross-runtime gate |
| `TestDeviceClaimAcrossActualClientAndHub` | run `scripts/test-desktop-hub-roundtrip.sh` for cross-runtime enrollment |
| `TestDeviceClaimAcrossActualClientAndPostgres` | run `scripts/test-desktop-hub-roundtrip.sh` for cross-runtime PostgreSQL enrollment |
| `TestEnvelopeReadTimeoutEnvironmentChild` | subprocess probe only |
| `TestFailureNoticeDriverTurnEndToEnd` | set `DW_FAILURE_NOTICE_PYTHON` to locked helper runtime |
| `TestFeishuStickyOldHubMigrationRollbackCompatibility` | `R14_OLD_HUB_BINARY` not supplied; exact-r48 compatibility fixture is run by the R14 evidence gate |
| `TestGroupReplyMCP` | requires locked helper Python; set `DW_SPEC_E2E_PYTHON` |
| `TestHelper0232MixedVersionEndToEnd` | explicit immutable release pins required |
| `TestHostedBodySendAcrossRealHub` | set explicit isolated integration interpreter |
| `TestHostedBodySendSameRequestIDAcrossCLIProcesses` | set explicit isolated integration interpreter |
| `TestLegacyIngressBurstKeepsFIFOAndCommittedACK` | set explicit isolated integration interpreter for real legacy helper |
| `TestLegacyIngressDefaultCycleLongBusinessKeepsPong` | set explicit isolated integration interpreter for real legacy helper |
| `TestLegacyIngressLimitedWirePongCompetition` | set explicit isolated integration interpreter for real legacy helper |
| `TestLegacyIngressReadPauseResumesWithoutDisconnect` | set explicit isolated integration interpreter for real legacy helper |
| `TestLegacyIngressSmallControlBlockedPhysicalWrite` | set explicit isolated integration interpreter for real legacy helper |
| `TestLegacyIngressSmallControlReadPause` | set explicit isolated integration interpreter for real legacy helper |
| `TestMuxProductHTTP1EightSeats` | set `MUX_PUPPETEER` to installed puppeteer-core for actual Chrome product integration |
| `TestPilotQuestionAcrossRealHTTPHelperIPCMCP` | set locked helper Python; canonical runs this local integration |
| `TestPilotTemplateAcrossRealHTTPHelperIPCMCP` | set locked helper Python; canonical runs loopback integration |
| `TestProjectContextAcrossRealHTTPHelperIPCMCP` | set `DW_DESKTOP_INTEGRATION_PYTHON` to the locked helper test venv; canonical shell gate runs the cross-runtime set |
| `TestR10OldR48HubUpgradeAndRollbackOnIsolatedCopy` | exact-r48 binary not provided; supplied by the isolated R10 compatibility evidence gate |
| `TestR10RestartChildAuthority` | subprocess-only probe, exercised by `TestR10SeparateProcessRestartPreservesAuthorityAndUnknownNotice` |
| `TestR60OutboundControlBackpressureRecoversWithSendStatus` | set explicit isolated integration interpreter |
| `TestSessionHistoryTwentyAgentPublishP99` | set `DW_SESSION_HISTORY_PERF=1` for isolated 20-agent latency gate |
| `TestSpecMCPEndToEnd` | set `DW_SPEC_E2E_PYTHON` for actual helper/MCP subprocesses |
