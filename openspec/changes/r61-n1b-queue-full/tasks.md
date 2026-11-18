# 任务

- [ ] R60 定锚后基于其新分支实现，不与当前 `session_ingress` 修改冲突。
- [ ] Hub 握手能力头、helper 能力字段与四种旧新组合。
- [ ] ACK v2 受限 `failed/queue_full/retryable=false` 校验和终态事务。
- [ ] 终态事务内写入 deduplicated durable sender notice outbox，覆盖 Hub/helper 重启及 ACK 丢失。
- [ ] session source 复用 failure_notice durable path；其他支持来源用有界 generic notice outbox，覆盖确定失败重试与不确定发送转 manual/dead-letter。
- [ ] sender/View 文案、正文不泄漏及 target 离线重试。
- [ ] 能力、reason/retryable、unknown 隔离、outbox 原子性和去重删保护变异。
