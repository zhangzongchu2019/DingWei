# Proposal

## Why

现有通用风险条目没有仓库差异、规则版本和双端投递依据，不能证明负责人收到指定版本的高风险提醒。切换 OpenSpec 后必须把七类风险按可重放规则识别，在 MR 与 Console 对同一 commit 留痕，同时避免把提醒变成额外审批门，或通过评论泄漏私库正文。

## What Changes

- 新增七类标签/路径/新增行关键字的并集判定；纯函数仅消费固定 diff、标签快照与不可变规则版本，网络读取和投递分离。
- 各仓 `openspec/risk-paths.yaml` 与平台默认表形成可追溯规则输入；GitHub 与工蜂通过同一提供方契约供给数据。
- 命中后 MR 评论与 Console 各一条汇总提醒，记录负责人、依据、commit、提醒/知悉/合并时刻；新命中 commit 再提醒、无命中不提醒；不阻塞合并。
- PM 决策交给独立 R9，本单不实现 PM 通道、仓库迁移或产品代码。

## Capabilities

### New Capabilities

- `change-risk-reminders`：R1-001～008；最小基线仅描述通用风险记录现状。

### Modified Capabilities

无。

## Impact

仅 OpenSpec 文档。未来涉及规则纯函数、提供方 diff/标签/评论适配、Console 提醒、知悉和审计。读取权限不包含评论写权限；未核真实权限与接口前不实施。普通 MR 合并、R7 确认、R8 状态和 R9 决策职责分离。

## 来源

- 委员会[全面切换决议](https://git.code.tencent.com/wego-ai/dingwei/blob/c2d9c9fdd0416828739f7042d2ceb9bc71f603c6/docs/decisions/OPENSPEC-SWITCH-DECISION-20261031.md)第 5、6、8 条，来源 commit c2d9c9fdd0416828739f7042d2ceb9bc71f603c6。
- [七类风险草案](https://git.code.tencent.com/wego-ai/dingwei/blob/6a2048187be7660f1c56a85d8ad79c743702d250/docs/decisions/OPENSPEC-HIGH-RISK-RULES-DRAFT-20261101.md)与[11 月计划](https://git.code.tencent.com/wego-ai/dingwei/blob/6a2048187be7660f1c56a85d8ad79c743702d250/docs/plans/DINGWEI-PLAN-2026-11.md)第 3、4.3 节，来源 commit 6a2048187be7660f1c56a85d8ad79c743702d250。仅提取本单已派 R1/R8，月计划其它草案不转为实施授权。
- QA [稳定期验收计划](https://git.code.tencent.com/wego-ai/dingwei/blob/a433fc71c7c43c24203ebc11a4e78a347ec9d92c/docs/decisions/OPENSPEC-STABILITY-ACCEPTANCE-PLAN-20261031.md)第 3～5、7 项及风险提醒专项，来源 commit a433fc71c7c43c24203ebc11a4e78a347ec9d92c。
- 本单从 r44 集成 dfc95fd8b6b2a063edd77f22bd38ecc63b729213 起；组长明确补充 dingwei 已决定转 GitHub、业务仓留工蜂，两种托管提供方都纳入接口，实际迁移和账号操作不在本单。Git 作者不是需求来源。

## 待确认假设

- R1-A01：prodops 分别核 GitHub/工蜂受控实例、稳定仓库/MR ID、base/head 完整 SHA、差异完整性/新增行号、标签快照、重命名/二进制/分页/截断/限流语义、合并时刻；失败不能当作空 diff。提供方迁移须重新核权与映射，不按同名仓库移交旧记录。
- R1-A02：核 Console/MR 当前读取权限、可信负责人及代理规则、写评论的最小独立 scope、幂等/重试/查回接口和 secret 管理位置。只读令牌不能假设能发评论；服务账号不能代替用户授权。
- R1-A03：架构组冻结路径 glob 语义、关键字大小写/字面匹配、YAML schema、默认表与仓库表的作用范围/合并规则、标签格式、资源上限和未知处理。草案中的目录与词是候选默认表，本单按派单固定七类/三路/只看新增行，不虚构已运行规则配置。
- R1-A04：投递与审计的持久机制、实际代码路径及去重字段在实施前映射需求评审；不预先新增 API、错误码或数据库表。

## 待 PM 决策

- R1-PM01 **已决（选 A）**：业主于 2026-11-01T04:0xZ 在 workpulse 对话批准，由组长 opus50tc2k.zzc 转达（来源仅给出该时间范围，不补造精确分钟）。同一 commit 的标签或规则变化时，更新原提醒并追加可追溯修订；命中集合变化时重置该修订的「已知悉」，历史修订和知悉记录保留。不采用独立新提醒；同一判定输入重放不重复追加修订或重置知悉，追加命中 commit 仍重新提醒。实际评论更新、查回和权限能力按 R1-A01～04 核实；本次只回写规格，R1／R8 实现仍排 r45、另派实施。
