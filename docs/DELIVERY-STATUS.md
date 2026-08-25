# DingWei 交付状态（机器生成，勿手改）

生成自 commit `cf348680094a6594fbec79a744e5b67d25f7a8a1` · 分支 `design/addendum-decisions`

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
| `dingwei-platform-specdoc.md` | **v3.40** | `cf348680094a6594fbec79a744e5b67d25f7a8a1` | 3473 / 242843 | `c6cfb269aa7a3222db2a1a9ca4437178f06cef8f2bbd35aedf513e53b4baf391` |
| `dingwei-prototype.html` | **v1.37** | `53f4033dac84c797f72ac6f01363ed6fc3a6bca1` | 2845 / 252826 | `222eccb1b650ef3132fd97bc0b7fb334037cf76948dc63bdb35be31478a1249e` |
| `dingwei-architecture-design.md` | **v0.33** | `61ad4390902debbc268aee03d1172e1a93108937` | 907 / 61534 | `e64dca22a7263ecf17cf0977cc14297d89e8ba196d913b5dea0c33ce82c47b7c` |

## 末次改动锚之后的提交（若为空，说明该件即为仓库最新）

```
cf34868 RV-乙14：乙域比甲域多抓到两处漏网冲突（v3.40）
6ac6f8c merge: 取回乙域 RV-乙14（台账两行均保留，非二选一）
fbf67ef 甲域 delta ②：跨企业 capability 现行冲突 7 处清零（v3.39）
d678483 RV-乙13：fixture 三 role 补齐、去明文口令（v3.38）
f978f02 Merge commit 'bf1a48a2' into design/addendum-decisions
e9df5dd ㉑ 重写：从「按行计数」改为「AST 按对象分类计数」（v3.37）
c077f1a C.3a.1c 特权面归平台 ops，不下放企业（业主裁定，v3.36）
e5e4ba1 review: flag cross-enterprise exception conflict
```
