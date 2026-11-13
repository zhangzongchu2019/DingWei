# 设计

## 未确认可重试状态

统一由 `isRetryableUnknownNorm` 判断：`delivery_status` 为 `unconfirmed_retryable` 或 `retried`，或 `reason` 为 `unconfirmed_retryable`。等待条和“查看未知条目”均显示“Agent 忙时未能确认接收，可能未送达”；`r40PaintQueuedWait` 对这些状态清理并不再建立排队计时标签。原始模型 admission unknown 文案维持前序规格。

## 本地不再跟踪储存

每个标记的 localStorage 键为 `dw-view-unknown-untracked-v1:<encode(session_name)>:<encode(delivery_id)>`，值为毫秒 Unix 时间戳。常量固定定义 30 天 TTL 与 500 条全局上限；页面读取时先删过期或无效值，再按时间戳保留最新 500 项。会话名变化时只把对应会话的 delivery ID 装入内存集合，避免同 ID 跨会话误隐藏。写入或读取 localStorage 失败时捕获异常，仅保持当前页内存标记；不发送请求。单项与批量“不再跟踪”共用储存和显示路径。

## 批量确认

“查看未知条目”内提供“全部不再跟踪”。操作打开原生 `<dialog>`，取消按钮位于首位并显式 autofocus/focus；Enter、Esc 与关闭均按取消处理。只有显式点确认才对打开对话框时捕获且仍在未知集合的条目写入本地标记。

## Hub 只读发现与 `expired_unknown` 设计草案

当前 Hub 没有把 protocol 2 的 `admission_unknown` 转为过期终态的代码：连接期 `ExpireSessionDeliveries` 显式排除 `state='admission_unknown'`；离线全局 `SweepSessionDeliveryFailures` 只处理非 ACK v2 行。终结回合时 `convergeAdmissionUnknownAfterTurn` 只发布 `unconfirmed_retryable` 视图事件，未更新数据库中的 unknown 行。只读证据见 `docs/reviews/view-stale-backlog/REPORT.md`。

草案建议：从持久化进入 `admission_unknown` 的 `updated_at` 起 24 小时仍无 `injected` / `processed` 正证据时，由 Hub 周期扫表转成单独的 `expired_unknown` 分类，不伪装成 `dead`/失败，不发送失败通知；View 明确标为“未知状态已超时，仍不能确认是否执行”。需要保留记录并处理迟到的正证据：允许迟到 ACK 将分类更正到真实 `injected` / `processed`，并发出带原因的更正事件。优点是无界未知列表可收敛、重发入口可被明确封住、跨重启持久；代价是状态模型、数据库迁移、ACK 转换、View 状态映射与审计事件均需同步，错误时钟/误扫可能暂时封住可重发项，且迟到事实必须有可审计的更正语义。该草案不在本变更实现。
