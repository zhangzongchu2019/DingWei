## Why

外部 Agent 当前发送后必须另调收件工具才能等待回应，操作不连贯；而请求自动回程会将目标整轮输出发给外部，接收席位缺少显式提示和拒绝开关。按业主新定的五个短名，本变更一次提供等待回复、只查不取、确认清空及自动回程外泄控制，同时保持既有令牌scope与旧工具行为，降低使用成本与敏感席位误发风险。

需求来源：组长 S-MCP-P1-SENDWAIT c9a0b31684ad，㊿-补49/50 378fd2283、补52 e4b5a328a；2026-11-10T06:00Z 接管裁定。基线 LEN最终交件tip5737f089a1e2ccafa4c978e9e90ce10fa9f2921b。

## What Changes
- send/recv/check/list/clear与REST同名入口，旧三名兼容，ack不变。
- send可在受理后等待首条符合条件的回复并received，超时提示后续recv/check。
- 外部请求回程的服务器抬头提示和服务端默认关闭名单，回程生成和出口双重拒绝。

## Capabilities
### Modified Capabilities
- extseat-sender-attribution：等待收件、工具别名和外部回程控制。

### 待确认假设

补59已提供prodops只读现网443证据；50443外层不可核。本机HTTP探针不是桌面版真实客户端测试。

### 待 PM 决策

- P-01 后续PG/Console方案已由补59认可，明确另单实施，本单不创建迁移。
- P-02 补59正式定服务端上限55秒，不提供扩大到60的入口。

## Impact

extseat、tempgroup只读查询接口、mcpgw、m8；不改scope、令牌、SQLite或PG迁移，不部署或改线上配置。
