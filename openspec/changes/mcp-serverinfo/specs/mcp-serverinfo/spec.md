# MCP serverInfo

## Purpose
本变更把 2026-07-28 MCP 的 tools/list 与成功 tools/call 从缺少服务身份元数据改为与 server/discover 共用同一 serverInfo，目的是让每个成功结果都自描述且身份值稳定。涉及 internal/mcpgw，不影响 JSON-RPC 错误响应与 2025-06-18 codec。

## ADDED Requirements

### Requirement: SR-001 2026 成功结果携带共享 serverInfo
2026-07-28 codec 的 server/discover、tools/list 与 tools/call 成功结果 MUST 在 `_meta.io.modelcontextprotocol/serverInfo` 携带相同的 name 和 version。

#### Scenario: 三类成功结果共享服务身份
- WHEN 客户端分别调用 server/discover、tools/list 和 tools/call 并得到成功结果
- THEN 三个结果均有 serverInfo 且 name、version 完全相同

#### Scenario: 工具级错误结果仍有服务身份
- WHEN tools/call 的结果对象标记 `isError=true`
- THEN 结果对象仍携带与其他 2026 成功结果相同的 serverInfo

#### Scenario: 旧 codec 与 JSON-RPC 错误
- WHEN 请求经 2025-06-18 codec，或 2026 请求返回 JSON-RPC 错误
- THEN 不新增 serverInfo 到结果元数据中
