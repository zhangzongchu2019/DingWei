# DingWei 交付状态（机器生成，勿手改）

生成自 commit `b71000a4c3a2c153bf9cba41ba091f7ecdf1ab46` · 分支 `design/addendum-decisions`

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
| `dingwei-platform-specdoc.md` | **v3.27** | `4633e6d415262c9a74d50b0c56a9885669c37f91` | 3289 / 244389 | `1f06d22b28045362057498cdc71e14bde0911d840f80410bbcc8f801fe784f3f` |
| `dingwei-prototype.html` | **v1.35** | `4633e6d415262c9a74d50b0c56a9885669c37f91` | 2860 / 265624 | `75e4867c59704a4123fce43886a260972e305efe1a523cc18e6c1704962a5e0a` |
| `dingwei-architecture-design.md` | **v0.30** | `b71000a4c3a2c153bf9cba41ba091f7ecdf1ab46` | 923 / 79448 | `51b594223eae582b2205693f55f766f82db9e9be1af33392e78f01f282f1fcc4` |

## 末次改动锚之后的提交（若为空，说明该件即为仓库最新）

```
b71000a Merge commit '1666d2d6' into design/addendum-decisions
4633e6d R2 轮5 甲域 sol 4 条处置；立闸 ⑳（路径锚）；--rv 47s → 0.49s
1666d2d review: cover staged deletion in evidence gate
0840d7b Merge commit 'ae758a7d' into design/addendum-decisions
fa9d6f4 RV-乙10：⑭ 改为工作区＋暂存区两侧比对；证据按乙域给的顺序重出
ae758a7 review: 标注 R2 甲域整改复核剩项
c704eb5 review: record evidence package still stale
e72a5a0 R2 轮4：甲域 sol 9 条 + 乙域 terra RV-乙09 处置；立闸 ⑲（冲突标记）
```
