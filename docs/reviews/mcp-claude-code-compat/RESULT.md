# S-MCP-CLAUDE-COMPAT 复核读数

基线：`opus50tc2k/r56.3-integration` `ee6b0998`。目标分支：`togpt60lunazzc/mcp-claude-compat`。实现按 RFC 8252 §7.3 允许大小写不敏感的精确 HTTP `localhost` 回环回调和动态端口；仍精确匹配 scheme、path、query。CIMD 可声明 `authorization_code` 与 `refresh_token`，但 refresh grant 未加入 token endpoint。Connect CSP 仅加入已验证回调的实际 localhost 端口。Codex 专用身份仍限制为 `127.0.0.1` 与 `::1`。

## 验证

- Go 1.22.2；Node `v22.23.1`；真实 Chromium（`DW_BROWSER_E2E=1`，Playwright）下 Claude Code 两链（立即 302、同意页）和 Codex 两链（立即 302、同意页）均通过，所有链 `CSP_ERRORS 0` 并到达回环回调；原有浏览器授权成功和目标缺失回归也通过。
- `HUB_TEST_PG_STRICT=1 DW_BROWSER_E2E=1 NODE_PATH=/tmp/dw-filecard-playwright/node_modules go test -p 1 -count=1 ./internal/mcpgw ./internal/feishulogin`：rc 0，mcpgw 19.812s，feishulogin 0.011s。
- `HUB_TEST_PG_STRICT=1 go test -p 1 -count=1 ./internal/m8`：rc 0，223.273s。
- `HUB_TEST_PG_STRICT=1 go test -p 2 -timeout 40m ./...`：全仓 rc 0。完整执行时 hosted 808.647s、identity 99.162s、m8 237.822s、mcpgw 7.090s、store 79.437s、store/migrate 62.475s 均通过。增加 Codex 浏览器用例后再次执行同一命令 rc 0；变更包 mcpgw 5.951s、m8 222.764s，其余成功包使用 go test 缓存。
- `PATH=/tmp/mcp-auth-diag-gate-venv/bin:$PATH scripts/run-build-tests.sh`：rc 0，`BUILD_TESTS_OK helper_profiles=2 python=27 shell=2`；Claude/Codex helper profile 各 1129/1129。
- `OPENSPEC_TELEMETRY=0 openspec validate --all --strict --no-interactive`：32/32，0 失败。
- `git diff --check`：通过。

## 变异

`python3 docs/reviews/mcp-claude-code-compat/mutations.py`：4/4 语义判红；脚本明确排除编译失败。

| 刀 | 目标测试/标记 | 读数 |
|---|---|---|
| 恢复拒绝 localhost | `TestClaudeCodeLocalhostRedirectPortAndExactPath` / `CLAUDE_LOCALHOST` | RED |
| 恢复拒绝 refresh_token | `TestClaudeCodeGrantTypesPermitRefreshMetadataOnly` / `CLAUDE_REFRESH_GRANT` | RED |
| CSP 不加入 localhost 源 | `TestWithLoopbackFormActionOnlyExactLoopback` / `CLAUDE_CSP_LOCALHOST_ORIGIN` | RED |
| localhost 路径改前缀匹配 | `TestClaudeCodeLocalhostRedirectPortAndExactPath` / `CLAUDE_LOCALHOST_PATH_EXACT` | RED |

未访问生产飞书、Hub、浏览器登录账号；所有端到端服务均为本地 TLS/飞书替身。
