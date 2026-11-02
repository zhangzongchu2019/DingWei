# 最终测试 SKIP 清单

生成UTC：2026-11-02T05:24:34.063281+00:00。逐项保留默认套件的SKIP；显式覆盖与仍未执行分开记录。SKIP本身均不计PASS。

| 用例（含子格） | 原因 | 补充覆盖 |
|---|---|---|
| `TestR31OldBinarySchemaReadCompatibility` | r31_rollback_compat_test.go:23: requires independently built R30 binary via R31_R30_BINARY | 本单未补此独立/延期/浏览器或真实CLI格；不冒称已覆盖。 |
| `TestHLCFaultProcessMatrix` | shutdown_fault_process_test.go:94: isolated fault/watchdog runner tests/hlc/fault_matrix.py | 本次 fault_matrix 实际进程格已显式执行。 |
| `TestHLCBindFailure` | shutdown_fault_process_test.go:329: isolated startup failure runner | 本次 S010-bind 显式执行，实际退1。 |
| `TestHLCLoadMatrix` | shutdown_load_test.go:560: isolated actual-process matrix: tests/hlc/process_matrix.py | 本次 S006 race50/S011/S014 已显式执行。 |
| `TestHLCS007Full` | shutdown_mixed_fault_process_test.go:164: isolated full mixed fault: tests/hlc/s007_full.py | 本次完整混合普通10/race3及变异已显式执行。 |
| `TestDeviceClaimAcrossActualClientAndHub` | claim_client_integration_test.go:33: run scripts/test-desktop-hub-roundtrip.sh for cross-runtime enrollment | canonical 本轮日志有同名 PASS；默认包SKIP保留。 |
| `TestDeviceClaimAcrossActualClientAndPostgres` | claim_client_integration_test.go:33: run scripts/test-desktop-hub-roundtrip.sh for cross-runtime enrollment / claim_client_integration_test.go:45: run scripts/test-desktop-hub-roundtrip.sh for cross-runtime PostgreSQL enrollment | canonical 本轮日志有同名 PASS；默认包SKIP保留。 |
| `TestChangeEvidenceConsoleBrowser` | console_change_evidence_test.go:128: set CHANGEFLOW_PLAYWRIGHT to installed Playwright for Chrome integration | 本单未补此独立/延期/浏览器或真实CLI格；不冒称已覆盖。 |
| `TestMuxProductHTTP1EightSeats` | console_mux_product_test.go:34: set MUX_PUPPETEER to installed puppeteer-core for actual Chrome product integration | 本单未补此独立/延期/浏览器或真实CLI格；不冒称已覆盖。 |
| `TestConsolePlatformPayloadDump` | console_platform_agentgroups_test.go:133: 仅在浏览器闸取真载荷时运行(设 DW_CONSOLE_PLATFORM_PAYLOAD_OUT) | 本单未补此独立/延期/浏览器或真实CLI格；不冒称已覆盖。 |
| `TestConsoleTaskBoardStartedTaskRestrictionsPendingCaseRecordModel` | console_task_board_page_test.go:122: K13 deferred: task case record model does not exist, so started state cannot be derived mechanically | 本单未补此独立/延期/浏览器或真实CLI格；不冒称已覆盖。 |
| `TestDeliveryACKActualCLIQueuedAdmissionAndTerminal/claude` | delivery_ack_real_cli_e2e_linux_test.go:373: INCONCLUSIVE: set DW_ACK_V2_E2E_PYTHON for actual CLI/Hub acceptance | 本单未补此独立/延期/浏览器或真实CLI格；不冒称已覆盖。 |
| `TestDeliveryACKActualCLIQueuedAdmissionAndTerminal/codex` | delivery_ack_real_cli_e2e_linux_test.go:373: INCONCLUSIVE: set DW_ACK_V2_E2E_PYTHON for actual CLI/Hub acceptance | 本单未补此独立/延期/浏览器或真实CLI格；不冒称已覆盖。 |
| `TestDeliveryACKActualCLIQueuedAdmissionAndTerminal/claude_explicit` | delivery_ack_real_cli_e2e_linux_test.go:373: INCONCLUSIVE: set DW_ACK_V2_E2E_PYTHON for actual CLI/Hub acceptance | 本单未补此独立/延期/浏览器或真实CLI格；不冒称已覆盖。 |
| `TestDeliveryACKActualCLIReplaysPredecessorAcrossDisconnect/claude/drop_injected` | delivery_ack_real_cli_e2e_linux_test.go:475: INCONCLUSIVE: set DW_ACK_V2_E2E_PYTHON for actual CLI/Hub acceptance | 本单未补此独立/延期/浏览器或真实CLI格；不冒称已覆盖。 |
| `TestDeliveryACKActualCLIReplaysPredecessorAcrossDisconnect/claude/drop_injected_confirm` | delivery_ack_real_cli_e2e_linux_test.go:475: INCONCLUSIVE: set DW_ACK_V2_E2E_PYTHON for actual CLI/Hub acceptance | 本单未补此独立/延期/浏览器或真实CLI格；不冒称已覆盖。 |
| `TestDeliveryACKActualCLIReplaysPredecessorAcrossDisconnect/codex/drop_injected` | delivery_ack_real_cli_e2e_linux_test.go:475: INCONCLUSIVE: set DW_ACK_V2_E2E_PYTHON for actual CLI/Hub acceptance | 本单未补此独立/延期/浏览器或真实CLI格；不冒称已覆盖。 |
| `TestDeliveryACKActualCLIReplaysPredecessorAcrossDisconnect/codex/drop_injected_confirm` | delivery_ack_real_cli_e2e_linux_test.go：INCONCLUSIVE，未设置 DW_ACK_V2_E2E_PYTHON；子格继承同一门槛。 | 本单未补此独立/延期/浏览器或真实CLI格；不冒称已覆盖。 |
| `TestDeliveryACKActualCLIAdmissionCrashWindows/claude/dispatch_sent_receipt_unread` | delivery_ack_real_cli_e2e_linux_test.go:527: INCONCLUSIVE: set DW_ACK_V2_E2E_PYTHON for actual CLI/Hub acceptance | 本单未补此独立/延期/浏览器或真实CLI格；不冒称已覆盖。 |
| `TestDeliveryACKActualCLIAdmissionCrashWindows/claude/receipt_before_admitted_persist` | delivery_ack_real_cli_e2e_linux_test.go:527: INCONCLUSIVE: set DW_ACK_V2_E2E_PYTHON for actual CLI/Hub acceptance | 本单未补此独立/延期/浏览器或真实CLI格；不冒称已覆盖。 |
| `TestDeliveryACKActualCLIAdmissionCrashWindows/codex/dispatch_sent_receipt_unread` | delivery_ack_real_cli_e2e_linux_test.go:527: INCONCLUSIVE: set DW_ACK_V2_E2E_PYTHON for actual CLI/Hub acceptance | 本单未补此独立/延期/浏览器或真实CLI格；不冒称已覆盖。 |
| `TestDeliveryACKActualCLIAdmissionCrashWindows/codex/receipt_before_admitted_persist` | delivery_ack_real_cli_e2e_linux_test.go：INCONCLUSIVE，未设置 DW_ACK_V2_E2E_PYTHON；子格继承同一门槛。 | 本单未补此独立/延期/浏览器或真实CLI格；不冒称已覆盖。 |
| `TestDeliveryACKActualCLICancelAtomicBoundary/claude` | delivery_ack_real_cli_e2e_linux_test.go:592: INCONCLUSIVE: set DW_ACK_V2_E2E_PYTHON for actual CLI/Hub acceptance | 本单未补此独立/延期/浏览器或真实CLI格；不冒称已覆盖。 |
| `TestDeliveryACKActualCLICancelAtomicBoundary/codex` | delivery_ack_real_cli_e2e_linux_test.go:592: INCONCLUSIVE: set DW_ACK_V2_E2E_PYTHON for actual CLI/Hub acceptance | 本单未补此独立/延期/浏览器或真实CLI格；不冒称已覆盖。 |
| `TestDeliveryACKActualCLILegacyUpgradeKeepsPerDeliveryProvenance/claude` | delivery_ack_real_cli_e2e_linux_test.go:675: INCONCLUSIVE: set DW_ACK_V2_E2E_PYTHON for actual CLI/Hub acceptance | 本单未补此独立/延期/浏览器或真实CLI格；不冒称已覆盖。 |
| `TestDeliveryACKActualCLILegacyUpgradeKeepsPerDeliveryProvenance/codex` | delivery_ack_real_cli_e2e_linux_test.go:675: INCONCLUSIVE: set DW_ACK_V2_E2E_PYTHON for actual CLI/Hub acceptance | 本单未补此独立/延期/浏览器或真实CLI格；不冒称已覆盖。 |
| `TestDeliveryACKActualCLILateWireTerminal` | delivery_ack_real_cli_e2e_linux_test.go:733: INCONCLUSIVE: set DW_ACK_V2_E2E_PYTHON for actual CLI/Hub acceptance | 本单未补此独立/延期/浏览器或真实CLI格；不冒称已覆盖。 |
| `TestDesktopMCPRoundTripAcrossRealHub` | desktop_mcp_roundtrip_test.go:25: run scripts/test-desktop-hub-roundtrip.sh for the cross-runtime gate | canonical 本轮日志有同名 PASS；默认包SKIP保留。 |
| `TestFailureNoticeDriverTurnEndToEnd` | failure_notice_driver_e2e_test.go:24: set DW_FAILURE_NOTICE_PYTHON to locked helper runtime | 本单未补此独立/延期/浏览器或真实CLI格；不冒称已覆盖。 |
| `TestGroupReplyMCP` | group_reply_test.go:104: requires locked helper Python; set DW_SPEC_E2E_PYTHON | 本单未补此独立/延期/浏览器或真实CLI格；不冒称已覆盖。 |
| `TestHelperSelfUpdateEndToEnd/install_restart_confirm` | helper_update_e2e_test.go:75: set DW_HELPER_E2E_PYTHON to locked helper runtime Python | 本单未补此独立/延期/浏览器或真实CLI格；不冒称已覆盖。 |
| `TestHelperSelfUpdateEndToEnd/sha_mismatch_keeps_factory` | helper_update_e2e_test.go:75: set DW_HELPER_E2E_PYTHON to locked helper runtime Python | 本单未补此独立/延期/浏览器或真实CLI格；不冒称已覆盖。 |
| `TestHelperSelfUpdateEndToEnd/new_layout_rejected_keeps_factory_and_projects_reason` | helper_update_e2e_test.go:75: set DW_HELPER_E2E_PYTHON to locked helper runtime Python | 本单未补此独立/延期/浏览器或真实CLI格；不冒称已覆盖。 |
| `TestHelper0232MixedVersionEndToEnd` | helper_update_e2e_test.go:55: explicit immutable release pins required | 本单未补此独立/延期/浏览器或真实CLI格；不冒称已覆盖。 |
| `TestHostedBodySendAcrossRealHub` | hosted_body_send_test.go:23: set explicit isolated integration interpreter | canonical 本轮日志有同名 PASS；默认包SKIP保留。 |
| `TestEnvelopeReadTimeoutEnvironmentChild` | hub_timeout_contract_test.go:50: subprocess probe only | 本单未补此独立/延期/浏览器或真实CLI格；不冒称已覆盖。 |
| `TestPilotQuestionAcrossRealHTTPHelperIPCMCP` | pilot_question_roundtrip_test.go:17: set locked helper Python; canonical runs this local integration | 本单未补此独立/延期/浏览器或真实CLI格；不冒称已覆盖。 |
| `TestPilotTemplateAcrossRealHTTPHelperIPCMCP` | pilot_template_roundtrip_test.go:17: set locked helper Python; canonical runs loopback integration | 本单未补此独立/延期/浏览器或真实CLI格；不冒称已覆盖。 |
| `TestProjectContextAcrossRealHTTPHelperIPCMCP` | project_context_roundtrip_test.go:17: set DW_DESKTOP_INTEGRATION_PYTHON to the locked helper test venv; canonical shell gate runs the cross-runtime set | canonical 本轮日志有同名 PASS；默认包SKIP保留。 |
| `TestSpecMCPEndToEnd` | spec_mcp_e2e_test.go:211: set DW_SPEC_E2E_PYTHON for actual helper/MCP subprocesses | 本单未补此独立/延期/浏览器或真实CLI格；不冒称已覆盖。 |
| `TestBlockerAcrossRealHTTPHelperIPCMCP` | task_blocker_roundtrip_test.go:21: set locked helper Python; canonical runs this local integration | canonical 本轮日志有同名 PASS；默认包SKIP保留。 |
| `TestReceiverPinnedSDKProcess` | lifecycle_sdk_process_test.go:28: process-reaped SDK fixture: run alone with HLC_PINNED_SDK_PROCESS=1 | 本次独立进程显式执行，SDK Start残余1由进程回收。 |
| `TestConsoleTaskLegacyDODMigrationPendingProductionCount` | console_task_lineage_test.go:135: K17 covers new writes only; legacy acceptance_json rows containing "DOD" await the production count before any 0049 migration decision | 本单未补此独立/延期/浏览器或真实CLI格；不冒称已覆盖。 |
| `TestConsoleTaskStartRequiresAcceptanceLinkPendingStartGateRuling` | console_task_lineage_test.go:139: future K7: enforce at the start gate using console_spec_task_acceptance, never acceptance_json; creation remains allowed with zero links until that timing is sealed | 本单未补此独立/延期/浏览器或真实CLI格；不冒称已覆盖。 |

原始来源：

- `docs/reviews/hlc-primary-final-gates/pg-strict.log`
- `docs/reviews/hlc-primary-integrated-tail/m8-race45.log`
- `docs/reviews/hlc-primary-integrated/related-race.log`
- `docs/reviews/hlc-primary-s007-final/cmd-plain.log`
- `docs/reviews/hlc-primary-s007-final/cmd-race.log`

PG严格整包设置 `HUB_TEST_PG_STRICT=1`，无PG缺环境而静默跳过的格。canonical helper Python/shell测试没有测试SKIP输出；bridge遥测字段 `skipped=` 不是测试跳过。
