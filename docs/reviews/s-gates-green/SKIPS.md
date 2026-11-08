# 跳过项

整包默认环境不注入额外集成开关；用相同最小环境单独执行所有静态Skip候选以取实际用例名/原因，原始输出见raw/skip-inventory.log。本轮实际SKIP如下（含子用例），不作为PASS；新增两域拒绝测试没有Skip。跨运行时六项由统一入口脚本明确设置隔离Python后另执行，最终结果见canonical日志。子进程探针由其父测试执行，单独发现时按原条件跳过。CLI端到端父测试即使显示PASS，其子用例SKIP仍在此逐项列出，不把父状态当作真实CLI门。

| 用例 | Skip原因 | 另行覆盖 |
| --- | --- | --- |
| TestConsoleTaskLegacyDODMigrationPendingProductionCount | console_task_lineage_test.go:135: K17 covers new writes only; legacy acceptance_json rows containing "DOD" await the production count before any 0049 migration decision | 本单不宣称该专项通过 |
| TestConsoleTaskStartRequiresAcceptanceLinkPendingStartGateRuling | console_task_lineage_test.go:139: future K7: enforce at the start gate using console_spec_task_acceptance, never acceptance_json; creation remains allowed with zero links until that timing is sealed | 本单不宣称该专项通过 |
| TestFeishuStickyOldHubMigrationRollbackCompatibility | feishu_sticky_compat_test.go:116: R14_OLD_HUB_BINARY not supplied; exact-r48 compatibility fixture is run by the R14 evidence gate | 本单不宣称该专项通过 |
| TestR10OldR48HubUpgradeAndRollbackOnIsolatedCopy | task_dispatch_compat_test.go:196: exact-r48 binary not provided; supplied by the isolated R10 compatibility evidence gate | 本单不宣称该专项通过 |
| TestR10RestartChildAuthority | task_dispatch_restart_test.go:22: subprocess-only probe, exercised by TestR10SeparateProcessRestartPreservesAuthorityAndUnknownNotice | 由父测试启动；独立探针不当成业务场景 |
| TestDeviceClaimAcrossActualClientAndHub | claim_client_integration_test.go:33: run scripts/test-desktop-hub-roundtrip.sh for cross-runtime enrollment | 统一入口test-desktop-hub-roundtrip.sh |
| TestDeviceClaimAcrossActualClientAndPostgres | claim_client_integration_test.go:45: run scripts/test-desktop-hub-roundtrip.sh for cross-runtime PostgreSQL enrollment | 统一入口test-desktop-hub-roundtrip.sh |
| TestChangeEvidenceConsoleBrowser | console_change_evidence_test.go:186: set CHANGEFLOW_PLAYWRIGHT to installed Playwright for Chrome integration | 本单不宣称该专项通过 |
| TestMuxProductHTTP1EightSeats | console_mux_product_test.go:34: set MUX_PUPPETEER to installed puppeteer-core for actual Chrome product integration | 本单不宣称该专项通过 |
| TestConsolePlatformPayloadDump | console_platform_agentgroups_test.go:133: 仅在浏览器闸取真载荷时运行(设 DW_CONSOLE_PLATFORM_PAYLOAD_OUT) | 本单不宣称该专项通过 |
| TestConsoleTaskBoardStartedTaskRestrictionsPendingCaseRecordModel | console_task_board_page_test.go:122: K13 deferred: task case record model does not exist, so started state cannot be derived mechanically | 本单不宣称该专项通过 |
| TestDeliveryACKActualCLIQueuedAdmissionAndTerminal/claude | delivery_ack_real_cli_e2e_linux_test.go:373: INCONCLUSIVE: set DW_ACK_V2_E2E_PYTHON for actual CLI/Hub acceptance | 本单不宣称该专项通过 |
| TestDeliveryACKActualCLIQueuedAdmissionAndTerminal/codex | delivery_ack_real_cli_e2e_linux_test.go:373: INCONCLUSIVE: set DW_ACK_V2_E2E_PYTHON for actual CLI/Hub acceptance | 本单不宣称该专项通过 |
| TestDeliveryACKActualCLIQueuedAdmissionAndTerminal/claude_explicit | delivery_ack_real_cli_e2e_linux_test.go:373: INCONCLUSIVE: set DW_ACK_V2_E2E_PYTHON for actual CLI/Hub acceptance | 本单不宣称该专项通过 |
| TestDeliveryACKActualCLIReplaysPredecessorAcrossDisconnect/claude/drop_injected | delivery_ack_real_cli_e2e_linux_test.go:475: INCONCLUSIVE: set DW_ACK_V2_E2E_PYTHON for actual CLI/Hub acceptance | 本单不宣称该专项通过 |
| TestDeliveryACKActualCLIReplaysPredecessorAcrossDisconnect/claude/drop_injected_confirm | delivery_ack_real_cli_e2e_linux_test.go:475: INCONCLUSIVE: set DW_ACK_V2_E2E_PYTHON for actual CLI/Hub acceptance | 本单不宣称该专项通过 |
| TestDeliveryACKActualCLIReplaysPredecessorAcrossDisconnect/codex/drop_injected | delivery_ack_real_cli_e2e_linux_test.go:475: INCONCLUSIVE: set DW_ACK_V2_E2E_PYTHON for actual CLI/Hub acceptance | 本单不宣称该专项通过 |
| TestDeliveryACKActualCLIReplaysPredecessorAcrossDisconnect/codex/drop_injected_confirm | delivery_ack_real_cli_e2e_linux_test.go:475: INCONCLUSIVE: set DW_ACK_V2_E2E_PYTHON for actual CLI/Hub acceptance | 本单不宣称该专项通过 |
| TestDeliveryACKActualCLIAdmissionCrashWindows/claude/dispatch_sent_receipt_unread | delivery_ack_real_cli_e2e_linux_test.go:527: INCONCLUSIVE: set DW_ACK_V2_E2E_PYTHON for actual CLI/Hub acceptance | 本单不宣称该专项通过 |
| TestDeliveryACKActualCLIAdmissionCrashWindows/claude/receipt_before_admitted_persist | delivery_ack_real_cli_e2e_linux_test.go:527: INCONCLUSIVE: set DW_ACK_V2_E2E_PYTHON for actual CLI/Hub acceptance | 本单不宣称该专项通过 |
| TestDeliveryACKActualCLIAdmissionCrashWindows/codex/dispatch_sent_receipt_unread | delivery_ack_real_cli_e2e_linux_test.go:527: INCONCLUSIVE: set DW_ACK_V2_E2E_PYTHON for actual CLI/Hub acceptance | 本单不宣称该专项通过 |
| TestDeliveryACKActualCLIAdmissionCrashWindows/codex/receipt_before_admitted_persist | delivery_ack_real_cli_e2e_linux_test.go:527: INCONCLUSIVE: set DW_ACK_V2_E2E_PYTHON for actual CLI/Hub acceptance | 本单不宣称该专项通过 |
| TestDeliveryACKActualCLICancelAtomicBoundary/claude | delivery_ack_real_cli_e2e_linux_test.go:592: INCONCLUSIVE: set DW_ACK_V2_E2E_PYTHON for actual CLI/Hub acceptance | 本单不宣称该专项通过 |
| TestDeliveryACKActualCLICancelAtomicBoundary/codex | delivery_ack_real_cli_e2e_linux_test.go:592: INCONCLUSIVE: set DW_ACK_V2_E2E_PYTHON for actual CLI/Hub acceptance | 本单不宣称该专项通过 |
| TestDeliveryACKActualCLILegacyUpgradeKeepsPerDeliveryProvenance/claude | delivery_ack_real_cli_e2e_linux_test.go:675: INCONCLUSIVE: set DW_ACK_V2_E2E_PYTHON for actual CLI/Hub acceptance | 本单不宣称该专项通过 |
| TestDeliveryACKActualCLILegacyUpgradeKeepsPerDeliveryProvenance/codex | delivery_ack_real_cli_e2e_linux_test.go:675: INCONCLUSIVE: set DW_ACK_V2_E2E_PYTHON for actual CLI/Hub acceptance | 本单不宣称该专项通过 |
| TestDeliveryACKActualCLILateWireTerminal | delivery_ack_real_cli_e2e_linux_test.go:733: INCONCLUSIVE: set DW_ACK_V2_E2E_PYTHON for actual CLI/Hub acceptance | 本单不宣称该专项通过 |
| TestDesktopMCPRoundTripAcrossRealHub | desktop_mcp_roundtrip_test.go:25: run scripts/test-desktop-hub-roundtrip.sh for the cross-runtime gate | 统一入口test-desktop-hub-roundtrip.sh |
| TestFailureNoticeDriverTurnEndToEnd | failure_notice_driver_e2e_test.go:24: set DW_FAILURE_NOTICE_PYTHON to locked helper runtime | 本单不宣称该专项通过 |
| TestGroupReplyMCP | group_reply_test.go:104: requires locked helper Python; set DW_SPEC_E2E_PYTHON | 本单不宣称该专项通过 |
| TestHelperSelfUpdateEndToEnd/install_restart_confirm | helper_update_e2e_test.go:75: set DW_HELPER_E2E_PYTHON to locked helper runtime Python | 本单不宣称该专项通过 |
| TestHelperSelfUpdateEndToEnd/sha_mismatch_keeps_factory | helper_update_e2e_test.go:75: set DW_HELPER_E2E_PYTHON to locked helper runtime Python | 本单不宣称该专项通过 |
| TestHelperSelfUpdateEndToEnd/new_layout_rejected_keeps_factory_and_projects_reason | helper_update_e2e_test.go:75: set DW_HELPER_E2E_PYTHON to locked helper runtime Python | 本单不宣称该专项通过 |
| TestHelper0232MixedVersionEndToEnd | helper_update_e2e_test.go:55: explicit immutable release pins required | 本单不宣称该专项通过 |
| TestHostedBodySendAcrossRealHub | hosted_body_send_test.go:23: set explicit isolated integration interpreter | 统一入口test-desktop-hub-roundtrip.sh |
| TestEnvelopeReadTimeoutEnvironmentChild | hub_timeout_contract_test.go:50: subprocess probe only | 由父测试启动；独立探针不当成业务场景 |
| TestPilotQuestionAcrossRealHTTPHelperIPCMCP | pilot_question_roundtrip_test.go:17: set locked helper Python; canonical runs this local integration | 本单不宣称该专项通过 |
| TestPilotTemplateAcrossRealHTTPHelperIPCMCP | pilot_template_roundtrip_test.go:17: set locked helper Python; canonical runs loopback integration | 本单不宣称该专项通过 |
| TestProjectContextAcrossRealHTTPHelperIPCMCP | project_context_roundtrip_test.go:17: set DW_DESKTOP_INTEGRATION_PYTHON to the locked helper test venv; canonical shell gate runs the cross-runtime set | 统一入口test-desktop-hub-roundtrip.sh |
| TestSessionHistoryTwentyAgentPublishP99 | session_history_test.go:356: set DW_SESSION_HISTORY_PERF=1 for isolated 20-agent latency gate | 本单不宣称该专项通过 |
| TestSpecMCPEndToEnd | spec_mcp_e2e_test.go:211: set DW_SPEC_E2E_PYTHON for actual helper/MCP subprocesses | 由父测试启动；独立探针不当成业务场景 |
| TestBlockerAcrossRealHTTPHelperIPCMCP | task_blocker_roundtrip_test.go:21: set locked helper Python; canonical runs this local integration | 统一入口test-desktop-hub-roundtrip.sh |
