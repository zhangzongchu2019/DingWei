# View 等候区统一队列、卡片规整与 ESC 中断技术规格

状态：技术设计稿
日期：通用工程版
Renderer 基线：`view-service/renderer-canary-sse.html`，包含 SSE 连接兜底

## 1. 目标与不变量

本批把 View 页的输入与执行状态统一为一个用户可理解的 FIFO：飞书、Agent 互调和 View 用户输入都先出现在同一等候区，只有 driver 真正接纳后才从等候区搬入新的回合卡片。

必须保持以下不变量：

1. 一张卡只表示一个 turn；同一 `delivery_id` 的 `queued → admitted` 只能搬迁，不能复制卡片或产生 orphan。
2. 单次多行输入始终是一项、一张卡；原文保留换行，使用 `white-space: pre-wrap`，不得为了等候区紧凑而截成单行。
3. tool/bash 只在回合执行中显示；收到该 turn 的终结事件后隐藏。基线已有此行为（`renderer-canary-sse.html:1133` 的 `if (tools.length && !completed)`），不得回退。
4. 子 Agent 执行中显示进度，完成后隐藏；最终答复和已产生的普通文本保留。
5. 等候区常驻：顶部显示当前执行项，下方显示剩余 FIFO。空队列也显示“暂无排队指令”。
6. ESC 是硬取消当前回合，不是取消排队项：保留部分输出并标注“已中断”，队列不重排，随后直接接纳下一项。

不在本规范范围：更改 driver 的模型行为、取消任意一条尚未执行的排队项、拖拽重排、优先级插队。

## 2. 现状核查

### 2.1 当前三条输入路径并未真正共用一个队列

View 用户输入路径：

```text
renderer POST /input
  → view_service.py:313-338
  → Hub HandleInternalViewInput (internal/m8/terminal.go:748-781)
  → routeTerminalInput，生成 meta.type=terminal_input (terminal.go:783-810)
  → helper recv_loop 特判 terminal_input (runtime app.py:298-300)
  → handle_terminal_input / adapter.write_terminal_input
  → DriverAdapter._deliver_human (runtime driver_adapter.py:299-319)
  → driver.deliver
```

`_deliver_human` 已在接纳前发 `queued`，接纳后以同一 `delivery_id` 发 `admitted`。但它会为每次提交创建独立线程（`driver_adapter.py:270`），并未与 `pending_inbound` 共用 FIFO。

Agent/飞书入站路径：

```text
helper recv_loop
  → buffer_if_busy (runtime app.py:353-371)
  → pending_inbound.append(env) (app.py:365)
  → pending_drain_loop popleft (app.py:382-404)
  → process_inbound_message (app.py:406-419)
  → DriverAdapter.handle (driver_adapter.py:94-114)
  → driver.deliver
```

该路径现在不发可见 `queued`，而 `handle()` 调用 `_settle(eid)` 时没有传 `human_text`，所以也不发 `admitted`（`driver_adapter.py:107-110,138-145`）。因此仅在 renderer 层拼 UI 无法达成“三来源同一执行顺序”。

### 2.2 当前中断能力

- renderer 已声明 `API.interrupt='/interrupt'`，按钮会 `POST /interrupt`（`renderer-canary-sse.html:744-748,1666-1675,1719-1724`）。
- View Service 会重写 `/interrupt` URL（`view_service.py:233`），但 `do_POST` 只接受 `/input`（`view_service.py:313-316`），所以当前按钮实际为 404。
- Hub 只有 `/internal/view/{session}/input`，没有 View interrupt 路由。
- 旧网页终端 Ctrl-C 最终以 `terminal_input` 的 `\x03` 到 helper；协议 adapter 在 `write_terminal_input` 中识别 `\x03` 并调用 `driver.interrupt()`（runtime `driver_adapter.py:284-291`）。这证明现有 driver 原语可用，但普通 `terminal_input` 是数据通道，不能作为新 View ESC 的最终设计：控制请求必须绕过 busy FIFO，才能抢占当前回合。
- helper sidecar 已有本机 `POST /interrupt` 直接调用 driver 的实现（runtime `sidecar.py:146-151`），可复用其 adapter 调用方式，不应从 View Service 跨主机直连 sidecar。

### 2.3 子 Agent 事件

Claude：stream-json 中所有 assistant `tool_use` 都映射为 `SessionEvent(kind='tool_call')`（runtime `claudeDriver.py:468-479`）。`Agent`、`TaskCreate`、`TaskOutput` 等没有独立事件 kind，均走该分支。因此当前 renderer 在 turn 完成后已经会隐藏它们；实现时只需把这些工具标记为 `data.category='subagent'`，以便执行中用“子 Agent”样式展示，不需要增加另一个顶层 kind。

Codex：当前 mapper 只把 `commandExecution`、`fileChange`、`dynamicToolCall` 映射为 `tool_call`（runtime `codexDriver.py:932-945,1029-1043`）。Codex daemon 的协作/子 Agent item 不在白名单中：live completed 会被丢弃，history 则退化为 `state_change(change=<item type>)`。所以不能宣称 Codex 子 Agent 已被“完成后隐藏”覆盖。实现时须把 daemon 实际协作 item（兼容 `collabToolCall`、`collab_tool_call` 及协议版本返回的等价 type）归一为 `kind='tool_call'`、`data.category='subagent'`；未知协作 item 先记录 raw type，不作为常驻卡片。必须用当前支持的 Codex daemon 版本 录制 fixture 锁定最终 type 名称，不能只凭字符串猜测。

OpenCode：本批只要求核查 Claude/Codex。OpenCode 的 `.tool.*` SSE 已统一成 `tool_call`（runtime `openCodeDriver.py:834-860`），若其子任务由 tool 触发，会自然获得相同行为。

## 3. 统一队列模型

### 3.1 单一所有者

在 SessionHelper 内新增一个会话级 `UnifiedCommandQueue`，它是三来源执行顺序的唯一所有者。不能继续让 View 输入由 `_deliver_human` 自建线程，同时让 Agent/飞书使用 `pending_inbound`。

建议将现有 `pending_inbound` 演进为下列结构，并由唯一 worker 串行调用 `driver.deliver`：

```text
QueuedCommand
  delivery_id
  queue_seq
  source                 user | agent | feishu
  display_name
  open_id
  bot_channel_id
  text
  envelope               Agent/飞书保留原信封；View 可为空
  enqueued_at
  state                  queued | admitting | admitted | completed | interrupted | failed
  turn_id
```

所有 `enqueue()`、`popleft()`、当前项切换和 `queue_seq` 分配必须在同一锁/事件循环内完成。只允许一个 delivery worker；driver 的内部锁只是最后防线，不能用它决定 UI 顺序。

“一个 worker”只表示一个 FIFO 调度者，不表示在 asyncio 事件循环内同步执行 driver。`driver.deliver()` 必须通过 `asyncio.to_thread()`、专用 executor 或独立 task 执行；worker 等待接纳期间，`recv_loop` 和 `terminal_interrupt` handler 必须继续运行，且 interrupt 可与在途 deliver 并发。禁止直接在 event-loop thread 调用阻塞式 `driver.deliver()`。

队列的 `current`、pending pop、admitting/admitted/terminal 状态迁移与 interrupt 的 stale-turn 核对必须共用 `UnifiedCommandQueue` 的同一把锁。interrupt handler 不得从另一个无锁字段读取 current，否则会在 worker 切换 turn 时中断错误回合。

队列满时不得沿用现在的“静默 drop oldest”（runtime `app.py:362-364`）。统一队列需要显式拒绝新项并发 `queue_rejected(reason='queue_full')`，否则页面显示顺序与实际执行会分叉。队列容量继续沿用 `SH_BUSY_BUFFER_MAX`，但拒绝策略固定为 reject-new，不删除已向用户展示的旧项。

### 3.2 来源归一

来源在入队时一次判定并冻结，renderer 不从地址字符串二次猜测：

| 路径 | `source` | `display_name` | `open_id` |
|---|---|---|---|
| View `terminal_input` | `user` | 当前可先用“用户”；后续可由 viewer profile 覆盖 | 空 |
| Agent 地址（两段 session address） | `agent` | `from` 中的 session 名 | 空 |
| 飞书地址/含飞书来源元数据 | `feishu` | Hub 解析的真名；查不到用 open_id | 发起人的 open_id |

飞书群消息必须取 `source_sender_openid`/`sender_open_id`，不能把群 `source_chat_id` 当人名；私聊依次取 `source_open_id`、`source_sender_openid`。`bot_channel_id` 取 `source_bot_channel_id`，供同一 open_id 在不同 bot 管道下查名。

真名解析复用 `internal/admin/admin.go:2158-2180` 的查找顺序：member → chat_entity(bot channel + open_id) → seen_person。该方法目前是 `admin.Server` 的私有方法，helper 不能直接调用。实施时应把解析逻辑抽成 internal 共享 resolver，或在 Hub 构造发往 helper 的 envelope 时写入：

```json
{
  "source_kind": "feishu",
  "source_open_id": "ou_xxx",
  "source_display_name": "示例用户",
  "source_bot_channel_id": "..."
}
```

查不到时 Hub 写 `source_display_name=open_id`。禁止 helper 访问 Hub SQLite 或飞书 API补查。

### 3.3 入队与接纳状态机

```text
收到命令
  └─ assign delivery_id + queue_seq
       └─ emit queued（turn_id 必为空）
            └─ FIFO 到队首且 driver idle
                 └─ driver.deliver
                      ├─ receipt 有 accepted/processing/done + turn_id
                      │    └─ emit admitted（同 delivery_id/queue_seq，新 turn_id）
                      ├─ 尚无 turn_id
                      │    └─ _settle 继续轮询，首次满足时只 emit 一次 admitted
                      └─ deliver 异常/最终 failed 且从未 admitted
                           └─ emit queue_failed，并移出当前项
```

`queued` 与 `admitted` 必须幂等。用 `(delivery_id,lifecycle)` 去重，重连、可靠投递重试和 receipt 轮询不能重复发事件。

`admitted` 后，该项从“排队列表”搬到“正在执行”；收到 turn terminal 后移出当前项，worker 再取下一项。driver 如果在 `deliver()` 内阻塞到接纳，当前状态显示“正在接纳”仍属于队首，不允许后项越过。

### 3.4 每-turn terminal 看门狗

每项 admitted 后必须启动 terminal watchdog，避免 driver 活着但永久漏发 terminal 时冻结整个 FIFO。watchdog 以 `(delivery_id, turn_id)` 绑定；任何同 turn 的 assistant/tool/state 进展事件可刷新最后进展时间，真实 terminal 到达时取消。

新增有限正数配置 `SH_TURN_TERMINAL_TIMEOUT_SECONDS`（建议默认 1800 秒，可按 driver 最长任务调整，但禁止无限）。超过“最后进展时间 + timeout”仍无 terminal 时，在队列锁内 compare-and-set：仅当 current 仍为同一 delivery/turn，才 emit 一次 `state_change(change='turn_completed', outcome='failed', reason='terminal_timeout')`、清 current 并推进下一项。迟到 terminal 只记诊断，不得二次 finish 或影响新回合。提交 2 应在超时推进前 best-effort 调 driver interrupt；interrupt 无论是否确认，队列都不能永久冻结。

## 4. Emit 点（文件、函数与确切位置）

行号以当前源码为准，后续代码移动时以函数名为锚点。

### 4.1 View 用户

现有位置：runtime `sessionhelper/driver_adapter.py:299-319`，`_deliver_human()` 在 `driver.deliver` 前 emit queued、接纳后 emit admitted。

目标改法：

1. `sessionhelper/app.py:298-300` 的 terminal-input 分支仍负责识别输入控制信封。
2. `handle_terminal_input()` 不再让 adapter 启动 `_deliver_human` 线程；完整一条消息在 CR 提交时调用 `UnifiedCommandQueue.enqueue(source='user', text=完整多行文本)`。
3. `enqueue()` 成功、对象已落入 FIFO 后立即 emit `queued`；这是唯一 queued 点。
4. 统一 worker 调 `driver.deliver`，首次 receipt 带真实 `turn_id` 时 emit `admitted`。
5. 删除/停用 `_deliver_human` 自有线程路径，避免同一输入双 emit、双 deliver。

View delivery_id 继续使用 `dw-view-<uuid>`，但由 enqueue 层生成。多行文本不得经过 `.strip()` 破坏内部换行；只移除提交用的最后一个 CR。

### 4.2 Agent 互调

现有入队点：runtime `sessionhelper/app.py:365`，`buffer_if_busy()` 的 `pending_inbound.append(env)`。现有接纳点：runtime `driver_adapter.py:103` 的 `driver.deliver` 及 `_settle()` receipt 轮询。

目标改法：

1. 在 `recv_loop()` 完成系统信封过滤之后，把普通 Agent envelope 交给 `UnifiedCommandQueue.enqueue()`；来源从 `from` session address 解析为 `agent`。
2. `enqueue()` 完成后立刻 emit queued，字段含 `source='agent'`、`display_name=<from session>`。
3. worker 取到队首后调用现有 `adapter.handle`/新的 `adapter.deliver_queued_command`。
4. 将现有 `_settle(eid)` 改为携带完整 `QueuedCommand`；在 immediate receipt 或 poll receipt 首次出现 turn_id 时 emit admitted。

可靠 delivery 信封同样在首次“进入统一队列”时发 queued，而不是每次 inbox claim 时发。delivery_id 作为幂等键，lease 释放/重领不得增加 `queue_seq` 或重复 emit。

### 4.3 飞书入站

helper 侧入队和接纳位置与 Agent 相同，但来源元数据由 Hub 在飞书入站生成 envelope 时补齐。Hub 当前已有 `sender_open_id`、`source_*` 元数据的构造点（`internal/m8/hub.go` 的飞书路由分支），在写 session client 前增加 `source_kind/source_display_name`。

确切动作：

1. Hub 飞书消息确定目标 session、准备 envelope 时，调用共享的 `displayNameForOpenID` resolver；在 `c.write()` 前写入上述四个 `source_*` 字段。
2. helper `enqueue()` 看见 `source_kind='feishu'` 时不再从地址猜测，直接复制显示名/open_id/bot channel 到 `QueuedCommand` 并 emit queued。
3. worker 接纳后以同一字段 emit admitted。

若旧 Hub 尚未携带新字段，helper 可用现有 `is_feishu_addr()` 和 `protocol.py:44-75` 的地址/元数据规则兼容判定，但 `display_name` 只能回退 open_id；这只是滚动升级兼容，不是最终真名方案。

## 5. 事件 schema

继续沿用 `SessionEvent` 顶层，不新增第二条 SSE。queued/admitted 都使用 `kind='user_input'`：

```json
{
  "kind": "user_input",
  "text": "第一行\n第二行",
  "data": {
    "lifecycle": "queued",
    "source": "feishu",
    "display_name": "示例用户",
    "open_id": "ou_xxx",
    "bot_channel_id": "bot_xxx",
    "queue_seq": 42,
    "enqueued_at": "2026-07-15T12:34:56.123Z"
  },
  "session_id": "...",
  "cursor": "queue:00000000000000000042:queued",
  "delivery_id": "dw-...",
  "turn_id": ""
}
```

接纳事件只改变 lifecycle/cursor/turn：

```json
{
  "kind": "user_input",
  "text": "第一行\n第二行",
  "data": {
    "lifecycle": "admitted",
    "source": "feishu",
    "display_name": "示例用户",
    "open_id": "ou_xxx",
    "bot_channel_id": "bot_xxx",
    "queue_seq": 42,
    "enqueued_at": "2026-07-15T12:34:56.123Z",
    "admitted_at": "2026-07-15T12:35:02.010Z"
  },
  "session_id": "...",
  "cursor": "queue:00000000000000000042:admitted",
  "delivery_id": "dw-...",
  "turn_id": "turn-..."
}
```

字段约束：

- `source` 必填且只能是 `user|agent|feishu`。
- `display_name` 必填；飞书查名失败时等于 `open_id`。
- `open_id` 仅飞书必填，其他来源为空字符串或省略。
- `text` 是原始完整输入；不得把摘要回写到该字段。
- `queue_seq` 是会话内严格递增整数，在统一 enqueue 临界区分配；queued/admitted 相同。
- `delivery_id` 是生命周期主键；queued/admitted/turn 原生事件都必须相同。
- queued 的 `turn_id` 必为空；admitted 必须非空。没有 turn_id 不允许为了 UI 提前 admitted。
- `cursor` 是事件去重/重放游标，不替代 `queue_seq`。renderer 按 `queue_seq` 排队，不能依赖不同 driver 的字符串 cursor 比较。

`queue:*` 与 driver 原生 cursor 属于不同命名空间，任何地方都不得直接比较二者。等候区与执行顺序只认 `data.queue_seq`；turn 内事件继续使用该 driver 自己的 cursor 排序。

终结事件扩展：

```json
{
  "kind": "state_change",
  "data": {
    "change": "turn_completed",
    "outcome": "interrupted",
    "interrupted": true,
    "queue_seq": 42
  },
  "delivery_id": "dw-...",
  "turn_id": "turn-..."
}
```

自然完成用 `outcome='completed'`；错误用 `failed`。不要把中断伪装成普通 completed。

## 6. Renderer 规格

### 6.1 常驻等候区

基线当前会在 `waitingDeliveries.size===0` 时隐藏等候区（`renderer-canary-sse.html:1024`），需改为始终可见：

```text
┌─────────────────────────────────────────┐
│ ▶ 正在执行：🤖 coordinator · 指令摘要       │
├─────────────────────────────────────────┤
│ 🪽 示例用户                               │
│ 第一行                                  │
│ 第二行                                  │
│                                         │
│ 👤 用户                                 │
│ 下一条多行指令……                        │
└─────────────────────────────────────────┘
```

没有 admitted/正在接纳项时，顶部显示“▶ 当前无执行指令”；没有 queued 项时，下方显示“暂无排队指令”。队列项按 `queue_seq` 升序，不能按事件到达时间或 cursor 字符串重排。

来源图标：

- 飞书：使用仓库内的通用透明图标资源；实现不得依赖外部网络请求或本机绝对路径。
- 用户：`👤`。
- Agent：`🤖`。

飞书图标应有固定宽高和 `alt='飞书'`，不能把 6KB data URI插入 `innerHTML` 的未转义动态字段。

### 6.2 摘要与多行卡

只有顶部“正在执行”标题使用摘要，建议取第一非空行并按 Unicode 字符限制 60 字，末尾加省略号。队列项和 turn 卡片显示完整 `text`，使用 `textContent`/HTML escape 与 `white-space: pre-wrap; overflow-wrap:anywhere`。不能用 `users.map(...).join('')` 合并两条不同 user 事件；同一 turn 若出现重复 admitted，按 `delivery_id+lifecycle` 去重。

### 6.3 一卡一轮与搬迁

renderer 维护：

- `waitingDeliveries: delivery_id → queued event`
- `currentDelivery: delivery_id|null`
- `turnGroups: turn_id → group`
- `seenLifecycle: delivery_id → set(lifecycle)`

queued：创建/更新等候项。admitted：原子删除同 delivery_id 等候项，设为 current，并将这一条 user event 放入 `turn_id` 对应卡。后续原生 userMessage 如携带相同 delivery_id，必须去重，不能再造 user section。turn terminal：清 current，保留卡片，等待下一个 admitted。

### 6.4 tool 与子 Agent

统一使用 `kind='tool_call'`，以 `data.category` 区分 `tool|bash|subagent`。执行中可在折叠区用不同标题；只要收到该 turn 的 terminal event，`tools.length && !completed` 规则会同时隐藏普通工具和子 Agent。不要把子 Agent 最终文本当工具结果删除；只有 tool_call 进度隐藏。

### 6.5 已中断展示

收到 `turn_completed outcome=interrupted` 后：

- 保留 user 输入与已经到达的 assistant_text。
- 隐藏 tool/subagent 进度。
- footer 显示“■ 已中断”，不用绿色完成勾。
- current 清空；下一 queued 的 admitted 到达后正常搬迁。

## 7. ESC 中断链路

### 7.1 浏览器行为

renderer 同时支持顶部中断按钮和键盘 ESC。只在没有输入法组合（`event.isComposing=false`）、没有打开 modal，且当前存在 executing delivery 时触发；`preventDefault()`，并做 500ms 单飞去重。

请求：

```http
POST /interrupt
Content-Type: application/json

{"delivery_id":"dw-...","turn_id":"turn-..."}
```

成功返回 HTTP 202：

```json
{"ok":true,"accepted":true,"delivery_id":"dw-...","turn_id":"turn-..."}
```

202 只表示控制请求已受理，页面必须等 SSE 的 interrupted terminal event 才标“已中断”。若当前项已经自然完成，返回 409 `no_active_turn`，renderer 刷新状态但不制造中断事件。

### 7.2 View Service

在 `view-service/view_service.py`：

1. `do_POST()` 增加 `/interrupt` 分支，与 `/input` 使用同一 `_session()`、viewer_id、writer token。
2. 代理到 `POST /internal/view/{session}/interrupt`，传 viewer_id/token/delivery_id/turn_id。
3. 不生成 CR，不调用 `/input`，不把 ESC 编码成普通文本。
4. 透传 Hub 的 202/403/409/503；当前 `/input` 总返回 200+`ok:false` 的模式不适合中断控制。

### 7.3 Hub 路由（Hub 变更范围）

在 `internal/m8/terminal.go` 新增 `HandleInternalViewInterrupt`，并在 Hub mux 注册：

```text
POST /internal/view/{session}/interrupt
```

处理步骤：

1. 复用 `routeTerminalInput` 的 viewer writer 校验（`terminal.go:787-801`）：session 存在、viewer 是当前 writer、token 匹配且未过期。
2. Hub 可用最近 SSE 快照做 best-effort 预筛，但它不是 stale-turn CAS 权威；SSE 可能落后于 helper，Hub 不得仅因快照不一致就拒绝合法 ESC。Hub 应把 delivery_id/turn_id 原样透传给 helper。
3. 向 session client 发送独立控制 envelope：

```json
{
  "body":"",
  "meta":{
    "type":"terminal_interrupt",
    "system":true,
    "no_mirror":true,
    "delivery_id":"dw-...",
    "turn_id":"turn-..."
  }
}
```

4. 该 envelope 必须走 session WebSocket 控制通道并优先于普通消息，不能写入可靠业务 delivery inbox 或飞书镜像。

可复用旧网页终端的鉴权和 session client `c.write()`；不可直接复用 `routeTerminalInput(...,"\x03")`，因为它丢失目标 turn，且 helper 会把 terminal_input 当数据路径处理。

### 7.4 Helper

在 `sessionhelper/app.py` 的 `recv_loop()` 中，把 `terminal_interrupt` 判断放在 `terminal_input`、`buffer_if_busy` 和 reliable delivery 之前：

```text
if is_terminal_interrupt(env):
    await handle_terminal_interrupt(env)
    continue
```

`handle_terminal_interrupt`：

1. 在统一队列锁内读取 current，并以 delivery_id+turn_id 做 compare-and-set 核对；helper 是 stale-turn CAS 的唯一权威。不匹配回控制 ack `stale`，Hub 再映射为 409。
2. 直接调用 `adapter.interrupt_current()`，内部调用 driver 原语；不得等 `is_idle`。
3. driver 确认中断后，由 driver 的真实 terminal frame/event发 `outcome=interrupted`；helper 只负责补齐 delivery_id、turn_id、queue_seq，不应抢先伪造成功。
4. 若 driver 返回 false/超时，发 `interrupt_failed`，保持 current，不能提前执行下一项。
5. 中断确认后结束当前队列项并唤醒 worker；FIFO 原顺序不变。

sidecar `POST /interrupt` 改为调用同一个 `adapter.interrupt_current()`，避免两份语义漂移。

### 7.5 各 driver 的原语

| Driver | 实现点 | 实际中断方式 | 成功证据 | 降级 |
|---|---|---|---|---|
| Claude | `claudeDriver.py:386-410` | stdin JSONL `control_request` / `subtype=interrupt`；不是发 ESC 或 Ctrl-C | result `subtype=error_during_execution`，active.interrupted=true（`:482-501`） | 无 active turn 返回 false；超时不推进队列 |
| Codex | `codexDriver.py:737-768` | daemon `turn/interrupt {threadId,turnId}` | receipt/turn status 为 interrupted（`:1115-1116`） | API 失败或无证据返回 false |
| OpenCode | `openCodeDriver.py:534-584` | `POST /api/session/{session_id}/interrupt` | `step.failed` 且 error 含 interrupt | 若模型先自然 stop，按 completed，不谎报 interrupted；超时保持 current |

因此 OpenCode 不是 N/A。PTY Ctrl-C 只适用于 legacy CLI adapter；协议 driver 必须使用上表原生 API。若仍保留 legacy PTY driver，调用已有 `write_terminal_input('\x03')`/pexpect sendcontrol('c')，成功证据至少为进程重新出现 prompt；单纯写字符成功不算中断成功。

Codex 实现必须在 admitted 时记录 `delivery_id → (threadId, turnId)`，interrupt 只能使用 current delivery 对应的 active IDs；不得用“最近一个 turn”或可变化的全局字段猜测。测试须覆盖下一 turn 已建立时旧 turn interrupt 到达，断言不会调用错误的 `(threadId, turnId)`。

### 7.6 Driver 重投与恢复约束

`admitting` 恢复不能假设 `driver.deliver(delivery_id, text)` 天然跨进程幂等。提交 2 必须逐 driver 用 fixture/实测证明以下二者之一：相同 delivery_id 重投由 driver/daemon 去重且不会新开 turn；或 driver 提供 by-delivery_id 查询，可恢复原 turn/receipt 而不重投。

恢复顺序固定为“先查询，后决定”。能查到原 turn 时恢复 current；明确查不到且已证明重投幂等时才允许重投。没有查询能力且未证明跨重启幂等的 driver，必须把该 admitting 项标记 `failed(reason='admitting_recovery_unverifiable')` 并继续队列，禁止盲目重投导致命令双跑。

## 8. Hub 变更范围与风险

以下改动会改变 Hub，必须重新构建并发布 Hub 二进制，不能靠替换 renderer/view_service 热更新完成：

1. 新增 `/internal/view/{session}/interrupt` 路由、writer token 鉴权和 stale-turn 防护。
2. 增加 `terminal_interrupt` 控制 envelope。
3. 在飞书→session envelope 上补 `source_kind/source_open_id/source_display_name/source_bot_channel_id`。真名 resolver 的 member → chat_entity → seen_person 三段 Repo 查询必须在 `h.mu` 外执行。
4. 把 `displayNameForOpenID` 的查找逻辑抽为 Hub/admin 可共享 resolver（不改变查找优先级）。
5. helper interrupt ack 是 delivery_id+turn_id stale-turn CAS 的唯一权威。Hub 若维护最近 admitted/terminal，只可用于展示或 best-effort 预筛，不能形成第二道权威 CAS。

Hub 锁序硬约束：interrupt 控制 envelope 必须照 `routeToSession` 模式，在 `h.mu` 内只抓 session client 指针，释放 `h.mu` 后再 `c.write()`；禁止持 `h.mu` 访问 Repo/数据库、等待网络写或获取 client 内部锁。per-session current-turn 快照也只能在短临界区读写纯内存值，不得在同一临界区调用 resolver、DB 或 `c.write()`。

主要风险：

- 安全：interrupt 与输入同等级写权限，绝不能只凭 session 名调用。
- 竞态：旧 tab 的 ESC 可能到达下一 turn；delivery_id+turn_id 双重 CAS 是硬要求，唯一权威在 helper。
- 兼容：旧 helper 不认识 `terminal_interrupt` 时应返回 capability unavailable，而非把它喂给模型。
- 真名：飞书 resolver 只读现有 Repo，不新增外部 API阻塞消息投递；查不到立即回退 open_id。
- 兼容：Hub 和 helper 使用 capability `view_interrupt_v1`、`unified_queue_v1` 协商；Hub 仅对声明 capability 的 helper 开放按钮。

不涉及 Hub 的改动：renderer 常驻队列/图标/卡片渲染、View Service `/interrupt` 代理、helper 统一队列、driver mapper 与 interrupt wrapper。但完整 ESC 链路仍需要 Hub 协议支持。

## 9. 持久性、重连与顺序

统一队列不能只靠进程内 deque 达成“可见即可信”。建议把 `QueuedCommand` 和 `next_queue_seq` 放入现有 helper inbox SQLite，同一事务完成：分配序号、按 delivery_id 幂等插入、状态 queued。View 输入也进入该存储。

重启恢复规则：

- queued：按 queue_seq 恢复。
- admitting 且没有可确认 turn：回到队首并依赖 driver delivery 幂等键恢复/查询，不能直接重复开 turn。
- admitted：先向 driver 查询 receipt/turn；仍 processing 则恢复 current，terminal 则补终结状态。
- interrupted/completed/failed：不重新入队，仅供 SSE replay。

SSE 重连后 renderer 通过 `delivery_id+lifecycle` 去重，通过 queue_seq 重建 FIFO。事件到达乱序时，admitted 可先被暂存；若缺 queued，仍能建立 current turn，但记录诊断 `missing_queued`，不能产生孤立的第二张卡。

## 10. 测试与验收

### 10.1 Helper/driver 单元测试

1. 三来源交错 enqueue，断言一个严格 queue_seq 序列和唯一 worker 调用顺序。
2. 每项恰好一对同 delivery_id 的 queued/admitted；queued turn 空、admitted turn 非空。
3. receipt 从 queued→accepted无turn→processing有turn，只有最后一步 emit admitted。
4. reliable delivery lease 重试不重复 queued、不改变 queue_seq。
5. queue full reject-new，已有可见项不丢。
6. ESC 控制 envelope 在 driver busy 时立即调用 interrupt，不进入普通 FIFO。
7. stale delivery/turn 中断被拒，不能打断下一轮。
8. 中断失败/超时不 dequeue；确认成功才启动下一项。
9. `driver.deliver` 阻塞期间 event loop 仍能接收 terminal_interrupt，并发调用 interrupt；不得等 deliver 返回。
10. admitted 后 driver 永不发 terminal：watchdog 恰好 emit 一次 failed/terminal_timeout，清 current 并执行下一项；迟到 terminal 不二次 finish。
11. current 的 pop/切换与 interrupt stale-turn 检查并发，断言同锁 CAS 不会读到半切换状态。

### 10.2 Driver fixtures

- Claude：Agent/Task tool_use 映射 `tool_call category=subagent`；result 后 renderer 隐藏。
- Codex：用当前 daemon 实录一轮 `spawn_agent → wait → completed`，把实际 item type 做 fixture；live 和 history 都映射 `tool_call category=subagent`。
- 三 driver 分别断言 interrupt 请求内容和真实 interrupted 证据；OpenCode 自然完成竞态必须归 completed。
- 三 driver 分别证明“相同 delivery_id 跨重启重投去重”或提供 by-delivery_id 查询 fixture；两者皆无时，admitting 恢复必须 failed，断言不会再次调用 deliver。
- Codex fixture 断言 current delivery 持有准确 `(threadId, turnId)`；旧 turn ESC 不会中断新 turn。

### 10.3 Renderer 浏览器自动化

1. 飞书/Agent/用户各两条交错事件，等候区按 queue_seq 排序，图标和飞书真名正确。
2. 空队列时区域仍可见并显示“暂无排队指令”。
3. 多行输入完整分行，不合并相邻 delivery，不截断。
4. queued→admitted 原子搬迁；每项最终恰好一张 turn 卡、0 orphan。
5. tool、bash、Claude/Codex 子 Agent 执行中可见，turn completed/interrupted 后隐藏。
6. ESC 与按钮只发一次 POST；202 后等 SSE 才标中断。
7. 部分 assistant_text 后中断：文本保留，footer 为“已中断”，下一项顺序不变并立即执行。
8. 旧 tab 对旧 turn 发 ESC，Hub 409；新 turn 不受影响。
9. 1280×900 和 390×844 双视口，等候区常驻、长真名/长 open_id 和多行文本不破版。
10. View 用户从统一队列新路径提交三条：每条恰好一对 queued/admitted，queued turn 空、admitted turn 非空；同 delivery 不双 emit、不双 deliver。这是替换已验证 `_deliver_human` 线程路径的强制 GO 门槛。

### 10.4 真 E2E GO 门槛

至少准备 9 条：三来源各 3 条，多行唯一标记，按已知交错顺序入队。每条都满足：

- 同 delivery_id 的 queued→admitted 成对；queue_seq 顺序一致。
- admitted 绑定新 turn_id；最终一条指令一张卡、0 orphan。
- 飞书图标+真名、Agent/用户图标正确。
- tool/子 Agent 完成后隐藏。
- 中断其中一个长回合：部分输出保留并标“已中断”，下一项自动执行且不重排。
- 刷新/SSE 重连后队列和 current 不重复、不丢失。
