# DingWei 平台 spec（Spec Kit 装载点）

> 🔴 **本文件不是 spec 正文，是【装载点】。** 正文在 `docs/dingwei-platform-specdoc.md`。

## 为什么不把正文搬进来

Spec Kit 惯例是 `specs/<feature>/spec.md` 放正文。本席**没有搬**，理由是两条实测教训：

| # | 理由 |
|---|---|
| 1 | 🔴 **搬动即改路径，会作废三域手上全部已 PASS 的锚**（本轮已有五份评审件钉在 `docs/` 路径上） |
| 2 | ⚠️ **拷贝一份到 `specs/` 就是第二事实源** —— 本轮反复栽的正是这一类（见 constitution 第 1 条） |

⇒ **单一正文 ＋ 装载点指向它**，不复制、不搬动。

## 取件

| 项 | 值 |
|---|---|
| 正文 | `docs/dingwei-platform-specdoc.md` |
| 版本 / 末次改动锚 / sha256 | 见 `docs/DELIVERY-STATUS.md`（机器生成，随提交自动刷新） |
| 架构设计 | `docs/dingwei-platform-architecture.md` |
| 界面契约（原型） | `docs/dingwei-prototype.html` |

## 门与状态

| 门 | 状态 |
|---|---|
| 文档执笔 | ✓ |
| 架构评审 | ✅ **PASS**（2026-08-25 重走轮，三域按各自 checklist 执行）<br>⚠️ 8-24 那轮仍只作历史证据，本次 verdict 是**按 checklist 新取得的** |
| **checklist** | ✅ **已产出并执行完毕** —— 三域 26 ＋ 18 ＋ 12 ＝ 56 项；执笔方全程未碰勾选 |
| 用例评审 | ✗ |
| 交付验证 | ✗ |

**`node.acceptance` 引用**：`specs/platform/checklist.md`，Git blob
`1b1ae1c440dfabc47c027c8497fd8e3e67825cf9`，sha256
`fae14d4e87fc77d4003af791527ed7dff0c18d6ef002444066296c3b1af4e103`。
执行层只读取该引用；checklist 内容与 checkbox 仅由 reviewer 修改。

🔴 **SpecDoc 生命周期：【生效】**（权威表述见 SpecDoc §0.1 生命周期字段，本处不复述版本与日期）。
§0.4 两项生效前置**均已闭合**：第 1 项（本工程结构）已落成；第 2 项的 checklist
**三域已产出并执行完毕**，按其重走的架构评审 **verdict 全 PASS**。
⇒ **本 spec 可被子 spec 引用为依据**；生效前施加的引用限制随之解除。

> ⚠️ **本段曾与本文件上方的门状态表自相矛盾**（表里已写「架构评审 PASS／checklist 执行完毕」，
> 此处却仍写着与之相反的生命周期与前置状态），由结构域复核中抓出。
> 成因是同一当前状态在**同一文件内两存**，改了一处没同步另一处 —— 与 ARCH-03 抓过的同形。
> ⇒ 已立闸 ⑨（状态一致性）：权威状态源单一，supporting spec 内不得留与之矛盾的现状断言。
