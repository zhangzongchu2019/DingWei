# Proposal

## Why

仓库任务勾选、工具归档与真实业务验收、环境发布是不同事实，沿用旧定稿或仅看清单会把未执行和失败场景误报为完成。需要明确固定版本上的七阶段状态及每一步的可检查前提，把确认、PM回应、任务交接、场景结果和发布记录分开留痕，拒绝缺证据、旧版本或并发事件造成的虚假推进。

## What Changes

- 新增 DRAFT→SPEC_REVIEW→SPEC_APPROVED→IMPLEMENTING→ACCEPTANCE→ARCHIVED→RELEASED；只允许有当前版本依据的合法相邻推进。
- SPEC_APPROVED 按类别核提供方当前MR head完整SHA的所需审阅批准及全部PM当前SHA明确回应；仅显式委员会范围再核R7当前SHA确认；ACCEPTANCE核任务完成和交接；ARCHIVED核完整场景四态、FAIL=0及未执行/不适用理由；RELEASED核release_id与环境。
- 场景记录PASS／FAIL／未执行／不适用及证据位置，绑定场景编号、受检版本和记录主体；修订可追溯，不覆盖历史。
- 分离归档、验收结论与实际发布，tasks全勾、archive命令或strict成功均不能自行判完成。本单不实现R9、R10或发布执行。

## Capabilities

### New Capabilities

- `change-lifecycle-evidence`：R8-001～009；最小基线不将旧流程或已有R7存储冒称新生命周期。

### Modified Capabilities

无。

## Impact

仅规格；未来涉及可信版本/状态事件、提供方MR审阅/显式类别与按需R7/R9事实读取、任务和场景清单、结果证据、发布事实及Console。新状态不覆盖旧Spec，不写回Git，不把运行中的灰度/发布或本单校验当作新状态机验收。

## 来源

- 委员会[全面切换决议](https://git.code.tencent.com/wego-ai/dingwei/blob/c2d9c9fdd0416828739f7042d2ceb9bc71f603c6/docs/decisions/OPENSPEC-SWITCH-DECISION-20261031.md)第 5、6、8 条，来源 commit c2d9c9fdd0416828739f7042d2ceb9bc71f603c6。
- [七类风险草案](https://git.code.tencent.com/wego-ai/dingwei/blob/6a2048187be7660f1c56a85d8ad79c743702d250/docs/decisions/OPENSPEC-HIGH-RISK-RULES-DRAFT-20261101.md)与[11 月计划](https://git.code.tencent.com/wego-ai/dingwei/blob/6a2048187be7660f1c56a85d8ad79c743702d250/docs/plans/DINGWEI-PLAN-2026-11.md)第 3、4.3 节，来源 commit 6a2048187be7660f1c56a85d8ad79c743702d250。仅提取本单已派 R1/R8，月计划其它草案不转为实施授权。
- QA [稳定期验收计划](https://git.code.tencent.com/wego-ai/dingwei/blob/a433fc71c7c43c24203ebc11a4e78a347ec9d92c/docs/decisions/OPENSPEC-STABILITY-ACCEPTANCE-PLAN-20261031.md)第 3～5、7 项及风险提醒专项，来源 commit a433fc71c7c43c24203ebc11a4e78a347ec9d92c。
- 本单从 r44 集成 dfc95fd8b6b2a063edd77f22bd38ecc63b729213 起；组长明确补充 dingwei 已决定转 GitHub、业务仓留工蜂，两种托管提供方都纳入接口，实际迁移和账号操作不在本单。Git 作者不是需求来源。

## 待确认假设

- R8-A01：核可提交推进/场景结果/独立验收/发布事实的可信主体与逐仓读取权限，不能由客户端自填角色、Git作者或旧management身份推导。QA独立验收与开发自测需在记录中可区分。
- R8-A02：提供方提供当前MR head完整SHA、所需审阅人集合、有效批准及所审SHA、当前标签；读取该SHA的proposal显式委员会标记与完整PM点，由R9提供绑定同SHA的有效明确回应。只有委员会范围另依赖R7同SHA确认；普通变更不依赖Console确认服务。缺失/失效/未知不得视为零项或已满足。具体提供方接口/权限、审阅策略映射及R9事实仍待核，R7当前未装配不能误阻普通变更。
- R8-A03：任务/场景完整集合、固定编号、交接及执行commit到当前目标规格版本的适用映射由读取适配核实。当前目标commit必须显式选取，不随仓库HEAD变化；进度/证据可以另有受检或执行commit，但不得靠同编号直接复用旧结果。映射与一致性细节实施前冻结，不虚构已存在的字段/API。
- R8-A04：核可信release_id、环境、发布成功事实与本变更/版本覆盖关系的来源；预发与生产分别展示，不把版本号或归档路径当发布记录。实际事务、幂等/审计持久化与代码位置实施前交审。

- R8-A05 **已决**：平台架构组、组长 opus50tc2k.zzc 于 2026-11-01T03:1xZ 裁定（通知仅给出该时间范围，不补造精确分钟）。普通变更：提供方MR在当前head完整SHA获所需审阅人批准，且proposal所有PM点有绑定当前SHA的明确回应；不要求Console确认。委员会范围：在前述两项外，再要求R7委员会确认绑定当前SHA。委员会范围只由人显式标注：MR标签 committee-review，或proposal写「需委员会确认：是」，两者任一即适用；平台不按风险、路径、作者或身份推断。旧SHA批准不算当前SHA批准，过期判定沿用R6/R7实质修改口径；具体接口事实仍按R8-A02待核。本裁定覆盖本提案首版的待核审批映射。

## 待 PM 决策

本单未新增须改变已派状态前提的PM选项；采用计划第4.3节及已决R8-A05类别映射。未执行/不适用有理由且FAIL=0可以归档，但仍不能显示“全部场景已执行通过”。如后续需要回退阶段、跳步例外或新的完成定义，须另列决策点并按R9处理，本单不预设批准。
