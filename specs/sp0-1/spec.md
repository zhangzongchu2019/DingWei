# DingWei SP0-1 设计合同

> 本件先冻结 SP0＋SP1 合并工作包的设计范围、输入、交付物和验收口径；合同通过前不写方案正文。
> `SP0-1` 是业主为本轮指定的 **SP0 与 SP1 合并包 ID**，不是「SP0 的第一个包」，也不是 SpecDoc §1。

## 0. 合同头块

| 项 | 值 |
|---|---|
| **Spec ID** | `dingwei-sp0-1-design` |
| **合同版本** | v1.1 |
| **状态** | `draft-for-review` |
| **执笔** | `zzc-chatgpt56soltchangshark-2642` |
| **评审** | `zzc-opus50tchangshark-2642`、`zzc-deepseekv4pro08130ca16c13-2642` |
| **冻结施工锚** | `46b79ab0022b0686a23455f328c9316d7f87281f` |
| **权威范围** | SpecDoc `SP0`（§1–§4、附录 A）＋ `SP1`（§21、§22.1、§22.3、§22.4、§7.1 P1）及业主点名的四项 SP1 地基 |
| **目标件** | `docs/dingwei-sp0-1-design.md` |
| **分级** | **L3**（合并包取最高级：SP1 安全面＝是）；全文评审 PASS 前不可施工 |

### 0.1 `SP0-1` 的唯一解释

业主已裁：`SP0-1 = SP0 + SP1`，是两个既有工作包合并推进，不创造第三个产品范围：

1. SP0 覆盖 SpecDoc §1–§4、附录 A；
2. SP1 覆盖 §21、§22.1、§22.3、§22.4、§7.1 P1，并包含业主点名的四项地基：
   `agent → principal` 绑定、`TenantCtx → AccessCtx`、一企业一 PG 库、RLS 隔离合同；
3. 产出设计，不实现代码、schema、部署或运维配置；
4. `SP0-1` 不写回 SpecDoc 的子 spec 枚举，不改变 `SP0 → SP1` 的语义依赖，只在本轮共同设计；
5. 不把 SP7a 的仓储实现、DDL/migration 或真实 PG 验证并入本包；SP1 定身份/隔离合同，SP7a 履约。

### 0.2 SYNC IMPACT REPORT（v1.0 → v1.1）

- 按业主裁定把范围从 SP0 单包改为 SP0＋SP1 合并包，撤回 v1.0 的错误解释。
- 分级由 L2 升为 L3；增加身份链、企业库映射、RLS 合同、命名迁移与 Agent 绑定的交付/验收项。
- 明确 SP1 与 SP7a 的边界：本包定义可信上下文和隔离合同，不实现仓储、DDL、迁移或真实 PG 验证。

## 1. 六个必备面

### ① 系统对接

本设计承接目标架构 `GOV`（SP0）与 `IDN`（SP1）的责任，至少定义：

- spec 元数据、版本、父子关系、生命周期与输入锚；
- 属性填写、规则集版本、分级复算与 gate-set 输出；
- 准入硬门、参考项、人审触发、非语义修订与依赖闭包；
- reviewer-owned checklist 的生成、持有、勾选与 gate 消费；
- 待完善池、认领、正文变更退审和预计数据偏离复核；
- 附录 A 调参项的权威载体、版本化、读取与变更边界。

SP1 同批定义：

- credential/connection → `SessionAuthRef` → envelope → Receiver 的 P1 主体传播；
- `agent → principal` 仅作溯源绑定，Agent 永不进入授权或门判定；
- enterprise registry、principal、channel/session binding 与 `AccessCtx`；
- principal 到企业数据库的 ops 注册映射：查不到/多结果均拒绝，禁止运行时自动发现或按域名猜库；
- 一企业一 PG 库、平台库独立的逻辑拓扑合同；
- 人层 RLS 的角色/上下文/policy/负测合同；
- `TenantCtx → AccessCtx` 的兼容迁移与旧名棘轮。

对外/跨域 seam 只复用目标架构既有接口方向：`GOV-CMD-SubmitSpec`、`GOV-QRY-ResolveGateSet`、
`IDN-CMD-AuthenticateChannel`、`IDN-CMD-OpenSession`、`IDN-CMD-BindAgent`、
`IDN-QRY-ResolveAccess`、`IDN-QRY-ResolveDataScope` 与 `MSG-CMD-AcceptEnvelope`，
以及向 WRK 提供的不可变 spec/gate-set ref。若设计发现确需新增架构级接口，先登记为冲突，
不得在 SP0-1 内悄悄扩展已 PASS 的接口目录。

### ② 运行约束

- 规则求值须由 `attributes + rules_version` 可复算；缺版本、规则或输入时 fail-closed。
- 硬门失败返回完整缺项，不进入人审队列。
- 机器通过不能替代人审；reviewer 与 implementer 权限分离。
- 同一 spec version 的输入、属性、规则版本、gate-set 和 checklist 快照须可关联取证。
- 并发提交、重复 request、迟到评审与正文变更均须给代际/幂等行为，不以最后写入者覆盖历史。
- 本轮不编造吞吐、延迟或存量数；影响容量的未知量进入待测表。
- 取连接后第一条事务语句设置 `SET LOCAL` 的 RLS context；缺 context、错企业库、错 principal 均失败关闭。
- `dw_owner`/superuser 不下放企业侧；企业级 ops 至多 `dw_app`，无 DDL、非属主、无 BYPASSRLS。
- 绑定未闭前 `agent` 列不得作审计交叉证据；任何时期 Agent 都不得进入授权/门判定链。

### ③ 预计数据

本轮只定义数据类别和测量出口，不手写业务规模：

| 数据类别 | 设计须给出的量 | 复核点 |
|---|---|---|
| spec / version | 总数、活跃版本、父子深度、平均输入锚数 | SP0 实现前基线扫描 |
| rule set | 版本数、规则行数、求值频率 | 规则迁移设计完成时 |
| checklist | 每 spec 项数、未勾项、证据大小 | reviewer 流程试运行 |
| review / decision | 队列长度、重试、代际冲突、平均闭合时长 | 用例评审与交付验证 |
| pending pool | 未认领、已认领、退审、stale 数 | 每两周复核机制验收 |
| tuning profile | 参数项数、版本、变更频率 | 附录 A 首次落库/载入前 |
| enterprise / principal | 企业、principal、渠道绑定、跨企业自然人数 | SP1 实现前平台注册表基线 |
| agent binding | 已绑定、未绑定、重绑、迟到代际冲突 | P1 负测与交付验证 |
| enterprise DB mapping | 0/1/多结果、错库拒绝、连接池复用 | SP7a 联调前 |
| AccessCtx migration | prod/test/comment 旧名数量与调用方分布 | 每个迁移提交 |

任何具体数字都须带依据与复核点；无依据时写 `unknown`，不得据此作容量结论。

### ④ 环境要求

- 设计只读冻结锚内 SpecDoc、目标架构、原型与代码现状，不接生产、不建库、不改部署。
- 不新增外部依赖；DMN 只沿用 SpecDoc 已裁的决策表，不引入执行引擎。
- secret、真实企业数据、主机地址、账号和凭据不进入正文或夹具。
- 后续实现环境至少区分 dev/test、preprod、prod；规则/参数版本不得由请求方自报。
- 企业数据库名、DSN 与 principal 映射仅来自平台 ops 维护的注册表；请求字段和 Agent 自述均不可信。

### ⑤ 设计约束

1. **范围不扩**：只设计 SP0＋SP1 权威范围；SP2–SP8 只定义依赖 seam。
2. **复用优先**：复用现有 Spec Kit 目录、现有 lint/gate 形态、目标架构 GOV 接口；不另造平行工具链。
3. **不新增依赖**：不引入工作流引擎、规则执行引擎、消息中间件或新数据库。
4. **单一权威**：SpecDoc 仍是规则语义权威；设计件只定义实现合同，不复述第二份产品规则表。
5. **权限分离**：作者不能改 reviewer checkbox，implementer 不能签自己的 gate PASS。
6. **空集不通过**：缺被检对象、规则集、输入锚、checklist 或 evidence 时返回 UNKNOWN/FAIL，不能折叠为 PASS。
7. **五态分开**：draft/review/effective/expired 等生命周期与评审 verdict、执行状态不得塞进同一枚举。
8. **附录 A 不扩权**：调参只能改声明为可调的数值；改变结构、授权、状态语义或默认约束属于语义变更。
9. **旧代码只证现状**：不得以现有包/表缺什么，反推目标产品规则必须怎样。
10. **不改已 PASS 架构**：发现架构冲突即登记并交裁，不在 SP0-1 里私自改写目标域或接口目录。
11. **主体唯一**：授权输入只认经验证 principal；Agent 只作 provenance，不能提升 assurance、scope 或 capability。
12. **企业与人分轴**：enterprise 决定数据库，principal 决定 owner/actor；不得再用单一 tenant 字段混装两层。
13. **RLS 不冒充全覆盖**：RLS 约束企业库在线访问；备份、副本、复制槽和平台特权面另走介质加密、审批与审计。
14. **命名迁移不等于语义完成**：`TenantCtx` 归零是必要条件，不足以证明 `AccessCtx` 双轴语义实现。

### ⑥ 输入来源

冻结输入锚：`46b79ab0022b0686a23455f328c9316d7f87281f`

| 输入 | 版本 / 指纹 | 用途 |
|---|---|---|
| `docs/dingwei-platform-specdoc.md` | v3.69 · sha256 `8b0b20b97af85bf1c42c1b8dd5a7c94a0e73d1de3100666c5acb2370ae56c61e` | SP0/SP1 产品规则、附录 A、附录 E owner 行 |
| `docs/dingwei-platform-architecture.md` | v1.3 · sha256 `0a2288c7b9e245eded06944cd063a6d0d458c6b1f45a3a9534d29817b6c58fdc` | GOV/IDN/MSG 边界、接口、数据与验收方向 |
| `docs/dingwei-prototype.html` | v1.47 · sha256 `eeb71a5a77a53990569e08f9745165903bcf9e34cf6ce0a5047824167e1df4b8` | spec 治理与身份/企业管理相关可达入口 |
| 冻结锚内代码与 `specs/` | 同 commit | 只证现状与复用点，不决定目标语义 |

## 2. 必须交付

目标设计件须包含以下稳定章节：

| ID | 交付块 | 最低内容 |
|---|---|---|
| `D-SP0-01` | 范围与术语 | SP0-1 定位、权威词汇、与 SP8/WRK/IDN/PLT 边界 |
| `D-SP0-02` | 领域模型 | spec/version/input anchor/attribute/rule set/checklist/review/pending/tuning 对象与不变量 |
| `D-SP0-03` | 生命周期 | spec、review、checklist、pending、rule set、tuning 各自状态机与代际 |
| `D-SP0-04` | 接口 | SubmitSpec、ResolveGateSet 细化及内部 ports/events；不得绕过既有架构边界 |
| `D-SP0-05` | 数据设计 | 权威表/字段/唯一键/引用/不可变记录/迁移责任；不写可直接执行的生产 DDL |
| `D-SP0-06` | 流程 | 准入、人审、非语义修订、依赖闭包、认领退审、调参变更时序 |
| `D-SP0-07` | 权限与审计 | author/reviewer/implementer/owner 权限矩阵与最小审计事件 |
| `D-SP0-08` | 失败与并发 | 幂等、代际、迟到、重复、部分失败、UNKNOWN/FAIL 语义 |
| `D-SP0-09` | 现状复用映射 | 现有 specs/scripts/代码到目标设计的 retain/adapt/new/retire，逐项有依据 |
| `D-SP0-10` | 验收矩阵 | SpecDoc 条款、附录 E 的 SP0 行、原型入口到用例/负测/证据出口 |
| `D-SP0-11` | 未决与转交 | 运行样本、跨 SP 依赖、需裁事项及 owner/验收门 |

| ID | SP1 交付块 | 最低内容 |
|---|---|---|
| `D-SP1-01` | 主体传播 | credential/connection → SessionAuthRef → envelope → Receiver；伪造、缺失、吊销时失败点 |
| `D-SP1-02` | 身份模型 | enterprise、principal、channel/session binding、agent provenance binding 的权威关系与唯一键 |
| `D-SP1-03` | `AccessCtx` | `Org()`/`Principal()` 双轴、不可互替、签发/引用/吊销/过期与最小字段 |
| `D-SP1-04` | 企业库映射 | ops 注册表、opaque database key、0/1/多结果语义、一企业一库与平台库分离 |
| `D-SP1-05` | RLS 合同 | 三 role、FORCE RLS、SET LOCAL、缺 context 默认拒、WITH CHECK、负测与平台特权残余 |
| `D-SP1-06` | Agent 绑定 | bind/rebind/generation/audit；绑定前禁用 agent 审计交叉证据，长期禁入授权/门判定 |
| `D-SP1-07` | 命名迁移 | TenantCtx/PendingTenantCtx/NewTenantCtx → AccessCtx 的兼容阶段、棘轮、禁止新增与完成判据 |
| `D-SP1-08` | SP7a 转交 | 仓储/RLS/迁移被测对象、输入、期望、证据形态、验收门；设计合同与实现责任分开 |

同时交付 `specs/sp0-1/check.sh`，机器派生并输出预期/实检/未知集合；未知非空不得宣称覆盖完成。

## 3. 明确不交付

- SP2–SP8 的详细设计或实现（仅保留本包所需依赖 seam 与 SP7a 转交合同）；
- 代码、DDL、migration、部署、CI/CODEOWNERS、生产配置；
- 新的产品规则、属性、风险档、角色、审批权或调参项；
- 对目标架构接口目录的扩充；
- 运行性能结论、真实 PG 证据或生产验收结果。

## 4. 验收合同

### 4.1 结构验收

| ID | 判据 |
|---|---|
| `AC-SP0-01` | `D-SP0-01`–`D-SP0-11` 全部存在且唯一 |
| `AC-SP0-02` | SpecDoc §1–§4 与附录 A 的机器派生预期集合均有设计落点；未知集合为 0 |
| `AC-SP0-03` | 附录 E 中 `owner_sp` 含 SP0 的行均有验收落点；未知集合为 0 |
| `AC-SP0-04` | 原型中与 spec 查阅、准入、评审、待决、调参相关的可达入口均已分类 |
| `AC-SP0-05` | 新增外部依赖数为 0；目标架构接口目录新增 ID 数为 0 |

### 4.2 语义验收

| ID | 判据 |
|---|---|
| `AC-SP0-06` | author/reviewer/implementer/owner 四类权限不互相代签；反例逐条有拒绝点 |
| `AC-SP0-07` | 规则复算固定 `attributes + rules_version`，同输入输出一致，缺任一项 fail-closed |
| `AC-SP0-08` | spec 生命周期、评审 verdict、checklist 状态、pending 状态分别建模，不共享含糊 status |
| `AC-SP0-09` | 非语义修订按依赖闭包判；闭包建不全时转语义修订，不放行 |
| `AC-SP0-10` | 所有恒零/全称断言给受检范围；空集、解析失败和未知对象不报 PASS |
| `AC-SP0-11` | 附录 A 数值变更不能改变结构、权限和语义；越界变更退回语义评审 |

### 4.3 SP1 验收

| ID | 判据 |
|---|---|
| `AC-SP1-01` | `D-SP1-01`–`D-SP1-08` 全部存在且唯一；SP1 权威章节与 owner_sp=SP1 的附录 E 行未知集合为 0 |
| `AC-SP1-02` | 连接层、信封与 Receiver 的 principal 同源；伪造/缺主体/吊销后写入与状态观测均 fail-closed |
| `AC-SP1-03` | enterprise 仅由可信 principal 经 ops 注册映射得出；请求自带 org/tenant、域名猜测、0/多映射均拒绝 |
| `AC-SP1-04` | 一企业一 PG 库、平台库独立；企业库选择发生在 SQL/selector 前，错库不得以成功空集伪装 |
| `AC-SP1-05` | `dw_owner`/`dw_app`/`dw_migrate` 分离，runtime 非属主/非 superuser/无 BYPASSRLS，表 FORCE RLS |
| `AC-SP1-06` | 每事务取连接后第一条设置 principal context；缺失、池复用残留、谓词错、WITH CHECK 漏失均有负测合同 |
| `AC-SP1-07` | Agent 绑定不提升 principal/assurance/capability；绑定前审计交叉证据拒绝，任何时期授权/门输入拒绝 |
| `AC-SP1-08` | `TenantCtx` 三类旧标识按 prod/test/comment 独立棘轮只降不升；归零后仍须双轴语义负测 |
| `AC-SP1-09` | 备份、副本、复制槽和平台特权面明确不由 RLS 覆盖，另列加密、审批、时限与审计转交 |
| `AC-SP1-10` | SP7a 转交逐项具备被测对象/输入/期望/证据形态/验收门，不以“设计已定”冒充实现 PASS |

### 4.4 证据验收

- 每条验收项给可复跑命令或人工判据、预期值、证据锚；
- 脚本通过只作结构旁证，不替代两位评审者的语义判断；
- 评审结论使用 PASS / NO / INCONCLUSIVE；任一阻断未闭即 NO；
- 最多 6 轮，按“全文评审 → 一次性整改 → 全文复审”收敛，不逐点往返。

## 5. 冲突与变更纪律

| 情形 | 处置 |
|---|---|
| SpecDoc 自身矛盾 | 登记双方章节、后果与建议，交 coordinator/业主裁，不自行取舍 |
| SpecDoc 与目标架构矛盾 | 登记精确锚；两件均不由本任务私改 |
| 原型与 SpecDoc/架构矛盾 | 以冲突项进入评审，不凭“界面已画”推翻权威规则 |
| 代码现状与目标冲突 | 记迁移差距，不把现状提升为目标约束 |
| 合同范围需变更 | 先 bump 合同版本并重新双评审，未过不得继续正文 |

## 6. 开工门

以下条件同时满足后才允许写 `docs/dingwei-sp0-1-design.md`：

- [ ] opus50 合同 PASS；
- [ ] deepseek 合同 PASS；
- [ ] `specs/sp0-1/contract-check.sh` 通过；
- [ ] coordinator 记录施工锚与合同锚。
