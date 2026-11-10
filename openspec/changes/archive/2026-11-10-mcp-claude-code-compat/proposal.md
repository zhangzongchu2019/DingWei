# MCP Gateway Claude Code localhost 回调兼容

## Why
Claude Code 2.1.234 使用其公开 CIMD 元数据注册无端口的 `http://localhost/callback`，并在授权请求中提供动态端口；现有网关拒绝 localhost 和 `refresh_token` 元数据，导致 OAuth 在 `invalid_client` 终止。本变更按 RFC 8252 §7.3 接受受限回环回调，并保持 token 端点只支持授权码、Codex 身份策略不变。来源：㊿-补54 [b96475316](https://git.code.tencent.com/wego-ai/dingwei/commit/b96475316)。

## What Changes
- 支持 Claude Code CIMD 中精确 `localhost` HTTP 回环回调，动态端口匹配且路径、查询精确。
- 元数据可额外声明 `refresh_token`，但 token 端点不签发或兑换 refresh token。
- 连接页 CSP 仅增加本次已校验回调的精确 localhost 端口源，并以真实 Chromium 验证立即跳转和同意页链路。
- 明确 Codex 专用身份继续限定 IP 回环地址。

## Capabilities
### New Capabilities
- `mcp-claude-code-compat`: Claude Code CIMD OAuth 回环兼容边界。

### Modified Capabilities
- `mcpgw-codex-compat`: 澄清通用 CIMD localhost 规则不扩大 Codex 专用身份。

## Impact
影响 `internal/mcpgw` 客户端元数据校验、回调匹配、连接页 CSP、接口说明及对应测试；不改变 token grant、scope、PKCE、授权身份和 Codex 客户端回调规则。

## 待确认假设
- 依据派单，Claude Code 原文 CIMD 的 `client_uri` 等字段维持现有解析要求；仅验证与本变更有关的字段。

## 待 PM 决策
- 无；需求边界由本单派工与㊿-补54裁定给定。
