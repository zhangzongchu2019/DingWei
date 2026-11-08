# Helper Delivery Evidence

## Purpose

本变更把helper在原生通知丢失与入场超时后的处理从只留不确定账本改为拟议的受限只读证据对账，目的是分别核实回合完成与ACK可推进的条件。涉及Codex驱动、adapter和原生账本，本步只交分析与红测，不改变线上路由、投递数据或发布行为。

## ADDED Requirements

### Requirement: HCD-001 只凭完整原生证据推进终态
Helper SHALL 以精确thread/turn及clientUserMessageId关联和原生startedAt/completedAt核实终态，并MUST按原ACK协议代次幂等提交；缺失时间不得由当前时钟、文本、usage或文件时间补造。completed/interrupted/failed分别记outcome。

#### Scenario: HCD-S001 终态通知丢失
- **WHEN** 原生history保留完整关联及起止时间，但实时通知丢失
- **THEN** 拟议对账可持久补齐原终态；同一证据重复输入不重复ACK

#### Scenario: HCD-S002 缺时间的输出补取
- **WHEN** 只有回合完成文字或原生时间为空
- **THEN** 不推进投递ACK，保留未确认状态

### Requirement: HCD-002 入场未知只读按原ID对账
Helper MUST 在当前及可信历史绑定thread按userMessage.clientId精确查询原clientUserMessageId，处理原生分页；无匹配、歧义或查询失败 SHALL 保留unknown，永不自动重投。

#### Scenario: HCD-S003 响应丢失但原生已入场
- **WHEN** 超时后本地没有turn ID，但历史绑定thread存在唯一匹配原生turn
- **THEN** 按原生证据恢复关联，既不新建turn也不改变当前绑定

### Requirement: HCD-003 输出补取与ACK证明分离
Helper SHALL 沿用能力协商的稳定item替换输出补取；输出补取 MUST 不写inbox终态或合成可推进ACK的缺证据terminal。

#### Scenario: HCD-S004 完成输出可见但终态缺证
- **WHEN** 历史agentMessage可补显示但起止证据不全
- **THEN** 允许受控显示输出，仍不把投递标为processed

### Requirement: HCD-004 忙等待不能无条件替换健康传输
Helper SHALL 区分等待已有回合和turn/start已经发出后的入场未知；MUST 不仅因新消息等忙超时就替换仍健康的传输。真实传输失活仍须有受控恢复及防重约束。

#### Scenario: HCD-S005 旧回合长时间运行
- **WHEN** 新消息尚无turn/start且只在等待旧回合
- **THEN** 不仅因新消息入场期限而请求替换健康传输，不假造原生入场

### Requirement: HCD-005 恢复有界且不扩寻址
Helper MUST 对跨driver对象的pending证据恢复保留持久来源，按稳定终态键幂等合并，并SHALL限制对账范围与预算；扫描不全不能给阴性结论，不改变正文或子Agent的寻址语义。

#### Scenario: HCD-S006 重连或分页预算耗尽
- **WHEN** driver被替换，或查找尚未遍历所有可信历史分页
- **THEN** 不把旧对象id/View游标当原生连续证明，未完成对账仍保留pending/unknown
