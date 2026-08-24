# DingWei 交付状态（机器生成，勿手改）

生成自 commit `f6458eae142de50bc8e23f34f1cebae1fc025e41` · 分支 `design/addendum-decisions`

⚠️ 本文件在 **pre-commit 时刻**生成，此刻本次提交尚未存在 ——
故「生成自 commit」指的是**上一个**提交；标了「本次提交中」的件，其新锚即紧随其后的那个提交。
**sha256 与行字节始终取自工作树，是当前值，不滞后。**
生成命令：`scripts/gen-status.sh`（内容全部由 git 实取，无手写值）

> 🔴 **本文件是单一状态源。** 消息会延迟、会交叉；本文件不会——它随仓库走。
> 判断一条评审意见是否已被处理：看该件「末次改动锚」是否晚于意见所针对的锚。

| 件 | 版本 | 末次改动锚 | 行 / 字节 | sha256 |
|---|---|---|---|---|
| `dingwei-platform-specdoc-v2.0.md` | **v3.13** | `4a5c1bea0b09ad6ec10ff06d75b7c0c915a883c5` | 3011 / 203021 | `8cecd5f8474358c3c978042cf01a33add19886b8e900308a3663d06e22cf3240` |
| `dingwei-prototype-v0.1.html` | **v1.12** | `47dadcea2c8cbdc2500f23b33323140fbd871e23` | 2845 / 252821 | `f92ffd1d37bc721f91778eb9370b1b21fdee716cb2b12348f08fb4cf4233d959` |
| `dingwei-architecture-design-v0.1.md` | **v0.1** | `f6458eae142de50bc8e23f34f1cebae1fc025e41` | 305 / 17767 | `49b88f29052c203711cffe0f8c5b4322d104eea88cfe4bd8442e274be66c88f3` |

## 末次改动锚之后的提交（若为空，说明该件即为仓库最新）

```
f6458ea docs: 架构设计 v0.1——补 §0.0 评审等级与已过的门;去掉内嵌 commit(修在此,前锚 c0c50cc)
c0c50cc chore: 交付状态改为拉取式单一状态源(修在此,前锚 47dadce)
47dadce fix: 原型件内版本 bump 至 v1.12;并把「版本未 bump」这道闸前移到提交时
f3e4984 proto: 原型 v1.12——丙域 kimi 一致性 21 项全修(修在此,前锚 9ed6cd1)
9ed6cd1 chore: 投递目录自检落成脚本;首跑即抓出原型内部版本标识停在 v1.8(修在此,前锚 4a5c1be)
4a5c1be fix(PB-01): 拒绝与不泄漏存在性是两件事——禁止返回「成功的空集合」;SpecDoc v3.13 + 原型 v1.11
4134196 proto: 原型 v1.10——补 §22.2c 可达入口(甲域 sol);nav↔section 22:22 一一对应
ffc6e4c proto: 原型 v1.9——修 PA-01/PA-02(乙域 terra 阻断);负测就地可跑并当场抓出一处本席自造缺陷
```
