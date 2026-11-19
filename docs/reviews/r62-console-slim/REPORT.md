# S-R62-CONSOLE-SLIM 交件报告（菜单只留 Agent 与平台管理）

- 席位：qwen38mvb4.zzc；派单：组长 2026-11-18T14:14:27Z（业主令 rulings ㊿-补129，tip ee1ade2b）。
- 分支：qwen38mvb4/r62-console-slim；基：main 206d41b26dab646c0e50a111eed5a45ccf7c72f6（ls-remote 核一致）。
- 代码提交：879612362fb5e6aeb7197a1627b8e89fae4edcea（14 件，298 增 141 删）；交件 tip 见文末（docs 提交后）。
- 期限：2026-11-19T06:00:00Z。不推 main、不改 VERSION、不部署、不出发布件。
- 门先报：request_id=1e942f13d172（14:55Z）；收单回执：request_id=1f4576bbfc04（14:15:30Z）。

## 一、范围与实现

业主口径：整个菜单只保留「Agent」与「平台管理」。只摘入口，后端路由／表／数据／迁移一律不删不改；直连旧 URL 的行为保持现状，不 404 化。

产品三件：
1. internal/m8/newplat_console.go（handleNewPlatformConsolePlatform）：新平台菜单恰为 agent-groups「Agent 管理」（人人）＋platform-admin「平台管理」（管理角色）。不再下发 specs／task-board／pilot-questions／coordinate-reviews／openspec-confirmations 五项。
2. internal/m8/console_platform.go（HandleConsolePlatform，旧平台同形下发处）：摘 specs／task-board 两 builtin 与 coordinate-reviews／openspec-confirmations 两条件项（旧平台本无 pilot-questions）。保留非枚举既有 builtin（admin 旧版后台、change-evidence、openspec-read）与仓库提交项（见§二-1 披露）。consolePlatformReservedItemIDs 防撞闸原样保留（后端提交验证，非菜单下发）。
3. internal/m8/console_auth.go（renderPlatform）：只摘 specs／task-board 两条菜单 onclick 接线。菜单外无其它入口按钮（全文件核过）。保留 task_manager=1 查询参数直连自动打开路径（openTaskBoardWorkspace）与 openSpecsWorkspace 函数定义；直连 /console/specs、/console/tasks 等后端路由原样。

页面源码只写任务号（S-R62-CONSOLE-SLIM）与 rulings 条目号，不写版本号。

## 二、解释性裁定（披露，候组长核）

1. 旧平台保留非枚举既有 builtin（admin／change-evidence／openspec-read）与仓库提交项：派单枚举摘五项，新平台落实恰 2／1；「只留两项」业主口号在旧平台的落地范围以派单枚举为准。若组长裁定旧平台也须恰两项，本席再摘（小改）。
2. consolePlatformReservedItemIDs 保留被摘 id 的占用规则：放行同 id 仓库提交会出现重复 data-item-id，闸与 UI 无法唯一寻址（原注释理由仍成立）。
3. 直连旧 URL 不 404 化：task_manager=1 自动打开路径保留并有格钉住（task-board 套件入口即走此路径）；/console/specs 直连由 console_specs_browser／console_spec_workflow 两套既有 runner 格覆盖（未改、G2 全绿）。
4. panel-pointerevents 套件失效变异锚修复：原锚 isPlatformPaneKey 已不存在于产品（锚漂移假绿），改锚 console_auth.go 真实防御「双下划线前缀 session 不开平台页」（与 console-browser 的 disable-platform-tabs-on-agent-refresh 同锚）。
5. specs-theme 套件改钉保留直连路径的任务管理 iframe（Specs 入口已摘，主题跨 iframe 边界判据不能悬空）：跟随外壳、src 不变、DOM／输入状态不丢、先暗后开四条判据不放宽；原「四色块互异」换成「面板底色随主题真实明暗变化」（亮 rgb(246,248,251)→暗 rgb(27,31,36)）；三刀重锚（same-block-colors／no-theme-receiver／reload-task-frame）。

## 三、测试改造与新增

Go（internal/m8）：
- newplat_view_test.go（TestNewPlatformConsolePlatform）：管理角色 id 序列恰 agent-groups,platform-admin；成员恰 agent-groups；五被摘项缺席循环。
- console_platform_agentgroups_test.go（TestConsolePlatformAgentGroupsEntryIsInternalTabForBothRoles）：owner 菜单恰 1 项 agent-groups。
- console_platform_test.go（TestConsolePlatformHandlersExposeOnlyActionableBuiltinsAndEnforceWorkflow）：owner 菜单恰 1 项 agent-groups。
- 新增 console_menu_slim_test.go 两格：TestNewPlatformConsoleMenuSlimExactlyTwoEntries（newViewEnv 真实载荷，管理恰 2／成员恰 1＋缺席循环）、TestLegacyConsoleMenuSlimRemovedEntriesAbsent（consolePlatformItemsFor 两角色，被摘项缺席＋agent-groups 在＋platform-admin 仅管理）。
- console_openspec_confirmation_test.go：TestR7OpenSpecConfirmationNavigationRequiresTrustedAdapter 翻转为 TestR7OpenSpecConfirmationSlimMenuNeverExposesEntry——任何适配器状态（未接线／接线无权威／就绪）菜单都不得下发该入口；「平台管理员不因此获得委员会权限」POST 403 判据原样保留。
- 不受影响核过：coordinate_review_test.go／coordinate_review_compat_test.go 只测后端路由；r31_admin_agent_capability_test.go 只断言 agent_list_admin 能力位；console_task_board_page_test.go 测后端载荷。

浏览器（view-service，六套）：
- console-browser-regression.js：夹具新形状（人人 agent-groups＋管理 platform-admin）；删 legacy-specs-entry 旧刀、新增 restore-slimmed-entries 刀（夹具把被摘三项发回来⇒缺席格红）；owner 格恰 1 项＋缺席循环；新格「slim menu is exactly two entries for management」；计时格与平台管理格改钉 platform-admin（adminInvite 探针）；补 platform/users 桩。
- console-quota-regression.js：夹具 specs→agent-groups；owner 等待项与缺席断言同形。
- console-platform-admin-regression.js：platformItems 基项 specs→agent-groups（管理仍＋platform-admin＋admin）；owner 等待与缺席同形。
- console-task-board-regression.js：夹具 task-board→agent-groups；入口改 /console?task_manager=1 直连＋菜单缺席断言；:307 受阻流本就直连，未改。
- console-panel-pointerevents-regression.js：specs→platform-admin 全程（夹具、桩、paneState 探针 adminInvite/groupAddOpen、adminTab、④ inviteName 输入）；变异锚修复见§二-4。
- console-specs-theme-regression.js：见§二-5。

## 四、删保护刀（全红读数，日志见 evidence/）

浏览器：
- knife-browser-restore-slimmed-entries.log：rc1，passed=19 failed=2——「renders no platform entry without an observable action: owner menu must be exactly one entry」＋「slim menu is exactly two entries for management: management menu must be exactly two entries」两新格红。
- knife-panel-treated-as-agent.log：rc1，「离线 Agent 的面板必须保持禁用…实测 pointer-events=""」。
- knife-theme-same-block-colors.log：rc1，「面板底色必须随主题真实变化: rgb(246,248,251) -> rgb(246,248,251)」。
- knife-theme-no-theme-receiver.log：rc1，「iframe html data-theme must follow the shell」（短超时＋显式断言报因）。
- knife-theme-reload-task-frame.log：rc1，「iframe src changed: …#theme」。
- knife-taskboard-theme-drop-receiver.log：rc1，「任务管理页公共主题接收器必须已连接」（既有刀不受改造影响）。
- knife-quota-ignores-feature-flag.log：rc1，passed=14 failed=1（既有刀抽查）。
- knife-admin-no-logout-button.log：rc1，5 格 FAIL（既有刀抽查）。

Go 临时刀（每刀：上刀⇒目标格红⇒撤刀；撤刀后基线 6/6 绿、git 树净＝879612362）：
- GO-K1-newplat-specs.log：新平台回 specs ⇒ TestNewPlatformConsoleMenuSlimExactlyTwoEntries＋TestNewPlatformConsolePlatform 红（rc1）。
- GO-K2-legacy-specs.log：旧平台回 specs ⇒ TestLegacyConsoleMenuSlimRemovedEntriesAbsent＋TestConsolePlatformAgentGroupsEntryIsInternalTabForBothRoles＋TestConsolePlatformHandlersExposeOnlyActionableBuiltinsAndEnforceWorkflow 红（rc1）。
- GO-K3-legacy-openspec.log：旧平台回委员会条件项 ⇒ TestR7OpenSpecConfirmationSlimMenuNeverExposesEntry 红（rc1）。
- GO-baseline-after-knives.log：六格全 PASS（rc0）。

## 五、门读数（逐名；原始日志全量入 evidence/）

 env -i 白名单（HOME=/home/is3、短 TMPDIR、venv PATH、GOPROXY=off GOFLAGS=-mod=mod、HUB_TEST_PG_STRICT=1、NODE_PATH/PLAYWRIGHT 环境），脚本 run-r62-gates.sh，锚 879612362。

### G1 m8 全包 -count=1（14:55:40Z～15:06:13Z，629.247s）
- rc=0；顶层 PASS=988、FAIL=0、SKIP=11。逐名全清单在 G1-m8.log（-v 原日志）。
- SKIP 11 名：TestChangeEvidenceConsoleBrowser、TestMuxProductHTTP1EightSeats、TestConsolePlatformPayloadDump、TestConsoleTaskBoardStartedTaskRestrictionsPendingCaseRecordModel、TestDeliveryACKActualCLILateWireTerminal、TestFailureNoticeDriverTurnEndToEnd、TestGroupReplyMCP、TestHelper0232MixedVersionEndToEnd、TestEnvelopeReadTimeoutEnvironmentChild、TestSessionHistoryTwentyAgentPublishP99、TestSpecMCPEndToEnd（均为既有条件跳过，非本单新增）。
- 🔴 Z-3 单列：interrupted(9) 3 行，落在 TestViewDirectOutputCanStillMentionAgent（view_direct_no_return_test.go:198，该格 PASS 0.35s）——越出 r60 白名单（TestNewSessionNameNotifiesnAndReconnectnStaySilent＋TestViewMentionRoute/a_peer_without_delivery_ack_v1_is_not_sent_to）。聚焦定因×10（Z3-focused-r62/main.log）：r62 锚 10/10 PASS、9 行；main 206d41b2 基锚 10/10 PASS、3 行 ⇒ 两锚均现、同族继承性竞态（session delivery lease release 在断连时 interrupted），在新格浮现；本单产品 diff 仅菜单下发＋前端接线，不触租约／会话代码，非本单回归。记名披露，根因仍归 r61（togpt a931 线）。如组长要求本分支也走 Z-3 五轮门，另跑。

### G2 全量浏览器 runner（不选格；15:06:13Z～15:18:50Z）
- aggregate rc=2；46 套中 44 绿。逐名 rc 全清单在 G2-browser.receipt。
- 两红：r15_session_history_pagination rc=2、r15_session_history_mutations rc=1，同根 r15-session-history-browser-regression.js:96 的 3000ms 滚动状态窗超时（mutations 格为 drop_history_node_move／drop_scroll_anchor_correction 期待断言被 :96 超时抢先，「wrong assertion failed」）。
- 本单相关格：console_browser rc=0（passed=21 failed=0）、renderer_flood_stress rc=0、console_specs_browser／console_spec_workflow（直连 Specs 路由）rc=0。
- 基锚静默对照（R15-*，15:36:55Z～15:37:38Z）：pagination×5 r62 锚 rc 0/2/0/0/0、main 基锚 0/2/2/0/0；mutations×1 两锚均 rc1 同 :96 根因 ⇒ 两锚同抖，既有时序窗抖性。按组长既定裁定（mutant-revive 核收 09:17:14Z 等）：r15 两格归冻结 415b9bf5（S-BROWSER-FLAKE-WINDOWS v2），r61 集成后验证；本分支未触 r15 件，记名继承，不算本单回归。

### G3 run-build-tests（15:18:50Z～15:30:49Z）
- rc=1。helper 闸：claude profile rc=0（actual=expected=1270，groups 1261+9）；codex profile rc=1，唯一红＝test_guide_staging.GuideRealAdapterBoundaryTest.test_claude_stream_json_user_frame_boundary_counts_zero_one_two（test_guide_staging.py:654，SECOND 帧带平台上下文前缀）。
- 该红即组长 mutant-revive 裁定2 的已知继承红（归 S-HELPER-FLAKE-2，f7b32525 改过该测试件；main＋九冻结分支试合已 BUILD_TESTS_OK）。本分支基 main、未含 f7b32525、产品 diff 不触 helper。单格两锚复现（G3c-helper-single-r62/main.log）：均 rc0 OK ⇒ 整闸负载下才红，时序抖族成立。本单不修。
- set -e 致 python27／shell2 段未在 G3 内跑 ⇒ 补跑（G3b-python-shell.log，同 env 同清单）：python 27/27 OK、shell 2/2 OK（deploy/test-view-service-release.sh PACKAGED/VERIFIED；scripts/test-desktop-hub-roundtrip.sh 内 go test ok）。无 FAILED。

## 六、单跑验证（五套未登记 runner 的套件；基线＋刀）

- standalone-console-browser.log：passed=21 failed=0（G2 内再核 rc0）。
- standalone-quota.log：passed=15 failed=0。
- standalone-platform-admin.log：passed=32 failed=0。
- standalone-panel-pointerevents.log：passed=11 failed=0。
- standalone-task-board.log：assertions=87 floor=86 failed=0（K13 deferred 原样）。
- standalone-theme.log：rc0（light=rgb(246,248,251) dark=rgb(27,31,36) src 不变 state=kept fresh-open=dark）。
- 刀读数见§四。

## 七、证据清单

- evidence/ 83 件：三门原日志＋receipt＋rc＋时刻戳、Z-3 聚焦两锚、G3 补证（单格两锚＋python/shell 段）、Go 三刀＋基线、浏览器八刀、五套单跑、r15 两锚对照（含 anchor 件）、四个可复跑脚本。
- SHA256SUMS：REPORT.md＋evidence/ 全件（仓根相对路径，sha256sum -c 可核）。
- 不用文件服务、不用消息传源码；日志原样入 Git。

## 八、交件坐标

- 代码提交：879612362fb5e6aeb7197a1627b8e89fae4edcea。
- docs 提交（本文件＋证据）：交件 tip，push 后 ls-remote 核一致，随交件信报。
