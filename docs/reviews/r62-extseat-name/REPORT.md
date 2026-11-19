# S-R62-EXTSEAT-NAME 实施报告

## 范围与决策

- 基点：`206d41b26dab646c0e50a111eed5a45ccf7c72f6`。
- 新增 Hub 迁移 `0112_np_agent_display_name.sql`。开工前检查 main 与 R61 集成锚 `d6e56a8b` 的 Hub `01xx` 序号，最大均为 `0111`；history 数据库的 `0078` 属另一迁移流。
- `np_agent.display_name` 可空。迁移仅从已关联的 `np_external_principal.display_name` 回填外部 Agent；空值及超过 320 UTF-8 字节的旧值保留 NULL，canonical `name`、`name_lc`、ID 与 key 不动。每次读取再按 80 Unicode 字符与控制字符规则校验，避免依赖数据库 locale 的 Unicode 字符计数。
- 飞书已验证资料取 `name`，空时回退 `en_name`；不读部门。显示名经 NFC 规范化，最多 80 Unicode 字符且不超过 320 UTF-8 字节，拒绝控制字符及双向覆盖／隔离控制符。超过限制或不合规时不写显示名并沿用 canonical 地址显示。
- `DW_EXTSEAT_DISPLAY_NAMES_ENABLED` 可关闭显示名投影；缺值默认启用。关闭时保留持久数据与 canonical 名称，界面退回 canonical 地址，供回退使用。
- Console Agent 导航、普通编组成员／选择器及临时外部席卡片通过 textContent 显示 `显示名 · canonical 全名`；无显示名时用 canonical 全名。API 地址、key、`SplitAgentName` 和 AMR 仍只看 canonical 字段。切片三跨租户标签与编组语义、切片四抄送均未改。

## 验证

环境为隔离 `env -i`、Go 1.22.2、锁定 Python venv、临时 HOME/TMP；PG 测试启用 `HUB_TEST_PG_STRICT=1`。未连接现网。

- `scripts/run-build-tests.sh`：`BUILD_TESTS_OK helper_profiles=2 python=27 shell=2`，rc=0；Claude、Codex profile 各 1270/1270，inventory 97。
- 定向 Go 包三遍 PG strict：`go test -p 1 -count=3 -timeout=90m -v ./internal/extseat ./internal/extshared ./internal/identity/... ./internal/store/migrate ./internal/feishulogin ./internal/tempgroup`，rc=0；extseat 151.354s、extshared 0.002s、identity 249.346s、identity/httpapi 10.221s、migrate 183.336s、feishulogin 0.030s、tempgroup 56.155s；0 FAIL。
- `internal/m8` 最终三遍复跑：2910 顶层 PASS、1914 子测试 PASS、0 FAIL、81 顶层 SKIP＋54 子测试 SKIP（每遍 27 个唯一 skip case）。SKIP 均为代码标明的跨运行时／专用环境／性能门；完整名称为：`TestBlockerAcrossRealHTTPHelperIPCMCP`、`TestChangeEvidenceConsoleBrowser`、`TestConsolePlatformPayloadDump`、`TestConsoleTaskBoardStartedTaskRestrictionsPendingCaseRecordModel`、`TestDeliveryACKActualCLILateWireTerminal`、`TestDesktopMCPRoundTripAcrossRealHub`、`TestDeviceClaimAcrossActualClientAndHub`、`TestDeviceClaimAcrossActualClientAndPostgres`、`TestEnvelopeReadTimeoutEnvironmentChild`、`TestFailureNoticeDriverTurnEndToEnd`、`TestGroupReplyMCP`、`TestHelper0232MixedVersionEndToEnd`、`TestHostedBodySendAcrossRealHub`、`TestHostedBodySendSameRequestIDAcrossCLIProcesses`、`TestLegacyIngressBurstKeepsFIFOAndCommittedACK`、`TestLegacyIngressDefaultCycleLongBusinessKeepsPong`、`TestLegacyIngressLimitedWirePongCompetition`、`TestLegacyIngressReadPauseResumesWithoutDisconnect`、`TestLegacyIngressSmallControlBlockedPhysicalWrite`、`TestLegacyIngressSmallControlReadPause`、`TestMuxProductHTTP1EightSeats`、`TestPilotQuestionAcrossRealHTTPHelperIPCMCP`、`TestPilotTemplateAcrossRealHTTPHelperIPCMCP`、`TestProjectContextAcrossRealHTTPHelperIPCMCP`、`TestR60OutboundControlBackpressureRecoversWithSendStatus`、`TestSessionHistoryTwentyAgentPublishP99`、`TestSpecMCPEndToEnd`。理由依源码注释包括需调用 `scripts/test-desktop-hub-roundtrip.sh`、显式隔离 helper／CLI Python 或 immutable release pin、`CHANGEFLOW_PLAYWRIGHT`／`MUX_PUPPETEER`／`DW_SESSION_HISTORY_PERF` 等专用开关，以及 K13 数据模型尚不存在。
- 浏览器全量 `tools/run-browser-regressions.py`：45/45 suites，所有 suite rc=0，`aggregate_rc=0`。Console harness 20/20；临时编组浏览器断言 `empty_dom=byte_equal`、`sidebar=true`、`detail_members=2`、`page_errors=0`。完整 runner 报告的 T-166 legacy interrupt 既有缺陷被该套件按既定 known-defect 口径记录，suite 仍 rc=0。
- 实现完成后复跑相关 Go 包（PG strict）及两个 Console 浏览器回归：均 rc=0。
- `git diff --check`：通过。

### SQLite 中断复跑记录

同一最终工作树上的首次 `internal/m8 -count=3` 返回 rc=1，唯一失败为 `TestNewSessionNameNotifiesFeishuOnceAndReconnectFlappingStaySilent`，`hub_test.go:3741: interrupted (9)`。按现有 S-SQLITE-INTERRUPT 跟踪，这是已知 SQLite 驱动中断落点；保留该失败，不把它合并成通过。随后对相同源、相同 PG strict 环境完整重跑 `internal/m8 -count=3`，rc=0，2910 顶层 PASS、1914 子测试 PASS、81 顶层 SKIP、54 子测试 SKIP，未出现 `interrupted (9)`。

## 变异

每个变异在隔离的单次副本中运行，随后立即还原：

1. AMR 寻址误用显示名：把 `FindAgentByName` 扩为按 `lower(display_name)` 匹配。`TestOnlineProjectsExternalDisplayNamePG` rc=1，业务断言 `EXTSEAT_DISPLAY_NAME_MUST_NOT_ROUTE` 红。
2. XSS：把临时编组成员行的 `textContent` 改为 `innerHTML`。`console-temp-groups-regression.js` rc=1，业务断言“external display name and canonical address render as text”红。
3. 回填覆盖地址：迁移额外把 `np_agent.name` 改为 principal 显示名。`TestExternalDisplayNameMigrationBackfillsWithoutChangingAddress` rc=1，业务断言 `EXTSEAT_DISPLAY_BACKFILL_ADDRESS_INVARIANT` 红，读数 `[ext-with-name 张伟 张伟]` 与期望 `[ext-with-name alice_AG7 张伟]` 不符。

## 首轮门记录

第一次 build 门因既有 `TestProductionHubMigrationLedgerMatchesExpectedNames` 没列新迁移而失败；测试清单补入 `0112_np_agent_display_name.sql` 后全门重跑通过。浏览器全量首轮也揭示 Console harness 仍断言旧短名；更新 fixture 到新的 canonical 全名口径后，全量 45/45 通过。两项都是随本改动更新的过期测试预期，不是放宽产品断言。

迁移专项格首轮还发现测试 PG 的 Unicode `char_length` 受数据库编码口径影响，会把中文按字节计数。迁移因此只用 UTF-8 字节上限筛掉明显超大历史值；展示读取继续用 Go Unicode 字符数、规范化及控制字符校验。修订后的有效中文回填、超 320 字节旧值留空、canonical 地址不变均通过 PG 测试。

## 回退

设置 `DW_EXTSEAT_DISPLAY_NAMES_ENABLED=0` 关闭显示名读取即可恢复 canonical 名称显示。数据库列与回填保留；地址与路由字段从未变化。

## 复核补正：Unicode 规范化依赖登记

复核指出 `internal/extshared/extshared.go` 直接 import `golang.org/x/text/unicode/norm`，但 `go.mod` 将 `golang.org/x/text v0.19.0` 标为 indirect。现已将同一版本改列直接依赖，并按 `specs/gates/dependency-baseline.txt` 的三栏登记：模块标注由 indirect 更正为 direct；用途为 NFC 规范化外部 Agent 的 Unicode 显示名；来源为 Go Authors 维护的 `golang.org/x/text` 扩展库（BSD-3-Clause）；标准库无 NFC 实现，自维护 Unicode 数据／算法或省略规范化均不满足正确性与维护要求。

依赖集合棘轮从 9 更新为 10，集合 SHA256 为 `c2639a93f89866960b3c699d4f23f244afb7a9a7e7e58bcb51e944b897304540`。`GOFLAGS=-mod=mod go list golang.org/x/text/unicode/norm` 与 `GOFLAGS=-mod=mod scripts/dependency-ratchet.sh` 均 rc=0；go.sum 前后 SHA256 均为 `bf70c12f906c5501f71fea73fda264e30d64d463de5b4ce04989d36698512822`，未增加版本或校验项。原产品门读数沿用，不因本次依赖元数据修正重跑。命令输出见 `dependency-ratchet-mod-mode.log`。
