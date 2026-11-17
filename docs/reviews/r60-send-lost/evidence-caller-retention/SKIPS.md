# 逐名 SKIP（不计 PASS）

### --- SKIP: TestDeviceClaimAcrossActualClientAndHub (0.00s)

```text
2026/11/17 00:03:19 replay guard: amr_completed_ttl=24h0m0s amr_completed_per_session=4096 amr_completed_total=100000 reply_dedup_ttl=24h0m0s reply_dedup_max=200000 session_touch_min_interval=5s
--- PASS: TestChannelMentionNotDispatchedAsMember (0.14s)
=== RUN   TestDeviceClaimAcrossActualClientAndHub
    claim_client_integration_test.go:33: run scripts/test-desktop-hub-roundtrip.sh for cross-runtime enrollment
--- SKIP: TestDeviceClaimAcrossActualClientAndHub (0.00s)
```

### --- SKIP: TestDeviceClaimAcrossActualClientAndPostgres (0.00s)

```text
    claim_client_integration_test.go:33: run scripts/test-desktop-hub-roundtrip.sh for cross-runtime enrollment
--- SKIP: TestDeviceClaimAcrossActualClientAndHub (0.00s)
=== RUN   TestDeviceClaimAcrossActualClientAndPostgres
    claim_client_integration_test.go:45: run scripts/test-desktop-hub-roundtrip.sh for cross-runtime PostgreSQL enrollment
--- SKIP: TestDeviceClaimAcrossActualClientAndPostgres (0.00s)
```

### --- SKIP: TestChangeEvidenceConsoleBrowser (0.00s)

```text
    --- PASS: TestR52ChangeEvidenceTrustedOriginMatrix/configured_HTTP_impostor (0.00s)
    --- PASS: TestR52ChangeEvidenceTrustedOriginMatrix/configured_missing_origin (0.00s)
=== RUN   TestChangeEvidenceConsoleBrowser
    console_change_evidence_test.go:186: set CHANGEFLOW_PLAYWRIGHT to installed Playwright for Chrome integration
--- SKIP: TestChangeEvidenceConsoleBrowser (0.00s)
```

### --- SKIP: TestMuxProductHTTP1EightSeats (0.00s)

```text
    console_mux_frames_test.go:216: MUX_TIMER_AFTER_SEAL: timer_rejected=true background_mux_timer=0 metadata_unchanged=true
--- PASS: TestMuxRecoveryExpiryAfterHubShutdownRejectsBackgroundWork (0.16s)
=== RUN   TestMuxProductHTTP1EightSeats
    console_mux_product_test.go:34: set MUX_PUPPETEER to installed puppeteer-core for actual Chrome product integration
--- SKIP: TestMuxProductHTTP1EightSeats (0.00s)
```

### --- SKIP: TestConsolePlatformPayloadDump (0.00s)

```text
2026/11/17 00:03:31 replay guard: amr_completed_ttl=24h0m0s amr_completed_per_session=4096 amr_completed_total=100000 reply_dedup_ttl=24h0m0s reply_dedup_max=200000 session_touch_min_interval=5s
--- PASS: TestConsolePlatformAgentGroupsIDIsReserved (0.15s)
=== RUN   TestConsolePlatformPayloadDump
    console_platform_agentgroups_test.go:133: 仅在浏览器闸取真载荷时运行(设 DW_CONSOLE_PLATFORM_PAYLOAD_OUT)
--- SKIP: TestConsolePlatformPayloadDump (0.00s)
```

### --- SKIP: TestConsoleTaskBoardStartedTaskRestrictionsPendingCaseRecordModel (0.00s)

```text
    console_task_board_page_test.go:118: final listed/opened; draft hidden; direct draft: 409 该 spec 尚未审结(当前状态:editing),审结后才能进入任务管理
--- PASS: TestConsoleTaskBoardListsAndOpensOnlyFinalSpecs (0.15s)
=== RUN   TestConsoleTaskBoardStartedTaskRestrictionsPendingCaseRecordModel
    console_task_board_page_test.go:122: K13 deferred: task case record model does not exist, so started state cannot be derived mechanically
--- SKIP: TestConsoleTaskBoardStartedTaskRestrictionsPendingCaseRecordModel (0.00s)
```

### --- SKIP: TestDeliveryACKActualCLIQueuedAdmissionAndTerminal/claude (0.15s)

```text
2026/11/17 00:04:02 amr startup mode=route_all structured_failure_min_version=0.1.3.0
2026/11/17 00:04:02 replay guard: amr_completed_ttl=24h0m0s amr_completed_per_session=4096 amr_completed_total=100000 reply_dedup_ttl=24h0m0s reply_dedup_max=200000 session_touch_min_interval=5s
    delivery_ack_real_cli_e2e_linux_test.go:373: INCONCLUSIVE: set DW_ACK_V2_E2E_PYTHON for actual CLI/Hub acceptance
--- PASS: TestDeliveryACKActualCLIQueuedAdmissionAndTerminal (0.44s)
    --- SKIP: TestDeliveryACKActualCLIQueuedAdmissionAndTerminal/claude (0.15s)
```

### --- SKIP: TestDeliveryACKActualCLIQueuedAdmissionAndTerminal/codex (0.15s)

```text
2026/11/17 00:04:02 replay guard: amr_completed_ttl=24h0m0s amr_completed_per_session=4096 amr_completed_total=100000 reply_dedup_ttl=24h0m0s reply_dedup_max=200000 session_touch_min_interval=5s
    delivery_ack_real_cli_e2e_linux_test.go:373: INCONCLUSIVE: set DW_ACK_V2_E2E_PYTHON for actual CLI/Hub acceptance
--- PASS: TestDeliveryACKActualCLIQueuedAdmissionAndTerminal (0.44s)
    --- SKIP: TestDeliveryACKActualCLIQueuedAdmissionAndTerminal/claude (0.15s)
    --- SKIP: TestDeliveryACKActualCLIQueuedAdmissionAndTerminal/codex (0.15s)
```

### --- SKIP: TestDeliveryACKActualCLIQueuedAdmissionAndTerminal/claude_explicit (0.14s)

```text
    delivery_ack_real_cli_e2e_linux_test.go:373: INCONCLUSIVE: set DW_ACK_V2_E2E_PYTHON for actual CLI/Hub acceptance
--- PASS: TestDeliveryACKActualCLIQueuedAdmissionAndTerminal (0.44s)
    --- SKIP: TestDeliveryACKActualCLIQueuedAdmissionAndTerminal/claude (0.15s)
    --- SKIP: TestDeliveryACKActualCLIQueuedAdmissionAndTerminal/codex (0.15s)
    --- SKIP: TestDeliveryACKActualCLIQueuedAdmissionAndTerminal/claude_explicit (0.14s)
```

### --- SKIP: TestDeliveryACKActualCLIReplaysPredecessorAcrossDisconnect/claude/drop_injected (0.14s)

```text
2026/11/17 00:04:02 amr startup mode=route_all structured_failure_min_version=0.1.3.0
2026/11/17 00:04:02 replay guard: amr_completed_ttl=24h0m0s amr_completed_per_session=4096 amr_completed_total=100000 reply_dedup_ttl=24h0m0s reply_dedup_max=200000 session_touch_min_interval=5s
    delivery_ack_real_cli_e2e_linux_test.go:475: INCONCLUSIVE: set DW_ACK_V2_E2E_PYTHON for actual CLI/Hub acceptance
--- PASS: TestDeliveryACKActualCLIReplaysPredecessorAcrossDisconnect (0.56s)
    --- SKIP: TestDeliveryACKActualCLIReplaysPredecessorAcrossDisconnect/claude/drop_injected (0.14s)
```

### --- SKIP: TestDeliveryACKActualCLIReplaysPredecessorAcrossDisconnect/claude/drop_injected_confirm (0.14s)

```text
2026/11/17 00:04:02 replay guard: amr_completed_ttl=24h0m0s amr_completed_per_session=4096 amr_completed_total=100000 reply_dedup_ttl=24h0m0s reply_dedup_max=200000 session_touch_min_interval=5s
    delivery_ack_real_cli_e2e_linux_test.go:475: INCONCLUSIVE: set DW_ACK_V2_E2E_PYTHON for actual CLI/Hub acceptance
--- PASS: TestDeliveryACKActualCLIReplaysPredecessorAcrossDisconnect (0.56s)
    --- SKIP: TestDeliveryACKActualCLIReplaysPredecessorAcrossDisconnect/claude/drop_injected (0.14s)
    --- SKIP: TestDeliveryACKActualCLIReplaysPredecessorAcrossDisconnect/claude/drop_injected_confirm (0.14s)
```

### --- SKIP: TestDeliveryACKActualCLIReplaysPredecessorAcrossDisconnect/codex/drop_injected (0.14s)

```text
    delivery_ack_real_cli_e2e_linux_test.go:475: INCONCLUSIVE: set DW_ACK_V2_E2E_PYTHON for actual CLI/Hub acceptance
--- PASS: TestDeliveryACKActualCLIReplaysPredecessorAcrossDisconnect (0.56s)
    --- SKIP: TestDeliveryACKActualCLIReplaysPredecessorAcrossDisconnect/claude/drop_injected (0.14s)
    --- SKIP: TestDeliveryACKActualCLIReplaysPredecessorAcrossDisconnect/claude/drop_injected_confirm (0.14s)
    --- SKIP: TestDeliveryACKActualCLIReplaysPredecessorAcrossDisconnect/codex/drop_injected (0.14s)
```

### --- SKIP: TestDeliveryACKActualCLIReplaysPredecessorAcrossDisconnect/codex/drop_injected_confirm (0.14s)

```text
--- PASS: TestDeliveryACKActualCLIReplaysPredecessorAcrossDisconnect (0.56s)
    --- SKIP: TestDeliveryACKActualCLIReplaysPredecessorAcrossDisconnect/claude/drop_injected (0.14s)
    --- SKIP: TestDeliveryACKActualCLIReplaysPredecessorAcrossDisconnect/claude/drop_injected_confirm (0.14s)
    --- SKIP: TestDeliveryACKActualCLIReplaysPredecessorAcrossDisconnect/codex/drop_injected (0.14s)
    --- SKIP: TestDeliveryACKActualCLIReplaysPredecessorAcrossDisconnect/codex/drop_injected_confirm (0.14s)
```

### --- SKIP: TestDeliveryACKActualCLIAdmissionCrashWindows/claude/dispatch_sent_receipt_unread (0.14s)

```text
2026/11/17 00:04:03 amr startup mode=route_all structured_failure_min_version=0.1.3.0
2026/11/17 00:04:03 replay guard: amr_completed_ttl=24h0m0s amr_completed_per_session=4096 amr_completed_total=100000 reply_dedup_ttl=24h0m0s reply_dedup_max=200000 session_touch_min_interval=5s
    delivery_ack_real_cli_e2e_linux_test.go:527: INCONCLUSIVE: set DW_ACK_V2_E2E_PYTHON for actual CLI/Hub acceptance
--- PASS: TestDeliveryACKActualCLIAdmissionCrashWindows (0.56s)
    --- SKIP: TestDeliveryACKActualCLIAdmissionCrashWindows/claude/dispatch_sent_receipt_unread (0.14s)
```

### --- SKIP: TestDeliveryACKActualCLIAdmissionCrashWindows/claude/receipt_before_admitted_persist (0.14s)

```text
2026/11/17 00:04:03 replay guard: amr_completed_ttl=24h0m0s amr_completed_per_session=4096 amr_completed_total=100000 reply_dedup_ttl=24h0m0s reply_dedup_max=200000 session_touch_min_interval=5s
    delivery_ack_real_cli_e2e_linux_test.go:527: INCONCLUSIVE: set DW_ACK_V2_E2E_PYTHON for actual CLI/Hub acceptance
--- PASS: TestDeliveryACKActualCLIAdmissionCrashWindows (0.56s)
    --- SKIP: TestDeliveryACKActualCLIAdmissionCrashWindows/claude/dispatch_sent_receipt_unread (0.14s)
    --- SKIP: TestDeliveryACKActualCLIAdmissionCrashWindows/claude/receipt_before_admitted_persist (0.14s)
```

### --- SKIP: TestDeliveryACKActualCLIAdmissionCrashWindows/codex/dispatch_sent_receipt_unread (0.14s)

```text
    delivery_ack_real_cli_e2e_linux_test.go:527: INCONCLUSIVE: set DW_ACK_V2_E2E_PYTHON for actual CLI/Hub acceptance
--- PASS: TestDeliveryACKActualCLIAdmissionCrashWindows (0.56s)
    --- SKIP: TestDeliveryACKActualCLIAdmissionCrashWindows/claude/dispatch_sent_receipt_unread (0.14s)
    --- SKIP: TestDeliveryACKActualCLIAdmissionCrashWindows/claude/receipt_before_admitted_persist (0.14s)
    --- SKIP: TestDeliveryACKActualCLIAdmissionCrashWindows/codex/dispatch_sent_receipt_unread (0.14s)
```

### --- SKIP: TestDeliveryACKActualCLIAdmissionCrashWindows/codex/receipt_before_admitted_persist (0.14s)

```text
--- PASS: TestDeliveryACKActualCLIAdmissionCrashWindows (0.56s)
    --- SKIP: TestDeliveryACKActualCLIAdmissionCrashWindows/claude/dispatch_sent_receipt_unread (0.14s)
    --- SKIP: TestDeliveryACKActualCLIAdmissionCrashWindows/claude/receipt_before_admitted_persist (0.14s)
    --- SKIP: TestDeliveryACKActualCLIAdmissionCrashWindows/codex/dispatch_sent_receipt_unread (0.14s)
    --- SKIP: TestDeliveryACKActualCLIAdmissionCrashWindows/codex/receipt_before_admitted_persist (0.14s)
```

### --- SKIP: TestDeliveryACKActualCLICancelAtomicBoundary/claude (0.16s)

```text
2026/11/17 00:04:03 amr startup mode=route_all structured_failure_min_version=0.1.3.0
2026/11/17 00:04:03 replay guard: amr_completed_ttl=24h0m0s amr_completed_per_session=4096 amr_completed_total=100000 reply_dedup_ttl=24h0m0s reply_dedup_max=200000 session_touch_min_interval=5s
    delivery_ack_real_cli_e2e_linux_test.go:592: INCONCLUSIVE: set DW_ACK_V2_E2E_PYTHON for actual CLI/Hub acceptance
--- PASS: TestDeliveryACKActualCLICancelAtomicBoundary (0.30s)
    --- SKIP: TestDeliveryACKActualCLICancelAtomicBoundary/claude (0.16s)
```

### --- SKIP: TestDeliveryACKActualCLICancelAtomicBoundary/codex (0.14s)

```text
2026/11/17 00:04:03 replay guard: amr_completed_ttl=24h0m0s amr_completed_per_session=4096 amr_completed_total=100000 reply_dedup_ttl=24h0m0s reply_dedup_max=200000 session_touch_min_interval=5s
    delivery_ack_real_cli_e2e_linux_test.go:592: INCONCLUSIVE: set DW_ACK_V2_E2E_PYTHON for actual CLI/Hub acceptance
--- PASS: TestDeliveryACKActualCLICancelAtomicBoundary (0.30s)
    --- SKIP: TestDeliveryACKActualCLICancelAtomicBoundary/claude (0.16s)
    --- SKIP: TestDeliveryACKActualCLICancelAtomicBoundary/codex (0.14s)
```

### --- SKIP: TestDeliveryACKActualCLILegacyUpgradeKeepsPerDeliveryProvenance/claude (0.24s)

```text
2026/11/17 00:04:04 amr startup mode=route_all structured_failure_min_version=0.1.3.0
2026/11/17 00:04:04 replay guard: amr_completed_ttl=24h0m0s amr_completed_per_session=4096 amr_completed_total=100000 reply_dedup_ttl=24h0m0s reply_dedup_max=200000 session_touch_min_interval=5s
    delivery_ack_real_cli_e2e_linux_test.go:675: INCONCLUSIVE: set DW_ACK_V2_E2E_PYTHON for actual CLI/Hub acceptance
--- PASS: TestDeliveryACKActualCLILegacyUpgradeKeepsPerDeliveryProvenance (0.48s)
    --- SKIP: TestDeliveryACKActualCLILegacyUpgradeKeepsPerDeliveryProvenance/claude (0.24s)
```

### --- SKIP: TestDeliveryACKActualCLILegacyUpgradeKeepsPerDeliveryProvenance/codex (0.24s)

```text
2026/11/17 00:04:04 replay guard: amr_completed_ttl=24h0m0s amr_completed_per_session=4096 amr_completed_total=100000 reply_dedup_ttl=24h0m0s reply_dedup_max=200000 session_touch_min_interval=5s
    delivery_ack_real_cli_e2e_linux_test.go:675: INCONCLUSIVE: set DW_ACK_V2_E2E_PYTHON for actual CLI/Hub acceptance
--- PASS: TestDeliveryACKActualCLILegacyUpgradeKeepsPerDeliveryProvenance (0.48s)
    --- SKIP: TestDeliveryACKActualCLILegacyUpgradeKeepsPerDeliveryProvenance/claude (0.24s)
    --- SKIP: TestDeliveryACKActualCLILegacyUpgradeKeepsPerDeliveryProvenance/codex (0.24s)
```

### --- SKIP: TestDeliveryACKActualCLILateWireTerminal (0.15s)

```text
=== RUN   TestDeliveryACKActualCLILateWireTerminal
2026/11/17 00:04:04 amr startup mode=route_all structured_failure_min_version=0.1.3.0
2026/11/17 00:04:04 replay guard: amr_completed_ttl=24h0m0s amr_completed_per_session=4096 amr_completed_total=100000 reply_dedup_ttl=24h0m0s reply_dedup_max=200000 session_touch_min_interval=5s
    delivery_ack_real_cli_e2e_linux_test.go:733: INCONCLUSIVE: set DW_ACK_V2_E2E_PYTHON for actual CLI/Hub acceptance
--- SKIP: TestDeliveryACKActualCLILateWireTerminal (0.15s)
```

### --- SKIP: TestDesktopMCPRoundTripAcrossRealHub (0.00s)

```text
2026/11/17 00:04:06 session delivery lease release decision=requeue key_id=FB-person-20261117-b2cef79d session=home conn_seq=1 reason=session_disconnected released=0
--- PASS: TestDesktopDurableReceiptNeverPublishesCLIInjection (0.26s)
=== RUN   TestDesktopMCPRoundTripAcrossRealHub
    desktop_mcp_roundtrip_test.go:25: run scripts/test-desktop-hub-roundtrip.sh for the cross-runtime gate
--- SKIP: TestDesktopMCPRoundTripAcrossRealHub (0.00s)
```

### --- SKIP: TestFailureNoticeDriverTurnEndToEnd (0.00s)

```text
2026/11/17 00:04:15 replay guard: amr_completed_ttl=24h0m0s amr_completed_per_session=4096 amr_completed_total=100000 reply_dedup_ttl=24h0m0s reply_dedup_max=200000 session_touch_min_interval=5s
--- PASS: TestFailureDockEventMatchesRendererFixture (0.14s)
=== RUN   TestFailureNoticeDriverTurnEndToEnd
    failure_notice_driver_e2e_test.go:24: set DW_FAILURE_NOTICE_PYTHON to locked helper runtime
--- SKIP: TestFailureNoticeDriverTurnEndToEnd (0.00s)
```

### --- SKIP: TestGroupReplyMCP (0.00s)

```text
    --- PASS: TestGroupReplyMembership/deleted (0.59s)
    --- PASS: TestGroupReplyMembership/leader-only (0.60s)
=== RUN   TestGroupReplyMCP
    group_reply_test.go:104: requires locked helper Python; set DW_SPEC_E2E_PYTHON
--- SKIP: TestGroupReplyMCP (0.00s)
```

### --- SKIP: TestHelperSelfUpdateEndToEnd/install_restart_confirm (0.00s)

```text
    helper_update_e2e_test.go:75: set DW_HELPER_E2E_PYTHON to locked helper runtime Python
=== RUN   TestHelperSelfUpdateEndToEnd/new_layout_rejected_keeps_factory_and_projects_reason
    helper_update_e2e_test.go:75: set DW_HELPER_E2E_PYTHON to locked helper runtime Python
--- PASS: TestHelperSelfUpdateEndToEnd (0.00s)
    --- SKIP: TestHelperSelfUpdateEndToEnd/install_restart_confirm (0.00s)
```

### --- SKIP: TestHelperSelfUpdateEndToEnd/sha_mismatch_keeps_factory (0.00s)

```text
=== RUN   TestHelperSelfUpdateEndToEnd/new_layout_rejected_keeps_factory_and_projects_reason
    helper_update_e2e_test.go:75: set DW_HELPER_E2E_PYTHON to locked helper runtime Python
--- PASS: TestHelperSelfUpdateEndToEnd (0.00s)
    --- SKIP: TestHelperSelfUpdateEndToEnd/install_restart_confirm (0.00s)
    --- SKIP: TestHelperSelfUpdateEndToEnd/sha_mismatch_keeps_factory (0.00s)
```

### --- SKIP: TestHelperSelfUpdateEndToEnd/new_layout_rejected_keeps_factory_and_projects_reason (0.00s)

```text
    helper_update_e2e_test.go:75: set DW_HELPER_E2E_PYTHON to locked helper runtime Python
--- PASS: TestHelperSelfUpdateEndToEnd (0.00s)
    --- SKIP: TestHelperSelfUpdateEndToEnd/install_restart_confirm (0.00s)
    --- SKIP: TestHelperSelfUpdateEndToEnd/sha_mismatch_keeps_factory (0.00s)
    --- SKIP: TestHelperSelfUpdateEndToEnd/new_layout_rejected_keeps_factory_and_projects_reason (0.00s)
```

### --- SKIP: TestHelper0232MixedVersionEndToEnd (0.00s)

```text
    --- SKIP: TestHelperSelfUpdateEndToEnd/sha_mismatch_keeps_factory (0.00s)
    --- SKIP: TestHelperSelfUpdateEndToEnd/new_layout_rejected_keeps_factory_and_projects_reason (0.00s)
=== RUN   TestHelper0232MixedVersionEndToEnd
    helper_update_e2e_test.go:55: explicit immutable release pins required
--- SKIP: TestHelper0232MixedVersionEndToEnd (0.00s)
```

### --- SKIP: TestHostedBodySendAcrossRealHub (0.00s)

```text
2026/11/17 00:04:48 replay guard: amr_completed_ttl=24h0m0s amr_completed_per_session=4096 amr_completed_total=100000 reply_dedup_ttl=24h0m0s reply_dedup_max=200000 session_touch_min_interval=5s
--- PASS: TestListHelperUpdateCatalogsAndOwnerJobsFilter (0.15s)
=== RUN   TestHostedBodySendAcrossRealHub
    hosted_body_send_test.go:23: set explicit isolated integration interpreter
--- SKIP: TestHostedBodySendAcrossRealHub (0.00s)
```

### --- SKIP: TestEnvelopeReadTimeoutEnvironmentChild (0.00s)

```text
=== RUN   TestEnvelopeReadTimeoutDerivesFromEnvironment
--- PASS: TestEnvelopeReadTimeoutDerivesFromEnvironment (0.01s)
=== RUN   TestEnvelopeReadTimeoutEnvironmentChild
    hub_timeout_contract_test.go:50: subprocess probe only
--- SKIP: TestEnvelopeReadTimeoutEnvironmentChild (0.00s)
```

### --- SKIP: TestPilotQuestionAcrossRealHTTPHelperIPCMCP (0.00s)

```text
2026/11/17 00:05:51 replay guard: amr_completed_ttl=24h0m0s amr_completed_per_session=4096 amr_completed_total=100000 reply_dedup_ttl=24h0m0s reply_dedup_max=200000 session_touch_min_interval=5s
--- PASS: TestPilotQuestionNoRecipientRemainsPendingConfig (0.15s)
=== RUN   TestPilotQuestionAcrossRealHTTPHelperIPCMCP
    pilot_question_roundtrip_test.go:17: set locked helper Python; canonical runs this local integration
--- SKIP: TestPilotQuestionAcrossRealHTTPHelperIPCMCP (0.00s)
```

### --- SKIP: TestPilotTemplateAcrossRealHTTPHelperIPCMCP (0.00s)

```text
=== RUN   TestPilotQuestionReminderExplicitlyDirectsReplyToOriginalNotice
--- PASS: TestPilotQuestionReminderExplicitlyDirectsReplyToOriginalNotice (0.00s)
=== RUN   TestPilotTemplateAcrossRealHTTPHelperIPCMCP
    pilot_template_roundtrip_test.go:17: set locked helper Python; canonical runs loopback integration
--- SKIP: TestPilotTemplateAcrossRealHTTPHelperIPCMCP (0.00s)
```

### --- SKIP: TestProjectContextAcrossRealHTTPHelperIPCMCP (0.00s)

```text
2026/11/17 00:05:53 replay guard: amr_completed_ttl=24h0m0s amr_completed_per_session=4096 amr_completed_total=100000 reply_dedup_ttl=24h0m0s reply_dedup_max=200000 session_touch_min_interval=5s
--- PASS: TestProjectContextPGSQLiteReadThenRevokeAndRevokeThenRead (2.86s)
=== RUN   TestProjectContextAcrossRealHTTPHelperIPCMCP
    project_context_roundtrip_test.go:17: set DW_DESKTOP_INTEGRATION_PYTHON to the locked helper test venv; canonical shell gate runs the cross-runtime set
--- SKIP: TestProjectContextAcrossRealHTTPHelperIPCMCP (0.00s)
```

### --- SKIP: TestSessionHistoryTwentyAgentPublishP99 (0.00s)

```text
2026/11/17 00:06:17 replay guard: amr_completed_ttl=24h0m0s amr_completed_per_session=4096 amr_completed_total=100000 reply_dedup_ttl=24h0m0s reply_dedup_max=200000 session_touch_min_interval=5s
--- PASS: TestSessionHistoryConcurrentGroupCursorMatchesLiveEvents (0.02s)
=== RUN   TestSessionHistoryTwentyAgentPublishP99
    session_history_test.go:520: set DW_SESSION_HISTORY_PERF=1 for isolated 20-agent latency gate
--- SKIP: TestSessionHistoryTwentyAgentPublishP99 (0.00s)
```

### --- SKIP: TestSpecMCPEndToEnd (0.00s)

```text
2026/11/17 00:06:24 session handshake timing key_id=u_zzc session=kimi total=1ms register=0s accept=0s conn_seq=0s onboard=0s register_lock=0s
--- PASS: TestSpecGrantLegacyRequestAndPollFailFastWithoutWrites (0.16s)
=== RUN   TestSpecMCPEndToEnd
    spec_mcp_e2e_test.go:211: set DW_SPEC_E2E_PYTHON for actual helper/MCP subprocesses
--- SKIP: TestSpecMCPEndToEnd (0.00s)
```

### --- SKIP: TestBlockerAcrossRealHTTPHelperIPCMCP (0.00s)

```text
2026/11/17 00:06:35 replay guard: amr_completed_ttl=24h0m0s amr_completed_per_session=4096 amr_completed_total=100000 reply_dedup_ttl=24h0m0s reply_dedup_max=200000 session_touch_min_interval=5s
--- PASS: TestBlockerPGHumanReplyCommitBeforeDisableAndDisableBeforeReply (2.97s)
=== RUN   TestBlockerAcrossRealHTTPHelperIPCMCP
    task_blocker_roundtrip_test.go:21: set locked helper Python; canonical runs this local integration
--- SKIP: TestBlockerAcrossRealHTTPHelperIPCMCP (0.00s)
```

### --- SKIP: TestConsoleTaskLegacyDODMigrationPendingProductionCount (0.00s)

```text
=== RUN   TestConsoleTaskAcceptanceLinksCascadeOnTaskDelete
--- PASS: TestConsoleTaskAcceptanceLinksCascadeOnTaskDelete (0.15s)
=== RUN   TestConsoleTaskLegacyDODMigrationPendingProductionCount
    console_task_lineage_test.go:135: K17 covers new writes only; legacy acceptance_json rows containing "DOD" await the production count before any 0049 migration decision
--- SKIP: TestConsoleTaskLegacyDODMigrationPendingProductionCount (0.00s)
```

### --- SKIP: TestConsoleTaskStartRequiresAcceptanceLinkPendingStartGateRuling (0.00s)

```text
    console_task_lineage_test.go:135: K17 covers new writes only; legacy acceptance_json rows containing "DOD" await the production count before any 0049 migration decision
--- SKIP: TestConsoleTaskLegacyDODMigrationPendingProductionCount (0.00s)
=== RUN   TestConsoleTaskStartRequiresAcceptanceLinkPendingStartGateRuling
    console_task_lineage_test.go:139: future K7: enforce at the start gate using console_spec_task_acceptance, never acceptance_json; creation remains allowed with zero links until that timing is sealed
--- SKIP: TestConsoleTaskStartRequiresAcceptanceLinkPendingStartGateRuling (0.00s)
```

### --- SKIP: TestFeishuStickyOldHubMigrationRollbackCompatibility (0.00s)

```text
=== RUN   TestSingleTenantAmbiguousParentFailsClosed
--- PASS: TestSingleTenantAmbiguousParentFailsClosed (0.17s)
=== RUN   TestFeishuStickyOldHubMigrationRollbackCompatibility
    feishu_sticky_compat_test.go:116: R14_OLD_HUB_BINARY not supplied; exact-r48 compatibility fixture is run by the R14 evidence gate
--- SKIP: TestFeishuStickyOldHubMigrationRollbackCompatibility (0.00s)
```

### --- SKIP: TestR10OldR48HubUpgradeAndRollbackOnIsolatedCopy (0.00s)

```text
=== RUN   TestBlockerAddressedWaiterGetsOnlyNeededContext
--- PASS: TestBlockerAddressedWaiterGetsOnlyNeededContext (0.14s)
=== RUN   TestR10OldR48HubUpgradeAndRollbackOnIsolatedCopy
    task_dispatch_compat_test.go:196: exact-r48 binary not provided; supplied by the isolated R10 compatibility evidence gate
--- SKIP: TestR10OldR48HubUpgradeAndRollbackOnIsolatedCopy (0.00s)
```

### --- SKIP: TestR10RestartChildAuthority (0.00s)

```text
=== RUN   TestR10PGFenceSQLiteFailureDoesNotGrant
--- PASS: TestR10PGFenceSQLiteFailureDoesNotGrant (2.69s)
=== RUN   TestR10RestartChildAuthority
    task_dispatch_restart_test.go:22: subprocess-only probe, exercised by TestR10SeparateProcessRestartPreservesAuthorityAndUnknownNotice
--- SKIP: TestR10RestartChildAuthority (0.00s)
```

### --- SKIP: TestMonitorPageInRealChrome (0.00s)

```text
=== RUN   TestWSKindsInstrumentedZeroVsNodeNotInstrumented
--- PASS: TestWSKindsInstrumentedZeroVsNodeNotInstrumented (0.00s)
=== RUN   TestMonitorPageInRealChrome
    page_browser_test.go:26: set OBS_PUPPETEER to an installed puppeteer-core directory for the real Chrome page check
--- SKIP: TestMonitorPageInRealChrome (0.00s)
```

### --- SKIP: TestNPSessionPG8To9AndR50BinaryCompatibility (0.00s)

```text
2026/11/17 00:12:11 session delivery lease release decision=requeue key_id=u_zzc session=seat1 conn_seq=3 reason=session_reconnected released=0
--- PASS: TestUpgradeRotationKeepsNewCredentialOnSharedWork (1.50s)
=== RUN   TestNPSessionPG8To9AndR50BinaryCompatibility
    np_session_migration_binary_test.go:26: set independently built current and certified R50 binary paths
--- SKIP: TestNPSessionPG8To9AndR50BinaryCompatibility (0.00s)
```

### --- SKIP: TestR31OldBinarySchemaReadCompatibility (0.00s)

```text
=== RUN   TestR31AdminNodeAPIAndOneTimeCredential
--- PASS: TestR31AdminNodeAPIAndOneTimeCredential (2.65s)
=== RUN   TestR31OldBinarySchemaReadCompatibility
    r31_rollback_compat_test.go:23: requires independently built R30 binary via R31_R30_BINARY
--- SKIP: TestR31OldBinarySchemaReadCompatibility (0.00s)
```

### --- SKIP: TestHLCFaultProcessMatrix (0.00s)

```text
=== RUN   TestSessionHistoryOffThenOnStartupPersistsDisabledGap
--- PASS: TestSessionHistoryOffThenOnStartupPersistsDisabledGap (0.03s)
=== RUN   TestHLCFaultProcessMatrix
    shutdown_fault_process_test.go:94: isolated fault/watchdog runner tests/hlc/fault_matrix.py
--- SKIP: TestHLCFaultProcessMatrix (0.00s)
```

### --- SKIP: TestHLCBindFailure (0.00s)

```text
    shutdown_fault_process_test.go:94: isolated fault/watchdog runner tests/hlc/fault_matrix.py
--- SKIP: TestHLCFaultProcessMatrix (0.00s)
=== RUN   TestHLCBindFailure
    shutdown_fault_process_test.go:329: isolated startup failure runner
--- SKIP: TestHLCBindFailure (0.00s)
```

### --- SKIP: TestHLCLoadMatrix (0.00s)

```text
=== RUN   TestHLCLoadProcessChild
--- PASS: TestHLCLoadProcessChild (0.00s)
=== RUN   TestHLCLoadMatrix
    shutdown_load_test.go:563: isolated actual-process matrix: tests/hlc/process_matrix.py
--- SKIP: TestHLCLoadMatrix (0.00s)
```

### --- SKIP: TestHLCS007Full (0.00s)

```text
    shutdown_load_test.go:563: isolated actual-process matrix: tests/hlc/process_matrix.py
--- SKIP: TestHLCLoadMatrix (0.00s)
=== RUN   TestHLCS007Full
    shutdown_mixed_fault_process_test.go:164: isolated full mixed fault: tests/hlc/s007_full.py
--- SKIP: TestHLCS007Full (0.00s)
```
