# RESULT — S-MCP-SERVERINFO-VERSION

- 基线：`ba7670db87be53d11d636ef68848b0c8feaf33fb`（冻结的 `mcp-serverinfo`）；单提交，分支 `togpt60lunazzc/mcp-serverinfo-version`。
- 改动：2026 serverInfo 版本绑定 `internal/version.Version`；2025 initialize 复用 `serverInfoName` 与 `serverInfoVersion`。2025 tools/list、tools/call 仍不带 `_meta`。
- 边界：`go list ./internal/mcpgw ./internal/version`、`sh scripts/repo-boundary-gate.sh`、`sh scripts/dependency-ratchet.sh` 均通过；外部直接依赖仍为 9，无新增依赖。
- 定向测试：2026 server/discover、tools/list、tools/call 成功响应及 tools/call `isError=true` 的身份版本断言通过；2025 initialize 版本断言通过，2025 tools/list/call 的 `_meta` 缺失断言通过。
- 变异：`python3 tests/mcp-serverinfo-version/mutations.py`：2/2 语义变异按各自标记失败，均非编译失败。2025 initialize 字面量变异红于 `MCP_2025_INITIALIZE_SERVERINFO_VERSION`；2026 字面量变异红于 `MCP_SERVERINFO_VERSION`。
- Go 门：`internal/mcpgw` 全包通过（含 `DW_BROWSER_E2E=1` 真 Chromium）；`go test -p 1 ./internal/m8` 通过；全仓 `go test -p 2 -timeout 40m ./...` 完成且所有包通过。
- 构建门：以 `env -i` 清除继承环境，PATH 前置 lock venv（含 pexpect），短路径 `TMPDIR=/tmp/sinob`、`GOPROXY=off` 运行 `scripts/run-build-tests.sh`，rc 0：`BUILD_TESTS_OK helper_profiles=2 python=27 shell=2`。
- OpenSpec：`openspec validate --all --strict`，35 passed / 0 failed。
- `git diff --check`：通过。
- 部署：未部署。
