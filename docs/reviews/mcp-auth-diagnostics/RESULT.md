# S-MCP-AUTH-DIAG 实现核验

- 基线：`0de00bc45d2716cb7ba22b5642dca6f9e73f229f`（r56.2）
- 目标分支：`togpt60lunazzc/mcp-auth-diag`
- 范围：授权表单说明、callback 五类失败原因、安全 HTML/JSON 说明、结构化日志、未命中目标的内部分类及回归测试；不部署。

## 验证

| 检查 | 命令／结果 |
|---|---|
| 三个目标包 | `go test ./internal/feishulogin ./internal/mcpgw ./internal/extseat -count=1`：PASS |
| m8 全包 | `go test -p 1 ./internal/m8`：PASS，217.872s |
| 真浏览器 | `DW_BROWSER_E2E=1 NODE_PATH=/tmp/dw-filecard-playwright/node_modules go test ./internal/mcpgw -run '^TestBrowserCallbackExplainsMissingTarget$' -count=1 -v`：PASS；真实 TLS Hub 与飞书替身，浏览器表单提交后自行完成回调导航，页面可见“目标 Agent 不存在或不可用”与 `access_denied`，`CSP_ERRORS 0` |
| OpenSpec | `OPENSPEC_TELEMETRY=0 npm exec --yes --package=@fission-ai/openspec@1.16.0 -- openspec validate --all --strict --no-interactive`：PASS，30/30 |
| 变异 | `python3 docs/reviews/mcp-auth-diagnostics/mutations.py`：10/10 判红且命中指定语义断言标记，详见脚本 |
| 构建测试门 | `PATH=/tmp/r41-item2-locked-venv/bin:$PATH ./build.sh --test-only`：PASS；两个 helper profile 各 1129/1129（1120＋9），Python 27 项、shell 2 项，`BUILD_TESTS_OK`。系统 `/usr/bin/python3` 首次尝试的 helper discovery 未完成；锁定 venv 重跑通过。 |

完整构建测试原文保存在执行环境 `/tmp/mcp-auth-diagnostics-final-build.log`；浏览器原文保存在 `/tmp/mcp-auth-diagnostics-browser.log`；变异原文保存在 `/tmp/mcp-auth-diagnostics-mutations.log`。变异脚本逐项恢复被测文件，10 个判红标记覆盖表单提示、目标缺失分类、原因映射、页面说明、attempt 前缀、UTC、敏感信息、OAuth error、Origin 守卫与浏览器 CSP 断言。

## 安全结果

回调日志只写 `reason_code`、`attempt_prefix`、`target_category`、`utc`；目标名、授权 code、访问/本地 token、open_id 和正文均不写。HTML 与非 HTML 响应保留原 OAuth error 值，新增固定安全说明。来源守卫拒绝也使用同一日志与说明出口。
