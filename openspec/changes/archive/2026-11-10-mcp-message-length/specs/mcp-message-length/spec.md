## ADDED Requirements

### Requirement: LEN-001 统一UTF-8业务长度
服务端 MUST 将最终业务正文（包括服务器抬头）按 UTF-8 字节计算，上限1048576；等于上限允许，超限返回body_too_large和actual_bytes、limit_bytes，MUST NOT 截断或入队。

#### Scenario: 精确边界
- WHEN ASCII、中文或emoji正文含抬头后的字节为L-1、L或L+1，L=1048576
- THEN 前两者完整投递，后者拒绝且持久队列与发送键无新增

### Requirement: LEN-002 JSON传输预算
MCP HTTP与helper IPC MUST 容纳每正文原字节六倍JSON转义及65536元数据预算，且仍单独执行业务正文上限。

#### Scenario: 最坏转义
- WHEN 不超过业务预算的控制字符经JSON Unicode转义提交
- THEN 请求不因旧HTTP或IPC行上限被拒绝

### Requirement: LEN-003 双向完整传输
Hub与helper MUST 完整传输合法正文；外部到目标、目标经send.py和自动回程均执行相同字节判据，WebSocket帧预算保持16MiB。

#### Scenario: 回程超限
- WHEN 目标显式或自动回程产生L+1字节正文
- THEN 外部待收队列不增加且不会收到被截断内容
