# S-R62-CLI-VIEW-B 原型交件报告：View 终端风格第二渲染（Codex CLI 版式）

- 派单：组长 opus50tc2k.zzc 2026-11-18T19:58:20Z 补发原件（fc3ef5a0d8a5）；业主 rulings ㊿-补131（tip 86e13532）。
- 分支：`qwen38mvb4/r62-cli-view-b`；基：手机单冻结 tip `7d3035c6`（产品 `95e48084`，含 CONSOLE-SLIM）。
- 设计页：`docs/reviews/r62-cli-view-b/DESIGN.md`（tip `ab87b96fe` 交审，-letter `04efe202da73`）；本原型在 rebase 到 `7d3035c6` 后的同分支追加（设计三提交保留，远端旧 tip 需快进覆盖，见 §一）。
- 范围：driver 不动、Hub 不动；只 View/renderer 加性层＋新回归件＋夹具＋runner 登记。不改 VERSION、不推 main、不出发布件、不碰现网。

## 一、交付坐标

- 产品改动件：`view-service/renderer-canary-sse.html`（唯一产品件）。
- 新件：`view-service/renderer-cli-view-regression.js`（G1～G12＋G5b/G6b，K1～K11 刀）、`view-service/fixtures/cli-view-tool-items-0170.json`（校准夹具）。
- 登记件：`tools/run-browser-regressions.py`（SUITES＋OVERRIDE_ENV_NAMES）。
- 证据：`docs/reviews/r62-cli-view-b/evidence/`（calibration／runs／shots／conn）。
- 提交：merge `1e8e46934`（ab87b96fe＋7d3035c6，无冲突，docs 与产品件不相交）；产品＋测试 `852ec1796`（renderer 308 增 0 删＋套件＋夹具＋runner，4 件 1013 增）；docs 提交见交件信 tip。
- 合并前后内容同一核对：renderer／套件／runner／夹具四件 sha256 合并前后逐字同（400a5118…／f5ac290b…／8661052a…／acadbb75…）；全量 runner 测的工作树内容即最终提交内容。

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

单跑三连绿（final 件，段 2026-11-18T22:33:47Z～22:38:27Z）：run10／run11／run12 均 rc0，G1～G12＋G5b/G6b 共 18 格全 PASS（`evidence/runs/run10.log` 等）。此前 run4～run9 为迭代轮（含判别力修复前），日志并存不混计。

删保护刀阵（final 件全量重跑，`evidence/runs/knives/summary.txt`，11/11 红在判据文案）：

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

负载纪律：门段串行、无人为加负载。段前 22:17:32Z／段后 22:38:27Z conn 探针：helper PID 555221，hub TCP inode 1222944048 前后相同 ⇒ 重连 0（`evidence/conn/conn-knives2-before.json`／`-after.json`）。

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

## 七、披露与已知边界

1. **加性 chrome**：状态栏多一个 ▣ 切换按钮（开关本体，任何切换功能不可避免）。G2「关态字节同一」按设计 §一  scope 于 `#timeline` innerHTML＋body class；G9 PC 关态像素同一 scope 于 `#timeline` 元素截图。整页 chrome 因按钮存在而不逐字节同——请组长裁定此 scope 是否符合「关态外观与现网逐字节一致」口径；若需整页同一，按钮须另藏（如设置菜单内），本期未做。
2. mention-reply 卡、reasoning 行、独立 error 卡（schema_conflict 等）不进 CLI 投影（映射表 scope 外，设计 §二）；回合内 error 有 ✗ 行。
3. N-2 offturn 折叠条：本基（7d3035c6）不含 N-2 代码；契约注释＋跳过标记登记，N-2 合入后激活（设计 §八）。
4. md-render-core bypass（markdown-lite）⇒ 无 console_specs_assets_test.go sha 回写；若后续改走 md-render-core 须按设计 §六 流程回写。
5. K5 刀锚在 G7 实际所经 c1ApplyToEntry 阶段更新钩子；renderUser 的 viaDlv 失败防御分支同语义保护，无单独格（该分支在正常索引命中时不可达，T-074 防御余量）。
6. r15 两格／console_browser 零组格等既有负载抖按既定裁定继承（415b9bf5／后续统筹单），不在本单修。
7. 校准探针的 dynamicToolCall 与 fileChange live 形状缺口见 §三.7。

## 八、边界遵守

- 未改 VERSION；未推 main；未跑 build.sh 出发布件；未碰预发/现网；driver/Hub/helper 零改动。
- 校准探针隔离 HOME/CODEX_HOME，mock 凭据 dummy；未复用 donor 凭据（本单不需要真 provider）；日志无密钥。
- 负载纪律：门段串行、无人为加负载；段前后 conn 探针读数入 evidence/conn/（helper 重连 >2 即停段报——本段未触发）。
