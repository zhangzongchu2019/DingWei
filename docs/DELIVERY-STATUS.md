# DingWei 交付状态（机器生成，勿手改）

生成自 commit `d67848357bc7c945d152a55d3e08a898c6c27d0a` · 分支 `design/addendum-decisions`

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
| `dingwei-platform-specdoc.md` | **v3.39** | `d67848357bc7c945d152a55d3e08a898c6c27d0a<br>⚠️ **本次提交中**（新锚见本文件顶部「生成自 commit」的下一个提交）` | 3471 / 240838 | `d09aa84e5ec3ab96343616df8e0168d6b7787c64589949c4708d2a571f0b7ba3` |
| `dingwei-prototype.html` | **v1.37** | `53f4033dac84c797f72ac6f01363ed6fc3a6bca1` | 2845 / 252826 | `222eccb1b650ef3132fd97bc0b7fb334037cf76948dc63bdb35be31478a1249e` |
| `dingwei-architecture-design.md` | **v0.33** | `61ad4390902debbc268aee03d1172e1a93108937` | 907 / 61534 | `e64dca22a7263ecf17cf0977cc14297d89e8ba196d913b5dea0c33ce82c47b7c` |

## 末次改动锚之后的提交（若为空，说明该件即为仓库最新）

```
d678483 RV-乙13：fixture 三 role 补齐、去明文口令（v3.38）
f978f02 Merge commit 'bf1a48a2' into design/addendum-decisions
e9df5dd ㉑ 重写：从「按行计数」改为「AST 按对象分类计数」（v3.37）
c077f1a C.3a.1c 特权面归平台 ops，不下放企业（业主裁定，v3.36）
57a804a 丙域 delta ③④：跨租户 51 处逐处重判 + §22.1c 存在前提修正（v3.35）
bf1a48a review: add RLS fixture security finding
f78aabd RV-乙12：人层 RLS 四条硬合同补齐 + 三条 fixture（🔴 未实跑）
568cdf9 Merge commit 'cdc8270d' into design/addendum-decisions
```
