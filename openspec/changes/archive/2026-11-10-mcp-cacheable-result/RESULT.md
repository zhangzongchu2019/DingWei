# 结果与门读数

- 基线：`b4273ddaca63012971d42e04209aa2eb65dd6a71`
- 分支：`togpt60lunazzc/mcp-cacheable-result`
- 实现：2026-07-28 `tools/list` 返回 `resultType=complete`、`ttlMs=300000`、`cacheScope=private`；两个缓存值均为具名常量。2025-06-18 codec 未改。
- schema：从官方 `modelcontextprotocol/specification` 2026-07-28 `schema.json` 提取 `$defs.ListToolsResult`，测试按其 required/properties 检查必填字段、整数及 minimum、cacheScope enum、tools 数组；另断言 ttlMs>0 及工具顺序固定。

## MUST/SHOULD 逐项核对

- MUST `server/discover`：已实现，公布 `2026-07-28`、tools capability 与 serverInfo identity。
- MUST 所有普通结果有 `resultType=complete`：server/discover、tools/list 与成功 tools/call 已具备。
- MUST CacheableResult：本次只覆盖已实现的 tools/list，补齐此前缺少的 ttlMs/cacheScope。prompts/resources 方法未实现，不新增。
- tools/call：成功结果含 content、structuredContent、isError 与 resultType；未发现本 changelog 所列新增 MUST 缺项。
- SHOULD server identity：changelog 建议每个结果在 `_meta.io.modelcontextprotocol/serverInfo` 提供服务端身份。当前 discover 有此字段，tools/list 与 tools/call 没有；记录为后续 SHOULD，本变更未改。
- SHOULD tools/list 顺序确定：当前工具定义稳定，本次增加固定顺序断言。
- 其他相关 SHOULD：客户端请求 `_meta.io.modelcontextprotocol/clientInfo`；网关收到请求但无需作为客户端发送，未涉及。

## 测试门

- `go test ./internal/mcpgw -run '^TestDiscoveryWithoutInitializeAndNoExtraCapabilities$' -count=1 -v`：PASS。
- `python3 tests/mcp-cacheable-result/mutations.py`：3/3 语义变异红，rc=1 且命中各自判据：`omit_ttl_ms` → `MCP_LIST_TTL_MS`；`string_ttl_ms` → `MCP_LIST_TTL_MS`；`invalid_cache_scope` → `MCP_LIST_CACHE_SCOPE`。无编译失败伪通过。
- `DW_BROWSER_E2E=1 NODE_PATH=/tmp/r52-node-deps/node_modules go test ./internal/mcpgw -count=1 -v`：PASS，`internal/mcpgw` 全包；真实 Chromium Claude localhost 即时跳转/consent、Codex 127.0.0.1 即时跳转/consent 均 PASS，CSP_ERRORS=0。
- `go test -p 1 ./internal/m8`：PASS，224.636s。
- `go test -p 2 ./...`：FAIL，`internal/hosted` 10 分钟超时；栈在 `pgtest.(*protocol).await` / `TestJobReasonOnlyContractCodes`，日志出现 hosted `g4_hosted_seat` relation missing。`internal/mcpgw` 与其他已完成包通过。单独 `go test ./internal/hosted -count=1` 重跑仍在 10 分钟超时，日志在 `TestRefreshCredNodeOutcomes/restart_failed` 等待 PG 清理 EOF；未触碰 hosted 源码。
- `./scripts/run-build-tests.sh`：FAIL，repo-boundary/dependency/tenant-isolation checks 继续执行后，helper build gate 报 4 个文件没有收集到用例：`test_desktop_endpoint`、`test_guide_staging`、`test_ipc_socket_path_isolation`、`test_sessionhelper`；Claude/Codex 两 profile 均报 `helper case discovery failed`，脚本因此提前退出，未到 BUILD_TESTS_OK。
- `OPENSPEC_TELEMETRY=0 npx --yes @fission-ai/openspec@1.16.0 validate --all --strict`：PASS，33 passed / 0 failed。首次校验发现 delta 标题缺失，已修规格后复跑通过。
- `git diff --check`：PASS。

## 交接记录

- 2026-11-10；执行者：togpt60lunazzc；commit：`2a47b0ef2e38fa90edb237ddc7ec74ea6c5118f9`。完成：实现、官方 schema 结构检查、三项变异、局部/浏览器门与 OpenSpec strict；阻塞：全仓 Go 门和 run-build-tests 在本机环境未通过，具体读数见上；下一步：推送本分支并交复核席决定是否接入 r56.5。
