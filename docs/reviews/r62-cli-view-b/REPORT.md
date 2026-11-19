# S-R62-CLI-VIEW-B 原型交件报告：View 终端风格第二渲染（Codex CLI 版式）

- 派单：组长 opus50tc2k.zzc 2026-11-18T19:58:20Z 补发原件（fc3ef5a0d8a5）；业主 rulings ㊿-补131（tip 86e13532）。
- 分支：`qwen38mvb4/r62-cli-view-b`；基：手机单冻结 tip `7d3035c6`（产品 `95e48084`，含 CONSOLE-SLIM）。
- 设计页：`docs/reviews/r62-cli-view-b/DESIGN.md`（tip `ab87b96fe` 交审，-letter `04efe202da73`）；本原型在 rebase 到 `7d3035c6` 后的同分支追加（设计三提交保留，远端旧 tip 需快进覆盖，见 §一）。
- 范围：driver 不动、Hub 不动；只 View/renderer 加性层＋新回归件＋夹具＋runner 登记。不改 VERSION、不推 main、不出发布件、不碰现网。

## 一、交付坐标

- 产品改动件：`view-service/renderer-canary-sse.html`（唯一产品件）。
- 新件：`view-service/renderer-cli-view-regression.js`（G1～G12＋G5b/G6b，K1～K12 刀；V1 追加窄屏关态初始/往返两态字节同一格）、`view-service/fixtures/cli-view-tool-items-0170.json`（校准夹具）。
- 登记件：`tools/run-browser-regressions.py`（SUITES＋OVERRIDE_ENV_NAMES）。
- 证据：`docs/reviews/r62-cli-view-b/evidence/`（calibration／runs／shots／conn）。
- 提交：merge `1e8e46934`（ab87b96fe＋7d3035c6，无冲突，docs 与产品件不相交）；产品＋测试 `852ec1796`（renderer 308 增 0 删＋套件＋夹具＋runner，4 件 1013 增）；docs 提交见交件信 tip。
- 合并前后内容同一核对：renderer／套件／runner／夹具四件 sha256 合并前后逐字同（400a5118…／f5ac290b…／8661052a…／acadbb75…）；全量 runner 测的工作树内容即最终提交内容。
- V1/V2 追加（组长 2026-11-18T23:15:25Z 复核 PASS 后小活）：测试提交只改 `renderer-cli-view-regression.js`（窄屏两视口初始关＋开→关往返后关态与基锚逐字节比对，＋K12 cli-view-toggle-residue 刀；**renderer 产品件零改动**，sha 与 852ec1796 逐字同）；docs 提交为本报告 §八＋v4 证据＋SUMS，tip 见交件信。

## 二、实现概要（加性双投影）

1. 结构：`#timeline-cli` 为 `#timeline` 的兄弟容器（默认 hidden）；全部 CSS  scoped 于 `body.cli-view-on`／`#timeline-cli`（暗/亮两套变量、pre-wrap＋overflow-wrap:anywhere、窄屏切换按钮 ≥40px）。状态栏上方新增 `#cliViewBtn`（▣）切换按钮——加性 chrome，见 §七披露。
2. 开关：`localStorage(dw-cli-view)`＋`BroadcastChannel`＋`storage` 事件三路（逐字仿主题先例，三段各 try/catch）；写入方自 apply；会话覆盖位 `window.CLI_VIEW_SESSION_OVERRIDE`（boolean）优先级最高，锁定时按钮置灰。默认关。
3. 投影：`rebuildTurnCLI(group)` 与 `renderUserCLI(entry)` 只读现管线同一份 `group.events`／输入卡 entry 数据（纯投影，不读主 DOM——G10 有确定性判别格）；七个钩子全部带 `cliViewOn()` 闸（rebuildTurn 尾、renderUser 失败防御分支、renderUser 建卡/更新尾、c1ApplyToEntry 尾、releaseTimelineNode、resetConversation），关态零副作用。
4. 合卡同步：G-MERGE-SAME 与 renderEvent T-074 两处「独立卡移入回合」的 `card.remove()` 后各加 CLI 节点同步移除（否则旧独立卡 CLI 行泄漏）；回合内用户行前缀经 `cliSourcePrefix(norm)` 与独立卡同源（同一 norm 可信判定，fail-closed），`retainCanonicalTrustedProvenance` 挂到 echo `_norm.prov` 后重建不丢前缀（G6b）。
5. 映射：› 用户（来源前缀＋阶段缀）／• assistant（markdown-lite：先转义再认 ```/`/** 三种内联，bypass md-render-core ⇒ 无 sha 回写）／• Ran|Edited|Running 工具行（完成后保留，└ details 摘要＋全文）／✗ 错误／Worked for 回合尾＋状态行（model/usage）。长文 >4000 字 ⇒ 1200 字摘要＋pager 行，全文点击时现拼 blob（懒拼装）。
6. 工具行形状（0.170 实证校准，见 §三）：`item.name` 优先于 `data.name`（dynamicToolCall 真名）；command 容错数组；输出读 `aggregatedOutput`（兼容 rollout `aggregated_output`）；fileChange 取 `changes` 映射首键为路径、多文件显示 N files、摘要列 change 类型。

## 三、版式校准（设计风险②落地）

探针：`evidence/calibration/appserver_capture.py` 直起真实 `codex app-server --stdio`（0.170.0-x86_64-unknown-linux-musl，不经 helper），隔离 HOME/CODEX_HOME，mock provider（`mock_provider_tools.py`，responses wire，tool 臂回 function_call，Codex 本地真跑 printf 无副作用命令；凭据 dummy，无真密钥）。

发现（原始捕获 `appserver-0170-green-capture.jsonl`）：
1. **live v2 通知是 camelCase**：`item.type="commandExecution"`、`processId`、`exitCode`、`aggregatedOutput`、command 为**串**（`/bin/bash -lc "…"`）。与 driver 过滤口径一致。
2. **rollout 序列化不同源**：本席 0.170.0 rollout `event_msg item_completed` 是 PascalCase（`CommandExecution`/`FileChange`）＋snake_case（`aggregated_output`/`process_id`）＋command **数组**。夹具两臂并收作容错对照（G5b）。
3. **输出字段是 `aggregatedOutput`**（不是 output）——原型已适配；未适配前工具摘要会空。
4. **fileChange 真形是 `changes` 映射**（path→{type,content}），无 path 字段——原型取 changes[0]；rollout 样本为 add 类信件件。
5. 0.170 工具集为 `exec_command`（cmd 串，PTY unified exec）等（`provider-tools-schema.json`）；无独立 shell/command 数组工具。
6. 沙箱臂：workspace-write 在本容器 bwrap 受限（`appserver-0170-bwrap-fail-capture.jsonl`，status failed/exitCode 1，输出为 bwrap 报错）——失败态形状亦入证据；绿样本用 danger-full-access（仅 printf，隔离 workdir）。
7. **未调研**：dynamicToolCall（MCP）无样本——保留 name 降级行；fileChange 的 live camelCase 形状由 rollout 样本＋driver 过滤口径推断（changes 字段名两源一致）。

## 四、回归与刀读数

单跑三连绿（18 格版 final 件，段 2026-11-18T22:33:47Z～22:38:27Z）：run10／run11／run12 均 rc0，G1～G12＋G5b/G6b 共 18 格全 PASS（`evidence/runs/cli-view-b-run10.log` 等）。此前 run4～run9 为迭代轮（含判别力修复前），日志并存不混计。

V1 追加后（22 格）单跑三连绿：run13／run14／run15 均 rc0、22 PASS／0 FAIL（段 2026-11-18T23:22:21Z 起，`evidence/runs/cli-view-b-run13.log` 等）。新增 4 格＝窄屏 390/360 各「初始关态字节同一」＋「开→关往返后关态字节同一」；G12 读数与 18 格版同（flood 16.7s／late-order 5.0s／peakEvents 20001≤20100／peakCliNodes 14／storm 55 次 mismatch 0）。

删保护刀阵（V1 追加后 final 件全量重跑，`evidence/runs/knives-v4/summary.txt`，12/12 红在判据文案；18 格版 11 刀原读数存 `evidence/runs/knives/`，renderer 产品件零改动、判据不变）：

| 刀 | rc | 首红判据 |
| --- | --- | --- |
| K1 cli-view-offstate-add | 2 | G1 关态不得有 CLI 行（G2 字节同一亦红） |
| K2 cli-view-toggle-wire | 2 | G3 开后 body 须带 cli-view-on |
| K3 cli-view-persist | 2 | G3 localStorage 须 on（G4 持久化值须 on 亦红） |
| K4 cli-view-trust-bypass | 2 | G6 不可信来源不得挂前缀（fail-closed） |
| K5 cli-view-fail-newline | 2 | G7 失败不得另起错误行 |
| K6 cli-view-longtext-full | 2 | G8 >4000 字须出 pager 行 |
| K7 cli-view-nowrap | 2 | G9 CLI 容器自身横向溢出须 0 |
| K8 cli-view-dom-depend | 2 | G10 投影不得依赖主 DOM（主 DOM 清空后仍须与事件态同） |
| K9 cli-view-override-flip | 2 | G11 override=true 须有 CLI 投影（钩子与开关同向） |
| K10 cli-view-threshold | 2 | 套件自检：SOFT_CAP 与 flood 套件漂移（阈值放宽刀） |
| K11 cli-view-tool-drop | 2 | G5 commandExecution 须 • Ran <command> |
| K12 cli-view-toggle-residue | 2 | G9 窄屏 390 往返后关态 innerHTML 与基锚字节不同（K12 切换残留须红在这里） |

K12 判别力专杀往返格：刀在 applyCliView 开态分支往主 `#timeline` 追加注释节点（关态分支不动）⇒ K12 下 G1／G2／两视口「初始关态」格均 PASS，仅两视口「开→关往返后」格红（390/360 各一，`knives-v4/knife-cli-view-toggle-residue.log`）；注释节点不渲染像素，PC 像素格不受扰。初始关态格防「加性层破坏现 DOM」，往返格防「切换残留」，两态各有判据。

判别力修复披露（首轮 knives-round1 假绿三把，已修并重跑全刀阵）：
- K7 首轮假绿：fixture 无超长命令，nowrap 不溢出。修：fixtureA commandExecution 加 300 字符长参数（anywhere 换行则溢出 0，nowrap 则容器 scrollWidth 超界）。
- K8 首轮假绿：splitTurnGroupAtHubSequence 后 turnGroups 指向空尾段，原格清错节点。修：G10 清 t1 全部段节点后再重建比对。
- K9 首轮假绿：override 页未注事件，翻转只影响钩子不影响 boot apply。修：G11 两页注 fixtureA，断言开态有投影／关态零 DOM（钩子与开关同向）。

G12 读数（run12，阈值与 flood 本体逐字同，K10 自检锚定）：

| 场景 | 推入耗时 | lastSeq | peakEvents | finalEvents | peakNodes | peakCliNodes | pager | 界 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| flood 100000 | 16.7s | 100002 | 20001 | 10020 | 55 | 14 | 1 | 20100 |
| late-order 30000 | 5.0s | 30002 | 20001 | 10006 | 55 | 14 | 1 | 20100 |
| toggle-storm | 55 次开合 | — | mismatch 0 | finalEvents 6002 | maxNodeDelta 42 | — | — | 20100 |

全文收敛判在数据层（软上限 compactTurnGroup 合并不丢文）：dataFull 与逐条拼接逐字同；DOM 按长文策略渲前 1200 字摘要＋pager（G8 覆盖点开全文逐字）。

负载纪律：门段串行、无人为加负载。段前 22:17:32Z／段后 22:38:27Z conn 探针：helper PID 555221，hub TCP inode 1222944048 前后相同 ⇒ 重连 0（`evidence/conn/cli-view-b-conn-knives2-before.json`／`-after.json`）。

V1/V2 追加段（v4，12 刀＋三绿＋全量 runner 串行）：段前 2026-11-18T23:22:21Z／段后 23:59:44Z conn 探针 inode 1222944048 逐字同 ⇒ 重连 0（`evidence/conn/cli-view-b-conn-v4-before.json`／`-after.json`）；无人为加负载。

## 五、截图（两视口＋PC，关/开态）

| 件 | sha256 |
| --- | --- |
| evidence/shots/pc-1440-off.png | a382fb9e05aa2f24c91b137c8e8bc80ada4bb21a3b36129e1bdec86dccf7aec2 |
| evidence/shots/pc-1440-on.png | 98695dc89d3f432ab3ca9a6cfb7981fb55e3f1fc624d59129bb612f5e5ac8f77 |
| evidence/shots/mobile-390-on.png | ad6d4b6609e23fcdec845bec8817dadd7f2575d6237ebaa5fe16fe07b7aff4e5 |
| evidence/shots/mobile-390-off.png | 52317613e1804d713ee1610689ae183bb8168cad2821e6f8ae993e99608af451 |
| evidence/shots/mobile-360-on.png | 4c4aabc4307b6c72f59e4a943b82b5115bee4fabbeb3667374c014ae59532574 |
| evidence/shots/mobile-360-off.png | a8065612235e309615be599ece78a4fce3e853a9d6a6312157b8cd3f0d048ebb |
| evidence/shots/pc-1440-tool-calibration.png | 504d0f00a407dd9ed4f35d641899d5f173870e37af994f3ce5964efa80bb6d25 |

设计期 TUI 完成态参照（tmux 帧）在 `evidence/layout/d-0159b/`、`d-0170b/`（设计提交 c9bac95eb 已入）；原型开态截图与之并排：› 用户行／• 输出行／工具行／状态行四区对应。

## 六、runner 登记与全量

- 登记：SUITES 增 `renderer_cli_view`（renderer_flood_stress 之后）；OVERRIDE_ENV_NAMES 增 `CLI_VIEW_SHOT_DIR`（MUTATION／STRESS_DEADLINE_S 已在册）。`--selftest-env-coverage` rc0。
- 登记前单跑三连绿（run10/11/12，§四）；登记后全量 runner 一轮（2026-11-18T22:40:43Z～22:56:01Z，`evidence/runs/fullrunner.log`，receipt commit_sha=4bca74f00＝合并前本地 tip，测的是工作树内容，与最终提交内容逐字同，见 §一 sha 核对）：**52 套 51 绿 1 红**，aggregate_rc=2。
- 逐套 rc 见 receipt（DW_BROWSER_RECEIPT_V1）：renderer_cli_view rc0；renderer_provenance_icon（8cabb351 来源图标）rc0；renderer_retry_role（r60 ④ 重发）rc0；console_mobile rc0；renderer_in_console_iframe rc0；renderer_failure_dock_embedded rc0；renderer_flood_stress rc0；md_render_core_sync／parity rc0（bypass 未触 sha）。
- 唯一红：`r15_session_history_mutations` rc1，case restore_live_eviction_during_paged_history，红在 r15-session-history-browser-regression.js:96 的 3000ms 窗（page.waitForFunction Timeout）。两锚对照：本树单跑同 case 红（r15-mutations-current-single.log）；基锚 7d3035c6 renderer（无 CLI 层，临时副本）单跑亦红、case drop_scroll_anchor_correction 同一 3000ms 窗（r15-mutations-base7d3035c6-single.log）⇒ 既有时序窗抖性，非本单回归。415b9bf5（时序窗修复）尚未合入本基，归既定继承族／后续统筹单，不在本单修。
- V1 追加后全量 runner 再一轮（v4，2026-11-18T23:47Z～23:59:43Z，`evidence/runs/fullrunner-v4.log`＋`fullrunner-v4-receipt.txt`，receipt commit_sha=3491f94ae＝当时 HEAD，测的工作树内容即 V1 测试提交内容，renderer 与 852ec1796 逐字同）：**52 套 50 绿 2 红**，aggregate_rc=2。renderer_cli_view rc0（22 格）；renderer_provenance_icon／renderer_retry_role／console_mobile／renderer_in_console_iframe／renderer_failure_dock_embedded／renderer_flood_stress／md_render_core_sync／parity／console_browser 均 rc0。
- v4 两红均为 r15 族同一 3000ms 窗（r15-session-history-browser-regression.js:96）：`r15_session_history_pagination` rc2（基线轮 page.waitForFunction Timeout）；`r15_session_history_mutations` rc1（mutant drop_scroll_anchor_correction 期望红在判据、实红在同窗 Timeout＝「wrong assertion failed」）。两锚单跑对照见 §六.1。415b9bf5 未合入本基，归既定继承族（组长 M2 复核全量亦曾 r15 族两红），不在本单修。

### 六.1 r15 族两红两锚单跑对照（v4）

- 方法：r15 两测试件本分支相对 7d3035c6 零改动（git diff 空）；基锚对照用临时副本（view-service 全量拷贝、仅把 renderer-canary-sse.html 换为 `git show 7d3035c6:` 版，sha256 c1f299d73fac…），唯一变量＝renderer。
- 单跑（2026-11-19T00:01:53Z～00:02:09Z）：本树 pagination rc2（r15:96 同窗 Timeout）、mutations rc0（三刀全红在判据）；基锚 pagination rc0、mutations rc0（三刀全红在判据）。
- pagination 两锚各五连（00:03:11Z～00:03:46Z）：本树 **2/2/0/0/0**、基锚 **2/2/0/0/0**——逐位相同，四次红全部是 r15-session-history-browser-regression.js:96 的 3000ms 窗 ⇒ 既有时序窗抖性（与本机历史「基锚原 renderer 0/0/2/0/2」读数同族），非本单 CLI 层回归。
- mutations 全量负载下红（drop_scroll_anchor_correction「wrong assertion failed」，实红在同窗 Timeout）、两锚单跑均绿 ⇒ 负载敏感，同族。
- 对照段 conn 前后 inode 1222944048 逐字同 ⇒ 重连 0。日志：`evidence/runs/r15-v4-compare/`（单跑 4＋五连 10＋rc＋conn）。
- 处置：照既定裁定继承（415b9bf5 时序窗修复未合入本基，r62 集成后验证），不在本单修。

## 七、披露与已知边界

1. **加性 chrome（已裁）**：▣ 切换按钮保留可见。组长 2026-11-18T23:15:25Z 复核裁定（逐字）：「状态栏的『▣』切换按钮保留可见——业主要能自己切换，按钮是开关本体。『关态与现网逐字节一致』的范围定为：时间线区域 innerHTML＋body class＋时间线元素截图；整页因多一个按钮不逐字节同，接受，本席向业主说明。不必藏进设置菜单。」G2/G9 字节同一与像素同一按此 scope；V1 另补窄屏 390/360 两视口初始关＋开→关往返后两态比对（§四）。
2. mention-reply 卡、reasoning 行、独立 error 卡（schema_conflict 等）不进 CLI 投影（映射表 scope 外，设计 §二）；回合内 error 有 ✗ 行。
3. N-2 offturn 折叠条：本基（7d3035c6）不含 N-2 代码；契约注释＋跳过标记登记，N-2 合入后激活（设计 §八）。
4. md-render-core bypass（markdown-lite）⇒ 无 console_specs_assets_test.go sha 回写；若后续改走 md-render-core 须按设计 §六 流程回写。
5. K5 刀锚在 G7 实际所经 c1ApplyToEntry 阶段更新钩子；renderUser 的 viaDlv 失败防御分支同语义保护，无单独格（该分支在正常索引命中时不可达，T-074 防御余量）。
6. r15 两格／console_browser 零组格等既有负载抖按既定裁定继承（415b9bf5／后续统筹单），不在本单修。
7. 校准探针的 dynamicToolCall 与 fileChange live 形状缺口见 §三.7。
8. **📎 附件行缺口（V2 盘点新发现，已裁本期补）**：设计 §二 承诺用户行/正文块下挂 `📎 <文件名>` 行，原型未实现（rebuildTurnCLI/renderUserCLI 无附件处理，主时间线 attachFileRenders 产物未投影）；此前披露 1～7 未含此项。组长 2026-11-19T00:09:16Z 裁定「本期补」，V3 已实现并加 G13 格＋K13 刀，见 §十。

## 八、与原生 Codex CLI 的差异（V2，按组长 2026-11-18T23:15:25Z 复核令增）

对照对象：原生 Codex CLI 0.170.0 TUI（设计期 tmux 帧参照 `evidence/layout/d-0170b/`）。分类：A＝无数据源/范围外；B＝有意差异（设计已定或加性增强）；C＝形状未校准（降级容错）。逐条写「不画什么、为什么、以后能否补」。

1. **reasoning 行不画**（A）。原生 TUI 有 reasoning 摘要行；driver 根本不转发 reasoning item（`tools/sessionhelper/sessionhelper/codexDriver.py:1883` 注释逐字「userMessage / reasoning / 其他未知 item 类型都不作为独立事件暴露」），View 侧无数据源。以后能否补：需 driver/Hub 暴露 reasoning 事件（本单不动 driver/Hub，范围外）；暴露后 View 可加暗色 • 行，纯加性。
2. **mention-reply 卡不投影**（B）。跨席镜像/提及回复卡不产生 CLI 行（设计 §二 明定：非本席回合语义）；原生 CLI 无跨席消息概念，无对应版式。以后能否补：可映射为分隔行＋来源前缀，需先裁版式。
3. **独立 error 卡不投影**（B）。schema_conflict 等系统级独立 error 卡不进投影；原型只画回合内 error 的 ✗ 行。设计映射表原写独立 error→✗ 行，原型未实现（原披露 2，组长 23:15:25Z 已接受）。以后能否补：可加 ✗ 诊断行，纯 View 加性。
4. **📎 附件行（V3 已补，B＝加性增强）**。设计 §二 承诺用户行/正文块下挂 `📎 <文件名>` 行（链接可点、复用 attachFileRenders 产物语义）；原型期遗漏（V2 盘点新发现，§七.8），组长 2026-11-19T00:09:16Z 裁定本期补，V3 已实现（§十）。与原生差异：原生 CLI 无附件概念，📎 行为 DingWei 特有加性增强；主视图的内联图让位/玩家卡不复刻，CLI 只挂名字链接行。
5. **dynamicToolCall 无样本降级**（C）。校准未获 MCP 工具真样本（§三.7），工具行降级为 `item.name||data.name` 真名行，无参数/输出摘要。以后能否补：取 MCP 真样本校准形状后补摘要，夹具两臂机制已留位。
6. **fileChange live 形状为推断**（C）。changes 字段名 rollout/live 两源一致，camelCase 外形由 commandExecution 同源推断（§三.7）；0.170 工具集无原生 apply_patch，实证需 MCP 或旧版工具。以后能否补：取真 fileChange live 样本校准；现容错（changes 首键路径、多文件 N files）已覆盖两源已知形状。
7. **工具完成保留行 vs 原生折叠**（B，有意差异）。原生 TUI 完成后把工具输出折叠为可展开摘要；原型保留 • Ran/Edited 行＋└ details 摘要＋全文（设计 §二，G5 格保护），保证时间线可回溯。不属缺口。
8. **长文 1200 摘要＋pager vs 原生全量滚动**（B，有意差异）。原生 TUI 全文渲染靠滚动；原型 >4000 字 ⇒ 1200 字摘要＋pager 行、全文点击现拼（G8/G12 flood 性能判据所需）。不属缺口。
9. **状态行快捷键提示仅版式参照**（B）。`← for agents · ? for shortcuts` 只是原生状态行版式复刻（renderer 状态行第二行），View 无对应快捷键功能。以后能否补：若要真快捷键另单（涉及交互层，非纯投影）。
10. **用户行阶段缀为 DingWei 特有**（B，加性增强）。`[已送达/失败:…]` 阶段缀来自 DingWei ACK 生命周期，原生 CLI 无此概念；有意加性（G7 格保护失败不另起行）。
11. **N-2 offturn 折叠条契约预留**（A）。本基 7d3035c6 不含 N-2 代码；契约注释＋跳过标记已登记，N-2（8ae6b083）合入后激活（设计 §八）。原生 CLI 无回合外输出概念，届时捷径为 DingWei 特有折叠条。
12. **子 Agent 无独立事件种**（A）。renderer 事件流无子 Agent 独立事件种（subagent/sub_agent/agent_task/collab grep 0 命中，设计 §二 已定）；父席的子 Agent 操作以 MCP tool_call 行（dw_* 工具）呈现，与原生 TUI 的子代理树/独立任务行不同。以后能否补：需 driver 暴露子 Agent 生命周期事件（范围外）；暴露后可映射独立任务行。

## 九、边界遵守

- 未改 VERSION；未推 main；未跑 build.sh 出发布件；未碰预发/现网；driver/Hub/helper 零改动。
- 校准探针隔离 HOME/CODEX_HOME，mock 凭据 dummy；未复用 donor 凭据（本单不需要真 provider）；日志无密钥。
- 负载纪律：门段串行、无人为加负载；段前后 conn 探针读数入 evidence/conn/（helper 重连 >2 即停段报——本段未触发）。

## 十、V3 📎 附件行（组长 2026-11-19T00:09:16Z 裁定「本期补」）

裁定逐字：「纯 View 加性、关态不受影响；在同一分支追加：用户行与正文块下挂『📎 文件名』行（链接可点），语义复用 attachFileRenders 产物，不重建上传下载；加一格（开态有附件时出现、关态无变化）＋一刀（去掉附件挂载必红）；V1 关态逐字节格须仍绿。」

### 十.1 实现（renderer-canary-sse.html，纯加性）

- 新 helper `cliAttachmentUrls(cardEl)`：只读 attachFileRenders 的挂载产物 `cardEl._fileHost` 当前子元素的 `data-file-url` 集（有序去重）。**不重扫正文、不重建 HEAD 探测/上传下载**——URL 集天然继承 attachFileRenders 的全部语义：本站 /f/ 过滤、尾标点剥除、流式 openTail 暂缓、长前缀让位、内联图让位、404 兜底保留。
- 新 helper `cliAttachLine(url, cardEl)`：已解析的附件卡直接取其 `.attachment-name` 链接（名字与 href 与主视图同名同源）；未解析槽位回退 `fileAttachmentShell` 同一名字源（uploadedFileHints → URL 末段）＋ `localFileUrl`。两处名字源一致 ⇒ 槽位→卡异步解析不改变 📎 行内容。
- `rebuildTurnCLI`：回合用户行下挂正文含该 URL 的 📎 行；正文块（含长文 pager 之后）挂其余 📎 行——长文摘要外的附件不丢（host 来自全文扫描）。无正文块的回合全部挂用户行下，不丢行。
- `renderUserCLI`：独立输入卡行尾 `insertAdjacentHTML` 追加该卡全部 📎 行（阶段更新/失败路径重渲时随 innerHTML 重置重挂，不重复）。
- CSS：`.cli-attach` 暗色行＋链接色；窄屏 ≤768 链接 inline-block min-height 40px（与 pager 按钮同例）。
- 关态零副作用：所有新路径都在 rebuildTurnCLI/renderUserCLI 内（本身带 cliViewOn() 闸），主时间线/attachFileRenders/DW-MD-CORE 生成段零改动 ⇒ 无 md-render-core sha 回写。
- 已知窗（披露）：📎 集随投影重建刷新；主视图附件的异步让位/补挂（内联图加载失败后补卡）发生在两次重建之间时，CLI 行至下次重建才同步。正文 URL 文本在 CLI 恒可见（cliMarkdownLite 不画图），不丢信息。

### 十.2 回归与刀（renderer-cli-view-regression.js）

- 新格 G13（一格覆盖裁定三要求）：夹具含独立输入卡附件、回合用户行附件、回合正文附件、长文（4500 字）尾部附件共 4 条 /f/ URL（HEAD/GET 路由替身）。断言：① 关态 `.cli-attach` 计 0 且 #timeline innerHTML＋body class 与基锚 7d3035c6 逐字节同（附件产物不被加性层改动）；② 开态恰 4 条「📎 文件名」行、href 指向文件本体、target=_blank；③ 用户行 📎 紧跟用户行、正文 📎 紧跟正文块、长文 📎 紧跟 pager（摘要外不丢）、独立卡下挂 📎；④ 开→关往返后关态仍与基锚字节同（防附件切换残留）。
- 🔴 夹具同源教训（首版实测踩中，已修）：/f/ 直链按 isOwnFileUrl 对 location.origin 判定。首版基页另起服务器（不同 port）⇒ 基页拒收全部夹具 URL、附件产物为空，G13 等待超时（run16-origin-fail.log 保留披露）。修为单服务器双路径（当前版根路径、基锚 /base/）保同源；两路径均无 /view/ 前缀 ⇒ apiBase 为空、端点一致；localFileUrl 无 canonical origin 时原样返回 ⇒ 两页 href 逐字同，字节同一有判别力。
- 新刀 K13 `cli-view-attach-drop`：`cliAttachmentUrls` 的 host 遍历改空集（去掉附件挂载读）⇒ G13 开态计数/文案断言红（非超时红：关态附件等待不受 K13 影响，先过等待再红在判据）。锚唯一性已核（现有 11 刀锚＋K13 锚各恰 1 命中）。
- 既有 G2/G9（V1 窄屏初始关＋往返）字节同一格不动、须仍绿。

### 十.3 门读数（v5，2026-11-19T01:02:12Z～01:48:46Z，串行无加负载）

- 单跑三绿：run16／run17／run18 均 rc0、23 PASS（G1～G13；原 22 格＋G13）。G12 读数与原型逐字同族：flood peakEvents=20001≤bound 20100、late-order 同、toggle-storm 55 次 0 mismatch ⇒ 📎 层无性能回归（夹具无 /f/ URL 时 host 扫描零成本）。
- 🔴 首版假启动披露：run16-origin-fail.log（rc2）——G13 基页另起服务器致跨 origin 拒收夹具 URL、等待超时；修法与教训见 §十.2。该轮作废不计三绿，保留披露。
- 刀 13/13 红在判据（evidence/runs/v5-attach/knife-*.log＋summary.txt）：原 12 刀首红判据与 v4 同族；新刀 K13 cli-view-attach-drop **外科手术式**：22 PASS＋仅 G13 红，文案「G13 开态须出现 4 条 📎 附件行（实 0；K13 去挂载须红在这里）」——红在计数断言、非超时（关态附件等待不受 K13 影响，先过等待再红判据）。
- V1 关态逐字节格仍绿：G2（PC）＋G9（390/360 初始关＋开→关往返）＋G9 PC 像素同一，三绿内全 PASS；G13 另含附件夹具的关态字节同一＋往返同一。
- 全量 runner（不选格）：52 套 **50 绿 2 红**，aggregate_rc=2。两红均 r15 继承族同窗：pagination rc2（r15:96 Timeout 3000ms）、mutations rc1（drop_history_node_move「wrong assertion failed」，实红同窗 r15:96）。本分支 r15 两件对基锚 7d3035c6 git diff 为空、415b9bf5 未合入 ⇒ 照既定裁定记名继承，r62 集成后验证。renderer_cli_view 在全量内 rc0；来源图标／重发／mobile／iframe／failure dock／flood／md-render-core／console_browser 等关键套全 rc0。
- conn：段前 01:02:12Z／段后 01:48:46Z，helper PID 555221、WS inode 1222944048 逐字同、ipc_connect_ex=0 ⇒ 重连 0，未触停段线。
- 截图：evidence/runs/v5-attach/shots/（两视口关/开＋PC 关/开＋工具校准＋**pc-1440-attach-on.png** sha256=7561faf2ec32f2534a03928d746b55a2c99a90c2d474e0b95c4b71a040ae1d4e）。📎 字符在 headless 截图呈方框（本机无 emoji 字体），DOM textContent 逐字「📎 文件名」、断言按文本判；真实桌面/手机浏览器有 emoji 字体正常显示。
- renderer sha256=09cf2e9869e8229a91ea90d6d635f24a53d2555d96dfcc111a72c67cf525c84a；对 95c97d616 diff **31 增 0 删**（纯加性）；套件 108 增 2 删（两删为 G1～G12→G1～G13 题头/尾注）。
- 跑法脚本与调试探针入 evidence/runs/v5-attach/scripts/（baseline/baseline2/gates＋g13-probe/g13-probe2，后者复现跨 origin 假启动）。
