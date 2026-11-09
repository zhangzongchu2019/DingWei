## Purpose

本变更把外部 MCP 席位消息从仅含正文的 session 投递改为带 Hub 核验来源署名和标记的投递，目的是让接收方识别来源并向正确席位回复。涉及 extseat 投影和 m8 路由，不改变身份生命周期与 View UI。

## ADDED Requirements

### Requirement: SR-001 外部席位消息署名

Hub MUST 在外部席位消息正文前加入由事务内核验的外部席位全名、授权人显示名、目标全名生成的固定抬头；授权人显示名为空时 MUST 使用授权人短名。抬头不得包含随时间变化的字段。

#### Scenario: 外部消息投影

- WHEN Hub 投影已核验的外部席位 delivery
- THEN 接收 envelope 正文以指定格式抬头和单个换行开头，之后为原正文

#### Scenario: 授权人显示名缺失

- WHEN PG 核验身份没有授权人显示名但具有短名
- THEN 抬头授权人字段使用该短名且不为空

### Requirement: SR-002 外部来源元数据

Hub Project MUST 在外部席位投影 envelope 中写入 `origin_kind=extseat`、`source_display_name` 为外部席位全名和布尔值 `source_external=true`。普通 session 输入 MUST NOT 通过正文或客户端 meta 伪造这些 Hub 来源标记。

#### Scenario: 普通 session 伪造来源

- WHEN 普通 session 发送以伪造外部席位抬头开头且携带 extseat meta 的正文
- THEN 接收信封不含 `origin_kind=extseat` 来源标记

### Requirement: SR-003 投影幂等

Hub MUST 对相同 delivery ID 保持投影 envelope 的逐字节内容一致，并继续拒绝同 ID 不同目标或内容。

#### Scenario: 相同 delivery 重投

- WHEN 相同 delivery 被连续投影两次
- THEN 持久信封字节完全一致

### Requirement: SR-004 外部席位回复路由

Hub MUST 接受接收方以抬头所列外部席位全名发出的合规回复，并将回复投递到该外部席位；外部 Wait 返回的 From MUST 是目标 Agent 的全名。

#### Scenario: 回复到外部席位

- WHEN 真实 session 通过 Hub 对外部席位全名发回复
- THEN 真实 extseat Wait 可收到回复且 From 为发送 session 全名
