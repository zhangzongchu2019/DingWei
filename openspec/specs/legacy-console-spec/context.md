# 旧 Console Spec 上下文

## 代码位置
- 本仓基线 commit：a8bb5e5847884b62ba01f6f48fbce3772e2d8b04。以下来自代码检查，不宣称已核现网；旧 `specs/` 仅参考，未导入为事实。
- `cmd/workpulse/console_routes.go`：页面、Console/Agent/shared 写入口、PM 流程和回调路由总表。
- `internal/m8/console_auth.go`、`console_specs_page.go`、`console_pm_spec_flow_page.go`：工作区入口、编辑与审结界面。
- `internal/m8/console_specs.go`、`agent_spec_direct.go`、`console_spec_agent_review.go`、`console_spec_pm_flow.go`：修订、共享 capability、Agent 直写与派审处理。
- `internal/store/console_specs.go`、`console_spec_agent_review.go`、`console_spec_pm_flow.go` 及 `internal/store/migrations/`：正文、修订、门状态与审计存储。实施前从路由逐项核对 SQLite/PostgreSQL 实现与触发器，不凭单一后端推断。
- `internal/m8/console_specs_test.go`、`console_spec_generation_gate_test.go`、`console_specs_boundary_test.go`：当前流程及调度边界的测试入口。

## 环境与配置
- 仓库：工蜂私有 `wego-ai/dingwei`；不读取生产数据库或部署状态建立本基线。
- 当前 Console 以 principal/platform action、Agent 专属凭据及共享 capability 区分权限；不是工蜂仓库 ACL。
- 凭据依现有运行环境的受控 secret 挂载管理，仅记录位置类型；本件不读取值、不创建新凭据。
- 切换生效时刻、在途边界与历史共享查询策略须由 proposal 中待 PM 决策记录确认。

## 依赖
- 上游：现有 Console 登录/权限、规格持久化、helper 已认证通道。
- 下游：旧生成审阅、PM 草稿、任务拆分和共享页面；冻结需阻断修改旧规格的回调，不能顺带停止在途任务进度。
- 与新只读浏览的边界：[console-spec-reading](../console-spec-reading/spec.md)；切换增量：[R11](../../changes/r11-freeze-legacy-console-spec/proposal.md)。

## 负责组
- Console 开发：coder56solzzc.zzc 单写；平台架构组确认切换边界，prodops 执行获批发布。

## 外部资料
- 决议与就绪计划的不可变来源见 R11 proposal「来源」；发布授权不由本上下文或 OpenSpec archive 产生。
