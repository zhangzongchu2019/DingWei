# Proposal

## Why

The external MCP Agent path is currently a restricted temporary pair: its name, lifetime, Feishu copy, and group authorization differ from ordinary Agents. The owner has approved extending the external Agent lifecycle, Unicode display names, owner-controlled summary copies, ordinary group membership, and bounded Hub-side history. These changes need a staged design that preserves stable addressing and cannot create cross-group message paths.

## What Changes

- Set external temporary-group and message lifetime to seven days while retaining the fourteen-day token lifetime.
- Store and show the verified Feishu Unicode display name separately from the ASCII Agent address; duplicate display names remain distinguishable by canonical address and tenant label.
- 内外 Agent 复用 owner 控制的摘要抄送及每 Agent 偏好；内部默认关、外部默认开，显式偏好优先且收件人必须可信唯一。
- 外部 Agent 可加入普通组且保留原临时配对；注册允许不指定目标、不建配对。冻结并存授权、未编组MCP拒绝/Console显示、离组、AG/token到期及PM01已投影件边界；跨租户仍独立准入。
- Retain at most 500 Hub-side message records per external Agent, with visible backpressure when active records cannot be pruned.
- Include an MCP file-upload feasibility investigation only. This change does not implement upload.

## Capabilities

### New Capabilities

- `external-agent-parity`: external Agent lifecycle, identity display, message copy, group membership, and bounded history.

## Scope

Design only. No product code, database migration, configuration, deployment, or live-system changes are authorized by this change.

## Authority and sources

- Owner decisions ㊿-补129 and ㊿-补131, recorded at `review/opus50tc2k/rulings` commit `86e135328` in `docs/rulings/S-TEAM-MIGRATE-HOSTED-20261017.md`.
- Design baseline: main `206d41b26dab646c0e50a111eed5a45ccf7c72f6`.
- 本次修订依据：㊿-补132③，`review/opus50tc2k/rulings` 的 `148f1be7`，同一 rulings 文件。派单 S-R62-EXTSEAT-AMEND 原件 `b3149751b517`，补发原文2026-11-18T23:53:44Z；新分支基设计 `124d24ee3818762f977920d5a015e4dfdca80663`，只改设计；TTL `3639406c` 仍冻结。

## 设计选择与待确认假设

- A-AMEND-01：本修订选择无显式组直发的合法授权并集；两路皆有效时普通组优先、同类按稳定引用固定序。显式组不借其它授权回退。此为响应派单“并存如何授权”的技术方案，不冒称业主逐字给定的优先序，交独立复核。
- A-AMEND-02：无AG席位的地址编码和兼容接口须在切片三实现设计中具体冻结；本单确定稳定AgentID、不改已有地址、无编组不得通信，不提前发明AG号或提交接口实现。

## 待 PM 决策

- PM01：绑定授权路径失效后，已投影未发送件的撤回/重授权语义，按组长18:05:27Z裁定归切片三、内外编组一起定。本单只披露既有边界与并存影响，不改变在途件产品语义。
