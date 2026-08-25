# DingWei 交付状态（机器生成，勿手改）

生成自 commit `d940b17c8cda2fbf12c6fda5bd856070faf9b52a` · 分支 `design/addendum-decisions`

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
| `dingwei-platform-specdoc.md` | **v3.29** | `1085ff0609ebadd951f59226632e401c4f2f769d<br>⚠️ **本次提交中**（新锚见本文件顶部「生成自 commit」的下一个提交）` | 3256 / 221329 | `0dc2141f07ccf3bd9ef26176ac718a3ce5ef8556a3a36eab5388fbacebbbc6f2` |
| `dingwei-prototype.html` | **v1.37** | `1085ff0609ebadd951f59226632e401c4f2f769d<br>⚠️ **本次提交中**（新锚见本文件顶部「生成自 commit」的下一个提交）` | 2845 / 252826 | `222eccb1b650ef3132fd97bc0b7fb334037cf76948dc63bdb35be31478a1249e` |
| `dingwei-architecture-design.md` | **v0.32** | `d940b17c8cda2fbf12c6fda5bd856070faf9b52a<br>⚠️ **本次提交中**（新锚见本文件顶部「生成自 commit」的下一个提交）` | 907 / 61534 | `605396fed102de54ee950bdbbdba61ec5355f553ff26d3d5a892af55cfc25ecc` |

## 末次改动锚之后的提交（若为空，说明该件即为仓库最新）

```
d940b17 ⑭ 补按暂存区【模式】判：内容哈希相同的符号链接条目此前会被放行
1085ff0 R2 轮6 甲域 2 条：⑳ selftest 假通过（甲47）+ 原型会话语境残留（甲48）
c74f246 Merge commit '20bd1d8b' into design/addendum-decisions
20bd1d8 review: 标注 R2 甲域轮5复核剩项
7480f92 RV-乙11：暂存区缺 REVIEWED 路径由「跳过」改为「拒绝」
b71000a Merge commit '1666d2d6' into design/addendum-decisions
4633e6d R2 轮5 甲域 sol 4 条处置；立闸 ⑳（路径锚）；--rv 47s → 0.49s
1666d2d review: cover staged deletion in evidence gate
```
