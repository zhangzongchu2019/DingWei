# View 历史来源 provenance

## ADDED Requirements

### Requirement: HP-001 历史保存 Hub canonical 来源

Hub MUST 为可信 primary 入站投递持久保存可精确关联的最小来源凭据，并在唯一匹配同一目标会话的 admitted user_input 时生成并附加 Hub attested provenance 后持久化。该凭据不得包含重复消息正文，helper 提交的 provenance 不得被信任。

#### Scenario: 入站 Agent 与 timer 来源写入历史

- **WHEN** Hub 接受可信 primary Agent 或 timer 投递，随后收到同 delivery 的 admitted user_input 并将其写入历史
- **THEN** 历史 user_input 含由 Hub 生成且可与 canonical 投递精确关联的 trusted provenance，且来源类型和显示名与 canonical 来源一致

#### Scenario: provenance 伪造或关联不唯一

- **WHEN** helper 自带 provenance，或 delivery ID 缺失、匹配不唯一、目标会话不符或凭据过期
- **THEN** Hub 丢弃 helper 声明且不向历史事件附加 trusted provenance

### Requirement: HP-002 历史分页回放来源

Hub MUST 通过既有 `/view/{session}/history` 分页 envelope 原样回放已保存 user_input 的 Hub provenance，renderer MUST 只按既有 trusted 且 Hub-attested 规则显示来源图标。

#### Scenario: 刷新后回放可信 Agent 与 timer 卡

- **WHEN** View 刷新并从历史分页加载带有效 Hub provenance 的 Agent 或 timer admitted user_input
- **THEN** 对应 DOM 卡片显示恰好一个与来源相符的图标，且不依赖实时 input_posted 已在浏览器内存中

#### Scenario: 未证明的历史来源

- **WHEN** 历史事件没有 provenance、Hub attestation 不成立或 trust_decision 不是 trusted
- **THEN** renderer 不显示来源图标，且不从文本、data.source、delivery_update 或相邻事件推断来源

### Requirement: HP-003 来源凭据有界保存

Hub MUST 令来源凭据与对应历史保留期及容量配额一致，且不得复制 input_posted 的正文；清理历史或 generation 时 MUST 清理相关来源凭据。

#### Scenario: 保留期与配额

- **WHEN** history retention 或 Agent/global 容量清理删除对应 user_input 历史
- **THEN** 相关来源凭据也在同一清理边界内删除，且无孤立的无界来源记录

### Requirement: HP-004 历史来源回归具备变异敏感性

实现 MUST 提供带 SSE id 的 Agent 与 timer 历史刷新回归，断言图标数为 1；删除 Hub 持久 provenance 字段或 renderer Hub trust 判定时，对应业务断言 MUST 失败。

#### Scenario: 删除字段的变异

- **WHEN** 回归运行于移除 Hub 附加 provenance 字段的变异版本
- **THEN** 刷新后的图标数为 0，且期望图标数为 1 的业务断言失败

#### Scenario: 删除信任判断的变异

- **WHEN** 回归运行于移除 renderer Hub attestation/trusted 判定的变异版本，并输入未证明 provenance
- **THEN** 未证明事件错误显示图标，且“不得显示未经 Hub 证明的图标”业务断言失败
