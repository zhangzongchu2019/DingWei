# Agent 寻址基线

## Purpose

记录现有主 Agent 寻址行为，整理散落在代码中的主身份解析约束，形成可复核的现状基线，供后续分层寻址设计与兼容检查使用。涉及 identity、Hub 和调用入口模块，不影响当前产品行为或授权边界。

## Requirements

### Requirement: AAB-001 主身份解析
现有 identity 主身份解析 SHALL 只接受通过主名与用户短名校验的两段全名；不得将三段全名静默截断为主身份。传输层井号拆分不等同全名校验。

#### Scenario: AAB-001-S01 多段不是主身份
- **WHEN** 主身份 API 收到 r2.agent.zzc
- **THEN** 解析拒绝，不返回 agent.zzc 的身份
