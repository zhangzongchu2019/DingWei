# 变更上下文

## 基线

- 冻结基线：`72d6d1a7949c2f1092d1d64d8f6185367a574564`（r59 View 未知条目后续候选）。
- renderer：`view-service/renderer-canary-sse.html`；定向浏览器回归：`view-service/renderer-delivery-status-regression.js`；全量 runner 使用现存 `renderer_delivery_status` 项，不改清单。
- `renderWaitingDelivery` 在 `helper_queued` 的 v2 driver 条目上创建 `queuedClock`；`r40PaintQueuedWait` 当前只排除 `reason=model_admission_unknown`，因此 retryable/retried 状态仍可出现排队秒数。
- 本地跟踪集合当前只有页面内 `Set`，重载即失；页面会话名来自 `applyStatusData` 的 `session_id` / `session`。

## Hub 只读证据

- `internal/store/sqlite.go::ExpireSessionDeliveries`（基线约第 2198 行）排除 protocol 2 的 `delivered`、`injected`、`admission_unknown`，所以这个 unknown 不会因现有 30 分钟队列期限转死信。
- `internal/store/failure_notice.go::SweepSessionDeliveryFailures`（基线约第 154 行）只扫 `COALESCE(ack_protocol,0)<>2` 的行，不会全局过期这类 ACK v2 unknown。
- `internal/m8/terminal.go::convergeAdmissionUnknownAfterTurn` 对 24 小时内缓存的 unknown 只发布 `unconfirmed_retryable`，不写库；`rememberAdmissionUnknown` 的 24 小时只是内存关联窗口，不是终态转换。
- `internal/m8/view_retry_v2.go` 仅允许持久状态 `admission_unknown` 重发；这也说明若未来有 `expired_unknown`，重发入口和迟到 ACK 规则必须同步设计。

## 约束

- 本变更只改 renderer 与其现有浏览器回归；Hub、helper、版本位和 runner 套件清单不动。
- `localStorage` 仅用于本浏览器显示整理，不作为 Hub 状态或投递事实；写入失败不得影响当前页且不得发请求。
- 24 小时未知显示收起逻辑沿用现有行为。
