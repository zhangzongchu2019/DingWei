# S-R62-CLI-VIEW-B 设计页：View 终端风格第二渲染（Codex CLI 版式）

- 依据：业主 rulings ㊿-补131（tip 86e13532，甲乙都想要、远期自由切换；先做乙、以 Codex 界面为原型）；组长派单 fc3ef5a0d8a5（14:44:57Z 原文，19:58:20Z 补发，两处更新：手机单冻结 7d3035c6、甲解析硬伤已由 togpt60 9b326857 修复）；本席勘察 425e32db REPORT §四（方案乙）。
- 期限：设计页 2026-11-20T18:00:00Z；原型 2026-11-21T18:00:00Z。复核：组长。
- 分支：qwen38mvb4/r62-cli-view-b；设计基 main 206d41b26dab646c0e50a111eed5a45ccf7c72f6；原型基手机单 tip 7d3035c6（渲染层交叠以手机单为先）。
- 硬边界：driver 不动、Hub 不动、Console 子 Agent 树不动；甲两处硬伤（TUI 终答不渲染、parse_codex_event）本单不修（后者已另单冻结，前者归 S-R62-CLI-A-TUI）。

## 一、总体架构：加性双投影，默认关＝逐字节不变

**选型**：与现时间线平行的第二套纯投影（方案 A），不做「单 DOM 重渲切换」（方案 B）。
- 现管线不动：normalizeEvent（:2242）→ renderEvent（:3808）→ 回合组 rebuildTurn（:4178）/输入卡 renderUser（:4308）。
- 新增 CLI 投影层：`rebuildTurnCLI(group)`、`renderUserCLI(ev/entry)`、`renderOffturnCLI(group)`（N-2 合入后），**只读同一份 group.events / entry 数据**，产出独立 CLI DOM（`#timeline-cli`，与 `#timeline` 同级、互斥显示）。
- 钩子全部带闸：`if (!cliViewOn()) return;` 置于 rebuildTurn 末尾、renderUser 建卡/更新处、逐出与 reset 处。**关态钩子零副作用**：不建 CLI DOM、不改现 DOM 一个字节、不加影响现布局的 CSS。
- 新 CSS 全部作用域化于 `body.cli-view-on`（及 `#timeline-cli` 内部选择器）；关态 body 无该 class ⇒ 现网样式逐字节不受影响。
- 切换：开＝按现有事件态全量构建 CLI DOM（纯投影、幂等）；关＝移除 CLI DOM、恢复现 DOM 显示。来回切换不丢事件（数据源是 group.events / 输入卡索引，不是 DOM）。
- 选型理由：关态字节同一可静态保证（加性层不触旧路径）；开态稳定性继承纯投影模式（N-2 同型论证）；避免方案 B 全量重渲对附件区/长文条/来源徽标等瞬态的重建风险。

**开关与持久化（先按用户）**：
- 键 `dw-cli-view`（localStorage），值 `on`/`off`，缺省＝off。手法逐字仿主题先例（:5930 起）：BroadcastChannel 跨 tab／隐藏 pane 同步；**写入方自己立刻 apply**（频道与 storage 事件不回触写入方）；不用 sessionStorage（不跨 tab）。
- 切换控件：状态栏区一枚按钮「终端视图」（与 themeBtn 同区），aria-pressed 随态；窄屏点击目标 ≥40px（手机单规则）。
- **按会话覆盖位（本期预留，不实现服务端配置面）**：优先级＝会话覆盖 ＞ 用户偏好 ＞ 默认关。注入点仿 view_service.py:540 _PROV_FLAG_SCRIPT（`</head>` 前插 `window.CLI_VIEW_SESSION_OVERRIDE=<bool>`，env 回退键预留 `VS_CLI_VIEW_SESSION`）；renderer 读取处本期即留（`cliViewOn()` 内先查 override），服务端注入本期不接（避免动 view_service 配置面，留待另令）。覆盖位存在时用户按钮仍可见但标注「会话级已锁定」与否由实现定，设计取「覆盖优先、按钮置灰并 title 说明」。

## 二、事件 → CLI 行映射表

版式语义参照 Codex CLI 0.170.0 TUI（§三）；所有行等宽字体；来源/信任语义**只复用现管线产物，不另派生**。

| 源事件／数据 | CLI 行形态 | 语义保留点 |
| --- | --- | --- |
| user_input／input_posted（canonical 输入，含 admitted echo 合卡） | `› <正文>`（用户行，亮色；实证 d-0170b/d-0159b 末屏）；行尾阶段缀 `[已注入]`/`[失败:原因]` 等，取自 stageFooterText 同源数据 | T-074 合卡语义：同一投递只一行，echo 就地更新不另起；**失败不另起错误行**（业主 09-11 规则），只更新该用户行阶段缀；来源徽标按 provenanceIconHtml／sourceBadgeHtml 同源输出转文本前缀（如 `[人类]`/`[agent名]`），fail-closed：不可信不挂来源 |
| assistant_text（delta 拼接／历史全文；回合组内） | `• <正文>`  bullet 行（等宽、markdown-lite，§四；实证：流式 after-3「• 收到」→完成 after-5「• 收到。」）；多段正文续行缩进对齐 | 回合分组键 turnKeyFor 同源；stableAssistantItemKey 去重／backfill 语义继承（rebuildTurnCLI 读同一 group.events，天然继承） |
| tool_call（commandExecution／fileChange／dynamicToolCall；data.item） | 进行中：`• Running <command>…`；完成：`• Ran <command>`＋`└ exit <code> · <输出首行摘要>`（details 可展开全输出）；fileChange→`• Edited <path>`；dynamicToolCall→`• <name>(<args摘要>)` | 与现时间线差异（有意）：现视图回合完成后隐藏工具，CLI 视图保留工具行在 scrollback（终端语义）；输出字段（aggregated_output／exit_code）原型期以真实 rollout fixture 核实，缺字段时降级为 name＋input 预览（现 renderTurnTool 同源） |
| 子 Agent | **无独立事件种**（renderer grep 0 命中）：父席操作以 MCP tool_call 行呈现（spawn/wait 等动态工具）；跨席消息以带来源的用户行呈现；Console 树（/api/console/agent-trees）不在本渲染面、不动 | 不新造子 Agent UI；来源绑定走 N-3 既有 attested 语义 |
| N-2 回合外折叠条（offturn-fold，Claude 席、双闸、极大连续段） | 暗色分隔行 `── 回合外输出（Claude） ──`＋details 折叠全文；标题逐字同 N-2 | **消费 offturnGroups 同一投影**，不另算判定/合并（双闸与断开条件以 N-2 实现为唯一事实源）；本期原型基（7d3035c6）尚无 N-2 代码 ⇒ 该行以「接口契约」形式预留：CLI 投影在 timeline 序上遇到 offturn 组节点即映射，N-2 未合入时该路径不触发；集成者合入 N-2（8ae6b083）后按 §八回归验证 |
| error（回合内／独立） | `✗ <正文>`（红色行） | 投递失败不在此列（走用户行阶段缀）；独立 error 事件才起错误行 |
| state_change turn_completed | 回合尾行 `  Worked for <耗时> • <时钟>`（实证形态）；model/usage 归底状态行（`<model> · <tokens> tokens`，data.model/usage/admitted_at/processed_at 已有） | 完成标志保留（现视图 ✓ footer 同义）；usage 缺字段时省略对应段，不造假数 |
| state_change 其余／delivery_update／mention_reply | 不产生 CLI 行（与现视图 HIDDEN_TIMELINE_CARD_KINDS／合卡语义一致）；状态栏副作用（成本/待提交区）走现管线、CLI 视图不复制 | 待提交区/状态栏为两视图共用控件，不在 #timeline-cli 内重画 |
| 文件附件（/f/ 直链、attachFileRenders） | 用户行/正文块下挂 `📎 <文件名>` 行（链接可点） | 附件渲染复用 attachFileRenders 产物语义；CLI 视图不重建上传/下载逻辑 |

## 三、版式参照（Codex CLI 0.170.0 TUI，已实证校准）

- **完成态四区（mock-provider 直启截屏实证，evidence/layout/d-0170b、d-0159b）**：
  ① 横幅：`  >_ OpenAI Codex (v0.170.0)`＋workdir 行；
  ② transcript：用户行 `› <正文>`；助手行 `• <正文>`（流式增量追加）；回合尾 `  Worked for <时长> • <时钟>`；
  ③ 输入区：完成后恢复 `› Ask Codex to do anything` 占位；进行中＝Working 转轮＋任务标题（勘察 T2 真 provider 帧为据）；
  ④ 底状态：`<model> default · <workdir> [· 末条 prompt]`＋`← for agents · ? for shortcuts`＋告警指示。
- 0.159.2 与 0.170.0 版式同构（差异仅横幅版本号、0.159.2 多一行 onboarding 提示、状态行不回显末条 prompt）；本设计以 0.170.0 为准。
- **wire 发现（重要，更正勘察表述）**：两版显式 `wire_api="chat"` 均启动即退（「no longer supported」，discussions/7782，错误原文入 evidence/layout/d-*-chat-rejected）；勘察 T1/T2 config 无 wire_api 键 ⇒ 默认 responses，即两版实际以 responses 线对接 token-plan compatible-mode。勘察 REPORT §3.2「chat 兼容端点」表述不准确（端点 OpenAI 兼容、wire 为 responses），勘察分支冻结不改写，更正记 evidence/layout/NOTES.md；CLI-A-TUI 定因以此为准。
- 中性观察：形状良好的 mock responses 流下两版 TUI 均正常收尾（rollout task_complete＋末屏完成态）；真 provider 直启对照归 CLI-A-TUI，本设计不预定因。
- 工具行版式（`• Ran <command>`＋`└` 输出摘要）：mock 短答无工具调用，**未实证**，原型期以 mock 工具调用臂或真实 rollout fixture 校准（§十风险②）。
- 保真边界：**参照而非像素级**——等宽字体、四区结构、行首符号（›/•/✗）、回合尾 Worked for 行；不复制 Codex 配色精确值与动画帧率。推理行不画（driver 直播无 reasoning 事件，§十）。
- 主题：CLI 视图随现 dw-theme 明暗两套（终端配色各一套 CSS 变量）；明暗切换不重建投影（只换变量）。

## 四、长文策略

- 沿用现阈值：LONGTEXT_THRESHOLD=4000／LONGTEXT_EXCERPT=1200（window.DW_LONGTEXT_* 可覆，同源）。
- CLI 视图超长正文：显示前 1200 字摘要＋终端风格 pager 行 `── 全文 N 字 · [打开全文] [下载 .md] [复制] ──`；全文点击时现拼（仿 wireLongtextBar 懒拼装，流式期不为未点全文付成本）。
- 理由：flood 判据零余量（§七），CLI 视图若无条件全量渲染长文会改变性能包络；与现视图同阈值 ⇒ 性能 parity 可证。终端「无限 scrollback」观感由 pager 行＋全文 overlay 满足（overlay 复用现长文全文展示面，不另造）。
- markdown-lite：CLI 正文走**独立轻量格式化**（纯文本转义＋代码块/行内码/粗体三种内联标记），**bypass md-render-core**（§六）；标题/列表等按纯文本原样显示（终端语义）。

## 五、与手机窄屏的关系（手机单为先）

- 原型基 7d3035c6 已含：viewport meta、≤768 抽屉菜单、标签横滚、document 横向溢出 0、点击目标 ≥40px、PC 1440 像素不变。
- CLI 视图附加规则：所有 CLI 行 `white-space:pre-wrap; overflow-wrap:anywhere`（长命令/长正文不产生横向滚动）；工具输出 details 展开同样约束；`#timeline-cli` 自身 overflow-y:auto、overflow-x:hidden。
- 切换按钮 ≥40×40（窄屏）；抽屉开合/标签横滚/会话阅读滚动不受 CLI 视图影响（CLI 视图只换 timeline 内容面，不动 nav/tabs/panes 骨架）。
- 回归：console-mobile 既有 13 格原样全绿＋新增「窄屏 CLI 开态横向溢出 0／目标 ≥40」格（§八）。

## 六、md-render-core sha 回写流程（本设计取 bypass 分支）

- **本期不改 md-render-core.js**：CLI 正文用独立 markdown-lite（§四），不复用六段副本 ⇒ 源不动、副本不动、**无需 sha 回写**。test_md_render_core.py 漂移闸与 console_specs_assets_test.go:44 pin（want=0f03c242…）原样通过。
- 若后续决定 CLI 视图复用 md-render-core（例如业主想要完整 markdown 观感），流程为：改 md-render-core.js → `python3 md-render-core-sync.py` 刷 renderer 六段副本 → `--check` rc0 → test_md_render_core.py 绿 → 重算 Go 嵌入 sha 并回写 console_specs_assets_test.go:44 want → 该 Go 格绿。顺序不可跳（漂移闸先红后绿是预期）。本设计不走此分支，仅登记流程备查。

## 七、flood 性能判据（零余量，不放宽）

- 现判据原值：RATE=6000 条/s、SOFT_CAP=20000、MARGIN=100（bound=20100）、DEADLINE_S=180；场景 flood=100000、late-order=30000（lateCursor 12000／lateAfter 21000）。
- CLI-on 判据：**同阈值同场景**再跑一遍（新套件内以 window 开关置 on；flood 套件本体不改阈值），节点数/背压/乱序收敛读数须落在同一 bound 内；CLI-off 读数即现套件原值。
- 附加微基准（新格）：toggle-storm——流式推入中反复开合 ≥50 次，断言 ① 每次开态投影与事件态一致（抽样比对行数/末行文本）② 关态 #timeline-cli 不存在 ③ timeline 节点数有界（≤SOFT_CAP+MARGIN 同口径）④ 无 detached 节点泄漏（开合前后 DOM 节点计数差有界）。
- 负载纪律：压测段先报起止时刻（组长 22:53Z 令）；段前后记 conn_seq／重连，>2 次即停段报。

## 八、回归设计（新套件＋删保护＋runner 登记）

新套件 `view-service/renderer-cli-view-regression.js`（仿 renderer-*-regression.js 骨架：真 console_auth.go shell 抽提＋fixture 事件流＋playwright）：
- G1 默认关：无 CLI DOM、body 无 cli-view-on、按钮 aria-pressed=false。
- G2 **关态 DOM 字节同一**：同一 fixture 流下，基锚 renderer 与本件关态的 `#timeline` innerHTML（及 body class 列表）逐字节相等（双页面序列化比对）。
- G3 开合接线：点按钮→CLI DOM 出现、现 DOM 隐藏、aria-pressed=true；再点→还原；localStorage 值随动。
- G4 持久化跨 tab：第二 tab/隐藏 pane 经 BroadcastChannel 同步开合；写入方自 apply。
- G5 映射正确性：fixture 含 user/assistant/tool（三类）/error/turn_completed（带 model+usage）→逐行断言行首符号与关键字段；工具完成后保留行（与现视图差异格）。
- G6 来源不绕过：不可信来源不挂前缀；canonical attested 来源前缀正确；retainCanonicalTrustedProvenance 路径不被 CLI 层改写（构造 admitted echo  fixture）。
- G7 失败不另起行：delivery failed 只更新用户行阶段缀，CLI 面无独立错误行。
- G8 长文：>4000 字出 pager 行＋摘要 1200；打开全文现拼正确。
- G9 窄屏（390×844／360×780）：CLI 开态 document 横向溢出 0、切换按钮 ≥40、抽屉开合不受影响；PC 1440 关态像素同基锚。
- G10 重连/分页/逐出稳定性：SSE 断线重连＋历史分页＋逐出后开态投影与事件态一致（纯投影断言：重建后行数/序/末行同）；toggle 后同断言。
- G11 会话覆盖位：window.CLI_VIEW_SESSION_OVERRIDE=true/false 时优先级正确（override 锁定时按钮置灰）。
- G12 flood CLI-on：§七同阈值场景＋toggle-storm。
- N-2 交互格（集成后）：offturn 组在 CLI 面映射为分隔行＋details，合并/断开语义同 N-2；**本期以契约注释＋跳过标记登记，N-2 合入后激活**（不伪造 N-2 代码在本基）。

删保护刀（每格至少一把，MUTATION env 或 --mutate 形式与现套件同轨）：
- K1 关态加性破坏：往现 rebuildTurn 无条件插一行 CLI 构建 ⇒ G2 红（字节同一）。
- K2 切换接线断：按钮 onclick=null ⇒ G3 红在文案。
- K3 持久化断：localStorage 写删 ⇒ G4 红。
- K4 来源派生绕过：CLI 层自判 trust ⇒ G6 红。
- K5 失败另起行 ⇒ G7 红。
- K6 长文全量渲染（阈值改 Infinity）⇒ G8 红＋G12 性能红。
- K7 窄屏换行删（pre-wrap→nowrap）⇒ G9 红。
- K8 投影改增量 DOM 依赖（重建读 DOM 而非 events）⇒ G10 红。
- K9 override 优先级反 ⇒ G11 红。
- K10 flood 阈值放宽 ⇒ 套件自检红（阈值常量断言，防判据漂移）。

runner 登记（M2 流程复用）：SUITES 增 `renderer_cli_view`；env 读（MUTATION／PW_CHANNEL／CLI_VIEW_SHOT_DIR 等）进 OVERRIDE_ENV_NAMES；--selftest-env-coverage rc0；登记前单跑 ×3 全绿；登记后全量 runner 一次报逐套读数。既有套件原样全绿硬要求：renderer-provenance-icon（8cabb351）、renderer-retry-role（r60 ④）、r15 两格（继承抖按既定口径两锚对照）、console-mobile（M1 文案刀）、iframe/dock/flood 原值。

## 九、原型验收（期限 2026-11-21T18:00Z）

- 基 7d3035c6；截图：390×844／360×780／1440×900 各「关态＋开态」（共 ≥6 张，带 sha）。
- 断言：G1-G11 绿（G12 flood 段按负载纪律另段跑）；关态 DOM 字节同一（G2）；五刀以上红在文案。
- 版式校准证据：mock-provider 直启 0.170.0 TUI 的 tmux 截屏（完成态四区）入 evidence/，与原型开态截图并排对照说明参照点。
- 交件：分支 tip＋REPORT＋evidence＋SHA256SUMS（仓库根相对、git add -f 日志、远端 archive 复核）。

## 十、非目标与风险

- 非目标：真原生交互（甲）、driver/Hub/Console 树改动、按会话服务端配置面实现（仅预留位）、N-2 代码本体（另分支冻结）、像素级 TUI 仿真。
- 风险与对策：① 双投影内存（开态两套 DOM）→ 关态即移除 CLI DOM、flood 节点数有界断言；② 工具行版式与输出字段形状未实证（mock 无工具臂）→ 原型期 mock 工具调用臂＋真实 rollout fixture 核实、缺字段降级为 name＋input 预览；③ 版式保真主观 → mock 截屏对照（已附）＋业主过目节点；④ 与 N-2/时序窗集成叠加 → 契约先行、集成后激活格；⑤ 钩子遗漏致开态漏事件 → G10 纯投影一致性断言兜底（重建即全量）。
- 未调研项（如实）：Codex TUI 推理行在直播态无数据源（driver 过滤 reasoning，勘察 §3.5 同源事实）⇒ CLI 视图**不画推理行**，避免造假；历史回放中 reasoning 仅 state_change 形状，同样不画。此点与真 TUI 观感有可见差异，交件时向业主明示。
