# 变更生命周期上下文

## 代码位置

- 基线 dfc95fd8b6b2a063edd77f22bd38ecc63b729213，只描述已核源码，不声称现网已开放新流程。
- `internal/store/console_specs.go`、`internal/m8/console_specs.go`：旧 Spec 修订/定稿；`internal/store/console_task_flow.go`、`console_task_acceptance_gate_test.go` 及 `internal/m8/console_task_flow.go`：旧任务与验收门，不直接迁成新 Git 变更状态机。
- `internal/specconfirm/confirmation.go`、`policy.go`：R7 固定坐标、不可变确认、版本关系；`internal/store/openspec_confirmation.go` 与 `migrations/0068_openspec_commit_confirmation.sql`：确认存储，不是 R8 状态/场景/发布账。
- `internal/m8/console_openspec_confirmation.go`、`cmd/workpulse/console_routes.go`：新确认路由。主程序未装配真实提供方/委员会/ACL，R7 实际读写默认 503；普通 R8 审批不依赖 Console 确认，显式委员会范围才加核 R7；不得把模拟服务当作真实依赖。
- 本锚未发现新 OpenSpec 七阶段状态和场景证据账；实际状态存储、事务与发布事实来源待实施设计，不在本单虚构新表或 API。

## 环境与配置

- OpenSpec 1.16.0 / Node >=22.12 / 遥测关闭；工具校验不代表业务验收。
- 提供方为 GitHub（dingwei）与工蜂（业务仓）；凭据、委员会和逐主体 ACL 接口由 prodops 核实，仅记录 secret 管理位置。
- R7 策略默认关闭，不能以本单规格或 CLI 结构齐备自动开启。

## 依赖

- 上游：提供方当前 MR head 及所需审阅人的同 SHA 有效批准、人工显式委员会标记、仅委员会范围的 R7 同 SHA 有效确认、R9 当前 PM 决策点/同 SHA 明确回应、完整任务/场景集合与交接、可信发布记录及权限。
- 同级：[R8](../../changes/r8-evidence-driven-change-lifecycle/proposal.md)、[风险提醒](../change-risk-reminders/context.md)。
- 下游：后续追溯视图/自动关联可消费事实，不能反向伪造验收或发布；不实现 R10 任务锁或 R9 消息编排。

## 负责组

- coder56solzzc.zzc 单写；opus50tc2k.zzc 审阅；prodops 核身份/提供方/发布事实；质量保障组按场景独立验收。

## 外部资料

- 固定决议、月计划第4.3节、验收计划链接见 R8 proposal 来源。未执行场景必须如实记录，不将本件视为验收结果。
