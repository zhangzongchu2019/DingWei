# mcp-auth-first 上下文

发布提交 4a349ee59a1d8d7b8b9c562c462bded2f8b0f192（r61，VERSION 0.2.3.41），2026-11-19 现网上线（SWITCH_PASS 10:01:37Z）后按方案甲归档建立本主规格。结项凭据见 docs/reviews/r61-closeout/RECEIPTS.md。

## 来自 2026-11-19-mcp-auth-first

# 现状与实现入口

main206d41b2，旧gateway.go mcp与codec2025.go路由先验版本，导致无bearer请求可能400；本变更新鉴权入口mcp、已鉴权mcpAuthorized与标准codec共享该请求Seat，不建立跨请求快照。旧规格MCC-001-S02的“错误版本拒绝”在标准initialize一项由MAF-002协商规则细化；其它已鉴权错误头/参数仍拒绝。客户端测试使用隔离fake Seats及本地HTTP，不冒称Antigravity现网验收。

冻结产品／测试源ce9b7a959e828ad717b8708651c9094a421272ec（tree1a1e9e174722f45acfd4520182800382abf47894），5个产品／6个测试路径；后续tip只补docs／openspec。新增connect_failure.go静态统一HTML400，仅替换oauth.go的attempt失败展示，readAttempt cookie哈希与5分钟TTL不变；feishulogin/connect.go只改手填目标文案，不碰GROUPS的allowUnpaired函数hunk。回归入口auth_first_test.go、connect_failure_test.go与connect_target_hint_test.go。初交审证据623fcf4c:docs/reviews/mcp-auth-first/REPORT.md，最终门与逐名SKIP后续在同目录追加；未执行生产HTTP或真实Antigravity授权。

完整作者证据锚e5754c6e91a1cc4a3ae48883a5e8eb90034e4ca7，REPORT／gate-summary／VERIFY／SKIPS及五轮原始日志同目录，产品ce9不变。原三遍rc1与基线18／2保留；新五轮4／5绿按既定Z3例外完成，不声称N4已修。
