# S-BROWSER-FOLLOWUP 交付报告（浏览器负载敏感格统筹＋覆盖补格）

- 席：qwen38mvb4.zzc ｜ 派单：dw-dlv_d34853b589ac747be704eb4f07669b86（组长 2026-11-18T01:19:36Z，send request_id 21596458d396；原件 failed session_unavailable 从未注入，read＋ack 后按催问 dw-dlv_4be9a3287d85b12d88ecc83365e059d7 开工）
- 开工：2026-11-18T01:55Z ｜ 期限：2026-11-19T06:00:00Z
- 分支：qwen38mvb4/browser-followup ｜ 基锚：501e5b8a185b20f5c931c8ebb89c16bfd633a01e（r60 定锚，VERSION 0.2.3.40，renderer sha 14ffc39e… 与锚逐字同，本单未改 renderer）
- 范围：只改测试件＋docs。产品代码、helper、VERSION、md-render-core 零改动（⇒ 无 console_specs_assets_test.go sha 回写需求）。
- 提交：本分支 tip/tree 逐字见交件信与 git log -1（单提交，含证据）

## 一 改动清单

| 件 | 改动 |
|---|---|
| view-service/r15-follow-release-after-replay-regression.js | 新增：r15「重放自然收尾后用户上翻释放跟随」覆盖补格＋刀 release-only-in-replay |
| tools/run-browser-regressions.py | SUITES 登记 r15_follow_release_after_replay（1 行） |
| view-service/console-browser-regression.js | 三负载敏感格修法（§三），断言一字不改 |
| view-service/console-specs-browser-regression.js | S-7 复制链接段修法（§三），断言一字不改；新增 CONDITION_WAIT_MS 常量 |
| docs/reviews/browser-followup/ | 本报告＋证据 |

与冻结分支的文件重叠（提请集成者注意）：console-browser-regression.js 同时被 qwen38mvb4/browser-flake-windows-v2（415b9bf5，冻结）改动（零组格超时＋waitForFunction、legacy-group-deadlock 刀复活）。本单只动格1/格2/格3，未动零组格与刀区；两分支 hunks不相交，预期可干净合并。r15 原格（r15-session-history-browser-regression.js）本单未动（冻结分支辖域）。

## 二 逐格定因（证据：evidence/，插桩副本在基锚树内运行、不入库）

### 2.1 console 格3「shows group load and network action failures beside their controls」
- 红形（Phase A 8 样本 6 红）：4× waitForFunction ~29.5s 超时（conditionDeadline 耗尽）＋2×「没有自己的 Agent」断言红。
- 转储实锤（console-red-a1c2/a2c2-*.json）：失败时页内 **groupFetchTimeoutMs=20**（测试在 timeout 段压下的旋钮，用毕未恢复），groupStatus/groupRetryFeedback=「编组加载失败: 请求超时」，groupLeader disabled=true。
- 根因：timeout 段把产品 fetch 超时压到 20ms 逼出挂起夹具的 AbortError；其后 empty/恢复段的健康 fetch 在负载下往返 >20ms ⇒ 被测试旋钮误 abort ⇒ 恢复段 leader 永不 enabled ⇒ 条件等待耗尽。产品默认 groupFetchTimeoutMs=8000（internal/m8/console_auth.go:792）。竞态在测试侧旋钮遗留，不在产品。
- 次要：共享 page 默认超时 1500ms 下的 click/fill 动作性等待，负载下可先红（显式上界修复）。

### 2.2 console 格1「distinguishes platform and Agent load failures from true empty states」
- 红形（A2 定向 12 样本 1 红，g1-12）：strictEqual 期望「工作台加载中…」实得「工作台加载失败: 请求超时」。
- 转储实锤（console-red-g1-12-*.json）：failedPage **consoleFetchTimeoutMs=100**，platformStatus 已被 abort 改写、agentsStatus 仍「Agent 列表加载中…」。
- 根因甲（实锤）：100ms abort 旋钮下，「两次 Playwright click → 两次 textContent 读」共 4 次 CDP 往返，负载下往返本身可超过 100ms ⇒ 读到已被 abort 改写的文案。竞态在测试侧读取时序，不在产品。
- 根因乙（同格3机制，格3已实锤）：consoleFetchTimeoutMs=100 用毕未恢复 ⇒ 后续 empty/成功段健康 fetch 负载下与 100ms abort 竞态（产品默认 8000，console_auth.go:792）。
- 根因丙：stalePage/failedPage 默认超时 1500ms 短于产品 fetch 超时 8000ms（零组格前单已定因同形）。

### 2.3 console 格2「runs the zero-group human workflow without hidden actions or long tasks」
- 本单负载协议下未捕获红（Phase A 8 样本＋A2 定向 12 样本＋Phase C 12/12 全绿，累计 32 样本）。
- 前单（browser-flake-windows REPORT §四）在更重复合负载（3x 全套件并行＋4x busy-spin）下捕获红形 deepEqual＝__dwLongTasks 非空（longtask>200ms 判据）。
- 代码读定因：auditPage 默认超时 1500ms 短于产品 fetch 超时 8000ms（丙类，已修）；longtask>200ms 判据在 CPU 超订下会把被抢占拉长的正常任务记为 longtask（与 flood_stress 同类的负载本性敏感，见§六）。本单未改该断言（派单约束），如实披露：本单协议下 N 样本未触发，更重负载下仍有触发可能，建议与 flood 一并统筹「需静默条件」标注（由组长定）。

### 2.4 console_specs S-7 复制链接反馈（console-specs-browser-regression.js:989 一带）
- 红形（前单全量 runner 瞬态）：rowStatus 实得 '' 期望 'link address copied'。
- 机制定因（代码读＋定点 widen 窗口诊断转储 ×3 实锤：evidence/phaseA2/specs-red-diag350-01/02/03-*.json）：show-spec-list 的 onclick 触发 fire-and-forget refreshSpecs（internal/m8/console_specs_page.go:235）；refreshSpecs 无 generation 守卫、fetch 晚归时 list.replaceChildren() 整行重建（console_specs_page.go:140 一带），把已落复制反馈的旧行 detach。负载下 fetch 续体迟于「点复制→400ms 固定窗→读 DOM」⇒ 读到新行的空 status。剪贴板本身是 addInitScript mock（writeText 同步记 __copiedCode），**与剪贴板权限无关**。
- 次生根因：两处 5200ms 固定窗对 product 5000ms 冒泡定时器（console_specs_page.go:124 _copyTimer）负载迟发余量仅 ~650ms，不足。
- 诊断转储读数（×3 逐字同形）：断言＝派单所报 :989（rowStatus '' vs 'link address copied'）；copied=正确只读链接（复制本身成功、mock 剪贴板已收到文本）；firstRowStatus=''、firstRowStatusHidden=true（新行）；**clickedRowConnected=false**（被点行已被 replaceChildren detach）；rowCount=20；marks 时序示 copy-click 后约 350ms 延迟刷新恰落在点击与读取之间。
- 自然负载样本：Phase A 4＋A2 8＋Phase C 28（22 轮到达 S-7 全绿、6 轮被范围外前段阻塞）累计 40 样本 S-7 0 红（窗口窄、命中率低；前单全量 runner 曾捕获自然红 :989 ''）。diag350 以 S-7 定点武装的 350ms 夹具延迟人工 widen 窗口作机制证明，与自然样本分列。
- 诊断伪影披露：首轮 EVIDENCE_LIST_DELAY_MS=600 为全局延迟，误伤前段（A2-diag-01/02 红于 :409 '没有自己的在线 Agent'，非 S-7）⇒ 作废，改为 /__dw_arm_list_delay 标记后仅延迟 S-7 触发的列表 GET（diag350）。另 A2-s2-6 自然红在范围外前段（#new-title click 2500ms 超时，:534），非 S-7，披露不修。

### 2.5 r15 覆盖缺口（派单第 1 项）
- 冻结时序窗修法（browser-flake-windows-v2）以 REPLAY_QUIET_MS/HARD=60000 撑开回放窗，手势走「重放窗内上翻」逃生口（finishTimelineReplay(false)）；「重放自然收尾（finishTimelineReplay(true)＋snapTimelineToBottomSettled 三拍）之后用户上翻释放跟随」无格直接覆盖。
- 新格走产品默认旋钮（QUIET 250/HARD 1000）真实自然收尾，等可观察条件（replayInProgress=false＋缓冲排空＋无补滚帧＋fonts loaded＋贴底）＋三拍可证排空（观察点后登记双 rAF＋300ms>250ms 定时器：rAF 按登记序、定时器按截止序 ⇒ 产品三拍必已执行完），再真手势 -600 上翻，断言释放（timelineAtBottom=false、gap>160）且后续 live 事件不抢回（top ±2px、gap>160）。

## 三 修法（断言一字不改；不删格、不加重试循环）

- 格1：stalePage/failedPage setDefaultTimeout 1500→CONDITION_WAIT_MS(30000)（≫产品 8000ms＋负载余量，零组格前例同值同理由）；超时段「点击＋读加载文案」改同一 evaluate 内同步执行（单线程内无定时器可插入，消除 100ms 旋钮下的 CDP 往返竞态；断言值不变）；consoleFetchTimeoutMs 超时段用毕恢复 8000。
- 格2：auditPage setDefaultTimeout 1500→CONDITION_WAIT_MS。longtask 断言未动（§2.3 披露）。
- 格3：retryGroups×5、createGroup×2、groupAddOpen、fill 显式 {timeout: CONDITION_WAIT_MS}（共享 page 默认 1500 不动，避免影响他格）；groupFetchTimeoutMs timeout 段用毕恢复 8000；expectText 既有条件等待不动。
- S-7：先等 show-spec-list 触发的那次列表响应归位（waitForResponse 显式 30000 上界），再 await 一轮完整 refreshSpecs（无 generation 守卫、无周期刷新 ⇒ 其 replaceChildren 是最后一次行重建）⇒ 行稳定后点复制；400ms 固定窗→中性条件等待（反馈节点 textContent!==''，成功/失败都写文本、不预设结果值，随后原断言一字不改）；两处 5200ms→条件等待（节点隐藏）上界 5000+30000（产品定时器 5000ms＋负载余量）；详情侧 400ms 同改中性条件等待。
- 新补格：上界理由见件内注释（CONDITION_WAIT_MS=30000≫产品回放硬截止 1000ms＋三拍；RELEASE_WAIT_MS=3000 沿用冻结格原值：释放是手势后首个 scroll 事件内同步判定，scroll 步骤先于 rAF）。

## 四 判别力（刀；全部锚缺失即报错，无静默 replace）

| 刀 | 对象 | 预期 | 读数（静默） |
|---|---|---|---|
| r15new --mutate=release-only-in-replay（释放分支 gated replayInProgress） | 新补格 | 红（自然收尾后永不释放） | rc=2，红于 3000ms 释放等待（quiet-knife-r15new.log） |
| console --mutate=silent-platform-load | 格1 | 红 | rc=1，格1 FAIL waitFor 30000ms（平台文案永不出现） |
| console --mutate=silent-agent-load | 格1 | 红 | rc=1，格1 FAIL strictEqual（Agent 文案 ''） |
| console --mutate=preserve-stale-load-data | 格1 | 红 | rc=1，格1 FAIL「failed refresh must clear stale platform entries」 |
| console --mutate=silent-group-load | 格3 | 红 | rc=1，格3 FAIL「编组加载失败: HTTP 500」文案缺失 |
| console --mutate=silent-group-action-feedback（**本单新增**，mutate() 锚缺失即报错） | 格2/格3 | 红（审计「该编组已关闭添加」与格3 失败文案永不出现） | rc=1，格2 FAIL waitFor 30000ms＋格3 FAIL「fixture create denied」 |
| specs --mutate=no-copy-link-feedback（既有刀） | S-7 | 红（中性等待 30s 超时后断言 ''） | rc=1 |
| specs --mutate=owner-copy-sticky（既有刀，_copyTimer=0） | S-7 | 红（隐藏等待 35s 超时） | rc=1 |

新补格刀不伤冻结格路径：release-only-in-replay 只 gated else-if 释放分支，重放窗内逃生口（第一分支 finishTimelineReplay(false)）不受影响。

🔴 **既有刀锚漂移披露（非本单修法所致）**：--mutate=unhandled-group-action 在**基锚 501e5b8a 未修件上亦全绿**（quiet-knife-console-unhandled-group-action-BASE.log，20 PASS，rc=0）——其锚 `status.textContent='编组操作失败: …'` 已随产品 groupAction 重构为 groupFeedback（console_auth.go:797）而消失，silent html.replace 不改字节＝假绿（与冻结单 legacy-group-deadlock 漂移同类）。本单不修该刀（范围外），新增 silent-group-action-feedback 补格2/格3 判别力；建议组长另单复活 unhandled-group-action 锚（改锚至现行 groupAction catch 路径）。另 group-no-precheck／group-json-message-ignored 两刀红在相邻 malformed-names 格（非本单目标格），不计入本单判别力读数。

## 五 验证读数

- 负载协议：384 busy-spin（4x，96 核，DWLOAD4XA/C 标记、到期自灭）＋全量 runner 并行＋目标套件 loop；起止先报（开工报 133dd0094718、Phase A 先报 ecc1608838694e3ba66ae4ba4ef854bd/MCP、静默段＋Phase C 先报 9973b49a2d2b）。
- Phase A 取证：实起 02:23:03Z、实止约 02:44Z（上界 03:30Z 内）；Phase C：实起 03:07:19Z、实止 03:40:16Z（上界 05:10Z 内；静默段实起 02:44:44Z、实止约 03:02Z）。
- **负载全量 runner（4x＋目标 lane 并行）轮 1：43/46**，红三件全为已知负载敏感/冻结辖域：r15_session_history_pagination（原格 3s 窗，冻结 flake v2 辖域）、r15_session_history_mutations（既有，见下）、renderer_flood_stress（吞吐本性，§六）；目标套件 console_browser／console_specs_browser／r15_follow_release_after_replay 全绿。轮 2＝41/46：红五件＝r15_session_history_pagination／r15_session_history_mutations／renderer_flood_stress（同上三件）＋console_specs_browser（红于 S-6④ 任务拆解 deep-equal，**未达 S-7**，范围外前段同类）＋renderer_failure_dock_embedded（iframe frame 对象负载下未现，套件自报「装置坏了」；静默轮该件绿）。目标格逐格计数以专项 loop 为准（specs 专项 28 轮 S-7 22 绿/到达后 0 红）。
- **逐格 ≥10 绿计数（4x＋全量并行，Phase C）：格1＝12/12、格2＝12/12、格3＝12/12、新补格＝12/12、S-7＝17 绿（到达后 0 红；另 5 轮被范围外前段阻塞未达 S-7，见下披露）**（evidence/phaseC/＋summary.json；specs 加跑 13–28 轮因前段阻塞稀释到达率）。
- 基锚对照（同负载协议，/tmp/wt-anchor-501 未修件）：console ×4＝格3 两红（29.1s 超时＋'没有自己的 Agent' 断言）两绿、格1/格2/零组格四绿；specs ×4＝4/4 绿（S-7 自然窗口窄：本单基锚自然样本累计 16 轮 0 红——Phase A 4＋A2 8＋Phase C 4；前单全量 runner 曾捕获自然红 :989 ''；判别力由机制诊断承担：基锚 3/3 红 vs 修后到达即绿，见下）；另有 Phase A/A2 基锚样本（4x＋3 套件并行、无全量 runner）：格3 8 样本 6 红、格1 定向 12 样本 1 红、格2 20 样本 0 红、S-7 自然 12 样本 0 红（窗口窄）。
- **静默全量 runner（46 套件含新登记）：45/46，aggregate_rc=2**（evidence/quiet/quiet-full.log＋receipt）。目标套件全绿：console_browser=0（20 PASS）、console_specs_browser=0（assertions=220）、r15_follow_release_after_replay=0、renderer_flood_stress=0。
- 🔴 **唯一红＝r15_session_history_mutations（既有、非本单影响，不修）**：红形 drop_history_node_move／restore_live_eviction_during_paged_history「wrong assertion failed」。定因：mutations harness 以 RENDERER_OVERRIDE 子跑 r15 原格，子进程红于 **r15-session-history-browser-regression.js:96 的 3000ms 释放窗**（原格既有窗，冻结 browser-flake-windows-v2 辖域），先于刀目标断言死亡 ⇒ harness 判 wrong assertion。本单未动 mutations/原格/renderer 三件，分支与基锚 sha 逐字同（mutations ce74ff5ee8acc710171b6b20ff2ed4aea4571168bcee11d3dbacf0b37faa0594、renderer 14ffc39ea1ed61c1d230b7e3f3a810564daf05f61a7ec17cefde07ffc98eb6ef）。单跑对照（静默）：分支 3/3 红、基锚 1/2 红，同点 r15:96（evidence/quiet/quiet-mutations-*.log）⇒ 当前机况下高频的既有抖性（组长 00:54:02Z 已判「mutations 既有窗」归后续统筹；本派单四项未含该件）。集成冻结 flake v2（r15 原格 REPLAY 旋钮＋-600 修法）后预期稳。请组长裁是否另单修 mutations harness 的「wrong assertion」判法或并入 r61 集成后复验。
- 已知负载敏感干扰（非本单辖域）：零组格/r15 原格（冻结分支辖域，本单未修）与 flood_stress（§六）在负载全量内红，静默轮绿（flood 静默绿已验；零组格/r15 原格静默绿见前单与本单静默轮 console_browser 20 PASS）；逐格计数不受影响（harness 逐格记 PASS/FAIL 不中断）。
- 🔴 **specs 范围外前段负载敏感披露（不修）**：Phase C 协议下 console-specs 套件前段三处偶发红阻塞 S-7 到达——「没有自己的 Agent 时空态必须说话」（assist 空态，:409 一带）、「选择第二个协助 Agent 时请求必须携带第二个 id」（assist 选择）、「reload must retain the first owned-spec page」（翻页 reload，:622，0!==20）；22 轮中 5 轮被阻塞（C-specs-01/02/05 等，log 在 evidence/phaseC/）。形状与本单所修同类（固定窗/异步渲染负载竞态），但派单第 3 项只含 copy-link ⇒ 不修、不掩盖，建议组长并入后续统筹单。S-7 到达后 17/17 绿。
- **机制阳性对照（修后 vs 基锚，同 350ms 定点武装窗）**：基锚插桩 ×3 全红（clickedRowConnected=false）；修后插桩 ×3 中 2 绿（S-7 反馈/隐藏全断言过；第 3 轮被范围外前段 :622 阻塞未达 S-7）＋加跑 04 绿）⇒ 修法击败已证机制（修后到达 S-7 三轮全绿）。evidence/phaseC/C-fixed-diag-*。

## 六 renderer_flood_stress 评估与建议（派单第 4 项：不改阈值，建议由组长定）

- 事实：现行判据 X0＝「页面收齐最后一帧（lastSeq=sent+2）＋push_rate≥5000/s」；receive_rate 是读数不是判据（STRESS-DET 第 1 条，件首注释）。前单 2x 负载全量红＝X0 lastSeq 20000!==100002，读数 pushed=100000、push_rate=5994（达标）、receive_rate=4611、page_unresponsive=true、agents_gap_max_ms=17048 ⇒ CPU 超订下渲染器排空速率跟不上灌入速率，观察窗内未收齐。
- 定性：该格是吞吐压力断言，负载敏感是其本性——外部 CPU 饥饿改变的被测对象本身（排空能力），红反映环境而非产品回归；静默全量该格绿（前单与本单静默轮）。放宽阈值会掩盖真实排空回归（R0/S0 判别力依赖它），不建议。
- 建议（供组长裁定，本单未改 runner/该格）：
  1. 维持全量 runner 始终跑该格（不跳过，保覆盖）；
  2. 把「权威读数取自静默轮」写成该格的明示标注（SUITES 注释或 runner receipt 增 loadavg 读数；负载高于文档阈值时该格红归类为环境性，需静默轮复核后才算产品回归）；
  3. 若组长取 receipt 增 loadavg 方案，改动在 tools/run-browser-regressions.py（测试基建），可另小单或并入 r61 集成，本单不自行实施。
- 同理建议统筹：格2 的 longtask>200ms 判据（§2.3）与 r15 原格/零组格的既有 3s/1500ms 窗（冻结分支已修后者）——「需静默条件」标注宜统一口径。

## 七 边界与披露

- 本单未改产品/helper/VERSION/md-render-core；未跑 run-build-tests.sh（无产品/helper 改动；浏览器全量 runner 为本单相关门）。如组长要求 build 门可补跑。
- 插桩副本（console-instrumented-followup.js／console-specs-instrumented-followup.js）只在基锚工作树内运行取证，不入库；EVIDENCE_LIST_DELAY_MS 诊断旋钮仅用于机制演示（人工 widen 窗口），与自然负载样本分列。
- 零组格与 r15 原格的负载红属冻结分支（browser-flake-windows-v2）辖域，本单未修、不掩盖；负载全量 aggregate 红由此与 flood 贡献，静默轮为权威。
- helper .39 IPC 随 WS 连接代起止：本单出站遇 send.py ConnectionRefused 一次（确定性失败），改 MCP 通道安全重试成功，随后 send.py 自愈；详见 STATE 账。
- 空闲期派单丢失（session_unavailable）已报组长登记为 N-1 家族新变体实证（开工报 133dd0094718）。

## 八 复核入口

```
git fetch origin qwen38mvb4/browser-followup
# 新补格（基线绿／刀红）：
NODE_PATH=… node view-service/r15-follow-release-after-replay-regression.js
NODE_PATH=… node view-service/r15-follow-release-after-replay-regression.js --mutate=release-only-in-replay
# 目标格单跑：
node view-service/console-browser-regression.js --case='distinguishes platform'
node view-service/console-browser-regression.js --case='zero-group human workflow'
node view-service/console-browser-regression.js --case='shows group load'
node view-service/console-specs-browser-regression.js
# 刀：--mutate= 名见§四；全量：python3 tools/run-browser-regressions.py
```
证据原始件：evidence/（phaseA/phaseA2/phaseC/quiet/smoke＋summary.json）。
