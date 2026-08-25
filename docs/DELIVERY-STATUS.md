# DingWei 交付状态（机器生成，勿手改）

生成自 commit `0cc20ea006be190095f421b6029617f2e1d4c779` · 分支 `design/addendum-decisions`

⚠️ 本文件在 **pre-commit 时刻**生成，此刻本次提交尚未存在 ——
故「生成自 commit」指的是**上一个**提交；标了「本次提交中」的件，其新锚即紧随其后的那个提交。
**sha256 与行字节始终取自工作树，是当前值，不滞后。**
生成命令：`scripts/gen-status.sh`（内容全部由 git 实取，无手写值）

> 🔴 **本文件是单一状态源。** 消息会延迟、会交叉；本文件不会——它随仓库走。
> 判断一条评审意见是否已被处理：看该件「末次改动锚」是否晚于意见所针对的锚。

> ⚠️ **若你是在共享目录读到本文件，它是副本，可能已过期。**
> 副本自身也是「第二事实源」—— 本席这几轮反复栽的正是这一类。
> **自检办法（不必问本席，也不要拿「生成自 commit」当判据）**：
> 把下表的 **sha256 行** 与你自己检出里的同名件实算一次（`sha256sum <file>`）——
> 对得上即内容同源，对不上才是副本落后。
> 🔴 **不要用顶部「生成自 commit」判落后**：它天然滞后一格（本文件在 pre-commit 生成，
> 那一刻本次提交尚不存在），拿它当落后判据【必然误报】。

| 件 | 版本 | 末次改动锚 | 行 / 字节 | sha256 |
|---|---|---|---|---|
| `dingwei-platform-specdoc.md` | **v3.48** | `9d70c867f5ed20052e53a274565fb941177c48fe<br>⚠️ **本次提交中**（新锚见本文件顶部「生成自 commit」的下一个提交）` | 3509 / 251138 | `f1665ba7653b657eddbd692351051fc2a9ffd3de200cbfdc32f987dad58b53bd` |
| `dingwei-prototype.html` | **v1.38** | `9d70c867f5ed20052e53a274565fb941177c48fe` | 2845 / 252879 | `6c1f00d9c94f3f0349c79aef94cc346c306b4ed842575519ec938f53c1c231b7` |
| `dingwei-architecture-design.md` | **v0.34** | `9d70c867f5ed20052e53a274565fb941177c48fe` | 907 / 61540 | `ccda038dd0980dfeab8ad5c7801afc17d8fef6ab08217e19cac7b4b674a26b05` |

## 末次改动锚之后的提交（若为空，说明该件即为仓库最新）

```
0cc20ea 按业主派工：fixture 实跑修正 ＋ 26 处跨租户重判 ＋ 丙域落勾合入（v3.47）
3c7ef59 Merge remote-tracking branch 'origin/design/addendum-decisions' into design/addendum-decisions
9d70c86 按业主派工：fixture 实跑修正 ＋ 26 处跨租户重判 ＋ RV-乙16 裁定入案（v3.47）
5d7c3b9 checklist: 丙域跨制品清单执行落勾——10 PASS + 2 FAIL（CAC-4/5 跨制品术语依赖未同步，附可复跑证据）
bb01cd6 待裁项落进仓 —— RV-乙16 被原样转达三次而无人能判断其状态
5c103a0 RV-乙16：半采纳半驳回，两半都说清（v3.46）
ef9bda8 Merge commit '75ee1dc0' into design/addendum-decisions
d5a8cfe E.1 #150–152 的裁定依据入正文（v3.45）—— 别让它只活在消息里
```
