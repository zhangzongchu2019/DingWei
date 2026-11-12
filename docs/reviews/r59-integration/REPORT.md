# S-R59-INTEGRATION 验证入口

任务来源：opus50tc2k.zzc开工令，source_message_id 8ced30a24b24468a87212a34da6f6429，实际入箱request_id dw-dlv_112a287d4bbb26da325fd9be336c0618。写者01a1cf05-9d3d-75e2-9233-eb9d75994424，锁/tmp/coder56-r59-integration.lock；普通uid1001，截止2026-11-12T18:00:00Z。不部署、不重启、不构建发布件。

## 提交与冲突

- helper冻结首父c0fecb12ed86003ff2d29a1c0902cfb16bc3f8c7。
- View冻结第二父e0de23713606d3e89f1bf578d268550e70d4786d。153bac4d为3c9f2722祖先已机器核验。
- no-ff merge c56556baabbd0f0b4f886cb3886c8865e37b0058，tree cd2e5dec858c221acd77d4453bd4a232d67ab73c；父序严格为以上两项。
- version d26a342d08d28bcae7fb393d50dba12b8359ba8f，父merge，仅同r58 c5f0c101的四文件抬至0.2.3.39；该提交不改页面且页面无当前0.2.3.39字面值；原历史注释版本引用按冻结blob保留。
- test infrastructure a8fcb1ae921dcdb5685f781a7410a9b863f082e3，父version；全量browser环境自检发现STALE_SCREENSHOT漏登记，补登记可选键，不设置该变量、不绕过自检或改产品。

renderer仅两处冲突：保留helper busyWait norm两行并接View unknownSinceMs/hidden；保留helper busy等待stageText并接View visibleStageText。独立生成blob 259ae2593386d1371f8442f9bb8e4027e7b6ee88，SHA256 8d39be9b7f9cf635da5cf5c4757a2e8030224a5ded9c1aa4f5c1aaa018a57d71。组长以source_message_id 5c2dafd17e3842799ab133bdeef5be69提供试合blob坐标，独立合并renderer与其逐字一致；两侧原件blob亦同值。具体在raw/renderer-blob-comparison.json。组长试合未推送，本报告不宣称fetch了其未发布整树。独立定向delivery-status20/20、model-admission26/26均rc0。

## 验证范围

最终产品树固定后执行：两种PATH完整run-build-tests（含均无codex/claude）、Go1.22.2下internal/m8全包与cmd/workpulse/internal/version/internal/mcpgw、无筛选全量browser runner（Node22）、OpenSpec1.16.0 strict、diff检查。所有子进程使用env -i等价的显式白名单环境、lock venv与HUB_TEST_PG_STRICT=1，临时HOME/TMP、合成协议或独立本机Hub/PG，不使用真实账号/在线席位。测试内部临时编译夹具不作发布件。

完整原始日志在/tmp/r59-cf05/raw；交件时填真实读数、跳过名及原文/完整日志hash，不把尚未结束的门当通过。后续只追加证据文档，不改已测产品源。


## 最终门读数

受测固定提交5a4c6beb1bd2311540b16569287532bddb7e10ad，tree 1c4b9336afa9cba8ebc1887a6160b30098326211；父a8fcb1ae921dcdb5685f781a7410a9b863f082e3。所有完整门都在这个提交上执行，后续证据提交只改文档/任务进度，不改已测产品。

### run-build-tests双PATH

| PATH | profile | expected | actual | rc |
|---|---|---:|---:|---:|
| normal | claude | 1203 | 1203 | 0 |
| normal | codex | 1203 | 1203 | 0 |
| no-cli | claude | 1203 | 1203 | 0 |
| no-cli | codex | 1203 | 1203 | 0 |

两路各完整27个Python脚本、2个shell脚本，原文BUILD_TESTS_OK helper_profiles=2 python=27 shell=2；均无named SKIP。normal可见codex与claude，no-cli两者均不可见，且Node两路均22.23.1。全部env -i等价白名单、普通uid1001、Go1.22.2、Python锁venv、HUB_TEST_PG_STRICT=1；完整argv/安全白名单环境、工具版本、UID、开始/结束UTC在raw/{normal,no-cli}-environment.json，逐行计数/结尾读数在各excerpt。

### Go指定包

执行原命令：go test -json -v -count=1 -p=2 -timeout=45m ./internal/m8 ./cmd/workpulse ./internal/version ./internal/mcpgw/...。全部四包rc0；以下为带Test字段的Go事件统计，包含父Test与子测试，包级PASS另列在raw/go-summary.json，不混入用例计数。

| 包 | PASS | FAIL | SKIP |
|---|---:|---:|---:|
| internal/m8 | 1545 | 0 | 37 |
| cmd/workpulse | 267 | 0 | 6 |
| internal/version | 5 | 0 | 0 |
| internal/mcpgw | 263 | 0 | 6 |

合计2080 PASS、0 FAIL、49 SKIP。完整-json原始日志/tmp/r59-cf05/raw/go.log；从Output字段逐字解出的完整-v落文件/tmp/r59-cf05/raw/go-v.log（不是只保留计数）。大日志不入Git，完整文件字节/SHA256见raw/log-artifacts.json；全部-v结果行在raw/go-v-results.log，所有SKIP原文/原因在raw/go-skips.json与raw/go-skips.log。以下全列49项，未执行的真实CLI、独立旧二进制及额外opt-in性能/故障/浏览器等条件不计通过；没有用真实账号、线上helper或发布二进制填这些门。

#### internal/m8 SKIP

- TestDeviceClaimAcrossActualClientAndHub — claim_client_integration_test.go:33: run scripts/test-desktop-hub-roundtrip.sh for cross-runtime enrollment
- TestDeviceClaimAcrossActualClientAndPostgres — claim_client_integration_test.go:45: run scripts/test-desktop-hub-roundtrip.sh for cross-runtime PostgreSQL enrollment
- TestChangeEvidenceConsoleBrowser — console_change_evidence_test.go:186: set CHANGEFLOW_PLAYWRIGHT to installed Playwright for Chrome integration
- TestMuxProductHTTP1EightSeats — console_mux_product_test.go:34: set MUX_PUPPETEER to installed puppeteer-core for actual Chrome product integration
- TestConsolePlatformPayloadDump — console_platform_agentgroups_test.go:133: 仅在浏览器闸取真载荷时运行(设 DW_CONSOLE_PLATFORM_PAYLOAD_OUT)
- TestConsoleTaskBoardStartedTaskRestrictionsPendingCaseRecordModel — console_task_board_page_test.go:122: K13 deferred: task case record model does not exist, so started state cannot be derived mechanically
- TestDeliveryACKActualCLIQueuedAdmissionAndTerminal/claude — delivery_ack_real_cli_e2e_linux_test.go:373: INCONCLUSIVE: set DW_ACK_V2_E2E_PYTHON for actual CLI/Hub acceptance
- TestDeliveryACKActualCLIQueuedAdmissionAndTerminal/codex — delivery_ack_real_cli_e2e_linux_test.go:373: INCONCLUSIVE: set DW_ACK_V2_E2E_PYTHON for actual CLI/Hub acceptance
- TestDeliveryACKActualCLIQueuedAdmissionAndTerminal/claude_explicit — delivery_ack_real_cli_e2e_linux_test.go:373: INCONCLUSIVE: set DW_ACK_V2_E2E_PYTHON for actual CLI/Hub acceptance
- TestDeliveryACKActualCLIReplaysPredecessorAcrossDisconnect/claude/drop_injected — delivery_ack_real_cli_e2e_linux_test.go:475: INCONCLUSIVE: set DW_ACK_V2_E2E_PYTHON for actual CLI/Hub acceptance
- TestDeliveryACKActualCLIReplaysPredecessorAcrossDisconnect/claude/drop_injected_confirm — delivery_ack_real_cli_e2e_linux_test.go:475: INCONCLUSIVE: set DW_ACK_V2_E2E_PYTHON for actual CLI/Hub acceptance
- TestDeliveryACKActualCLIReplaysPredecessorAcrossDisconnect/codex/drop_injected — delivery_ack_real_cli_e2e_linux_test.go:475: INCONCLUSIVE: set DW_ACK_V2_E2E_PYTHON for actual CLI/Hub acceptance
- TestDeliveryACKActualCLIReplaysPredecessorAcrossDisconnect/codex/drop_injected_confirm — delivery_ack_real_cli_e2e_linux_test.go:475: INCONCLUSIVE: set DW_ACK_V2_E2E_PYTHON for actual CLI/Hub acceptance
- TestDeliveryACKActualCLIAdmissionCrashWindows/claude/dispatch_sent_receipt_unread — delivery_ack_real_cli_e2e_linux_test.go:527: INCONCLUSIVE: set DW_ACK_V2_E2E_PYTHON for actual CLI/Hub acceptance
- TestDeliveryACKActualCLIAdmissionCrashWindows/claude/receipt_before_admitted_persist — delivery_ack_real_cli_e2e_linux_test.go:527: INCONCLUSIVE: set DW_ACK_V2_E2E_PYTHON for actual CLI/Hub acceptance
- TestDeliveryACKActualCLIAdmissionCrashWindows/codex/dispatch_sent_receipt_unread — delivery_ack_real_cli_e2e_linux_test.go:527: INCONCLUSIVE: set DW_ACK_V2_E2E_PYTHON for actual CLI/Hub acceptance
- TestDeliveryACKActualCLIAdmissionCrashWindows/codex/receipt_before_admitted_persist — delivery_ack_real_cli_e2e_linux_test.go:527: INCONCLUSIVE: set DW_ACK_V2_E2E_PYTHON for actual CLI/Hub acceptance
- TestDeliveryACKActualCLICancelAtomicBoundary/claude — delivery_ack_real_cli_e2e_linux_test.go:592: INCONCLUSIVE: set DW_ACK_V2_E2E_PYTHON for actual CLI/Hub acceptance
- TestDeliveryACKActualCLICancelAtomicBoundary/codex — delivery_ack_real_cli_e2e_linux_test.go:592: INCONCLUSIVE: set DW_ACK_V2_E2E_PYTHON for actual CLI/Hub acceptance
- TestDeliveryACKActualCLILegacyUpgradeKeepsPerDeliveryProvenance/claude — delivery_ack_real_cli_e2e_linux_test.go:675: INCONCLUSIVE: set DW_ACK_V2_E2E_PYTHON for actual CLI/Hub acceptance
- TestDeliveryACKActualCLILegacyUpgradeKeepsPerDeliveryProvenance/codex — delivery_ack_real_cli_e2e_linux_test.go:675: INCONCLUSIVE: set DW_ACK_V2_E2E_PYTHON for actual CLI/Hub acceptance
- TestDeliveryACKActualCLILateWireTerminal — delivery_ack_real_cli_e2e_linux_test.go:733: INCONCLUSIVE: set DW_ACK_V2_E2E_PYTHON for actual CLI/Hub acceptance
- TestDesktopMCPRoundTripAcrossRealHub — desktop_mcp_roundtrip_test.go:25: run scripts/test-desktop-hub-roundtrip.sh for the cross-runtime gate
- TestFailureNoticeDriverTurnEndToEnd — failure_notice_driver_e2e_test.go:24: set DW_FAILURE_NOTICE_PYTHON to locked helper runtime
- TestGroupReplyMCP — group_reply_test.go:104: requires locked helper Python; set DW_SPEC_E2E_PYTHON
- TestHelperSelfUpdateEndToEnd/install_restart_confirm — helper_update_e2e_test.go:75: set DW_HELPER_E2E_PYTHON to locked helper runtime Python
- TestHelperSelfUpdateEndToEnd/sha_mismatch_keeps_factory — helper_update_e2e_test.go:75: set DW_HELPER_E2E_PYTHON to locked helper runtime Python
- TestHelperSelfUpdateEndToEnd/new_layout_rejected_keeps_factory_and_projects_reason — helper_update_e2e_test.go:75: set DW_HELPER_E2E_PYTHON to locked helper runtime Python
- TestHelper0232MixedVersionEndToEnd — helper_update_e2e_test.go:55: explicit immutable release pins required
- TestHostedBodySendAcrossRealHub — hosted_body_send_test.go:23: set explicit isolated integration interpreter
- TestEnvelopeReadTimeoutEnvironmentChild — hub_timeout_contract_test.go:50: subprocess probe only
- TestPilotQuestionAcrossRealHTTPHelperIPCMCP — pilot_question_roundtrip_test.go:17: set locked helper Python; canonical runs this local integration
- TestPilotTemplateAcrossRealHTTPHelperIPCMCP — pilot_template_roundtrip_test.go:17: set locked helper Python; canonical runs loopback integration
- TestProjectContextAcrossRealHTTPHelperIPCMCP — project_context_roundtrip_test.go:17: set DW_DESKTOP_INTEGRATION_PYTHON to the locked helper test venv; canonical shell gate runs the cross-runtime set
- TestSessionHistoryTwentyAgentPublishP99 — session_history_test.go:520: set DW_SESSION_HISTORY_PERF=1 for isolated 20-agent latency gate
- TestSpecMCPEndToEnd — spec_mcp_e2e_test.go:211: set DW_SPEC_E2E_PYTHON for actual helper/MCP subprocesses
- TestBlockerAcrossRealHTTPHelperIPCMCP — task_blocker_roundtrip_test.go:21: set locked helper Python; canonical runs this local integration

#### cmd/workpulse SKIP

- TestNPSessionPG8To9AndR50BinaryCompatibility — np_session_migration_binary_test.go:26: set independently built current and certified R50 binary paths
- TestR31OldBinarySchemaReadCompatibility — r31_rollback_compat_test.go:23: requires independently built R30 binary via R31_R30_BINARY
- TestHLCFaultProcessMatrix — shutdown_fault_process_test.go:94: isolated fault/watchdog runner tests/hlc/fault_matrix.py
- TestHLCBindFailure — shutdown_fault_process_test.go:329: isolated startup failure runner
- TestHLCLoadMatrix — shutdown_load_test.go:563: isolated actual-process matrix: tests/hlc/process_matrix.py
- TestHLCS007Full — shutdown_mixed_fault_process_test.go:164: isolated full mixed fault: tests/hlc/s007_full.py

#### internal/mcpgw SKIP

- TestBrowserAuthorizeChainReachesLoopback — browser_e2e_test.go:31: browser E2E is opt-in: DW_BROWSER_E2E=1 NODE_PATH=<playwright>
- TestBrowserCallbackExplainsMissingTarget — browser_e2e_test.go:35: browser E2E is opt-in: DW_BROWSER_E2E=1 NODE_PATH=<playwright>
- TestBrowserClaudeCodeLocalhostConsentChains/immediate-302 — browser_e2e_test.go:168: browser E2E is opt-in: DW_BROWSER_E2E=1 NODE_PATH=<playwright>
- TestBrowserClaudeCodeLocalhostConsentChains/consent-page — browser_e2e_test.go:168: browser E2E is opt-in: DW_BROWSER_E2E=1 NODE_PATH=<playwright>
- TestBrowserCodexIPLoopbackConsentChains/immediate-302 — browser_e2e_test.go:178: browser E2E is opt-in: DW_BROWSER_E2E=1 NODE_PATH=<playwright>
- TestBrowserCodexIPLoopbackConsentChains/consent-page — browser_e2e_test.go:178: browser E2E is opt-in: DW_BROWSER_E2E=1 NODE_PATH=<playwright>

### 全量浏览器runner

最终无筛选运行全部41套，41/41套rc0，aggregate_rc=0；原始机器回执raw/browser-receipt.txt绑定受测commit 5a4c6beb，全部suite.*.rc逐项在raw/browser-summary.json，完整控制台原文raw/browser.log。执行：python3 tools/run-browser-regressions.py --receipt <独立回执路径>，未传选格参数。Node22.23.1、Playwright1.56.1、Playwright Chromium构建1194的本地安装位置由环境文件记录；仅NODE_PATH与PLAYWRIGHT_BROWSERS_PATH为显式依赖路径override，未设置产品行为/截图/夹具替换变量。

初轮41套中mcp_zero_consent=1、其余40套0，aggregate_rc=2，不能冒作通过；完整初轮日志、回执、环境在raw/browser-initial.*与raw/browser-receipt-initial.txt。初轮错误为隔离Hub未ready即退出，其Hub stdout被该套件临时目录自动清理，未恢复原错误正文，故不伪造确证根因。并发Go门原始头部实际下载cron/v3与go-pinyin，而该浏览器探针强制GOPROXY=off；冷离线模块缓存是依时间线推断的可能原因，不是已追回的原始错误。Go门完成后，完全同环境、同产品提交用持久输出目录复跑该套，单目标/多目标×immediate/consent四组合均browser_rc=hub_rc=0；子日志/退出码在raw/mcp-zero-diagnostic，仅交非秘密stdout及结果，不交ready配置、keys、HOME或数据库。然后第二次完整无选格runner 41/41全绿，源码未变。初轮与最终轮均保留，不以定向复跑替代完整门。

C2套件沿原机制报告25个硬通过及T-166四项既有known-defect形态变化0；aggregate_rc=0不表示这些旧问题已修复。定向合并检查delivery-status20/20、model-admission26/26均rc0，原文在target两日志。

### strict、diff与版本/父关系

env -i、Node22、OPENSPEC_TELEMETRY=0，OpenSpec1.16.0 validate --all --strict：42 passed/0 failed；git diff --check（工作树及helper冻结基线到候选）rc0。四个版本文件与r58 c5f0c101文件集合逐字相同；当前根/Go/helper版本0.2.3.39，版本提交不触碰页面。renderer沿用冻结历史注释中的旧版本引用，未新增当前版本硬编码，且blob始终259ae259与组长逐字一致。

## 证据与复跑

raw/environment文件逐项记录实际命令、白名单env、UID、版本、UTC和rc；依赖为已安装锁venv和公开Go模块/构建缓存，非生产或在线CLI状态。为离线浏览器夹具准备Go依赖，推荐先完成指定Go门，再执行强制GOPROXY=off的完整browser runner；不改变产品树。两种PATH复跑时分别保留/usr/local/bin或以仅含Node22的临时bin替代，保留/usr/bin的Go1.22.2及锁venv Python。全部源/测试通过Git取件；不上传源包或发布件。

raw/source-sha256.json记录当前10个关键源/钉值/runner文件SHA，raw/renderer-blob-comparison.json记录与组长对比；commits.json记录所有新产品/计划提交与准确父序。最终交件tip/tree及证据提交父关系随send.py交审回执提供。SHA256SUMS覆盖交件文件；小型日志仅剥行尾空白以满足diff门，原始字节hash在raw/original-log-hashes.json，大日志原件与解码-v原件hash在raw/log-artifacts.json，不改计数/断言。

受测5a4c6beb1已创建并推送工蜂refs/heads/coder56solzzc/r59-integration，ls-remote逐字一致；随后仅追加本报告及证据/任务收尾。最终证据提交首父为受测5a4c6beb1，最终tip/tree以交审消息为准；没有force push或改写任何冻结提交。
