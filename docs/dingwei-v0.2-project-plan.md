# DingWei v0.2 项目计划

> 状态：评审稿
>
> 部署目标：`ts3poc.wegoab.com`（`193.112.157.47`）
>
> 部署方式：Docker 容器化部署与运维
>
> 设计基线：`docs/dingwei-platform-architecture.md`、`docs/dingwei-prototype.html`、`specs/v0.2/spec.md`

## 1. 目的与边界

本计划把 DingWei v0.2 从第一版可运行系统推进到完整生产形态所需的版本、资源、并行开发拆分、Mock 边界、集成顺序和验收门统一成一份执行入口。

本计划不修改已经收口的 SpecDoc、目标架构、SP3、原型或其评审结论。实施中若发现设计缺口，应登记变更请求并按原治理流程处理，不得由开发任务静默改写设计。

### 1.1 第一版目标

第一版必须闭合以下最小链路：

```text
飞书用户
  → 飞书事件接入
  → 身份绑定与认证
  → DingWei canonical envelope
  → 路由到指定 Agent
  → Agent 收取并处理
  → 回复进入可靠投递
  → 飞书收到回复
  → Web UI 可查看 Agent、消息和运行状态
```

同时满足：

1. 系统可在目标服务器以 Docker Compose 最小化运行。
2. 三个小组可以基于冻结合同独立开发和 Mock 测试。
3. 三组完成后按 3、10、30 Agent 分级集成联测。
4. UI 以原型 v1.51 为基础，补齐真实系统需要的加载、错误、确认、权限和投递状态交互。
5. 第一版保证 30 个 Agent 正常在线并可可靠收发消息。
6. 第一版外部渠道只接飞书；其他渠道保留适配边界，不进入交付范围。

### 1.2 第一版明确不做

- 企微、Slack、Google Chat 等其他渠道。
- 200+ Agent 容量目标。
- 多企业自动供给和完整多租户运营。
- 完整知识治理、skill 晋升和复杂审批。
- Kubernetes 化和跨机房高可用。
- 生产计费、商业化运营和完整多模态附件处理。

未实现能力可以在 UI 中保留入口，但必须明确显示“未启用”，不得返回伪造成功结果。

## 2. 从第一版到完整形态的版本路线

规划共六个版本：第一版 `v0.2`，之后经过四个能力扩展版本，以 `v1.0` 完整生产版收口。版本号表示产品能力阶段，不替代 Git commit、镜像 digest 和部署锚。

| 版本 | 定位 | 核心交付 | 退出条件 |
|---|---|---|---|
| `v0.2` | 最小可运行 | 飞书、30 Agent、统一信封、可靠投递、基础任务、核心 UI、Docker 部署 | 30 Agent 分级联测和 24 小时稳定性验证通过 |
| `v0.3` | 协作闭环 | 项目/任务 DAG、执行与门分离、帖子、通知、摘要、证据和返工 | 一个真实小项目在 DingWei 内完成全生命周期 |
| `v0.4` | 知识与能力 | 分层知识、上下文快照、检索审计、skill/MCP 目录及安装撤销 | 新 Agent 可按授权取得任务上下文与能力，结束后不越权继承 |
| `v0.5` | 治理与多租户 | 企业隔离、principal/角色、待决审批、规则版本、RLS 和安全审计 | 至少两个企业通过数据、权限、消息和 Agent 隔离测试 |
| `v0.6` | 规模化运行 | 200+ Agent、资源池、自动供给、sidecar、多渠道、容灾和完整可观测 | 200+ Agent 稳定运行，单组件故障不导致平台整体失效 |
| `v1.0` | 完整生产形态 | 全域闭环、高可用、灾备、安全、升级回滚、容量与运营体系 | 功能、安全、容量、故障恢复和运维五类验收全部通过 |

### 2.1 v0.2：最小可运行

- 单企业、单飞书应用。
- 飞书私聊和群聊显式 `@机器人`。
- Agent 注册、心跳、上线、下线、寻址和连接代际。
- 消息入站、路由、投递、ACK、重试、死信与去重。
- 基础项目、任务、指派和结果回传。
- Agent 列表、工作面、对话、任务、渠道和健康 UI。
- PostgreSQL、outbox、审计、备份和恢复。
- Docker Compose 一键启动及 30 Agent 容量验收。

### 2.2 v0.3：协作闭环

- 项目、任务和 node DAG。
- planner、executor、reviewer、ops 等角色。
- execution 与 gate verdict 分离。
- FAIL evidence、revision node 和依赖重连。
- 不可编辑帖子流、更正帖、附件引用和封存 digest。
- 通知 taxonomy、待决队列、周报和阻塞管理。

### 2.3 v0.4：知识与能力

- 平台、企业、项目和任务级知识。
- visibility、生命周期、锚和 stale 检测。
- 派发时上下文快照、先授权过滤后检索和检索审计。
- skill/MCP catalog、版本、归属和分发策略。
- 安装、卸载、撤销和 generation fencing。

### 2.4 v0.5：治理与多租户

- 多企业注册与平台库/企业库边界。
- principal、渠道身份和 Agent 溯源绑定。
- 项目角色、effect 权限、规则版本和风险分级。
- 单签、双签、预授权、紧急 allowlist 和吊销。
- RLS、跨企业负测、拒绝面防泄漏和完整审计。

### 2.5 v0.6：规模化运行

- 200+ Agent runtime 资源池。
- 自动供给、启停、重启、租约和回收。
- session generation、owner epoch、横向扩展和故障转移。
- sidecar 紧急恢复面和恢复后审计补账。
- 企微、Slack、Google Chat 等渠道适配。
- 多模态、限流、熔断、背压、灰度和容量预测。

### 2.6 v1.0：完整生产形态

- 八个目标业务域与平台机制完整闭环。
- 多副本、高可用、备份、时间点恢复和灾备演练。
- migration 前滚/回滚、镜像签名、SBOM 和漏洞治理。
- 密钥轮换、最小权限、安全审计和正式 SLI/SLO。
- 安装、升级、扩容、备份、恢复和应急运行手册。

## 3. 各版本需要业主提供的资源

凭据、token、cookie、数据库口令、模型 key 和私钥不得写入本计划、Git、DingWei 消息或测试日志；只通过服务器受控 secret、环境注入或既有凭据通道提供。

### 3.1 资源总表

| 版本 | 必须提供 | 可后补 |
|---|---|---|
| `v0.2` | 三组负责人、总集成负责人、目标服务器授权、飞书应用、模型资源、Agent 配置、试用与验收人员 | 正式监控大屏、额外域名优化 |
| `v0.3` | 真实低风险项目、负责人/执行者/评审者、流程裁定和独立测试 | 更多项目模板 |
| `v0.4` | 首批知识、skill/MCP 清单、知识与能力 owner、分发策略 | 大规模历史知识迁移 |
| `v0.5` | 第二试点企业、安全负责人、权限审批规则、隔离数据库资源 | 外部合规认证 |
| `v0.6` | 扩容节点、200+ Agent 模型容量、多渠道应用、运维值守 | 异地灾备机房 |
| `v1.0` | 生产集群、正式域名与证书、安全测试、灾备、正式上线签批 | 商业化运营资源 |

### 3.2 v0.2 资源

#### 人员

- 第一组：建议 2 名后端。
- 第二组：建议 2 名后端/基础设施开发。
- 第三组：建议 1 名前端和 1 名全栈或 DevOps。
- 1 名不兼任主要开发的独立测试人员。
- 1 名总集成负责人，负责合同冻结、跨组门禁和 release set。
- 1 名业务验收人，3～5 名飞书试用人员。

每组缩减为 1 人仍可施工，但会增加单点风险并延长 Mock、实现和测试周期。

#### 目标服务器

业主需授权对 `193.112.157.47`：

1. 先做 CPU、内存、磁盘、端口、Docker、nginx、TLS 和现有负载的只读勘察。
2. 勘察通过并取得部署授权后，创建 DingWei 专用目录、网络、volume 和容器。
3. 配置 `ts3poc.wegoab.com` 对应 UI/API/飞书回调反向代理。
4. 创建 PostgreSQL、备份任务、日志轮转和健康检查。
5. 执行受控启停、升级、回滚和故障演练。

初始容量估算为 16 vCPU、32 GB RAM、200 GB 可用 SSD；这只是勘察前的规划值，正式配额必须由 3/10/30 Agent 实测曲线确定。

#### 飞书

- 一个 v0.2 企业自建应用和应用管理员。
- 机器人、事件订阅和回调地址配置权。
- 私聊、群内显式 `@机器人`、发送消息和卡片所需权限。
- 一个开发联调群。
- 普通用户、项目负责人、平台管理员三类测试身份。

#### Agent 与模型

- 第一版支持的 Agent/CLI 类型。
- 每类 Agent 的模型提供方、账号或 API 配额和费用归属。
- 账号能否共享及供应商并发限制。
- 30 个 Agent 的稳定全名、角色、所属组和环境 profile。
- 明确 30 Agent 验收是全真实，还是允许真实 Agent 与模拟 Agent 分阶段完成。

建议先以 3 个真实 Agent 闭合业务链，扩至 10 个真实/轻量 Agent，再以 30 个 Agent 验证容量；若要求 30 个均为真实模型会话，必须同步提供模型并发额度、费用和更高内存资源。

#### 产品裁定与验收

- 是否确认 v0.2 为单企业、飞书单渠道。
- 普通用户是否可直接选择 Agent。
- Agent 新建、关闭、重启权限。
- 项目创建、任务指派和人工审批权限。
- 第一版必须上线的原型页面。
- “30 Agent 正常运行”的准确 SLO。
- 是否承载真实业务，或只作为内部试点。
- 至少 24 小时稳定性观察和一次恢复演练窗口。

### 3.3 v0.3 资源

- 1～2 个真实、低风险试点项目。
- 每个项目的 owner、executor、reviewer 和 ops。
- 真实任务拆解、评审、返工和交付样例。
- 必须独立评审的任务类型及职责互斥规则。
- 取消、暂停、重派、FAIL 返工和通知规则。
- 至少两个完整项目周期的试运行窗口。

### 3.4 v0.4 资源

- 平台纪律、企业制度、项目介绍、开发测试发布手册和故障手册。
- 每份知识的 owner、可见范围、有效期、权威源和更新方式。
- 首批 skill/MCP 清单、owner、版本、环境和使用范围。
- 安装、升级、撤销、强制推送和人工审批策略。
- 至少一名知识管理员和一名能力管理员。

### 3.5 v0.5 资源

- 第二个企业或内部隔离测试组织。
- 两企业各自的管理员、普通用户和明确不可互访的数据。
- 平台库、至少两个企业数据库及最小权限账号。
- 安全负责人和独立权限测试人员。
- 低、中、高危动作以及单签、双签、预授权规则。
- 数据留存、解绑、离职、吊销和紧急操作制度。
- 跨企业、越权、重放和伪造身份专项测试授权。

### 3.6 v0.6 资源

- 至少 2～3 台 Agent runtime 节点。
- 具备故障迁移能力的控制/消息节点和独立 PostgreSQL。
- 对象存储、集中日志、指标、告警和备份空间。
- 200+ Agent 的模型账号、并发/token 配额和月度预算。
- 主备模型策略及容量降级规则。
- 每个新增渠道的企业应用、管理员、权限、测试群和测试账号。
- 运维 owner、值守时段、升级联系人、变更窗口和故障演练安排。

### 3.7 v1.0 资源

- 正式生产集群、域名、证书、高可用 PostgreSQL 和对象存储。
- 正式 secret 管理、集中日志、告警、备份和异地灾备。
- 渗透测试、依赖/镜像扫描、数据分类和密钥轮换制度。
- 产品、技术、安全和运维四类验收负责人。
- 首批正式用户、上线窗口、回滚窗口和上线后观察期。

## 4. v0.2 三组并行开发拆分

### 4.1 第一组：控制平面与数据基础

责任范围：`EDGE + IDN + GOV-lite + WRK-lite + DATA`。

交付任务：

- v0.2 服务骨架、统一配置和依赖注入。
- `AccessCtx`、统一接口信封和错误分类。
- 飞书身份到 principal 的绑定与解析。
- 企业、用户、Agent、项目和任务基本模型。
- 最小任务状态机：`created → assigned → running → succeeded/failed/cancelled`。
- PostgreSQL repository、migration、事务、outbox 和幂等表。
- API 鉴权、审计、request/correlation ID。
- 控制平面 OpenAPI、事件 schema、fixture 和 Mock server。

MVP API：

- `POST /api/v1/auth/feishu/session`
- `GET /api/v1/me`
- `GET/POST /api/v1/agents`
- `GET /api/v1/agents/{id}`
- `GET/POST /api/v1/projects`
- `GET/POST /api/v1/tasks`
- `POST /api/v1/tasks/{id}/assign`
- `POST /api/v1/tasks/{id}/result`
- `GET /api/v1/audit-events`
- `GET /healthz`、`GET /readyz`

组内 Mock：飞书身份、Agent 在线目录、消息投递成功/超时/重复 ACK、固定时钟和固定 ID。

完成门：

1. 空库可一键 migration。
2. 所有写接口以 `request_id` 幂等。
3. 请求自带的企业/principal 不得覆盖服务端上下文。
4. outbox 在进程重启后不丢事件。
5. API 合同和 PostgreSQL 集成测试通过。

### 4.2 第二组：消息、Agent 运行与飞书渠道

责任范围：`MSG + AGT + Feishu Adapter + sessionHelper/sidecar`。

交付任务：

- canonical envelope 编解码。
- Agent 稳定全名、连接代际、在线目录和心跳。
- 消息寻址、队列、投递、ACK、重试和死信。
- 重复消息、重连和迟到 ACK fencing。
- 飞书 webhook 验签、challenge 和事件去重。
- 飞书消息与 canonical envelope 双向转换。
- 私聊和群聊显式 `@机器人`。
- 飞书限流、退避和错误分类。
- Agent runtime Docker 镜像和 30 Agent 负载工具。
- sidecar 最小健康面；紧急动作不扩大既有授权边界。

MVP API：

- `POST /webhooks/feishu/events`
- `GET /ws/agents`
- `POST /internal/v1/envelopes`
- `POST /internal/v1/deliveries/{id}/ack`
- `GET /internal/v1/sessions`
- `GET /internal/v1/agents/{id}/health`
- `POST /internal/v1/agents/{id}/commands`
- `GET /healthz`、`GET /readyz`

组内 Mock：第一组 AccessCtx/Agent/Task API、飞书 OpenAPI 的 200/429/5xx/超时、Agent 在线/忙/重连/重复 ACK/迟到 ACK。

完成门：

1. 同一飞书事件重复到达不产生重复业务 effect。
2. 旧连接不能 ACK 新 generation 的投递。
3. 服务重启后未完成投递可以恢复。
4. 单 Agent 故障不阻塞其他 Agent。
5. 30 Agent 模拟连接稳定运行至少 2 小时。

### 4.3 第三组：Web UI、协同面与部署可观测

责任范围：`UI + COL-lite + Portal + deployment/observability`。

交付任务：

- 把原型 v1.51 转换为真实前端路由和组件。
- 登录态、权限守卫、Agent 列表和 Agent 工作面。
- Agent 对话、任务列表、任务指派和结果展示。
- 飞书渠道状态、投递记录、失败原因和受控重试。
- 最小审计/帖子时间线。
- Docker Compose、nginx、健康检查和部署脚本。
- 结构化日志、指标、告警、备份、升级和回滚 runbook。
- 集成测试入口和演示 fixture。

第一版优先页面：

1. 登录与平台概览。
2. Agent 列表。
3. Agent 工作面。
4. 与 Agent 交互。
5. 最小任务/项目看板。
6. 渠道状态。
7. 投递与审计记录。
8. 平台运行健康。

补充交互：加载、空数据、无权限、服务不可用、表单校验、防重复提交、危险操作确认、消息状态、Agent 状态、搜索筛选分页、correlation ID 复制和“未启用”提示。

组内 Mock：由冻结 OpenAPI 生成控制面 Mock，以 SSE/WS fixture 模拟 Agent 状态，提供 30 Agent 名册及成功、超时、无权限、冲突和部分失败场景。

完成门：

1. 不依赖真实飞书和真实 Agent 即可完整演示。
2. 所有写操作正确处理重复点击和后端冲突。
3. 关键页面有组件测试和端到端测试。
4. Docker Compose 可在空环境按文档拉起。
5. UI 和监控均可见健康、依赖和失败信息。

## 5. 跨组合同与 Mock 纪律

三个组开工前冻结 `contracts/v0.2-mvp1`：

- `openapi.yaml`
- `asyncapi.yaml` 或等价事件 schema
- `canonical-envelope.schema.json`
- `access-context.schema.json`
- `agent-status.schema.json`
- `error.schema.json`
- `mock-fixtures/`
- 合同版本与兼容规则

首批核心事件：

- `PrincipalBound`
- `AgentRegistered`
- `AgentHealthChanged`
- `SessionConnected`、`SessionDisconnected`
- `EnvelopeAccepted`
- `DeliveryQueued`、`DeliveryAcknowledged`、`DeliveryFailed`
- `TaskAssigned`、`TaskResultRecorded`

规则：

1. 字段只可兼容增加；改名、删字段或改变语义必须升合同版本。
2. command 必带 `request_id`。
3. event 必带 `event_id`、`schema_version` 和 `correlation_id`。
4. Agent 只见 canonical envelope，不感知飞书协议。
5. Mock 与真实实现共同运行 consumer-driven contract tests。
6. 合同变化必须同步三组并登记，不以口头约定替代。

## 6. v0.2 部署拓扑

```text
Internet
   │
ts3poc.wegoab.com
   │
[宿主 nginx / TLS]
   │
[dingwei-edge]
   ├── [dingwei-ui]
   ├── [dingwei-control]
   ├── [dingwei-message]
   └── [feishu-adapter]
             │
       [dingwei-worker]
             │
       [PostgreSQL]
             │
  [agent-runtime-01 ... agent-runtime-30]
```

Compose 服务建议：

- `nginx`
- `ui`
- `control`
- `message`
- `worker`
- `feishu-adapter`
- `postgres`
- `agent-runtime`，通过 profile 或生成配置启动 30 个实例
- `mock-feishu`、`mock-control`、`mock-agent`，仅开发 profile
- `prometheus`、`grafana`，在 MVP 后半段纳入

持久化 volume：PostgreSQL、消息恢复数据、Agent 工作目录、审计导出和备份。证书与 secret 只引用宿主受控路径。

部署门：

1. 镜像带 commit、构建时间和制品 SHA，不以 `latest` 作正式部署锚。
2. migration 独立执行；失败时应用不得带病启动。
3. 容器设置健康检查、资源限制和日志轮转。
4. 发布前登记组件、环境、commit 和镜像 digest。
5. 未经授权不得直接操作目标服务器或飞书正式应用。

## 7. v0.2 里程碑与集成计划

### M0：实施准备，计划 2～3 个工作日

- 冻结 MVP 范围、API、事件和 canonical envelope v1。
- 建立三个组的目录、CI、Mock profile 和合同测试。
- 确认飞书应用权限和回调域名。
- 只读勘察目标服务器，输出资源、端口和 volume 清单。

退出条件：三个组只依赖 Mock 均能启动和测试自己的交付物。

### M1：最小骨架，计划 3～5 个工作日

- 控制面可启动并连接 PostgreSQL。
- 消息面可接受模拟 Agent。
- UI 可读取 Mock Agent 名册。
- Compose 可拉起开发环境。
- 飞书 webhook challenge 通过。

退出条件：模拟飞书消息能够穿过系统并由模拟 Agent 回应。

### M2：三组并行实现，计划 1～2 周

- 第一组完成身份、Agent、任务和 outbox。
- 第二组完成飞书、会话、投递和 Agent runtime。
- 第三组完成核心 UI、部署件和运维面。
- 各组在不依赖其他组临时环境的前提下通过组内 E2E。

退出条件：单元测试、集成测试、合同测试和组内 E2E 全绿。

### M3：组合集成，计划 3～5 个工作日

按以下顺序接真：

1. 控制平面 + UI。
2. 控制平面 + 消息面。
3. 飞书适配 + 模拟 Agent。
4. 真控制平面 + 真消息面 + 真 UI。
5. 真飞书 + 3 Agent。
6. 扩至 10 Agent。
7. 扩至 30 Agent。

任一级失败，先在当前规模修复，不跳级掩盖问题。

### M4：30 Agent 稳定性验证，至少 24 小时

- 30 Agent 同时在线并可分别寻址。
- 30 Agent 同时收消息不串线。
- 正常投递成功率目标不低于 99%，且不允许静默丢失。
- 重复事件不产生重复业务 effect。
- 重启 message、worker、PostgreSQL 客户端连接和单 Agent 后可恢复。
- 飞书 429、短时 5xx 和超时可退避恢复。
- 日志可按 `request_id/correlation_id/delivery_id` 串联。
- 无持续增长的队列、连接、goroutine、内存或磁盘泄漏。

最终 SLO 由业主在 M0 冻结；上述数值是评审稿建议值。

### M5：ts3poc 第一版发布

冻结 release set：Git commit、镜像 digest、Compose、env 模板、migration、飞书权限、健康检查、备份点、回滚命令和 30 Agent 验收报告。

## 8. v0.2 工作分解编号

| 编号 | 任务 | 主责 |
|---|---|---|
| `V02-000` | MVP 范围与合同冻结 | 总集成负责人 + 三组 |
| `V02-001` | 仓库、目录、CI 和开发 profile | 三组共同 |
| `V02-010` | AccessCtx、接口信封和错误模型 | 第一组 |
| `V02-011` | 企业、principal 与飞书身份绑定 | 第一组 |
| `V02-012` | Agent、项目和任务控制面 | 第一组 |
| `V02-013` | PostgreSQL migration、outbox 与审计 | 第一组 |
| `V02-020` | canonical envelope | 第二组 |
| `V02-021` | session 与 Agent 在线目录 | 第二组 |
| `V02-022` | 投递、ACK、重试、死信与 fencing | 第二组 |
| `V02-023` | 飞书事件接入与回复 | 第二组 |
| `V02-024` | Agent runtime 镜像与 30 Agent 工具 | 第二组 |
| `V02-030` | Web 骨架与权限守卫 | 第三组 |
| `V02-031` | Agent 列表与工作面 | 第三组 |
| `V02-032` | Agent 对话与消息状态 | 第三组 |
| `V02-033` | 任务看板与审计时间线 | 第三组 |
| `V02-034` | 渠道与系统健康页 | 第三组 |
| `V02-040` | Docker Compose、nginx 与部署脚本 | 第三组 |
| `V02-041` | 可观测、备份与回滚 | 第三组 |
| `V02-050` | 跨组合同测试 | 总集成负责人 + 三组 |
| `V02-051` | 3/10/30 Agent 分级联测 | 三组 + 独立测试 |
| `V02-052` | 24 小时稳定性测试 | 独立测试 + 运维 |
| `V02-053` | ts3poc 发布和部署锚登记 | 运维 + 总集成负责人 |

## 9. 质量门与风险

### 9.1 每版本共同质量门

- 范围、合同和验收标准先冻结。
- 开发自测与独立测试分离。
- Mock 和真实实现使用同一合同测试。
- 不允许跳过失败测试或把 UNKNOWN 记为 PASS。
- 发布只来自干净 commit 和可复现镜像。
- 数据 migration、备份、恢复和回滚均有演练证据。
- 生产或正式渠道动作必须逐项授权。

### 9.2 当前主要风险

| 风险 | 影响 | 控制措施 |
|---|---|---|
| 30 个真实模型 Agent 的资源与费用未确定 | v0.2 容量口径不清 | M0 冻结真实/模拟比例和供应商并发额度 |
| 单机资源不足或与现有服务冲突 | 无法稳定部署 | 部署前只读勘察，按 3/10/30 曲线分配资源 |
| 三组接口漂移 | 集成延迟 | 先冻结合同，consumer-driven contract test 阻断不兼容变更 |
| 飞书限流、重试和重复事件 | 重复 effect 或消息堆积 | 快速 ACK、异步处理、幂等、退避和死信 |
| UI 原型直接照搬静态状态 | 真实错误和权限面缺失 | 补齐加载、失败、权限、确认、重试和审计交互 |
| MVP 把后续完整治理提前做完 | 版本失控 | 严格执行第一版不做清单，后续只保留接口边界 |

## 10. 开工前业主确认清单

1. 指定三个开发小组及负责人。
2. 指定总集成负责人和独立测试人员。
3. 授权对 `193.112.157.47` 做只读资源勘察。
4. 勘察后单独授权 Docker/nginx/PostgreSQL 部署动作。
5. 提供或安排创建飞书企业自建应用、管理员和测试群。
6. 确定 30 Agent 全真实或真实+模拟的分阶段口径。
7. 确定 Agent 类型、模型、并发额度和费用来源。
8. 指定 3～5 名试用人员和最终业务验收人。
9. 确认 v0.2 单企业、飞书单渠道范围。
10. 冻结 30 Agent SLO、观察窗口和故障演练范围。

## 11. 评审要求

评审人应给出 `PASS` 或 `NO`，并一次性列全 findings。重点检查：

1. 六版本边界是否递进且无关键能力遗漏。
2. 各版本资源是否足以支撑退出条件。
3. v0.2 三组边界是否能真正并行，是否存在双 owner 或无人负责。
4. Mock 合同能否支撑独立开发并在集成时替换为真实实现。
5. 30 Agent、飞书接入、Docker 运维和 UI 交互是否具备可验收口径。
6. 是否违反目标架构的不变量、权限边界或数据边界。
7. 工期估计与依赖是否遗漏。

所有意见收拢后，本文件将修订为“执行版”，补充评审处置表、冻结锚、明确 owner 和最终里程碑；执行版提交前不得把本评审稿当作已批准开工令。
