# 提案：MCP CacheableResult 完整性

## Why
MCP 2026-07-28 的 `ListToolsResult` 将 `ttlMs` 与 `cacheScope` 定义为必填字段。Claude Code 2.1.292 已在实际接入时拒绝缺少字段的结果，造成 OAuth 与飞书授权成功后工具仍无法加载。本变更补齐 2026 codec 的缓存元数据，并通过协议结构与变异检查防止回归；不改变 2025 codec，也不扩展未实现的 prompts/resources。

## 来源
https://modelcontextprotocol.io/specification/2026-07-28/changelog

## 范围
仅影响 `internal/mcpgw` 的 2026-07-28 `tools/list` 响应及相关测试证据。

## 待确认假设
无。

## 待 PM 决策
无。
