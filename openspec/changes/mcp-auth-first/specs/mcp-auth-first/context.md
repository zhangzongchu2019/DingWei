# 现状与实现入口

main206d41b2，旧gateway.go mcp与codec2025.go路由先验版本，导致无bearer请求可能400；本变更新鉴权入口mcp、已鉴权mcpAuthorized与标准codec共享该请求Seat，不建立跨请求快照。旧规格MCC-001-S02的“错误版本拒绝”在标准initialize一项由MAF-002协商规则细化；其它已鉴权错误头/参数仍拒绝。客户端测试使用隔离fake Seats及本地HTTP，不冒称Antigravity现网验收。
