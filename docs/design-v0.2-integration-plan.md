# DingWei `design/v0.2` 统一基准整合方案（待业主裁定）

> 状态：业主裁定后的落地版；执行证据以 `specs/v0.2/` 为准。  
> 产出日期：2026-08-26 UTC。  
> 硬边界：不 force-push，不改写任何已收口 SP 历史锚；整合使历史锚成为 v0.2 ancestor。  
> 本方案的取件原则：沿用 SP4 已经 3/3 PASS @ `16028ac` 的“各取最新”组合；设计域为长期权威轴，SP 保留为交付/评审封套。

## 1. 结论摘要

1. `design/v0.2` 应以 `design/addendum-decisions@4220195` 的 SpecDoc/RV 治理面为一条父线，以 `6a98629` 的新架构、原型、SP0～SP3 为另一条父线，用真实 merge ancestry 整合，不用无来源的“手拼新树”。
2. 建议纳入已收口 SP8：合同取 `a8066f5`，正文取 `970effe`，并保留 SP8 分支后续证据提交至 `c7ac0c3`的 ancestry。这是 SP4 的已冻结权威输入；不纳入会在统一基准里重造“树外键”。
3. 不建议直接改写 SP0～SP4 旧 checker 的冻结锚。旧锚是已发生评审的证据坐标，改它会把“新基准兼容复核”伪装成“旧评审从未发生”。应另立 `specs/v0.2/baseline-manifest.tsv` 及 `check.sh`，直接校验 `design/v0.2` 当前树的路径哈希，再对受 SpecDoc v3.73 变更影响的 SP 做 delta 复核。
4. 6 条未闭阻断必须原 ID、原归属、原状态带入。建议“先无损整合，继续挂账，禁止宣告 v0.2 可生效/可实施”；“先闭再整合”是可选门禁，归业主裁定。
5. `design/v0.2` 成为后续设计唯一主线；`design/addendum-decisions` 和各旧 SP 分支保留为可追溯历史锚，冻结/归档而不删除。后续 SP5～SP8 的新工作从 `design/v0.2` 切出。

### 1.1 业主最终轴裁定及实跑结果

- SP 编号继续作为交付/评审/冻结封套；架构域升为长期设计权威轴。
- 存量 SP0/SP2/SP3/SP4/SP8 不重切；用架构 §17 owner 表对原章节范围做机器等价性核验。
- 实跑：SP0/GOV 1 行、SP3/KNO 1 行、SP4/CAP 2 行、SP8/GOV 1 行无错配；SP2/WRK 6 行中 3 行 owner 为 GOV，已以 `DM-SP2-01..03` 登记跨域 seam；**未登记错配=0**。
- 未起件从域 owner 派生：SP5-COL、SP6-AGT、SP6-MSG、SP7-PLT；SP6 明确拆为 AGT/MSG 两件。
- GOV=SP0·SP8 是已知分期例外，不改变“同一 §17 责任行只有一个主责域”。
- F.15/C.1 等非 §N 内容随域 owner 进义务台账，不再因章节区间派生而成为盲区。

## 2. 取件口径与精确指纹

### 2.1 三大权威件

| 对象 | `design/v0.2` 路径 | 出处锚 | sha256 | 处置 |
|---|---|---|---|---|
| SpecDoc | `docs/dingwei-platform-specdoc.md` | `4220195b311ca9b9b734706ce80d8b3fcc1abfae` + v0.2 架构切换 delta | `772220830b47a1dbbab69b2740efac1992f6681f73f92d0821476fa516c6a655` | 保留 v3.73 语义变更，仅把其 7 处现行 `A§` 引用切到新架构数字节；原 `4220195` 仍为 SP 历史 checker 输入 |
| 新架构 | `docs/dingwei-platform-architecture.md` | `6a98629fefd91e69b146516291491cbc6d1136da` （内容源自 `46b79ab`） | `0a2288c7b9e245eded06944cd063a6d0d458c6b1f45a3a9534d29817b6c58fdc` | 唯一现行架构事实源 |
| 原型 | `docs/dingwei-prototype.html` | `6a98629fefd91e69b146516291491cbc6d1136da` | `eeb71a5a77a53990569e08f9745165903bcf9e34cf6ce0a5047824167e1df4b8` | 取 SP 基线版，不取名义主线的 `0308a24…` 版 |

上述组合与 `specs/sp4/contract-check.sh@16028ac` 的冻结组合一致，无需重做“为什么各取最新”的语义选择。整合评审仍须核对 merge 结果字节不变。

### 2.2 旧架构件

不删除历史内容；沿用 `46b79ab` 已通过的 R100 归档处置：

| 现行路径 | 归档路径 | 出处锚 | sha256 |
|---|---|---|---|
| `docs/dingwei-architecture-design.md` | `docs/v0.1/dingwei-architecture-design-v0.34.md` | `46b79ab0022b0686a23455f328c9316d7f87281f` | `ef54ad86fa9763ef29510ef60a98b5471be0bfadc56b17a1c0fb22d1f593d3e7` |

合并后根路径 `docs/dingwei-architecture-design.md` 必须不存在；归档件只能作沿革参考，不得被 checker、新 SP 合同或正文当作现行输入。保留归档比直接删除好：它保留旧 verdict 的可解释性，同时新路径已消除双事实源。

### 2.3 SP 件

| SP | 设计件/合同 | 权威出处 | sha256 / 说明 |
|---|---|---|---|
| SP0 | `docs/dingwei-sp0-design.md` | `6a98629` | `f635b96e6c3574319d2e8df7e6a1c41e9693a66d7581504df60a6801531b8bec` |
| SP1 | `docs/dingwei-sp1-design.md` | `6a98629` | `8fafe6c5bac5595eaa771543dfc4199ba3670081786f1769f39833dc616e27bf` |
| SP2 | `docs/dingwei-sp2-design.md` | `6a98629` | `37d2c17cc0761914aae91abd20273746231d894c50163cb89b8123cc03d76a86` |
| SP3 | `docs/dingwei-sp3-design.md` | `6a98629` | `e6317b5c7257eb86b405d355b3bbcdb72751eb8c677fa2f388891c1d307b1e97` |
| SP4 | `specs/sp4/{spec.md,check.sh,contract-check.sh,xref-check.sh}` | `16028ac41ad7aab2779bba0c6395dd37494cac96` | sha256 依次为 `77abb7161d101e05a77a85e58632392169a9badbccbfeac452b40be21ef98848`、`242726ba4c63dfe0ad7df18d66bcc2d164e54261bf4c9fc534ba6449d1024916`、`05658fdde1394157ab863e3500b25e60531d3da804af679ba6babe192ba40805`、`292cd40a438aa81d6b4dbff476afe24adee913d6578a1679ca1b1d24048e7e78`；正文尚未产生，不得伪造 |
| SP8 合同 | `specs/sp8/spec.md` 及 checker | `a8066f5` 并保留后续 ancestry | `spec.md` sha256 `f099de9694f9d80dbb3b18c696eb2060e958296c11c3c8faea06874cd5aab219` |
| SP8 正文 | `docs/dingwei-sp8-design.md` | `970effe3fdcc52b43e455597dd877a39341f9cba` | `4f5e4e06d29d4a9fffcdb46f413fbc37bcfd615e9af9b04215e0a6c20205a98a` |
| SP8 证据补件 | SP8 分支上的证据件 | `c7ac0c3` 分支头 | 保留评审可追溯性，不得用它暗改 `970effe` 正文内容 |

SP4 v1.8 已 3/3 PASS @ `16028ac`，并以 `specs/sp4/REVIEW-REGISTRATION-v1.8.md` 登记被评四件指纹；`4006cbe` 保留为 v1.3 历史票据锚。

### 2.4 跨分支 commit 引用与可达性清单

对 `16028ac` 与 `c7ac0c3` 带入的 `specs/sp*` 及 `docs/dingwei-sp*-design.md` 扫描所有可解析 commit 字面，需分为“运行时硬依赖”和“沿革/证据引用”两类；`4006cbe` 作为必须保持 ancestor 的历史证据锚单列。

| 类型 | commit | 主要引用者 | 当前可达性依赖 | v0.2 处置 |
|---|---|---|---|---|
| checker 硬依赖 | `46b79ab0022b0686a23455f328c9316d7f87281f` | SP0/SP1 合同与 coverage checker | 架构/SP 血脉 | 通过合入 `6a98629` 成为 v0.2 ancestor |
| checker 硬依赖 | `53d91aedd158c0a8618f0196f013cfee5cfd44aa` | SP2 checker | SP2/SP3/SP4/SP8 血脉 | 通过合入 `6a98629` 成为 v0.2 ancestor |
| checker 硬依赖 | `53e4a2aed1569eef91e4048e7da2a1cd32db3ec2` | SP3 `BASE_ANCHOR` | SP3/SP4/SP8 血脉 | 通过合入 `6a98629` 成为 v0.2 ancestor |
| checker 硬依赖 | `fcfec9a1f43769ddd831eb4071a2a787624936ea` | SP3 `SPECDOC_ANCHOR` | `design/addendum-decisions` 血脉 | 通过合入 `4220195` 成为 v0.2 ancestor |
| checker 硬依赖 | `6a98629fefd91e69b146516291491cbc6d1136da` | SP4/SP8 `BASE_ANCHOR` | SP3/SP4/SP8 血脉 | v0.2 的显式 merge parent/ancestor |
| checker 硬依赖 | `4220195b311ca9b9b734706ce80d8b3fcc1abfae` | SP4/SP8 `SPECDOC_ANCHOR` | `design/addendum-decisions` | v0.2 起点或显式 merge parent/ancestor |
| checker 硬依赖 | `970effe3fdcc52b43e455597dd877a39341f9cba` | SP4 两 checker 的 `SP8_ANCHOR` | 目前仅 SP8 施工分支可达，无 tag | 必须通过合入 `c7ac0c3` 成为 v0.2 ancestor，不能只靠原分支续命 |
| 设计头/合同证据 | `fa60efa1977188ca2061cd1ec4929a363cdd6a99`、`a8066f54b44b6b9f89bc5752e75f25140ba368e0`、`524115249d75bdff46247aeeabdc60c5e5b9d266` | SP2/SP8 头块与证据 | 各 SP ancestry | 分别由 `6a98629`/`c7ac0c3` 合入保持可达 |
| SP8 沿革/证据 | `3af1bfa1ea3763cb0b56e82958d056690e11f9b5`、`7382f512eca87a29652904a261b8ae419c040973`、`d3aceac814203b2be49822061bf6997660aab0c9`、`1f703b85540daa376967101acc37fbc2c920f753`、`496c95236afa810ddab39b055414d7b37788531e`、`704d4d90e36c2b11e47bb59cd8e513b7c9bfd30b`、`9fc69d801aec5aeaad965c2d5c9230833f8888e0` | `specs/sp8/EVIDENCE-*` | SP8 分支 ancestry | 合入 `c7ac0c3` 后全部成为 v0.2 ancestor |
| SP4/SP8 历史交叉引用 | `aa8d83b53c0fc93aae2f6208e0ac37aebe56149e` | SP8 合同对早期 SP4 挂起状态的记录 | SP4 分支 ancestry | 合入 `16028ac` 后成为 v0.2 ancestor |
| SP4 v1.3 历史证据 | `4006cbeeda3edd986d6a9da53268e68aa01c9c9e` | v1.3 三票与登记的被评锚 | 不得依赖施工分支存续 | 由 `16028ac` ancestry 和 freeze tag 保持可达 |

可达性门不只检上表手写集合；应从合并后树内所有受管设计/合同/checker/证据件机器提取可解析 commit，输出“预期/实检/未知”。每个硬依赖必须同时满足：

1. `git cat-file -e <sha>^{commit}` 成功；
2. `git merge-base --is-ancestor <sha> <V02_FREEZE_COMMIT>` 成功；
3. 受保护 `design/v0.2` ref 或不可变 freeze tag 至少一个长期指向其后代；
4. 在浅克隆/CI 中显式 fetch 该冻结 ref 的完整 ancestry，不依赖“某开发者本地恰好还有 loose object”。

建议在 v0.2 整合 PASS 后另建不可变 freeze tags（具体命名由业主/coordinator 裁定）作第二层防护，但**不以 tag 代替 merge ancestry**。只打 tag 会让 v0.2 仍 checkout 不出统一树；合入 `c7ac0c3` 则使 `970effe` 由 v0.2 本身保活，即使日后删 SP8 施工分支也不会丢锚。

### 2.5 跨件小节号与数量自述的同锚校验

SP4 `spec.md@4006cbe` 曾有 4 处称 SP8 §11.5a 是“五项独立复算”，而其冻结的 `970effe` 实为 6 项；该债已经过 v1.4～v1.8 定向 bump、跨件派生门和三轮评审在 `16028ac` 收口。

处置判据：

- `4006cbe` 的 3/3 PASS 仍是不可改写的历史事实；后续收债不倒签为 v1.3 当时已包含；
- `16028ac` 已把六项机器派生、第 6 项设计/AC 落点、计数反测及区间派生器台账收齐，并取得 3/3 PASS；
- v0.2 只整合 `16028ac`，同时保留 `4006cbe` 为 ancestor 和沿革证据。

v0.2 新增跨件引用门：凡文本声称“N 项”、“第 N 节”、“N 个接口/事件”，必须同时记录目标 commit，并由 checker 在该 commit 的精确节界机器派生实数/集合。只校验目标文件 sha256 不够：它能证明“取对件”，不能证明“对这件的复述是对的”。目标节不存在、提取空集、数量不等均 rc=2；不手写分母作真相源。

## 3. SP8 是否纳入：建议与待裁点

### 建议：纳入已收口 SP8

理由：

1. SP4 的 v1.3 合同已把 `docs/dingwei-sp8-design.md@970effe` 列为冻结权威输入，并校验其 sha256。不纳入 SP8 会使 v0.2 仍无法单树 checkout 出 SP4 的完整语义。
2. SP8 不是未开工的“后续包”，而是合同、正文均据称已 3/3 PASS 的已收口输入。统一基准应整合已成事实，不应退回到“SP8 未定义”。
3. 业主所说“后续 SP5～SP8 以该基准开展”可解为：SP5～SP7 新设计，以及 SP8 的实现/修订，从 v0.2 切支；不必把已通过的 SP8 设计从基准排除。

代价：SP8 与 SP4 为 `6a98629` 上的兄弟支，落地时需引入 SP8 全部评审 ancestry，且在合并后重跑 SP8/SP4 两套 checker。这是可审计成本，不是语义风险。

### 备选：不纳入 SP8

只有业主明确把“后续 SP8”解释为“已收口 SP8 也不得进基准”时才选。代价是：SP4 仍须依赖树外 `970effe`，v0.2 不是可独立 checkout 的完整基准；需把 SP4 定义为“带外部历史依赖的例外”，直到 SP8 再开工。这与本次整合目标相悖，故不建议。

## 4. SP0～SP4 冻结锚：保留历史证据＋增设 v0.2 活基线

### 4.1 不建议的两个极端

- **只保留历史锚，什么都不加**：当前 checker 会继续全绿，但它们只证明历史快照没丢；`design/v0.2` 后续漂移时不会报警。
- **把旧 checker 里的锚全改成 v0.2 头**：会改写已 PASS 合同的证据口径，必须重开相应合同评审；而且“分支头”是浮动 ref，不能充当不变锚。

### 4.2 推荐的双层模型

**A. 历史证据层**

保留 SP0～SP4 和 SP8 现有 contract checker 的原冻结锚与原 sha256，用于回答“当时评审了什么”。旧 checker 不允许被 v0.2 落地提交顺手改锚。

**B. v0.2 活基线层**

新建（须业主放行后）：

- `specs/v0.2/baseline-manifest.tsv`：每行 `path<TAB>sha256<TAB>source_commit<TAB>status`；
- `specs/v0.2/check.sh`：对**当前工作树/当前 commit**重算路径 sha256，不去 `git show` 一组树外锚；
- `specs/v0.2/check.py`：要求 `4220195`、`6a98629`、`16028ac`以及业主裁定后的 SP8 证据头 `c7ac0c3` 均为 v0.2 不变锚的 ancestor；
- 输出必须含“预期/实检/未知”；缺件、空清单、重复路径、重复 RV ID 一律 rc=2。

这样，旧评审证据不被篡改，v0.2 树上任一冻结件的后续漂移又会当场报警。后续有意变更某件时，必须同步 bump manifest、给出 delta 和重开范围，不允许静默改哈希。

### 4.3 哪些 SP 需要 delta 复核

SpecDoc v3.69 到 v3.73 的实际变更不只是版本号：新增/改写了 §11.6、§11.6a、§11.6b 的派发快照、FAIL evidence、repro 分类、`cancelled/timeout/effect_unknown` 证据形态，并同步了若干新架构引用。因此：

- **SP3 必须 delta 复核**：其范围是 §10～§13，v3.72→v3.73 又直接改了 §11.6a；不能仅因旧 checker 绿就宣告 v0.2 兼容。
- **SP2 必须做定向 seam 复核**：SpecDoc 明说 SP2 是引用侧，须确认 `TaskSnapshot`、execution outcome 与新证据形态无对撞；这是 delta 复核，不是默认改 SP2 正文。
- **SP0/SP1**：按所有权范围做机器差集。若 v3.69→v3.73 差集未落其范围，可出“无语义影响”的带证据 PASS；不得因没改它们的文件就免复核。SP1 的 3 条未闭阻断仍单独挂账。
- **SP4** 已直接冻结 v3.73 和 SP8 `970effe`，且已 3/3 PASS @ `16028ac`；v0.2 整合新锚，`4006cbe` 仅作历史证据 ancestor。

判据：**输入字节变了且差集命中 SP owner/range，就重开该 SP 的 delta 评审；仅 commit 变、文件字节与可见语义都不变，只做整合不变性验收。**

## 5. 6 条未闭阻断与 REVIEW-STATE 无损迁移

### 5.1 必须保留的六条

| ID | 状态 | 归属 |
|---|---|---|
| `RV-甲06` | 挂账/挂账 | SP1 接手人（业主待定） |
| `RV-乙04` | 挂账/挂账 | SP1 接手人（业主待定） |
| `RV-甲10` | 挂账/挂账 | SP1 接手人（业主待定） |
| `RV-甲08` | 挂账/挂账 | 业主＋ops |
| `RV-执笔01` | 挂账/挂账 | coordinator 归口 |
| `RV-丙01` | 未决/挂账 | coordinator 归口 |

说明：上表双状态依次是“评审方状态/执笔方处置”。整合不是闭环动作，不得改 ID、改归属、把挂账改成采纳，也不得通过重生成让“未闭 0”。

### 5.2 ledger 合并算法

当前事实：`4220195`、`6a98629`、`16028ac`、`970effe`、`c7ac0c3` 的 `specs/gates/rv-ledger.txt` 字节一致，sha256 均为 `706c97c396a087fcdedd8f4a85d12403426ebd8fdce31f1b7f952012f2260d40`；84 行、84 个唯一 ID、重复 ID=0。`docs/REVIEW-STATE.md` 也字节一致，sha256 为 `220080482e8584a7e86fa5ffafa13a0b3f058b1a52b810486b93925606e97f9d`。

因此本次**不做文本拼接**，直接取同一权威 ledger。落地闸必须验：

1. 解析每条非注释行的稳定 ID；
2. 总行数=84、唯一 ID=84、重复 ID=0；
3. 与冻结 ledger 的 ID 集合做双向差集，两边均为空；
4. 对同 ID 比对域、严重度、评审状态、执笔处置、件、摘要；任一列不同即冲突阻断，禁止“后来分支覆盖前者”；
5. 用 `scripts/gen-review-state.sh` 从合并后 ledger 重生成临时件，与入库 `docs/REVIEW-STATE.md` cmp 一致；
6. 重生结果必须仍为采纳 78 / 挂账 6 / 合计 84，且未闭表精确含上述 6 ID。

若落地时某个分支 ledger 已分叉，才执行“按稳定 ID 并集”：新 ID 可合入；同 ID 同内容折叠；同 ID 不同内容必须交原 owner/coordinator 裁定，不能自动择边。

### 5.3 “随件挂账”与“先闭再整合”

**建议：随件无损带入 v0.2，但将 v0.2 状态标为 `integrated / blocked-for-effective-baseline`。**

理由：整合要先结束“任一树都 checkout 不出组合基准”的结构问题；6 条阻断的 owner 又不是本整合执笔。把“结构整合完成”与“设计基准可生效”分轴，既不丢条，也不被无 owner 的 SP1 无期限卡住建树。代价是需要一个明确的红灯状态，禁止把“已整合”转述成“已通过”。

**备选：先闭 6 条再整合。**

优点是 v0.2 首个锚就可声称无这 6 条挂账；代价是 SP1 接手人未定、ops/业主/coordinator 多方前置会让统一树无法建立，各 SP 继续在分裂树上前进。若业主选此案，必须同时给 SP1 owner 和闭环时限；否则它不是门禁，而是无期挂起。

### 5.4 SP1 证据续接

重锚或增设 v0.2 活基线不得让 `RV-甲06`、`RV-乙04`、`RV-甲10` 的复现面失效。应在 v0.2 集成证据中为每条记录：

- 历史输入锚及原 repro 命令；
- v0.2 当前路径/章节的映射；
- 同一 repro 在 v0.2 的结果；
- 如因路径/结构调整无法运行，先标 `UNKNOWN(rc=2)` 并由原 owner 给等价新命令，禁止删条或改 PASS。

“旧 repro 命令的字面失效”不等于“语义阻断已闭”。

## 6. 分支治理与建议落地顺序

### 6.1 主线关系

- `design/v0.2`：业主放行后的唯一后续设计基线；SP5～SP8 的新工作只从它切支。
- `design/addendum-decisions`：整合后冻结为 SpecDoc/RV 沿革支，不再接收新 SP 设计。如有必须并入的新业主裁定，应先在 v0.2 走变更流程，不再双写两条主线。
- SP0～SP4/SP8 旧施工分支：在 v0.2 整合评审 PASS 前保留 remote ref 和精确头，标记 archived/read-only，禁止删除。PASS 后可由业主选择“继续保留”或“关闭/删分支”，但后者只能在以下门全过后做：①所有可解析跨分支锚均已是受保护 `design/v0.2` 冻结 commit 的 ancestor；②不可变 freeze tag 已建并受保护；③CI 从空 object store 完整 fetch 后所有 `git show <anchor>:<path>` 仍通；④删分支后做一次隔离克隆全量 conformance。因此，旧 checker 需要的是 commit 长期可达，不是原施工分支名必须永久存在。

### 6.2 建议的 ancestry-preserving 顺序

业主放行后才执行：

1. 从 `design/addendum-decisions@4220195` 建 `design/v0.2`；
2. `--no-ff` 合入 `6a98629`，冲突按本方案的逐件指纹解决；
3. 合入 `design/sp8-v1-opus50tczc2000@c7ac0c3`，并确认 `docs/dingwei-sp8-design.md` 仍等于 `970effe` 的 sha256；
4. 在 coordinator 登记 SP4 3/3 PASS @ `16028ac` 后，合入 `design/sp4-v1-opus50tczc2000@16028ac`；
5. 解决生成件、加 v0.2 manifest/checker，重生 REVIEW-STATE，全量跑闸；
6. 对 SP2/SP3 做 v3.73 delta 复核，其他 SP 按 owner/range 差集决定是否重开；
7. 给 `design/v0.2` 整合锚做独立评审，不用“原分支各自 PASS”代签 merge 结果。

已做的只读合并预检：`git merge-tree --write-tree origin/design/addendum-decisions 6a98629` 报冲突的是 `docs/DELIVERY-STATUS.md` 和 `specs/evidence/EVIDENCE.md`；SpecDoc 可自动合并。这不意味可对两个冲突盲选 ours/theirs：

- `DELIVERY-STATUS.md` 应由统一树的真实件/版本重生成，不保留任一父线的过期状态；
- `EVIDENCE.md` 是作者侧旁证，应在统一树上按 collect 流程重出，不把父线证据包当成 merge 结果；
- 重生件若因“产物不能内嵌自身 commit”而需两阶段锚，应先做整合树锚，再做证据/状态锚，并记录两者关系。

## 7. conformance 可跑性与改锚清单

### 7.1 落地后必跑命令

```sh
sh specs/platform-architecture/check.sh
sh specs/platform-architecture/cutover-check.sh

sh specs/sp0-1/contract-check.sh
sh specs/sp0/contract-check.sh
sh specs/sp0/check.sh
sh specs/sp1/contract-check.sh
sh specs/sp1/check.sh
sh specs/sp2/contract-check.sh
sh specs/sp2/check.sh
sh specs/sp2/check.sh --selftest
sh specs/sp3/contract-check.sh
sh specs/sp3/check.sh
sh specs/sp3/check.sh --selftest

sh specs/sp4/contract-check.sh
sh specs/sp4/check.sh --selftest
# SP4 正文未产生时，下行预期 rc=2，不得记 PASS
sh specs/sp4/check.sh

sh specs/sp8/contract-check.sh
sh specs/sp8/check.sh
sh specs/sp8/check.sh --selftest

sh specs/tools/analyze.sh
sh scripts/reviewer-repro-gate.sh
sh scripts/speclint-gate.sh
```

可达性须在**隔离克隆**中单独跑，不得借用本地已有 loose object：

```sh
for a in \
  46b79ab0022b0686a23455f328c9316d7f87281f \
  53d91aedd158c0a8618f0196f013cfee5cfd44aa \
  53e4a2aed1569eef91e4048e7da2a1cd32db3ec2 \
  fcfec9a1f43769ddd831eb4071a2a787624936ea \
  6a98629fefd91e69b146516291491cbc6d1136da \
  4220195b311ca9b9b734706ce80d8b3fcc1abfae \
  970effe3fdcc52b43e455597dd877a39341f9cba
do
  git cat-file -e "$a^{commit}" || exit 2
  git merge-base --is-ancestor "$a" "$V02_FREEZE_COMMIT" || exit 2
done
```

上述是已知 checker 硬依赖集；落地时还必须用机器扫描派生完整 commit 引用集，防止手写清单漏新锚。

REVIEW-STATE 再生成与精确对账：

```sh
tmp=$(mktemp)
sh scripts/gen-review-state.sh > "$tmp"
cmp "$tmp" docs/REVIEW-STATE.md
rm -f "$tmp"
```

另须抽验以下定值不退化：

- 门方复现台账：11 条全给出期望值；
- `prose-style` 反测 53；
- `evidence-fresh` 反测 13；
- `path-anchor` 反测 12；
- `conflict-marker` 反测 7；
- `rv-integrity` 反测 5；
- `legacy-name` 反测 9。

任一闸因受检对象不存在而跳过，都应是 UNKNOWN/rc=2，不得当 PASS。

### 7.2 需改的锚常量

不在原 SP checker 内批量替换。需新增/更新的是：

1. `specs/v0.2/baseline-manifest.tsv` 的当前树哈希；
2. `specs/v0.2/check.py` 的不变来源锚集合；
3. `docs/DELIVERY-STATUS.md` 的统一版本/状态；
4. 新切 SP5～SP8 合同中的施工基线，必须指向业主冻结的 v0.2 **commit**，不得只写浮动分支名；
5. SP2/SP3 delta 复核如需改正文，必须 bump 各自合同版本并重开受影响验收，不得只改 checker 哈希求绿。

### 7.3 SP4 `deadbeef` 反测口径

`specs/sp4/contract-check.sh@16028ac` 第 3～5 行是脚本内部裸赋值：

```sh
BASE_ANCHOR=6a98629
SPECDOC_ANCHOR=4220195
SP8_ANCHOR=970effe
```

因此：

- `SP8_ANCHOR=deadbeef sh specs/sp4/contract-check.sh` 预期仍 rc=0：环境变量被脚本内部赋值覆盖，这证明 contract checker 不受外部环境牵走；
- `SP8_ANCHOR=deadbeef sh specs/sp4/check.sh` 预期 rc=2：coverage checker 显式允许环境注入，供反测与复算；
- 要反测 contract checker 的“换锚会拦截”，必须在临时副本/隔离工作树里把脚本内部 `SP8_ANCHOR` 改为 `deadbeef`，再期望 rc=2；不得用环境变量方式声称它漏检。

v0.2 可统一**文档化口径**，但不应强行把两类 checker 改成同一变量语义：不受环境影响的 contract freeze 与可注入反测的 coverage checker 承担的问题不同。建议每个 checker 增 `--print-inputs`/头注说明，并在 selftest 里明确验证“环境变量应生效/应被忽略”，避免再混用两种换锚方法。

## 8. `docs/v0.1/` 的语义

`docs/v0.1/` 是**历史归档层**，不是与 `design/v0.2` 平行的分支/发布序列：

- `docs/v0.1/` 中的 `v0.1` 是文档归档命名，说明被新事实源取代的历史件存放何处；
- `design/v0.2` 是 Git 分支名，表示统一设计基准；
- 两者不共享“目录版本递增即分支版本递增”的语义，不应因建 `design/v0.2` 就把现行文档整体搬进 `docs/v0.2/`；
- 现行权威件继续位于 `docs/` 根下稳定路径，旧件才进 `docs/v0.1/`；否则会制造路径迁移、已 PASS 锚失效和新的双事实源。

应在仓根 README/设计索引补一句权威说明：“现行平台规范、架构、原型以 `docs/` 根下指定文件为准；`docs/v0.1/` 只是历史归档。”

## 9. 业主需要裁定的最小集

1. SP8 是否按本方案纳入 v0.2（建议：是）；
2. 6 条未闭阻断是“随件挂账，v0.2 结构整合后仍不可生效”，还是“先闭再建 v0.2”（建议：前者）；
3. 是否采纳“保留历史 checker 锚＋新增 v0.2 活基线 manifest/checker”（建议：是）；
4. 是否要求 SP2/SP3 在 v0.2 生效前完成 v3.73 delta 复核（建议：是）；
5. `design/addendum-decisions` 是否在 v0.2 整合后正式冻结为历史支（建议：是）。

## 10. 整合 PASS 的必要条件

- 三大权威件和 SP0～SP4/SP8 取件哈希与本方案/业主裁定一致；
- 旧架构只存于 `docs/v0.1/` 归档路径，现行引用为 0；
- 六条 RV 原样存在，ledger 84/84 且重复 0，REVIEW-STATE 可重生一致；
- 门方复现台账 11 条与六组反测数不退化；
- platform architecture、SP0～SP3、SP8 普通检查和 selftest 符合各自预期；SP4 合同/selftest 通过，缺正文的普通检查必须如实 rc=2；
- `analyze.sh`、REVIEW-STATE cmp 与提交闸结论有证据；任一 UNKNOWN 不被折叠成 PASS；
- SP4 头仍精确为 `16028ac`，其第三票与 coordinator 登记可取；
- deepseekv4pro 对同一 v0.2 整合锚完成独立评审；
- 业主对第 9 节最小裁定集已给出明确结论。
