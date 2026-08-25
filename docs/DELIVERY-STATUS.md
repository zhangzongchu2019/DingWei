# DingWei 交付状态（机器生成，勿手改）

生成自 commit `f834b7ea1536dd202e164c30288711f5e17186aa` · 分支 `design/addendum-decisions`

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
f834b7e gate: 补齐 §22.2c #1 的后半句——裸库句柄逃逸另立判据,基线 escape=1(前锚 709c44e)
709c44e gate: SP7a 收口判据立闸——仓储包外库句柄棘轮,基线 8 起(前锚 626af15)
626af15 docs: 会话交接件入仓,准备重启会话(前锚 dcc7c04)
dcc7c04 docs: SpecDoc v3.21 转【生效】——§0.4 两项生效前置均已闭合(前锚 0150f7b)
0150f7b fix: rule-sync 扩为逐行内容比较(结构域非阻断注 ②,前锚 e8c4f13)
e8c4f13 docs: SpecDoc v3.20——未采纳方案移出正文入参考资料(业主裁,前锚 4acd5d7)
4acd5d7 fix(FLOW-01): §5.4a 与 §5.6 对撞已解——按 §5.6 为准改回,并把「两表一致」机器化(前锚 05ccf17)
05ccf17 docs: 结构域重跑 FLOW-01/FLOW-05 全修;RLS 全扫暴露 C.3 裁定后 26 处未同步(修在此,前锚 681917a)
```
