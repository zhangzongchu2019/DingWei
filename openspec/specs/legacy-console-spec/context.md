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

## R11 核心实现接线
- `cmd/workpulse/legacy_spec_freeze.go`：读取 `WP_LEGACY_SPEC_FROZEN`，默认不启用；非法配置拒启动，true 在迁移后/接流前锁存冻结。状态已冻结时 false/漏配不解冻。无公共修改接口。
- `internal/store/legacy_spec_freeze.go`、`legacy_spec_lock_linux.go`、`migrations/0067_legacy_spec_freeze.sql`：SQLite 冻结位、同数据库锁文件共享/排他租约、26 张业务表的事务级拒写。Linux 部署，锁文件从 SQLite 实际数据库路径派生；不得在服务运行中删锁文件或为同一数据库另建锁路径。内存测试库使用进程锁。
- `internal/m8/legacy_spec_freeze.go` 与各旧规格/模板 handler：修改请求拒绝为 HTTP409/legacy_spec_frozen；普通消息和任务进度不走该拒写。`hub.go` 覆盖旧生成派发及 durable queue 实际发送，WS 回调和后台重试另有租约。
- `console_specs_page.go`：冻结后在原页面标注历史只读，不改变读取可见范围。LCS-002 的普通入口移除、独立查询与共享策略仍待后续。
- 生产旧规格对象实际保存在 SQLite；PostgreSQL 是身份与授权依赖，不存在需要同步冻结的第二份 PG Spec 表。必须先把所有访问该旧规格库的 Hub 更新为含 R11 守卫的版本，再由平台架构组按 GO 开启开关；未升级进程不具备出站共享租约。本文不授权部署或开关操作。
