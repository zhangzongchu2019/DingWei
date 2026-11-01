# R7 能力上下文

## 代码位置

- 基线 d1a7a686110c9721c61f403bb9cac4cae0b694ee，只核源码，不代表现网验收。
- `internal/m8/console_specs.go`：旧数据库 Spec 的 submit-review/review/finalize 路由。
- `internal/m8/console_spec_agent_review.go`：旧 Agent 派审、身份与会话检查。
- `internal/store/console_spec_agent_review.go`：旧请求含 spec_id、revision、content_sha256、snapshot、deadline；snapshot 含正文，不可直接用于新坐标派审。
- `internal/store/console_specs.go`、`internal/model/model.go`：旧审阅与修订数据。上述旧路径在基线未实现本能力的 Git commit 坐标契约，不复用旧 snapshot。

- `internal/specconfirm/confirmation.go`、`policy.go`：当前 R7 部分实现的坐标、不可变事实、默认关闭的可替换策略；不包含 Git 网络适配。
- `internal/store/openspec_confirmation.go`、`openspec_confirmation_migration.go`、`migrations/0068_openspec_commit_confirmation.sql`：新事实存储、无账 schema 核验与保护触发器。
- `internal/m8/console_openspec_confirmation.go`、`cmd/workpulse/console_routes.go`：独立页面/接口与既有可信 Console principal 认证；`console_platform.go`/`newplat_console.go` 在可信依赖就绪后才显示入口。

## 环境与配置

- OpenSpec 1.16.0 / Node >=22.12 / `OPENSPEC_TELEMETRY=0`，只用于执行席和 CI，不加入 Hub 构建链。
- 新能力的仓库主体映射、当前 ACL、固定 commit 查询继承 R5 的受控 Git 适配前置事实；首期不依赖 R5 页面已上线，可独立验证坐标与授权，不能绕过其安全契约。
- 工蜂凭据管理位置待 prodops 核定；不记录值，不将服务凭据或源码放消息/文件服务。策略和提醒时限配置名未定，不虚构线上配置。R7 策略仅是进程启动时依赖，零值关闭，无 HTTP/CLI/env 开启入口；主程序尚未装配委员会/ACL/commit 验证器，读写默认 503。

## 依赖

- 上游：Console 可信主体、工蜂仓库只读与逐主体 ACL；[R5 读取契约](../../changes/r5-render-openspec-readonly/proposal.md)。
- 同级：[R7 变更](../../changes/r7-confirm-immutable-version/proposal.md)、[R6 变更](../../changes/r6-coordinate-review/proposal.md)；两者共享版本比较口径，确认与审阅记录分开。
- 下游：R8 状态机、R9 PM 决策、能力时间线另行变更；本能力不代替验收或发布。

## 负责组

- Console 开发 coder56solzzc.zzc 单写；opus50tc2k.zzc 审阅；prodops 核实 Git/权限事实；质量保障组独立执行场景。

## 外部资料

- 固定决议与月计划链接集中在本能力变更 proposal「来源」；从 context → spec → 进行中变更 → 平台时间线读取，不依赖他席聊天或会话记忆。
