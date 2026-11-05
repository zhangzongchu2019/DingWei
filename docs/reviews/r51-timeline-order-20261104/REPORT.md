# S-R51-TIMELINE-ORDER rework evidence

## Scope and implementation

基于原派单基线 `113ac4befcbfafe9f392ab679d7c3eab137f9129`，当前工作从退回 tip `b5677648` 继续。只改 renderer、timeline browser regression 与本报告；未改 Hub/helper、wire protocol、schema、migration、release/version 位。

- `view-service/renderer-canary-sse.html`：为首次观察到的 Hub epoch／R15 history generation 分配单调 rank，同一 rank 内再比 seq；不同 epoch 的同 turn 输出分成独立 timeline group。回合组的最小排序键随事件增量维护，流式片段不再全量 map/min，也不再重复移动位置未变的组节点。二分查找使用独立的 sequenced-node 索引，无序号通知与缺口提示可留在相邻 DOM 子节点之间，不参与该索引。
- `view-service/renderer-timeline-order-regression.js`：覆盖 Claude 完整文本块、Codex 同 item 重复 cursor 的 delta、Hub 实时 SSE、刷新重放、历史上翻（确认先于输入载入）、回合结束后才到的确认、空闲对照、无序号通知夹入，以及 append/no-split 两刀。
- `view-service/renderer-epoch-order-regression.js`：正式回归 Hub 重启后新 SSE epoch 的低 seq 排在旧内容之后；还验证同一 turn 在 Hub epoch 与 history generation 改变时分组/排序仍保持先后。`--mutate-drop-epoch` 变异必须红。
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

## Second rework readings (2026-11-05)

- Final renderer SHA-256: `2bf883a284cb7f791d03cda14d000bf021cfffe4151b25e62d99c1d9fa786b3a`.
- Bundled Chromium and system Chrome: idle 10-round and 16-worker loaded 10-round timeline matrices all passed. Each reported `realtime=10`, Claude active `20`, Codex active `20`, refresh replay `20`, older history page `10`, after-end `10`, idle `10`; the unsequenced-notice ordering cell passed. Insert benchmark (2000 existing nodes + 100) was 3.20ms / 3.20ms idle and 9.40ms / 8.50ms loaded, respectively.
- Flood stress regression, final renderer: rc0 `ALL PASS`; flood pushed and received 100,000/100,000 at 5993/5991 events/s; late-order pushed and received 30,000/30,000 at 5983/5977 events/s. Flood longtasks 0 (max 0ms, total 0ms), agents maximum gap 1002ms, final text exact, `big_sorts=0`. Late-order longtasks 0, agents maximum gap 1002ms, text exact. The reviewer-provided baseline was longtask 0 and agents maximum gap 1003ms; current readings are at the same scale.
- All three order mutations were detected: append and no-split each exited rc1 on the existing late-input ordering assertion; `--mutate-drop-epoch` exited rc1 on the restart-order assertion. The epoch regression printed `EPOCH_CHECK_PASS` and `HISTORY_GENERATION_CHECK_PASS` on the unmutated source.
- `renderer-markdown-image-regression.js`: bundled Chromium and system Chrome each 15 pass / 0 fail.
- `tests/file-card-streaming.cjs`: bundled Chromium and system Chrome each 12 pass / 0 fail.
- `delivery-ack-v2-browser-regression.js`: bundled Chromium and system Chrome each 46 assertions / 0 failed.
- `r15-session-history-browser-regression.js`: bundled Chromium and system Chrome both passed, including 100+100 rows, live continuation, exclusive cursors and anchor delta `0.34px`. One Chrome run while other browser jobs were competing timed out at the user-scroll assertion; isolated reruns on both browsers passed.
- `view-service/r15-session-history-mutations.js`: bundled Chromium and system Chrome each detected all three inherited mutations at `timeline order invalid`, `scroll anchor shifted`, and `post-live history prefix was evicted`.
- `scripts/run-build-tests.sh`: locked environment `PATH=/tmp/dw-filecard-build-venv/bin:/usr/bin:/bin`; rc0, `BUILD_TESTS_OK helper_profiles=2 python=27 shell=2`, helper profiles each `1097/1097`.
- `openspec validate r51-console-timeline-order --strict`: valid.

## Handoff

真实样本待 r51 预发。复核要求的验证已完成；提交并推送后核对 `ls-remote`，再回报分支、tip 与读数；交件后冻结。本提交保持在 `b5677648` 之后，未改基线。
