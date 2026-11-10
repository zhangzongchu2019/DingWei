# MCP CacheableResult

## Purpose
本变更把 2026-07-28 MCP tools/list 从缺少缓存元数据改为返回完整 CacheableResult，目的是通过严格客户端的结果校验并限定授权响应缓存范围。涉及 internal/mcpgw，不影响 2025-06-18 codec 与其他未实现方法。

## ADDED Requirements

### Requirement: SR-001 2026 tools/list 提供 CacheableResult
2026-07-28 codec MUST 在 tools/list 结果中返回正整数 ttlMs 与值为 public 或 private 的 cacheScope。

#### Scenario: 严格客户端校验 tools/list
- WHEN 客户端请求 tools/list
- THEN 结果包含正整数 ttlMs、合法 cacheScope、complete resultType 和确定顺序的工具列表

### Requirement: SR-002 旧 codec 保持独立
2025-06-18 codec MUST 保持原有结果结构，不得新增 2026-07-28 CacheableResult 字段。

#### Scenario: 旧协议工具列表
- WHEN 客户端通过 2025-06-18 codec 请求 tools/list
- THEN 返回仍符合该 codec 现有结构
