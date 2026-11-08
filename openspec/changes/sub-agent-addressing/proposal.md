# 子 Agent 分层寻址

## Why

一个 DingWei 地址背后常有多个模型会话（ts3dev55：01a1b202／01a1b6fa；coder56solzzc：01a19c8c／01a1b9ec），而地址只能投到 helper 绑定的那一个；正文里写「致某会话」不起路由作用，已造成错路由执行与派单无人接。业主不赞成「一个会话一个地址」，要求在现有路由体系上实现子 Agent 路由，并把层数上限写入平台规则。

来源：rulings 〔51〕、〔51〕-补1（业主 2026-11-08T10:34:55Z，dw-dlv_9fdd06a98e071a1a284b98707d826459）、〔51〕-补2（业主 2026-11-08T10:41:28Z，dw-dlv_f0dd4b425ce327a74d8c530c9f4e5dfa）。设计：docs/design/SUB-LANE-ROUTING-PROPOSAL.md。

## What Changes

- 全名从右往左分层：`用户` ← `Agent` ← 子 Agent（最多 3 级）。
- Hub 侧解析与校验；子 Agent 不存在 ⇒ 拒收并提醒发送方「子 Agent 不存在」。
- 提及只认主 Agent 两种写法。
- 第一期只做 Codex 席 helper 管理的一级子 Agent（并列会话）。

## Capabilities

### New Capabilities
- agent-addressing：DingWei 平台 Agent 与子 Agent 的寻址规则。

## Impact

Hub（地址解析、投递校验、回程、在线清单）、sessionHelper（子 Agent 树上报与分道驱动）、send.py、View、提及解析。本变更登记完整一期规则；当前实现任务仅交付先行兼容拒收保护，子 Agent 创建、注册与投递能力仍由后续任务交付。

## 第一期设计补充与兼容拒收（2026-11-08）

派单 S-SUBAGENT-ADDRESSING-DESIGN，详见 docs/design/SUBAGENT-ADDRESSING-PHASE1.md。AA-001～006 保留编号，按最新派单细化 ended 与 unknown；补 AA-007～010 覆盖生命周期、协议来源、持久化回退和一期兼容。〔51〕-补10 已批准生命周期默认。本次只实施 AMR/Hub/send.py 拒收护栏，不开放子 Agent 能力。

## 待确认假设

A-01：一期选用broker管理实验dynamicTools作为子发信可信入口；已验证锁版schema，不等于真实工具调用来源闭环，实施前需完成原生工具调用负测。
A-02（〔51〕-补5 已裁定）：默认名取无连字符UUID的随机末8位，冲突逐步加长尾段，旧名不变；不再使用时间戳前缀。
A-03：历史external两段主名继续兼容，一期不开放其子能力；不以本变更改历史主身份。

## 待 PM 决策

PM-03 墓碑保留：持续保留或定义安全归档策略；建议一期持续保留，不能复用旧别名，也不能把2d最长存活或5d平台上限当墓碑TTL。
