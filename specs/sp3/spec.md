# DingWei SP3 上下文与知识库设计合同

> 本件冻结 SP3 的范围、前置、交付物与验收口径；合同 3/3 PASS 且 coordinator 登记前不写正文。

## 0. 合同头块

| 项 | 值 |
|---|---|
| **Spec ID** | `dingwei-sp3-design` |
| **合同版本** | v1.2 |
| **状态** | `draft-for-review` |
| **执笔** | `zzc-chatgpt56soltchangshark-2642` |
| **评审** | `zzc-opus50tchangshark-2642`、`zzc-deepseekv4pro08130ca16c13-2642`、`zzc-opus50tczc2000-2642` |
| **冻结施工锚** | 组合冻结：SpecDoc `4fb34f2`；架构/原型/SP0–SP2/代码基线 `53e4a2aed1569eef91e4048e7da2a1cd32db3ec2` |
| **权威范围** | SpecDoc §10–§13；owner_sp=SP3 的附录 E 行 |
| **目标架构域** | `KNO`；消费 `IDN/PLT/WRK`，向 `WRK/CAP/COL/AGT` 提供受控知识与上下文 snapshot |
| **目标件** | `docs/dingwei-sp3-design.md` |
| **分级** | L2 |

<!-- EVENT-SET-SP3: NOT_APPLICABLE -->

**事件集合处置**：本轮不设 event 面。冻结架构 §4.2 的 owner=KNO 事件为 0，`EV-KNO-*` 亦为 0；
KNO 只消费既有 WRK dispatch 事件，并经四个 KNO command/query 与 repository outbox seam 协作。
若未来需要 KNO 作为事件 producer，须先修改目标架构 §4.2、bump 本合同并重新评审，不能在正文私造事件 ID。

**v1.1 同步**：派发快照字段全集的唯一权威源钉为 SpecDoc §11.6 表；SP2 是引用侧而非定义源。

**v1.2 同步**：业主已裁 §11.6 按 SP2 更新并补 §11.6a FAIL evidence；SpecDoc 输入重锚 v3.70，其他输入不回退。

### 0.1 范围判定

SpecDoc §0.3 对 SP3 定义清晰且与目标架构一致：

1. 产品范围为 §10–§13；
2. 前置为 SP1、SP7a；
3. KNO schema migration/cutover 由 SP3 作为 SP7b 自交；
4. 横切附录 E 只认领 owner_sp 含 SP3 的行；
5. 不设计 SP4 skill/MCP、SP5 帖子流或 SP6 UI 内部模型，只定义 seam。

### 0.2 前置依赖处置

| 前置/输入 | 冻结锚可见事实 | SP3 设计期处理 | SP3 实现期开工门 |
|---|---|---|---|
| **SP1** | 冻结 SP1 设计件头仍为 `draft，待双评审`；coordinator 已在本轮外告知其收口 | 只消费 AccessCtx/企业库/Agent provenance 已定义 seam；不把消息 verdict 倒签进冻结件 | SP1 verdict 锚、AccessCtx 与一企业一库/RLS 负测未落账前，KNO 仅做 contract test，不上线 query/ingest |
| **SP7a** | 受控仓储 API、migration runner、访问抽象尚未在本分支交付 | KNO schema 与 repository port 可并行设计；不得自建 DB 句柄或应用层补 tenant 过滤 | 受控 transaction、先过滤后检索、迁移 runner conformance 未过，不得 cutover |
| **SP7b（SP3 自交）** | KNO 最终 schema 未设计，不能由统一 SP7 先做 | 本件须交 KNO schema/migration/cutover/rollback 五列合同 | SP3 实现批次履约，不推回统一 SP7 |
| **SP2（协作输入，非 §0.3 前置）** | SP2 正文已 3/3 PASS，锚 `53e4a2a`；其 §3.3 是 §11.6 快照的引用侧 | SP3 以 SpecDoc §11.6 表为字段全集权威源；消费 WRK task snapshot，但不采信 SP2 自列字段替代 §11.6 | SP2 与 §11.6 字段差异未裁前，用 §11.6 表逐项 fixture，不接真实派发 |

冻结证据与消息状态分层：设计可在 seam 明确时推进；实现开工门必须有可取 verdict/接口证据，不以本合同代填。

## 1. 六个必备面

### ① 系统对接

SP3 设计 `KNO` 领域的知识条目、四维元数据、三条 ingest 管道、visibility 授权、锚与生命周期、检索、
immutable context snapshot，以及四个既有接口：

- `KNO-CMD-IngestItem`；
- `KNO-QRY-Search`；
- `KNO-CMD-BuildContextSnapshot`；
- `KNO-CMD-MarkLifecycle`。

不新增架构接口 ID。KNO 消费 AccessCtx、DataScopeGrant、受控 Repository、WRK dispatch/task snapshot；
向 WRK/AGT 返回不可变 context snapshot ref，向 CAP/COL 提供过滤后的知识查询。

### ② 运行约束

- `domain_level`、`visibility`、生命周期、记忆类型四维正交，任一维不得推出另一维；
- 装载先求 domain scope ∩ visibility grant，再检索；先检索后过滤恒为 0；
- 未显式 visibility 默认 restricted；降级逐跳审批落账；引用继承取严；
- 运行态不入知识库、不灌上下文；需要时经 SP4 受限 MCP 查询；
- 每任务新 Spawn=新上下文；dispatch snapshot 不可变且字段不全即拒；
- 多锚任一失配即 suspect；不同 anchor_type 使用各自判据；连续两版本 suspect 才 archived；
- ingest 只经项目收口、策展/外部导入、人工订正三条管道；订正建 revision，不改历史。

### ③ 预计数据

只定义测量出口：各 domain/visibility/type/lifecycle 数量、anchors 数、ingest source、检索候选/过滤/命中、suspect、
snapshot 大小与构建耗时、拒绝与审计量。无实测写 unknown；不得用未知规模先证某 provider 必成瓶颈。

### ④ 环境要求

- 设计阶段只读冻结件，不接生产、不导入企业知识、不建库、不跑真实 LLM/MCP；
- 不新增依赖；复用 Go 标准库、既有 repository/outbox/audit seam；
- 实现须在 SP1 AccessCtx 与 SP7a 受控 transaction 内；缺任一前置 fail-closed；
- fixture 不含 secret、真实企业内容、真实拓扑或个人可识别事故。

### ⑤ 设计约束

1. 仅设计 §10–§13，不把 skill/MCP/帖子/界面内部实现并入 KNO。
2. 四维分别持久化与判定；符号固定 L=风险、D=domain、G=出站闸。
3. KBDoc.Meta 必有 domain_level、owner_key、visibility、type、anchors[]、version、updated_at。
4. anchors[] 是数组；code/deploy/artifact/external 分判，任一失配即 suspect。
5. query 必须经受控仓储 API 先过滤后检索；deny 不伪装成功空集。
6. D0 仅 public 可全量；D0 internal/restricted 与 D1–D3 一律检索式。
7. 固化与运行时取按「旧值是否误导」判；运行态不进 KNO。
8. context snapshot 不可变、可重灌；OriginHint 不承担正确性。
9. fsgrep 仅为现状雏形，不升级为目标 provider 或安全边界。
10. KNO SP7b 随本域交付，不以 SP7a PASS 代签 schema/cutover。

### ⑥ 输入来源

冻结锚：`53e4a2aed1569eef91e4048e7da2a1cd32db3ec2`

| 输入 | sha256 | 用途 |
|---|---|---|
| `docs/dingwei-platform-specdoc.md` @ `4fb34f2` | `e5c59a4ac3e8a7a44aa1ed647e24b9fcbb20d8ea53c789fe78e27eae49e9d7df` | v3.70 §10–§13、§11.6/§11.6a、owner_sp=SP3 附录 E |
| `docs/dingwei-platform-architecture.md` | `0a2288c7b9e245eded06944cd063a6d0d458c6b1f45a3a9534d29817b6c58fdc` | KNO 边界、4 接口、数据、迁移、原型入口 |
| `docs/dingwei-prototype.html` | `eeb71a5a77a53990569e08f9745165903bcf9e34cf6ce0a5047824167e1df4b8` | 知识库与 Agent 工作面的交互输入 |
| `docs/dingwei-sp0-design.md` | `f635b96e6c3574319d2e8df7e6a1c41e9693a66d7581504df60a6801531b8bec` | GOV 规则/评审 seam 的冻结输入 |
| `docs/dingwei-sp1-design.md` | `8fafe6c5bac5595eaa771543dfc4199ba3670081786f1769f39833dc616e27bf` | AccessCtx/企业边界的冻结输入 |
| `docs/dingwei-sp2-design.md` | `37d2c17cc0761914aae91abd20273746231d894c50163cb89b8123cc03d76a86` | dispatch/task snapshot 的已收口设计输入 |
| `internal/datad/kb.go`、`kb_test.go` | 冻结 commit | 只证现状差距与可复用测试形状，不决定目标语义 |

## 2. 必须交付

| ID | 交付块 | 最低内容 |
|---|---|---|
| `D-SP3-01` | 范围与四维模型 | KNO 所有/不所有、上下游、四维正交不变量 |
| `D-SP3-02` | KBDoc 领域模型 | item/revision/meta/anchors/visibility grant/audit/context snapshot |
| `D-SP3-03` | domain 与生命周期 | D0–D3、三条 ingest、晋升/订正、不可变历史 |
| `D-SP3-04` | visibility 与授权 | 默认 restricted、降级审批、继承取严、放行审计、负测 |
| `D-SP3-05` | 固化判型与来源 | 条目类型、四类权威源、运行时取边界、导入/链接 |
| `D-SP3-06` | 上下文装载 | 新 Spawn；以 v3.70 §11.6 派发快照表为字段全集唯一权威源；正文按表逐字段给拒绝/重灌判据，并按 §11.6a 五子字段定义 FAIL evidence；snapshot/evidence 不可变 |
| `D-SP3-07` | 锚与生命周期 | 四锚判据、多锚 OR、active/suspect/archived、reverify |
| `D-SP3-08` | 检索与排序 | 先过滤、scope∩visibility、降权、cursor、拒绝语义、审计 |
| `D-SP3-09` | 四接口合同 | schema/effect/auth/idempotency/generation/tx/error/audit |
| `D-SP3-10` | KNO 数据与 SP7b | schema/约束/index/RLS、migration/cutover/rollback 五列 |
| `D-SP3-11` | 复用与差距 | datad/kb retain/adapt/rewrite/new/retire、conformance suite |
| `D-SP3-12` | 追踪与验收 | §10–§13、E、KNO 接口、原型入口、负测与证据出口 |

正文须提供 `COVERAGE-SP3-BEGIN/END` 严格两列表格；checker 只从块内第二列真实 `D-SP3-*` 取实检集合。
块外提及、块内散文、空落点与「见全文」不计。预期集合从冻结锚机器派生，输出预期/实检/未知；
块缺失、冻结输入漂移或任一预期集合为空均 exit 2。
例外只有上方机器标记声明的 event 不适用面；checker 同时验证架构 KNO owner 事件确为 0。标记缺失或实数非 0 均 exit 2。

正文另须提供 `ACCEPTANCE-SP3-BEGIN/END` 四列表，每条验收独立写受检范围、判据与判不了什么。

## 3. 明确不交付

- SP0/SP1/SP2/SP4–SP8 的详细设计或实现；
- skill/MCP 晋升分发、帖子/通知、Agent runtime、产品 UI 内部状态；
- 实际 DDL/migration、企业数据导入、生产查询、部署与性能结论；
- 新数据库、新向量依赖、新架构接口 ID 或运行态 MCP schema；
- 把当前 fsgrep 的「query=全员」或文件目录当成目标授权模型。

## 4. 验收合同

<!-- ACCEPTANCE-SP3-BEGIN -->
| ID | 受检范围 | 判据 | 判不了什么 |
|---|---|---|---|
| `AC-SP3-01` | checker 派生的 §10–§13 heading | COVERAGE 逐项 D 落点，未知 0 | 落点语义正确性 |
| `AC-SP3-02` | owner_sp=SP3 的附录 E 行 | 逐项语义落点，未知 0 | 人工判据是否专业正确 |
| `AC-SP3-03` | 架构 §6 KNO 接口 | 4 接口逐项落点，无跨表污染 | 字段语义充分性 |
| `AC-SP3-04` | 架构 §13 主责域含 KNO 的原型入口 | 逐项落点且冻结原型入口存在 | UI 可用性与实现状态 |
| `AC-SP3-05` | 四维模型与 KBDoc.Meta | 四维不互推；字段/枚举/默认值/约束齐 | 数据是否已迁移 |
| `AC-SP3-06` | 三条 ingest 与 revision | 无旁路写入；订正不改历史 | curator 判断质量 |
| `AC-SP3-07` | visibility 与引用 | 默认 restricted、降级审批、继承取严、放行审计齐 | 定级是否恰当 |
| `AC-SP3-08` | v3.70 §11.6 字段表逐项 + §11.6a FAIL evidence 子字段逐项 | 每字段有构造/缺失拒绝/重灌判据；repro 空/不可复跑则 evidence 不成立；snapshot/evidence 不可变 | 字段齐备后诊断是否正确的人审结论 |
| `AC-SP3-09` | anchors 与生命周期 | 四锚分判、多锚 OR、连续两版本 archived | 外部替代锚质量 |
| `AC-SP3-10` | 检索全链 | 过滤先于检索、suspect 降权、deny 不伪装空集 | relevance 质量 |
| `AC-SP3-11` | 4 接口与复用映射 | auth/idempotency/generation/tx/error/audit 及正反例齐 | provider 性能 |
| `AC-SP3-12` | SP7b 五列合同与证据 | 对象/输入/期望/证据/门齐；设计 PASS 不升级实现 PASS | 实际 cutover/rollback |
| `AC-E05` | 同一正文锚三份 verdict | 三位评审者 3/3 PASS；缺任一票即未过 | verdict 专业质量 |
<!-- ACCEPTANCE-SP3-END -->

### 4.1 评审协议

- verdict 词表仅 `PASS/NO/INCONCLUSIVE`；任一阻断未闭即 NO；
- 最多 4 轮：每轮全文一次性给全量阻断，作者一次性整改，不逐条往返；
- 第 3 轮仍有产品/范围争议交 coordinator/业主，第 4 轮只签最终 verdict；
- 三位评审者须对同一 commit 独立 PASS（3/3），不以多数票替代。

## 5. 冲突与变更纪律

| 冲突 | 处置 |
|---|---|
| SpecDoc 自身矛盾 | 登记精确章节、两种后果与建议，交 coordinator/业主，不自行取舍 |
| SpecDoc 与目标架构冲突 | 登记双方锚与影响，交裁，不设私有优先级 |
| 与 SP1/SP2 seam 对撞 | 停止该路径，登记接口差异；不改已收口件 |
| SpecDoc §11.6 与 SP2 §3.3 snapshot 字段不一致 | ✅ 业主已裁并在 v3.70 落地：§11.6 承认 SP2 所需字段且仍为唯一权威表；SP3 按新表与 §11.6a 设计，SP2 为引用侧 |
| 目标与 datad/kb 现状冲突 | 记 retain/adapt/rewrite 差距；旧实现不升级为目标规则 |
| 合同范围或机器集合变化 | bump 合同并重走三方评审，未过不写正文 |

## 6. 开工门

- [ ] opus50 合同 PASS；
- [ ] deepseek 合同 PASS；
- [ ] opus50tczc2000 合同 PASS；
- [ ] `specs/sp3/contract-check.sh` PASS；
- [ ] `specs/sp3/check.sh --selftest` PASS；
- [ ] coordinator 登记冻结施工锚与合同锚。
