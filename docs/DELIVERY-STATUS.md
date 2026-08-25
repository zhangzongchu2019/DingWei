# DingWei 交付状态（机器生成，勿手改）

生成自 commit `835b92ba92f49a09ab6dd1d5c210f1da6b0534e9` · 分支 `design/addendum-decisions`

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
| `dingwei-platform-specdoc.md` | **v3.21** | `dcc7c0413551a37281c3d1e7a5c07212185ff9c3` | 3091 / 211151 | `726dabee36ed29441b351a896ed3ffe000c5735f6ba759d3bcc4c2568b9cbac4` |
| `dingwei-prototype.html` | **v1.16** | `05ccf171d3d45ebd25f24495af32563eb79b61b5` | 2849 / 254430 | `c9e4e0257c7ef916cf341675e69aa45de0cc39c8edd47bc5f53380d9d5795ce2` |
| `dingwei-architecture-design.md` | **v0.15** | `dcc7c0413551a37281c3d1e7a5c07212185ff9c3` | 799 / 56456 | `3957b04075a8601b4167754cd08071c3992ef8e685cf26d2ce9b8c740b33614d` |

## 末次改动锚之后的提交（若为空，说明该件即为仓库最新）

```
835b92b docs: SP7a 设计短件——仓储包形状与 TenantCtx 落法(前锚 b7036d6)
b7036d6 docs: 交接件 SP7a 收编面写清作用域(coordinator 撤回第 3 条指令后的定论,前锚 01640ec)
01640ec gate: 按甲域 sol CR 修四条绕过 + 收窄两处过度宣称(前锚 552e92e)
552e92e docs: 实现阶段总览交底(SP0-SP8 · 依赖图 · 并行判断)入仓(前锚 722ac82)
722ac82 SP7a: 建受控仓储包 internal/repo,收编 datad 的 3 处建库,判据一 8→6(前锚 b6db59b)
b6db59b gate: 按业主裁定拆掉 _test.go 开关,回严口径基线 8,并立防回潮判据(前锚 e2574ea)
e2574ea gate: _test.go 口径改为可切换,生产码 6 / 含测试 8 两值并存(前锚 d9e74c6)
d9e74c6 fix: specs/README 的结构表漏列 8 件,改为可校验表并立闸 ⑧(前锚 a80a7f4)
```
