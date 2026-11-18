# 设计

## 基线

基于 `9818b979` 读码：Hub 连接能力从 helper `session_capabilities` 读取，ACK v2 仅在 `delivery_ack_v2=true` 且 `processing_mode=driver` 时启用（`internal/m8/hub.go:4623`）；ACK state 入口白名单与非 v2 回退在 `hub.go:4751-4765`，v2 phase 校验在 `internal/m8/delivery_ack_v2.go:20-43`。Hub 当前将 `failed` 限于 `session_unavailable` 且不可重试。helper 容量溢出在 `tools/sessionhelper/sessionhelper/app.py:1506` 一带会发 `busy, reason=queue_full, retryable=true`，而 ACK outbox 只包含已成功持久接收的 inbox 行，容量溢出行无法入本地 outbox。

发送方通知已有 durable 路径：delivery 终态事务经 `enqueueDeliveryFailureNoticeTx` 建立 `failure_notice` delivery，按原 failed delivery ID 做确定 ID 去重；通知队列失败由 Hub delivery drain 重试。N-1b 应扩展这一路径，不另造内存通知。

## 协议与字段

合法新 ACK：

```json
{"type":"delivery_ack","delivery_id":"<原 delivery id>","ack_protocol":2,"processing_mode":"driver","ack_generation":1,"state":"failed","reason":"queue_full","retryable":false,"occurred_at":"<RFC3339Nano>"}
```

Hub 接受条件全部成立：当前连接确为该 session 的 current connection；连接声明 `delivery_ack_v2` 与 `delivery_ack_queue_full_v1`；ACK protocol/mode/generation 与持久 delivery 完全相同；delivery 尚无 injected/admitted/processed 证据；`state=failed`、`reason=queue_full`、`retryable=false` 三者精确匹配。实现需将 `queue_full` 加入明确的 delivery failure reason allowlist 与安全显示文案。其他组合保持忽略或既有校验；不得把队列忙、超时、unknown 或一般 failed 翻译成 queue_full。

能力采用双向确认以避免新 helper 对旧 Hub 发新语义：helper 在 `session_capabilities` 增加 `delivery_ack_queue_full_v1=true`；新 Hub 的 WebSocket 握手回 `X-DW-Queue-Full-ACK: queue-full/v1`。helper 仅在该响应头精确匹配且本连接已发送能力后，对本地持久 inbox `accept` 因容量超限失败的当前 delivery 直接回新 ACK。该拒收发生在 inbox 行/outbox 创建之前，故不伪造本地 ACK outbox 行；ACK 丢失时 Hub 重投同一 delivery，helper 再次按同一确定容量事实拒收，Hub terminal update 与 notice ID 均幂等。任何可能已持久化、已派发或提交不确定的 delivery 不得走该 ACK。

## 兼容矩阵

| Hub | Helper | 行为 |
| --- | --- | --- |
| 新 | 新且握手头已确认 | 新 helper 可对确定容量超限回 `failed/queue_full/nonretryable`；Hub 终结原 delivery 并排 durable notice。 |
| 新 | 旧，未声明新能力 | 不启用扩展；继续 ACK v1 或旧 busy 重试语义。 |
| 旧 | 新，握手头缺失 | 新 helper 不发新终态，继续现有 `busy/queue_full/retryable=true`；旧 Hub 不会误收未知字段/状态。 |
| 旧 | 旧 | 原行为逐字节保持。 |

新 Hub 必须在 WebSocket handshake response headers 增加能力头，但不能仅因 server 支持就将其当作 helper 能力；新 helper 不能仅凭请求头或本地版本猜测 Hub 能力。

## Hub 状态、outbox 与通知

合法拒收在 ACK v2 phase handler 内，以仓储事务将原 `session_delivery` 更新为 dead，reason 固定 `queue_full`，清除 lease，并在同一事务建立发送方可见通知。发信方是已支持 durable failure notice 的 session 时，复用 `enqueueDeliveryFailureNoticeTx` / `failure_notice` `session_delivery` outbox，并将 `queue_full` 纳入 `DeliveryFailureReasonAllowed` 与 `failureNoticeReasonText`；保留 source type 限制和防递归。其他已支持来源由新增有界 `delivery_failure_notice_outbox` 记录 failed delivery ID、确定 notice ID、路由目标引用与模板码，不复制原正文；dispatcher 用稳定 notice ID 投递，只有确认未发送的错误才重试，发送结果不确定时转 manual/unknown 并告警，避免重复通知。

发送方文案固定为：`发送未进入目标收件箱：目标 Agent 忙且收件箱已满；本条未提交，请缩短排队或稍后重新发送。` 通知只包含原 delivery ID 和 reason code，不带正文、原始账户标识或任意 helper 文本；按原 delivery ID 唯一去重。View 的 delivery_update 以 `stage=helper_received,status=failed,reason=queue_full` 显示明确拒收。sender outbox dispatch 失败会按有界退避重试，持久化 attempt/next_attempt_at；达到上限后保留 dead-letter 记录和可观测告警，不能静默删除。

若 ACK transaction 失败，Hub 不确认该 phase、保留现有 lease/retry recovery；若确认写回 helper 失败，helper 重发，Hub 依据相同 generation 与终态幂等确认、不重复通知。failure notice outbox 投递失败继续由 Hub durable delivery drain 重试。

## 实现迁移与改动面

预期改动 `internal/m8/hub.go`、`internal/m8/delivery_ack_v2.go`、仓储 delivery state/terminal transaction、failure notice reason allowlist/文案、Hub WS handshake headers、generic sender-notice outbox dispatcher、helper `app.py` 与 capacity ACK queue tests。N-1b 基于 R60 最终锚另开分支，不与 coder56 当前的 `session_ingress` 改动并行。

## 验收与删保护

- 新 Hub＋新 helper 双能力：容量满时一条原 delivery 终态、无第二次 writer submit，session/Feishu 等支持来源都能收到一条 durable 可见拒收通知；generic outbox 达上限或路由结果不确定时有 dead-letter/manual 状态与告警。
- Hub ACK commit 前断线、ACK confirm 写失败、Hub 重启、notice target 离线：同一 delivery/request ID 最终只产生一条 notice，不误判 processed。
- 旧 Hub 缺响应头时新 helper 保持旧 busy ACK；旧 helper 缺能力时新 Hub 拒绝新 ACK 并保持旧路径；四组合矩阵覆盖 v1 与 v2。
- 只删握手头检查、只删 helper capability gate、放宽 reason/retryable 组合、删 reason allowlist、删 terminal transaction 中的 notice enqueue、删两类 notice dedupe、删 retry/dead-letter 保护、把 busy/unknown 映射成 queue_full，各自必须在对应业务断言转红。
- 未持久接收之外的队列等待不发 `queue_full`；已开始/不确定投递不进入容量拒收通道。
