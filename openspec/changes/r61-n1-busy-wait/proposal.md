# R61-N1：确定未提交的忙等待保留在收件箱

## 来源与范围

业主 rulings `review/opus50tc2k/rulings` 的 `docs/rulings/S-TEAM-MIGRATE-HOSTED-20261017.md` 第 975～990 行，N-1（业主 2026-11-16T23:07:44Z 批准）；组长派单 S-R61-N1，2026-11-17T18:19:37Z。

目标是：对驱动能够证明从未越过提交边界的入站投递，helper 保留原 delivery_id 在持久收件箱按序等当前回合结束；等待不因时间经过而变成 unknown。收件箱容量仍有界，满时明确拒收并让发信方看见；可能已经提交的投递维持人工处置，不自动重投。

本 change 仅设计，不包含实现。实施分支应从最终 R60 集成锚 9818b979 开始，限 helper 侧；任何必须改 Hub/`internal/m8` 投递写路径的协议工作，先由组长协调，不能在此设计分支自行实现。不得改 VERSION、main 或现网。开子线程并行投递明确不在范围内。

## 需要审阅的决策

1. 持久化的 busy-wait 证明与即将提交/不确定的原子转换。
2. 忙等待时不让后续队列项越过 FIFO 头；恢复时继续同一 delivery，而非新建重投。
3. 容量满时使用一个可由 Hub 接受且会产生终态通知的拒收 ACK；现有 ACK v2 对 `failed` 限定为 `session_unavailable`，故拒收协议及 Hub 通知是实施前置协调项。
4. `SH_BUSY_ADMISSION_MAX_SECONDS` 不再限制已证明未提交的忙等待；保留配置兼容并限定为提交后的 admission/transport 预算，等待状态改用可选告警阈值但不自动终止。

## 未决事项

- Hub 是否允许 ACK v2 增加显式 `queue_full` 终态拒收原因，以及该终态通知复用 `notifyDeliveryRetryExhausted` 还是新增中性容量拒收通知。必须在实现前由组长确认协议落点。
- 对旧 helper/旧 Hub 的协商方式：不得把未知 ACK phase 静默当成成功；能力协商失败应保持原有安全语义并可观测，不能自动重投不确定投递。
