# SKIP逐名与条件

固定2fd源-count=3共84次SKIP（30顶层、54子项），28个不同完整用例名各出现3次。没有把SKIP计入PASS。原始行及上下文在gate-summary.json与full-go3压缩日志。

| 完整用例名 | 次数 | 实际条件 |
|---|---:|---|
| `TestChangeEvidenceConsoleBrowser` | 3 | 未设CHANGEFLOW_PLAYWRIGHT（独立Chrome集成入口） |
| `TestConsolePlatformPayloadDump` | 3 | 未设DW_CONSOLE_PLATFORM_PAYLOAD_OUT，仅浏览器闸提取真载荷时运行 |
| `TestConsoleTaskBoardStartedTaskRestrictionsPendingCaseRecordModel` | 3 | 既有K13延期：task case record模型不存在，不能机械推导started状态 |
| `TestDeliveryACKActualCLIAdmissionCrashWindows/claude/dispatch_sent_receipt_unread` | 3 | 未设DW_ACK_V2_E2E_PYTHON，实际CLI/Hub专项验收入口（INCONCLUSIVE） |
| `TestDeliveryACKActualCLIAdmissionCrashWindows/claude/receipt_before_admitted_persist` | 3 | 未设DW_ACK_V2_E2E_PYTHON，实际CLI/Hub专项验收入口（INCONCLUSIVE） |
| `TestDeliveryACKActualCLIAdmissionCrashWindows/codex/dispatch_sent_receipt_unread` | 3 | 未设DW_ACK_V2_E2E_PYTHON，实际CLI/Hub专项验收入口（INCONCLUSIVE） |
| `TestDeliveryACKActualCLIAdmissionCrashWindows/codex/receipt_before_admitted_persist` | 3 | 未设DW_ACK_V2_E2E_PYTHON，实际CLI/Hub专项验收入口（INCONCLUSIVE） |
| `TestDeliveryACKActualCLICancelAtomicBoundary/claude` | 3 | 未设DW_ACK_V2_E2E_PYTHON，实际CLI/Hub专项验收入口（INCONCLUSIVE） |
| `TestDeliveryACKActualCLICancelAtomicBoundary/codex` | 3 | 未设DW_ACK_V2_E2E_PYTHON，实际CLI/Hub专项验收入口（INCONCLUSIVE） |
| `TestDeliveryACKActualCLILateWireTerminal` | 3 | 未设DW_ACK_V2_E2E_PYTHON，实际CLI/Hub专项验收入口（INCONCLUSIVE） |
| `TestDeliveryACKActualCLILegacyUpgradeKeepsPerDeliveryProvenance/claude` | 3 | 未设DW_ACK_V2_E2E_PYTHON，实际CLI/Hub专项验收入口（INCONCLUSIVE） |
| `TestDeliveryACKActualCLILegacyUpgradeKeepsPerDeliveryProvenance/codex` | 3 | 未设DW_ACK_V2_E2E_PYTHON，实际CLI/Hub专项验收入口（INCONCLUSIVE） |
| `TestDeliveryACKActualCLIQueuedAdmissionAndTerminal/claude` | 3 | 未设DW_ACK_V2_E2E_PYTHON，实际CLI/Hub专项验收入口（INCONCLUSIVE） |
| `TestDeliveryACKActualCLIQueuedAdmissionAndTerminal/claude_explicit` | 3 | 未设DW_ACK_V2_E2E_PYTHON，实际CLI/Hub专项验收入口（INCONCLUSIVE） |
| `TestDeliveryACKActualCLIQueuedAdmissionAndTerminal/codex` | 3 | 未设DW_ACK_V2_E2E_PYTHON，实际CLI/Hub专项验收入口（INCONCLUSIVE） |
| `TestDeliveryACKActualCLIReplaysPredecessorAcrossDisconnect/claude/drop_injected` | 3 | 未设DW_ACK_V2_E2E_PYTHON，实际CLI/Hub专项验收入口（INCONCLUSIVE） |
| `TestDeliveryACKActualCLIReplaysPredecessorAcrossDisconnect/claude/drop_injected_confirm` | 3 | 未设DW_ACK_V2_E2E_PYTHON，实际CLI/Hub专项验收入口（INCONCLUSIVE） |
| `TestDeliveryACKActualCLIReplaysPredecessorAcrossDisconnect/codex/drop_injected` | 3 | 未设DW_ACK_V2_E2E_PYTHON，实际CLI/Hub专项验收入口（INCONCLUSIVE） |
| `TestDeliveryACKActualCLIReplaysPredecessorAcrossDisconnect/codex/drop_injected_confirm` | 3 | 未设DW_ACK_V2_E2E_PYTHON，实际CLI/Hub专项验收入口（INCONCLUSIVE） |
| `TestEnvelopeReadTimeoutEnvironmentChild` | 3 | 仅由父测试启动的subprocess probe；常规整包入口跳过子入口 |
| `TestGroupReplyMCP` | 3 | 未设DW_SPEC_E2E_PYTHON（独立MCP子进程闸） |
| `TestHelper0232MixedVersionEndToEnd` | 3 | 未提供explicit immutable release pins（旧/新helper不可变发布钉点） |
| `TestHelperSelfUpdateEndToEnd/install_restart_confirm` | 3 | 未设DW_HELPER_E2E_PYTHON到锁定helper运行时 |
| `TestHelperSelfUpdateEndToEnd/new_layout_rejected_keeps_factory_and_projects_reason` | 3 | 未设DW_HELPER_E2E_PYTHON到锁定helper运行时 |
| `TestHelperSelfUpdateEndToEnd/sha_mismatch_keeps_factory` | 3 | 未设DW_HELPER_E2E_PYTHON到锁定helper运行时 |
| `TestMuxProductHTTP1EightSeats` | 3 | 未设MUX_PUPPETEER（独立Chrome产品集成入口） |
| `TestSessionHistoryTwentyAgentPublishP99` | 3 | 未设DW_SESSION_HISTORY_PERF=1，独立20席性能闸，本单不压测 |
| `TestSpecMCPEndToEnd` | 3 | 未设DW_SPEC_E2E_PYTHON（独立helper/MCP子进程闸） |
