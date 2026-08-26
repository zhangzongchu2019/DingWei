# DingWei SP4 skill / MCP 体系设计合同

> 本件冻结 SP4 的范围、前置、交付物与验收口径；合同 3/3 PASS 且 coordinator 登记前不写正文。

## 0. 合同头块

| 项 | 值 |
|---|---|
| **Spec ID** | `dingwei-sp4-design` |
| **合同版本** | v1.6 |
| **状态** | `draft-for-review` |
| **执笔** | `zzc-opus50tczc2000-2642` |
| **评审** | `zzc-chatgpt56soltchangshark-2642`、`zzc-opus50tchangshark-2642`、`zzc-deepseekv4pro08130ca16c13-2642` |
| **冻结施工锚** | 组合冻结：SpecDoc `4220195`（v3.73）；**SP8 设计件 `970effe`**；架构/原型/SP0–SP3/代码基线 `6a98629` |
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
分段取会**静悄悄丢掉这两节**（实测：按章取 §15 段只得 7 个 heading，缺 `§15.4`、`§15.5`）。
`specs/sp4/check.sh` 已把「派生集合必须含 `SRC:§15.4` 与 `SRC:§15.5`」写成硬断言（缺任一即 exit 2），
并有一条专门的陷阱反测证明分段取法会被拦下。

### 0.2a 跨件引用台账〔判据：凡称「N 项 / N 个接口 / N 个事件」，一律按目标锚精确节界机器派生〕

<!-- XREF-NOTE-BEGIN -->

🔴 **两轮的教训**：v1.3 里本合同 4 处称 SP8 §11.5a 为「五项」，那是更早锚 `1f703b8` 的计数，
所锚 `970effe` 上该节实为 6 项 —— **分母手写**；v1.4 建了本门来防它，门自己却把**锚**留成手写、
把台账自身算作正文命中、只验起界不验止界 —— **手写只是上移了一层**。本版按三票的 B1–B5 收窄到下述可判形态。

**「一律」的确切含义（声明＝实现，不留未覆盖面）**：`specs/sp4/xref-check.sh` 对**生效正文**
（＝本合同全文剥离台账块与说明区之后的部分）做**全域穷举扫描**，
数词形态族 `[0-9一二两三四五六七八九十]+ ?(项|个?接口|个?事件)` 的**每一处命中**都必须归属下表某一行的「正文形态」；
**未归属即 exit 2**。说明区（`XREF-NOTE-BEGIN/END`，即本段与 §0.5）是唯一豁免区，
且**判据行不得划入说明区**（划入即红）。故本合同**不存在**「声称一律、实际只覆盖几条」的余地。

**逐条判据**：① 台账「锚」列必须与 ⑥ 输入表所冻结的锚**逐字一致**，不得手写任意 commit（锚不合法 ⇒ rc=2）；
② 切片的**起界与止界都必须实证命中**，任一未命中 ⇒ rc=2（不靠「结果非 0」兜底）；
③ 台账**行号与派生器各自唯一**，且台账派生器集合与 checker 派生器集合**精确相等**（双向差集均空）；
④ 「正文形态」必须在**生效正文**中命中 ≥1 次 —— **台账自己那一格不算命中**；
⑤ 块内以数字开头却解析不出 8 列的行 ⇒ 报「有行未被解析」并 rc=2，不得静默跳过；
⑥ 旧数词残留扫描同时覆盖 `spec.md` 与三个 checker 自身。

<!-- XREF-NOTE-END -->

<!-- XREF-SP4-BEGIN -->

| # | 派生器 | 目标件 | 锚 | 节界 | 计数对象 | 声称实数 | 正文形态 |
|---|---|---|---|---|---|---|---|
| 1 | `sp8-11.5a-items` | `docs/dingwei-sp8-design.md` | `970effe` | `^### 11.5a … ^### 11.6` | 行首编号条目 | 6 | 六项独立复算 |
| 2 | `arch-cap-interfaces` | `docs/dingwei-platform-architecture.md` | `6a98629` | `^## 6. … 下一个 ^##` | owner=CAP 的接口行 | 5 | 五个既有接口 |
| 3 | `arch-cap-events` | `docs/dingwei-platform-architecture.md` | `6a98629` | `^### 4.2 … 下一个 ^##` | owner=CAP 的事件行 | 2 | 两个既有域事件 |

<!-- XREF-SP4-END -->

<!-- RECUSAL-SP4: NO-REF-11.6AB -->
<!-- RECUSAL-DECL-BEGIN -->

**回避边界（判据，不只是声明）**：实测冻结锚上 §14–§16 全文引用 `§11.6a` / `§11.6b` **0 次**。
🔴 判据覆盖**合同与目标正文的全部生效内容**（散文段、表格、清单一律在内），**仅排除本声明区**
（`RECUSAL-DECL-BEGIN/END` 之间这几行）——**声明区是唯一可以提这两节的地方**。
- 合同侧：`contract-check.sh` 剥离本声明区后全文扫描，出现即红；
- 正文侧：`check.sh` 全文扫描；若正文确需引用，**必须同时**写 `<!-- RECUSAL-SP4-REF: AC-SP4-xx -->`
  逐条点名受影响的 AC（该 AC 由 opus50 回避），**只引用不标记＝红，只标记不引用亦红**。
⚠️ v1.1 此处只扫 D/AC 判据行，普通段落可绕过——**声明大于实现**；本版按 SP8 的同一套围栏收窄为全量扫描并补普通段反测。

<!-- RECUSAL-DECL-END -->

### 0.3 前置依赖的当前状态与处置

| 前置/输入 | 冻结锚可见事实 | SP4 设计期处理 | SP4 实现期开工门 |
|---|---|---|---|
| **SP1** | 冻结 SP1 设计件头为 `draft，待双评审`；coordinator 已在本轮外告知其收口 | 只消费 AccessCtx/principal/企业边界既有 seam；不把消息 verdict 倒签进冻结件 | SP1 verdict 锚与 AccessCtx/隔离负测未落账前，CAP 只做 contract test，不上线分发与运行态查询 |
| **SP3** | 冻结 SP3 设计件头为 `draft，待三方全文评审`；coordinator 已告知收口，本席已投 PASS | 复用其 domain/visibility/晋升/装载语义；**CAP 只持 asset/version 与 KNO ref，不另造旁路**（SP3 §3.3 已定） | SP3 verdict 锚与四维/装载负测未落账前，不接真实 KNO 装载 |
| **SP8（§0.3 明列前置）** | ✅ **已收口**：合同 `a8066f5`、正文 `970effe` 均 3/3 PASS；批准权、`approval_ref`、grant/challenge 状态机与消费侧合同（SP8 §11.5a）已定义 | **不自造批准权**：平台级晋升与 immutable 名单变更**按 SP8 §11.5a 消费侧合同接线**——**六项独立复算**：①签名集合含 ≥`required_signatures` 个互不相同合格 principal · ②`audience`/`enterprise_id` 与执行主体一致 · ③`scope` 含本 effect 且 `effect_target_digest` 逐位相同 · ④未过期未吊销 · ⑤**通过原子 claim 取得 execute permit**（非"读一下 remaining 再执行"） · ⑥🔴 **无 permit 不得发起外部 effect**：重放得 `IN_PROGRESS` 时只能等待或按 `retry_after` 重试查询、**不得据同一 claim 再执行一次**；**lease 过期后的接管须持新 `fence_token`，且先对账再决定是否执行** | SP8 实现侧 grant conformance（并发 claim / fence 接管 / revoke×pending）未过前，CAP 只用 fixture，不接真实 grant |
| **SP7a/PLT（协作输入，非 §0.3 前置）** | 受控仓储 API、migration runner 未在本分支交付 | CAP schema 与 repository port 并行设计；不自建 DB 句柄、不在应用层补租户过滤 | 受控 transaction、隔离与 migration runner conformance 未过，不得 cutover |
| **SP2（协作输入，非 §0.3 前置）** | SP2 正文已收口，锚 `53e4a2a` | 分发目标与执行侧 seam 只读消费；不得改 WRK 状态语义或派发信封定义 | 接口 conformance 未就绪时用固定 fixture，不接真实派发 |
| **SP7b（SP4 自交）** | CAP 最终 schema 未设计 | 本件须交 CAP schema/migration/cutover/rollback 五列合同 | SP4 实现批次履约，不拿 SP7a PASS 代签 |

冻结证据与消息状态分层：设计可在 seam 明确时推进；实现开工门必须有可取 verdict/接口证据，不以本合同代填。

### 0.4 待裁项的处置结果〔业主已裁，随本版落地〕

| # | 事项 | 处置 |
|---|---|---|
| 1 | **前置 SP8** | ✅ **已解除**：SP8 全流程收口（合同 `a8066f5`、正文 `970effe`，均 3/3）。本件由"批准权无权威定义时 fail-closed"改为**按 SP8 §11.5a 消费侧合同接线**，见 §0.3 SP8 行 |
| 2 | **SpecDoc §15.4 / §15.5 排版位置落在 §16 之后** | ✅ 按裁定**不改 SpecDoc**：单区间取法 ＋ canary 硬断言 ＋ 陷阱反测已把它挡成机器可判（§0.2） |
| 3 | **§14.5a PDP/PEP 边界** | ✅ 按裁定：**本件定义 PEP**（MCP 运行态查询的接入点、判据来源与拒绝语义）；**PDP 策略与授权权力归 SP1/SP8**，本件不定义策略内容 |
| 4 | **§16.6 紧急路径的执行入口在 SP6**〔新增登记〕 | 冻结架构中唯一紧急执行入口 `AGT-CAP-EmergencyAction` 属 `AGT`（SP6 范围），**SP6 前置写 SP4、只能晚于本件**。⇒ 本件**不等 SP6、也不自造入口**：只定义 capability-effect 判据与拒绝语义，并把入口侧登记为**跨件义务**；🔴 **在 SP6 定义该入口的强制执行之前，CAP 侧紧急路径一律 fail-closed**（`disable`/`quarantine`/`revoke` 只走常规同级同期审批路径），不得以"紧急"为由绕开 |

### 0.5 v1.6 变更说明〔定向 bump 第 2 轮，供门方一轮判完〕

<!-- XREF-NOTE-BEGIN -->

v1.4 三票均 NO，但三家一致认定：v1.4 相对 v1.3 **严格变好**（旧债全闭），判 NO 只因**新长出来的那道门自身证不到它自称在证的东西**。
本版加固那道门，并按 coordinator 的合并清单把**执笔自查发现的 `check.sh` 三处同族缺陷一并收掉**。
不动：范围 · 三锚组合冻结 · §4.1 评审协议 · 已闭的语义落点。
🔴 **`check.sh` 本版有改动，评审面随之变化**——v1.4 里它 sha 与 v1.3 逐字节相同，三个门方据此都跳过了它；**本轮不得沿用「未变故免检」**。

| # | findings | 处置 | 如何自证 |
|---|---|---|---|
| B1 三票 | 正文形态绑定是死检查（台账自身在 `spec.md` 内 ⇒ 恒真） | **采纳**：判命中前先剥离台账块与说明区；selftest 改成「只改正文、不改台账」的真脱节变异 | `xref-check.sh --selftest` 的「B1 真脱节」一项 |
| B2 两票 | §0.2a 称「一律」但只覆盖三条中文数词，阿拉伯形态 0 覆盖 | **甲＋乙都做**：判据行阿拉伯分母改写为已登记形态（`D-SP4-11`／`AC-SP4-03`／`AC-SP4-04`／⑥ 表用途列）＋ 新增**全域穷举扫描**，每处命中须归属台账行，未归属 rc=2 | 正文插入「9 接口」→ rc=2；见「B2 未登记数词形态」一项 |
| B3 一票 | 台账「锚」列不受冻结集约束，锚仍是手写 | **采纳**：台账锚须与 ⑥ 输入表逐字一致，不一致 rc=2；⑥ 表本身已与两个 checker 常量三方对撞 | 台账锚改 `c7ac0c3` → rc=2；见「B3 台账锚与 ⑥ 表不一致」 |
| B4 一票 | `_slice` 只记 start，不验 stop 真命中 | **采纳**：`_slice` 强校 `start_seen`/`stop_seen`，任一未命中抛错 | 见「B4 止界／起界不命中必抛错」两项 |
| B5 一票 | 台账不拒重复行号／重复派生器 | **采纳**：行号与派生器各自唯一 ＋ 与 checker 派生器集合精确相等 | 见「B5 重复整行／重复行号／孤儿派生器」三项 |
| N1 | 报错文案停在旧分母；残留扫描不扫 checker 自身 | **采纳**：文案改正；残留扫描纳入三个 checker 文件 | 见「N1 checker 自身残留旧数词必红」 |
| N2 | 台账列数 ≠8 的行静默跳过 | **采纳**：块内数字开头行解析失败即报「有行未被解析」rc=2 | 见「N2 台账行列数写坏」 |
| N3 | `D-SP4-11`／`AC-SP4-07` 四处语义无断言 | **采纳**：逐条补 `grep -qE` 断言 | 删任一处 → `contract-check.sh` rc=1 |
| N4 | 台账第 3 行节界写 `^## 4.`，而 §0.2／`AC-SP4-04` 称 §4.2 | **采纳**：`xref-check.sh` 与 `check.sh` 的事件派生器**一并**收窄到 `^### 4.2`，实数仍为 2 | 台账「节界」列与 checker label 逐字对撞 |
| N5 | `EVENT_N -eq 2` 手写，与台账同源双写 | **采纳**：改为取 `xref-check.sh` 输出的 `XREF-VALUE arch-cap-events` | `contract-check.sh` 内已无手写事件数 |

**另行加固（同族自查，非门方所提）**：§0.2 的机器派生快照（`28/45/5/2/1`）此前只是文字，
本版由 `contract-check.sh` 与 `check.sh --selftest` 的实算值**逐项对撞**，快照写错即 rc=2 —— 收掉「§0.2 快照可随意改」这一面。

#### `check.sh` 三处同族缺陷（执笔自查提出，本版收掉；请门方定向复核）

三处与 B4／B5 同根，均做成**通用不变量 ＋ 常驻反测**，而不是三个补丁：

| 编号 | 原缺陷 | 收法 | 常驻反测（证明「不加固就会红」） |
|---|---|---|---|
| C1 | `derive_src` 只验起界不验止界 —— 删掉 SpecDoc 的 `## §17` 标题后派生集合由 **28 撑到 114** 而不报错 | 抽出 `slice_lines()`，**起界与止界都强校**，任一未命中抛 `Boundary` ⇒ exit 2；三个派生器统一走它，起界另加边界（`^## 6.` 不再误命中 `6.5`） | 「止界不命中必抛错」「起界不命中必抛错」「架构 §6 止界不命中必抛错」 |
| C2 | COVERAGE 比对只算 `want-got`，`actual` 中多出的伪 token 不判，非精确相等 | 增判 `actual − ⋃expected`，多出即红并列名 | 「COVERAGE 含不属于任一派生集合的伪落点必须转红」 |
| C3 | AC 解析用 `ac[aid]=…` 字典赋值，同一 `AC-SP4-xx` 写两行**后者静默覆盖前者** | 强校唯一性，重复即红并列名（相邻 D 行本就用 `ids.count(...)!=1` 强校「恰好一次」，此处是遗漏而非取法所限） | 「同一 AC 写两行必须转红」 |

🔴 **C1 为什么必须在 v0.2 建树前收**：`spec=28 E=45 interface=5 event=2 prototype=1` 这组派生实数，三个门方在 v1.2／v1.3／v1.4 都当过回归判据。
现在没出事只因 SpecDoc 被三锚按 sha256 钉死、源改不动 —— **缺陷被冻结掩住**。v0.2 整合要重锚，锚一换掩护即失效，而取法仍不验止界。

**收后回归**：派生实算值 `spec=28 E=45 interface=5 event=2 prototype=1` **逐项未变**（加固是行为保持的），
`check.sh --selftest` 由 18 项增至 **23 项**。

<!-- XREF-NOTE-END -->


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

组合冻结锚：SpecDoc `4220195`；**SP8 设计件 `970effe`**；其余 `6a98629`。

| 输入 | sha256 | 用途 |
|---|---|---|
| `docs/dingwei-platform-specdoc.md` @ `4220195` | `66df042ec49e6c7345e5c5cdbe1cf4e7f5c3bbdc94a1c671bb35c588532e8e0f` | v3.73 §14–§16、owner_sp 含 SP4 的附录 E |
| `docs/dingwei-platform-architecture.md` | `0a2288c7b9e245eded06944cd063a6d0d458c6b1f45a3a9534d29817b6c58fdc` | CAP 边界、五个既有接口、两个既有域事件、数据与原型入口 |
| `docs/dingwei-prototype.html` | `eeb71a5a77a53990569e08f9745165903bcf9e34cf6ce0a5047824167e1df4b8` | skill/MCP 入口 `s5` 的交互输入 |
| `docs/dingwei-sp0-design.md` | `f635b96e6c3574319d2e8df7e6a1c41e9693a66d7581504df60a6801531b8bec` | 规则版本与评审 seam 的冻结输入 |
| `docs/dingwei-sp1-design.md` | `8fafe6c5bac5595eaa771543dfc4199ba3670081786f1769f39833dc616e27bf` | AccessCtx/企业边界的冻结输入 |
| `docs/dingwei-sp2-design.md` | `37d2c17cc0761914aae91abd20273746231d894c50163cb89b8123cc03d76a86` | generation/幂等/派发 seam 的已收口输入 |
| `docs/dingwei-sp3-design.md` | `e6317b5c7257eb86b405d355b3bbcdb72751eb8c677fa2f388891c1d307b1e97` | 四维、晋升与装载语义的已收口输入 |
| `docs/dingwei-sp8-design.md` @ `970effe` | `4f5e4e06d29d4a9fffcdb46f413fbc37bcfd615e9af9b04215e0a6c20205a98a` | 🔴 **本件"真接线"的权威来源**：§11.5a 消费侧合同（**六项独立复算**，含原子 claim / execute permit / `IN_PROGRESS` 重放 / 接管 `fence_token` 与对账；项数由 `xref-check.sh` 在本锚上现算，见 §0.2a）；两个 checker 均校验其 sha256，取不到或漂移 **rc=2** |
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
| `D-SP4-08` | 生命周期与晋升 | 三件事分判、候选触发、晋升与创建解耦；**按 SP8 §11.5a 落消费侧**：**六项独立复算**（①签名集合 ≥`required_signatures` 个互不相同合格 principal · ②`audience`/`enterprise_id` 一致 · ③`scope` 含本 effect 且 `effect_target_digest` 逐位相同 · ④未过期未吊销 · ⑤**原子 claim 取 execute permit** · ⑥**无 permit 不得发起外部 effect**）；**重放非终态只得 `IN_PROGRESS`，只能等待或按 `retry_after` 重试查询，不得据同一 claim 二次执行**；接管路径的 `fence_token` 与对账义务见 `D-SP4-11` |
| `D-SP4-09` | 自我进化与回写 | 提案不批准、暂存审批、回写纪律、SYNC IMPACT REPORT |
| `D-SP4-10` | immutable 与装卸对称 | 名单与重两签、对称审批、capability-effect 判据、紧急原语与 fallback |
| `D-SP4-11` | 接口与事件合同 | 五个既有接口 ＋ 两个既有域事件逐项 schema/effect/auth/idempotency/generation/tx/error/audit；tombstone 与 `installation_generation` fencing；**凡产生外部 effect 的接口（`CAP-CMD-RequestPromotion`〔§0.3 明列的平台级晋升正是这条路〕/`CAP-CMD-Distribute`/`CAP-CMD-Uninstall` 及其派发面）须在合同列写明其 `approval_ref` 与 execute permit 的取用点**；并落 **SP8 §11.5a 第 ⑥ 项的接管语义**：`claimed→executing` CAS 须校验 `execution_owner`＋`fence_token`＋grant 仍有效同代；**lease 过期后的接管必须持新 `fence_token`，且先对账（查既有 `effect_result`/外部副作用是否已发生）再决定是否执行，不得盲重执行**；对账不可判时置 `unknown` 并转 reconcile，不得默认重发 |
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
| `AC-SP4-03` | 架构 §6 CAP 接口 | 五个既有接口逐项落点，无 RT/EV 跨表污染 | 字段语义充分性 |
| `AC-SP4-04` | 架构 §4.2 owner=CAP 事件 | 两个既有域事件逐项落点；新增事件 ID 0 | 消费方实现状态 |
| `AC-SP4-05` | 架构 §13 主责域含 CAP 的原型入口 | 逐项落点且冻结原型中存在 | UI 可用性与实现状态 |
| `AC-SP4-06` | 判型三判据与归属分级 | 三判据各自可判、互不替代；分级按 §14.3a | 某具体资产该判哪类 |
| `AC-SP4-07` | 准入三层与 SP8 §11.5a 接线 | CAP 不自造批准权；**六项独立复算齐**（项数按 §0.2a 台账在 `970effe` 上机器派生，手写分母漂移即红）；**外部 effect 前必持 execute permit**（`claimed→executing` CAS 校验 owner＋`fence_token`＋grant 同代，失败即拒）；**重放非终态返 `IN_PROGRESS` 且不发起第二次 effect**（只等待或按 `retry_after` 重查）；**lease 过期接管须持新 `fence_token` 且先对账再决定是否执行**，对账不可判置 `unknown` 转 reconcile，**不得盲重执行** | 批准决定是否恰当、SP8 实现是否正确 |
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
| **与已收口 SP8 的接口差异** | SP8 已收口（合同 `a8066f5`、正文 `970effe`）。本件按其 §11.5a 接线；**若发现该合同与本件所需不符，停止该路径并登记差异交 coordinator/业主裁**，不改已收口件、也不在 CAP 侧自造等价物 |
| 与已收口 SP1/SP2/SP3 seam 对撞 | 停止该路径，登记接口差异并交裁；不改已收口件，也不单方给上游产物加必填字段 |
| 目标与现状代码冲突 | 记 retain/adapt/rewrite 差距；旧实现不升级为目标规则 |
| **跨件引用计数漂移**〔本版新增〕 | 合同所称「N 项/N 个接口/N 个事件」与目标锚现算不符时，`xref-check.sh` **exit 2**；一律**以目标锚现算为准更正合同**（并同步 §0.2a 台账与全部正文形态），不得改台账迁就旧数字；**「为凑数字换锚」已由机器挡住**：台账锚须与 ⑥ 输入表逐字一致，改锚必先改 ⑥ 表，而 ⑥ 表与两个 checker 常量三方对撞 |
| 合同范围或机器集合变化 | bump 合同并重走三方评审，未过不写正文 |

## 6. 开工门

- [ ] sol 合同 PASS；
- [ ] opus50 合同 PASS；
- [ ] deepseek 合同 PASS；
- [ ] `specs/sp4/contract-check.sh` PASS；
- [ ] `specs/sp4/check.sh --selftest` PASS（正文未创建前普通 check 应 exit 2，不算通过）；
- [ ] `specs/sp4/xref-check.sh` 与 `specs/sp4/xref-check.sh --selftest` 均 PASS（跨件引用台账与目标锚现算一致、反测全过）；
- [ ] coordinator 登记**本版三锚组合冻结**（SpecDoc `4220195` ＋ SP8 件 `970effe` ＋ 基线 `6a98629`）与合同锚。
