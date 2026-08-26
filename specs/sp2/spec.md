# DingWei SP2 执行层与门禁设计合同

> 本件先冻结 SP2 的输入、边界、交付物与验收口径；合同通过前不写设计正文。

## 0. 合同头块

| 项 | 值 |
|---|---|
| **Spec ID** | `dingwei-sp2-design` |
| **合同版本** | v1.5 |
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

### 0.2 SYNC IMPACT REPORT（v1.0 → v1.1）

业主将评审上限从 6 轮收紧为 4 轮。本次只改评审收敛协议，SP2 范围、交付物、验收项和技术约束不变。

### 0.3 SYNC IMPACT REPORT（v1.1 → v1.2）

- 开工门与证据验收写死三位评审者，要求 3/3 独立 PASS，不以多数票替代。
- 将 SP0、SP1、SP7a、SP7b 的当前状态、设计期处理和实现期开工门分列，防止把“设计已定”升级成“前置已履约”。
- 冻结锚实算口径写入 checker：§5–§9 heading=29；owner_sp=SP2 的 E 行=30（E.1=16、E.3=3、E.4=11）。

### 0.4 SYNC IMPACT REPORT（v1.2 → v1.3）

将评审方预公布的十条判据固化为 `specs/sp2/check.sh`：四类预期集合全由冻结输入派生，只认
`COVERAGE-SP2` 块内两列表格与真实 `D-SP2-*` 落点；缺块/空预期/输入不可取均 exit 2；自带正交变异反测。

### 0.5 SYNC IMPACT REPORT（v1.3 → v1.4）

- 沿革中的“6 轮”不再参与生效规则检查；checker 只检查当前评审协议段为最多 4 轮。
- 删除冻结范围的手写 29/30 比较；只要求四类机器派生集合非空并打印该次实算值。
- 每条正文 AC 固定为“ID / 受检范围 / 判据 / 判不了什么”四列，并由 checker 校验结构化验收块。

### 0.6 前置依赖的当前状态与处置

| 前置/责任 | 当前可用事实 | SP2 设计期处理 | SP2 实现期开工门 |
|---|---|---|---|
| **SP0** | SpecDoc 产品规则已生效；SP0 设计件已通过本轮评审 | 消费 GateSet/评审 seam，不能改其规则语义 | SP0 对应实现开工门和接口 conformance 未闭前，SP2 只允许 mock/contract test，不接真实 GOV 写面 |
| **SP1** | AccessCtx、主体传播、Agent provenance 设计已通过本轮评审 | 所有 actor/reviewer schema 依该设计；不回退 TenantCtx/agent/session 身份 | P1/AccessCtx/agent binding 的实现与负测未闭前，SP2 mutating/status observation 均不得上线 |
| **SP7a** | 目标架构接口已定，**实现尚未交付** | 只依赖 `RunInAccessTx`/migration runner/仓储 port 合同；与 SP2 详细设计并行 | 受控 transaction、隔离与 migration runner conformance 未过，不得执行 WRK schema cutover |
| **SP7b（SP2 责任）** | 尚未设计/实施 | 本件必须给 WRK schema、migration、cutover、rollback 五列合同 | 由 SP2 实现批次履约；不得推给“统一 SP7”或拿 SP7a PASS 代签 |

上述“设计已通过”只允许作为接口设计输入，不构成实现 PASS。若某前置的 reviewer/coordinator 落账尚未齐，
该缺口留在对应包的开工门；SP2 不代填、不倒签，也不因此停止自身设计。

### 0.7 SYNC IMPACT REPORT（v1.4 → v1.5）

- B1 确认：SP0/SP1 设计 PASS 只作输入，未履约项仍留对应实现开工门，不倒签前置完成。
- 原型预期集合改由冻结架构 §13 中主责域含 `WRK` 的行机器派生，不再按导航文案猜“相关入口”。
- 新增附录 F 判据集合，承接 F.1.2、F.1.3、F.2 #4；A§21 悬空引用登记后上交，不自行补造。

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
| `D-SP2-06` | 调度并发 | mcsd/board seam、owner epoch、CAS、fan-out、迟到结果、outbox/reconcile；承接 F.2 #4 的 agent 择人算法与可复核判据 |
| `D-SP2-07` | 失败与返工 | execution failed/gate FAIL/blocked/timeout/cancel/revision 分流；承接 F.1.3 短程小任务的可复核交付验证判据 |
| `D-SP2-08` | 发版 | 八格 node 模板、preprod/prod、观察期门、rollback 判据与七项记录 |
| `D-SP2-09` | 复盘 | §9 输入与四段输出、版本/锚、回写 SP0 定义层的边界 |
| `D-SP2-10` | WRK 数据与 SP7b | schema、约束、migration/cutover/rollback、双写/回读、验收门 |
| `D-SP2-11` | 复用映射 | mcsd/board/dispatch/result_outbox 等 retain/adapt/rewrite/new/retire 与依据 |
| `D-SP2-12` | 追踪验收 | §5–§9、owner_sp=SP2 的附录 E 行、架构 §13 WRK 原型入口、F.1.2 `delivery.verify` 验收门、负测与证据出口 |

`specs/sp2/check.sh` 已随合同先落，正文必须提供 `COVERAGE-SP2-BEGIN/END` 块。检查器只认块内严格两列表格：
第一列为派生 token，第二列须含真实 `D-SP2-xx`；块外提及、块内散文、空落点、“见全文”均不计。
检查器输出五类预期/实检/未知集合；未知非空不得宣称覆盖完成，预期空/块缺失/输入不可取均 exit 2。
`--selftest` 固化：正常放行、删真落点转红、块外补同 token 仍红、缺块 exit 2。
正文另须提供 `ACCEPTANCE-SP2-BEGIN/END` 四列表格；每条 `AC-SP2-*` 独立填写受检范围与能力边界，
不得用全局边界句代替逐条说明，也不得写空值或“见全文”。

## 3. 明确不交付

- SP0/SP1/SP3–SP8 的详细设计或实现；
- 实际代码、DDL/migration、数据库、部署、CI、真实发版/回滚；
- 新产品规则、风险属性、角色、授权权力或新架构接口 ID；
- 运行性能结论、生产流量或真实故障演练结果；
- 帖子正文/知识/Agent runtime 的内部模型。

## 4. 验收合同

<!-- ACCEPTANCE-SP2-BEGIN -->

### 4.1 结构

| ID | 受检范围 | 判据 | 判不了什么 |
|---|---|---|---|
| `AC-SP2-01` | checker 从冻结 SpecDoc 派生的 §5–§9 heading 集合 | 每个 token 在 COVERAGE 块有 D-SP2 落点，未知 0 | 落点语义是否正确 |
| `AC-SP2-02` | checker 派生的 owner_sp=SP2 附录 E 集合 | 逐项结构化落点，未知 0 | 探测手段是否足够 |
| `AC-SP2-03` | checker 从冻结架构 §13 主责域含 WRK 的行派生、并在冻结原型核存在的入口集合 | 逐项结构化落点，未知 0 | UI 是否好用或实现 |
| `AC-SP2-04` | checker 派生的目标架构 WRK 接口集合 | 逐项落点；新增接口 ID 0 | 接口字段语义是否完整 |

### 4.2 语义

| ID | 受检范围 | 判据 | 判不了什么 |
|---|---|---|---|
| `AC-SP2-05` | node execution/gate/eligibility/seal 状态与 guard | 两轴独立，eligibility/封存/FAIL 无矛盾 | 代码状态机是否实现 |
| `AC-SP2-06` | 正文列出的全部 WRK mutating command | request_id、generation/state、principal、tx/audit 齐 | SDK/handler 是否漏传 |
| `AC-SP2-07` | reviewer/implementer 冲突与 Agent/session/display-name 反例 | 比对 verified principal，反例均拒绝 | 真实 reviewer 是否具备专业能力 |
| `AC-SP2-08` | gate FAIL/execution failed/blocked/timeout/cancel | 返工、终结与证据路径分别定义 | 运行异常是否穷尽 |
| `AC-SP2-09` | deploy/rollback node、effect grant、capability、result | node/template 不绕授权、幂等、审计 | 外部部署器是否可靠 |
| `AC-SP2-10` | 迟到、重投、未知 effect、部分失败 | fencing/reconcile 齐且不猜成功 | 生产网络故障覆盖率 |
| `AC-SP2-11` | 合同列出的既有并发语义与 conformance 用例 | 每项有正反例；未覆盖项不宣称已判 | 未观测隐藏语义 |
| `AC-SP2-12` | WRK schema migration/cutover/rollback 转交表 | 被测对象/输入/期望/证据/验收门齐 | 实际迁移与回滚结果 |

### 4.3 评审证据门

| ID | 受检范围 | 判据 | 判不了什么 |
|---|---|---|---|
| `AC-E05` | 同一正文锚上的三份独立 verdict | 三位评审者均 PASS（3/3）；缺任一票即未过 | 票的专业质量与判断正确性 |

<!-- ACCEPTANCE-SP2-END -->

### 4.4 证据与评审

- checker 结构通过只作旁证；三位评审者各自全文判断，须 3/3 PASS，不以多数票掩盖未闭阻断；
- verdict 使用 PASS/NO/INCONCLUSIVE，任一阻断未闭即 NO；
- **最多 4 轮**：每轮全文一次性给全量阻断，不拆成逐点往返；
- 第 3 轮仍有范围/语义争议即交 coordinator/业主裁定；第 4 轮只签 PASS/NO，不再追加细节讨论；
- 同一轮内晚到的补充意见并入该轮全量清单；作者一次性整改，不边收边推版；
- 只判本轮阻断，不翻旧账；发现旧问题仅在直接破坏本设计时入阻断。

## 5. 冲突与变更纪律

| 冲突 | 处置 |
|---|---|
| SpecDoc 自身矛盾 | 登记冲突双方的精确章节、两种后果与建议，交 coordinator/业主，不自行取舍 |
| SpecDoc 与目标架构冲突 | 登记双方精确章节与后果，交 coordinator/业主，不自行设优先级 |
| SpecDoc F.1.2 引用不存在的“架构 A§21 转交合同” | 登记为悬空引用；本件只承接明确写出的 `delivery.verify` 门，不臆造 A§21 内容，交 coordinator/业主修权威引用 |
| 新设计与已 PASS SP0/SP1 对撞 | 停止该路径，登记接口/语义差异，不改上游件 |
| 目标与代码现状冲突 | 记迁移差距；不得把旧实现提升为目标规则 |
| 合同范围变化 | bump 合同并重走三方评审，未过不写正文 |

## 6. 开工门

- [ ] opus50 合同 PASS；
- [ ] deepseek 合同 PASS；
- [ ] opus50tczc2000 合同 PASS；
- [ ] `specs/sp2/contract-check.sh` PASS；
- [ ] `specs/sp2/check.sh --selftest` PASS（正文未创建前普通 check 应 exit 2，不算通过）；
- [ ] coordinator 登记施工锚与合同锚。
