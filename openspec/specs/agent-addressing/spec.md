# Agent 寻址基线

## Purpose

本变更把现有主 Agent 寻址规则从散落在代码中的隐含约束改为可复核的最小基线，目的是支持后续分层寻址设计与兼容检查。涉及 identity、Hub 和调用入口模块，不影响当前产品行为或授权边界。

## Requirements

### Requirement: AAB-001 主身份解析
现有 identity 主身份解析 SHALL 只接受通过主名与用户短名校验的两段全名；不得将三段全名静默截断为主身份。传输层井号拆分不等同全名校验。

#### Scenario: AAB-001-S01 多段不是主身份
- **WHEN** 主身份 API 收到 r2.agent.zzc
- **THEN** 解析拒绝，不返回 agent.zzc 的身份
