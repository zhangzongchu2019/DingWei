# DingWei 交付状态（机器生成，勿手改）

生成自 commit `7399c685025eb73fd0144f2cc91c2c8c87f1ced4` · 分支 `design/addendum-decisions`

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
| `dingwei-platform-specdoc.md` | **v3.23** | `7399c685025eb73fd0144f2cc91c2c8c87f1ced4` | 3257 / 228396 | `b443a1083e732f527a67a63b2a64c77a0c634e743554e810251755461595f025` |
| `dingwei-prototype.html` | **v1.26** | `435bbbf24e57a9f4e36e5092bfd8fd339c2ef47e<br>⚠️ **本次提交中**（新锚见本文件顶部「生成自 commit」的下一个提交）` | 2854 / 256452 | `a9d521619d862a835d09a9e59f057331b7078ff3767d4bd9948f85ec3123daf2` |
| `dingwei-architecture-design.md` | **v0.22** | `435bbbf24e57a9f4e36e5092bfd8fd339c2ef47e` | 903 / 64245 | `1670f70356e11ece2230a164d783c2d8da7ca97f778d9540b105d165079283f8` |

## 末次改动锚之后的提交（若为空，说明该件即为仓库最新）

```
7399c68 fix: 附录 E 加 owner_sp 列（269 行全部认领）+ 立闸 ⑮ · §9.2/§15.3 同步(前锚 435bbbf)
435bbbf fix: 修闸的盲区 + EVIDENCE 重生成 + 立证据时效闸 ⑭(前锚 6d8ae86)
6d8ae86 fix: R2 轮2 —— 立【已撤销断言闸】并按其输出清零 35 处引用点(前锚 92936db)
9b4ea3f Merge remote-tracking branch 'origin/review/r2-sol-structure-round1' into design/addendum-decisions
92936db Merge remote-tracking branch 'origin/review/security-r2-r1' into design/addendum-decisions
e714a11 chore: DELIVERY-STATUS 随丙域标注合并重生成（机器生成，勿手改）
e083f81 Merge remote-tracking branch 'origin/design/addendum-decisions' into design/addendum-decisions
f93fa01 gate+docs: 评审前预修 —— 快照包三缺 · analyze 覆盖面 · checklist 落账闸(前锚 e0ce00d)
```
