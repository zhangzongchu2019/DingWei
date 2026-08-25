# DingWei 交付状态（机器生成，勿手改）

生成自 commit `e8c4f139f6bb51ea43c000ff813df704504b1591` · 分支 `design/addendum-decisions`

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
| `dingwei-platform-specdoc.md` | **v3.20** | `e8c4f139f6bb51ea43c000ff813df704504b1591<br>⚠️ **本次提交中**（新锚见本文件顶部「生成自 commit」的下一个提交）` | 3076 / 209898 | `03515954372165966bf9be65d10e8b4fc55b0df857c46c49ad390cf0de97cb69` |
| `dingwei-prototype.html` | **v1.16** | `05ccf171d3d45ebd25f24495af32563eb79b61b5` | 2849 / 254430 | `c9e4e0257c7ef916cf341675e69aa45de0cc39c8edd47bc5f53380d9d5795ce2` |
| `dingwei-architecture-design.md` | **v0.14** | `4acd5d762f6ae08254691a4061f54c7b8c62706d` | 796 / 56515 | `2c22da8df450a34ce9bebba8bfba67d9fa28b94b0b2e17ea240c8648deb0977e` |

## 末次改动锚之后的提交（若为空，说明该件即为仓库最新）

```
e8c4f13 docs: SpecDoc v3.20——未采纳方案移出正文入参考资料(业主裁,前锚 4acd5d7)
4acd5d7 fix(FLOW-01): §5.4a 与 §5.6 对撞已解——按 §5.6 为准改回,并把「两表一致」机器化(前锚 05ccf17)
05ccf17 docs: 结构域重跑 FLOW-01/FLOW-05 全修;RLS 全扫暴露 C.3 裁定后 26 处未同步(修在此,前锚 681917a)
681917a docs: 组 2 task 定名已裁并落三处(SpecDoc v3.18 + 架构 v0.12 + 分析件,修在此,前锚 d872147)
d872147 docs: 架构设计 v0.11——按 sol 扩后 scope 落设计合同(组5全量+组6设计半+组4设计半+组3枚举半)
1fd84e7 docs: 结构域重走 12 FAIL 分类 + 组 1 已修(修在此,前锚 62951fc)
62951fc docs: SpecDoc v3.16——业主裁定「重走」,架构评审重走轮起(修在此,前锚 bfac466)
bfac466 docs: SpecDoc v3.15——把「是否起重走轮」登记进 C.2(修在此,前锚 dd88734)
```
