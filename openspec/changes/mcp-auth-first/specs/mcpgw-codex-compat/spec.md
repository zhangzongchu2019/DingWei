## MODIFIED Requirements

### Requirement: MCC-001 双协议通讯编解码
网关 MUST 并存2025-06-18的initialize、notifications/initialized、tools/list、tools/call、ping，与既有2026封套按版本分流，独立严格解码；不全局放宽未知字段。2025通知成功返回202空体，不建立协议授权会话。标准initialize的未知非空版本提议 MUST 协商到支持的06-18，MUST NOT仅因提议不同拒绝；允许已知clientInfo可选description/websiteUrl/icons但不据此授权。后续未支持版本操作仍拒绝。

#### Scenario: MCC-001-S01 2025与2026同时成功
- **WHEN** 实际Codex0.167.0使用2025初始化/通知/四工具/心跳，既有2026客户端使用合法封套
- **THEN** 两者各按相应结构成功，2026原有负测保持；撤2025入口则initialize目标红

#### Scenario: MCC-001-S02 参数与版本拒绝
- **WHEN** 未知顶层/方法参数、未支持的操作版本或不合法通知id到达，或标准initialize提出不支持的非空版本
- **THEN** 前三类拒绝且无工具副作用；initialize按MAF-002协商06-18，不以宽松JSON解析接受未知字段
