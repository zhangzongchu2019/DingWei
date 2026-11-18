# N-1b 设计核查

基线 `9818b979`。本 change 只读核对 ACK v2 与 helper capacity flow，未改 Hub、helper、数据库或部署。

- ACK v2 当前能力由 helper `session_capabilities.delivery_ack_v2` 与 driver mode 建立；Hub phase validator 限制 `failed` 为 `reason=session_unavailable` 且 `retryable=false`。新语义采用精确受限 `failed/queue_full/retryable=false`，另设 `delivery_ack_queue_full_v1` helper capability 和 `X-DW-Queue-Full-ACK: queue-full/v1` Hub handshake response header 双向协商。
- 容量溢出发生在 helper 持久接收前，不能假造本地 ACK outbox 行。ACK 丢失后由 Hub 重投同 delivery；Hub 的 terminal phase/confirm 幂等，sender notice 复用同一事务的 durable failure-notice outbox 与 `FailureNoticeDeliveryID` 去重。
- 四种新旧 Hub/helper 组合、View 和发信方文案、reason allowlist、重试/重连与删保护已写入 `design.md`、`spec.md`、`tasks.md`。
- 设计未要求任何 Hub 改动现在实施；等 R60 定锚后另派 N-1b 实现。

OpenSpec 1.16.0 `validate r61-n1b-queue-full --strict` valid，rc=0。
