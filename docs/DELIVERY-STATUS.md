# DingWei 交付状态（机器生成，勿手改）

生成自 commit `2f2a1cd5c04a33b179eff6e02c153f480a5182ee` · 分支 `design/addendum-decisions`

⚠️ 本文件在 **pre-commit 时刻**生成，此刻本次提交尚未存在 ——
故「生成自 commit」指的是**上一个**提交；标了「本次提交中」的件，其新锚即紧随其后的那个提交。
**sha256 与行字节始终取自工作树，是当前值，不滞后。**
生成命令：`scripts/gen-status.sh`（内容全部由 git 实取，无手写值）

> 🔴 **本文件是单一状态源。** 消息会延迟、会交叉；本文件不会——它随仓库走。
> 判断一条评审意见是否已被处理：看该件「末次改动锚」是否晚于意见所针对的锚。

> ⚠️ **若你是在共享目录读到本文件，它是副本，可能已过期。**
> 副本自身也是「第二事实源」—— 本席这几轮反复栽的正是这一类。
> **自检办法（不必问本席）**：把上方「生成自 commit」与分支最新比一次——
> `git -C <repo> rev-parse origin/design/addendum-decisions` 不等于它，就说明本副本落后，以仓内 `docs/DELIVERY-STATUS.md` 为准。
> 仓内那份由 pre-commit 自动重生成，不依赖任何人记得。

| 件 | 版本 | 末次改动锚 | 行 / 字节 | sha256 |
|---|---|---|---|---|
| `dingwei-platform-specdoc.md` | **v3.14** | `f2c497fc9f49cc2afd2edb5c874bec7e5edd0d66` | 3011 / 203223 | `934fda6f6b17fda04f2630835e9086fd8e201f535822365446537367aa736c71` |
| `dingwei-prototype.html` | **v1.14** | `2f2a1cd5c04a33b179eff6e02c153f480a5182ee` | 2845 / 253294 | `a6ec3d550fbeae68482fd3d04ce9268bf6c4a325a5f800a170ffc3b1dd260a30` |
| `dingwei-architecture-design.md` | **v0.8** | `b6b9a18486a8f6cb183fe2d368d78be8a18b8429` | 548 / 37352 | `37245685775fba88cb5550fae8d0f7f58b3311be3d4687d064dcaef078d748cb` |

## 末次改动锚之后的提交（若为空，说明该件即为仓库最新）

```
2f2a1cd feat: 落 SpecDoc §0.4 第 1 项生效前置——Spec Kit 工程结构(修在此,前锚 b6b9a18)
b6b9a18 docs: 架构设计 v0.8——A§0.0 同步三域 PASS,并写明两个门是分开的(修在此,前锚 f2c497f)
f2c497f chore: 收官后清两件挂账——仓内文件名去版本号 + task 表定名分析入仓(修在此,前锚 645bf36)
645bf36 docs: 架构设计 v0.7——A§ 切分反向漏改一处(丙域 kimi,修在此,前锚 d01e12c)
d01e12c fix: arch-refcheck 失败条件漏 sp_dup——「检测到了但没接进失败条件」(修在此,前锚 82501df)
82501df docs: 架构设计 v0.5——甲域复核 2 局部必改(修在此,前锚 f904a9f)
f904a9f docs: 架构设计 v0.4——AD-SA-03 按方案 A 收(乙域 terra,修在此,前锚 8173cb9)
8173cb9 docs: 架构设计 v0.3 + SpecDoc v3.14——丙域 F-1~F-5 全修(修在此,前锚 0df45c9)
```
