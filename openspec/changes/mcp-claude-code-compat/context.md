# Context

- 基线：`opus50tc2k/r56.3-integration` 发布提交 `ee6b0998`；实现分支 `togpt60lunazzc/mcp-claude-compat`。
- 依据：㊿-补54 `b96475316`；Claude Code 2.1.234 CIMD 原文夹具位于 `internal/mcpgw/claude_code_client.original.json`。
- 代码位置：`internal/mcpgw/clients.go`、`oauth.go`、`codex_client.go`、`browser_e2e_test.go`；接口说明 `internal/mcpgw/INTERFACE.md`。
- 浏览器测试为 `DW_BROWSER_E2E=1` opt-in，需 Node >=20 和 Playwright/Chromium；用 TLS Hub、TLS Feishu 替身和本地临时回调，不访问生产身份。
