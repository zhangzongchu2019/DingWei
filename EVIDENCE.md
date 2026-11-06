# S-R52-TRUSTED-ORIGINS-IMPL evidence

Baseline: `f7f31fca1d1f1468285138578c3f6fafacfb5537`; branch `togpt60lunazzc/r52-trusted-origins-impl`.

## Scope

- Added `internal/trustedorigin`: `DW_TRUSTED_ORIGINS` is empty by default; invalid/empty elements, malformed origins, normalized duplicates, non-HTTPS, and noncanonical ports fail startup with explicit errors. Configured entries normalize scheme/host and require explicit port.
- The shared decision preserves each existing handler predicate when the list is empty, without normalizing the legacy values first. The 14 inventoried source-validation write points use it. `/view` WebSocket follows the same policy. SSE validates a supplied Origin before writing stream headers; no-Origin and empty-list behavior remain unchanged.
- Link/redirect generation, Cookie attributes, CORS, `/ws/agent`, proxy headers, and version are unchanged.

## Build and validation gates

UTC, 2026-11-05 unless stated.

| Gate | Command | Result |
|---|---|---|
| Build helper gate, implementation tree | `TMPDIR=$(mktemp -d /tmp/r52to.XXXX) scripts/run-build-tests.sh` | rc=1 at 21:46:32Z. `HELPER_INVENTORY_OK files=90`; Claude and Codex discovery each reported: `helper files without collected cases: ['test_desktop_endpoint', 'test_guide_staging', 'test_ipc_socket_path_isolation', 'test_sessionhelper']`. Python/shell stages did not run. |
| Same gate, baseline | Same command in baseline worktree at f7f31fca | rc=1 at 21:50:03Z with the same four files and both profile failures. Failure was reported to lead (send receipt request_id `a890de0a54af`). |
| Build helper gate, locked interpreter rerun | `TMPDIR=$(mktemp -d /tmp/r52to.XXXX) PATH=/tmp/r52lv/bin:$PATH scripts/run-build-tests.sh` | **rc=0**, started 01:01:26Z, ended 01:10:09Z. `HELPER_INVENTORY_OK files=90`; both `claude` and `codex` profiles each expected and ran 1097 cases (groups 1088+9), rc=0; `BUILD_TESTS_OK helper_profiles=2 python=27 shell=2`. The initial default `/usr/bin/python3` gate lacked `pexpect` and `websockets`; the locked venv supplies both. |
| Strict OpenSpec | `OPENSPEC_TELEMETRY=0 npx --yes @fission-ai/openspec@1.16.0 validate --all --strict --no-interactive` | rc=0, 23 passed, 0 failed, 21:46:38Z. |
| Full m8 | `go test -json ./internal/m8 -count=1` | rc=0, 1468 pass, 37 skip, 0 fail; 22:04:13Z–22:08:04Z. Skip names/reasons below. |
| Full workpulse | `go test -json ./cmd/workpulse -count=1` | rc=0, 233 pass, 7 skip, 0 fail; final-source run completed 22:05:41Z. Skips: `TestHLCBindFailure`, `TestHLCFaultProcessMatrix`, `TestHLCLoadMatrix`, `TestHLCS007Full` (isolated startup/process-matrix runners); `TestNPSessionPG8To9AndR50BinaryCompatibility` (certified R50 and current binary paths absent); `TestR31OldBinarySchemaReadCompatibility` (`R31_R30_BINARY` absent); `TestUpgradeRotationKeepsNewCredentialOnSharedWork` (requires helper E2E Python with websockets>=15). |
| Full store | `go test ./internal/store -count=1` | rc=0, 72.090s, completed before 21:32Z. |
| Targeted package tests | `go test -json ./internal/trustedorigin ./internal/identity/httpapi -count=1` | rc=0, 76 pass events, 0 skip/fail, 22:04:22Z–22:04:26Z. |

The helper gate returned rc=1 on both the implementation and baseline worktrees; it did not reach Python/shell checks.

## Targeted mutation checks

Final-source mutation overlays ran against `./internal/identity/httpapi` at 22:07:38Z–22:07:40Z. Each mutant exited rc=1 on the named assertion:

- Remove membership check: `TestConsoleWriteUsesTrustedOriginPolicy/extra_origin`; `httpapi_test.go:267: status=403 ... want=404`.
- Ignore port: `.../different_port_rejected`; `httpapi_test.go:267: status=404 ... want=403`.
- Accept wildcard subdomains: `.../wildcard_subdomain_rejected`; `httpapi_test.go:267: status=404 ... want=403`.
- Accept HTTP: `.../http_rejected`; `httpapi_test.go:267: status=404 ... want=403`.

## m8 full-run skip names and reasons

- Requires `scripts/test-desktop-hub-roundtrip.sh` / `DW_DESKTOP_INTEGRATION_PYTHON`: `TestDeviceClaimAcrossActualClientAndHub`, `TestDeviceClaimAcrossActualClientAndPostgres`, `TestDesktopMCPRoundTripAcrossRealHub`.
- Requires `CHANGEFLOW_PLAYWRIGHT`: `TestChangeEvidenceConsoleBrowser`.
- Requires installed Puppeteer/Chrome (`MUX_PUPPETEER`): `TestMuxProductHTTP1EightSeats`.
- Requires `DW_CONSOLE_PLATFORM_PAYLOAD_OUT`: `TestConsolePlatformPayloadDump`.
- Deferred because task case record model does not exist: `TestConsoleTaskBoardStartedTaskRestrictionsPendingCaseRecordModel`.
- Requires actual CLI ACK E2E Python (`DW_ACK_V2_E2E_PYTHON`): `TestDeliveryACKActualCLIQueuedAdmissionAndTerminal/claude`, `/codex`, `/claude_explicit`; `TestDeliveryACKActualCLIReplaysPredecessorAcrossDisconnect/claude/drop_injected`, `/claude/drop_injected_confirm`, `/codex/drop_injected`, `/codex/drop_injected_confirm`; `TestDeliveryACKActualCLIAdmissionCrashWindows/claude/dispatch_sent_receipt_unread`, `/claude/receipt_before_admitted_persist`, `/codex/dispatch_sent_receipt_unread`, `/codex/receipt_before_admitted_persist`; `TestDeliveryACKActualCLICancelAtomicBoundary/claude`, `/codex`; `TestDeliveryACKActualCLILegacyUpgradeKeepsPerDeliveryProvenance/claude`, `/codex`; `TestDeliveryACKActualCLILateWireTerminal`.
- Requires `DW_FAILURE_NOTICE_PYTHON`: `TestFailureNoticeDriverTurnEndToEnd`.
- Requires locked helper Python (`DW_SPEC_E2E_PYTHON`): `TestGroupReplyMCP`, `TestSpecMCPEndToEnd`.
- Requires locked helper E2E Python and immutable release pins: `TestHelperSelfUpdateEndToEnd/install_restart_confirm`, `/sha_mismatch_keeps_factory`, `/new_layout_rejected_keeps_factory_and_projects_reason`, `TestHelper0232MixedVersionEndToEnd`.
- Requires explicit isolated integration interpreter: `TestHostedBodySendAcrossRealHub`.
- Subprocess-only probe: `TestEnvelopeReadTimeoutEnvironmentChild`.
- Requires locked helper Python: `TestPilotQuestionAcrossRealHTTPHelperIPCMCP`, `TestPilotTemplateAcrossRealHTTPHelperIPCMCP`, `TestBlockerAcrossRealHTTPHelperIPCMCP`.
- Requires project desktop integration Python: `TestProjectContextAcrossRealHTTPHelperIPCMCP`.
- Opt-in perf (`DW_SESSION_HISTORY_PERF=1`): `TestSessionHistoryTwentyAgentPublishP99`.

## Race checks

Serial command: `go test -json -race -count=3 -timeout 90m <package>`.

- `./internal/store`: started 21:34:01Z; rc=0 at 23:04:01Z; 592 unique pass names, 5 skip names, 0 fail names. Skips: `TestConsoleTaskLegacyDODMigrationPendingProductionCount` (K17 awaits production count); `TestConsoleTaskStartRequiresAcceptanceLinkPendingStartGateRuling` (K7 ruling pending); `TestFeishuStickyOldHubMigrationRollbackCompatibility` (exact R14 binary absent); `TestR10OldR48HubUpgradeAndRollbackOnIsolatedCopy` (exact R48 binary absent); `TestR10RestartChildAuthority` (subprocess probe covered by parent test `TestR10SeparateProcessRestartPreservesAuthorityAndUnknownNotice`).
- `./internal/m8`: initial serial run reached `-timeout 90m` at 00:34:25Z, rc=1. Go output: `FAIL .../internal/m8 5400.045s`, `panic: test timed out after 1h30m0s`; no assertion failure or race detector finding before timeout. Three requested repetitions did not complete. An extended rerun `go test -json -race -count=3 -timeout 4h ./internal/m8` completed rc=0 at 03:14:16Z (started 00:34Z), 1474 unique pass names, 37 skip names, 0 fail names; 4422 pass events / 111 skip events across three repetitions. Log: `/tmp/r52-race-internal_m8-final.jsonl`. This was run in the shared checkout while additional unstaged, out-of-scope edits were present in `internal/m8/file_capability.go` and its test, so it supplements but does not erase the prescribed clean-scope 90m timeout result above. Skips are the opt-in/integration tests listed above.
- `./cmd/workpulse`: after the prescribed serial chain stopped at the preceding `m8` 90m timeout, separately ran `-race -count=3 -timeout 90m` from 00:35:05Z to 00:55:57Z; rc=0, 233 unique pass names, 7 skip names, 0 fail names. The seven skip names and reasons are listed in the full `workpulse` row above.
- Final-source `./internal/trustedorigin`: `-race -count=3 -timeout 90m`, 00:57:41Z–00:57:43Z, rc=0, 11 pass/0 fail/0 skip.
- Final-source `./internal/identity/httpapi`: same flags, 00:57:43Z–00:59:36Z, rc=0, 65 pass/0 fail/0 skip.

## Delivery coordinates

- Branch: `togpt60lunazzc/r52-trusted-origins-impl`.
- Implementation commit: `fd415c15c446c90f254eae285777e607a6ffc5b7`.
- Implementation tree: `00f24a3749729189f752a97286060f2e3cc8d84d`.
- Push verification (raw `git ls-remote --heads origin togpt60lunazzc/r52-trusted-origins-impl` output):
  `fd415c15c446c90f254eae285777e607a6ffc5b7	refs/heads/togpt60lunazzc/r52-trusted-origins-impl`.

This evidence update is committed separately after the implementation commit; the final report sends its own final tip/tree and post-push `ls-remote` output.
