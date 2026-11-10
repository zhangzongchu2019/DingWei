# mcp-claude-code-compat Specification

## Purpose
Claude Code 经 CIMD 接入 MCP 网关时，以受限 HTTP 回环 OAuth 完成真实浏览器登录，并保持最小授权边界。涉及mcpgw客户端解析、回调校验与连接页CSP，不影响Codex专用身份和token端点授权码策略。

## Requirements

### Requirement: MCCL-001 精确 localhost 动态端口回调
通用外部 CIMD MUST 接受注册的无端口 `http://localhost` 回调由请求指定有效动态端口，仅忽略端口且精确匹配scheme、主机、路径及query；主机不区分大小写但必须恰为localhost。HTTPS既有规则不变，其他localhost拼写变体 MUST 拒绝。HTTPS回调规则不变；Codex专用身份仍只允许IP回环。

#### Scenario: MCCL-001-S01 Claude 回调
- **WHEN** Claude Code CIMD 注册 `http://localhost/callback`，请求使用 `http://localhost:<有效端口>/callback`
- **THEN** Resolve及授权接受回调；授权响应到达该端口，换票仍绑定该实际URI

#### Scenario: MCCL-001-S02 路径和主机边界
- **WHEN** 请求改变path/query，或使用localhost带尾点、子域、近似主机
- **THEN** 回调拒绝且不重定向到该URI

### Requirement: MCCL-002 可声明 refresh grant 但不签发
客户端元数据 MUST 包含 `authorization_code` 方可接受，可额外包含 `refresh_token`，并 MUST 拒绝未知grant；token endpoint MUST 继续只处理 `authorization_code`。

#### Scenario: MCCL-002-S01 Claude 元数据grant集
- **WHEN** 原文元数据列出 authorization_code 与 refresh_token，或另含未知grant
- **THEN** 前者Resolve成功，后者拒绝

#### Scenario: MCCL-002-S02 refresh token 请求
- **WHEN** token endpoint 收到 `grant_type=refresh_token`
- **THEN** 返回不支持的grant错误且不签发token

### Requirement: MCCL-003 精确连接页CSP回环源
Connect 页面 MUST 仅把本次已校验的HTTP localhost回调端口作为精确form-action源，不得使用任意端口或通配；既有IP回环源规则保持不变。

#### Scenario: MCCL-003-S01 localhost CSP来源
- **WHEN** 已校验回调为 `http://localhost:<有效端口>/callback`
- **THEN** CSP包含对应精确源且不含通配localhost来源；两条Chromium登录链均无CSP错误并到达回环
