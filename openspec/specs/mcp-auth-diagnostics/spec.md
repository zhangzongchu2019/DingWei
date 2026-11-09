# MCP 飞书授权诊断

## Purpose

本能力记录 MCP 飞书授权回调的既有边界，目的是约束服务端状态消费与 OAuth 错误兼容。涉及 feishulogin、mcpgw 与 extseat，不影响成功授权与令牌协议。

## Requirements

### Requirement: MAD-000 服务端回调状态边界

飞书授权回调 MUST 只使用服务端保存并一次性消费的 state 和飞书返回的授权 code 决定签发意图；失败时以固定 OAuth error 值结束，不把内部错误正文传回客户端。

#### Scenario: MAD-000-S01 回调状态绑定

- WHEN 飞书浏览器返回 state 与 code
- THEN 服务端消费对应授权状态并以状态内绑定的目标和身份意图签发，不采用 callback query 中的目标或主体字段

#### Scenario: MAD-000-S02 内部失败不回传

- WHEN 授权或本地签发失败
- THEN 回调返回固定 OAuth error JSON，响应不包含底层错误正文
