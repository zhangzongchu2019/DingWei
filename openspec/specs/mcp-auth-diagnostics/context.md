# 能力上下文：MCP 飞书授权诊断

本能力位于 `internal/feishulogin` 与 `internal/mcpgw`。C 路渲染 `/feishu/connect` 表单、消费飞书 OAuth state 并调用席位签发接口；B 路关联外层 attempt、处理 `/mcp/oauth/feishu/callback` 并完成标准 OAuth 回跳。失败说明只服务授权浏览器，OAuth `error` 字段仍保持既有协议值。

目标 Agent 权威校验由 `internal/extseat.IssueToken` 完成；`identity.ResolveFullName` 未找到时会给出内部缺失分类，但页面与日志不得暴露目标全名。回调 attempt ID 来自 B 的服务端事务，不接受飞书 query 提供。成功路径、凭据生命周期与 MCP scope 不由本能力改变。

日志通过 `slog.Logger` 注入，字段为固定原因码、attempt 前八位、目标存在性类别及 UTC。严禁记录授权 code、访问/本地 token、open_id、请求正文及底层错误原文。原因码与页面说明见 `internal/mcpgw/callback_diagnostics.go`；结构契约见 `internal/feishulogin/types.go`。
