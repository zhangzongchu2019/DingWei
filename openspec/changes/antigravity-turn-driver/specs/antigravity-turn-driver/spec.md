# antigravity-turn-driver Specification

## Purpose

本变更把 dwgemini4 的旧 CLI 回合处理改为 ACP 协议驱动，目的是让页面展示回答增量和明确的回合完成状态，并保持信封级去重和未知结果保护。涉及 sessionHelper 驱动与适配器，不影响 Hub、View 和其他 CLI 的传输逻辑。

## Requirements

### Requirement: ATD-001 ACP 回合事件归属

Helper MUST 将 Antigravity ACP 的回答与工具通知归属当前信封及回合，并 SHALL 在终态事件已入队后才返回完成回执。

#### Scenario: ATD-S001 正常回答
- **WHEN** ACP 对当前 session 发出回答增量并以 end_turn 结束
- **THEN** 页面事件先包含回答增量，再包含该信封的 completed 终态，回执随后为 done

### Requirement: ATD-002 信封幂等与未知保护

Driver MUST 在写入 ACP 管道前持久登记信封，重复信封不得二次提交；无法确认是否入场时 SHALL 保留 unknown，不自动重投。

#### Scenario: ATD-S002 重复或冲突信封
- **WHEN** 相同 ID 再次投递相同或不同正文
- **THEN** 相同正文返回原回执且不再写入，异文返回冲突失败

### Requirement: ATD-003 定向中断

Driver MUST 仅取消与当前信封及回合均匹配的 ACP session；成功返回 SHALL 以可观察的 cancelled 终态为依据。

#### Scenario: ATD-S003 旧回合中断请求
- **WHEN** 中断请求的回合 ID 与当前回合不符
- **THEN** 不发送取消请求并返回失败

### Requirement: ATD-004 原生证据边界

Driver MUST 不把本机时钟冒充 ACP 原生起止时刻；无原生时刻的终态由直接协议回执结算。

#### Scenario: ATD-S004 完成响应无原生时刻
- **WHEN** ACP 返回 end_turn 且不提供原生起止时间
- **THEN** 回合事件不包含伪造时刻，适配器依据直接回执完成结算
