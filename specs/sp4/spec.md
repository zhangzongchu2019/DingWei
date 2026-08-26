# DingWei SP4 skill / MCP 体系设计合同

> 本件冻结 SP4 的范围、前置、交付物与验收口径；合同 3/3 PASS 且 coordinator 登记前不写正文。

## 0. 合同头块

| 项 | 值 |
|---|---|
| **Spec ID** | `dingwei-sp4-design` |
| **合同版本** | v1.1 |
| **状态** | `draft-for-review` |
| **执笔** | `zzc-opus50tczc2000-2642` |
| **评审** | `zzc-chatgpt56soltchangshark-2642`、`zzc-opus50tchangshark-2642`、`zzc-deepseekv4pro08130ca16c13-2642` |
| **冻结施工锚** | 组合冻结：SpecDoc `4220195`（v3.73）；架构/原型/SP0–SP3/代码基线 `6a98629` |
| **权威范围** | SpecDoc §14–§16；owner_sp 含 SP4 的附录 E 行 |
| **目标架构域** | `CAP`；消费 `IDN/GOV/KNO/PLT`，向 `AGT/MSG/WRK` 提供受控资产分发与运行态查询面 |
| **目标件** | `docs/dingwei-sp4-design.md` |
| **分级** | L3（安全面＝是） |

<!-- EVENT-SET-SP4: DERIVED -->

**事件集合处置**：与 SP3 不同，本域**有** owner=CAP 事件。冻结架构实算 `EV-CAP-*` 为 2
（`EV-CAP-DistributionRequested`、`EV-CAP-InstallAcknowledged`），故事件面按机器派生集合验收，不设 NOT_APPLICABLE 标记。
新增事件 ID 须先改目标架构 §4.2 并 bump 本合同，不得在正文私造。

### 0.1 范围判定〔实取，非转述〕

冻结锚上实取的权威定义：

1. SpecDoc §0.3：`SP4 skill / MCP 体系 | §14–§16 | L3（安全面=是） | 前置 SP1、SP3、SP8（晋升审批依赖 SP8）`；
2. 架构 §17 责任追踪：`§14 判据/MCP → CAP（协作 KNO·GOV）`、`§15–§16 分发/生命周期 → CAP（协作 GOV·AGT·MSG）`——与 §0.3 一致；
3. 横切附录 E 只认领 `owner_sp` 含 SP4 的行；
4. CAP 领域 schema migration/cutover 由 SP4 自己作为 SP7b 交付，不推回统一 SP7；
5. 不设计 SP1 身份、SP3 知识内容、SP8 授权策略、AGT runtime 与 SP6 界面的内部模型，只定义 seam。

### 0.2 冻结锚上的机器派生实算值〔本合同不写手写分母，此处仅为送审时的实算快照，判据以 checker 现算为准〕

`§14–§16 heading = 28` · `owner_sp 含 SP4 的 E 行 = 45（E.1=27 · E.3=6 · E.4=12）` ·
`架构 §6 CAP 接口 = 5` · `架构 §4.2 CAP 事件 = 2` · `架构 §13 主责域含 CAP 的原型入口 = 1（s5，冻结原型中实存）`。

🔴 **heading 取法本身是判据，不是实现细节**：**必须以「`## §14` 起、`## §17` 止」的单区间取**，
**禁止按章分段（§14 段 ＋ §15 段 ＋ §16 段）拼接** —— SpecDoc 排版异常使 §15.4 / §15.5 物理落在 `## §16` 之内，
分段取会**静悄悄丢掉这两节**（实测：按章取 §15 段只得 7 项，缺 `§15.4`、`§15.5`）。
`specs/sp4/check.sh` 已把「派生集合必须含 `SRC:§15.4` 与 `SRC:§15.5`」写成硬断言（缺任一即 exit 2），
并有一条专门的陷阱反测证明分段取法会被拦下。

<!-- RECUSAL-SP4: NO-REF-11.6AB -->

🔴 **本件（合同与正文）不引用 SpecDoc `§11.6a` / `§11.6b`**：实测冻结锚上 §14–§16 全文引用该两节 **0 次**，
故评审方 opus50 无需回避任一条 AC。若正文后续确需引用，**必须在 bump 时于本节点明并标注受影响的 AC 须其回避**，
不得等到投票时才发现；`contract-check.sh` 对此有反向断言（合同内出现该引用即红）。

### 0.3 前置依赖的当前状态与处置

| 前置/输入 | 冻结锚可见事实 | SP4 设计期处理 | SP4 实现期开工门 |
|---|---|---|---|
| **SP1** | 冻结 SP1 设计件头为 `draft，待双评审`；coordinator 已在本轮外告知其收口 | 只消费 AccessCtx/principal/企业边界既有 seam；不把消息 verdict 倒签进冻结件 | SP1 verdict 锚与 AccessCtx/隔离负测未落账前，CAP 只做 contract test，不上线分发与运行态查询 |
| **SP3** | 冻结 SP3 设计件头为 `draft，待三方全文评审`；coordinator 已告知收口，本席已投 PASS | 复用其 domain/visibility/晋升/装载语义；**CAP 只持 asset/version 与 KNO ref，不另造旁路**（SP3 §3.3 已定） | SP3 verdict 锚与四维/装载负测未落账前，不接真实 KNO 装载 |
| **SP8（§0.3 明列前置）** | 🔴 **SP8 尚未设计**：`§23–§24` 无设计件，平台级晋升审批（§16.1b、§15.1a、§24）的批准权无权威定义 | **不自造批准权**：只定义 CAP 侧消费 `approval_ref` 的形状、校验与拒绝路径；批准语义留 GOV/SP8 | SP8 批准权与 §24 重两签路径未定义前，平台级晋升与 immutable 名单变更**一律拒绝**，不得以 CAP 本地审批替代 |
| **SP7a/PLT（协作输入，非 §0.3 前置）** | 受控仓储 API、migration runner 未在本分支交付 | CAP schema 与 repository port 并行设计；不自建 DB 句柄、不在应用层补租户过滤 | 受控 transaction、隔离与 migration runner conformance 未过，不得 cutover |
| **SP2（协作输入，非 §0.3 前置）** | SP2 正文已收口，锚 `53e4a2a` | 分发目标与执行侧 seam 只读消费；不得改 WRK 状态语义或派发信封定义 | 接口 conformance 未就绪时用固定 fixture，不接真实派发 |
| **SP7b（SP4 自交）** | CAP 最终 schema 未设计 | 本件须交 CAP schema/migration/cutover/rollback 五列合同 | SP4 实现批次履约，不拿 SP7a PASS 代签 |

冻结证据与消息状态分层：设计可在 seam 明确时推进；实现开工门必须有可取 verdict/接口证据，不以本合同代填。

### 0.4 已上报待裁项〔本合同不自行取舍〕

| # | 事项 | 本合同的临时处置 |
|---|---|---|
| 1 | **SP8 未设计而 §0.3 列其为前置** | 按 §0.3 表 SP8 行处理：设计期只定 approval_ref 消费侧，平台级晋升在实现期 fail-closed；若业主裁定改为「等 SP8」，本合同 bump 重走三票 |
| 2 | **SpecDoc §15.4 / §15.5 物理位置落在 §16 与 §16.1 之后** | 只登记不修改（冻结窗内不擅改上游）：本件用单区间取法 ＋ 两条硬断言 ＋ 一条陷阱反测把它挡成机器可判，见 §0.2；SpecDoc 侧是否调序交执笔方与业主 |
| 3 | **§14.5a 统一 PDP/PEP 与 §22/§23 授权语义的边界** | 本件只定义 MCP 运行态查询的 **PEP 接入点、判据来源与拒绝语义**；PDP 策略与授权权力归 SP1/SP8，本件不定义策略内容 |

## 1. 六个必备面

### ① 系统对接

SP4 设计 `CAP` 域的 skill / MCP 资产、判型、准入、分发、装卸、生命周期与自我进化边界，并细化五个既有接口：

- `CAP-CMD-RegisterAsset`；
- `CAP-CMD-RequestPromotion`；
- `CAP-CMD-Distribute`；
- `CAP-CMD-AcknowledgeInstall`；
- `CAP-CMD-Uninstall`。

两个既有域事件 `EV-CAP-DistributionRequested`、`EV-CAP-InstallAcknowledged` 属本域产出。
不新增架构接口 ID 或事件 ID。CAP 消费 AccessCtx、GOV 批准与规则版本、KNO 的 domain/visibility 与条目 ref、受控 Repository；
向 AGT/MSG 提供分发与 ACK 面，向 WRK 提供可用资产查询。

CAP 不拥有 principal、批准权、知识内容、Agent runtime、企业库路由与界面内部模型。

### ② 运行约束

- 判型三判据分别可判：skill vs 机制、skill vs MCP（两维）、动词 × 资源类型；归属分级按 §14.3a；
- 运行态不固化：易变项运行时取，经受限 MCP 查询，且受统一 PDP/PEP 约束；
- 分发分界在**辖域**不在层级高低；跨人扩散只有一条合法路径；push/pull 形态按资产类型分判；
- 晋升与创建解耦；三件事（候选触发 / 晋升 / 回写）分开判，不得混判；
- 自我进化只允许提案不允许批准；写入审批在平台级与租户级默认开启；
- immutable 名单内资产 Agent 连提案都不能提；名单本身变更须重两签；
- 装与卸必须对称；**紧急路径按 capability effect 判，不按动作名判**；无 fail-closed fallback 的停用不合法；
- 卸载留 tombstone，重装同版本形成新的 `installation_generation`，迟到 ACK/旧 delivery 不得复活已卸载实例；
- skill 目录常驻上下文成本设硬预算，采渐进披露。

### ③ 预计数据

只定义测量出口，不编造规模：资产数按类型/归属域、版本数、分发目标数、install/uninstall/ACK 数与时延、
迟到 ACK 与 fencing 丢弃数、晋升提案/批准/拒绝数、immutable 拦截数、紧急路径使用数与兜底记录、
运行态查询量与拒绝数、常驻上下文字节与预算占用。每项须带依据与复核点，无实测写 `unknown`。

### ④ 环境要求

- 设计只读冻结件，不接生产、不装真实 skill/MCP、不跑真实 Agent、不建库；
- 不新增外部依赖；复用既有 repository/outbox/audit seam 与 Go 标准库；
- 实现须在 SP1 AccessCtx 与 SP7a 受控 transaction 内；缺任一前置 fail-closed；
- fixture 不含 secret、真实企业资产内容、真实拓扑或个人可识别事故。

### ⑤ 设计约束

1. **范围不扩**：仅设计 §14–§16；身份、授权策略、知识内容、Agent runtime、界面只定义 seam。
2. **复用优先**：复用 KNO 的四维与晋升/装载语义、WRK 的 generation/幂等口径、既有 outbox/audit，不另造平行机制。
3. **不自造权力**：批准权来自 GOV/SP8 的 `approval_ref`；角色字符串与 Agent 身份不授予任何权力。
4. **按 effect 判不按名判**：任何动作是否走紧急路径，只看「执行后是否存在任一主体的权限或可达范围被扩大」。
5. **对称性**：install/uninstall 同级同期审批；分发通道必须同时提供装与卸。
6. **代际优先于版本号**：幂等与复活判定由 `installation_generation` 保证，不由版本号保证。
7. **immutable 不可协商**：安全/合规/平台域基线类资产 Agent 不得提案；名单变更走重两签。
8. **运行态不固化**：运行态查询结果不入知识库，不灌上下文；只保留 typed 查询定义。
9. **不新增依赖**：不引入新数据库、新消息中间件、新架构接口或事件 ID。
10. **SP7b 随本域交付**：本件定义 CAP schema/migration/cutover/rollback 合同。

### ⑥ 输入来源

组合冻结锚：SpecDoc `4220195`；其余 `6a98629`。

| 输入 | sha256 | 用途 |
|---|---|---|
| `docs/dingwei-platform-specdoc.md` @ `4220195` | `66df042ec49e6c7345e5c5cdbe1cf4e7f5c3bbdc94a1c671bb35c588532e8e0f` | v3.73 §14–§16、owner_sp 含 SP4 的附录 E |
| `docs/dingwei-platform-architecture.md` | `0a2288c7b9e245eded06944cd063a6d0d458c6b1f45a3a9534d29817b6c58fdc` | CAP 边界、5 接口、2 事件、数据与原型入口 |
| `docs/dingwei-prototype.html` | `eeb71a5a77a53990569e08f9745165903bcf9e34cf6ce0a5047824167e1df4b8` | skill/MCP 入口 `s5` 的交互输入 |
| `docs/dingwei-sp0-design.md` | `f635b96e6c3574319d2e8df7e6a1c41e9693a66d7581504df60a6801531b8bec` | 规则版本与评审 seam 的冻结输入 |
| `docs/dingwei-sp1-design.md` | `8fafe6c5bac5595eaa771543dfc4199ba3670081786f1769f39833dc616e27bf` | AccessCtx/企业边界的冻结输入 |
| `docs/dingwei-sp2-design.md` | `37d2c17cc0761914aae91abd20273746231d894c50163cb89b8123cc03d76a86` | generation/幂等/派发 seam 的已收口输入 |
| `docs/dingwei-sp3-design.md` | `e6317b5c7257eb86b405d355b3bbcdb72751eb8c677fa2f388891c1d307b1e97` | 四维、晋升与装载语义的已收口输入 |
| 冻结代码 | 同 `6a98629` | 只证现状差距与可复用形状，不决定目标语义 |

## 2. 必须交付

| ID | 交付块 | 最低内容 |
|---|---|---|
| `D-SP4-01` | 范围与域边界 | CAP 所有/不所有、上下游、fail-closed、权威事实来源 |
| `D-SP4-02` | 资产与版本模型 | asset/version/归属四维/immutable 标记/预算字段/审计载体 |
| `D-SP4-03` | 判型三判据 | skill vs 机制、skill vs MCP 两维、动词×资源、归属分级 |
| `D-SP4-04` | 运行态查询与 PEP | 运行态不固化、typed 查询定义、PEP 接入点与拒绝语义 |
| `D-SP4-05` | MCP 准入三层 | 三层判据、证据、拒绝路径与复核点 |
| `D-SP4-06` | 分发与辖域 | 辖域判据、push/pull 形态、三类资产、D2 口径、上下文预算与渐进披露 |
| `D-SP4-07` | 流通与跨企业 | 沉淀面流通、跨企业交流边界、跨人扩散唯一合法路径 |
| `D-SP4-08` | 生命周期与晋升 | 三件事分判、候选触发、晋升与创建解耦、`approval_ref` 消费侧 |
| `D-SP4-09` | 自我进化与回写 | 提案不批准、暂存审批、回写纪律、SYNC IMPACT REPORT |
| `D-SP4-10` | immutable 与装卸对称 | 名单与重两签、对称审批、capability-effect 判据、紧急原语与 fallback |
| `D-SP4-11` | 接口与事件合同 | 5 接口 + 2 事件逐项 schema/effect/auth/idempotency/generation/tx/error/audit；tombstone 与 `installation_generation` fencing |
| `D-SP4-12` | 复用、CAP 数据与追踪验收 | 现状 retain/adapt/rewrite/new/retire + conformance；CAP schema 与 SP7b 五列；§14–§16、E、接口、事件、原型入口的覆盖与证据出口 |

正文须提供 `COVERAGE-SP4-BEGIN/END` 严格两列表格；checker 只从块内第二列真实 `D-SP4-*` 取实检集合。
块外提及、块内散文、空落点与「见全文」不计。预期集合从组合冻结锚机器派生，输出预期/实检/未知；
块缺失、冻结输入漂移或任一预期集合为空均 exit 2。

正文另须提供 `ACCEPTANCE-SP4-BEGIN/END` 四列表，每条验收独立写受检范围、判据与判不了什么。

## 3. 明确不交付

- SP0–SP3、SP5–SP8 的详细设计或实现；
- 身份/授权策略内容、批准权本身、§23–§24 的治理设计；
- 实际代码、DDL/migration、真实资产导入、真实分发与生产运行结论；
- 新数据库、新依赖、新架构接口 ID 或新事件 ID；
- Agent runtime 内部模型、界面内部状态与 skill 内容本身的质量判断。

## 4. 验收合同

<!-- ACCEPTANCE-SP4-BEGIN -->

| ID | 受检范围 | 判据 | 判不了什么 |
|---|---|---|---|
| `AC-SP4-01` | checker 派生的 §14–§16 heading | COVERAGE 逐项 D 落点，未知 0 | 落点语义正确性 |
| `AC-SP4-02` | owner_sp 含 SP4 的附录 E 行 | 逐项语义落点，未知 0 | 人工判据是否专业正确 |
| `AC-SP4-03` | 架构 §6 CAP 接口 | 5 接口逐项落点，无 RT/EV 跨表污染 | 字段语义充分性 |
| `AC-SP4-04` | 架构 §4.2 owner=CAP 事件 | 2 事件逐项落点；新增事件 ID 0 | 消费方实现状态 |
| `AC-SP4-05` | 架构 §13 主责域含 CAP 的原型入口 | 逐项落点且冻结原型中存在 | UI 可用性与实现状态 |
| `AC-SP4-06` | 判型三判据与归属分级 | 三判据各自可判、互不替代；分级按 §14.3a | 某具体资产该判哪类 |
| `AC-SP4-07` | 准入三层与晋升审批 seam | 消费 `approval_ref`；CAP 不自造批准权；平台级晋升缺 SP8 定义时 fail-closed | 批准决定是否恰当 |
| `AC-SP4-08` | 分发、辖域与流通 | 辖域判据、push/pull 形态、跨人扩散唯一路径、跨企业边界齐 | 分发时延与容量 |
| `AC-SP4-09` | 生命周期三分与自我进化 | 三件事分判、晋升与创建解耦、提案不批准、暂存审批与回写纪律齐 | 提案内容质量 |
| `AC-SP4-10` | immutable 与紧急路径 | 名单变更重两签；**按 capability effect 判不按动作名**；无 fallback 的停用判非法 | 兜底机制实际有效性 |
| `AC-SP4-11` | 装卸对称与 fencing | 装卸同级审批；tombstone；重装形成新 `installation_generation`；迟到 ACK 不复活 | 真实并发下的实现正确性 |
| `AC-SP4-12` | 接口/事件合同与 SP7b 五列 | auth/idempotency/generation/tx/error/audit 齐；五列转交齐；设计 PASS 不升级实现 PASS | 实际迁移与运行结果 |
| `AC-E05` | 同一正文锚上的三份独立 verdict | 三位评审者均 PASS（3/3）；缺任一票即未过 | 票的专业质量 |

<!-- ACCEPTANCE-SP4-END -->

### 4.1 评审协议

- verdict 词表仅 `PASS/NO/INCONCLUSIVE`；任一阻断未闭即 NO；
- **最多 4 轮**：每轮全文一次性给全量阻断，作者一次性整改，不逐条往返；
- 第 3 轮仍有范围/语义争议交 coordinator/业主裁定，第 4 轮只签最终 verdict；
- 三位评审者须对同一 commit 独立 PASS（3/3），不以多数票替代；
- 只判本轮阻断，不翻旧账；旧问题仅在直接破坏本设计时入阻断。

## 5. 冲突与变更纪律

| 冲突 | 处置 |
|---|---|
| SpecDoc 自身矛盾 | 登记冲突双方的精确章节、两种后果与建议，交 coordinator/业主，不自行取舍 |
| SpecDoc 与目标架构冲突 | 登记双方精确章节与后果，交裁，不自设优先级 |
| **SpecDoc §15.4 / §15.5 排版位置落在 §16 之后** | 已登记（§0.4 #2）：单区间取法为判据，checker 硬断言 ＋ 陷阱反测；不改 SpecDoc、不因排版顺序改归属 |
| **前置 SP8 未设计** | 已登记（§0.4 #1）：只消费 `approval_ref`，不自造批准权；平台级晋升在实现期 fail-closed，不以 CAP 本地审批替代 |
| 与已收口 SP1/SP2/SP3 seam 对撞 | 停止该路径，登记接口差异并交裁；不改已收口件，也不单方给上游产物加必填字段 |
| 目标与现状代码冲突 | 记 retain/adapt/rewrite 差距；旧实现不升级为目标规则 |
| 合同范围或机器集合变化 | bump 合同并重走三方评审，未过不写正文 |

## 6. 开工门

- [ ] sol 合同 PASS；
- [ ] opus50 合同 PASS；
- [ ] deepseek 合同 PASS；
- [ ] `specs/sp4/contract-check.sh` PASS；
- [ ] `specs/sp4/check.sh --selftest` PASS（正文未创建前普通 check 应 exit 2，不算通过）；
- [ ] coordinator 登记组合冻结锚与合同锚，并回执 §0.4 三项待裁的裁定结果。
