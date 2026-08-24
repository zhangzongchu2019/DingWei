# DingWei 交付状态（机器生成，勿手改）

生成自 commit `1fd84e732dc1cb8b584c5fd84e7e3937bd8cf898` · 分支 `design/addendum-decisions`

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
| `dingwei-platform-specdoc.md` | **v3.17** | `1fd84e732dc1cb8b584c5fd84e7e3937bd8cf898` | 3018 / 204535 | `55bc669cb225c387ac555e28f90bd0ee3dfa4fb3e2e6d9d787e289f48ae4ee43` |
| `dingwei-prototype.html` | **v1.15** | `185d23af0a42866374d07b05fca27e90cf678857` | 2845 / 253294 | `fa386312caa2bc78ee81e7d02a9f404dbcf34a3e0d2397f1584b0d918387048b` |
| `dingwei-architecture-design.md` | **v0.11** | `dd887340a57ccb80647461f5f0c215f24ea4f891<br>⚠️ **本次提交中**（新锚见本文件顶部「生成自 commit」的下一个提交）` | 770 / 54272 | `6a295b828b7f3b3ecd6e5bb63f720f932508877ba4f61a07e3abfc9a69650772` |

## 末次改动锚之后的提交（若为空，说明该件即为仓库最新）

```
1fd84e7 docs: 结构域重走 12 FAIL 分类 + 组 1 已修(修在此,前锚 62951fc)
62951fc docs: SpecDoc v3.16——业主裁定「重走」,架构评审重走轮起(修在此,前锚 bfac466)
bfac466 docs: SpecDoc v3.15——把「是否起重走轮」登记进 C.2(修在此,前锚 dd88734)
dd88734 docs: 架构设计 v0.10——P-1 残留三处全清(丙域 kimi 复核 R-1/R-2 + 本席自查第三处,修在此,前锚 51cec1d)
51cec1d feat: 快照包清单改由 git 导出,不手写(丙域 kimi m 类,修在此,前锚 185d23a)
185d23a fix: 丙域终审 3 阻断 + 6 次要全修(修在此,前锚 26ecb6e)
26ecb6e chore: 三域 checklist 齐备入仓 + 索引(修在此,前锚 8b173b7)
8b173b7 chore: 收安全门方 checklist(18 项) + 备重走轮的证据包(修在此,前锚 2f2a1cd)
```
