# Go SKIP全名与原因

最终短TMPDIR完整整跑；HUB_TEST_PG_STRICT=1、-count=1。48条测试SKIP，含子例；顶层30条。包级无测试文件另记于go-short-summary.json，不计入测试SKIP。原始-v、JSON全日志的本机路径/字节数/SHA见gate-metadata.json。

| 包 | 测试全名 | 原因原文 |
|---|---|---|
| internal/store | TestConsoleTaskLegacyDODMigrationPendingProductionCount | console_task_lineage_test.go:135: K17 covers new writes only; legacy acceptance_json rows containing "DOD" await the production count before any 0049 migration decision |
| internal/store | TestConsoleTaskStartRequiresAcceptanceLinkPendingStartGateRuling | console_task_lineage_test.go:139: future K7: enforce at the start gate using console_spec_task_acceptance, never acceptance_json; creation remains allowed with zero links until that timing is sealed |
| internal/store | TestFeishuStickyOldHubMigrationRollbackCompatibility | feishu_sticky_compat_test.go:116: R14_OLD_HUB_BINARY not supplied; exact-r48 compatibility fixture is run by the R14 evidence gate |
| internal/store | TestR10OldR48HubUpgradeAndRollbackOnIsolatedCopy | task_dispatch_compat_test.go:196: exact-r48 binary not provided; supplied by the isolated R10 compatibility evidence gate |
| internal/store | TestR10RestartChildAuthority | task_dispatch_restart_test.go:22: subprocess-only probe, exercised by TestR10SeparateProcessRestartPreservesAuthorityAndUnknownNotice |
| cmd/workpulse | TestNPSessionPG8To9AndR50BinaryCompatibility | np_session_migration_binary_test.go:26: set independently built current and certified R50 binary paths |
| cmd/workpulse | TestR31OldBinarySchemaReadCompatibility | r31_rollback_compat_test.go:23: requires independently built R30 binary via R31_R30_BINARY |
| cmd/workpulse | TestHLCFaultProcessMatrix | shutdown_fault_process_test.go:94: isolated fault/watchdog runner tests/hlc/fault_matrix.py |
| cmd/workpulse | TestHLCBindFailure | shutdown_fault_process_test.go:329: isolated startup failure runner |
| internal/m8 | TestDeviceClaimAcrossActualClientAndHub | claim_client_integration_test.go:33: run scripts/test-desktop-hub-roundtrip.sh for cross-runtime enrollment |
| internal/m8 | TestDeviceClaimAcrossActualClientAndPostgres | claim_client_integration_test.go:45: run scripts/test-desktop-hub-roundtrip.sh for cross-runtime PostgreSQL enrollment |
| cmd/workpulse | TestHLCLoadMatrix | shutdown_load_test.go:563: isolated actual-process matrix: tests/hlc/process_matrix.py |
| cmd/workpulse | TestHLCS007Full | shutdown_mixed_fault_process_test.go:164: isolated full mixed fault: tests/hlc/s007_full.py |
| internal/m8 | TestChangeEvidenceConsoleBrowser | console_change_evidence_test.go:186: set CHANGEFLOW_PLAYWRIGHT to installed Playwright for Chrome integration |
| internal/m8 | TestMuxProductHTTP1EightSeats | console_mux_product_test.go:34: set MUX_PUPPETEER to installed puppeteer-core for actual Chrome product integration |
| internal/m8 | TestConsolePlatformPayloadDump | console_platform_agentgroups_test.go:133: 仅在浏览器闸取真载荷时运行(设 DW_CONSOLE_PLATFORM_PAYLOAD_OUT) |
| internal/m8 | TestConsoleTaskBoardStartedTaskRestrictionsPendingCaseRecordModel | console_task_board_page_test.go:122: K13 deferred: task case record model does not exist, so started state cannot be derived mechanically |
| internal/m8 | TestDeliveryACKActualCLIQueuedAdmissionAndTerminal/claude | delivery_ack_real_cli_e2e_linux_test.go:373: INCONCLUSIVE: set DW_ACK_V2_E2E_PYTHON for actual CLI/Hub acceptance |
| internal/m8 | TestDeliveryACKActualCLIQueuedAdmissionAndTerminal/codex | delivery_ack_real_cli_e2e_linux_test.go:373: INCONCLUSIVE: set DW_ACK_V2_E2E_PYTHON for actual CLI/Hub acceptance |
| internal/m8 | TestDeliveryACKActualCLIQueuedAdmissionAndTerminal/claude_explicit | delivery_ack_real_cli_e2e_linux_test.go:373: INCONCLUSIVE: set DW_ACK_V2_E2E_PYTHON for actual CLI/Hub acceptance |
| internal/m8 | TestDeliveryACKActualCLIReplaysPredecessorAcrossDisconnect/claude/drop_injected | delivery_ack_real_cli_e2e_linux_test.go:475: INCONCLUSIVE: set DW_ACK_V2_E2E_PYTHON for actual CLI/Hub acceptance |
| internal/m8 | TestDeliveryACKActualCLIReplaysPredecessorAcrossDisconnect/claude/drop_injected_confirm | delivery_ack_real_cli_e2e_linux_test.go:475: INCONCLUSIVE: set DW_ACK_V2_E2E_PYTHON for actual CLI/Hub acceptance |
| internal/m8 | TestDeliveryACKActualCLIReplaysPredecessorAcrossDisconnect/codex/drop_injected | delivery_ack_real_cli_e2e_linux_test.go:475: INCONCLUSIVE: set DW_ACK_V2_E2E_PYTHON for actual CLI/Hub acceptance |
| internal/m8 | TestDeliveryACKActualCLIReplaysPredecessorAcrossDisconnect/codex/drop_injected_confirm | delivery_ack_real_cli_e2e_linux_test.go:475: INCONCLUSIVE: set DW_ACK_V2_E2E_PYTHON for actual CLI/Hub acceptance |
| internal/m8 | TestDeliveryACKActualCLIAdmissionCrashWindows/claude/dispatch_sent_receipt_unread | delivery_ack_real_cli_e2e_linux_test.go:527: INCONCLUSIVE: set DW_ACK_V2_E2E_PYTHON for actual CLI/Hub acceptance |
| internal/m8 | TestDeliveryACKActualCLIAdmissionCrashWindows/claude/receipt_before_admitted_persist | delivery_ack_real_cli_e2e_linux_test.go:527: INCONCLUSIVE: set DW_ACK_V2_E2E_PYTHON for actual CLI/Hub acceptance |
| internal/m8 | TestDeliveryACKActualCLIAdmissionCrashWindows/codex/dispatch_sent_receipt_unread | delivery_ack_real_cli_e2e_linux_test.go:527: INCONCLUSIVE: set DW_ACK_V2_E2E_PYTHON for actual CLI/Hub acceptance |
| internal/m8 | TestDeliveryACKActualCLIAdmissionCrashWindows/codex/receipt_before_admitted_persist | delivery_ack_real_cli_e2e_linux_test.go:527: INCONCLUSIVE: set DW_ACK_V2_E2E_PYTHON for actual CLI/Hub acceptance |
| internal/m8 | TestDeliveryACKActualCLICancelAtomicBoundary/claude | delivery_ack_real_cli_e2e_linux_test.go:592: INCONCLUSIVE: set DW_ACK_V2_E2E_PYTHON for actual CLI/Hub acceptance |
| internal/m8 | TestDeliveryACKActualCLICancelAtomicBoundary/codex | delivery_ack_real_cli_e2e_linux_test.go:592: INCONCLUSIVE: set DW_ACK_V2_E2E_PYTHON for actual CLI/Hub acceptance |
| internal/m8 | TestDeliveryACKActualCLILegacyUpgradeKeepsPerDeliveryProvenance/claude | delivery_ack_real_cli_e2e_linux_test.go:675: INCONCLUSIVE: set DW_ACK_V2_E2E_PYTHON for actual CLI/Hub acceptance |
| internal/m8 | TestDeliveryACKActualCLILegacyUpgradeKeepsPerDeliveryProvenance/codex | delivery_ack_real_cli_e2e_linux_test.go:675: INCONCLUSIVE: set DW_ACK_V2_E2E_PYTHON for actual CLI/Hub acceptance |
| internal/m8 | TestDeliveryACKActualCLILateWireTerminal | delivery_ack_real_cli_e2e_linux_test.go:733: INCONCLUSIVE: set DW_ACK_V2_E2E_PYTHON for actual CLI/Hub acceptance |
| internal/m8 | TestDesktopMCPRoundTripAcrossRealHub | desktop_mcp_roundtrip_test.go:25: run scripts/test-desktop-hub-roundtrip.sh for the cross-runtime gate |
| internal/m8 | TestFailureNoticeDriverTurnEndToEnd | failure_notice_driver_e2e_test.go:24: set DW_FAILURE_NOTICE_PYTHON to locked helper runtime |
| internal/m8 | TestGroupReplyMCP | group_reply_test.go:104: requires locked helper Python; set DW_SPEC_E2E_PYTHON |
| internal/m8 | TestHelperSelfUpdateEndToEnd/install_restart_confirm | helper_update_e2e_test.go:75: set DW_HELPER_E2E_PYTHON to locked helper runtime Python |
| internal/m8 | TestHelperSelfUpdateEndToEnd/sha_mismatch_keeps_factory | helper_update_e2e_test.go:75: set DW_HELPER_E2E_PYTHON to locked helper runtime Python |
| internal/m8 | TestHelperSelfUpdateEndToEnd/new_layout_rejected_keeps_factory_and_projects_reason | helper_update_e2e_test.go:75: set DW_HELPER_E2E_PYTHON to locked helper runtime Python |
| internal/m8 | TestHelper0232MixedVersionEndToEnd | helper_update_e2e_test.go:55: explicit immutable release pins required |
| internal/m8 | TestHostedBodySendAcrossRealHub | hosted_body_send_test.go:23: set explicit isolated integration interpreter |
| internal/m8 | TestEnvelopeReadTimeoutEnvironmentChild | hub_timeout_contract_test.go:50: subprocess probe only |
| internal/m8 | TestPilotQuestionAcrossRealHTTPHelperIPCMCP | pilot_question_roundtrip_test.go:17: set locked helper Python; canonical runs this local integration |
| internal/m8 | TestPilotTemplateAcrossRealHTTPHelperIPCMCP | pilot_template_roundtrip_test.go:17: set locked helper Python; canonical runs loopback integration |
| internal/m8 | TestProjectContextAcrossRealHTTPHelperIPCMCP | project_context_roundtrip_test.go:17: set DW_DESKTOP_INTEGRATION_PYTHON to the locked helper test venv; canonical shell gate runs the cross-runtime set |
| internal/m8 | TestSessionHistoryTwentyAgentPublishP99 | session_history_test.go:520: set DW_SESSION_HISTORY_PERF=1 for isolated 20-agent latency gate |
| internal/m8 | TestSpecMCPEndToEnd | spec_mcp_e2e_test.go:211: set DW_SPEC_E2E_PYTHON for actual helper/MCP subprocesses |
| internal/m8 | TestBlockerAcrossRealHTTPHelperIPCMCP | task_blocker_roundtrip_test.go:21: set locked helper Python; canonical runs this local integration |
