# specs/ —— Spec Kit 工程结构

落成 SpecDoc **§0.4 第 1 项生效前置**（Spec Kit 形态承载）。

## 结构

```
specs/
  constitution.md              治理原则的【唯一】载体（§4.1）
  platform/
    spec.md                    装载点 —— 指向 docs/ 的正文，不复制、不搬动
    checklist.PENDING.md       ⛔ checklist 未产出，且执笔方不得代写（§4.1 · §7.2）
  tools/
    analyze.sh                 /speckit.analyze —— 跨制品一致性检查
```

## 三项与 §4.1 的对应

| §4.1 要求 | 本结构 | 状态 |
|---|---|---|
| `constitution.md` 治理原则唯一载体 | `specs/constitution.md` | ✅ |
| `/speckit.analyze` 跨制品一致性检查 | `specs/tools/analyze.sh` | ✅ |
| `/speckit.checklist` → `node.acceptance` | `specs/platform/checklist.PENDING.md` | ⛔ **须门方产出** |

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
