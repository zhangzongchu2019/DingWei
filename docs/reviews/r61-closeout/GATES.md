# R61 openSpec 结项门读数

执行：opus50tc2k.zzc，dev 用户，2026-11-19T10:31:17Z 记；本机 nproc 16，go version go1.22.2 linux/amd64，OpenSpec 1.16.0（Node v22.23.1）。基于文档 tip 0bc90cecf（发布产品提交 4a349ee59a1d8d7b8b9c562c462bded2f8b0f192），本提交只改 openspec/ 与 docs/reviews/r61-closeout/。跑测前剥除继承的 SH_* 与 DINGWEI_* 环境变量。

- openspec validate --all --strict：47 passed，0 failed。
- go test -count=1 ./cmd/workpulse ./internal/{changeflow,coordinatereview,specconfirm,specread,taskdispatch,store}：rc=0（workpulse 228.381s、store 91.977s、taskdispatch 36.680s），10:23:04Z～10:26:54Z。
- go test -count=1 -v -timeout 580s ./internal/m8/：rc=0，256.922s，10:26:59Z～10:31:17Z；PASS 行（含子测试）1648、FAIL 0、SKIP 45（顶层 27）。跑后 19998 端口无监听、无残留测试进程。

## m8 SKIP 条件（逐名）

- TestDeviceClaimAcrossActualClientAndHub：claim_client_integration_test.go:33: run scripts/test-desktop-hub-roundtrip.sh for cross-runtime enrollment
- TestDeviceClaimAcrossActualClientAndPostgres：claim_client_integration_test.go:45: run scripts/test-desktop-hub-roundtrip.sh for cross-runtime PostgreSQL enrollment
- TestChangeEvidenceConsoleBrowser：console_change_evidence_test.go:186: set CHANGEFLOW_PLAYWRIGHT to installed Playwright for Chrome integration
- TestMuxProductHTTP1EightSeats：console_mux_product_test.go:34: set MUX_PUPPETEER to installed puppeteer-core for actual Chrome product integration
- TestConsolePlatformPayloadDump：console_platform_agentgroups_test.go:133: 仅在浏览器闸取真载荷时运行(设 DW_CONSOLE_PLATFORM_PAYLOAD_OUT)
- TestConsoleTaskBoardStartedTaskRestrictionsPendingCaseRecordModel：console_task_board_page_test.go:122: K13 deferred: task case record model does not exist, so started state cannot be derived mechanically
- TestDeliveryACKActualCLIQueuedAdmissionAndTerminal/claude：delivery_ack_real_cli_e2e_linux_test.go:373: INCONCLUSIVE: set DW_ACK_V2_E2E_PYTHON for actual CLI/Hub acceptance
- TestDeliveryACKActualCLIQueuedAdmissionAndTerminal/codex：delivery_ack_real_cli_e2e_linux_test.go:373: INCONCLUSIVE: set DW_ACK_V2_E2E_PYTHON for actual CLI/Hub acceptance
- TestDeliveryACKActualCLIQueuedAdmissionAndTerminal/claude_explicit：delivery_ack_real_cli_e2e_linux_test.go:373: INCONCLUSIVE: set DW_ACK_V2_E2E_PYTHON for actual CLI/Hub acceptance
- TestDeliveryACKActualCLIReplaysPredecessorAcrossDisconnect/claude/drop_injected：delivery_ack_real_cli_e2e_linux_test.go:475: INCONCLUSIVE: set DW_ACK_V2_E2E_PYTHON for actual CLI/Hub acceptance
- TestDeliveryACKActualCLIReplaysPredecessorAcrossDisconnect/claude/drop_injected_confirm：delivery_ack_real_cli_e2e_linux_test.go:475: INCONCLUSIVE: set DW_ACK_V2_E2E_PYTHON for actual CLI/Hub acceptance
- TestDeliveryACKActualCLIReplaysPredecessorAcrossDisconnect/codex/drop_injected：delivery_ack_real_cli_e2e_linux_test.go:475: INCONCLUSIVE: set DW_ACK_V2_E2E_PYTHON for actual CLI/Hub acceptance
- TestDeliveryACKActualCLIReplaysPredecessorAcrossDisconnect/codex/drop_injected_confirm：delivery_ack_real_cli_e2e_linux_test.go:475: INCONCLUSIVE: set DW_ACK_V2_E2E_PYTHON for actual CLI/Hub acceptance
- TestDeliveryACKActualCLIAdmissionCrashWindows/claude/dispatch_sent_receipt_unread：delivery_ack_real_cli_e2e_linux_test.go:527: INCONCLUSIVE: set DW_ACK_V2_E2E_PYTHON for actual CLI/Hub acceptance
- TestDeliveryACKActualCLIAdmissionCrashWindows/claude/receipt_before_admitted_persist：delivery_ack_real_cli_e2e_linux_test.go:527: INCONCLUSIVE: set DW_ACK_V2_E2E_PYTHON for actual CLI/Hub acceptance
- TestDeliveryACKActualCLIAdmissionCrashWindows/codex/dispatch_sent_receipt_unread：delivery_ack_real_cli_e2e_linux_test.go:527: INCONCLUSIVE: set DW_ACK_V2_E2E_PYTHON for actual CLI/Hub acceptance
- TestDeliveryACKActualCLIAdmissionCrashWindows/codex/receipt_before_admitted_persist：delivery_ack_real_cli_e2e_linux_test.go:527: INCONCLUSIVE: set DW_ACK_V2_E2E_PYTHON for actual CLI/Hub acceptance
- TestDeliveryACKActualCLICancelAtomicBoundary/claude：delivery_ack_real_cli_e2e_linux_test.go:592: INCONCLUSIVE: set DW_ACK_V2_E2E_PYTHON for actual CLI/Hub acceptance
- TestDeliveryACKActualCLICancelAtomicBoundary/codex：delivery_ack_real_cli_e2e_linux_test.go:592: INCONCLUSIVE: set DW_ACK_V2_E2E_PYTHON for actual CLI/Hub acceptance
- TestDeliveryACKActualCLILegacyUpgradeKeepsPerDeliveryProvenance/claude：delivery_ack_real_cli_e2e_linux_test.go:675: INCONCLUSIVE: set DW_ACK_V2_E2E_PYTHON for actual CLI/Hub acceptance
- TestDeliveryACKActualCLILegacyUpgradeKeepsPerDeliveryProvenance/codex：delivery_ack_real_cli_e2e_linux_test.go:675: INCONCLUSIVE: set DW_ACK_V2_E2E_PYTHON for actual CLI/Hub acceptance
- TestDeliveryACKActualCLILateWireTerminal：delivery_ack_real_cli_e2e_linux_test.go:733: INCONCLUSIVE: set DW_ACK_V2_E2E_PYTHON for actual CLI/Hub acceptance
- TestDesktopMCPRoundTripAcrossRealHub：desktop_mcp_roundtrip_test.go:25: run scripts/test-desktop-hub-roundtrip.sh for the cross-runtime gate
- TestFailureNoticeDriverTurnEndToEnd：failure_notice_driver_e2e_test.go:24: set DW_FAILURE_NOTICE_PYTHON to locked helper runtime
- TestGroupReplyMCP：group_reply_test.go:104: requires locked helper Python; set DW_SPEC_E2E_PYTHON
- TestHelperSelfUpdateEndToEnd/install_restart_confirm：helper_update_e2e_test.go:75: set DW_HELPER_E2E_PYTHON to locked helper runtime Python
- TestHelperSelfUpdateEndToEnd/sha_mismatch_keeps_factory：helper_update_e2e_test.go:75: set DW_HELPER_E2E_PYTHON to locked helper runtime Python
- TestHelperSelfUpdateEndToEnd/new_layout_rejected_keeps_factory_and_projects_reason：helper_update_e2e_test.go:75: set DW_HELPER_E2E_PYTHON to locked helper runtime Python
- TestHelper0232MixedVersionEndToEnd：helper_update_e2e_test.go:55: explicit immutable release pins required
- TestHostedBodySendAcrossRealHub：hosted_body_send_test.go:42: set explicit isolated integration interpreter
- TestHostedBodySendSameRequestIDAcrossCLIProcesses：hosted_body_send_test.go:42: set explicit isolated integration interpreter
- TestEnvelopeReadTimeoutEnvironmentChild：hub_timeout_contract_test.go:50: subprocess probe only
- TestLegacyIngressSmallControlBlockedPhysicalWrite：ingress_control_backpressure_linux_test.go:70: set explicit isolated integration interpreter for real legacy helper
- TestLegacyIngressBurstKeepsFIFOAndCommittedACK：ingress_legacy_compat_test.go:227: set explicit isolated integration interpreter for real legacy helper
- TestLegacyIngressReadPauseResumesWithoutDisconnect：ingress_legacy_compat_test.go:337: set explicit isolated integration interpreter for real legacy helper
- TestLegacyIngressSmallControlReadPause：ingress_legacy_compat_test.go:380: set explicit isolated integration interpreter for real legacy helper
- TestLegacyIngressLimitedWirePongCompetition：ingress_legacy_compat_test.go:492: set explicit isolated integration interpreter for real legacy helper
- TestLegacyIngressDefaultCycleLongBusinessKeepsPong：ingress_legacy_compat_test.go:567: set explicit isolated integration interpreter for real legacy helper
- TestPilotQuestionAcrossRealHTTPHelperIPCMCP：pilot_question_roundtrip_test.go:17: set locked helper Python; canonical runs this local integration
- TestPilotTemplateAcrossRealHTTPHelperIPCMCP：pilot_template_roundtrip_test.go:17: set locked helper Python; canonical runs loopback integration
- TestProjectContextAcrossRealHTTPHelperIPCMCP：project_context_roundtrip_test.go:17: set DW_DESKTOP_INTEGRATION_PYTHON to the locked helper test venv; canonical shell gate runs the cross-runtime set
- TestR60OutboundControlBackpressureRecoversWithSendStatus：r60_outbound_joint_test.go:25: set explicit isolated integration interpreter
- TestSessionHistoryTwentyAgentPublishP99：session_history_test.go:520: set DW_SESSION_HISTORY_PERF=1 for isolated 20-agent latency gate
- TestSpecMCPEndToEnd：spec_mcp_e2e_test.go:211: set DW_SPEC_E2E_PYTHON for actual helper/MCP subprocesses
- TestBlockerAcrossRealHTTPHelperIPCMCP：task_blocker_roundtrip_test.go:21: set locked helper Python; canonical runs this local integration

以上均为需显式集成解释器、浏览器、PG 或性能开关的格；发布集成第五段串行全门在固定源上设定相应环境跑过（docs/reviews/r61-integration-stage5/REPORT.md 与 evidence/）。
