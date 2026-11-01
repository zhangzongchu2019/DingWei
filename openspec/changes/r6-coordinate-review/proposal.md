# Proposal

## Why

旧 Agent 派审携带数据库正文快照，不能沿用到 Git 管理的 OpenSpec，否则既可能超出消息体积限制，也可能借平台服务账号泄露私库内容。新派审需要只发固定坐标、由审阅主体独立获权读取，并把结论与被审版本绑定，使超时和改动后重审都可核查。

## What Changes

- 派审内容只包含仓库、变更编号与完整 commit SHA；投递路由/关联元数据不携带正文、补丁、附件、下载能力或凭据。
- 发起者与 Agent 独立核权，派审不授予仓库权限；接收方按指定 commit 和交接顺序读取。
- 审阅结论及 UTC 时刻绑定审阅人和该 commit；多人逐人展示缺审、超时提醒，实质修改后标「针对旧版本」并提示重审。

## Capabilities

### New Capabilities

- `console-coordinate-review`：新增 R6-001～008；最小基线/context 明确旧 snapshot 派审不等于坐标派审。

### Modified Capabilities

无。

## Impact

仅规格。未来涉及 Console 派审页面、Git 授权/坐标适配、审阅记录、提醒与审计。r44 定义基本派审和超时提醒；r46 的自动跨组负责人发现、复杂催办和升级编排不在本单实现。无源码转发、Git 写入、旧 Spec 新功能、任务锁、合并门或部署动作。

## 来源

- 委员会[全面切换决议](https://git.code.tencent.com/wego-ai/dingwei/blob/c2d9c9fdd0416828739f7042d2ceb9bc71f603c6/docs/decisions/OPENSPEC-SWITCH-DECISION-20261031.md)第 3、4、15、21 条（读取顺序补充依第 19 条）。
- [本月计划](https://git.code.tencent.com/wego-ai/dingwei/blob/edadb623f0bad3b64b3bdc53a297c8d0c3de05bc/docs/plans/DINGWEI-PLAN-2026-11.md)第 2、3 节；G2/G1；按组长本单仅提取已裁定的 r44 R6/R7，计划其它草案不转为授权。
- 源码基线 d1a7a686110c9721c61f403bb9cac4cae0b694ee；R11 实现分支不动。规格场景是未来验收依据，尚未执行产品验证。

## 待确认假设

- R6-A01 prodops 核实发起者与 Agent 的可信身份、各自仓库 read 查询和指定 SHA 可读性；不能从同 owner、同项目或服务账号权限推导获权。
- R6-A02 指派人名单由获权发起者提供；自动跨组/跨仓发现、组长升级在 r46 独立变更。本单的 Console 仍须逐人显示待审、完成和旧版本，不能合并成虚假的「所有人已审」。
- R6-A03 沿用 Hub 可信路由与持久投递作为传输，不另开连接；具体请求/回执字段、去重持久化与提醒执行机制待实施设计核实，不冒称现有旧协议支持新语义。

## 待 PM 决策

- R6-PM01 直接引用 [R7-PM01](../r7-confirm-immutable-version/proposal.md#待-pm-决策) 作为两能力唯一的实质修改政策决策；选项 A/B/C 与建议一致，不再创建第二个冲突口径。未定前不把「SHA 变了」自动等同实质修改，也不默认为旧审阅可用于新版本。
- R6-PM02 提醒时限：A 派审时明确带首催时限与重复周期（有界正值），按服务端 UTC 计时；B 平台固定统一时限；C 仅人工提醒。建议 A，界面显示约定截止时刻；默认数值、重复上限与升级时点由 PM 定，不擅定 SLA。r44 必有超时提醒，不因未定数值取消该需求；组长升级编排留 r46。
