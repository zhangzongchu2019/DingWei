# 提议规格

## ADDED Requirements

### Requirement: MAD-001 接入表单说明

接入表单 MUST 说明短名属于本次接入的 Codex 且系统会追加 `_AG` 编号；目标必须是系统已存在的另一个 Agent 全名，并说明接入后仅能与该目标收发。

#### Scenario: MAD-001-S01 展示身份和目标说明

- WHEN 用户打开飞书接入表单
- THEN 页面在短名与目标字段旁显示各自含义、既存目标示例及单目标通信限制

### Requirement: MAD-002 安全失败原因

授权回调 MUST 对目标不可用、组织外飞书账号、授权过期或已使用、飞书暂不可用及其它失败提供人类可读说明，同时保持 OAuth `error` 字段原语义；说明不得包含凭据、内部错误原文或主体标识。

#### Scenario: MAD-002-S01 分别呈现已定义失败

- WHEN 回调命中任一固定原因码
- THEN HTML 页面显示对应说明和原 OAuth error 值，非 HTML 响应保留 error 并附安全 error_description

#### Scenario: MAD-002-S02 未知失败安全降级

- WHEN 回调遇到未分类内部失败或来源守卫拒绝
- THEN 用户看到通用重试说明，响应不含底层错误内容

### Requirement: MAD-003 失败结构化日志

每次授权回调失败 MUST 写一条结构化日志，包含固定原因码、attempt 前八位、目标类别（存在／不存在／未核实）及 UTC 时间，且不得包含目标名、授权 code、任何 token、open_id 或请求正文。

#### Scenario: MAD-003-S01 记录最小诊断字段

- WHEN 任一 callback 分支以失败结束
- THEN 产生一条包含原因码、attempt 前缀、目标类别及 UTC 的日志，且敏感值搜索无命中
