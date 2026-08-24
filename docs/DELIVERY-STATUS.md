# DingWei 交付状态（机器生成，勿手改）

生成自 commit `8173cb9ff8666c53f00bd88eadf01fe3fbf01474` · 分支 `design/addendum-decisions`

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
| `dingwei-platform-specdoc-v2.0.md` | **v3.14** | `8173cb9ff8666c53f00bd88eadf01fe3fbf01474` | 3011 / 203223 | `934fda6f6b17fda04f2630835e9086fd8e201f535822365446537367aa736c71` |
| `dingwei-prototype-v0.1.html` | **v1.13** | `a2bf3226e6b959786e98583c24daee1b50016eaa` | 2845 / 253250 | `cb32d24815851e5d536e6aa73029870073dd8fae6a7eb6f7621d5752b493a8cb` |
| `dingwei-architecture-design-v0.1.md` | **v0.4** | `8173cb9ff8666c53f00bd88eadf01fe3fbf01474<br>⚠️ **本次提交中**（新锚见本文件顶部「生成自 commit」的下一个提交）` | 527 / 35371 | `ce8acfa47ebe7088d19f24aaf58fbf2d2a726ecbb528a3c80fa4e1bbee5e1a76` |

## 末次改动锚之后的提交（若为空，说明该件即为仓库最新）

```
8173cb9 docs: 架构设计 v0.3 + SpecDoc v3.14——丙域 F-1~F-5 全修(修在此,前锚 0df45c9)
0df45c9 docs: 架构设计 v0.2——甲域 4 组 + 乙域 AD-SA-01/02 全修(修在此,前锚 f6458ea)
7f75539 docs: 架构设计 v0.2——修 AD-SA-01/AD-SA-02(乙域 terra 阻断,修在此,前锚 f6458ea)
c4aa572 fix: delivery-check 同名判据改为按「仓内名有没有烂」(修在此,前锚 afaadda)
afaadda fix: delivery-check 误判状态文件同名(修在此,前锚 a2bf322)
a2bf322 proto: 原型 v1.13——丙域 4 条非阻断观察全处理,不挂账(修在此,前锚 a1593c0)
a1593c0 chore: 状态文件自述「副本可能过期」及其自检法(修在此,前锚 59e9768)
59e9768 fix: 钩子是旧拷贝导致第④⑤项从未执行——「定义了却没装上」第三次(修在此,前锚 f6458ea)
```
