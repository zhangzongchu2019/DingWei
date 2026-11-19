# S-R62-OLDPLAT-SLIM 交件报告（旧平台菜单余 4 项摘除）

- 席位：qwen38mvb4.zzc；派单：组长 2026-11-18T22:03:16Z 原件（收件库 admission_unknown，已 read＋ack）＋23:22:03Z 补发信；收单回执 request_id=24bff2c3861e（2026-11-19T00:12Z）。
- 依据：业主 rulings ㊿-补133①（review/opus50tc2k/rulings tip d74a0958）＋㊿-补129「整个菜单只保留 Agent 和平台管理」；组长 2026-11-19T00:20:38Z 裁定取白名单口径（第四项「仓库提交」为库内自定义项）。
- 分支：qwen38mvb4/r62-mobile；基：冻结 tip 7d3035c6e（追加提交，不改写已冻结历史）。
- 期限：2026-11-20T06:00:00Z。不推 main、不改 VERSION、不部署、不出发布件、不碰预发/现网。

## 一、范围与实现

产品一件：internal/m8/console_platform.go（HandleConsolePlatform 旧平台分支）。

1. 摘 admin builtin（旧版后台，/console/admin）：management 块只留 platform-admin 内部页签；授权域注释同步改写（管理面凭证页仍仅 management 可见，理由不变）。
2. 摘 change-evidence（变更风险与验收证据）与 openspec-read（OpenSpec 只读文档）两条件 builtin；h.ChangeEvidence／h.SpecReader 接线保留（直连页面仍用）。
3. 白名单口径（组长 00:20:38Z 裁定）：`items = builtins`——console_platform_item 表内自定义项（含「仓库提交」项，已批准与待审）一律不再随菜单下发；ListConsolePlatformItems 读取保留只为维持表不可用时既有的 503 语义。表数据、提交／审批工作流与对应 API 不动。
4. consolePlatformReservedItemIDs 防撞闸原样保留（后端提交验证，非菜单下发）。
5. 前端核过：console_auth.go renderPlatform 对被摘四项无 onclick 接线（仅 agent-groups／platform-admin 有接线，保留）；菜单外无其它入口按钮指向被摘三 URL（/api/console/admin/* 是平台管理工作台 API、openSpecBindingEntry 是 openspec-bindings 功能，均不在四项内）。页面源码只写任务号（S-R62-OLDPLAT-SLIM）与 rulings 条目号，不写版本号。

## 二、白名单口径的影响（组长令写明）

管理员此后新增的自定义菜单项不会出现在旧平台菜单（提交／审批仍照旧落库，工作流 API 不变）；与新平台现状一致（新平台本就只发 agent-groups＋platform-admin）。组长会告知业主。「仓库提交」项即此类自定义项：本单不验回环，路由与数据未动。

## 三、测试改造与新增

Go（internal/m8）：
- console_menu_slim_test.go：consoleMenuSlimmedIDs 扩至 8 id（原 5＋admin／change-evidence／openspec-read）；TestLegacyConsoleMenuSlimRemovedEntriesAbsent 改用接线 ChangeEvidence／SpecReader 的夹具（不接线则两条件项「缺席」无判别力——加回也观测不到）＋菜单恰 2/1 形状断言；新增 TestLegacyConsoleMenuSlimCustomItemsNeverEmitted（夹具自建已批准＋待审自定义项各一，两角色菜单仍恰 2/1；不碰现网库）。
- console_platform_admin_entry_test.go：原「management 可见 admin」格翻转为 TestConsolePlatformAdminEntrySlimMenuNeverExposesEntry（两角色 admin 缺席＋阳性对照三条：200 可解析、项非空、通道看得见 agent-groups）；TestConsolePlatformAdminIDIsReserved 原样保留。
- console_platform_group_entry_owner_test.go：②owner 不含 admin 保留（文案更新）；TestSGroupEntryManagementStillSeesBothEntries 翻转为 TestSGroupEntryManagementKeepsWorkspaceEntriesNotLegacyAdmin（agent-groups＋platform-admin 必须在、admin 必须不在）。
- console_platform_test.go：工作流格「批准后上菜单」翻转为「菜单不下发自定义项＋批准照旧落库 approved」（204 与落库状态双断言，工作流语义不放宽）。

浏览器（view-service）：
- console-platform-admin-regression.js：platformItems 夹具摘 admin 项（与后端 Go 格同口径）；management 格翻转为 admin count 0（判据文案「旧版后台入口已摘（rulings ㊿-补133）：菜单不得再渲染 admin 项」）。owner 格 admin count 0 原样。

## 四、刀读数（5/5 红在判据；还原后基线绿）

| 刀 | 变异 | rc | 首红判据 |
| --- | --- | --- | --- |
| K1 admin-restore | management 块加回 admin builtin | 1 | 菜单不得再下发被摘项 "admin"（缺席格＋恰 2/1 格＋admin 缺席格同红） |
| K2 change-evidence-restore | 加回条件 builtin | 1 | 菜单不得再下发被摘项 "change-evidence"（接线夹具，两角色同红） |
| K3 openspec-read-restore | 加回条件 builtin | 1 | 菜单不得再下发被摘项 "openspec-read"（同上） |
| K4 custom-items-merge-restore | items = append(builtins, items...) | 1 | 库内有自定义项时菜单仍应恰为两项（repo-commits/pending-extra 出现即红）＋工作流格红 |
| K5 browser-admin-restore | 夹具加回 admin 项 | 1 | 旧版后台入口已摘（rulings ㊿-补133）：菜单不得再渲染 admin 项（31/1） |

- Go 刀日志 evidence/knives/K1..K4*.log；还原后目标格全绿 baseline-after-knives.log rc0。K5 日志 K5-browser-admin-restore.log；还原后 32/0（K5-baseline-after.log）。
- 刀锚唯一性：每把替换前核 count==1，替换后还原逐字核同（脚本 /tmp/oldplat-slim-knives.py 逻辑入证据说明）。

## 五、门读数

### G1 m8 全包 -count=1（00:29:42Z 起，423.163s）
- 顶层 PASS=971、SKIP=27、FAIL=2（evidence/G1-m8.log，-v 原日志）。
- 两红均 interrupted(9)（hub_test.go:3746／session delivery lease release），且恰落 Z-3 白名单两格：TestNewSessionNameNotifiesFeishuOnceAndReconnectFlappingStaySilent、TestViewMentionRoute/a_peer_without_delivery_ack_v1_is_not_sent_to。无白名单外 interrupted。
- 聚焦×10 两锚对照（evidence/z3/）：本树 rc 1/1/0/0/1/1/0/1/0/0（5 红）、基锚 7d3035c6 独立工作树 rc 0/1/0/1/0/0/1/1/0/1（5 红）；两锚红全为同两格 interrupted(9) ⇒ 既有继承性竞态（r61 togpt a931 线），非本单回归。本单产品 diff 仅菜单下发，不触租约／会话代码。
- 负载披露：m8 段尾与刀段起始重叠（ViewMentionRoute 红于 00:36:38Z，在刀段窗内）；聚焦段串行、K5 浏览器 22s 与 current 聚焦尾段重叠——两锚读数同级（5/10），结论不受影响。

### G2 全量浏览器 runner（不选格）
- 窗口 2026-11-19T00:41:53Z～00:55:39Z（13m46s），tools/run-browser-regressions.py 全量 51 套：**49 绿、2 红**，aggregate_rc=2（evidence/G2-browser.log 原日志＋G2-browser.receipt 逐套 rc＋G2-browser.rc）。
- 两红均 r15 继承族，红点逐字同既有口径：
  - r15_session_history_pagination rc2：page.waitForFunction Timeout 3000ms，r15-session-history-browser-regression.js:96（滚动状态翻转窗）。
  - r15_session_history_mutations rc1：restore_live_eviction_during_paged_history「wrong assertion failed」，实红同窗（r15:96 Timeout）。
- 继承判定：本分支 r15 两件对基锚 7d3035c6 `git diff` 为空（逐字同）；415b9bf5 时序窗修复未合入本基；与手机单 M1/M2 冻结信（组长 2026-11-18T19:40:26Z）「2 红为 r15 继承族（415b9bf5），接受」同族同处置——记名继承，r62 集成后验证，不在本单修。
- 本单靶件在全量内全绿：console-platform-admin 32/0（mutation=none）、console_browser 21/0。
- conn 段前后（evidence/conn-browser-before/after.json）：helper PID 555221、WS inode 1222944048 逐字同、ipc_connect_ex=0 ⇒ 重连 0，未触停段线。
- 跑法脚本 evidence/scripts/oldplat-browser.sh（env NODE_PATH/PLAYWRIGHT_BROWSERS_PATH、receipt 逐套 rc、段前后 conn、起止时刻）。

### G3 直连回环 GET（隔离实例）
- 装置：本工作树 go build workpulse，WP_ADDR=127.0.0.1:18777、隔离临时库 wp3.db、WP_CONSOLE_AUTH_ENABLED=1、种子夹具（principal＋agent，非现网数据）；PID 1364371，GET 时活、日志无 exited。
- 未登录 GET：/console/admin 401、/console/change-evidence 401、/console/openspec 401（＋/api/console/platform 401）；响应体均本实例文案「authentication required」（evidence/loopback/resp_*.txt）。路由未删未改，直连可达（未登录拒）。
- 🔴 假启动披露：先试 18099 端口被本机 nginx/1.24.0 占用（401 页为 nginx 体）且 workpulse 因种子校验未起 ⇒ 该轮读数作废不入证据；换 18777 后以响应体＋PID＋日志三重核实例归属才取数（evidence/loopback/NOTE.md）。
- 「仓库提交」：库内自定义项，本单不验回环（组长裁定）；路由与数据未动。
- GET 后实例 SIGTERM 优雅停（exit_class graceful、exit_code 0）；临时库不入库。

### 负载纪律
各段 conn 探针前后读数入 evidence/（helper PID 555221）；门段串行（重叠处已披露）；无人为加负载。

## 六、边界遵守

- VERSION 未改；main 未推；无发布件；预发/现网未碰（回环为隔离临时库＋种子夹具，非现网数据）；driver/helper 零改动（本单产品 diff 仅 Hub 菜单下发一处＋测试）。
- 后端路由、表、数据、迁移一律未动；直连旧 URL 行为保持现状（G3 实证）。

## 七、交件坐标

- 代码提交：4a662ef92（6 件：产品 console_platform.go＋Go 测试四件＋浏览器 console-platform-admin-regression.js；152 增 129 删）。
- docs 提交（本文件＋证据＋SHA256SUMS）：交件 tip，push 后 ls-remote 核一致，随交件信报。
