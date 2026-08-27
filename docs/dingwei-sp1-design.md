# DingWei SP1 身份与隔离地基设计 v1.2

| 项 | 值 |
|---|---|
| **Design ID** | `dingwei-sp1-design` |
| **版本** | v1.2 |
| **状态** | candidate，待 terra＋deepseekv4pro 双评审 |
| **输入基线** | `acc531e55dce80e8c0e11db9abb4a5b6e86fb08d` |
| **合同** | `specs/sp1/spec.md` |
| **范围** | P1 主体链、一企业一 PG 库、RLS 隔离合同、AccessCtx 改名、Agent 绑定 |
| **目标架构** | `IDN` 主责，消费 `MSG/PLT` 既有接口；不增加架构接口 ID |

> v1.2：在统一 v0.2 基线收 RV-甲06／乙04／甲10；完成 AccessCtx 双轴代码迁移、三 role 合同与 enterprise registry/opaque database key 路由合同。

输入锚：`acc531e55dce80e8c0e11db9abb4a5b6e86fb08d` · `docs/dingwei-platform-specdoc.md` ·
`docs/dingwei-platform-architecture.md` · `docs/dingwei-prototype.html`

## D-SP1-01 P1 可信主体传播

### 1.1 信任链

```text
credential / channel proof
  → IDN-CMD-AuthenticateChannel
  → immutable SessionAuthRef(principal, enterprise_hint?, channel, assurance, authn_ref, generation)
  → IDN-CMD-OpenSession
  → AccessCtxRef
  → MSG canonical envelope（删除调用方自述 from/org/tenant）
  → Receiver 解析同一 AccessCtxRef
  → application command / query
```

| hop | 可写字段 | 禁止 |
|---|---|---|
| edge/authenticator | canonical principal/channel/assurance/authn_ref | 信客户端 principal/org |
| connection/session | 只持 SessionAuthRef/AccessCtxRef/generation | 改写主体、降级 assurance |
| envelope | 复制 ref 与 generation | 接受 body/header 自述身份 |
| Receiver | 解析 ref 并校验吊销/过期 | 缺 ref 回退 anonymous/default enterprise |

P1 不变量：连接层、canonical envelope、Receiver 所见 principal 必须相同；任一 ref 缺失、吊销、代际不符时，
mutating command 和状态观测都返回 `UNAUTHENTICATED`，不创建业务记录、不返回成功空集。

### 1.2 负例

1. 客户端在 envelope 写另一个 principal/org：edge 删除后以认证主体为准；若签名覆盖不一致则拒绝。
2. 复用已吊销 SessionAuthRef：Receiver fail-closed。
3. connection 有主体而 envelope 无 ref：拒绝，不从连接对象隐式补值。
4. envelope ref 合法但 enterprise mapping 0/多结果：`TENANT_NOT_BOUND`，SQL/query spy 必为 0。

## D-SP1-02 身份模型与 Agent 绑定

### 2.1 权威对象

| 对象 | 唯一键 | 权威字段 | 不拥有 |
|---|---|---|---|
| `Enterprise` | enterprise_id | ops registration status、opaque database key ref | 业务角色/项目 |
| `Principal` | principal_id | enterprise_id、natural_person_ref、status | Agent 运行状态 |
| `ChannelBinding` | `(issuer, platform_subject)` | principal、channel、assurance、generation | 授权决定 |
| `SessionAuth` | authn_ref | principal、channel、assurance、issued/expiry/revocation | 业务 scope |
| `AgentBinding` | `(agent_id, binding_generation)` | owner_principal、authn_ref、bound/revoked time | capability/门权 |

同一自然人在不同企业映射为不同 principal；不存在自动跨企业合并。邮箱/手机号/显示名不能作为静默合并键。

### 2.2 `agent → principal` bind/rebind

`IDN-CMD-BindAgent(agent_id, AccessCtxRef, authn_ref, request_id, expected_generation)`：

- owner principal 只从 AccessCtx 解析，不收请求中的 owner 字段；
- 首绑创建 generation=1；重绑须旧 owner 或治理批准，并 generation+1；
- request_id 重放返回原 binding；迟到请求 FENCED；
- 审计记录 old/new principal、reason、authn_ref、request_id、generation，不把 secret 落账。

### 2.3 两条不可放宽禁令

1. 绑定完成前，`agent` 列不得作审计交叉证据；审计 actor 仍取 principal。
2. 绑定前后，Agent 永不参与授权、批准、门 verdict、assurance 或 capability 求值。

授权入口的 schema 不出现 `agent_id`；若业务 effect 需要溯源，`agent_id` 仅在 AuditRecord 的 provenance 子对象。

## D-SP1-03 `AccessCtx` 双轴合同

### 3.1 逻辑形状

```go
type AccessCtx interface {
    Principal() PrincipalRef
    Org() EnterpriseRef
    AuthnRef() AuthnRef
    Channel() ChannelRef
    Assurance() AssuranceLevel
    IssuedAt() time.Time
    ExpiresAt() time.Time
    Generation() uint64
}
```

这是语言无关合同，不要求按该 Go 代码直接实现。`Org()` 只决定企业边界/数据库，`Principal()` 决定 owner/actor；
两者不可互替。AccessCtx 以 opaque ref 传播，业务域不能修改字段。

### 3.2 签发与吊销

- OpenSession 在认证成功后签发 AccessCtxRef，包含 authn/session generation。
- ResolveAccess 每次验证 expiry、principal status、channel binding generation、revocation watermark。
- 渠道解绑不重写历史 actor，只使未来解析失败；缓存最大 TTL 不超过凭据剩余有效期。

### 3.3 SystemCtx

系统动作使用独立 service principal / SystemCtx；不得用“默认 principal”冒充自然人。SystemCtx 不自动拥有跨企业权限，
每个 effect 仍需 service role 与 scope。使用点列为评审面，不能借改名隐藏增长。

## D-SP1-04 企业注册与一企业一 PG 库

### 4.1 平台注册表

| 字段 | 约束 |
|---|---|
| enterprise_id | 平台生成、不可复用 |
| legal/display refs | 展示/审计，不作运行时猜库键 |
| database_key | opaque key，只供受控 resolver；不向请求方返回 DSN |
| status | pending/active/suspended/retired；仅 ops 审核后 active |
| config refs | IM 渠道、库名、dev/test 环境、LLM 出网 VPN、Git 地址一次配齐 |
| generation/audit | 每次变更 CAS、审批、actor、reason、timestamp |

查不到企业时绝不自动建库；域名、邮箱后缀、渠道显示名不得推断 enterprise/database。

### 4.2 数据拓扑

```text
platform database
  enterprise_registry / principal / channel_binding / agent_binding / database routing metadata

enterprise database (one per enterprise)
  domain schemas: GOV/WRK/KNO/CAP/COL/AGT/MSG business records
  person-level RLS inside the enterprise database
```

企业数据库不存平台级注册表/密钥；平台库不存企业业务正文。database resolver 返回限时 `DataScopeGrant + opaque database key`，
连接池按 database key 隔离；不得在一个池内切换 DSN 冒充多企业。

### 4.3 选择顺序

1. ResolveAccess 得 principal/enterprise。
2. ResolveDataScope 查 ops 注册表；0 或多结果即拒。
3. 以 opaque database key 选池；在任何 SQL/selector 前完成。
4. 开事务，第一条设置 principal context。
5. 领域 repository 执行；同事务写 outbox/audit。

deny/not-found 的状态、响应体大小和可观察时延不得泄露目标存在性；拒绝路径 query spy=0，且留最小审计。

## D-SP1-05 人层 RLS 隔离合同

### 5.1 三 role 与属主

| role | 权限 | 禁止 |
|---|---|---|
| `dw_owner` | NOLOGIN object owner，平台 DDL 流程使用 | 不下放企业侧、不作 runtime login |
| `dw_app` | runtime DML，经 policy | 非 superuser、无 BYPASSRLS、非 table owner、无 DDL |
| `dw_migrate` | 受控 migration service role | 非日常 ops；短时、审批、审计、不可被应用池使用 |

企业级 ops 至多获得 `dw_app` 级能力；平台 ops 特权风险被移到平台治理面，并未消失，须重两签、时限和审计。

### 5.2 表级强制项

每张人层业务表必须同时满足：

1. `owner_principal_id NOT NULL`，由 AccessCtx 写入，不收 payload owner；
2. `ENABLE ROW LEVEL SECURITY` + `FORCE ROW LEVEL SECURITY`；
3. SELECT/UPDATE/DELETE policy 取事务局部 `dw.principal`；
4. INSERT/UPDATE 配 `WITH CHECK`；
5. 关联表、间接查询、view/function 同步审查，不以主表有 policy 推定闭合；
6. 缺 context 的 current_setting 路径默认拒绝，不回退空串/公共用户；
7. runtime role 不是属主且不能切 role 绕过；
8. policy/version 进入 conformance evidence。

### 5.3 事务包装

`PLT-CMD-RunInAccessTx(AccessCtxRef, DataScopeGrant, request_id, operation)` 的顺序是合同：

```text
checkout enterprise-specific pool connection
BEGIN
SET LOCAL dw.principal = verified principal
SET LOCAL dw.enterprise = verified enterprise (audit/assertion, not routing)
execute repository operation
write outbox/audit
COMMIT or ROLLBACK
```

禁止 session-level `SET`。连接池复用负测须先用 principal A，再借同一物理连接以 B/缺 context 查询，确认 A 数据不可见。

### 5.4 RLS 不覆盖的残余面

备份、只读副本、逻辑复制槽、superuser、文件系统快照不受 RLS 保护。转交平台 ops/SP7a：介质加密、独立凭据、
访问审批、时限、全量审计、恢复演练与泄漏响应。不得写“RLS 已覆盖全部数据面”。

## D-SP1-06 Agent 绑定流程、失败与审计

### 6.1 正常流程

1. 已认证 principal 提交 runtime order；Agent 尚无授权身份。
2. AGT 激活实例后，IDN 用 order 的 AccessCtxRef 调 BindAgent。
3. binding 提交成功后，Agent 收到只含 binding ref/generation 的 receipt。
4. Agent 发出的 envelope 仍由 session auth 决定 principal；binding 只用于核对 provenance。

### 6.2 失败矩阵

| 场景 | 结果 |
|---|---|
| agent_id 已绑另一 principal，缺 rebind grant | CONFLICT，不改旧绑定 |
| expected generation 旧 | FENCED |
| AccessCtx 吊销/过期 | UNAUTHENTICATED |
| binding store 成功、receipt 失败 | 重投 request_id 返回原 binding；迟到 receipt 不复活旧代际 |
| binding 未建立却写 `agent` 审计交叉证据 | 拒绝该证据资格；业务 actor 仍为 principal |
| 授权请求含 agent_id 作为主体 | schema/policy 拒绝并告警 |

### 6.3 AuditRecord

`audit_id, principal_actor, enterprise, channel, authn_ref, agent_provenance?, binding_generation?, effect,
resource_ref, request_id, outcome, reason_code, ts`。敏感 credential/DSN 不落账。

## D-SP1-07 `TenantCtx → AccessCtx` 迁移

### 7.1 受控旧标识

`TenantCtx`、`PendingTenantCtx`、`NewTenantCtx` 三个标识按 prod/test/comment 分别棘轮；一降一升不能以总数持平蒙混。
迁移期禁止新增生产调用点，解析失败或扫描根缺失判闸坏。

### 7.2 阶段

| 阶段 | 动作 | 完成门 |
|---|---|---|
| M0 inventory | AST 列出定义、构造、传播、Org-only 判定、测试/注释 | 三分类基线可复算 |
| M1 introduce | 新增 AccessCtx port 与 adapter，不改业务语义 | P1 传播正负测通过 |
| M2 dual-read | 旧调用经 adapter 显式补 principal；禁止默认值 | Org()/Principal() 缺一拒绝 |
| M3 migrate | 逐域替换生产调用；每批有 conformance | prod 棘轮单调下降 |
| M4 remove | 删除旧构造/类型/adapter，更新 test/comment | 三类归零 |
| M5 semantic verify | 跨人、跨企业、错库、池残留负测 | 全部 PASS 后才称迁移完成 |

改名完成不等于隔离完成；M4 只证明符号归零，M5 才证明双轴语义。

## D-SP1-08 SP7a 转交与验收追踪

### 8.1 SP7a 五列转交

| 被测对象 | 输入 | 期望 | 证据形态 | 验收门 |
|---|---|---|---|---|
| enterprise registry/resolver | 0/1/多映射、伪造 org | 仅 1 返回 opaque key；其余 deny，query spy=0 | integration test + audit | SP1 用例评审 / SP7a delivery.verify |
| enterprise pool router | 两企业、错 key、池复用 | 不串库、不自动建库 | pool spy + DB identity | SP7a delivery.verify |
| RLS role/owner/policy | catalog fixture | 三 role/FORCE/WITH CHECK/context 全满足 | catalog query + mutation tests | 安全代码评审 |
| RunInAccessTx | A→B→missing context 同物理连接 | 无残留身份，缺 context 拒绝 | integration test | SP7a delivery.verify |
| AgentBinding repository | bind/rebind/replay/late receipt | 幂等、FENCED、审计齐 | conformance suite | SP1 delivery.verify |
| legacy-name migration | AST 三分类 | 各棘轮只降，终值 0 | gate output + mutation | 每批代码评审 |
| backup/replica/slot | 受控介质与账号 | 加密、审批、时限、审计、演练 | ops evidence | 安全交付验证 |

### 8.2 SpecDoc / E 覆盖

<!-- COVERAGE-SP1-BEGIN -->

| SpecDoc 来源 | 设计落点 |
|---|---|
| `§7.1` | `D-SP1-01` |
| `§21` | `D-SP1-02` |
| `§21.1` | `D-SP1-02`、`D-SP1-03` |
| `§21.1a` | `D-SP1-02`、`D-SP1-06` |
| `§21.1b` | `D-SP1-02`、`D-SP1-03` |
| `§21.1c` | `D-SP1-03`、`D-SP1-07` |
| `§21.2` | `D-SP1-01`、`D-SP1-03` |
| `§21.3` | `D-SP1-01` |
| `§21.4` | `D-SP1-01`、`D-SP1-02` |
| `§21.4-1` | `D-SP1-01`、`D-SP1-02` |
| `§21.4a` | `D-SP1-01`、`D-SP1-03` |
| `§21.4b` | `D-SP1-02`、`D-SP1-03` |
| `§22.1` | `D-SP1-03`、`D-SP1-04` |
| `§22.1a` | `D-SP1-03`、`D-SP1-04` |
| `§22.1b` | `D-SP1-04`、`D-SP1-05` |
| `§22.1c` | `D-SP1-03`、`D-SP1-05` |
| `§22.3` | `D-SP1-04`、`D-SP1-05`、`D-SP1-08` |
| `§22.4` | `D-SP1-01`、`D-SP1-03`、`D-SP1-08` |

| 探测来源 | 设计落点 |
|---|---|
| `E.1#12` | `D-SP1-03`、`D-SP1-05` |
| `E.1#40` | `D-SP1-03`、`D-SP1-08` |
| `E.1#41` | `D-SP1-03`、`D-SP1-04` |
| `E.1#42` | `D-SP1-01`、`D-SP1-03` |
| `E.1#43` | `D-SP1-04` |
| `E.1#44` | `D-SP1-03`、`D-SP1-05` |
| `E.1#45` | `D-SP1-03`、`D-SP1-06` |
| `E.1#87` | `D-SP1-02` |
| `E.1#88` | `D-SP1-01`、`D-SP1-02` |
| `E.1#89` | `D-SP1-01`、`D-SP1-03` |
| `E.1#90` | `D-SP1-02`、`D-SP1-06` |
| `E.1#91` | `D-SP1-02` |
| `E.1#102` | `D-SP1-01`、`D-SP1-03` |
| `E.1#103` | `D-SP1-01`、`D-SP1-03` |
| `E.1#104` | `D-SP1-01` |
| `E.1#105` | `D-SP1-01`、`D-SP1-02` |
| `E.1#115` | `D-SP1-03`、`D-SP1-08` |
| `E.1#116` | `D-SP1-03`、`D-SP1-08` |
| `E.1#120` | `D-SP1-06`、`D-SP1-08` |
| `E.1#121` | `D-SP1-06`、`D-SP1-08` |
| `E.1#122` | `D-SP1-06`、`D-SP1-08` |
| `E.1#123` | `D-SP1-06`、`D-SP1-08` |
| `E.1#124` | `D-SP1-06`、`D-SP1-08` |
| `E.1#125` | `D-SP1-06`、`D-SP1-08` |
| `E.1#126` | `D-SP1-03`、`D-SP1-08` |
| `E.1#134` | `D-SP1-06`、`D-SP1-08` |
| `E.1#135` | `D-SP1-06`、`D-SP1-08` |
| `E.1#71` | `D-SP1-03`、`D-SP1-08` |
| `E.1#150` | `D-SP1-04`、`D-SP1-05` |
| `E.1#151` | `D-SP1-04`、`D-SP1-06` |
| `E.1#152` | `D-SP1-04`、`D-SP1-05` |
| `E.1#159` | `D-SP1-04` |
| `E.1#161` | `D-SP1-07` |
| `E.1#162` | `D-SP1-03`、`D-SP1-05` |
| `E.1#167` | `D-SP1-02`、`D-SP1-06` |
| `E.1#168` | `D-SP1-03` |
| `E.1#176` | `D-SP1-02`、`D-SP1-06` |
| `E.1#182` | `D-SP1-02`、`D-SP1-06` |
| `E.1#189` | `D-SP1-03`、`D-SP1-08` |
| `E.3#4` | `D-SP1-03`、`D-SP1-05` |
| `E.3#17` | `D-SP1-01`、`D-SP1-03` |
| `E.3#18` | `D-SP1-03`、`D-SP1-05` |
| `E.3#28` | `D-SP1-01`、`D-SP1-02` |
| `E.3#32` | `D-SP1-03`、`D-SP1-08` |
| `E.3#39` | `D-SP1-06`、`D-SP1-08` |
| `E.3#40` | `D-SP1-06`、`D-SP1-08` |
| `E.3#41` | `D-SP1-06`、`D-SP1-08` |
| `E.3#42` | `D-SP1-06`、`D-SP1-08` |
| `E.3#45` | `D-SP1-06`、`D-SP1-08` |
| `E.3#46` | `D-SP1-06`、`D-SP1-08` |
| `E.4#9` | `D-SP1-03`、`D-SP1-08` |
| `E.4#14` | `D-SP1-03`、`D-SP1-05` |
| `E.4#40` | `D-SP1-03`、`D-SP1-05` |
| `E.4#46` | `D-SP1-01`、`D-SP1-02` |
| `E.4#52` | `D-SP1-03`、`D-SP1-08` |
| `E.4#56` | `D-SP1-03`、`D-SP1-08` |
| `E.4#57` | `D-SP1-06`、`D-SP1-08` |
| `E.4#58` | `D-SP1-08` |
| `E.4#59` | `D-SP1-06`、`D-SP1-08` |
| `E.4#64` | `D-SP1-04`、`D-SP1-05` |

其中敏感内容与 sink（E.1#115–135、E.3#32/39–46、E.4#52–59）属于 SP1 安全合同的交叉边界，
本件只定义 identity/scope 输入与 fail-closed 要求；判定器实现由其 owner SP 交付，不在本包吞并。

### 8.3 原型入口

| 入口 | 本件落点 |
|---|---|
| `#a7` 身份与渠道绑定 | D-SP1-01、02、03 |
| `#b3` 受控仓储 API·租户隔离核验 | D-SP1-04、05、08 |
| `#a1` Agent 列表 | `D-SP1-02`、`D-SP1-06` |
| `#a4` 配置新 Agent 工单 | `D-SP1-02`、`D-SP1-06` |
| `#s1` 跨人 capability 待决 | `D-SP1-03`；仅消费 verified principal/enterprise，批准归 SP8 |
| `#s6` Agent 管理 | `D-SP1-02`、`D-SP1-06`；Agent 只 provenance，不升级授权 |

<!-- COVERAGE-SP1-END -->

### 8.4 验收结论边界

本件 PASS 只表示 SP1 身份与隔离设计闭合，不表示注册表、AccessCtx、RLS、企业库或 Agent 绑定已经实现。

## D-SP1-09 三阻断收口登记

| RV | 收口载体 | 可复跑判据 |
|---|---|---|
| RV-甲06 | §3、§7；`internal/repo` 的 `AccessCtx{Org, Principal}`；原型仓储入口同步 | `scripts/legacy-name-gate.sh` 报 prod/test/comment=0；`go test ./internal/repo` 验双轴缺一触库前拒 |
| RV-乙04 | §5 三个受控 role、FORCE/WITH CHECK/SET LOCAL；`specs/fixtures/rls/` | `specs/fixtures/rls/RUN.sh`（有 PG 环境时）；无环境不得冒充已实跑 |
| RV-甲10 | §4 enterprise registry、opaque database key、0/1/多映射、企业专池 | `specs/sp1/contract-check.sh` 对字段/顺序/拒绝语义精确断言并跑变异反测 |

三条在设计与当前仓储 port 层闭合；真实企业 registry、DSN secret resolver、PG DDL/RLS 部署仍归 SP7a 实现门，
不得以本设计 PASS 倒签实现 PASS。
