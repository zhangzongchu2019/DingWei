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
| `specs/evidence/EVIDENCE.md` | 证据包产出（作者侧旁证，非门方结论） |
| `specs/evidence/collect.sh` | 生成证据包：每条断言附产出它的命令，供门方复算 |
| `specs/evidence/reference-index.md` | 引用索引 |
| `specs/gates/repo-packages.txt` | SP7a 仓储包允许清单；加条目＝放宽判据，属评审面 |
| `specs/gates/boundary-baseline.txt` | SP7a 边界棘轮基线（`open` / `escape` 各记各的，不合并） |

## 三项与 §4.1 的对应

| §4.1 要求 | 本结构 | 状态 |
|---|---|---|
| `constitution.md` 治理原则唯一载体 | `specs/constitution.md` | ✅ |
| `/speckit.analyze` 跨制品一致性检查 | `specs/tools/analyze.sh` | ✅ |
| `/speckit.checklist` → `node.acceptance` | `specs/platform/checklist.md`（+ 安全 / 跨制品两份） | ✅ 三域已产出；⛔ **尚未执行**，须按其重走架构评审取得新 verdict |

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
