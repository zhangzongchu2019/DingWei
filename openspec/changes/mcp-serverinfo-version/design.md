# 设计

`gateway.go` 以 `dwversion.Version` 定义共享 `serverInfoVersion` 常量；`codec2025.go` 的 initialize 使用同一 `serverInfoName`、`serverInfoVersion`。2026 的 discover/list/call 已通过 `serverInfoMeta()` 使用该常量。测试从 `internal/version.Version` 比较响应值，不固定发行版本字面量。变更不新增外部依赖。

导入检查：Go `internal` 可见性允许同一 module 内 `internal/mcpgw` 导入 `internal/version`，`go list` 已解析两包。repo-boundary gate 的判据针对仓储库句柄和数据库建库调用；当前代码的修改未涉及这两类边界。dependency-ratchet 不限制 module 内包导入，且外部依赖清单不变。
