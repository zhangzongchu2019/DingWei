# Helper Codex投递证据对账提议

## Why

本变更针对传输替换或通知丢失后原生回合已完成而本地投递仍未终态，以及入场超时后没有turn ID而无法只读核查的缺口。第一步只提交分析与基线失败测试，明确可见输出、原生完成证据和投递ACK三条链，防止通过补造时间或盲目重投消除积压，供后续实现范围冻结。

需求来源：组长派单request_id 932d3ef7daa0，rulings〔51〕仓库 `git@git.code.tencent.com:wego-ai/dingwei.git` / `review/opus50tc2k/rulings` / `24985813e3a8d3434e7b1872d6df9b6ada9ffa12` / `docs/rulings/S-TEAM-MIGRATE-HOSTED-20261017.md`。写锁更正以2026-11-08T10:23:37Z认证消息为准。

## What Changes

只新增分析、原生协议失败复现、长回合等待协议夹具及证据；不改产品代码。增量规格是待实现目标，本步不apply/archive，不宣称已实现。测试单独显式执行，不将预期红测接入发布门。

## 待确认假设

- ts3dev55报告52条已完成与101次替换的逐条关联未由本席核查。
- 本机夹具证明放大前半链，完整daemon故障链尚待后续授权实现/复核。
- 历史绑定集合能否仅由现有driver记录完整恢复，需核数据模型；不能假定LIMIT 1足够。

## 待 PM 决策

- HCD-PM01：后续证据持久性是否需最小迁移；建议先冻结证据字段/旧数据降级，未确认前不擅加迁移。
- HCD-PM02：对账预算、分页上限、退避/指标；建议给上限但超限保留unknown而非输出阴性。
- HCD-PM03：子Agent/多thread路由当前排除。提交前已读rulings fe847fc9的〔51〕-补2：业主已定三项，详细设计在本步交件后另派，不能以本单测试扩授权。

## Impact

可能的后续实现挂点为codexDriver、driver_adapter、inbox_lifecycle及相关账本；本步实际变更只在docs、tests和本提议目录。现网、他席机器、绑定配置均不操作。
