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
| 架构评审 | ✓ 2026-08-24 三域 PASS |
| **checklist** | ⛔ **未产出** —— 见 `checklist.PENDING.md` |
| 用例评审 | ✗ |
| 交付验证 | ✗ |

🔴 **SpecDoc 生命周期仍为「草案」**：§0.4 两项生效前置中，第 1 项（本工程结构）本次落成，
**第 2 项 checklist 未闭** ⇒ 按 §0.4，**本 spec 尚不得被任何子 spec 引用为依据**。
