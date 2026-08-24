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
| 架构设计 | `docs/dingwei-architecture-design.md` |
| 界面契约（原型） | `docs/dingwei-prototype.html` |

## 门与状态

| 门 | 状态 |
|---|---|
| 文档执笔 | ✓ |
| 架构评审 | 🔄 **重走轮进行中**（业主 2026-08-24 裁定）—— 三域按各自 checklist 逐项执行，形成新 verdict；<br>8-24 那轮只作历史证据 |
| **checklist** | ✓ **已由架构评审门方产出** —— 见 `checklist.md`；尚未执行，checkbox 不预填 |
| 用例评审 | ✗ |
| 交付验证 | ✗ |

**`node.acceptance` 引用**：`specs/platform/checklist.md`，Git blob
`1b1ae1c440dfabc47c027c8497fd8e3e67825cf9`，sha256
`fae14d4e87fc77d4003af791527ed7dff0c18d6ef002444066296c3b1af4e103`。
执行层只读取该引用；checklist 内容与 checkbox 仅由 reviewer 修改。

🔴 **SpecDoc 生命周期仍为「草案」**：§0.4 两项生效前置中，第 1 项（本工程结构）本次落成，
**第 2 项的输入已产出，但须按 `checklist.md` 重走架构评审并取得新的 gate verdict**；
⚠️ 本口径与 `docs/dingwei-architecture-design.md` A§0.0 **已统一**（丙域 kimi P-1）——
此前 A§0.0 写「架构评审 ✓ 可作设计依据」与本句冲突，现以本句为准。在此之前，
**本 spec 尚不得被任何子 spec 引用为依据**。
