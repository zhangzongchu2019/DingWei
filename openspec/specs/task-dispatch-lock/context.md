# 按任务编号派单与加锁上下文

## 代码位置与最小现状

- 代码锚2af469dd6919d8eb6064aa7da0e646f0851becf8，仅源码核验，不是线上实测。
- internal/m8/console_task_flow.go、internal/store/console_task_flow.go：旧数据库UUID任务的分配、assignment_version、执行/审阅角色和通知outbox，非openspec任务T编号租约。
- internal/store/migrations/0043_console_spec_tasks.sql、0047_console_spec_task_assignees.sql、0055_console_task_flow_r32.sql：旧任务结构，不能直接解释为跨版本互斥键。
- internal/specread/service.go：R5 Service.Read/Coordinate逐主体read、完整SHA与受限Markdown路径、15秒读取预算；github.go为已写适配，工蜂不可用。读取器本身不提供任务语义解析/锁。
- internal/specconfirm/policy.go：R7已批A-conservative-exemptions-v2，实质变化及tasks勾选/交接节豁免；比较不等于任务加锁或授权。
- 新编号锁、租约/代际、交接与漂移守卫是本变更提议，尚未实现；不声称旧任务表已满足。

## 环境与配置

OpenSpec1.16.0、Node≥22.12、OPENSPEC_TELEMETRY=0只用于执行席/CI。R5默认关闭与凭据/允许仓库/绑定沿console-spec-reading/context.md；不新增或读取真实凭据。新TTL/轮询数值仅proposal待PM建议，不造已存在配置键。

## 依赖

上游R5获权固定版本读取、R7比较策略及可信Console/Agent映射；下游R8任务完成/交接证据，不写其状态。来源链接见本变更proposal。平台能力时间线未假定已上线，缺记录不能用聊天替代。

## 负责组

Console开发coder56solzzc.zzc单写；组长独立审阅；prodops核提供方/运行依赖；质量保障组后续验收。
