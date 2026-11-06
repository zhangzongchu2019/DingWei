# S-R52-TRUSTED-ORIGINS-IMPL evidence

Original R52 baseline: `f7f31fca1d1f1468285138578c3f6fafacfb5537`; supplemental upload/render scope starts from accepted intermediate tip `d555768c102b04caac3a1f05afcff4a55832cc13` in independent worktree `/tmp/togpt60lunazzc-r52-trusted-origins-impl`; branch intent `togpt60lunazzc/r52-trusted-origins-impl`.

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
| Baseline helper gate, locked interpreter rerun | Same clean `env -i`, short `TMPDIR`, `/tmp/r52lv` runtime venv, module cache, `GOPROXY=off`, `GOFLAGS=-mod=readonly` command in baseline worktree `f7f31fca` | **rc=0**, `BUILD_TESTS_OK helper_profiles=2 python=27 shell=2`. The first rc=1 was collection environment only: default `/usr/bin/python3` could not import `pexpect` and `websockets`; both baseline and branch passed with the locked interpreter. |
| Strict OpenSpec | `OPENSPEC_TELEMETRY=0 npx --yes @fission-ai/openspec@1.16.0 validate --all --strict --no-interactive` | rc=0, 23 passed, 0 failed, 21:46:38Z. |
| Full m8 | `go test -json ./internal/m8 -count=1` | rc=0, 1468 pass, 37 skip, 0 fail; 22:04:13Z–22:08:04Z. Skip names/reasons below. |
| Full workpulse | `go test -json ./cmd/workpulse -count=1` | rc=0, 233 pass, 7 skip, 0 fail; final-source run completed 22:05:41Z. Skips: `TestHLCBindFailure`, `TestHLCFaultProcessMatrix`, `TestHLCLoadMatrix`, `TestHLCS007Full` (isolated startup/process-matrix runners); `TestNPSessionPG8To9AndR50BinaryCompatibility` (certified R50 and current binary paths absent); `TestR31OldBinarySchemaReadCompatibility` (`R31_R30_BINARY` absent); `TestUpgradeRotationKeepsNewCredentialOnSharedWork` (requires helper E2E Python with websockets>=15). |
| Full store | `go test ./internal/store -count=1` | rc=0, 72.090s, completed before 21:32Z. |
| Targeted package tests | `go test -json ./internal/trustedorigin ./internal/identity/httpapi -count=1` | rc=0, 76 pass events, 0 skip/fail, 22:04:22Z–22:04:26Z. |

The helper gate returned rc=1 on both the implementation and baseline worktrees; it did not reach Python/shell checks.

## Scope extension: upload URL and renderer

- `internal/m8/file_capability.go`: the upload ticket now takes the request `Origin` only when the shared trusted-origin policy accepts it (exact `PublicOrigin` or an allow-listed origin). Empty `DW_TRUSTED_ORIGINS` keeps the existing `ticket.PublicBaseURL + "/files/upload"` path and exact response JSON bytes. Invalid, absent, duplicate, or untrusted Origin values fall back to the configured file-service base.
- `internal/fileservice/http.go` and `internal/fileservice/spec_permanent.go`: response `url` remains `PublicBaseURL + /f/ + key`; this is the canonical persisted address and was intentionally not rewritten.
- View Service injects validated read-only `VS_CANONICAL_PUBLIC_ORIGIN` as `DW_CANONICAL_PUBLIC_ORIGIN`. The shared markdown renderer regards both `location.origin` and canonical origin `/f/` URLs as own files; it localizes canonical links for draft attachments, body links, images, `src`/`href`, probes and download actions. Before send, current-origin `/f/` links normalize back to the canonical origin. The core was synchronized to the generated renderer HTML and the `internal/m8` embedded-core SHA pin was updated to `8a4f09f09df40489bafa9006bffd7bab5d1f57d942089fbf3f7c939f570f11aa`.

## 盘点补遗

盘点方式：在 `internal/`, `cmd/`, and `view-service/` source searched `FS_PUBLIC_BASE_URL`, `PublicBaseURL`, `*_PUBLIC_*`, `https://` construction and Hub `wss://`-to-HTTPS derivation; excluded tests, fixtures, vendored/minified assets, and documentation examples. 甲 is a browser-consumed Console/view URL; 乙 is a server or node client URL, including canonical persisted addresses; 丙 is an external/fixed navigation or third-party source.

| 类别 | 生成点与用途 | 经 `:50443` 的 Console 是否使用 | 本次处理 |
|---|---|---|---|
| 甲/乙 | `internal/fileservice/config.go` reads `FS_PUBLIC_BASE_URL` into `PublicBaseURL`; `internal/m8/file_capability.go` returns the upload ticket URL; `internal/fileservice/http.go` returns uploaded-file `/f/` URLs; `internal/fileservice/spec_permanent.go` returns permanent `/f/` URLs | Yes: ticket URL is POSTed by Console upload; `/f/` URLs are read in drafts, editor body and messages; canonical URL is stored with the message/spec | Ticket uses accepted request Origin; file-service response and persisted link stay canonical; View maps canonical `/f/` links locally for display and normalizes before send |
| 甲 | `view-service/view_service.py` reads `VS_CANONICAL_PUBLIC_ORIGIN` and injects the renderer's canonical origin; `view-service/md-render-core.js` performs own-file recognition, localization and send normalization | Yes, all Console file rendering/thumbnail paths | Covered; renderer canary and shared core synchronized, SHA pin updated |
| 乙 | `cmd/workpulse/newplat.go` reads `DW_PUBLIC_ORIGIN` as the Hub's canonical public origin/security origin; `internal/nodeagent/login.go` and `internal/nodeagent/backup.go` derive HTTPS login/upload bases from the node's configured Hub `wss://` URL; `internal/hosted/login.go` and `internal/hosted/backup.go` make hosted client login/backup endpoints | Not the Console file-upload/read flow; these are node/client protocols | Deferred to a later specification; not altered by this change |
| 丙 | Fixed external links include the SessionHelper download in `internal/m8/hub.go`, Feishu/LLM navigation in Console guidance/auth handlers, GitHub source navigation in `internal/specread/github.go`; `internal/portal/schedule.go` loads Mermaid from jsDelivr | Some Console external links are visible, but none is a Console `/f/` URL; Mermaid is a Portal page, and GitHub/API URLs are source/server requests | Deferred; no external navigation, CDN, client API or source-link behavior changed |

Coverage boundary: this change covers the Console upload-ticket origin and canonical `/f/` display/send cycle described above. File-service canonical URLs, CORS, all non-Console node/hosted URLs and unrelated external links remain unchanged pending any later ruling.

## Upload/renderer verification

- Chromium flow suite `view-service/trusted-origins-file-flow-regression.js` passed on both 443 and 50443: upload request uses the page origin; draft thumbnail loads; current-origin link is sent as canonical `/f/` URL; post-send rendering uses the page origin. The empty-list exact-byte ticket test and upload-ticket race3 both pass.
- Mutation controls both failed at their intended assertion: `--mutate-send-normalization` submitted a port-bearing URL to persistence; `--mutate-own-url` failed to display the 50443 draft thumbnail.
- Renderer parity: 103/103 fixtures pass; renderer synchronization check reports all six sections byte-identical. Python renderer/injection suite: 23 tests pass.
- Full browser bundle's last aggregate was rc=2: `md_render_core_parity`'s initial failure was fixed and its direct rerun passes; the remaining `renderer_replay_seq` S5 failure reproduces unchanged with the baseline `f7f31fca` renderer (epoch transition retains only `[NEW]` instead of `[002]…[030][NEW]`). The trusted-origin Chromium suite and all other reported browser suites pass. This pre-existing timeline replay failure is outside this scope.

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
- `./internal/m8`: initial serial run reached `-timeout 90m` at 00:34:25Z, rc=1. Go output: `FAIL .../internal/m8 5400.045s`, `panic: test timed out after 1h30m0s`; no assertion failure or race detector finding before timeout. Three requested repetitions did not complete. Final-source rerun `go test -json -race -count=3 -timeout 4h ./internal/m8` completed rc=0 at 03:14:16Z, 1474 unique pass names, 37 skip names, 0 fail names; 4422 pass events / 111 skip events across three repetitions. Log: `/tmp/r52-race-internal_m8-final.jsonl`. Skips are the opt-in/integration tests listed above.
- `./cmd/workpulse`: after the prescribed serial chain stopped at the preceding `m8` 90m timeout, separately ran `-race -count=3 -timeout 90m` from 00:35:05Z to 00:55:57Z; rc=0, 233 unique pass names, 7 skip names, 0 fail names. The seven skip names and reasons are listed in the full `workpulse` row above.
- Final-source serial `./cmd/workpulse` race3 rerun `go test -json -race -count=3 -timeout 4h ./cmd/workpulse`: **rc=0**, 03:14:45Z–03:35:50Z, 233 unique pass names, 7 unique skip names, 0 fail names (699 pass events / 21 skip events). Log: `/tmp/r52-race-workpulse-final.jsonl`.
- Final embedded markdown-core SHA pin test `go test -json -race -count=3 -timeout 2m ./internal/m8 -run '^TestConsoleSpecEmbeddedMarkdownCoreIsPinned$'`: **rc=0**, 03:35:58Z–03:36:00Z; 3 pass, 0 skip/fail.
- Final-source `./internal/trustedorigin`: `-race -count=3 -timeout 90m`, 00:57:41Z–00:57:43Z, rc=0, 11 pass/0 fail/0 skip.
- Final-source `./internal/identity/httpapi`: same flags, 00:57:43Z–00:59:36Z, rc=0, 65 pass/0 fail/0 skip.

## Delivery coordinates

- Branch: `togpt60lunazzc/r52-trusted-origins-impl`.
- Trusted-origin policy commit: `fd415c15c446c90f254eae285777e607a6ffc5b7`.
- Console file-origin implementation commit: `88e726627e92e15fbececcf90ed16f5305f174b8`.
- The pushed implementation tip was `88e726627e92e15fbececcf90ed16f5305f174b8`, tree `f012576abd0a16b7caf3300fdb172e37aed139ee`.
- Evidence checkpoint commit `fb3ee016c9e8065b148cd35e37a48ab4c22b45ce`, tree `b75d4457311759023ca8bf625e0066be6c70a3d5`; post-push `git ls-remote --heads origin togpt60lunazzc/r52-trusted-origins-impl` output was `fb3ee016c9e8065b148cd35e37a48ab4c22b45ce\trefs/heads/togpt60lunazzc/r52-trusted-origins-impl`. A final baseline-gate evidence correction follows this checkpoint; final tip is included in the final receipt.

## 2026-11-06 implementation handoff gates

The following supplemental implementation was developed in the authorized independent worktree at `/tmp/togpt60lunazzc-r52-trusted-origins-impl`, based on accepted tip `d555768c102b04caac3a1f05afcff4a55832cc13`; the branch already contained later renderer and evidence refinements, which were retained when integrating the supplemental commit.

- Five serial `./internal/m8` race shards, each `go test -json -race -count=3 -timeout 90m`, all rc=0 with no fail events. Top-level test names per shard: 179, 180, 179, 180, 180 (898 total). Ranges and UTC intervals: shard 1 `TestAgentDiskCurrentCredentialIngress`–`TestConsolePageShortNameAndOfflineGrouping` 04:04:21–04:31:24; shard 2 `TestConsolePageMultiTab`–`TestR14S019DirectUnknownNeverBlindlyReplays` 04:31:24–05:12:10; shard 3 `TestR14LateReconciliationPreservesNewerBinding`–`TestDeviceIDV1RejectsDifferentDeviceWithoutEvictingHolder` 05:12:10–05:46:15; shard 4 `TestSessionChurnDoesNotAffectOtherSessionLookupAndRoute`–`TestPilotTemplateHTTPSourceNoFetchAndForgedKind` 05:46:15–06:18:58; shard 5 `TestPilotTemplateHTTPActualConfirmRemainsEditing`–`TestSignViewServiceRequestBindsBody` 06:18:58–06:50:09. Logs: `/tmp/r52-m8-race-shards/shard-{1..5}.jsonl`; summary `/tmp/r52-m8-race-shards/summary.json`.
- The independent-worktree locked build gate `TMPDIR=$(mktemp -d /tmp/r52to-final.XXXXXX) PATH=/tmp/r52lv/bin:$PATH scripts/run-build-tests.sh` completed rc=0: `BUILD_TESTS_OK helper_profiles=2 python=27 shell=2`; helper inventory 90 files, 1097 expected cases per helper profile.
- Browser flow regression passed at 443 and 50443, covering upload target, draft thumbnail, canonical stored URL, and post-send local rendering. Both mutation checks failed at their intended assertion. Targeted Python and renderer tests passed (42 and 12 cases respectively).
- Current integrated branch strict OpenSpec validation: rc=0, 24 passed, 0 failed. Current integrated branch focused Go tests for trusted upload-ticket selection, new-platform upload ticket, and canonical View proxy header: rc=0. `git diff --check`: rc=0.

## 2026-11-06 预复核修正

- 组长对 `a706a53b` 预跑发现 canonical `Origin` 会覆盖 file-service `ticket.PublicBaseURL`，违反空 `DW_TRUSTED_ORIGINS` 时响应逐字节不变。修正 `uploadTicketPublicBase`：只有单一、格式精确、明确列入 `DW_TRUSTED_ORIGINS` 且不等于 `PublicOrigin` 的 Origin 才选择浏览器端点；canonical、空名单、非信任、多值均保留 file-service fallback。新平台 canonical 子用例期望同步为 fallback。
- `go test ./internal/m8 -run 'FileCapability|UploadTicket|Upload' -count=1`: rc=0。
- `go test -race -count=3 -timeout 90m ./internal/m8 -run 'FileCapability|UploadTicket|Upload'`: rc=0。
- 变异检查移除 `requestOrigin != h.PublicOrigin` 条件，`TestConsoleUploadTicketOriginSelection/empty_list_preserves_canonical_443_response_bytes` 按预期失败，报告实际 `https://dw123.cmjrt.com/files/upload` 与预期 `https://files.example/files/upload` 不同；变异随后撤销。
