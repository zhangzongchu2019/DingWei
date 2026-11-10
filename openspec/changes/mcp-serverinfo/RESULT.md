# 结果与门读数

- 基线：`3b6157859f5a329026bfcf43bf95de81909919f8`
- 分支：`togpt60lunazzc/mcp-serverinfo`
- 变更：只在 2026-07-28 路径给 discover、tools/list、成功 tools/call 添加 `_meta.io.modelcontextprotocol/serverInfo`。`serverInfoName` 与 `serverInfoVersion` 是共享常量，三个路径调用同一 helper。错误响应仍提前返回，不加 result metadata。2025-06-18 codec 无产品改动。

## 测试与变异

- 三种 2026 方法结果均带 serverInfo，name/version 与共享常量和彼此一致。
- `toolCallResult(data, true)` 的工具结果构造测试确认 `isError=true` 时仍含相同 serverInfo；HTTP 路由现有业务错误仍按原有 JSON-RPC error 映射。
- 2025-06-18 tools/list 与 tools/call 结果均断言没有 2026 serverInfo metadata。
- `python3 tests/mcp-serverinfo/mutations.py`：2/2 语义变异红，非编译失败：删除 tools/list serverInfo → `MCP_SERVERINFO_TOOLS_LIST`；tools/list 改用与其他结果不同的 name/version 字面量 → `MCP_SERVERINFO_SHARED_VALUES`。

## 门

- 定向测试（serverInfo 三方法、isError=true、2025 codec）：PASS。
- `DW_BROWSER_E2E=1 NODE_PATH=/tmp/r52-node-deps/node_modules go test ./internal/mcpgw -count=1 -v`：mcpgw 全包 PASS，126.861s。
- 真 Chromium：`TestBrowserClaudeCodeLocalhostConsentChains` 与 `TestBrowserCodexIPLoopbackConsentChains` 两种即刻跳转/consent 子场景全部 PASS，CSP_ERRORS=0。
- `go test -p 1 ./internal/m8`：PASS，223.161s。
- `go test -p 2 -timeout 40m ./...`：PASS；`internal/hosted` 753.898s，`internal/mcpgw` 113.601s，其余包均完成且无 FAIL。
- `TMPDIR=/tmp/sinfotmp ./scripts/run-build-tests.sh`：FAIL。repo-boundary/dependency-ratchet/tenant-isolation-gate 完成；helper build gate 的 Claude 与 Codex profile 均因以下 4 个文件未发现收集用例而失败，未到 `BUILD_TESTS_OK`：`test_desktop_endpoint`、`test_guide_staging`、`test_ipc_socket_path_isolation`、`test_sessionhelper`。未修改相关测试或构建门。
- `OPENSPEC_TELEMETRY=0 npx --yes @fission-ai/openspec@1.16.0 validate --all --strict`：PASS，34 passed / 0 failed。
- `git diff --check`：PASS。

## 交接记录

- 2026-11-10；执行者：togpt60lunazzc；commit/tip 与 tree：由单提交坐标及远端校验回报提供。完成：实现、定向/浏览器测试、变异、全仓 Go 与 OpenSpec 门；阻塞：run-build-tests 的 helper case discovery 失败（详见上文）；下一步：推送后交复核。
