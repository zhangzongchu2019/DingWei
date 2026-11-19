# 设计

## 分类与边界

重试仅由 Codex 预提交路径显式调用：Transport TimeoutError、ConnectionError、OSError；或 initialize/thread/read/thread/resume 上的临时 server RPC 错误（-32603、-32000 至 -32099）。确定拒绝按入口区分：本次 delivery 的首次直接 `start()` 中，Codex initialize 明确返回 authentication rejection（例如 401），确认没有 thread/start／turn/start 后，写入不可重试的 `failed/session_unavailable` 终态并回报 Hub；它不是「等待人工处理」状态。若此前暂错后由 `_recover_transport` 启动 replacement，replacement `start()` 的 initialize 认证拒绝仍按恢复失败隔离：不重试、不发 terminal failed ACK，原件保留 FIFO 头并告警。thread/read 或 thread/resume 的认证／绑定拒绝，以及无效／丢失 thread、cwd 或 direct-input 不匹配、daemon identity／私有 endpoint 身份错误，也保持此 operator hold。thread/start 不是只读校验，响应丢失时继续按 unknown/manual 处理。

## 等待与恢复

调用现有 BusyAdmission.waiting 持久化 `busy_wait_started_at`。该标记必须先写成功，delivery 才进入无限期等待；退避为 1/2/5/10/30 秒封顶循环，`SH_BUSY_ADMISSION_MAX_SECONDS` 仅控制告警。当前 delivery 保持 FIFO 头，后项不越过。Helper 重启后 marker 存在且 submission marker 为空时由既有恢复路径重建同一头；提交边界已写入时恢复为 admission_unknown，不得再次 turn/start。

## Hub 与兼容

持久 busy marker 写入后，helper 发送 ACK v2 `state=busy`、`reason=presubmit_validation_wait`、`retryable=true`，不写 helper terminal ACK outbox，也不放宽 `failed/session_unavailable` 规则。Hub 按 `DeliveredAt` 区分处理：尚无 `DeliveredAt` 时沿现有 busy→queued 路径重排；`DeliveredAt` 已写入时，busy 只是状态信息，Hub 保持 `delivered`，不清除收件确认、不再次投递。此时 helper 的持久 FIFO 负责继续等同一队首并在恢复后只提交一次。旧 Hub 0.2.3.40／501e5b8a 的行为符合该边界，无需修改 Hub。

## 测试与保护

1. 临时 read/resume/initialize 错误后恢复：marker 先持久化；校验成功后 boundary 先落库、turn/start 恰一次。
2. 持久 busy 头阻塞后项；重启 marker-null 恢复原头；submission marker 非空变 unknown 且不重投。
3. 首次直接 start 的 initialize 明确拒绝认证且 submission marker 为空：终结为不可重试 failed，不发 thread/start／turn/start；此前暂错后 replacement.start 的 initialize 认证拒绝、thread/read／thread/resume 认证拒绝及身份／cwd／direct-input／daemon 不匹配则留在 FIFO 头，输出 operator alert，不换线程、不提交、不发 terminal failed ACK。
4. 忙等待越过 SH_BUSY_ADMISSION_MAX_SECONDS 仍不取消；分别覆盖 `DeliveredAt` 为空时 Hub 重排、非空时 Hub 保持 delivered 且 helper FIFO 恢复后同一 delivery 恰有一次 turn/start。

变异 G1 删除 retryable transport 分类；G2 将临时 validation error 改为 failed/session_unavailable；G3 移除持久 busy marker／允许后项越过；G4 允许 submission marker 已存在的恢复路径再次 turn/start。每刀须由业务断言转红。
