# 提案：为每个 2026 MCP 结果标注服务端身份

## Why
MCP 2026-07-28 changelog 建议服务端在每个结果的 `_meta` 中标注自身身份。当前 `server/discover` 已返回 `io.modelcontextprotocol/serverInfo`，但成功的 `tools/list` 与 `tools/call` 缺少该元数据，客户端因此只能从 discover 单独获取身份。本变更让三个成功结果复用同一来源的服务名与版本，避免响应间身份漂移；不改变错误响应与 2025-06-18 codec。

## 来源
https://modelcontextprotocol.io/specification/2026-07-28/changelog

## 范围
仅为 2026-07-28 的 server/discover、tools/list、成功 tools/call 结果添加一致的 `_meta.io.modelcontextprotocol/serverInfo`。

## 待确认假设
无。

## 待 PM 决策
无。
