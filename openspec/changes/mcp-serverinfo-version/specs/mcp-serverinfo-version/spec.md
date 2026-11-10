# MCP serverInfo 版本来源

## Purpose
本变更把 MCP 2026-07-28 与 2025-06-18 serverInfo 的版本来源从历史字面量改为 `internal/version.Version`，目的是避免发行版本与网关身份显示不一致。涉及 internal/mcpgw，不影响 tools/list、tools/call 的协议字段边界或错误响应。

## ADDED Requirements

### Requirement: SR-001 serverInfo 版本共用发行常量
2026-07-28 的 server/discover、tools/list、tools/call 成功结果及 2025-06-18 initialize MUST 使用相同的 serverInfo name 与 version；version MUST 等于 `internal/version.Version`。

#### Scenario: 2026 三种结果版本一致
- WHEN 请求分别成功返回 server/discover、tools/list、tools/call
- THEN 各结果 serverInfo.version 等于 `internal/version.Version` 且三者相同

#### Scenario: 2025 initialize 使用共享版本
- WHEN 客户端通过 2025-06-18 codec 请求 initialize
- THEN 顶层 serverInfo.name 与 serverInfo.version 分别等于共享名称常量和 `internal/version.Version`

#### Scenario: 旧协议工具结果结构保持独立
- WHEN 客户端通过 2025-06-18 codec 请求 tools/list 或 tools/call
- THEN 工具结果不带 2026 的 `_meta.io.modelcontextprotocol/serverInfo`
