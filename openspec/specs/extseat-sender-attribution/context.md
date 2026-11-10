# 外部席位来源归属上下文

## 基线

截至 2026-11-09，`internal/extseat/outbox.go` 在 PG 事务内校验外部来源及目标 Agent，但投影到 `internal/m8/extseat.go` 时只传递原正文和基础 delivery meta。普通 Hub 路由已有 `routeExternalSeat`，可以按外部 Agent 身份入队并由 Wait 取回；按外部席位完整名称解析的路由此前先经过普通编组门。

## 代码位置

- 身份锁定与 outbox 投影输入：`internal/extseat/outbox.go`、`internal/extseat/types.go`
- session envelope 与外部回复路由：`internal/m8/extseat.go`、`internal/m8/hub.go`
- OpenSpec 变更：`openspec/changes/extseat-sender-attribution`

## 环境和依赖

Go 1.22 项目；E2E 使用 `pgtest.Pool` 建立隔离 PG schema、真实 extseat Service、Hub WebSocket 和 Wait API。浏览器包测试使用 Node 20 以上。Helper 构建测试使用仓库 runtime lock 对应的锁定 venv。

## 2026-11-09 补充

外部 MCP `send_message` 可选 `expect_reply` 在 `internal/mcpgw/tools.go` 解析，REST `/api/external/send_message` 共用同一分发；PG `np_extseat_outbox.expect_reply` 由迁移 `0110_extseat_expect_reply.sql` 持久化，Hub 通过 `internal/m8/extseat.go` 仅对 true 写 reply policy。E2E 通过 AMR completed-turn `process`、Hub `RouteEnvelope` 和 extseat Wait 验证闭环。Helper/全仓构建门要求用 runtime lock 对应 venv；直接系统 Python 不满足依赖。

## r56.4 后续实现位置

同步等待和短名分发：internal/mcpgw/tools.go；只读收件、等待与确认清空：internal/extseat/inbox_controls.go；组只读复核：internal/tempgroup/service.go ReadGroup。自动回程配置与抬头：internal/extshared/external_reply.go；生成/出口保护：internal/m8/reply_to_origin.go 与 extseat.go。变更mcp-sendwait-controls，新增schema迁移为零。

## r56.5 FEISHU-COPY

㊿-补43、㊿-补44、㊿-补65：外部→内部和内部→外部消息在主 PG 事务内向独立 `np_extseat_feishu_copy` outbox 写入；worker 私聊抄送授权主体，复用 r56.4 `extshared.ExternalSenderHeader`，只截断副本，飞书 reply-parent 命中已投递抄送时不回流。迁移为 `internal/store/migrate/migrations/0111_extseat_feishu_copy.sql`，开关 `DW_MCP_FEISHU_COPY_ENABLED` 默认开启。
