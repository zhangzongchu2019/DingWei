# specs/ —— Spec Kit 工程结构

落成 SpecDoc **§0.4 第 1 项生效前置**（Spec Kit 形态承载）。

## 结构

> 🔴 **本表的路径列由闸逐行核对**（`scripts/specs-index-check.sh`，提交时跑）：
> 表里列的路径必须在仓内真实存在，仓内 `specs/` 下的件也必须在表里出现，**两边都不许有余数**。
> 立此判据的直接原因：本表原为手写树形，自称是 `specs/` 的结构，实际 15 件只列了 7 件 ——
> `specs/evidence/` 三件与 `specs/tools/export-bundle.sh` 早已漏列，`specs/gates/` 两件与
> `export-exclude.txt` 是新增后未同步。**「改了没同步引用点」在同一天同一目录里的第三次。**
> ⇒ 与 `export-bundle.sh` 那条同源：**凡说「全部」，清单就不能只靠人手维护。**

| 仓内路径 | 说明 |
|---|---|
| `specs/README.md` | 本文件：`specs/` 的结构说明与取舍记录 |
| `specs/constitution.md` | 治理原则的【唯一】载体（§4.1） |
| `specs/platform/spec.md` | 装载点 —— 指向 `docs/` 的正文，不复制、不搬动 |
| `specs/platform/checklist.md` | 结构域（sol 主笔，26 项） |
| `specs/platform/checklist.security-reviewer.md` | 安全域（terra，18 项） |
| `specs/platform/checklist.crossartifact-reviewer.md` | 跨制品域（kimi，12 项） |
| `specs/platform/checklist.INDEX.md` | 作者维护的登记，不判定 |
| `specs/tools/analyze.sh` | `/speckit.analyze` —— 跨制品一致性检查 |
| `specs/tools/export-bundle.sh` | 导出锚定快照包；作用域由 `git ls-tree` 整棵导出，不手写 |
| `specs/tools/export-exclude.txt` | 快照包的显式排除清单，每条须带理由；缺理由或漂移即导出失败 |
| `specs/tools/handle-escape/main.go` | 裸库句柄逃逸检测器（Go AST）—— 判据二的权威 detector；覆盖换行签名 · import alias · 类型别名链 · 导出字段 · 匿名嵌入 · 接口方法 |
| `specs/gates/checklist-execution-exceptions.txt` | checklist 未落账的**登记例外** —— 🔴 登记 ≠ 修复；闸打印「登记债务」时**不是 PASS**；例外的新增与批准属 owner/门方评审面，执笔方不得自增自批 |
| `scripts/rv-integrity-gate.sh` | RV 完整性闸 —— 标注是 **reviewer-owned 的证据**，执笔方的清理/替换工具不得改写其正文；🔴 与「改一处漏一处」互为镜像：那是该改的没改，这是**不该改的改了** |
| `specs/gates/rv-ledger.txt` | RV 标注台账（reviewer-owned）—— 稳定 ID ＋ 最终状态；`--final` 以它为可比对象判「标注是否消失」。🔴 执笔方可增行，**不得改他域已有行的状态** |
| `scripts/appendix-e-owner-gate.sh` | 附录 E 认领闸 —— 「未认领恒为 0」须有 `owner_sp` 列承载；🔴 判「每行有没有主」，**判不了认领得对不对** |
| `scripts/evidence-fresh-gate.sh` | 证据包时效闸 —— 证据必须产自被评审的那个锚；🔴 **快照指纹一致只证明文件没被篡改，不证明内容是当期产出**，两者是不同的保证 |
| `scripts/superseded-claims-gate.sh` | 已撤销断言闸 —— 裁定改了引用点必须跟着改；🔴 判「清单内旧断言还在不在」，**判不了清单是否完备** |
| `specs/gates/superseded-claims.txt` | 「旧断言 → 现行断言 → 撤销依据」清单，本闸的数据源 |
| `scripts/checklist-executed-gate.sh` | checklist 执行落账闸 —— 宣称「已按 checklist 执行」时机器要求 checkbox 已勾**且证据栏非空**；未落账可登记例外，但输出明说**这不是 PASS** |
| `specs/tools/foreign-version-check.py` | 正文他件旧版本号检查（analyze ④b）—— ④a 只读头块一处，正文里的假版本看不见；本脚本补正文面，只在与该件称谓同行时才判 |
| `scripts/prose-style-gate.sh` | 体例合规闸 —— 正文不得含评审过程痕迹（指向特定评审轮次/版本/评审方的表述）；含 **RV 评审标注**模式：轮内可标、收官须逐条落到 采纳/驳回(带理由)/挂账，且正文清零 |
| `specs/tools/pending-debt/main.go` | 「尚未租户化」债的检测器（Go AST）—— **按效果判**：谁返回 `PendingTenantCtx` 造的上下文谁就是债源，其调用点全部计入，helper 叫什么与判据无关（乙域 terra SA-12b）；按 `(domain, ownerSP)` 分组供闸逐领域棘轮 |
| `specs/tools/safe-replace.py` | RV 段免疫的批量替换（提交闸 ⑯a）—— 替换前遮蔽【RV…】标注段及其处置行，模式若**只**命中标注段则判误操作并拒绝（静默零命中会被读成「已清理干净」）。🔴 立件原因：闸 ⑯ 是**事后检测**，丙域 kimi 要的是「作用域排除 ＋ 防复发」，检测不是预防 |
| `scripts/conflict-marker-gate.sh` | 冲突标记恒零闸（提交闸 ⑲）—— 交付件不得含未解决的 Git 合并冲突标记。🔴 立闸原因：原型导出函数里提交了冲突段而 ①–⑰ 与 analyze 全绿（标记落在 JS 块注释内，语法不坏）。⚠️ 按【行首精确 7 字符】判：本仓 charter 件用 32 个 `=` 作分隔线，子串判据会全部误报 |
| `scripts/path-anchor-gate.sh` | 路径锚解析闸（提交闸 ⑳）—— 正文反引号包起的仓内路径须解析得到。🔴 立闸原因：#36b 的 reviewer owner 锚被写成仓内不存在的路径，人工判据落点指向空文件而 ①–⑲ 全绿 |
| `scripts/legacy-name-gate.sh` | 旧模型名棘轮（提交闸 ㉑）—— `TenantCtx` 出现数只许降不许升，终值 0。🔴 立闸原因：§21.1c 称该规矩由 ⑦ 执行，**而 ⑦ 只按效果统计 PendingTenantCtx**，一般 TenantCtx 出现它不看 —— **规矩没有执行者就等于没写** |
| `specs/gates/legacy-name-baseline.txt` | 旧模型名棘轮的基线（㉑）—— `legacy_TenantCtx=40`，只许降不许升，终值 0；改完一批须同步下调 |
| `specs/tools/rv-state-check.py` | RV 状态/时点一致性单次解析 —— 替代原逐行 shell 循环（47s → 0.49s）。🔴 立件原因：原实现每行起四个进程，七千行两万余次 fork，门方 `timeout 60` 跑不完。⚠️ 自带**时限反测**（合成 2 万行断言 <5s）：selftest 夹具只有几行，覆盖不到**输入规模**这一维 |
| `specs/tools/disposition-audit.py` | 处置兑现 triage（提交闸 ⑱，**不判门**）—— 对账「RV 标了采纳」与「正文真改了」。🔴 立项原因：R2 轮3–轮4 三域独立抓到四次「标采纳而正文未动／改在了别处」，**没有一次是执笔方自查出来的**。⚠️ 误报率高（首跑 4 命中 3 误报：「一律不入正文」是规则本身、「移出表格」≠「移入沿革」），命中须逐条人判；判不了没被「」引起来的诉求 |
| `specs/evidence/EVIDENCE.md` | 证据包产出（作者侧旁证，非门方结论） |
| `specs/evidence/collect.sh` | 生成证据包：每条断言附产出它的命令，供门方复算 |
| `specs/evidence/reference-index.md` | 引用索引 |
| `specs/gates/repo-packages.txt` | SP7a 仓储包允许清单（现含 `internal/repo`，甲域 sol 附条件授权）；加条目＝放宽判据，属评审面 |
| `specs/gates/boundary-baseline.txt` | SP7a 边界棘轮基线（`open` / `escape` 各记各的，不合并） |
| `specs/gates/status-stale-exceptions.txt` | 状态一致性闸的显式例外清单，每条须带理由与归属；例外是债不是豁免 |

## 三项与 §4.1 的对应

| §4.1 要求 | 本结构 | 状态 |
|---|---|---|
| `constitution.md` 治理原则唯一载体 | `specs/constitution.md` | ✅ |
| `/speckit.analyze` 跨制品一致性检查 | `specs/tools/analyze.sh` | ✅ |
| `/speckit.checklist` → `node.acceptance` | `specs/platform/checklist.md`（+ 安全 / 跨制品两份） | ✅ **三域已产出【并执行完毕】**（26＋18＋12＝56 项），按其重走的架构评审 **verdict 全 PASS**；⇒ §0.4 第 2 项已闭 |

## 用法

```sh
specs/tools/analyze.sh     # 跨制品一致性（件与件之间）
scripts/speclint-gate.sh   # 单件闸六项（提交时自动跑）
```

## 两条设计取舍，写明理由免得被当成偷懒

| 取舍 | 理由 |
|---|---|
| **正文不搬进 `specs/`**，用装载点指向 `docs/` | ① 搬动即改路径，作废三域已 PASS 的锚；② 拷贝一份即第二事实源 —— 本轮反复栽的正是这一类 |
| **checklist 不由本席代写** | 执笔方自备 checklist 自己勾 ＝ 自签门 PASS（§4.1 · §7.2 同一条纪律） |

⚠️ **`analyze.sh` 只报机器可判的不一致。** 语义层的「引对号引错义」它判不了，
那归人审（SpecDoc E.4 #60 覆盖不到项之①）—— 不宣称能覆盖它。
