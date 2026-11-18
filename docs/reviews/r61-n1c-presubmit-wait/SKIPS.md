# m8/store count=3 skip ledger

The frozen-source command completed with rc=0. The `-v` log records 48 top-level and 54 nested skip occurrences across three repetitions. The following 34 unique test/subtest names are the complete de-duplicated set; each condition is the opt-in boundary, not a failure.

| Test or subtest | Skip condition |
| --- | --- |
| `TestChangeEvidenceConsoleBrowser` | Requires `CHANGEFLOW_PLAYWRIGHT` pointing to installed Playwright/Chrome. |
| `TestConsolePlatformPayloadDump` | Browser gate must supply `DW_CONSOLE_PLATFORM_PAYLOAD_OUT`. |
| `TestConsoleTaskBoardStartedTaskRestrictionsPendingCaseRecordModel` | K13 deferred: no task case record model exists to derive the started state. |
| `TestConsoleTaskLegacyDODMigrationPendingProductionCount` | K17 waits for a production count of legacy `acceptance_json` rows before migration decision. |
| `TestConsoleTaskStartRequiresAcceptanceLinkPendingStartGateRuling` | Future K7 start-gate timing is not decided; creation remains allowed without links. |
| `TestDeliveryACKActualCLILateWireTerminal` | Requires `DW_ACK_V2_E2E_PYTHON` and actual CLI/Hub acceptance setup. |
| `TestDeliveryACKActualCLIAdmissionCrashWindows/{claude,codex}/{dispatch_sent_receipt_unread,receipt_before_admitted_persist}` | Same actual CLI/Hub opt-in (`DW_ACK_V2_E2E_PYTHON`); no configured CLI subprocess in this isolated Go run. |
| `TestDeliveryACKActualCLICancelAtomicBoundary/{claude,codex}` | Same actual CLI/Hub opt-in. |
| `TestDeliveryACKActualCLILegacyUpgradeKeepsPerDeliveryProvenance/{claude,codex}` | Same actual CLI/Hub opt-in, including immutable legacy helper pins. |
| `TestDeliveryACKActualCLIQueuedAdmissionAndTerminal/{claude,claude_explicit,codex}` | Same actual CLI/Hub opt-in. |
| `TestDeliveryACKActualCLIReplaysPredecessorAcrossDisconnect/{claude,codex}/{drop_injected,drop_injected_confirm}` | Same actual CLI/Hub opt-in. |
| `TestEnvelopeReadTimeoutEnvironmentChild` | Subprocess-only probe; the parent test exercises the timeout contract. |
| `TestFailureNoticeDriverTurnEndToEnd` | Requires `DW_FAILURE_NOTICE_PYTHON` set to the locked helper runtime. |
| `TestFeishuStickyOldHubMigrationRollbackCompatibility` | Requires pinned `R14_OLD_HUB_BINARY`; exact binary compatibility is maintained by the separate R14 evidence gate. |
| `TestGroupReplyMCP` | Requires locked helper runtime via `DW_SPEC_E2E_PYTHON`. |
| `TestHelper0232MixedVersionEndToEnd` | Requires explicit immutable release/factory commits and the actual old Hub binary. |
| `TestHelperSelfUpdateEndToEnd/{install_restart_confirm,sha_mismatch_keeps_factory,new_layout_rejected_keeps_factory_and_projects_reason}` | Requires `DW_HELPER_E2E_PYTHON` (or `DW_SPEC_E2E_PYTHON`) for package/update subprocesses. |
| `TestMuxProductHTTP1EightSeats` | Requires `MUX_PUPPETEER` pointing to installed `puppeteer-core` for real Chrome product integration. |
| `TestR10OldR48HubUpgradeAndRollbackOnIsolatedCopy` | Exact r48 Hub binary is supplied only by the isolated R10 compatibility gate. |
| `TestR10RestartChildAuthority` | Child-process-only probe; covered by `TestR10SeparateProcessRestartPreservesAuthorityAndUnknownNotice`. |
| `TestSessionHistoryTwentyAgentPublishP99` | Opt-in latency gate requires `DW_SESSION_HISTORY_PERF=1`. |
| `TestSpecMCPEndToEnd` | Requires `DW_SPEC_E2E_PYTHON` for actual helper/MCP subprocesses. |

The run did set `DW_DESKTOP_INTEGRATION_PYTHON` to the locked helper interpreter. Consequently the real local desktop/Hub roundtrip, hosted-body, project-context, blocker, claim, and legacy-ingress integration cases were not skipped for a missing interpreter. PostgreSQL cases ran against an isolated server (`server_version_num=180003`) under `HUB_TEST_PG_STRICT=1`.
