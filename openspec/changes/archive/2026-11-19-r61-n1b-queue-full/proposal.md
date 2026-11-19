# 提案

## Why

N-1a 让确定未提交的 busy delivery 留在 helper FIFO 中，但 inbox 容量满时仍沿现有 `busy` 重试路径处理。本 change 设计一个受限、可协商的 `queue_full` 终态，让容量拒收可被 Hub 确认为终态并通过既有可靠通知交给发信方。

## Scope

只写协议和实现设计，不改 Hub、helper 或迁移。实现须等 R60 定锚后另派。只覆盖 helper 能证明 delivery 尚未持久接收、且 durable inbox 确实无容量的拒收；不覆盖驱动忙等、提交不确定或任意 failed。

## Decisions

- 保留 ACK v2 的 `state=failed` phase，新增严格受限的 `reason=queue_full, retryable=false` 组合；不新增泛化终态字段或重试语义。
- 用独立能力 `delivery_ack_queue_full_v1` 标识 helper 具备此拒收语义。Hub 在 WebSocket 握手响应头 `X-DW-Queue-Full-ACK: queue-full/v1` 广告支持；新 helper 只有看到该头且本连接声明该能力时才发新 ACK。两条件任一缺失就继续现有 `busy/queue_full/retryable=true` 行为。
- Hub 将合法 ACK 作为原始 delivery 的终态拒收，沿同一事务完成 dead 状态与既有 durable failure-notice outbox 写入；通知以原 delivery ID 幂等去重。

## Out of scope

新建 Hub 入站 endpoint、改变 ACK v1、对 uncertain 自动重投、调整容量大小，以及与 N-1a 合并实现。
