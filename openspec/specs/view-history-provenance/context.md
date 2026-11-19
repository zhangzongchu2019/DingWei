# view-history-provenance 上下文

发布提交 4a349ee59a1d8d7b8b9c562c462bded2f8b0f192（r61，VERSION 0.2.3.41），2026-11-19 现网上线（SWITCH_PASS 10:01:37Z）后按方案甲归档建立本主规格。结项凭据见 docs/reviews/r61-closeout/RECEIPTS.md。

## 来自 2026-11-19-history-provenance

# 基线 context

## 范围

本变更研究 Hub 的 session history 来源持久化与 View history 回放。核对点为 `329eb9f9` 和 `d2cc47c7`；两个提交的 `internal/m8/terminal.go`、`internal/store/session_history.go` 内容一致。当前分支只含提案，不代表行为已实现。

## 已核实实现

- `terminal.go:823-883` 实时观察事件包含 canonical source/provenance，但 `publishSessionEvent` 只进实时 ring 与订阅。
- `terminal.go:570-604` helper session event 会先剥除客户端 provenance，随后以 sanitized JSON 写入 history。
- `session_history.go:267-315,430` Append 持久化事件 JSON，Page 在 `693-738` 读回原始事件；HTTP history handler 在 `terminal.go:287-357` 做 owner/session 授权并编码响应。
- renderer 329 与 d2 的 `normalizeEvent` 均校验 provenance trusted 与 Hub attestation；但 329 的 history loader 用 `JSON.parse(row.event)` 解析字符串，当前 API 因 `json.RawMessage` 返回对象，不能加载现有 history 响应。d2 的 `parseHistoryEvent` 支持对象和字符串。旧版本只对实时字段形状兼容；旧版刷新回放兼容不在本提案承诺内。
- 现有 history 上限：单事件 1,000,000 bytes、每 Agent 64,000,000 bytes、全局 2,000,000,000 bytes，保留 30 天（`session_history.go:23-29`）。

## 关联字段与安全约束

可用于关联的主键为 Hub 已知目标会话及 delivery ID；历史 user_input 的 `origin_delivery_id`/`delivery_id` 是相关性字段，不是独立鉴权依据。只有 Hub 保存的 canonical primary 来源凭据可建立 trusted provenance。缺少、过期、重复或跨会话匹配必须无图标，不以 source 文本或投递更新补猜。

## 实施待定

该提案推荐紧凑 sidecar，避免重复正文；具体表、唯一索引、与 Append 的事务/重试边界、过期清理和配额核算需要实现设计确认。`attested_by=hub` 是现存字段契约中的 Hub 信任标记，不宣称为密码学签名。

## 2026-11-18 实施现场

基main206d41b2，当前实现位于internal/store/session_history_provenance.go、history_migrations/0078_history_provenance.sql和internal/m8/history_provenance.go。原Append group-commit与Page envelope保持；真实Agent/timer WebSocket及HTTP历史输出由TestHistoryProvenanceRealHubAgentTimerReplay生成，view-service/history-provenance-browser-regression.js启动该隔离fixture后只加载历史页验证刷新，已登记全量runner。fixture为无模型合成协议事件，不使用现网身份/凭据/数据库。

## S-R61-N3-IMPL 作者交件（2026-11-18）

基main 206d41b26dab646c0e50a111eed5a45ccf7c72f6；首提交c3b3db27c9532ab19a80d769da55b98f02e08696原样导入批准6a60a1a6。当前冻结产品/测试源a32c877cf6ef21a909d2806f4b08e3e925d05d98，作者证据9a145fd138c7d15bf1ec027b773b960919e69056。history独立0078，不改应用库0077；五元组同事务增强消费、超限回滚保留sidecar、原有清理边界同步计费均已实现。六门真实rc0，N-4历史失败与最终诊断不抹除，SKIP/变异/DB-HTTP-SSE夹具见docs/reviews/r61-n3-history-provenance。独立复核待togpt60，不代表验收或发布批准。


## 2026-11-18 legacy 重锚修订现场

独立复现 legacy ACK 同名重锚缺口，见 internal/m8/history_provenance_test.go 的 TestHistoryProvenanceLegacyReanchorDoesNotFollowReusedName。hub.go 的 legacy_received 实时 observation 显式传空history目标，禁用此晚到路径的新sidecar；已有入队捕获证据按原五元组保留。修订不改迁移/存储schema/renderer。当前正式复核方为qwen38mvb4（组长改派），旧togpt请求已撤回；其具体发现作为技术输入核实，不复用其未跑或不完整的门。

范围校准与新交件：新源 `9313197f8ac3e2a585cd399935d5b313084b3b1e`，全门与42刀证据 `f868fe6a3f725b9f0f242468d864020c9947e788`。正常route会剥除 `_hub_ingress_channel`；真实入队控制在A32/931均绿。四格旧产品红是人为保留该标记的记录形态，尚无证据确认真实历史记录存在，不声称正常当前路由或现网已遭身份串写。新门完成，正式复核仍待qwen；旧togpt派审不恢复。
