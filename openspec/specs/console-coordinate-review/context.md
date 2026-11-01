# R6 能力上下文

## 代码位置

- 基线 d1a7a686110c9721c61f403bb9cac4cae0b694ee，只核源码，不代表现网验收。
- `internal/m8/console_specs.go`：旧数据库 Spec 的 submit-review/review/finalize 路由。
- `internal/m8/console_spec_agent_review.go`：旧 Agent 派审、身份与会话检查。
- `internal/store/console_spec_agent_review.go`：旧请求含 spec_id、revision、content_sha256、snapshot、deadline；snapshot 含正文，不可直接用于新坐标派审。
- `internal/store/console_specs.go`、`internal/model/model.go`：旧审阅与修订数据。上述路径未实现本能力的 Git commit 坐标契约，实施位置待实施派单后核定并回写。

## 环境与配置

- OpenSpec 1.16.0 / Node >=22.12 / `OPENSPEC_TELEMETRY=0`，只用于执行席和 CI，不加入 Hub 构建链。
- 新能力的仓库主体映射、当前 ACL、固定 commit 查询继承 R5 的受控 Git 适配前置事实；首期不依赖 R5 页面已上线，可独立验证坐标与授权，不能绕过其安全契约。
- 工蜂凭据管理位置待 prodops 核定；不记录值，不将服务凭据或源码放消息/文件服务。策略和提醒时限配置名未定，不虚构线上配置。

## 依赖

- 上游：Console 可信主体、工蜂仓库只读与逐主体 ACL；[R5 读取契约](../../changes/r5-render-openspec-readonly/proposal.md)。
- 同级：[R7 变更](../../changes/r7-confirm-immutable-version/proposal.md)、[R6 变更](../../changes/r6-coordinate-review/proposal.md)；两者共享版本比较口径，确认与审阅记录分开。
- 下游：R8 状态机、R9 PM 决策、能力时间线另行变更；本能力不代替验收或发布。

## 负责组

- Console 开发 coder56solzzc.zzc 单写；opus50tc2k.zzc 审阅；prodops 核实 Git/权限事实；质量保障组独立执行场景。

## 外部资料

- 固定决议与月计划链接集中在本能力变更 proposal「来源」；从 context → spec → 进行中变更 → 平台时间线读取，不依赖他席聊天或会话记忆。

## r47实施位置（S-R6-IMPL，T-04～T-06）

- `internal/coordinatereview/`：独立坐标派审服务、固定版本读取、R7比较与追加事实、有界提醒；不接旧snapshot字段。`internal/specread/permission.go`复用R5核权；正文仍只由同一R5 `Service.Read`取得。
- `internal/m8/coordinate_review*.go`：默认关闭装配、当前owner/Agent独立Git授权、Console/Agent API、页面、生命周期调度。`internal/identity/coordinate_review.go`：当前Agent/owner/可信私聊身份事务屏障。
- `internal/store/coordinate_review.go`、`migrations/0071_coordinate_review.sql`：坐标元数据、历史、独立Agent Git映射与审计、R6/R9事件归属。0070仍属R5；无生产迁移执行。
- `cmd/workpulse/{main,console_routes,pilot_question_feishu}.go`：身份装配后接入R6及现有飞书personal发送器；总开关`DW_OPENSPEC_REVIEW_ENABLED`只有显式1生效。还依赖R5的`DW_OPENSPEC_READ_ENABLED`、`DW_OPENSPEC_GITHUB_CREDENTIAL_FILE`（只记配置位置，不记值）、受控`DW_OPENSPEC_GITHUB_REPOSITORY_IDS`。真实GitHub凭据/主体映射/渠道回执须T-07核验；工蜂仍不可用。R5 T-08前默认关的发布前置不变。
- `tools/sessionhelper/sessionhelper/{spec_review,app,ipc_listener,mcp_shim}.py`：新的`coordinate_review_v1`能力与`dw_coordinate_review`工具，保留旧工具；固定Hub来源、请求ID与坐标白名单，不接受正文/下载能力。
- `view-service/coordinate-review-browser-regression.js`已登记`tools/run-browser-regressions.py` SUITES；核心/HTTP/WS/PG/SQLite测试见相邻`*_test.go`，目标变异runner为`scripts/test-r6-mutations.py`。所有测试隔离HOME、临时DB和loopback；实际构建门用helper-deps-lock-venv，不连接生产。
- 技术决策/API/状态与需求映射写入变更design的“S-R6-IMPL实施接线”；当前结果不会自动生成委员会确认、验收、发布或合并门。T-07独立验收、T-08预发仍未完成。
