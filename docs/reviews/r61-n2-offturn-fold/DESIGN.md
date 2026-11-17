# S-R61-N2 设计稿：Claude 席回合外输出在时间线内显示为折叠条

- 派单：opus50tc2k.zzc 2026-11-17T18:20:07Z（delivery dw-dlv_35d9f6230febc79419f6b55c3b1c5a6f）。
- 依据：业主 ㊿-补126（2026-11-16T23:07:44Z「N-2 --- 时间线里的折叠条， nice」；rulings eb397962，docs/rulings/S-TEAM-MIGRATE-HOSTED-20261017.md 第 984 行起）。
- 组长口径：Claude 席回合外 assistant_text 在时间线内以**浅灰折叠条**显示，标题「回合外输出（Claude）」，**连续的合并为一条、默认折叠、点开看全文**；不开浮窗、不另开文字区。
- 本稿基线：r60 集成锚 **9818b979e9e20f0bcfeffa6b78045dda71b1bd79**（产品锚 a311aaf3b）。分支 qwen38mvb4/r61-n2-offturn-fold；r60 最终锚定后 rebase。
- 上游勘察：docs/reviews/short-cards-upstream/REPORT.md（分支 qwen38mvb4/short-cards-upstream，tip 36dd130c，基 main d2cc47c7）。本稿在其结论上落地，全部坐标已按 9818b979 复核（行号有漂移处一律以本稿为准）。
- 边界：**只动 View／renderer 一侧**（renderer-canary-sse.html＋新增回归件＋CSS）；不改 VERSION、不推 main、不出发布件、不碰现网。Hub／helper 零改动；如实现阶段发现必须动 Hub／helper，先报组长（本稿 §1.4 已预列一项可选 Hub 硬化，属先报项，未经批准不实现）。
- 阶段：本稿为**设计稿**，组长过目后再定实现；本提交纯 docs，无产品改动。

## 0. 现状与介入点（已核）

渲染分发链（renderer-canary-sse.html，坐标相对 9818b979）：

1. 流边界：eventSource.onmessage（5459 起）→ **admitEventId(id)**（5291）按 SSE id「纪元-seq」去重（R-V2-0105）；无 id 旧 Hub 照旧。
2. handleIncomingEvent（2545）：historyBootstrapPending 缓冲（2549-2552）→ dispatchIncomingEvent（2602）。
3. dispatchIncomingEvent：历史去重闸（2603-2612，historyInputReplayKeys／historyHighWater）→ normalizeEvent（2632）→ delivery_update 角色闸（2636-2641）→ processedDeliveries／重发按钮移除（2644-2648，仅带 deliveryId 的事件）→ mention_reply 早退（2662-2666，renderMentionReply）→ delivery_update／copy 原位更新（2669 起）→ 其余进 renderEvent（3808）。
4. renderEvent：turnKeyFor(ev)（3004：ev.turn_id || ev._norm?.turnId || ev.delivery_id || ''）非空且 kind∈{user_input, assistant_text, tool_call, state_change, error} → renderTurnEvent（3946）；HIDDEN_TIMELINE_CARD_KINDS={tool_call, state_change}（3806）不产卡；**assistant_text 无 turnKey → renderAssistant（4414）独立卡**（3851）。
5. keyFor(ev)（3000）= delivery_id || turn_id || `cursor-${ev.cursor||rendered}`。回合外事件两 id 皆空 ⇒ 键退化为逐事件 cursor ⇒ **每条各自成卡**（触发样本：ts3opus55 六条短卡，见勘察报告 §1）。
6. stableAssistantItemKey（3013）要求 threadId＋turnId＋itemId 三者皆非空，否则返回 ''⇒ 回合外事件无稳定条目键，现有回合组去重／backfill 替换（3987-3995）对其不生效。

**介入点**：renderEvent 的 3851 分支前加「回合外折叠闸」；dispatchIncomingEvent 的产卡出口处挂折叠组边界跟踪（§2）。不改 1-3 层任何去重／归并／生命周期逻辑。

## 1. ① 「回合外」判定依据与数据来源

### 1.1 事件类型与字段（已证，勘察报告 §2 复核）

- 事件类型：session_event **kind=assistant_text**。
- 判定字段：**turn_id 与 delivery_id 皆为空串**。来源：claudeDriver.py:605-607 在 _handle_frame 开头一次性快照 `active=self._active`，`turn_id=active.turn_id if active else ""`、`delivery_id=active.envelope_id if active else ""`；640-651 对 assistant 帧每个 text 块发 `SessionEvent(kind="assistant_text", text=…, data={}, delivery_id=…, turn_id=…)`。
- 空窗只有两个（勘察报告 §2.1）：**W1** start() 后到首次 deliver() 前；**W2** result 终结帧发出 turn_completed 后清空 _active（claudeDriver.py:663-694，fix A 27c386f29 有意加入、有测试钉死：tools/sessionhelper/tests/test_claude_turn_attribution.py）。超时不清归属（470-492）⇒ 超时期间文本仍归属原回合，不属回合外。
- **data 恒为空字典 {}**：claudeDriver 对 assistant_text 从不写 item_id／thread_id（勘察报告 §2.3；codexDriver 对照 1531、1893-1897）。

### 1.2 数据来源（View 侧可见面）

- 实时：SSE GET /internal/view/{session}/events（cmd/workpulse/main.go:398；HandleInternalViewEvents，internal/m8/terminal.go:1163 起）——先回放 eventRing 再挂实时流，**无开场元数据帧**（不携带席 tool/cli）。
- 历史：loadSessionHistory 分页（renderer 4862 起；before_seq／cursor.generation／gap_before／gaps），历史行与实时事件**同一 session_event schema**，经同一 dispatchIncomingEvent 管道（4900-4902：setTimelineEventEpoch(ev, `history:<generation>`) → handleIncomingEvent）。
- 🔴 **AMR 不影响本数据源**：internal/m8/amr.go:349-361 对 DeliveryID=="" || TurnID=="" 的事件只在**投递对账（AMR）路径**丢弃并计数（missingIdentityOrReplay）；session_event 走独立扇出（terminal.go:594 handleSessionEventEnvelopeContext 起：eventRing＋SessionHistory 入库＋内网订阅扇出），回合外事件**照常到达时间线**（六条短卡样本即实证）。折叠条不读 AMR、不读投递正文，只读 session_event 流。

### 1.3 Codex 席是否适用：**不适用，只限 Claude**（理由已证）

- Codex 的 assistant_text 归属来自**协议帧本身**：delta 路径 codexDriver.py:1888-1897 `turn_id=params.get("turnId","")`、`delivery_id=self._turn_to_envelope.get(turn_id,"")`、item_id 非空即写 data["item_id"]／data["thread_id"]；历史重放路径 1960-1987 按 turn 作用域发射（agentMessage 必带 turn_id／delivery_id，data 带 raw_method／item／turnId）。⇒ Codex 文本**构造上回合绑定**，不存在 Claude 式「内存快照清空」空窗。
- 形状签名差异：Claude assistant_text 的 data **恒 {}**；Codex assistant_text 的 data 必带 item_id／thread_id（delta）或 raw_method／item／turnId（history）之一。
- 结论：折叠**只限 Claude 席**；Codex 席即使出现空 turn 异常帧也**不折叠**（保持现状渲染），见 §1.4 门控与 §6 残余风险。

### 1.4 Claude-only 门控（View 侧可落地方案）

🔴 权威 cli 现状（已核）：View Service 是独立进程、只跟 Hub 打交道、**不认识 driver**（view_service.py:3-4，status 自报 driver="via-hub"）；Hub 唯一暴露 cli 的视图口是 mention-candidates（internal/m8/newplat_mention.go:14-58），**候选去掉自己**、cli 仅在线握手时取 sc.tool（agent_ws.go:102），离线为空 ⇒ **现有接口拿不到本会话的权威 cli**。事件流内亦无 Claude 正向标记：claudeDriver turn_completed data 无 model／driver（663-675）；codexDriver turn_completed 带 model／usage（1813-1830）⇒ 事件流只能**识别 Codex 存在**，不能正向确认 Claude。

**本稿采用（View 侧、零 Hub／helper 改动）：形状门控＋会话级 Codex 标记扫描，双闸皆过才折叠：**

- 闸 A（事件级）：kind=assistant_text 且 turnKeyFor(ev)==='' 且 data 无 Codex 标记（item_id／thread_id／params／raw_method／item／model／usage 任一非空即拒）。
- 闸 B（会话级）：当前已加载事件窗口内，若出现过任何 Codex 标记事件（同签名扫描，含 state_change 的 model／usage／raw_method），则**本会话整体不折叠**（保守回落现状渲染）。
- 两闸皆为已接受事件序列的**纯函数**⇒ 重连／回放重建后判定一致（§3）。
- 残余风险与兜底：Codex 协议异常帧（turnId 空＋itemId 空＋data 空）理论上与 Claude 签名不可区分——概率极低（协议构造上 item 必带 itemId），以 §5 变异刀钉死门控，并以下方可选硬化收口。

**可选硬化（🔴 先报项，动 Hub，未经组长批准不实现）**：Hub 在既有内网视图口暴露本会话 tool（如 internal status 响应加一字段，或 mention-candidates 含 self；sc.tool 已存在，只读、零语义变更）→ VS 透传 → renderer 优先用权威 cli 判定，形状门控降为 fallback。若组长批准，另起小锚实现；本稿主体不依赖它。

## 2. ② 连续合并的边界

### 2.1 合并定义（规范序，非到达序）

- 折叠组 = **规范时间线序**（placeTimelineNode 3743 的排序：epoch rank → seq／ts／cursor，compareTimelineNodes 3704）上，**极大连续**的、过 §1.4 双闸的 assistant_text 事件段。
- 本页为单会话视图（/view/{full_name}，SSE 按会话订阅）；「其它席输出」在本页的具体形态是 **mention_reply**（被提及 Agent 的回复镜像，2662-2666 早退分支）。
- 组键 = `cursor-<组内首事件 cursor>`（keyFor 3000 的退化形，跨回放稳定）；组状态存**独立 offturnGroups 映射**，不污染 messageNodes／messageBuffers／assistantEntries（与独立 assistant 卡生命周期隔离）。

### 2.2 断开边界（与组长口径逐条对应）

| 事件 | 是否断开 | 依据 |
| --- | --- | --- |
| 正常回合事件（turnKeyFor 非空：user_input／assistant_text／tool_call／state_change／error 入回合组） | **断** | renderEvent 3839-3845 |
| 入站卡（input_posted／无回合 user_input 独立卡） | **断** | renderEvent 3849-3850；输入卡路径 3815-3838 |
| 其它席输出（mention_reply） | **断** | dispatchIncomingEvent 2662-2666 |
| error 独立卡／renderUnknown 卡 | **断**（保守：任何非本组延续的新产卡事件即断） | renderEvent 3852-3853 |
| tool_call／state_change 隐藏 kind | **不断**（不产卡；state_change 状态栏／成本副作用照旧） | HIDDEN_TIMELINE_CARD_KINDS 3806、3846-3848 |
| delivery_update／copy 原位更新（不新增时间线条目） | **不断** | 2669 起原位更新路径 |
| 历史缺口提示卡（「这段历史未保存」） | **断**（它是时间线条目，语义上隔断连续性） | 4918-4924、2626-2631 |

### 2.3 合并实现先例与 UI

- 先例：R-V2-0105 回合组软上限合并（3908-3926）——连续 assistant_text **文本按序拼接、cursor 取首条**，合并不改正文。折叠组沿用同语义：buffer 逐事件 append（renderAssistant 4432 同款「不去重、不吞正文」），组键取首事件 cursor。
- UI：`<details class="offturn-fold">`（**默认无 open 属性＝默认折叠**；原生键盘可达）；`<summary>` 标题逐字「回合外输出（Claude）」，附条数与首末时间（次要 meta，供组长过目时裁量）；展开后正文 = 拼接全文经 **renderMarkdown 既有路径**渲染（复用 renderAssistantBody 4435-4443 的 markdown／复制按钮／图片让位接线，不新造渲染路径）。
- 样式：浅灰底（中性灰，与现有 message-card 主题协调；具体色值实现阶段定，CSS 仅新增 .offturn-fold 族，不改既有类）。**不开浮窗、不另开文字区**（组长口径硬约束）。
- 折叠条**不挂**重发按钮、来源图标、投递状态标（§4）；eventTimeHtml 取首事件 ts（无 ts 不显示不猜，3866-3878 同语义）。
- 跟踪钩子位置：dispatchIncomingEvent 内、**全部去重闸之后**（历史闸 2603-2612、流边界 admitEventId 已先行）的产卡出口处更新组边界；mention_reply 早退分支单独挂一处。🔴 不得挂在去重之前（否则重放重复 append）。

## 3. ③ 刷新与历史回放下的稳定性（不重复、不丢）

**总原则：折叠组是「已接受事件序列按规范序」的纯投影；不新增任何去重机制。**
🔴 3889-3905 P0 教训钉死：cursor 去重不得作用于流式内容（Codex delta 整 item 共用 cursor）；幂等只靠既有两种机制，折叠条必须在其伞下工作。

### 3.1 带序号重连（R-V2-0105，新 Hub）

- 重连带 after=<纪元>-<lastContiguousSeq>（eventsStreamUrl 5285-5289），Hub 只补其后；已渲染后缀保留（5443-5448 注释与分支）。
- 补缺事件按 seq 序进入同一 dispatch 管道：若延续尾部开放组 ⇒ append；若插入边界事件 ⇒ 按 §2 断开重算。组键（首 cursor）不变 ⇒ **不重复、不丢**。
- 换纪元：保留现有内容、以新基线续（admitEventId 5296-5300）；折叠组同回合组一样保留后缀，跨纪元相邻段按规范序重算边界。

### 3.2 无序号重连（旧 Hub 回落）

- markStreamReconnect（5255）挂起 pendingReconnectClear，首条重放事件消费（5250）→ **resetConversation（2078）清空重建**；窗口内无事件则取消清空保内容（5261-5268）。
- 🔴 实现必做：resetConversation 的清理清单（2082-2091：messageNodes／assistantEntries 等）**同步清 offturnGroups 与折叠渲染队列条目**；重建后同一事件序列产生同一组序列（组键=首 cursor 不变）⇒ 折叠条数与全文逐字节一致（§5 G6 钉死）。

### 3.3 历史分页

- 历史页经 historyApplying 同步应用（4880-4916）：逐行 setTimelineEventEpoch(history:<generation>) → handleIncomingEvent → flushPendingRenders 同步刷 → 新增节点 placeTimelineNode 重排。
- 旧页**前插**会改变相邻关系：原分离的两段可能变相邻（须合并）、原连续段可能被插入的边界事件隔断（须分裂）。⇒ **每页应用后按规范序对受影响区间重算折叠分组**（与回合组同步刷同一批，不依赖 rAF／定时器时序）；重算是纯函数 ⇒ 任意翻页顺序收敛到同一结果。
- historyBootstrapPending 缓冲（2549-2552、4944-4953）：bootstrap 期间实时事件入队，历史落定后仅补 cursor.seq > historyHighWater 者 ⇒ **历史／实时重叠不双份 append**。
- 历史缺口提示卡断开分组（§2.2）；gap  refill（5276 起 GAP_REFILL_*）补回的洞内事件同 3.1 规则。

### 3.4 逐出与刷新

- enforceTimelineCap（2524-2543）oldest-first 逐出；historyApplying／扩窗阅读期间暂停。折叠卡以 dataset.messageKey 注册进 releaseTimelineNode 反查清理（4419 同款）⇒ 逐出不留组状态泄漏。
- 组内首事件被逐出 ⇒ 组键随**首个保留事件**重键（与既有回合组逐出语义一致）；折叠**不保留已逐出内容**（不绕过容量界）。
- 整页刷新 = startApp（4946 起）resetConversation → 历史 bootstrap → 实时流：全量重建，纯投影 ⇒ 与刷新前一致（§5 G5/G6/G7 覆盖）。

## 4. ④ 与 r60 ④ 重发按钮、8cabb351 来源图标逻辑不冲突

- **重发按钮（r60 ④）**：只挂投递生命周期卡，键控 delivery_id（c2SendControl kind=retry，1354-1360；waiting-retry-btn 1435-1454；V2 口 /internal/view-v2/{session}/retry，main.go:407）。回合外事件 delivery_id 为空、非投递生命周期 ⇒ 折叠条**无重发 UI、无 retry 接线**；processedDeliveries／按钮移除逻辑（2644-2648）仅作用于带 deliveryId 事件 ⇒ 零交集。
- **retainCanonicalTrustedProvenance（8cabb351 热修）**：函数体 3856-3864——只在**输入卡 delivery 精确命中**后，把 canonical primary 的 Hub 认证 provenance 复用给已 admit 的回显事件；调用点仅两处（2801 user_input 回显归并、3828 legacy user_input 合卡）。折叠路径**不调用该函数、不读写 c1InputCards／c1DeliveryCards／deliveryTurnInputs**⇒ 热修语义（信任只来自 canonical primary、绝不来自 helper echo）不被触碰。
- **来源图标**：provenanceIconHtml（2385-2402）仅在 norm.prov.trusted===true 时出图标；回合外 assistant_text 无 _norm.prov ⇒ 无图标（与现状独立 assistant 卡一致）。折叠条 summary 不造 provenance、不猜来源。
- **回归保证**：既有重发／来源格（delivery-ack-v2-browser-regression.js、renderer-provenance-icon-regression.js、renderer-t074-delivery-id-regression.js 等）在新锚必须**原样全绿**（§5 G9）；折叠改动不得使其任一断言变红。

## 5. ⑤ 浏览器回归格与删保护清单

### 5.1 新增回归件（实现阶段交付）

- view-service/renderer-offturn-fold-regression.js＋配套 python fixture（仿 renderer-replay-seq-regression.js／renderer-replay-seq-fixture.py 模式：真 EventSource、真断线重连、renderer-regression-selfhost 的 launchChromium／regressionCollector／finishRegression）；注册进浏览器全量 runner 清单。
- 工装需能构造：W1/W2 事件序列、边界事件插队、带序号／无序号两种重连、历史分页（before_seq／generation／gap）、逐出上限、Codex 标记事件。

### 5.2 回归格（每格列删保护对象）

| 格 | 断言 | 删保护（删之须红） |
| --- | --- | --- |
| G1 W2 折叠 | turn_completed 后 N 条回合外文本 ⇒ **一条**折叠条；默认折叠（details 无 open）；展开全文==按序拼接 | 删 append／删默认折叠属性 |
| G2 W1 折叠 | 启动后未投递即出文本 ⇒ 折叠条；标题逐字「回合外输出（Claude）」 | 删 W1 空窗判定 |
| G3 边界 | 正常回合／入站卡／mention_reply／error 插队 ⇒ 断成两条；tool_call／state_change 插队 ⇒ **不断**；历史缺口提示 ⇒ 断 | 删各断开分支／误把隐藏 kind 当边界 |
| G4 Codex 不折叠 | 带 item_id／thread_id／raw_method 标记事件（即使 turn 空）⇒ 不折叠；会话窗口出现过 Codex 标记 ⇒ 全会话不折叠 | 删闸 A 标记检查／删闸 B 会话扫描 |
| G5 带序号重连 | after 补缺后正文不翻倍、组数不变、组键不变 | 删 seq 伞下纯投影（引入自造去重即红） |
| G6 无序号重连 | 清空＋ring 重建 ⇒ 折叠条数与全文**逐字节一致** | 删 resetConversation 的 offturn 清理 |
| G7 历史分页 | 旧页前插改变相邻 ⇒ 确定性合并／分裂；bootstrap 缓冲不双份；任意翻页顺序收敛同结果 | 删页后重算／删 highWater 跳过 |
| G8 逐出 | cap 逐出后组状态无泄漏、反查清理命中；组键随首保留事件 | 删 releaseTimelineNode 折叠反查 |
| G9 不冲突 | 折叠条无重发按钮、无来源图标；既有重发／来源格全绿 | 删 §4 隔离（误接 retry／prov 即红） |
| G10 不吞正文 | 展开全文长度==事件文本长度和（折叠只改显示不改接受） | 删 append-only（任何吞文本变异即红） |

### 5.3 md-render-core 与 sha 回写流程（条件性）

- **本设计不改 md-render-core.js**：折叠正文复用既有 renderMarkdown 路径 ⇒ 无 sha 回写需求。
- 若实现阶段确需改 core，流程（已核）：
  1. 改 view-service/md-render-core.js（单一事实源）；
  2. `python3 view-service/md-render-core-sync.py` 把 renderer 内 6 段生成副本刷成逐字节一致（--check 为漂移闸，rc=1 即红；结构错误硬中止）；
  3. 重算 md-render-core.js 的 sha256，回写 **internal/m8/console_specs_assets_test.go 的 TestConsoleSpecEmbeddedMarkdownCoreIsPinned const want**（当前 pin 0f03c242f05e359d94ff1dbe0453d77e83ec7f50d0dec2add4bb6e70b7eb146a，42-46 行；Go 侧 viewservice.MarkdownRenderCore 嵌入同源）；
  4. test_md_render_core.py＋md-render-core-parity.js＋上述 Go 测试全绿；spec 展示面与 timeline 同渲染路径的交付条件（0.2.1.50）不破。

### 5.4 门（实现阶段，集成锚上）

- scripts/run-build-tests.sh rc=0（两 profile，只跑 Go 不算）；浏览器全量 runner（PATH 含 go）；新格逐名列读数；跳过用例逐名写原因。
- 变异读数：§5.2 每格删保护各跑一次变异，列「业务红／还原 SHA ok」；编译 0 不算过。
- 本设计稿阶段不跑产品门（纯 docs）；实现另锚另门。

## 6. 边界、残余风险与承诺

- 只动 View／renderer：renderer-canary-sse.html（分发闸＋组管理＋UI＋CSS）＋新增回归件；**Hub／helper 零改动**；不改 VERSION、不推 main、不出发布件、不碰现网、不部署。
- 可选 Hub 硬化（§1.4）为**先报项**：未经组长批准不实现；批准后另起小锚。
- 残余风险（如实列）：
  1. Codex 协议异常帧（turnId 空＋itemId 空＋data 空）理论上与 Claude 签名不可区分 ⇒ 闸 A/B＋G4 变异钉死；权威收口靠可选硬化。
  2. 深历史窗口无任何 driver 正向信号时，Claude 判定依赖 data={} 签名（claudeDriver 构造上恒成立，已证）；若未来 helper 改动给 Claude assistant_text 加 data 字段，门控须同步复审（上游勘察 O1/O2 若另立单落地，本单门控随之修订——O2 的 attribution 标记落地后可直接替代形状闸，更稳）。
  3. 折叠条数与时间范围 meta 为设计提议（组长口径只定标题），过目时裁量。
- r60 最终锚定后本分支 rebase（派单承诺）；rebase 后重跑 §5 门再交实现。
- 与并行单隔离：coder56solzzc/r60-ingress-fix（Hub 入站，尚未见于远端）、togpt60 N-1（helper 收件箱）——本单不读不改其分支；若 ingress-fix 改变 session_event 入站语义（如 stripClientProvenance 范围），实现前重核 §1.2 扇出路径。

## 7. 坐标索引（全部相对 9818b979，已逐一复核）

| 主题 | 坐标 |
| --- | --- |
| 回合外判定（快照／发射／清空） | tools/sessionhelper/sessionhelper/claudeDriver.py:605-607、640-651、663-694 |
| fix A 钉死测试 | tools/sessionhelper/tests/test_claude_turn_attribution.py |
| Codex 归属对照 | tools/sessionhelper/sessionhelper/codexDriver.py:1888-1897、1960-1987、1813-1830 |
| AMR 丢空归属（仅对账路径） | internal/m8/amr.go:349-361 |
| session_event 扇出／入库 | internal/m8/terminal.go:570-609、1163 起 |
| SSE 口／after 回放 | cmd/workpulse/main.go:398；internal/m8/terminal.go:1163-1215 |
| cli 唯一暴露口（去自己） | internal/m8/newplat_mention.go:14-58；agent_ws.go:102 |
| VS 不认识 driver | view-service/view_service.py:3-4、1420、1440 |
| 流边界序号去重 | view-service/renderer-canary-sse.html:5291（admitEventId）、5276-5289 |
| 分发链／历史闸 | renderer-canary-sse.html:2545-2555、2602-2632、2662-2666 |
| turnKeyFor／keyFor／stableAssistantItemKey | renderer-canary-sse.html:3004、3000、3013 |
| renderEvent 分发／隐藏卡集 | renderer-canary-sse.html:3808-3855、3806 |
| renderAssistant／buffer append | renderer-canary-sse.html:4414-4433 |
| 回合组合并先例（R-V2-0105） | renderer-canary-sse.html:3908-3926、3987-3995 |
| 重连清空／重建 | renderer-canary-sse.html:5255-5270、5250、2078-2091、5443-5448 |
| 历史分页应用 | renderer-canary-sse.html:4862 起、4880-4916、4944-4953 |
| 时间线排序／逐出 | renderer-canary-sse.html:3704、3743-3779、2524-2543 |
| 重发按钮（r60 ④） | renderer-canary-sse.html:1354-1360、1435-1454、2644-2648；cmd/workpulse/main.go:407 |
| retainCanonicalTrustedProvenance | renderer-canary-sse.html:3856-3864（调用点 2801、3828） |
| provenanceIconHtml | renderer-canary-sse.html:2385-2402 |
| scheduleRender／renderPendingEntry | renderer-canary-sse.html:4133-4156、4126-4132 |
| md-render-core 同步／漂移闸 | view-service/md-render-core-sync.py；test_md_render_core.py；md-render-core-parity.js |
| sha pin 回写点 | internal/m8/console_specs_assets_test.go:42-46（TestConsoleSpecEmbeddedMarkdownCoreIsPinned） |
| 回归工装先例 | view-service/renderer-replay-seq-regression.js＋renderer-replay-seq-fixture.py；renderer-regression-selfhost.js |
| 上游勘察 | docs/reviews/short-cards-upstream/REPORT.md（36dd130c） |
