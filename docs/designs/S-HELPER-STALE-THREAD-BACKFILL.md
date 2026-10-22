# S-HELPER-STALE-THREAD：重连后补显已完成回复

**状态：设计稿；基线 `integration/spec5ff-r36` / `c78c66c2`。** 范围仅为 Codex driver 的 View 正文恢复；不改变投递协议、delivery receipt、processed 状态或 ACK。

## 丢失窗口与现有路径

Codex 的 `item/agentMessage/delta` 是实时正文来源（`tools/sessionhelper/sessionhelper/codexDriver.py:1192-1200`）；`item/completed` 对 `agentMessage` 明确不再发全文，避免把已流式显示的正文累加两遍（`1163-1172`）。`turn/completed` 只产生空正文终态事件（`1097-1136`）。因此 helper/Codex RPC 在 delta 到达期间断开、helper 进程重启清空内存事件队列，且 item 已完成时，View 可缺正文而后续没有 delta 可补。driver 的 `_event_log` 是进程内缓冲并有裁剪（`1057-1071`）；重连桥以无游标 `events()` 重订阅（`driver_adapter.py:443-468`），此路径只回放缓冲，不请求 Codex 历史（`codexDriver.py:854-885`）。重连只重建 RPC，已绑定 thread 要到下一次投递才经 `thread/read`／`thread/resume` 校验恢复；resume 明确 `excludeTurns=True`（`codexDriver.py:648-676,1019-1038`）。虽然 driver 另有 `events(since)`→`thread/turns/list` 全历史转换（`1228-1300`），当前桥没有持久游标调用它。

结构化路径由 `driver_adapter` 将事件编码入队（`driver_adapter.py:588-607`），helper 只以现有 `session_event` 信封上送（`app.py:1867-1880`）。Hub 将其存入结构化 `eventRing`，不写老终端 buffer；View SSE 从 ring 回放（`internal/m8/terminal.go:469-492,937-979`）。所以缺的是 helper 事件桥缺口；Hub ring 只能回放已收到的事件，不能自行读取 Codex thread history。老 `terminal_output` 是另一条原始终端流并附加 buffer（`terminal.go:434-466`），不应用来补结构化回复。

## 最小方案

在 Codex RPC 恢复后，先沿现有精确绑定门验证／恢复原 `thread_id`、工作目录与接收状态；不创建 thread。随后只读请求该 thread 最近有界 turn 列表，筛选包含已完成 `agentMessage` 的 item，按 `(thread_id, turn_id, item.id)` 形成稳定键。将全文作为现有 `assistant_text` 的“完整 item 替换”事件送入原 `session_event` 路径；View 按稳定键把已显示 delta 片段替换为完整正文，未见过的 item 则插入一次。实时 delta 同时携带同一 `item_id`，从而处理“补查期间新 delta 到达”的竞态。重复扫描相同键只更新同一显示项，不追加副本；不能确认 item/turn 身份或历史读取失败时，记录可观测缺口并停止该轮补显，不猜 thread、不发新内容。

恢复时订阅实时通知，再取历史快照并按 item 键合并，避免快照与订阅之间漏事件。只允许 `thread/turns/list` 只读读取及 View 事件发布；禁止 `turn/start`、`thread/inject_items`、重新投递或 ACK/receipt 更新。补显事件标记为 history-backfill，排除在投递状态机和 ACK 计数之外。

## Hub 兼容、验证与撤修格

继续使用旧 Hub 已识别的 `session_event` 信封和 `assistant_text` kind，不增加 envelope 类型；新增字段放在事件 `data` 中。旧 Hub 可转发未知 JSON 字段但旧 View 不具 item-key 替换语义，因此默认关闭补显；仅在 helper 部署配置精确设置 `SH_VIEW_ITEM_BACKFILL=item-backfill/v1`（表示对应 View renderer 已支持替换）后启用，未设置或其他值一律关闭，旧部署保持现有实时路径，不发送可能重复累加的补显事件。不得回退到 `terminal_output`。

验收由本席 `togpt60lunazzc.zzc` 在隔离 Codex daemon／临时 thread 上执行，`opus50tc2k.zzc` 独立审查；不在真实工作 thread 制造测试输出。实现撤修格：删除 completed-item 查询后缺失正文仍红；删除 item-key 替换后 delta＋全文重复红；删除实时订阅先行／合并屏障后快照竞态漏项红；放宽 exact-thread 校验后错误 thread 内容泄露格红；能力门绕过后旧 View 重复显示格红；补显触发新 turn、改变 delivery receipt/ACK 的断言红。另有正例覆盖 helper 重启、RPC 短断重连、同键重复扫描、历史读取失败、活动 turn 中已完成 item、旧 Hub 能力关闭时无重复补发。
