# 外部席位来源归属上下文

## 基线

截至 2026-11-09，`internal/extseat/outbox.go` 在 PG 事务内校验外部来源及目标 Agent，但投影到 `internal/m8/extseat.go` 时只传递原正文和基础 delivery meta。普通 Hub 路由已有 `routeExternalSeat`，可以按外部 Agent 身份入队并由 Wait 取回；按外部席位完整名称解析的路由此前先经过普通编组门。

## 代码位置

- 身份锁定与 outbox 投影输入：`internal/extseat/outbox.go`、`internal/extseat/types.go`
- session envelope 与外部回复路由：`internal/m8/extseat.go`、`internal/m8/hub.go`
- OpenSpec 变更：`openspec/changes/extseat-sender-attribution`

## 环境和依赖

Go 1.22 项目；E2E 使用 `pgtest.Pool` 建立隔离 PG schema、真实 extseat Service、Hub WebSocket 和 Wait API。浏览器包测试使用 Node 20 以上。Helper 构建测试使用仓库 runtime lock 对应的锁定 venv。
