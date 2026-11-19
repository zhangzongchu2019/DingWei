# 提案

## Why

刷新页面时，Hub 当前保存的历史只有 helper 产生的 user_input echo；实时 input_posted 中的 Hub 可信来源没有进入历史，因此已注入的 Agent／timer 卡无法在刷新后恢复来源图标。该变更为历史建立可持久、可精确关联的 canonical 来源依据，并让已支持 provenance 的 renderer 安全回放，解决刷新后的信息丢失，同时维持存量无来源记录的 fail-closed 显示。

## What Changes

- Hub 在历史存储中为每个可信 primary 入站投递持久化最小来源凭据，以目标会话和 delivery ID 精确关联，不重复保存消息正文。
- Hub 持久化 admitted user_input 时，仅在找到匹配的 Hub 凭据后附加标准 provenance 对象；历史分页 API 按原事件 JSON 返回该字段。
- 历史来源缺失、关联不唯一或校验失败时，renderer 不推断、不显示来源图标。
- 新增刷新后 Agent 与 timer 卡的历史回放验收及字段／判定变异判据。

## 需求来源

组长派单 S-HISTORY-PROVENANCE（2026-11-16），依据 S-VIEW-ICON-RESIDUAL 交件 23016d6b：实时路径已由 8cabb351 修复，刷新后历史缺 canonical input_posted 可信来源；本单只要求提案，不实施产品代码。

## 范围与影响

本变更基于 main d2cc47c7，仅新增 OpenSpec 文档，不改 Hub、renderer、数据库 schema 或部署。提案覆盖 Hub 历史来源写入和分页回放、存量数据规则、容量与兼容风险，以及后续实现必须满足的回归判据；实施与迁移另行派单。

## 待确认假设

- 推荐以有界、按历史保留期清理的紧凑 provenance sidecar 保存 canonical 证据，并在 user_input 落库前精确关联；不将完整 input_posted payload 再存一份。具体 SQLite 表结构、事务边界与索引在实现派单中审定。
- Hub 现有 `attested_by: "hub"` 是信任边界标记，不是密码学签名。本文中的“Hub 签发”指由 Hub 依据其 canonical ingress 判断生成，客户端提交的同名字段必须剥除。

## 待 PM 决策

无。本提案沿用现有 provenance 信任契约；存量无来源时保持不显示。

## 实施授权（2026-11-18）

以上为原设计阶段的基线与范围记录。组长 S-R61-N3-IMPL 已授权在 main `206d41b26dab646c0e50a111eed5a45ccf7c72f6` 上实施本设计；第一提交 `c3b3db27c9532ab19a80d769da55b98f02e08696` 原样导入 `6a60a1a625ba36e8240866d641050ed83aaa64ef` 指定目录。实际实现、独立 history migration 0078、作者门与未完成的独立复核见 `docs/reviews/r61-n3-history-provenance/REPORT.md`。不改变应用库0077、renderer生产逻辑或部署状态。
