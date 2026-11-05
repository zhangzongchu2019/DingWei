# S-R51-TIMELINE-ORDER rework evidence

## Scope and implementation

基于原派单基线 `113ac4befcbfafe9f392ab679d7c3eab137f9129`，当前工作从退回 tip `b5677648` 继续。只改 renderer、timeline browser regression 与本报告；未改 Hub/helper、wire protocol、schema、migration、release/version 位。

- `view-service/renderer-canary-sse.html`：实时 SSE 用 `Last-Event-ID` 的 Hub epoch/seq 标记事件；历史 API 用 row cursor seq 标记事件。Hub 序号可用时按序号二分定位，不再扫描每张卡的 DOM 时间标签。`cli_injected` 确认到达后，以对应 `input_posted` 的 Hub 序号为分界，把同一 turn 拆成前后 segment；即使确认晚于输出也能分开文本。分页先读到确认、再上翻读到输入时，暂存确认栅栏并在输入到达时补建 segment。
- `view-service/renderer-timeline-order-regression.js`：覆盖 Claude 完整文本块、Codex 同 item 重复 cursor 的 delta、Hub 实时 SSE、刷新重放、历史上翻（确认先于输入载入）、回合结束后才到的确认、空闲对照，以及 append/no-split 两刀。
- 真实样本：**待 r51 预发**。夹具为读码对照形状，不冒充现网采样。

## Read-code event shape and clocks

以下均为当前基线代码的读码结果，fixture 对齐这些字段：

1. Claude 输出块：`tools/sessionhelper/sessionhelper/claudeDriver.py:624-632` 从 assistant message 的 text content 发一个 `SessionEvent(kind="assistant_text", text=完整块, data={})`；每段文本块各自一条事件。
2. Codex 输出增量：`tools/sessionhelper/sessionhelper/codexDriver.py:1444-1454` 每个 `item/agentMessage/delta` 发一条 `assistant_text`，携带 `data.item_id/thread_id` 与 item cursor；同一 item 的多片 delta 可共用 cursor。夹具同时覆盖注入点前后各两片、同 item/cursor 的形状。
3. 两类 helper `SessionEvent` 的 JSON 信封与 `ts`：`tools/sessionhelper/sessionhelper/driver_adapter.py:939-951`；`ts=int(time.time()*1000)` 是 helper 主机 Unix epoch 毫秒，不是 Hub、浏览器或查看者时钟。
4. 输入卡：`internal/m8/terminal.go:754-779,782-806` 的 Hub `publishInputObservation` 用 `time.Now().UTC()` 写 `updated_at`，没有给该事件写 `ts`。Hub 随后在 `internal/m8/terminal.go:636-655` 递增该会话 event ring seq 并发布 SSE ID。
5. 注入确认：`tools/sessionhelper/sessionhelper/inbox_ack.py:38-48` 从 helper 持久化 admission 时间写 `delivery_ack.occurred_at`；Hub `internal/m8/delivery_ack_v2.go:42-65` 接收后提交 ACK。`internal/m8/hub.go:4684-4706` 再发布 `delivery_update`，包含 `turn_id`、helper 提供的 `occurred_at`；`internal/m8/terminal.go:885-912` 以 Hub `h.now()` 写 `updated_at`，并由同一个 Hub ring 分配序号。
6. 因此跨机器墙钟可能有偏差：助手 `ts` 可能看起来早于/晚于 Hub 输入时间，几百毫秒或更大偏差都不能用来推断插入位置。本修复对同一 Hub event epoch 以 Hub ring seq 排序，并在输入卡的 seq 处分段；不以 helper/Hub 的墙钟差做比较。`delivery_update` 是注入 ACK 锚点，`input_posted` 的 Hub 序号则保留用户卡在本回合事件流中的位置。

夹具采用与代码一致的形状：Hub `input_posted` / `delivery_update` 用 ISO `updated_at`（输入无 `ts`），助手事件使用 helper envelope 的整数毫秒 `ts`；history row 含 `{cursor:{generation,seq},event_id,received_at,event}`；实时 SSE 使用 `{epoch}-{seq}` ID。两种 helper 分段形状分别独立测试。实际预发序列仍待业主在 Console 发出回合中输入后核对。

## Readings

- Bundled Chromium 与系统 Chrome，空闲各 10 轮：`realtime=10`、Claude active `20`、Codex active `20`、refresh replay `20`、older history page `10`、after-end `10`、idle `10`，全绿。最终 renderer SHA-256 `d26a2485b636eed4131b175dba91a5edb8292dd5f388dd9afe227e518089748f`。
- Bundled Chromium 与系统 Chrome，16 个并行 CPU-bound worker 下同一 10 轮矩阵均全绿。
- 两刀均 rc=1，失败在原判据 `timeline order invalid: late accepted user card must be between earlier and later same-turn output`：`--mutate-append` 与 `--mutate-no-split`。
- 2000 个已有 timeline 节点后插入 100 项：bundled Chromium 空闲读数 `2.40ms`、CPU 满载 `2.40ms`；系统 Chrome 空闲 `2.50ms`、CPU 满载 `9.30ms`；每轮最终 2100 children。序号路径使用二分 DOM 定位，没有逐子节点 `querySelectorAll`。
- `renderer-markdown-image-regression.js`：bundled Chromium、系统 Chrome 均 15 pass / 0 fail。
- `tests/file-card-streaming.cjs`：bundled Chromium、系统 Chrome 均 12 pass / 0 fail。
- `delivery-ack-v2-browser-regression.js`：bundled Chromium 与系统 Chrome 各 46 assertions / 0 failed。
- `r15-session-history-browser-regression.js`：bundled Chromium 与系统 Chrome 均通过，100+100 页、实时衔接、cursor 排他与锚点读数 `0.34px`。
- `view-service/r15-session-history-mutations.js`：三刀均 rc 2，分别红在 `timeline order invalid`、`scroll anchor shifted`、`post-live history prefix was evicted`。
- `scripts/run-build-tests.sh`：使用锁定环境 `PATH=/tmp/dw-filecard-build-venv/bin:/usr/bin:/bin scripts/run-build-tests.sh`，rc=0，末行为 `BUILD_TESTS_OK helper_profiles=2 python=27 shell=2`；helper 两 profile 各 `1097/1097`。
- `openspec validate r51-console-timeline-order --strict`：valid。

## Handoff

真实样本待 r51 预发。完成剩余验证后提交、推送并核对 `ls-remote`，再回报分支、tip 与读数；交件后冻结。本提交保持在 `b5677648` 之后，未改基线。
