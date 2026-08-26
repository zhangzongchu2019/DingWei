# DingWei SP2 执行层与门禁设计合同

> 本件先冻结 SP2 的输入、边界、交付物与验收口径；合同通过前不写设计正文。

## 0. 合同头块

| 项 | 值 |
|---|---|
| **Spec ID** | `dingwei-sp2-design` |
| **合同版本** | v1.0 |
| **状态** | `draft-for-review` |
| **执笔** | `zzc-chatgpt56soltchangshark-2642` |
| **评审** | `zzc-opus50tchangshark-2642`、`zzc-deepseekv4pro08130ca16c13-2642`、`zzc-opus50tczc2000-2642` |
| **冻结施工锚** | `53d91aedd158c0a8618f0196f013cfee5cfd44aa` |
| **权威范围** | SpecDoc SP2：§5–§9 |
| **目标架构域** | `WRK`；消费 `GOV/IDN/PLT`，向 `KNO/COL/AGT/MSG` 提供稳定 seam |
| **目标件** | `docs/dingwei-sp2-design.md` |
| **分级** | L3（数据模型与状态机语义变更） |

### 0.1 范围与依赖判定

SpecDoc §0.3 对 SP2 的定义唯一且与目标架构一致：

1. 产品范围为 §5–§9；
2. 前置为 SP0、SP1、SP7a；
3. WRK 领域 schema migration/cutover 由 SP2 自己作为 SP7b 交付；
4. 本轮只做设计，前置“已设计”不升级为“已实现”；凡运行依赖未就绪均列转交门，不倒签 PASS。

## 1. 六个必备面

### ① 系统对接

SP2 设计 WRK 领域的项目、`board_task`、node DAG、角色绑定、派发、execution outcome、独立 gate verdict、
发版记录与复盘。必须细化而不扩充目标架构既有接口：

- `WRK-CMD-CreateProject`、`WRK-CMD-CreateTaskPlan`；
- `WRK-CMD-DispatchNode`、`WRK-CMD-RecordExecution`；
- `WRK-CMD-RecordGateVerdict`、`WRK-CMD-AppendRevision`；
- `WRK-CAP-DeployRelease`、`WRK-QRY-TaskSnapshot`；
- 消费 `GOV-QRY-ResolveGateSet` 与 `GOV-CMD-IssueEffectGrant`。

SP2 不拥有 principal、规则集、真实 deploy adapter、帖子正文、知识内容或仓储实现。

### ② 运行约束

- execution state 与 gate verdict 分轴；`execution done` 不等于 gate PASS。
- DAG eligibility = execution done 且 verdict 属于 `not_required/pass`；FAIL 不放行下游但该 node 终结并封存。
- mutating command 必带 request_id、expected generation/state、actor principal；重复返原 outcome。
- 派发遵守 owner epoch、generation、记账先行→外呼、迟到结果 fencing 与 reconciler。
- mcsd 调度语义保留；board 状态模型按新契约重构，既有并发语义须先由 conformance suite 固化。
- 门方按 principal 与执行者比对；Agent、session、显示名均不能作为门方身份。

### ③ 预计数据

只定义测量出口，不编造规模：project/task/node 数、DAG 边数/深度/宽度、角色绑定、派发重试、代际冲突、
execution/gate outcome、revision、release/rollback、复盘记录、outbox/audit 增长。每项预计值须带依据与复核点；
无依据写 `unknown`，不得用未知 TPS 证明某实现必成瓶颈。

### ④ 环境要求

- 设计只读冻结仓，不接生产、不改数据库/部署、不执行真实 deploy/rollback。
- 不新增外部依赖；保留 mcsd，不引入流程引擎。DMN 只由 SP0/GOV 求值，SP2 消费 GateSetSnapshot。
- 实现环境须经 SP1 的 AccessCtx 与 SP7a 的受控 transaction；缺前置时 fail-closed。
- secret、真实企业数据、真实主机/DSN 不进入正文或夹具。

### ⑤ 设计约束

1. **范围不扩**：仅设计 §5–§9；帖子、知识、Agent runtime、授权策略只定义依赖 seam。
2. **复用优先**：保留 mcsd 调度语义、CAS/generation、fan-out、reconciler 与记账先行→外呼切点。
3. **conformance 先行**：无法由套件证明的旧行为不得因“重写”被静默丢弃。
4. **门独立**：execution 与 gate 两轴不可折回单 status；gate FAIL 与 execution failed 分流。
5. **历史不可改**：返工创建 revision node；不原地改旧 node outcome、gate evidence、release record 或复盘。
6. **副作用不藏数据**：node/template 只编排；deploy/rollback 仍由授权、幂等、可审计 capability 执行。
7. **身份不可降级**：actor/reviewer 来自 AccessCtx principal；Agent 只作 audit provenance。
8. **状态未知不猜成功**：外部 effect 未知进入 reconcile；不得以“已发出”写成功。
9. **不新增依赖**：不引入外部 workflow engine、消息中间件或新数据库。
10. **SP7b 随本域交付**：本件定义 WRK schema/cutover/rollback 合同，不把迁移责任推回统一 SP7。

### ⑥ 输入来源

冻结锚：`53d91aedd158c0a8618f0196f013cfee5cfd44aa`

| 输入 | 版本 / sha256 | 用途 |
|---|---|---|
| `docs/dingwei-platform-specdoc.md` | v3.69 / `8b0b20b97af85bf1c42c1b8dd5a7c94a0e73d1de3100666c5acb2370ae56c61e` | §5–§9、附录 E owner_sp=SP2 |
| `docs/dingwei-platform-architecture.md` | v1.3 / `0a2288c7b9e245eded06944cd063a6d0d458c6b1f45a3a9534d29817b6c58fdc` | WRK 边界、接口、数据、迁移与验收方向 |
| `docs/dingwei-prototype.html` | v1.47 / `eeb71a5a77a53990569e08f9745165903bcf9e34cf6ce0a5047824167e1df4b8` | 看板、分级、待决、发版/回滚与复盘交互输入 |
| `docs/dingwei-sp0-design.md` | v1.1 / `f635b96e6c3574319d2e8df7e6a1c41e9693a66d7581504df60a6801531b8bec` | 规则/GateSet 与评审 seam 的已通过设计输入 |
| `docs/dingwei-sp1-design.md` | v1.1 / `8fafe6c5bac5595eaa771543dfc4199ba3670081786f1769f39833dc616e27bf` | AccessCtx/企业边界/Agent provenance 的已通过设计输入 |
| 冻结代码 | 同 commit | 只证现状、复用 seam 与迁移差距，不决定目标语义 |

## 2. 必须交付

| ID | 交付块 | 最低内容 |
|---|---|---|
| `D-SP2-01` | 范围与边界 | WRK 所有/不所有、上下游、权威数据、不变量 |
| `D-SP2-02` | 领域模型 | project/board_task/node/edge/role/execution/gate/revision/release/retro |
| `D-SP2-03` | 状态机 | execution/gate/DAG eligibility/封存分轴及全部转换 guard |
| `D-SP2-04` | 分级与门 | GateSetSnapshot 消费、gate catalog、冲突/subsumption、门方矩阵与防绕过 |
| `D-SP2-05` | 接口 | 8 个 WRK 接口逐项 schema/effect/auth/idempotency/generation/tx/error/audit |
| `D-SP2-06` | 调度并发 | mcsd/board seam、owner epoch、CAS、fan-out、迟到结果、outbox/reconcile |
| `D-SP2-07` | 失败与返工 | execution failed/gate FAIL/blocked/timeout/cancel/revision 分流 |
| `D-SP2-08` | 发版 | 八格 node 模板、preprod/prod、观察期门、rollback 判据与七项记录 |
| `D-SP2-09` | 复盘 | §9 输入与四段输出、版本/锚、回写 SP0 定义层的边界 |
| `D-SP2-10` | WRK 数据与 SP7b | schema、约束、migration/cutover/rollback、双写/回读、验收门 |
| `D-SP2-11` | 复用映射 | mcsd/board/dispatch/result_outbox 等 retain/adapt/rewrite/new/retire 与依据 |
| `D-SP2-12` | 追踪验收 | §5–§9、owner_sp=SP2 的附录 E 行、原型入口、负测与证据出口 |

同时交付 `specs/sp2/check.sh`：预期集合从冻结 SpecDoc/架构/原型机器派生，只认结构化覆盖块；
须有“删真落点转红、块外散文补 token 仍红”的变异反测。

## 3. 明确不交付

- SP0/SP1/SP3–SP8 的详细设计或实现；
- 实际代码、DDL/migration、数据库、部署、CI、真实发版/回滚；
- 新产品规则、风险属性、角色、授权权力或新架构接口 ID；
- 运行性能结论、生产流量或真实故障演练结果；
- 帖子正文/知识/Agent runtime 的内部模型。

## 4. 验收合同

### 4.1 结构

| ID | 判据 |
|---|---|
| `AC-SP2-01` | `D-SP2-01..12` 唯一齐全；§5–§9 heading 未知 0 |
| `AC-SP2-02` | owner_sp=SP2 的附录 E 行逐项映射，未知 0 |
| `AC-SP2-03` | 原型相关可达入口逐项映射，未知 0 |
| `AC-SP2-04` | 8 个 WRK 接口均有落点；目标架构新增接口 ID 0 |

### 4.2 语义

| ID | 判据 |
|---|---|
| `AC-SP2-05` | execution 与 gate 两轴独立，DAG eligibility/封存/FAIL 逐项无矛盾 |
| `AC-SP2-06` | 每个 mutating command 有 request_id、generation/state、principal、tx/audit |
| `AC-SP2-07` | 门方≠执行者按 principal 判；Agent/session/显示名反例均拒绝 |
| `AC-SP2-08` | gate FAIL、execution failed、blocked、timeout、cancel 的返工与终结路径互不混淆 |
| `AC-SP2-09` | deploy/rollback capability 不被 node/template 绕过授权、幂等和审计 |
| `AC-SP2-10` | 迟到、重投、外部 effect 未知、部分失败有 fencing/reconcile；不猜成功 |
| `AC-SP2-11` | conformance suite 覆盖既有并发语义；未覆盖项不宣称保留/替换完成 |
| `AC-SP2-12` | WRK schema migration/cutover/rollback 五列转交齐；设计 PASS 不升级实现 PASS |

### 4.3 证据与评审

- checker 结构通过只作旁证；三位评审者各自全文判断，不以多数票掩盖未闭阻断；
- verdict 使用 PASS/NO/INCONCLUSIVE，任一阻断未闭即 NO；
- 最多 6 轮，全文评审→一次性整改→全文复审，不逐项往返；
- 只判本轮阻断，不翻旧账；发现旧问题仅在直接破坏本设计时入阻断。

## 5. 冲突与变更纪律

| 冲突 | 处置 |
|---|---|
| SpecDoc 内部/与架构冲突 | 登记双方精确章节与后果，交 coordinator/业主，不自行取舍 |
| 新设计与已 PASS SP0/SP1 对撞 | 停止该路径，登记接口/语义差异，不改上游件 |
| 目标与代码现状冲突 | 记迁移差距；不得把旧实现提升为目标规则 |
| 合同范围变化 | bump 合同并重走三方评审，未过不写正文 |

## 6. 开工门

- [ ] opus50 合同 PASS；
- [ ] deepseek 合同 PASS；
- [ ] opus50tczc2000 合同 PASS；
- [ ] `specs/sp2/contract-check.sh` PASS；
- [ ] coordinator 登记施工锚与合同锚。

