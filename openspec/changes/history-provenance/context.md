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
