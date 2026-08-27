# DingWei 平台目标架构 v1.4

> 本件从新版 SpecDoc 与原型重新设计目标架构，同时承载分域定义、接口设计与技术架构设计。
> 旧件已移至 `docs/v0.1/dingwei-architecture-design-v0.34.md`，仅作历史参考，不是本件输入，也不是本件的修改对象。

## 0. 文档头块

| 项 | 值 |
|---|---|
| **Architecture ID** | `dingwei-platform-target-architecture` |
| **版本** | v1.4 |
| **状态** | draft，待全文评审 |
| **执笔** | `zzc-chatgpt56soltchangshark-2642` |
| **评审** | `zzc-opus50tchangshark-2642`、`zzc-deepseekv4pro08130ca16c13-2642` |
| **施工合同** | `specs/platform-architecture/spec.md` |
| **输入基线** | `acc531e55dce80e8c0e11db9abb4a5b6e86fb08d` |
| **可否施工** | **否**；全文评审 PASS 前仅可作设计评审输入 |

输入锚：`acc531e55dce80e8c0e11db9abb4a5b6e86fb08d` · `docs/dingwei-platform-specdoc.md` · `docs/dingwei-prototype.html`

| 输入 | 冻结指纹 | 用途 |
|---|---|---|
| SpecDoc v3.77 | sha256 `8889a2e8fc73ffccee17555362a4e9be8da9f700c15273b4092c3a9c0b38baff` | 规范、业主裁定、约束与验收来源 |
| 原型 v1.48 | sha256 `f2995730ca986ae38993f62391312982e7ed5026dab990c78ff29a6bb00ef9cb` | 可达入口、操作者视角与交互状态来源 |
| 当前代码 | 冻结 commit 中的 `cmd/`、`internal/` | 只证实现现状与迁移面，不决定目标域 |

### 0.1 本件回答什么

本件回答三个问题：

1. **平台目标态由哪些业务域构成，各域拥有什么、不拥有什么；**
2. **域内、跨域与平台边界通过哪些稳定接口协作；**
3. **这些域与接口怎样落到进程、部署、数据、安全、可靠性、可观测与迁移结构。**

本件不复述产品规则；规则由 SpecDoc 权威定义。本件只在需要确定责任边界、接口 effect 或技术不变量时引用它。

SpecDoc 正文现行定义与 F.0 索引用于识别其自身现行/撤销条款；附录 F 中已撤销/改判的过程记录不进入目标设计。
SpecDoc、原型、代码三件之间**不自设优先级**：任一语义冲突均按施工合同 §7.3 登记精确锚与两种后果，
交 coordinator/业主裁定。代码只证明现状，但这条事实边界不能替代对冲突目标的裁定。

### 0.1.1 SYNC IMPACT REPORT（v1.0 → v1.1）

- 增加 SpecDoc、生产 Go 包目录、原型 nav、责任主题四组机器覆盖声明，不再依赖手写分母。
- 增加目标态 `new` 单元表，补齐迁移状态；原 8 域、40 接口、数据/运行/安全/迁移结论不变。
- 覆盖检查由 `specs/platform-architecture/check.sh` 产出四集合；UNKNOWN 非空不能宣称覆盖闭合。

### 0.1.2 SYNC IMPACT REPORT（v1.1 → v1.2）

- SpecDoc 责任覆盖扩至全部二级附录项；责任表逐项补 B2/C/E/F 主责。
- 接口目录新增 PLT 的 access transaction、migration、RLS conformance、audit、health、metrics 与 reconcile 稳定接口；
  `IF-01`–`IF-10` 均有可解析落点。
- 现状/目标映射全部使用五态受控 token，移除 transition/consolidate 等自由状态。
- 跨件冲突不再由本件自排优先级；统一登记后交 coordinator/业主裁定。

### 0.1.3 SYNC IMPACT REPORT（v1.2 → v1.3）

- 本件接管 `DELIVERY-STATUS`、评审包、证据采集与架构引用核验，成为现行架构唯一事实源。
- 旧件移入 `docs/v0.1/`；现行 SpecDoc / 原型依赖点改指本件，旧式架构节号只存在于历史件。

### 0.2 重做边界

- 不沿用旧架构的章节、域表、接口表、包归属或部署结论。
- 不以现有目录命名目标域；目录只在第 12 节映射到已经定义好的目标域。
- 不把代码现状包装为目标设计；现状与目标分栏记录。
- 不实施代码、schema、部署与 cutover。
- 新件通过并切换后，`DELIVERY-STATUS` 的「架构设计」行替换为本件；旧件转历史，不形成双权威源。

## 1. 架构总览

### 1.1 核心选择：模块化控制平面 + 专用实时/异步执行单元

DingWei 的业务一致性集中在企业、人、项目、任务、门、知识、能力与帖子上；这些对象跨功能频繁组合，
但吞吐并不足以证明需要拆成大量网络服务。因此目标态采用：

- **一个模块化控制平面**承载八个业务域，域间只经应用接口协作；
- **会话投递、渠道接入、异步 worker、Agent sidecar**作为专用运行单元；
- **平台库 + 每企业独立 PG 库**作为数据边界；
- **同库事务保证域内强一致，跨域/跨进程使用 outbox + 幂等消费**；
- 先守住模块边界，只有独立扩缩容、故障域或监管边界有实证时才拆服务。

```text
渠道 / 浏览器 / Agent / ops
            │
      [EDGE-01 统一入口]
            │ 已验证 SessionAuth
   ┌────────┴──────────────────────────────┐
   │ CTRL-01 平台控制平面（模块化单体）   │
   │ IDN GOV WRK KNO CAP COL AGT           │
   └──────┬─────────┬────────────┬─────────┘
          │         │            │
     MSG-01      JOB-01       DATA-01
   会话与投递    异步 worker   受控仓储层
          │         │            │
       FCS/IM    摘要/通知/       ├─ 平台库
       Hub/WS    provision        └─ N 个企业库
          │
   SIDE-01 Agent sidecar（本地运行与紧急面）
```

### 1.2 架构层

| 层 | ID | 职责 | 禁止 |
|---|---|---|---|
| 接入适配 | `ADP` | HTTP/WS/IM/UI/CLI 协议转换、认证材料接入 | 定业务授权、直接 SQL、伪造 principal |
| 应用编排 | `APP` | use case、事务边界、域调用顺序、outbox | 绕过域不变量、把 Agent 当授权主体 |
| 业务域 | `IDN/GOV/WRK/KNO/CAP/COL/AGT` | 权威模型、策略与业务接口 | 依赖具体 HTTP/DB SDK、跨域改表 |
| 消息投递域 | `MSG` | 会话、信封、路由、投递、渠道回执 | 决定业务权限、读取企业正文作路由 |
| 平台机制 | `PLT` | 仓储、策略执行、审计、时钟、机密、事件总线 | 自创业务规则 |
| 基础设施 | `INF` | PG、对象存储、Git、IM、LLM、edge | 成为业务事实源 |

依赖只允许从上到下；业务域之间依赖接口，不依赖对方存储实现。

## 2. 目标态业务域

### 2.1 域清单

| 域 ID | 名称 | 使命 | 权威数据 | 主责 SP |
|---|---|---|---|---|
| `IDN` | 身份与组织域 | 将企业、自然人、渠道、会话与 Agent 绑定到可信主体，产出 `AccessCtx` | enterprise registry、principal、channel binding、session auth、agent provenance binding | SP1 |
| `GOV` | 规范与治理域 | 管 spec、规则版本、风险分级、预授权、待决、平台批次与门方资格 | spec metadata、rule set、approval、decision、policy manifest | SP0·SP8 |
| `WRK` | 工作编排域 | 管项目、任务、node、角色、门 verdict、发版与复盘的生命周期 | project、board_task、node、role binding、gate verdict、release record | SP2 |
| `KNO` | 上下文与知识域 | 管知识条目、visibility、生命周期、装载快照与运行态查询 | knowledge item、anchor、visibility、context snapshot、retrieval audit | SP3 |
| `CAP` | 能力资产域 | 管 skill/MCP catalog、归属、晋升、分发、安装与卸载 | asset、catalog entry、version、distribution policy、install state | SP4 |
| `COL` | 协同与证据域 | 管帖子、通知、摘要、周报、附件、封存与 digest | post、thread gate、notification、digest、attachment ref、weekly report | SP5 |
| `AGT` | Agent 运行域 | 管 Agent 实例、环境工单、资源租约、生命周期、sidecar 与健康 | agent instance、runtime order、resource lease、health、emergency session | SP6 |
| `MSG` | 会话与投递域 | 管 canonical envelope、在线会话、寻址、投递、ACK、渠道适配 | session endpoint、envelope、delivery、route、channel receipt | SP6（通信基座） |

`PLT` 不是业务域；它是所有业务域共用的受控机制层，由 SP7a/SP7b 提供仓储与迁移能力。

### 2.1.1 责任主题覆盖

<!-- RESPONSIBILITY-COVERAGE-BEGIN -->
| 主题 | 主责域/层 | 接口族 | 验收 |
|---|---|---|---|
| `RT-01` | IDN | `IDN-*`、`MSG-CMD-AcceptEnvelope` | `AC-S01`、`AC-S02` |
| `RT-02` | GOV | `GOV-*` | `AC-S01`、`AC-S02` |
| `RT-03` | WRK | `WRK-*` | `AC-S01`、`AC-S02` |
| `RT-04` | KNO | `KNO-*` | `AC-S01`、`AC-S02` |
| `RT-05` | CAP | `CAP-*`、`AGT-CMD-ActivateInstance` | `AC-S01`、`AC-S02` |
| `RT-06` | COL | `COL-*` | `AC-S01`、`AC-S02` |
| `RT-07` | AGT | `AGT-*`、`MSG-*` | `AC-S01`、`AC-S02` |
| `RT-08` | PLT | `IDN-QRY-ResolveDataScope`、Repository ports | `AC-S01`、`AC-S02` |
<!-- RESPONSIBILITY-COVERAGE-END -->

### 2.2 域边界与禁止职责

#### 2.2.1 `IDN` 身份与组织域

**拥有**：企业注册、principal、渠道账号绑定、会话认证、Agent 溯源绑定、环境 assurance、`AccessCtx`。

**不拥有**：项目角色、node 指派、业务授权决定、帖子内容、企业数据库里的领域记录。

**不变量**：

1. 一次请求只有一个 enterprise 与一个 principal；缺一即失败关闭。
2. enterprise 由平台 ops 注册表与可信认证材料派生，不信请求中的 org/tenant 字段。
3. `agent_id` 只与已认证 principal 做溯源绑定；它不能提升 principal、assurance 或 capability。
4. 多企业同一自然人表现为不同 principal，不存在跨企业自动合并。
5. 渠道解绑不改写历史 actor；吊销后只影响未来会话。

#### 2.2.2 `GOV` 规范与治理域

**拥有**：spec 状态、规则集 version、风险属性、门要求、预授权、审批、平台域批次、待决事项。

**不拥有**：node 执行状态、实际 deploy、Agent 运行、知识内容。

**不变量**：

1. 门方与执行者按 principal 比对，不按会话或 Agent。
2. 批次审批强度取最高危项，只能单调收紧。
3. 预授权不能降低动作固有风险；过期、超范围或代际不符即失效。
4. 冲突 gate 跨责任域并存；只有预定义 subsumption 可消解。

#### 2.2.3 `WRK` 工作编排域

**拥有**：项目责任、`board_task`、node DAG、角色、派发、执行结果、独立 gate verdict、发版记录、复盘。

**不拥有**：消息传输、人员身份、真实 deploy adapter、知识条目与帖子正文。

**不变量**：

1. DAG eligibility = execution done 且 verdict 属于 `not_required/pass`；`fail` 不放行下游但仍终结并封存该 node。
2. gate FAIL 与 execution failed 分流；返工新建 revision node，不原地重写历史。
3. mutating command 带 request_id、expected generation/state 与 actor principal。
4. 实际 deploy/rollback 由受授权、幂等、审计的 capability 执行，node 只编排。

#### 2.2.4 `KNO` 上下文与知识域

**拥有**：知识条目、锚、归属级、visibility、生命周期、上下文快照、检索审计。

**不拥有**：运行日志原始数据、skill/MCP schema、任务执行状态。

**不变量**：

1. 先按 enterprise/principal/visibility 过滤，后检索。
2. 运行态数据经 MCP 查询，不固化为知识事实。
3. 任务上下文以派发快照为准，任务结束后不继承未授权上下文。
4. 条目过期不静默删除；进入 suspect/archived 并保留锚。

#### 2.2.5 `CAP` 能力资产域

**拥有**：skill/MCP 元数据、归属、版本、晋升候选、分发政策、安装/卸载状态。

**不拥有**：MCP 运行数据源、Agent 实例、审批 verdict。

**不变量**：

1. skill 不内嵌易变 API schema；运行时经 catalog/MCP 查询。
2. D0/D1 push，D2 按 mandatory 判 push/pull，D3 只在项目域内。
3. 安装与卸载对称；撤销须 generation/fencing，迟到 ACK 不得复活旧版本。
4. 跨企业无扩散路径；企业内跨人只走受控沉淀规则。

#### 2.2.6 `COL` 协同与证据域

**拥有**：帖子流、线程闸、通知、摘要 observation、周报、附件引用、封存 digest。

**不拥有**：node 状态、授权 verdict、对象存储 blob、渠道传输。

**不变量**：

1. 帖子入库即不可编辑；更正为新帖 + `in_reply_to`。
2. 归属链按发帖时刻冗余存并进入 digest；不与今天链路比较。
3. execution done 且 gate verdict 非 pending 时封存；FAIL evidence 先落账再算 digest。
4. 通知只从已批准 taxonomy 产生，经 outbox 投递；正文不决定路由。

#### 2.2.7 `AGT` Agent 运行域

**拥有**：Agent 实例、形态、环境工单、资源租约、健康、启停、sidecar 紧急会话。

**不拥有**：principal、项目授权、任务判定、skill 资产定义。

**不变量**：

1. Agent 由 principal 派生，但 Agent 不是 principal。
2. 绑定完成前 agent 记录不得作审计交叉证据；绑定后仍不得作授权或门判定。
3. 环境 profile 来自部署件；客户端自报环境不生效。
4. 紧急面只允许单调收紧的 allowlist effect，离线审计恢复后强制补账。

#### 2.2.8 `MSG` 会话与投递域

**拥有**：在线 session、canonical envelope、route、delivery/outbox、ACK、渠道 receipt。

**不拥有**：业务授权、任务状态、帖子正文语义、企业数据查询。

**不变量**：

1. 只有认证器可写 canonical principal；调用方自带身份字段在 edge 删除。
2. connection → envelope → receiver 传播同一不可变 `SessionAuthRef`。
3. 投递至少一次；消费者以 delivery_id/event_id 幂等，不宣称网络恰一次。
4. 路由只读可信结构化头，不读正文决定权限或跨边界投递。

### 2.3 权威数据唯一性

| 对象 | 唯一 owner | 其他域如何使用 |
|---|---|---|
| enterprise / principal / channel binding | `IDN` | 只持稳定 ID 与签发的 `AccessCtx` |
| spec / approval / rule set | `GOV` | 读取不可变 version/ref，不复制当前状态 |
| project / task / node / gate verdict | `WRK` | 事件或 query；不得跨域 UPDATE |
| knowledge / context snapshot | `KNO` | 引用 item/snapshot ID |
| skill/MCP asset / install state | `CAP` | Agent 只持已解析版本与 receipt |
| post / digest / attachment reference | `COL` | WRK 只持 thread/digest ref |
| agent instance / resource lease | `AGT` | WRK 只持 agent_id 与 assignment ref |
| envelope / delivery / channel receipt | `MSG` | 业务域只持 envelope/delivery ref |

### 2.4 分域取舍

| 备选 | 不取原因 | 当前选择 |
|---|---|---|
| 按 `internal/` 包直接成域 | 包是历史实现与技术切面，`datad/m8/model` 均跨多项责任 | 先按责任/数据/事务定八域，再映射包 |
| 按 SP0–SP8 一一成域 | SP 是交付拆分与依赖阶段；SP7 是机制、SP6 横跨多个业务责任 | SP 作为交付 owner，不作为领域边界 |
| GOV 与 WRK 合并 | 规则/批准的生命周期与任务/node 的高频状态机不同；合并会让执行状态改写政策事实 | GOV 管「可不可以」，WRK 管「做到了哪」 |
| AGT 与 MSG 合并 | Agent 资源生命周期与会话/投递的扩缩容、故障和一致性键不同 | AGT 管实例，MSG 管连接与交付 |
| MSG 只作无状态基础设施 | session generation、delivery/ACK、route 与去重本身有权威状态和不变量 | MSG 作为明确责任域，但不做业务授权 |
| 每业务域独立数据库/微服务 | 当前无容量或监管证据支撑，且会引入跨服务事务与运维成本 | 企业内共享 PG，schema/接口守模块边界 |

## 3. 关键上下文模型

### 3.1 `AccessCtx`

`AccessCtx` 是一次已认证访问的不可变上下文，不是客户端 DTO：

| 字段 | 来源 | 用途 | 可否为空 |
|---|---|---|---|
| `enterprise_id` | ops 企业注册表 + 已验证渠道/会话绑定 | 选择企业库 | 否 |
| `principal_id` | 认证器解析的平台主体 | owner 谓词、授权、问责 | 否 |
| `session_id` | 连接建立时服务端分配 | 会话代际与审计 | 否 |
| `session_generation` | `MSG` 会话注册表 | fencing/replay | 否 |
| `agent_id` | `AGT` 已登记实例，服务端绑定 | 仅溯源和绑定一致性检查 | 可；人直接操作时为空 |
| `channel_id` | `IDN` 渠道绑定 | assurance 与吊销 | 否 |
| `assurance` | 认证方法与渠道强度 | 授权上限 | 否 |
| `environment` | 受控部署 profile | 凭据/内容策略条件 | 否 |
| `authn_ref` | 不可变认证记录 | 审计复核 | 否 |

**禁止**：从 JSON/body/query/header 直接构造；以 agent_id 回查后替换 principal；缺 principal 时回落 SystemCtx。

`DataScopeGrant` 是 IDN 针对一次 AccessCtx 解析出的短期数据范围凭据，至少含：
`grant_id, access_ctx_ref, enterprise_id, principal_id, opaque_database_key, registry_version, expires_at`。
它不含 DSN/密码；Repository 必须校验 grant 与 AccessCtx 一致、未过期且 registry version 未被吊销。
这样企业映射仍由 IDN 权威维护，而底层 Repository 无须反向调用业务域。

`SessionAuthRef`/`AccessCtxRef`/`DataScopeGrant` 均为服务端签发的 opaque 或完整性保护引用，须校验 issuer、audience、
expiry、generation 与 replay；不得置于 URL/query，不得由客户端解包后挑字段回传。

### 3.2 两条并行链

```text
授权链：Credential → Authenticator → principal → SessionAuthRef → AccessCtx
溯源链：AgentRegistry(agent_id, owner_principal) ───────────────┘ 一致性校验
```

溯源链只能拒绝不一致，不能授予授权链没有的权限。即使 agent binding 命中，授权仍以 principal、assurance、
resource 与 policy 为准。

### 3.3 系统上下文

`SystemCtx` 仅用于平台域自身、迁移、健康与受控后台任务：

- 使用独立入口、独立 DB role、独立连接池与独立审计；
- 不接受 enterprise/principal 的半空组合；
- 不可作为「暂时没有 AccessCtx」的兼容回退；
- 每个调用点必须登记 effect、owner 与为何确实无人/无企业语义。

`PendingCtx` 是迁移债，不是运行权限。其 prod/test/comment 数分别棘轮，终值为 0。

## 4. 域依赖与协作

### 4.1 允许的同步依赖

```text
ADP → IDN
APP → IDN / GOV / WRK / KNO / CAP / COL / AGT / MSG
WRK → GOV（规则快照查询）
KNO → IDN（AccessCtx 验证结果，只读）
CAP → GOV（审批结果，只读）
COL → WRK（node/gate 快照，只读）
AGT → IDN（绑定验证，只读）
MSG → IDN（SessionAuthRef 验证，只读）
各域 → PLT.Repository / PLT.Outbox / PLT.Audit
```

业务域不得同步回调上游形成环。需要反馈时发布事件，由消费方在自己的事务中更新投影视图。
APP 先向 IDN 解析 `DataScopeGrant`，再调用 Repository；Repository 不反向调用 IDN。

### 4.2 关键事件

| 事件 ID | 发布域 | 消费域 | 语义 |
|---|---|---|---|
| `EV-IDN-PrincipalBound` | IDN | MSG·AGT·GOV | principal 与渠道/会话绑定生效 |
| `EV-IDN-PrincipalRevoked` | IDN | MSG·AGT·CAP | 吊销未来访问与租约，不改历史 |
| `EV-IDN-EnterpriseDatabaseRegistered` | IDN | PLT.Repository | 更新 opaque database key 到连接池配置的只读投影；不携带明文 secret |
| `EV-WRK-NodeDispatched` | WRK | AGT·MSG·KNO·COL | 派发快照已冻结，可开始投递 |
| `EV-WRK-ExecutionFinished` | WRK | COL·GOV | 执行结果落账，等待/合并 gate |
| `EV-WRK-GateVerdictRecorded` | WRK | COL·GOV | verdict 与 evidence 已持久化 |
| `EV-CAP-DistributionRequested` | CAP | AGT·MSG | 已批准资产待下发 |
| `EV-CAP-InstallAcknowledged` | CAP | GOV·AGT | 指定 generation 已应用 |
| `EV-COL-ThreadGated` | COL | MSG·WRK | 停止自动注入与提交，保留受限读 |
| `EV-COL-DigestSealed` | COL | WRK·GOV | node 证据摘要已封存 |
| `EV-AGT-HealthChanged` | AGT | WRK·GOV | Agent 可派状态变化 |
| `EV-MSG-DeliveryAcknowledged` | MSG | 发布请求域 | 投递已 ACK，不代表业务 effect 成功 |

所有事件均带 `event_id`、`occurred_at`、`producer`、`schema_version`、`enterprise_id?`、`principal_id?`、
`correlation_id`、`causation_id`；企业无关事件必须明确 `scope=platform`，不得靠空 enterprise 猜测。

## 5. 接口通则

### 5.1 接口类别

| 类别 | 命名 | 成功语义 | 重试 |
|---|---|---|---|
| Command | `*-CMD-*` | effect 已在 owner 域事务中提交 | 仅带 idempotency key 重试 |
| Query | `*-QRY-*` | 返回请求时刻的有版本快照 | 可重试；不得产生业务 effect |
| Event | `EV-*-*` | 事实已发生；消费者至少一次收到 | 按 event_id 去重 |
| Capability | `*-CAP-*` | 授权的外部副作用执行并返回 result_ref | 按 request_id + generation 去重 |

### 5.2 所有接口共用信封

```text
InterfaceCall {
  interface_id, schema_version,
  request_id, correlation_id, causation_id,
  access_ctx_ref,
  resource_ref,
  expected_generation?, expected_state?,
  payload,
  deadline,
  issued_at
}
```

- `access_ctx_ref` 指向服务端持有的不可变上下文，不允许调用方内嵌 principal/org 覆盖它。
- command/capability 的 request_id 在 `(interface_id, owner_domain, enterprise_id)` 内唯一。
- 有代际对象必须带 expected_generation；陈旧请求返回 `FENCED`，不自动重取新代际执行旧意图。
- deadline 到期返回 `DEADLINE_EXCEEDED`；取消只表示不再等待，不推断 effect 未发生。

### 5.3 错误分类

| code | 含义 | 可否重试 |
|---|---|---|
| `UNAUTHENTICATED` | 无可信 SessionAuth/AccessCtx | 否；重新认证 |
| `FORBIDDEN` | 主体可信但无该 effect 权限 | 否 |
| `TENANT_NOT_BOUND` | principal 无企业注册或映射不唯一 | 否；走注册/选择流程 |
| `NOT_FOUND` | 在授权过滤后对象不存在 | 否；不得泄漏跨边界存在性 |
| `CONFLICT` | 状态不满足但非代际问题 | 读取新状态后由人/上层决定 |
| `FENCED` | generation/epoch 陈旧 | 否；须生成新命令 |
| `DUPLICATE` | 同键不同 payload | 否；安全告警 |
| `DEPENDENCY_UNAVAILABLE` | 必需下游不可用 | 可，受 deadline/backoff 限制 |
| `VALIDATION_FAILED` | schema/规则不满足 | 否 |
| `INTERNAL` | 未分类失败 | 仅由 owner 域策略决定 |

拒绝跨企业访问时，状态、响应体大小与可观察时延不得因目标是否存在而产生可利用差异。

## 6. 接口目录

下表的 `Auth` 指授权主体；所有 `agent` 仅出现在 Audit 列。`Tx` 指 owner 域的提交边界。

| ID | Owner | Consumer / transport | Effect 与 schema | Auth / 幂等 / 代际 | Tx / failure / audit |
|---|---|---|---|---|---|
| `IDN-CMD-AuthenticateChannel` | IDN | edge、IM adapter / internal RPC | 验证 issuer/audience/channel proof，返回 `SessionAuthRef` | credential；challenge 单次+代际 | IDN tx；失败不创建 session；记 auth attempt |
| `IDN-CMD-OpenSession` | IDN | MSG / internal RPC | `SessionAuthRef + client nonce → AccessCtxRef` | principal；request_id；session generation | IDN tx+outbox；映射缺失 fail-closed |
| `IDN-CMD-BindAgent` | IDN | AGT / internal RPC | `agent_id + AccessCtxRef + authn_ref` 建溯源绑定；owner principal 从 AccessCtx 取 | principal；request_id；binding generation | IDN tx；不能提升授权；记 bind/rebind |
| `IDN-QRY-ResolveAccess` | IDN | APP/MSG/PLT / in-process | 以 AccessCtxRef 取 immutable context | caller service identity；只读 | 快照读；过期/吊销即 UNAUTHENTICATED |
| `IDN-QRY-ResolveDataScope` | IDN | APP / in-process | principal 注册映射 → 限时 `DataScopeGrant`（含 opaque database key） | AccessCtx；不得传 raw org | 平台库只读；0/多结果均 TENANT_NOT_BOUND |
| `GOV-CMD-SubmitSpec` | GOV | UI/API | spec ref + 属性 + 输入锚，进入准入 | principal；request_id；spec version | GOV tx+outbox；硬门失败返回完整缺项 |
| `GOV-CMD-DecidePending` | GOV | typed queue UI | 对 approval/gate-fix/batch 作 typed decision | 有批准权 principal；request_id+generation | GOV tx；提案者/批准者冲突拒绝；记决策证据 |
| `GOV-QRY-ResolveGateSet` | GOV | WRK | attributes + rules version → gate requirements | service + AccessCtx；纯函数版本化 | 无写；规则缺失 fail-closed；记 rule version |
| `GOV-CMD-IssueEffectGrant` | GOV | WRK/CAP/AGT | approval_ref + effect manifest → 单次/限时 effect grant | principal + approval；request_id；grant generation | GOV tx；超范围拒绝；记签发与消费状态 |
| `WRK-CMD-CreateProject` | WRK | UI/API | project metadata → project；enterprise/owner 从 AccessCtx 取 | principal；request_id | 企业库 tx；无权创建拒绝，不接受自述 owner |
| `WRK-CMD-CreateTaskPlan` | WRK | planner/API | project + spec ref + nodes/deps → board_task | principal；request_id；plan version | WRK tx；DAG 非法拒绝；记来源信封 |
| `WRK-CMD-DispatchNode` | WRK | scheduler | node + agent assignment + context snapshot ref | scheduler service + owner epoch；request_id+generation | WRK tx+outbox；陈旧 FENCED |
| `WRK-CMD-RecordExecution` | WRK | Agent Receiver | node + outcome/result_ref/evidence | AccessCtx principal；request_id+generation | WRK tx；重复返原 outcome；agent 仅 audit |
| `WRK-CMD-RecordGateVerdict` | WRK | gate UI/Receiver | node + verdict + evidence version | reviewer principal；request_id+generation | WRK tx+outbox；执行者同 principal 则拒 |
| `WRK-CMD-AppendRevision` | WRK | executor/coordinator | failed node + revised spec/ref → new node | principal；request_id+generation | WRK tx；旧 node 不改；重连 deps |
| `WRK-CAP-DeployRelease` | WRK | approved deploy adapter | release manifest + approval + rollback ref | ops principal + approval；request_id+release generation | 外部 effect + result outbox；状态未知时 reconcile |
| `WRK-QRY-TaskSnapshot` | WRK | UI/KNO/COL | task/node/gate/release 版本快照 | AccessCtx；只读 | 企业库过滤读；跨人按 policy |
| `KNO-CMD-IngestItem` | KNO | curator/retro | item + anchors + domain_level + visibility | curator principal；request_id+item version | KNO tx；无锚拒绝；记来源 |
| `KNO-QRY-Search` | KNO | UI/MCP/context builder | query + scope + cursor → filtered hits | AccessCtx；只读 | 先过滤后检索；拒绝不伪装成功空集 |
| `KNO-CMD-BuildContextSnapshot` | KNO | WRK | dispatch fields + role → immutable snapshot ref | service + AccessCtx；request_id+task generation | KNO tx+outbox；字段缺失拒绝 |
| `KNO-CMD-MarkLifecycle` | KNO | verifier/curator | active/suspect/archived + evidence | principal；request_id+item version | KNO tx；不删除历史；记状态因果 |
| `CAP-CMD-RegisterAsset` | CAP | platform/enterprise curator | asset metadata + artifact ref + ownership | principal；request_id+asset version | CAP tx；immutable 冲突拒绝 |
| `CAP-CMD-RequestPromotion` | CAP | curator | asset version + target domain + evidence | principal；request_id+generation | CAP tx→GOV pending；不自行晋升 |
| `CAP-CMD-Distribute` | CAP | worker | approved manifest + target set + version | approval principal；request_id+distribution generation | CAP tx+outbox；目标越界拒绝 |
| `CAP-CMD-AcknowledgeInstall` | CAP | Agent sidecar | distribution_id + applied version + result | session principal；delivery_id+generation | CAP tx；迟到 ACK FENCED；记 agent audit |
| `CAP-CMD-Uninstall` | CAP | worker/sidecar | asset version + target + tombstone generation | approval/owner；request_id+generation | CAP tx+outbox；撤销后旧 ACK 不复活 |
| `COL-CMD-AppendPost` | COL | UI/Agent Receiver | immutable post + ownership snapshot + attachments | AccessCtx principal；request_id | COL tx+outbox；ts 由 DB；agent 仅 audit |
| `COL-CMD-GateThread` | COL | quota engine/coordinator | thread + quota + reason + generation | principal/service；request_id+generation | COL tx+notification outbox；冻结 cursor |
| `COL-CMD-UnlockThread` | COL | typed queue | decision + next quota + evidence | authorized principal；request_id+generation | COL tx；旧代际拒；通知实际参与方 |
| `COL-CMD-SealDigest` | COL | worker/WRK event | node + verdict + canonical sequence → digest | service；request_id+node generation | COL tx；pending 不封；FAIL evidence 先落 |
| `COL-QRY-ReadIncrement` | COL | Agent context/UI | thread + consumer + cursor → posts/digest | AccessCtx；cursor 非投递账 | 过滤读；多活需 consumer_id/lease |
| `COL-CMD-EmitNotification` | COL | domain outbox worker | taxonomy event + recipients + ref | service；event_id 幂等 | notification outbox；正文不可决定收件人 |
| `AGT-CMD-CreateRuntimeOrder` | AGT | UI/WRK | project/role/env refs → order；principal 从 AccessCtx 取 | principal；request_id+order generation | AGT tx；无绑定不启动高信任实例 |
| `AGT-CMD-ActivateInstance` | AGT | provision worker | order + resource lease + sidecar receipt | service；request_id+generation | AGT tx+outbox；部分失败进入 reconcile |
| `AGT-CMD-ReportHealth` | AGT | sidecar | instance + health + watermarks | session principal；sample id | 时序写；过期样本忽略并审计 |
| `AGT-CAP-EmergencyAction` | AGT | local sidecar UI | allowlisted effect + target + local auth | local emergency principal；request_id | 本地 append-only；判定器失败则不执行 |
| `MSG-CMD-AcceptEnvelope` | MSG | WS/FCS/sidecar | raw envelope → canonical envelope | connection SessionAuthRef；envelope_id | MSG tx/outbox；from 自述删除；伪造拒绝 |
| `MSG-CMD-RouteDelivery` | MSG | APP/domain outbox | canonical envelope + target selector | service + AccessCtx；delivery_id | MSG tx；selector 先验；不可读正文授权 |
| `MSG-CMD-AcknowledgeDelivery` | MSG | recipient | delivery_id + generation + receipt | session principal；delivery_id | MSG tx；迟到 ACK FENCED；不等于业务成功 |
| `MSG-QRY-OnlineSessions` | MSG | scheduler/UI | enterprise/project scoped online snapshot | AccessCtx；只读 | 元数据过滤；不返回他企业存在性 |
| `PLT-CMD-RunInAccessTx` | PLT | 全部企业业务域 / in-process port | AccessCtxRef + DataScopeGrant + domain operation → committed result | service identity + principal；request_id；grant generation | 企业库 tx；第一条 SET LOCAL；同事务 outbox/audit；失败 rollback |
| `PLT-CMD-RunMigration` | PLT | migration runner / in-process port | migration grant + enterprise + artifact digest + from/to version | migration service + approval；request_id+schema generation | 独立迁移 tx；失败停止后续企业；留恢复点 |
| `PLT-QRY-RLSConformance` | PLT | SP7a/ops verifier | enterprise/schema → role/owner/policy/FORCE/WITH CHECK 核验结果 | verifier service；只读；`N/A(NA-READONLY)` | 只读 catalog tx；取不到对象 INCONCLUSIVE，不报全绿 |
| `PLT-CMD-AppendAudit` | PLT | 全域 / in-process or outbox | canonical AuditRecord → append receipt | service identity；audit_id 幂等；`N/A(NA-IMMUTABLE)` | 企业/平台审计 tx；只追加；sink 失败进入有界重试 |
| `PLT-QRY-HealthSnapshot` | PLT | GOV/AGT/ops UI | component + dependency refs → live/ready/degraded/safe_mode | authorized ops/service；只读；`N/A(NA-READONLY)` | 不读业务行；依赖未知明确返回 unknown |
| `PLT-QRY-MetricsWindow` | PLT | GOV/ops UI | metric ID + scoped window → bounded aggregates | authorized ops/service；只读；`N/A(NA-READONLY)` | 脱敏聚合；不返回跨企业正文/高基数字段 |
| `PLT-CMD-ReconcileEffect` | PLT | JOB/各 capability owner | request/result refs + observed state → reconciled outcome | owner service；request_id+effect generation | reconcile ledger tx；状态仍未知则保持 unknown，不猜成功 |

<!-- INTERFACE-FAMILY-COVERAGE-BEGIN -->
| 族 | 稳定接口 ID |
|---|---|
| `IF-01` | `IDN-CMD-AuthenticateChannel` · `IDN-CMD-OpenSession` · `MSG-CMD-AcceptEnvelope` |
| `IF-02` | `IDN-QRY-ResolveAccess` · `IDN-QRY-ResolveDataScope` · `PLT-CMD-RunInAccessTx` |
| `IF-03` | `WRK-CMD-CreateTaskPlan` · `WRK-CMD-DispatchNode` · `WRK-CMD-RecordExecution` · `WRK-QRY-TaskSnapshot` |
| `IF-04` | `GOV-CMD-DecidePending` · `GOV-CMD-IssueEffectGrant` · `WRK-CMD-RecordGateVerdict` · `WRK-CAP-DeployRelease` |
| `IF-05` | `PLT-CMD-RunInAccessTx` · `PLT-CMD-RunMigration` · `PLT-QRY-RLSConformance` |
| `IF-06` | `KNO-CMD-IngestItem` · `KNO-QRY-Search` · `KNO-CMD-BuildContextSnapshot` |
| `IF-07` | `CAP-CMD-RegisterAsset` · `CAP-CMD-Distribute` · `CAP-CMD-AcknowledgeInstall` · `CAP-CMD-Uninstall` |
| `IF-08` | `COL-CMD-AppendPost` · `COL-CMD-EmitNotification` · `COL-QRY-ReadIncrement` · `COL-CMD-SealDigest` |
| `IF-09` | `AGT-CMD-CreateRuntimeOrder` · `AGT-CMD-ActivateInstance` · `AGT-CAP-EmergencyAction` |
| `IF-10` | `PLT-CMD-AppendAudit` · `PLT-QRY-HealthSnapshot` · `PLT-QRY-MetricsWindow` · `PLT-CMD-ReconcileEffect` |
<!-- INTERFACE-FAMILY-COVERAGE-END -->

### 6.1 接口兼容策略

- schema 只做向后兼容增量；删除/改义须新 major version。
- 生产者在消费者支持新版本前不得切默认；未知字段由消费者忽略，未知 enum 必须拒绝或显式 `unknown`，不得映射默认权限。
- 双读/双写仅用于迁移窗口，须有截止时间、差异计数与单一权威方向。
- 每项接口在代码中只有一个 adapter 入口；HTTP、WS、IM 适配器共用同一 application handler。

## 7. 关键流程

### 7.1 可信主体到领域事务

```text
1 Edge 验 credential，剥除 principal/org/agent 自述头
2 IDN AuthenticateChannel → SessionAuthRef
3 MSG Open connection，以 SessionAuthRef 固化 principal + generation
4 IDN ResolveAccess → AccessCtxRef
5 APP 调 IDN ResolveDataScope，取得限时 DataScopeGrant
6 PLT Repository 验 grant，以 opaque DB key 查本地受控池投影
7 BEGIN enterprise transaction
8 SET LOCAL dw.principal = AccessCtx.principal（事务第一条 DB 语句）
9 执行领域 SQL；owner_principal 主谓词 + RLS 兜底
10 写 domain event/outbox/audit，同事务 commit
11 清理连接；SET LOCAL 随事务失效
```

任何一步缺失均不进入下一步。不存在「先查后补 AccessCtx」或「先用默认库再换库」。

### 7.2 Agent 绑定与派发

```text
IDN principal ──creates──> AGT runtime order
AGT instance ready ──bind request──> IDN agent provenance binding
IDN emits PrincipalBound(agent_id, principal_id, generation)
WRK dispatches node with principal-owned assignment + snapshot_ref
MSG delivers canonical envelope with AccessCtxRef
Receiver checks: session principal == assignment principal == binding principal
```

三者不一致即拒绝并落安全审计。该检查只做完整性约束，不赋予 agent 新权限。

### 7.3 node 执行、门与封存

```text
dispatch → execution result → gate verdict
                           ├─ pass/not_required → downstream eligible
                           └─ fail → downstream blocked; append_revision creates new node
execution done + verdict != pending
    → persist complete evidence
    → COL SealDigest
    → node evidence immutable
```

gate verdict 与 execution status 分列；不得新增 `passed` node status。

### 7.4 通知与投递

领域事务只写通知意图 outbox，不在事务内调用外部渠道：

```text
domain tx: state + notification_outbox(enqueued)
worker: claim lease → MSG RouteDelivery → channel send
receipt: delivered/acked 或 retry schedule
consumer: delivery_id/event_id dedupe
```

ACK 证明收件端接收，不证明业务 action 已执行；业务结果由独立 command/result 记录。

### 7.5 skill/MCP 分发与撤销

```text
catalog version → GOV approval → distribution manifest(generation)
→ outbox → sidecar apply → ACK(applied version)
→ CAP install state

revoke/uninstall increments generation and emits tombstone
late ACK(old generation) => FENCED
```

## 8. 数据架构

### 8.1 数据拓扑

```text
PLATFORM_DB
  enterprise_registry, principal_directory, channel_binding,
  platform_policy, platform_asset, global_audit_ref, schema_registry

ENTERPRISE_DB_<enterprise>
  project/task/node/gate/release
  knowledge/context
  asset_install
  post/notification/digest
  agent/runtime metadata
  domain_outbox/domain_audit

OBJECT_STORAGE
  enterprise-scoped attachment/artifact blobs（DB 只存 ref + sha256）
```

平台库不得存企业业务正文。企业库不得存可修改的平台根 policy。跨库不做分布式事务；平台事件与企业事件以 outbox 协调。

#### 8.1.1 数据驻留矩阵

| 数据 | 驻留 | 平台面可见内容 | 禁止 |
|---|---|---|---|
| enterprise registry / database key | 平台库 | 企业稳定 ID、状态、opaque DB key、配置锚 | 企业业务正文、可直接展示的 DSN/secret |
| principal/channel/session binding | 平台库 | 身份与吊销 metadata、认证证据 ref | 渠道 secret 原值、企业内容 |
| platform policy / D0 catalog | 平台库 | 平台公共基线、批准记录 | 企业私有派生内容 |
| enterprise spec/project/task/node | 对应企业库 | 平台只持交付/健康聚合 ref | 把正文镜像到平台库 |
| knowledge/post/attachment metadata | 对应企业库 | 平台只见有界状态与不含正文的水位 | 跨企业全文索引 |
| envelope/delivery payload | 对应企业库或企业作用域对象存储 | MSG 平台面只持 encrypted/scoped payload_ref 与投递 metadata | 在平台 session 表持久化业务正文 |
| agent runtime inventory | 平台库 + 企业投影 | 平台库持实例/健康/租约；企业库持项目/任务指派 | 把企业上下文灌入全局 inventory |
| audit | 企业审计流 + 平台安全审计流 | 平台只收最小安全事件或经批准聚合 | 企业 ops 可改写自己的特权审计 |

跨企业运营视图只读预先定义的最小聚合，不从多个企业库临时拼正文。任何新聚合须列字段、用途、刷新窗口、
访问者与反推风险，并经治理域批准。

### 8.2 受控仓储接口

```text
Repository.WithAccess(ctx, AccessCtxRef, DataScopeGrant, func(Tx) error)
Repository.WithSystem(ctx, SystemWorkRef, func(SystemTx) error)
Repository.WithMigration(ctx, MigrationGrantRef, func(MigrationTx) error)
```

`WithAccess` 内部完成：AccessCtx/grant 一致性与时效校验、opaque DB key 的受控池选择、BEGIN、第一条 `SET LOCAL`、领域回调、
outbox/audit 同事务、commit/rollback。包外不持有 pool/DB/Tx 原始句柄。

### 8.3 数据所有权与 RLS

- 企业边界：一企业一库；错误 DSN 是整企业泄漏，故 DB key 只由 Repository 解析。
- 人边界：所有人层领域表有 `owner_principal`，应用查询显式谓词为主守，RLS 为漏谓词兜底。
- RLS：`USING` 与 `WITH CHECK` 均按 `current_setting('dw.principal', true)`；缺值读 0 行、写拒绝。
- 应用角色：非 superuser、`NOBYPASSRLS`、非表 owner、无 DDL。
- 企业 ops/平台 ops 特权面按 SpecDoc 现行 C.3a.1c；所有可绕 RLS 的动作外发不可篡改审计。
- 备份、只读副本、复制槽不受 RLS 保护，使用介质加密、审批与独立审计。

### 8.4 schema 与迁移

| 层 | owner | 机制 |
|---|---|---|
| 平台机制 | SP7a | registry、migration runner、schema version、连接/事务封装 |
| 领域 schema | 各领域/SP7b | 领域表、policy、回填、双读验证、cutover |
| 跨企业 rollout | migration orchestrator | 小批 → 验证 → 扩批；任一企业失败停止后续 |

迁移记录包含 enterprise、from/to version、artifact digest、approval、started/finished、result、rollbackability。
不可逆迁移须先备份与恢复演练；「脚本成功」不等于数据语义正确，另做领域一致性查询。

### 8.5 时间与不可变证据

- 业务 `ts` 由对应企业 DB 自动写 UTC；客户端时间只作 metadata，不入排序权威。
- 跨平台库与企业库的顺序使用 event_id/causation，不假设两库时钟给全序。
- post/digest canonical sequence 含 enterprise、principal、agent provenance、归属快照、seq、server ts、content/attachment digest。
- 外部锚 manifest 含 count、high-water cursor、前一 root 与权限独立的提交位置；裸 root 不防 omission。

## 9. 运行与部署架构

### 9.1 运行单元

| ID | 运行单元 | 承载 | 独立原因 |
|---|---|---|---|
| `EDGE-01` | edge gateway | TLS、认证材料接入、头清洗、限流、常规/紧急路由分离 | 信任边界 |
| `CTRL-01` | control plane | IDN/GOV/WRK/KNO/CAP/COL/AGT application + domain | 共享事务与一致部署 |
| `MSG-01` | session router | WS session、envelope、routing、delivery/ACK | 长连接与独立扩缩容 |
| `FCS-01` | channel service | IM gateway/receiver、渠道去重与 canonical adapter | 外部 SDK 故障隔离 |
| `JOB-01` | async worker | outbox、摘要、digest、通知、报告、reconcile | 后台负载与重试 |
| `SIDE-01` | agent sidecar | 本地 Agent 管理、provision、health、紧急面 | 平台故障时仍可用 |
| `DATA-01` | repository library/service seam | DB registry、pool、transaction、migration | 数据边界单点；初期库内模块，不急拆网络服务 |

`DATA-01` 初期是同进程库与受控 port，不是独立网络服务；只有连接池需独立扩缩容、语言边界或审计隔离出现实证时才拆。
拆出后接口语义不变，且不得把裸 SQL/Tx 暴露为远程「万能查询」。

### 9.2 部署视图

```text
Internet / Enterprise IM
          │
     [EDGE-01]
      │      └────────────── emergency fixed route ──> [SIDE-01 local]
      ├─> [CTRL-01 x N]
      ├─> [MSG-01 x N] ──> [SIDE-01 / Agent]
      └─> [FCS-01 x N]  ──> IM providers

[CTRL-01/JOB-01/MSG-01]
      ├─> PLATFORM_DB
      ├─> ENTERPRISE_DB pool registry → DB per enterprise
      ├─> OBJECT_STORAGE
      ├─> Git/CI
      └─> LLM/MCP external systems
```

部署件声明 environment profile 与 service identity。常规应用、迁移、平台审计、备份使用不同凭据与连接池。

### 9.3 扩缩容与线性化点

- CTRL 无本地业务状态，可横向扩；command 线性化点在企业 DB 的 generation/CAS 行。
- MSG 以 session_id 一致性哈希/租约持有连接；同 session generation 只允许一个 active owner。
- JOB 以 outbox row lease + fencing token 竞争消费；lease 过期不允许旧 worker commit ACK。
- FCS 渠道事件以 provider event id + channel id 去重。
- SIDE 每 Agent instance generation 单活；替换实例使旧 token、旧 ACK、旧 provision 全部 fenced。

### 9.4 降级

| 故障 | 允许 | 禁止 |
|---|---|---|
| IDN 不可用 | 已签发且未吊销的短期 session 继续至到期；只读静态帮助 | 新会话、主体切换、权限提升 |
| 企业 DB 不可用 | 排队有界 outbox 意图、显示明确不可用 | 切到平台库/其他企业库 |
| MSG 不可用 | 领域事务继续落 outbox | 把「已入队」报成「已送达」 |
| KNO/LLM 不可用 | 不带摘要执行显式任务、回退原帖读取 | 伪造摘要或跳过必需上下文 |
| GOV 不可用 | 已批准且未过期 token 可在范围内消费 | 默认批准、降低门数 |
| 平台整体不可用 | SIDE 紧急 allowlist + 本地审计 | 全权管理、读企业正文、签门 |

## 10. 安全架构

### 10.1 信任边界

1. 外部渠道/浏览器到 EDGE：不可信输入。
2. EDGE 到 SessionAuthRef：认证边界。
3. 平台库到企业库：强数据边界。
4. principal 到 owner rows：人层策略边界。
5. 控制平面到 sidecar：远程执行边界。
6. 平台正常面到紧急面：降级但不扩权边界。
7. 数据库到备份/副本/对象存储：介质边界。

### 10.2 PEP/PDP 分工

| 位置 | PEP | PDP/权威 |
|---|---|---|
| EDGE | 清洗身份头、验证 token、路由隔离 | IDN session policy |
| APP handler | interface/effect、resource、approval 校验 | GOV policy + IDN AccessCtx |
| Repository | 企业选库、owner predicate、事务上下文 | IDN registry + domain query policy |
| PG | RLS/role/DDL boundary | migration policy |
| MSG | target selector、recipient、generation | MSG route + domain-produced recipient set |
| SIDE | capability allowlist、target、local auth | signed manifest + local emergency policy |

任何一层通过不替代下一层；RLS 不替代应用谓词，approval 不替代 capability allowlist。

### 10.3 审计

统一 AuditRecord：`audit_id, enterprise?, principal?, agent?, session, interface_id, effect, resource,
decision, reason_code, approval_ref?, request_id, generation, occurred_at, result_ref, redaction_version`。

- agent 字段只用于溯源和一致性检查。
- 安全拒绝落最小 metadata，不落 secret/正文。
- 企业 ops 特权审计外发到其无写权位置。
- 紧急面本地 append-only；平台恢复后补账完成前该 ops-Agent 不领新任务。

### 10.4 内容与附件

- 入站、帖子、摘要、审计、日志、通知统一 redaction/secret detector 规则版本。
- S0 拒绝进入文档/帖子/日志/附件；判定器失败则拒绝该 effect。
- blob 存对象存储，接口只传 scoped ref + sha256 + classification；下载再鉴权，不信 URL 难猜。
- LLM 输入是非可信数据；prompt/schema 与帖子正文隔离，模型不能获得额外 capability。

## 11. 可靠性与可观测

### 11.1 可靠性模式

| 问题 | 模式 | 验收出口 |
|---|---|---|
| 重复 command | request_id + payload digest + prior outcome | 同键同 payload 返原 outcome；异 payload 告警 |
| 并发变更 | expected generation/state + CAS | 一胜其余 FENCED/CONFLICT |
| DB 与消息 | transactional outbox | commit 后最终投递，不双写 |
| 至少一次事件 | event_id inbox/dedupe | 重放无重复 effect |
| worker 死亡 | lease + fencing token | 旧 worker 不可提交 |
| 部分外部 effect | result_ref + reconcile state | unknown 不报成功，定期对账 |
| 长连接换代 | session generation | 旧连接、旧 ACK、旧 token 失效 |
| 证据封存 | canonical digest + external anchor | 改动可检测；omission 由 count/high-water 链发现 |

### 11.2 SLI 与日志关联

所有请求贯通 `request_id/correlation_id/causation_id/enterprise_hash/interface_id`，不把 principal 明文放普通指标标签。

最低 SLI：认证拒绝率、AccessCtx 解析失败、错库防护拒绝、接口 latency/error、CAS/FENCED、outbox lag、
delivery retry/ACK、gate pending age、context build failure、thread gate、digest backlog、Agent health、紧急补账债务。

### 11.3 健康语义

- `live`：进程循环可运行；
- `ready`：必需依赖、schema 与 policy version 匹配，可接新请求；
- `degraded`：明确列禁用 capability，不等同 ready；
- `safe_mode`：只允许只读/收紧 effect；
- 健康检查不得使用 SystemCtx 读取业务行证明「数据库正常」。

## 12. 现状包到目标架构映射

本节只描述冻结锚现状的迁移归处，不让包名反向定义域。

### 12.1 顶层 `internal/` 包

| 现有包 | 目标归处 | 状态 | 说明 |
|---|---|---|---|
| `admin` | ADP + GOV/AGT/WRK application | `state:rewrite` | 拆分角色、页面、配置与业务 handler |
| `bridge` | ADP legacy | `state:retire` | mock unlock 不进入目标常规身份链 |
| `bus` | PLT.EventBus | `state:adapt` | 只传 domain event，不承载业务 owner |
| `clock` | PLT.Clock | `state:retain` | 统一可注入时钟，DB ts 仍由 DB 给 |
| `config` | PLT.Config | `state:adapt` | 环境 profile/secret ref 分开，禁止客户端自述 |
| `coordination` | WRK application | `state:adapt` | 协调 use case，不直接改其他域表 |
| `datad` | GOV/WRK/KNO/CAP read models + adapters | `state:rewrite` | 按目标域拆 repository/application |
| `evidence` | GOV/Audit adapter | `state:adapt` | 证据引用，不成为第二事实源 |
| `fanout` | MSG channel delivery | `state:adapt` | spool/outbox 归 MSG，业务 recipient 由上游给 |
| `fcs` | MSG channel adapter | `state:adapt` | 保留 canonical channel 语义，收紧边界 |
| `feishu` | ADP IM | `state:adapt` | 只做 provider protocol |
| `jcs` | PLT canonical serialization | `state:retain` | digest/event canonicalization |
| `llm` | KNO/COL external adapter | `state:adapt` | 无权限提升，输入隔离 |
| `m5` | AGT provision adapter | `state:adapt` | 资产下发由 CAP manifest 驱动 |
| `m8` | MSG session router + ADP terminal | `state:rewrite` | 路由与管理/UI 分开；身份从 SessionAuthRef 取 |
| `mcs` | WRK runtime family | `state:adapt` | 子包见 12.2，保留经验证语义 |
| `model` | 各业务域 | `state:rewrite` | 删除全局共享业务模型，保留跨域 DTO 于接口包 |
| `portal` | ADP UI | `state:adapt` | 只调 application interfaces |
| `redact` | PLT Security | `state:adapt` | 规则版本统一用于多 sink |
| `reminder` | COL notification application | `state:adapt` | taxonomy/outbox 驱动 |
| `repo` | PLT Repository | `state:adapt` | 演进为 AccessCtx 与企业库 registry |
| `router` | MSG routing | `state:adapt` | 只读可信头，不读正文授权 |
| `schedule` | WRK scheduling | `state:adapt` | 计划属于 WRK |
| `scheduler` | WRK scheduling application | `state:adapt` | 通过 WRK command，不裸改状态 |
| `secretbox` | PLT Secret | `state:adapt` | 保留加密能力，禁止输出 secret |
| `store` | legacy persistence | `state:rewrite` | 迁入 repo + 各域 repository；迁移 runner 可复用 |
| `version` | PLT BuildInfo | `state:retain` | 不作业务 schema version |
| `worker` | JOB application | `state:adapt` | 消费 outbox，按域 handler 分派 |

### 12.2 `internal/mcs/` 子包

| 包 | 目标归处 | 状态 | 说明 |
|---|---|---|---|
| `board` | WRK domain/repository | `state:rewrite` | 重写 storage/state machine，以 conformance suite 守语义 |
| `dea` | WRK/GOV rule adapter | `state:adapt` | 决策表版本来自 GOV |
| `demo` | demo only | `state:retire` | 不作目标能力 |
| `dispatch` | WRK→MSG adapter | `state:adapt` | 派发先落 WRK outbox |
| `hubdriver` | WRK→MSG adapter | `state:adapt` | 不拥有 session principal |
| `mcsd` | WRK application/runtime | `state:adapt` | 保留调度语义，命令改走 WRK 接口 |
| `pool` | AGT assignment read model | `state:adapt` | 与 AGT instance/lease 对账 |
| `rotation` | WRK/AGT application | `state:adapt` | 代际/fencing 明确 |
| `route` | WRK routing policy | `state:adapt` | 与 MSG 传输路由分名分责 |

### 12.3 `cmd/` 进程

| 现有进程 | 目标运行单元 | 状态 |
|---|---|---|
| `workpulse` | `CTRL-01` composition root | `state:rewrite`（重写 wiring） |
| `dingwei-datad` | 并回 CTRL/JOB 或保留只读投影进程 | `state:rewrite`（拆分后不保留「大数据域」） |
| `fcs` | `FCS-01` | `state:adapt`（保留协议语义） |
| `feishu-fanout` | `FCS-01/MSG-01` worker | `state:adapt`（合并重复运行职责） |
| `mcsd` | `CTRL-01` WRK runtime 或独立 WRK worker | `state:adapt`（保留调度语义） |
| `mcs-demo` | demo/test | `state:retire`（不部署生产） |

### 12.4 映射完备性

完备性分母不取「顶层目录 + 子目录」的手写和（`internal/mcs` 只是容器目录，直接相加会重复/串单位）。
唯一口径为冻结锚中**含至少一个非测试 Go 文件的唯一目录**；由 `check.sh` 从 Git 输入派生预期集合，
与下方实检集合双向比较并输出 UNKNOWN/EXTRA。

<!-- COVERAGE-IMPLEMENTATION-BEGIN -->
cmd/dingwei-datad
cmd/fcs
cmd/feishu-fanout
cmd/mcs-demo
cmd/mcsd
cmd/workpulse
internal/admin
internal/bridge
internal/bus
internal/clock
internal/config
internal/coordination
internal/datad
internal/evidence
internal/fanout
internal/fcs
internal/feishu
internal/jcs
internal/llm
internal/m5
internal/m8
internal/mcs/board
internal/mcs/dea
internal/mcs/demo
internal/mcs/dispatch
internal/mcs/hubdriver
internal/mcs/mcsd
internal/mcs/pool
internal/mcs/rotation
internal/mcs/route
internal/model
internal/portal
internal/redact
internal/reminder
internal/repo
internal/router
internal/schedule
internal/scheduler
internal/secretbox
internal/store
internal/version
internal/worker
<!-- COVERAGE-IMPLEMENTATION-END -->

### 12.5 目标态新增单元

| 新单元 | 状态 | 责任 | 为什么不能只适配既有件 |
|---|---|---|---|
| IDN domain module | `state:new` | enterprise/principal/channel/session/agent binding | 现有身份散在 m8/admin/datad，无唯一 owner |
| GOV domain module | `state:new` | spec/rule/approval/typed pending | 现有管理 handler 与任务状态混杂 |
| `AccessCtx` / `DataScopeGrant` | `state:adapt` | 双层访问上下文与限时数据范围 | AccessCtx 双轴 port 已落；registry/RLS 实现待 SP7a |
| domain interface packages | `state:new` | 接口 schema 与 ports | 现有全局 model 不能表达 owner 边界 |
| enterprise registry projection for Repository | `state:new` | opaque DB key 到受控池映射 | 现有 repo 无企业注册投影 |
| unified domain outbox/inbox | `state:new` | 跨域事实与幂等消费 | 现有 outbox 形态未覆盖全部域 |

## 13. 原型入口到架构能力

| 原型入口 | 主责域 | 主要接口 | 必须显示的失败/空态 |
|---|---|---|---|
| 待决队列 `s1` | GOV | `GOV-QRY/CMD-DecidePending` | 无批准权、过期、代际冲突、空队列 |
| 任务看板 `s2` | WRK | task snapshot / dispatch / revision | starved、无人愿接、gate pending/fail |
| 平台治理 `s8/s8r/s8h` | GOV | batch/policy/health queries | 缺第二签、闸债务、依赖不可用 |
| Spec/准入/交付链 `s4/a5/a6` | GOV/WRK | submit spec / artifact chain | 缺面、锚悬空、门未过 |
| 知识库 `s3` | KNO | search/ingest/lifecycle | 过滤拒绝、suspect、无结果 |
| 分级对齐 `b1` | GOV | rule set query | 规则版本缺失/冲突 |
| 周报 `b2` | COL/WRK | weekly report query/export | 数据截止时刻、部分项目缺数 |
| 仓储核验 `b3` | PLT/IDN | conformance/negative evidence | 未达标、负测缺失、旁路命中 |
| skill/MCP `s5` | CAP | catalog/promote/distribute | 无可见性、待批、迟到 ACK |
| Agent 管理/列表/工单 `s6/a1/a4` | AGT/IDN | runtime order/activate/health | 无 principal 绑定、资源不足、环境不可信 |
| Agent 工作面 `a2` | WRK/AGT/KNO | assignment/context snapshot | 信封缺字段、已有 in_progress |
| 帖子流 `s7` | COL | append/read/gate/seal | gated、quota exhausted、digest pending |
| 常规对话 `a8/a3` | MSG/COL | envelope/post | session fenced、内容拒收 |
| ops 紧急对话 `a9` | AGT | emergency capability | 本地认证失败、effect 不在 allowlist |
| 渠道授权/绑定 `s9/a7` | IDN | authenticate/bind/revoke | 弱渠道、challenge 过期、rebind 冲突 |
| 架构总览 `s0` | GOV/本件 | artifact query | 当前设计未过门 |

UI 只展示后端判定结果，不在浏览器重算权限、门集、visibility 或归属链。

<!-- COVERAGE-PROTOTYPE-BEGIN -->
s1
s2
s8
s8r
s8h
s4
a5
a6
s3
b1
b2
b3
s5
s6
s7
a8
a9
a1
a2
a4
a3
s9
a7
s0
<!-- COVERAGE-PROTOTYPE-END -->

## 14. 迁移路线

### 14.1 阶段

| 阶段 | 目标 | 出口 |
|---|---|---|
| M0 边界钉死 | 新接口/域包骨架、禁止新增旧名与裸 DB、建立 trace | lint/棘轮无回升 |
| M1 身份地基 | agent→principal、SessionAuthRef、AccessCtx、企业 registry | 伪造/缺失/多映射负测全过 |
| M2 仓储基座 | Repository WithAccess、企业池、RLS transaction wrapper | SP7a 边界与 PG fixture |
| M3 工作域 | board_task/node/gate 独立模型，mcsd 经接口 | conformance suite + replay/cas |
| M4 知识/能力/协同 | KNO/CAP/COL schema 与 outbox | 各 SP 用例评审/交付验证 |
| M5 运行与接入 | Agent/runtime、MSG/FCS、UI adapter | E2E、故障/降级/紧急面演练 |
| M6 cutover | 企业分批迁移、旧存储只读、回滚窗口 | 对账恒零、观察期门、签收 |

### 14.2 双轨规则

- 双轨期间单一写权威必须明确；禁止双向同步。
- 旧读与新读做影子比对，差异带 object ID、规则/schema version，不记录敏感正文。
- 每批企业独立 high-water 与 rollback decision；一个企业成功不代表其余成功。
- 旧接口适配器在所有消费者迁完后 tombstone；迟到调用明确失败，不静默转发无限期兼容。

### 14.3 不可逆点

以下动作前必须独立门：删除旧列/表、停止旧 outbox、切换企业 DSN、重建 board CHECK/状态机、
重算 digest、吊销旧 Agent generation、改变 interface major version。

## 15. Worked examples

### 15.1 伪造 principal

调用方在 WS envelope 写 `principal=bob`，连接 SessionAuthRef 属 alice。EDGE 删除自述字段，MSG 写 canonical alice；
Receiver 比对 assignment/binding 后接受 alice 或因不一致拒绝。系统从不因 bob 字段查 bob 权限。

### 15.2 错企业库

alice 的 AccessCtx 属 enterprise A，请求 resource 声称 enterprise B。Repository 不读取该字段，只以 IDN registry 的 opaque key
选 A 库；resource ref 在 A 库不存在返回 NOT_FOUND，B 库 query count 必须为 0。

### 15.3 冒用 owner 写入

alice 事务尝试写 `owner_principal=bob`。应用谓词先拒；即使漏掉，RLS `WITH CHECK` 再拒。拒绝审计不包含正文。

### 15.4 重复 command

同 request_id、同 payload 重放 `RecordExecution`，返回首次 outcome 且不重复事件；同 key 不同 payload 返回 DUPLICATE 并安全告警。

### 15.5 迟到 gate result

node generation 7 已因 revision 升为 8，generation 7 的 GateResult 到达：返回 FENCED，不更新 verdict、不触发下游、不重取 generation 8 自动套用。

### 15.6 跨企业查询

企业 A principal 请求企业 B project。IDN/Repository 在选库前拒绝或只查 A 库；B 对象存在与否对状态、体积、时延不可区分；拒绝落最小审计。

### 15.7 紧急面越权

sidecar 紧急用户请求「安装新 skill」。effect 不在收紧 allowlist，sidecar 本地拒绝，即便平台/GOV 不可用也不降级放行；失败写本地 append-only，恢复后补账。

## 16. 未实测与待验证

| ID | 项 | 当前结论边界 | 验证 owner/门 |
|---|---|---|---|
| `U-01` | 目标规模与峰值 | 未给足数字，不据此拆微服务 | 各 SP 预计数据/性能门 |
| `U-02` | PG 版本与全部 RLS fixture | 现有日志覆盖有限版本/用例，不外推全闭 | ops + 安全交付验证 |
| `U-03` | board PG 并发语义 | 需 conformance suite，不以 SQLite 行为直接外推 | SP2/SP7a |
| `U-04` | 工蜂保护分支/CODEOWNERS | Git 内不可证明平台配置已生效 | 业主+ops |
| `U-05` | 多活 consumer 形态 | 数据模型预留 consumer_id，默认单活 | SP5 压测/故障测试 |
| `U-06` | 外部 digest anchor 权限独立性 | 方案可行但须确认 DB/Git 管理员是否独立 | ops/安全门 |
| `U-07` | 紧急 sidecar 离线全链 | 设计已定，未做断网演练 | SP6 交付验证 |

## 17. SpecDoc 责任追踪

「主责」每行只列一个 owner；其他域只能作为协作者或机制提供者，不能借「共同负责」形成双 owner。

| SpecDoc | 主责域/层 | 协作者 | 本件落点 |
|---|---|---|---|
| §1–§4、附录 A | GOV | 全域 | 0、1、2、5 |
| §5 分级与门集 | GOV | WRK | 2.2.2、5、6 |
| §6.1 判定/回执 | GOV | WRK | 5、11 |
| §6.2 状态机变更 | WRK | GOV | 2.2.3、7.3 |
| §7 门禁纪律 | GOV | WRK | 2.2.2、6 |
| §8 发版 | WRK | GOV | 2.2.3、6、7.3 |
| §9 复盘 | WRK | COL·KNO | 2.2.3、6 |
| §10–§13 | KNO | IDN·PLT | 2.2.4、6、8 |
| §14 判据/MCP | CAP | KNO·GOV | 2.2.5、6 |
| §15–§16 分发/生命周期 | CAP | GOV·AGT·MSG | 2.2.5、7.5 |
| §17–§18 帖子流 | COL | WRK·MSG | 2.2.6、7.3、8.5 |
| §19 通知 | COL | MSG | 2.2.6、7.4 |
| §20 摘要/周报 | COL | WRK·KNO | 2.2.6、13 |
| §21 身份 | IDN | AGT·MSG | 2.2.1、3、7.1/7.2 |
| §22.0–§22.1 隔离语义 | IDN | PLT | 3、7.1、10 |
| §22.2 存储隔离机制 | PLT | IDN·各业务域 | 8、10 |
| §22.3–§22.4 ops/凭据 | IDN | GOV·AGT | 3、10 |
| §22.5 内容识别 | PLT.Security | COL·KNO·MSG | 10.4 |
| §23–§24 授权/平台治理 | GOV | IDN·各 effect owner | 2.2.2、6、10.2 |
| §25.1 待决入口 | GOV | 全域 | 13 |
| §25.2 知识入口 | KNO | IDN | 13 |
| §25.3 交付链 | GOV | WRK | 13 |
| §25.4/§25.8 交互入口 | MSG | AGT·COL | 9、13 |
| §25.5–§25.6 Agent 面 | AGT | WRK·KNO | 13 |
| §25.7 多模态 | COL | PLT.Security | 10.4、13 |
| §25.9 项目维度 | WRK | 全企业域 | 13 |
| §26 Agent 管理 | AGT | IDN·CAP | 2.2.7、6、9 |
| §27 渠道接入 | MSG | IDN | 2.2.8、6、9 |
| §28–§29 工作纪律/自检 | GOV | PLT·门方 | 11、16、18 |
| 附录 B 新建清单 | GOV | 各 owner SP | 14 |
| B2.1 项目产品线台账 | WRK | GOV | 6、14 |
| B2.2 团队/产品线映射 | WRK | IDN·GOV | 6、14 |
| B2.3 导出落账 | COL | GOV | 6、8、14 |
| C.1 硬前置 | GOV | 各 owner | 16 |
| C.2 外部确认 | GOV | 各 owner | 16 |
| C.3a 企业库/RLS | PLT | IDN·各领域 SP7b | 8、14 |
| 附录 D 沿革 | GOV | —— | 0 |
| E.0–E.5 探测映射 | GOV | PLT·门方·各 SP | 11、18 |
| F.0 现行裁定索引 | GOV | 全域 | 本表各落点；撤销项不作目标输入 |
| F.1 Agent 承接 | AGT | IDN·WRK | 2.2.7、7.2 |
| F.2 落点记录 | GOV | 各 owner | 17 |
| F.3 协同依据/帖子归属 | COL | WRK·GOV | 2.2.6、8.5 |
| F.4 接口承载/冗余存 | COL | GOV·本件 | 6、8.5 |
| F.5 业务线/团队 | WRK | IDN | 2.2.3、17 |
| F.6 产品线属性 | WRK | COL | 2.2.3、8.5 |
| F.7 版本/聚合/时间 | WRK | COL·GOV | 7.3、8.5 |
| F.8 帖子 Agent 溯源 | COL | IDN·AGT | 2.2.6、3.2 |
| F.9 版本收口/帖子不可编辑 | WRK | COL | 7.3、8.5 |
| F.10 密级/接口合并 | GOV | COL·本件 | 0、10.4 |
| F.11 输入来源 | GOV | 本件 | 0、17 |
| F.12 默认设计约束 | GOV | 全域 | 0、18 |
| F.13 原型规范对接 | GOV | ADP·各域 | 13 |
| F.14 周报导出 | COL | WRK | 2.2.6、13 |
| F.15 skill/MCP 归属鉴权 | CAP | IDN·GOV | 2.2.5、6 |

<!-- COVERAGE-SPECDOC-BEGIN -->
§0
§1
§2
§3
§4
§4.6
§4.7
§5
§6
§7
§8
§9
§10
§11
§12
§13
§14
§15
§16
§17
§18
§19
§20
§21
§22
§23
§24
§25
§26
§27
§28
§29
B2.1
B2.2
B2.3
C.1
C.2
C.3a
E.0
E.1
E.2
E.3
E.4
E.5
F.0
F.1
F.2
F.3
F.4
F.5
F.6
F.7
F.8
F.9
F.10
F.11
F.12
F.13
F.14
F.15
<!-- COVERAGE-SPECDOC-END -->

## 18. 验收矩阵

### 18.1 结构

| ID | 判据 |
|---|---|
| `AC-S01` | 八业务域均有使命、权威数据、不变量、禁止职责与接口；无双 owner |
| `AC-S02` | `check.sh` 派生的全部 SpecDoc 二级标题 ID 均有主责落点，UNKNOWN/EXTRA 为 0 |
| `AC-S03` | 冻结锚中含非测试 Go 文件的实现目录均有目标归处，UNKNOWN/EXTRA 为 0 |
| `AC-S04` | 原型 nav 派生的全部入口均追到接口与失败/空态，UNKNOWN/EXTRA 为 0 |
| `AC-S05` | 接口目录覆盖平台边界、跨域、稳定 seam，稳定 ID 唯一 |
| `AC-S06` | 旧架构件未修改、未作为设计依据，目标件切换规则明确 |

### 18.2 语义

| ID | 判据 |
|---|---|
| `AC-M01` | principal 是授权唯一主体；agent 仅溯源且无授权输入 |
| `AC-M02` | enterprise 选库与 principal 行过滤分轴，缺一 fail-closed |
| `AC-M03` | gate verdict 与 execution status 分列，FAIL 证据可封存 |
| `AC-M04` | command 幂等、generation fencing、outbox/ACK 与业务结果边界明确 |
| `AC-M05` | 外部副作用没有被模板/数据包装成免审路径 |
| `AC-M06` | 平台故障不扩权，紧急面只允许单调收紧 effect |
| `AC-M07` | 跨企业路径恒无；企业内跨人仅按显式 policy |

### 18.3 证据与交付

| ID | 判据 |
|---|---|
| `AC-E01` | 输入锚与指纹可复算，现状断言声明搜索范围 |
| `AC-E02` | 七类 worked example 可转成用例，结果无歧义 |
| `AC-E03` | 未实测项集中登记，不在正文写成已具备 |
| `AC-E04` | 域依赖、接口引用、包映射、SpecDoc 追踪双向无断链 |
| `AC-E05` | opus50 与 deepseek 均完成全文独立评审，阻断项为 0 |

## 19. 评审与后续

评审按全文一次性提交 findings，不做逐点消息拉扯；最多三轮。机器检查只证明形状与已编码判据，
不能代替「域是否合理、接口 effect 是否完整、引用是否引对义」的人审。

本件 PASS 后，各 SP 以本件域与接口为共同边界分别写实现 spec；任何 SP 若要改变域 owner、跨域方向、
AccessCtx、接口 major schema、数据拓扑或信任边界，须重开架构评审，而不是在实现 PR 中顺手改。
