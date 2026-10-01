# R0 · Hub 存储面按功能分组的表清单

- 派工 `R-NEWPLAT-G2-qwen38maxkhovvob4-20261001084334`（组长 opus50ts，2026-10-01T08:43:34Z）第一件
- 编排件 `runbooks/WORKGROUPS-NEWPLAT-20261001.md`（板锚 c796e65e）· 集成分支 `newplat/main` 起点 `fcd3a3dd`
- 代码锚：分支 `qwen38maxkhovvob4/newplat-r0`，基线 `fcd3a3dd`（旧平台 preprod 未动）
- 用途：供业主定「旧功能带哪些」。本件只做**归属与计数**，不改任何判据、不改任何 schema。
- 归属依据的优先级：迁移文件里的分节注释 ＞ Go 引用点 ＞ 列名形状推断。凡属推断的，本文标注「推断」。

## 一 · 表数口径（先说清「61」数的是什么）

派工写的是「Hub 主库 61 张表」。实测四个口径，各不相同，逐个列清：

| 口径 | 数 | 说明 |
|---|---|---|
| 迁移文件数 | 52 | `internal/store/migrations/*.sql` |
| `CREATE TABLE` 语句数 | 61 | 与派工的「61」一致 |
| 不同表名数 | 61 | 无重名建表 |
| **真跑完迁移后的活表** | **60** | 59 张业务表 ＋ 1 张 `schema_migrations` 账表 |

差额来源，两处，都已核实：

1. **2 个表名只是改名中转，活库里不存在。**
   - `console_spec_v2`（0044:3 建）→ 0044:34 删旧 `console_spec` → 0044:35 改名为 `console_spec`
   - `console_spec_revision_v3`（0050:1 建）→ 0050:24 删旧 `console_spec_revision` → 0050:25 改名为 `console_spec_revision`
   - ⇒ 61 − 2 = **59 张业务表**真活在库里。
2. **59 张里有 2 张建了但没人用**（Go 非测试引用 = 0，见第五节）⇒ **57 张真在用**。

下文分组按 **61 条建表语句**逐条归位（含中转名与退役表，都单列标注），这样每条建表都有归属，不漏不重。

## 二 · 主库按功能分组（15 组 · 61 条建表全覆盖）

「活」= 真跑完迁移后库里有这张表。「引用」= Go 代码里非测试文件的命中次数（`internal/store` 等），用来粗排改造工作量，不是精确调用图。

### A · 用户与鉴权 —— 主体、角色、会话（5 张，全活）🔴 R0 核心

| 表 | 建表处 | 引用 | 归属依据 |
|---|---|---|---|
| `console_principal` | 0033_console_auth.sql:1 | 27 | 列形状 (provider, provider_issuer, provider_subject) 唯一 → 身份主体 |
| `console_principal_role` | 0035_console_principal_roles.sql:1 | 5 | 迁移文件名 |
| `console_session` | 0034_console_sessions.sql:1 | 6 | 列形状 (token_hash, expires_at, revoked_at) |
| `console_page_capability` | 0034_console_sessions.sql:10 | 4 | 同文件；页面级能力位 |
| `console_platform_role` | 0037_console_platform_roles.sql:1 | 10 | 迁移文件名 |

### B · 用户与鉴权 —— Agent 与授权（2 张，全活）🔴 R0 核心

| 表 | 建表处 | 引用 | 归属依据 |
|---|---|---|---|
| `console_agent` | 0033_console_auth.sql:11 | 31 | 列形状 (key_id, session_name, owner_principal, active) |
| `console_agent_grant` | 0033_console_auth.sql:20 | 6 | 同文件；授权关系 |

### C · 编组（Agent 分组）（3 张，全活）🔴 R0 核心

| 表 | 建表处 | 引用 | 归属依据 |
|---|---|---|---|
| `agent_group` | 0036_agent_grouping.sql:1 | 20 | 迁移文件名 |
| `agent_group_member` | 0036_agent_grouping.sql:10 | 10 | 同文件 |
| `agent_group_state` | 0041_console_agent_groups.sql:5 | 3 | 表名前缀 |

### D · 会话端点与投递（3 条建表，2 活 1 退役）🔴 R0 核心

| 表 | 建表处 | 引用 | 归属依据 |
|---|---|---|---|
| `session_endpoint` | 0003_session_endpoint.sql:2 | 45 | 迁移注释「M8/M10 统一寻址会话端点。key_id 是公开地址标识；连接 secret 只存 service_api_key.key_hash」 |
| `session_delivery` | 0027_session_delivery.sql:1 | 14 | 表名前缀 ＋ 引用点 amr.go / reply_origin.go |
| `session_delivery_attempt` | 0029_session_delivery_attempt.sql:1 | **0** | 🔴 疑似退役，见第五节 |

### E · 服务注册与密钥（5 张，全活）

| 表 | 建表处 | 引用 | 归属依据 |
|---|---|---|---|
| `registered_service` | 0001_init.sql:53 | 7 | 0001 分节注释「服务注册（路由平台，M8）」 |
| `routing_rule` | 0001_init.sql:69 | 3 | 同上 |
| `service_api_key` | 0001_init.sql:83 | 8 | 同上；🔴 是 `session_endpoint` 连接 secret 的唯一存放处 |
| `api_key_account` | 0001_init.sql:93 | 6 | 同上 |
| `key_application` | 0023_key_application.sql:1 | 5 | 列形状（applicant_open_id / status / approver_open_id）＝飞书 bot 密钥申请审批 |

### F · 消息总线（飞书渠道）（4 条建表，3 活 1 退役）

| 表 | 建表处 | 引用 | 归属依据 |
|---|---|---|---|
| `bot_channel` | 0001_init.sql:5 | 54 | 0001 分节注释「消息总线（M0）」 |
| `chat_entity` | 0001_init.sql:15 | 7 | 同上 |
| `message` | 0001_init.sql:34 | 61 | 同上 |
| `contact_cache` | 0001_init.sql:26 | **0** | 🔴 疑似退役，见第五节 |

### G · spec —— 规格与评审（8 条建表，6 活 2 中转）

| 表 | 建表处 | 引用 | 归属依据 |
|---|---|---|---|
| `console_spec` | 0039_console_specs.sql:1（0044 重建） | 31 | 迁移文件名 |
| `console_spec_revision` | 0039_console_specs.sql:15（0050 重建） | 18 | 同上 |
| `console_spec_review` | 0039_console_specs.sql:28 | 1 | 同上 |
| `console_spec_event` | 0039_console_specs.sql:61 | 2 | 同上 |
| `console_spec_presplit_request` | 0045_console_spec_presplit.sql:1 | 11 | 同上 |
| `console_spec_gate_review` | 0046_console_spec_generation_gate.sql:1 | 5 | 同上 |
| `console_spec_v2` | 0044_console_spec_workflow.sql:3 | — | 🔴 改名中转，0044:35 后不存在的独立表 |
| `console_spec_revision_v3` | 0050_console_spec_actor_and_derive.sql:1 | — | 🔴 改名中转，0050:25 后不存在的独立表 |

### H · 任务 —— spec 任务、提案、驱动（6 张，全活）

| 表 | 建表处 | 引用 | 归属依据 |
|---|---|---|---|
| `console_spec_task` | 0043_console_spec_tasks.sql:1 | 17 | 迁移文件名 |
| `console_spec_task_acceptance` | 0048_console_spec_task_acceptance.sql:1 | 7 | 同上 |
| `console_spec_task_case_result` | 0049_console_spec_task_case_result.sql:1 | 4 | 同上 |
| `console_task_proposal` | 0039_console_specs.sql:44 | 6 | 表名前缀 ＋ 引用点 console_specs.go |
| `console_task_driver_start` | 0051_console_task_driver.sql:1 | 4 | 迁移文件名 |
| `console_task_pause_report` | 0051_console_task_driver.sql:18 | 3 | 同上 |

### I · 平台条目（1 张，活）

| 表 | 建表处 | 引用 | 归属依据 |
|---|---|---|---|
| `console_platform_item` | 0038_console_platform_items.sql:1 | 6 | 列形状 (kind, title, href, status pending\|approved, submitted_by) ＝条目提交与审核 |

### J · 控制面 L0/L1/L2（3 张，全活）

| 表 | 建表处 | 引用 | 归属依据 |
|---|---|---|---|
| `control_task` | 0021_control_plane_p1.sql:3 | 28 | 迁移注释「control-plane P1: L0 task queue + data-driven L1 deci…」 |
| `l1_decision_rule` | 0021_control_plane_p1.sql:29 | 5 | 同上 |
| `control_task_l2_metric` | 0022_control_plane_l2_metrics.sql:3 | 6 | 迁移注释「control-plane P2: L2 processing observability」 |

### K · 日程（2 张，全活）

| 表 | 建表处 | 引用 | 归属依据 |
|---|---|---|---|
| `schedule` | 0001_init.sql:111 | 64 | 0001 分节注释「业务（M1-M7）」 |
| `schedule_doc` | 0008_projects.sql:16 | 4 | 0008 迁移注释「R14: project-scoped, versioned schedule documents」 |

### L · 项目与进度风险（8 张，全活）

| 表 | 建表处 | 引用 | 归属依据 |
|---|---|---|---|
| `project` | 0008_projects.sql:3 | 132 | 迁移文件名 |
| `project_member` | 0008_projects.sql:30 | 3 | 同上 |
| `project_aggregate_source` | 0013_project_aggregate_source.sql:3 | 6 | 迁移注释「R16 step C: configurable aggregate project sources」 |
| `project_weekly_report` | 0019_project_weekly_report.sql:3 | 12 | 迁移注释「R22: persisted per-project weekly reports」 |
| `progress` | 0001_init.sql:124 | 29 | 0001 分节注释「业务（M1-M7）」 |
| `risk` | 0001_init.sql:134 | 20 | 同上 |
| `ai_evidence` | 0001_init.sql:145 | 6 | 同上 |
| `reconciliation` | 0001_init.sql:160 | 5 | 同上 |

### M · 名册与人员发现（3 张，全活）

| 表 | 建表处 | 引用 | 归属依据 |
|---|---|---|---|
| `member` | 0001_init.sql:100 | 154 | 0001 分节注释「业务（M1-M7）」；列含 owner_key / role member\|collaborator\|manager |
| `seen_person` | 0004_seen_person_and_bot_secret.sql:4 | 5 | 迁移注释「R5/R6: member candidate discovery + encrypted multi-bot app_secret」 |
| `seen_person_group` | 0008_projects.sql:36 | 3 | 表名前缀（0008 注释归在 R14，此处按语义归名册，**推断**） |

### N · 通知调度与交互确认（4 张，全活）

| 表 | 建表处 | 引用 | 归属依据 |
|---|---|---|---|
| `notification_log` | 0002_notification_log.sql:2 | 1 | 迁移注释「M7 reminder idempotency log」 |
| `system_service` | 0006_system_scheduler.sql:2 | 2 | 迁移注释「R8: global system services and keyword routes」 |
| `system_route` | 0006_system_scheduler.sql:10 | 4 | 同上 |
| `pending_update` | 0001_init.sql:171 | 4 | 0001 分节注释「业务（M1-M7）」；列注释 `pending\|confirmed\|cancelled\|expired §15.2`＝卡片待确认 |

### O · 配置、旧管理后台、审计（4 张，全活）

| 表 | 建表处 | 引用 | 归属依据 |
|---|---|---|---|
| `app_config` | 0001_init.sql:182 | 5 | 0001 分节注释「配置 / 管理 / 审计」 |
| `admin_user` | 0001_init.sql:188 | 3 | 同上 |
| `admin_login_attempt` | 0001_init.sql:197 | 1 | 同上 |
| `audit_log` | 0001_init.sql:205 | 3 | 同上；🔴 G3 `IdentityStore.AppendAudit` 直接对接这张 |

### 分组合计核对

    A 5 ＋ B 2 ＋ C 3 ＋ D 3 ＋ E 5 ＋ F 4 ＋ G 8 ＋ H 6 ＋ I 1 ＋ J 3 ＋ K 2 ＋ L 8 ＋ M 3 ＋ N 4 ＋ O 4 = 61 ✓
    其中改名中转 2（G）⇒ 活业务表 59；其中零引用 2（D、F 各 1）⇒ 真在用 57

## 三 · 主库之外的存储面（R0「删 SQLite」的真实范围）

派工写「Hub 主库只用 PG、删 SQLite（含 fileservice、fanout、board、view 页存储）」。实测：**主库只是一个 SQLite 文件，另有三处独立 SQLite，且它们不属于 Hub 进程**。逐处列清：

| 存储面 | 打开点 | 驱动 | 表 | 张数 | 归属进程 |
|---|---|---|---|---|---|
| Hub 主库 | `cmd/workpulse/main.go:107` → `internal/store/sqlite.go:53` | SQLite（WAL） | 见第二节 | 60 | `workpulse`（Hub 本体） |
| 文件元数据 | `cmd/file-service/main.go:45` → `internal/fileservice/repository.go:24` | SQLite | `files`(:29)、`file_archive_audit`(:40)、`spec_permanent_object`(:47) | 3 | `file-service`（独立进程） |
| 板件（MCS board） | `cmd/mcsd/main.go:26` → `internal/mcs/board/board.go:62`，schema 在 `internal/mcs/board/schema.sql` | SQLite（单连接串行） | `orchestrator`、`task`、`task_role`、`role_exemption`、`agent`、`node`、`board_event`、`node_request`、`task_request`、`result_outbox`、`owner_op` | 11 | `mcsd`（独立进程） |
| 出站/入站 spool | `internal/fanout/spool.go:54` → `repo.OpenSQLite` | SQLite（synchronous=FULL） | `outbound`(spool.go:61)、`inbound_msg`(inspool.go:29)、`inbound_sink`(inspool.go:35)、`waive_audit`(inspool.go:45)、`selfcheck`(admin.go:334) | 5 | `feishu-fanout`（独立进程） |
| datad | `cmd/dingwei-datad/main.go:47` → `internal/datad/pgstore.go:107` | **已是 PG** | `task`、`dispatch`、`config`、`session_state`、`heartbeat`、`skill`、`audit`、`assignment`、`assign_first_result` | 9 | `dingwei-datad` |

要点三条：

1. **SQLite 总量 = 60 ＋ 3 ＋ 11 ＋ 5 = 79 张表，分布在 4 个独立库文件、4 个进程。** 「删 SQLite」不是改一个 `store` 包就完事，`file-service`、`mcsd`、`feishu-fanout` 三个进程各自持有自己的库。
2. **可换池已经就位**：`internal/repo/pool.go` 同时导出 `OpenPG`(:25) 与 `OpenSQLite`(:185)，且 `PoolTuner` 是未导出接口、不交出裸句柄（pool.go:150 起的甲域 CR #6 收口）。fanout 已经走 `repo.Pool`；**但 Hub 主库、fileservice、board 三处仍各自 `sql.Open("sqlite", …)`，没过 `repo` 包**——这是 R0 的第一刀。
3. **`fanout` 的 spool 语义要单独立判据**：`spool.go:3` 明写「202 仅在事务提交（synchronous=FULL）后返回」，即 COMMIT 返回 = 已 fsync。换 PG 后这条持久性语义必须有等价判据（PG 侧对应 `synchronous_commit=on`），不能只测「能写能读」。

## 四 · 三格如实报：0 张表的功能

派工点名的功能里，有三格在主库里**没有表**。不是漏查，是本来就不落库：

| 功能 | 实测结论 | 证据 |
|---|---|---|
| **view 页** | 主库无独立表。解锁器 `hmacViewPageUnlocker` 是**无状态 HMAC**，不落库 | `internal/m8/view_unlock_v2.go:26`、`:42`、`:56` |
| **错误看板** | **完全不落库**。`publishFailureDockEvent` 只发运行时 SSE 事件，无持久化 | `internal/m8/hub.go:3183`、`internal/m8/amr.go:857`、`:915` |
| **文件** | 主库无表；文件元数据在**独立进程的独立 SQLite 库**里（3 张，见第三节） | `internal/fileservice/repository.go:24,29,40,47` |

⇒ **待组长确认**：编排件把「view 页存储」列进 R0 范围，但现网这一格没有存储可迁。是指
（a）新平台要给 view 页**新增**持久化（那就是新建表，不是迁移）；还是
（b）只是沿旧写法列了范围、实际无对象？本件按实测报「0 张表」，不自行假定。

## 五 · 疑似已退役（建了表，Go 非测试引用 = 0）

| 表 | 全树命中（含 .sql 与测试） | 命中内容 | 定性 |
|---|---|---|---|
| `contact_cache` | 1 | 仅 `0001_init.sql:26` 那条建表 | 建了从未被读写 |
| `session_delivery_attempt` | 3 | 仅 `0029_session_delivery_attempt.sql` 的 1 条建表 ＋ 2 条索引 | 同上 |

⇒ R0 建议：**这两张不迁 PG**，但也不在本件里删（删表是改判据，属另一单）。等业主定「旧功能带哪些」时一并处置。若业主决定带，需先补出它们本该有的读写路径——现状是**没有任何代码用它们**。

## 六 · 与 G3 存储接口的对接（既有表能否复用）

G3 设计件 `runbooks/DESIGN-G3-ENROLL-AUTH-API-20261001.md` 第五节定了 `IdentityStore` 与 `EnrollStore` 两个接口，明写「PG 表名与类型由 R0 定」。逐条比对既有表：

| G3 逻辑模型 | 既有表 | 能否复用 | 差在哪 |
|---|---|---|---|
| 用户（user_id / user_id_lc / short_name / type / state / password_hash / must_change_password） | `console_principal` | 🔴 **不能直接复用** | 既有只有 (provider, issuer, subject, display_name, active)。**没有** user_id、short_name、type、state 三态、password_hash。既有 `console_principal` 实际对应 G3 的「身份绑定」，不是「用户」 |
| 身份绑定（provider, issuer, subject → user_id_lc，唯一） | `console_principal` 的 UNIQUE(provider, provider_issuer, provider_subject) | ✅ 形状吻合 | 只需把指向从 principal.id 改成 user_id_lc |
| 注册码（code_digest 唯一 / expires_at / redeemed_at） | 无 | 🔴 **净新建** | 主库无对应表 |
| 会话（token_hash / expires_at / revoked_at） | `console_session` | ✅ **可复用** | 形状完全吻合，只改外键指向 |
| 审计 | `audit_log` | ✅ **可复用** | (actor, action, target, before_json, after_json, ts) 够 `AppendAudit` 用 |
| 入网申请（application_id / pairing_digest / poll_token_hash / state 四态 / pairing_failures / expires_at） | `key_application` | 🔴 **不能复用** | `key_application` 是飞书 bot 密钥申请（applicant_open_id / approver_open_id / service_id），**另一个域**，不是客户端配对入网 |
| Agent 与凭据（name_lc / group_id / origin / state active\|revoked / credential_hash / revoked_at / revoked_by） | `console_agent` ＋ `service_api_key` | ⚠️ **需扩展** | `console_agent.active` 是 0/1 布尔，**表达不了 G3 要求的「吊销即保留行 state=revoked」**（G3 明写要区分 invalid 与 revoked，不能删行）。`credential_hash` 现由 `service_api_key.key_hash` 承担 |

⇒ 结论：**G3 那两个接口需要 3 张净新建表**（用户、注册码、入网申请）＋ **1 处既有表扩展**（`console_agent` 的 active 布尔改 state 枚举），另有 2 张可直接复用（`console_session`、`audit_log`），1 张需拆分（`console_principal` 拆成「用户」＋「身份绑定」）。这部分表名与类型由 R0 定，需要 G2/G3 对齐一次；**建议放在 R0 的 PG schema 首版里一起出**，不要先出旧表迁移、再回头补新表。

## 七 · R0 建议的推进顺序（核心表先行）

派工要求「先改核心表（用户与鉴权、会话端点、编组、文件元数据）」。按依赖与工作量粗排：

| 序 | 切片 | 涉及表 | 主要改动点 | 粗排依据 |
|---|---|---|---|---|
| 1 | 过 `repo` 包收口 | 主库全部 | `internal/store/sqlite.go:53` 的 `sql.Open` 换成 `repo.OpenPG`；4552 行的实现是主战场 | 不先收口，后面每张表都要改两次 |
| 2 | 用户与鉴权 | A 组 5 ＋ B 组 2 ＋ O 组 `audit_log` ＋ 第六节 4 张净新建 | 与 G3 接口对齐 | G3 已在等 PG 实现（设计件第五节 🔵 条明写「R0 就绪前用内存实现」） |
| 3 | 会话端点 | D 组 `session_endpoint`、`session_delivery` ＋ E 组 `service_api_key` | 🔴 `session_endpoint` 的连接 secret 只存 `service_api_key.key_hash`（0003 迁移注释），两张表必须同一事务语义 | 引用 45 ＋ 14 ＋ 8，且跨表约束 |
| 4 | 编组 | C 组 3 | 相对独立 | 引用 20/10/3 |
| 5 | 文件元数据 | fileservice 3 张 | **跨进程**：`file-service` 不在 Hub 里，要单独起、单独测 | 见第三节要点 1 |
| 6 | 其余 SQLite 面 | board 11 ＋ fanout 5 | fanout 的 fsync 语义需等价判据 | 见第三节要点 3 |

**旧功能（G/H/I/J/K/L/M/N 共 35 条建表；8＋6＋1＋3＋2＋8＋3＋4＝35）在业主定「带哪些」之前不动。**

## 八 · 待组长／业主答的问题

1. **表数口径**：派工写 61，实测活表 60（59 业务 ＋ 1 账表）。清单按 61 条建表语句逐条归位，两个口径都列在第一节。是否需要在后续派工里统一改口径？
2. **「view 页存储」何指**：第四节。现网 0 张表、无状态 HMAC。是新增持久化，还是范围里本无对象？
3. **错误看板**：现网完全不落库（只发 SSE）。新平台要不要给错误看板**加**持久化？若要，属新增功能，不在「删 SQLite」这条里，建议单开一格。
4. **退役两表**（`contact_cache`、`session_delivery_attempt`）：不迁 PG 是否可行？还是业主要带（带则需先补出读写路径，现状零代码使用）。
5. **分表切分由谁切**：G2 是 qwen38maxkhovvob4（承办）＋ togpt56terrats（协作）＋ 待回线的 coder56solzzc。本件已给出 15 组归属，建议按**组**切而不是按表切（组内表有跨表约束，见第七节序 3）。切法请组长定，本席不自行认领他人份。
6. **PG schema 首版是否含 G3 净新建表**：第六节。建议含，避免出两版。

## 附 · 取证方法（可重现）

    # 建表语句与归属
    grep -rn "CREATE TABLE" internal/store/migrations/*.sql
    # 活表全集（跑真迁移后 dump，本席用临时测试跑一遍后已删除该临时文件）
    #   61 条建表 − 2 条改名中转 = 59；＋ schema_migrations = 60
    # 各表 Go 引用计数（非测试）
    grep -rn "<表名>" --include=*.go . | grep -v _test.go | wc -l
    # 其它 SQLite 面
    grep -rn "sql.Open(\"sqlite\"" --include=*.go .
    grep -rn "repo.OpenSQLite\|repo.OpenPG" --include=*.go .

🔴 本件为**只读核查产物**：未改任何 schema、未改任何判据、未动 preprod。分支 `qwen38maxkhovvob4/newplat-r0` 上本件是唯一新增文件。
