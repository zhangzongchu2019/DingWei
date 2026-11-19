# R62 external Agent Feishu summary copy

## Scope and implementation

This branch implements the approved copy slice on top of `main` `206d41b26dab646c0e50a111eed5a45ccf7c72f6`. It does not implement projected-message recall or N-6 failure notices.

- `0114_agent_feishu_summary_copy.sql` adds a nullable per-Agent preference, a group label for the existing external copy queue, and a durable internal-Agent copy outbox. `NULL` means external Agents default on and internal Agents default off; an explicit `false` is persistent and takes precedence over the environment's default-on setting. The migration is after the already assigned `0112` and `0113` slots.
- Both directions now send a deterministic summary with sender, recipient, group, UTC time, and a sanitized excerpt capped at 512 bytes. The full body is not copied. Credential-like values, Feishu open identifiers, and explicitly labelled system/developer/hidden prompt lines are removed. The final message is capped at 4096 bytes. No authenticated per-message view URL exists in the current API, so the summary omits a link rather than generating an unauthenticated one.
- The helper queue retries independently of message TTL for at most 24 hours and 20 attempts. Each projection pass sweeps expired rows to `dead`; exhausting either limit also ends retries. Copy delivery runs after the primary internal route or in a savepoint around existing external-seat transactions, so a copy failure does not change primary delivery outcome.
- Only an authenticated Feishu personal message may change a preference. Commands are anchored as `Agent抄送 开|关 [Agent全名]`; when the verified user has one active Agent, the name may be omitted. With multiple Agents, an omitted or ambiguous name only returns a selection prompt. Ordinary body text is not parsed as a preference command.
- Internal-Agent recipients are resolved from `np_agent.owner_lc` through an active `np_user` to exactly one Feishu `np_identity`. Missing or multiple identities fail closed, and the same exact identity is rechecked immediately before projection so a stale queued copy is dead-lettered after identity changes. The queue stores issuer/subject server-side; neither is included in the Feishu summary. Replies to copied Feishu messages are suppressed by the existing parent-message reply check and are not routed back to MCP.
- The existing `DW_MCP_FEISHU_COPY_ENABLED=0` remains a global kill switch; per-Agent persisted `false` is checked before the origin default and cannot be re-enabled by the default-on setting. Disabling a preference dead-letters queued/sending rows; a send already claimed by a worker may finish its in-flight Feishu API call.
- The current Feishu API integration has no authenticated per-message View link contract. The summary therefore contains no link; the renderer does not manufacture a public or unauthenticated URL.
- The migration is numbered `0114` after the assigned name/group slots `0112` and `0113`. At implementation start and delivery, remote `main` still points to `206d41b2`; this branch contains neither competing migration.

## Tests and gates

All commands ran against the frozen implementation tree on 2026-11-19 UTC. PostgreSQL gates used the isolated embedded PostgreSQL test server and `HUB_TEST_PG_STRICT=1`; no production connection was used.

| Gate | Result |
|---|---|
| Canonical `scripts/run-build-tests.sh` | PASS, `BUILD_TESTS_OK helper_profiles=2 python=27 shell=2`; helper inventory 97. Ran with `env -i`, locked helper venv first on PATH, Go 1.22.2, separate HOME/TMP. |
| `HUB_TEST_PG_STRICT=1 go test ./internal/extseat -count=3 -p 1` | PASS, 183.657s. Verbose audit: no SKIP. |
| `HUB_TEST_PG_STRICT=1 go test ./internal/identity -count=3 -p 1` | PASS, 264.488s. Verbose audit: no SKIP. |
| `DW_DESKTOP_INTEGRATION_PYTHON=/tmp/lockvenv/bin/python3 HUB_TEST_PG_STRICT=1 go test -v ./internal/m8 -count=3 -p 1 -timeout=30m` | PASS on third full run, 1038.711s; 0 test failures and 0 `interrupted (9)` in that run. Earlier full attempts are recorded below. |
| Four required mutation cases | 4/4 red on business assertions; no compile errors or panics. Logs are under `evidence/mutation-*.log`. |
| Post-gofmt targeted rerun | PASS: extseat copy tests, m8 command/external-seat tests, and `internal/store/migrate -count=1 -p 1`; exact command and output were checked before commit. |

The canonical gate's first run failed only because the existing device-claim integration fixture still expected the migration ledger to stop before the newly added schema. The fixture was updated to include migration 0114; the final canonical run passed. This was a test-fixture correction, not a production failure.

The first complete m8 run failed `TestViewMentionRoute` with the already tracked SQLite `interrupted (9)` lease-release race. The second complete run failed `TestAMRLogHygieneMissingIdentityCountedNotLogged`: a background AMR periodic summary entered the test's shared logger capture. That run also emitted four lease-release `interrupted (9)` diagnostics, but those were not test failures. The third complete run passed with no interrupt diagnostics. All three original logs are retained; no failing run was discarded or combined with another run.

An additional partial exploratory m8 log, `evidence/m8-count3-incomplete-env.log`, is retained but excluded from the gate count: it was stopped before completion after the first invocation omitted the locked desktop integration Python environment.

### m8 SKIP audit (final passing run)

The final `-count=3` log has these unique skipped test/subtest names; repeated occurrences reflect the three count iterations:

- `TestChangeEvidenceConsoleBrowser` — `CHANGEFLOW_PLAYWRIGHT` not configured.
- `TestConsolePlatformPayloadDump` — `DW_CONSOLE_PLATFORM_PAYLOAD_OUT` not configured; this is a browser-gate fixture producer.
- `TestConsoleTaskBoardStartedTaskRestrictionsPendingCaseRecordModel` — intentional K13 deferral; no mechanical task case-record model exists.
- `TestDeliveryACKActualCLIAdmissionCrashWindows/{claude,codex}/{dispatch_sent_receipt_unread,receipt_before_admitted_persist}` — actual CLI acceptance runtime is opt-in via `DW_ACK_V2_E2E_PYTHON`.
- `TestDeliveryACKActualCLICancelAtomicBoundary/{claude,codex}` — same actual CLI runtime gate.
- `TestDeliveryACKActualCLILateWireTerminal` — same actual CLI runtime gate.
- `TestDeliveryACKActualCLILegacyUpgradeKeepsPerDeliveryProvenance/{claude,codex}` — same actual CLI runtime gate.
- `TestDeliveryACKActualCLIQueuedAdmissionAndTerminal/{claude,codex,claude_explicit}` — same actual CLI runtime gate.
- `TestDeliveryACKActualCLIReplaysPredecessorAcrossDisconnect/{claude,codex}/{drop_injected,drop_injected_confirm}` — same actual CLI runtime gate.
- `TestEnvelopeReadTimeoutEnvironmentChild` — subprocess-only helper sentinel; the parent test performs the check.
- `TestFailureNoticeDriverTurnEndToEnd` — `DW_FAILURE_NOTICE_PYTHON` not configured.
- `TestGroupReplyMCP` and `TestSpecMCPEndToEnd` — actual helper/MCP subprocess tests require `DW_SPEC_E2E_PYTHON`.
- `TestHelper0232MixedVersionEndToEnd` — requires explicit immutable helper release/factory commits and a pinned Hub binary.
- `TestHelperSelfUpdateEndToEnd/{install_restart_confirm,new_layout_rejected_keeps_factory_and_projects_reason,sha_mismatch_keeps_factory}` — same release-pin/helper runtime integration prerequisites are absent.
- `TestMuxProductHTTP1EightSeats` — `MUX_PUPPETEER` not configured.
- `TestSessionHistoryTwentyAgentPublishP99` — performance-only gate requires `DW_SESSION_HISTORY_PERF=1`.

### Mutation results

1. Replaced the sanitized excerpt with raw body: `TestFeishuCopyTextContainsOnlySanitizedSummary` failed on hidden prompt, credential, and Feishu ID assertions.
2. Removed the persistent opt-out check: `TestExternalCopyPersistentOptOutBlocksDefaultAndLeavesPrimaryDeliveryPG` failed at `PERSISTENT_OPTOUT_PRECEDES_EXTERNAL_DEFAULT` (`copy_count=1`).
3. Allowed an omitted name to select an Agent when multiple are controlled: `TestAgentFeishuCopyCommandRequiresVerifiedIdentityAndDisambiguates` failed because the ambiguous command changed the setting.
4. Changed the internal default to enabled: `TestFeishuCopyPersistentPreferenceAndInternalRecipientResolutionPG` failed at `INTERNAL_COPY_DEFAULT_OFF` (`count=1`).

The final passing m8 run skipped both `TestGroupReplyMCP` and `TestSpecMCPEndToEnd` because the actual-helper subprocess variable was not set; those gates are therefore not claimed as exercised. The new internal routing behavior is covered by in-process Hub tests and PG outbox tests, not by an actual helper process in this run.
