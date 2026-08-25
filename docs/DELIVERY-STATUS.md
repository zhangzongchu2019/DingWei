# DingWei 交付状态（机器生成，勿手改）

生成自 commit `c704eb53d58e70912f1b1b6aa0e1902d0f5f9f91` · 分支 `design/addendum-decisions`

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
| `dingwei-platform-specdoc.md` | **v3.26** | `e72a5a05563f6e152127c5379b7bf054711f8ccb` | 3290 / 243040 | `1b8a35e2cf8acd960ef28a58ea14d6ba3f758b35bd08686e9386b1869dfb6fac` |
| `dingwei-prototype.html` | **v1.34** | `e72a5a05563f6e152127c5379b7bf054711f8ccb` | 2859 / 265016 | `1faa018347bdcb9be629827535f57c7b83c2fabc568ef0aff63c698faa6ecd12` |
| `dingwei-architecture-design.md` | **v0.28** | `c704eb53d58e70912f1b1b6aa0e1902d0f5f9f91<br>⚠️ **本次提交中**（新锚见本文件顶部「生成自 commit」的下一个提交）` | 921 / 77858 | `4a6484347e3f70e1030672704eae4a5914b1e8ca996402f7d234607d9b5c34f9` |

## 末次改动锚之后的提交（若为空，说明该件即为仓库最新）

```
c704eb5 review: record evidence package still stale
e72a5a0 R2 轮4：甲域 sol 9 条 + 乙域 terra RV-乙09 处置；立闸 ⑲（冲突标记）
84276a6 Merge commit 'c351ab21' into design/addendum-decisions
82fc88e Merge commit '16ff70a' into design/addendum-decisions
9412991 处置兑现 triage（⑱）+ arch 一处过程叙述
c351ab2 review: keep evidence closure open
2ed414f RV-乙08：⑭ 判据由「提交距离」换为「内容指纹」
16ff70a review: 标注 R2 轮4甲域复核意见
```
