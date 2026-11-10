# 上下文

代码位置：`internal/mcpgw/gateway.go` 中 2026-07-28 MCP switch；旧协议由 `internal/mcpgw/codec2025.go` 独立处理。测试位于 `internal/mcpgw/gateway_test.go`，官方 schema 摘录及语义变异检查位于 `tests/mcp-cacheable-result/`。

基线：main `b4273ddaca63012971d42e04209aa2eb65dd6a71`。项目 Go 工具链按 go.mod 固定为 1.22.2；OpenSpec 使用仓库钉版 1.16.0。
