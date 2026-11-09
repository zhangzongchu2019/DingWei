# S-EXTSEAT-SENDER 实现核验

- 基线：`togpt60lunazzc/mcp-auth-diag` 冻结提交 `a0b5ec724`。
- 目标分支：`togpt60lunazzc/extseat-sender`；单一提交，待门禁完成。
- View 检查：当前 View 代码无依据 extseat 来源 meta 渲染“外部 Agent”的来源组件；本单未新增 UI。来源通过 Hub 投影的 meta 和正文抬头提供。

## 实现

PG outbox 事务锁定 source owner 显示名/短名和 target owner 短名并存入 Delivery 内部快照。Hub Project 基于该快照集中生成抬头，并写入 `origin_kind=extseat`、`source_display_name`、`source_external=true`。普通 session 的伪造 extseat meta 由 Hub ingress 清除。相同 delivery 重投仍以 `GetSessionDelivery` 比较完整 envelope 字节。

回信 E2E 使用隔离 PG schema、真实 extseat IssueToken/Send/ProjectOne/Wait、Hub session WebSocket 和 `RouteEnvelope`。回复按抬头全名路由，经 `routeExternalSeat` 入队，再由外部 Wait 收取；测试核对 From、To 和正文。

## 验证

| 门 | 结果 |
|---|---|
| `PATH=/opt/node22/bin:$PATH DW_BROWSER_E2E=1 NODE_PATH=/tmp/dw-filecard-playwright/node_modules go test -p 1 -count=1 -v ./internal/mcpgw` | PASS；Node 22.23.1；浏览器用例 `CSP_ERRORS 0` |
| `go test -p 1 -count=1 -v ./internal/extseat` | PASS |
| `go test -p 1 -count=1 -v ./internal/extshared` | PASS |
| `HUB_TEST_PG_STRICT=1 go test -p 1 -count=1 -v ./internal/m8` | PASS，230.810s |
| `PATH=/tmp/mcp-auth-diag-gate-venv/bin:/opt/node22/bin:$PATH ./scripts/run-build-tests.sh` | PASS，`BUILD_TESTS_OK helper_profiles=2 python=27 shell=2` |
| `OPENSPEC_TELEMETRY=0 openspec validate --all --strict --no-interactive`（OpenSpec 1.16.0） | PASS，31/31 |
| `python3 docs/reviews/extseat-sender-attribution/mutations.py` | PASS，5/5 语义判红并命中对应标记；编译失败不计入 |

分支、tip、tree 与远端 `ls-remote` 一致性待最终提交后补录。
