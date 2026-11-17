# Design

## 1. “确定未提交”的证据

判定必须来自驱动的提交边界，而不能从“还没看到 turn_id/ACK”反推。

- **Codex**：仅当同一 delivery 的 `_busy_admission` 明确处于 waiting，`submission_started(delivery_id) is None`，当前 transport 健康、没有 `transport_recovery_required`，且 driver 的 idle-wait 仍在等待上一回合时，才可标记 proven-waiting。`_turn_start_writing` 前的本地排队/等待是未提交证据；进入 writer 或调用 turn/start 后立即失去该资格。没有可信状态、连接被替换/不健康、等待线程丢失或进程重启后缺 marker 都不能判为未提交。
- **Claude**：仅当同一 delivery 的 `BusyAdmission` 为 waiting，仍处在 `deliver` 等待前回合终结的循环，`before_submit` 尚未成功、没有 stdin write/flush，且进程/transport 健康时，才可标记 proven-waiting。`before_submit` 成功与即将写 stdin 是提交边界；此后即使没有观察到 admitted 回执也属于不确定。
- 两者共用一个 helper 持久 CAS：`busy_waiting`（写入 wait marker）→ `submission_may_have_started`（在驱动边界前持久转换）→ authoritative `admitted`/`processed` 或 `admission_unknown`。若驱动提供的临界区无法让 helper CAS 与实际 writer 原子协调，则必须让驱动提供边界回调/锁；仅靠轮询内存标志不能作为重启安全证明。
- 任何丢失/冲突/过期证据一律 fail closed 为 unknown/manual，不发送第二次。

## 2. FIFO 与 R60 持久生命周期对齐

R60 的 `DeliveryInbox` 已按 `(received_at, delivery_id)` 取最早 `received`；`DeliveryLifecycle` 持有 `dispatch_intent_at`、`request_id`、`turn_id` 等事实，`DeliveryACKOutbox` 持久发送 queued/injected/processed/admission_unknown。`delivery_progress` 是观察层，不拥有调度、ACK 或重投权。

实现应为 inbox lifecycle 增加独立的 `busy_wait_started_at`/`busy_wait_reason` 或等价受约束状态；不要把 `dispatch_intent_at` 当作“已提交”，因为当前 driver 在等待忙回合期间已先写 dispatch intent。入队时仍只保留一个原 delivery_id；busy waiter 必须阻塞该 session 的后续头项，不得由 adapter 超时后释放队列并让后项越过。若跨 session worker 可并行，顺序至少按目标 session 保持。

实际提交前以事务/锁把 `busy_waiting` 转为 `submission_may_have_started`，并保留同一 ACK generation/outbox。Driver 产生权威 admitted/terminal 证据后，按 R60 状态机推进。`delivery_progress` 可在 busy wait期间更新为 `adapter_queued` 并发非权威等待提示，恢复时重新投影；不得用 progress 的 `unknown`/`failed` 来重放。ACK 重连仍先 drain 原持久 outbox，再接纳/处理新工作。

## 3. 超容量拒收与可见通知（N-1b；N-1a 保持现状）

N-1a 不修改容量满处理路径，不新增拒收协议，也不改变已有忙/容量回退语义；本节需求与验收格只供 N-1b 设计承接。


收件箱在事务内检查容量并仅对成功插入项创建 durable queued ACK。满时不得创建部分行、推进 progress 或写入 adapter 队列。需要对原 delivery_id 返回一次可幂等确认的 `queue_full` 终态拒收，并让 Hub 把原消息置为终态并向原发件方发送“收件箱已满，未提交，请稍后重新发送”的可见状态；不能使用当前 `busy`/`retryable`，因为 Hub 会延迟后再次投递，也不能用 admission_unknown。

**协议阻断项**：R60 Hub 的 ACK v2 当前只接受 `failed` 且 reason=`session_unavailable`、`retryable=false`（`internal/m8/delivery_ack_v2.go` 41–42）；ACK 的 `busy` 会把 delivery 延后 15 秒，而不会给发送方拒收通知。故 `queue_full` 需要 Hub 明确扩展（允许一个受限终态 reason，并生成明确容量拒收通知）或另一条现有、可证明语义等价的端到端协议。N-1 本单明确要求发信方可见，不能只在 helper 日志/本席 UI 展示。组长确认 Hub 协调方式前，helper-only 实现不得声称完成容量可见拒收。

发件人看到：原 delivery 的终态状态“拒收：收件箱已满；未提交；请在收件箱有空间后重新发送”。不自动创建副本，不把正文回显到通知。通知失败需保留 Hub 侧 outbox/重试能力或可观测错误。

## 4. Helper 重启恢复

启动迁移保留已有行。对有 durable `busy_waiting` 标记且无 `submission_may_have_started` 的行，恢复为同一个 inbox head；按 `received_at, delivery_id` 重建 FIFO，清除仅进程内 worker 引用，重新绑定当前 driver 后再继续等待。不得依赖 `pending_inbound` deque 或 `_busy_admission` 内存字典恢复顺序。

若标记为 `submission_may_have_started`，恢复为 admission_unknown/manual 并生成原 delivery 的 unknown ACK；不再自动调 driver。若 busy-wait marker 与生命周期不一致、数据库迁移失败或 driver identity/generation 不匹配，采取同一保守路径并告警。Outbox phase 唯一键和 ACK confirm 继续提供幂等性；不重复发 queued 提示作为新的 delivery。

## 5. `SH_BUSY_ADMISSION_MAX_SECONDS`

保留环境变量、配置字段和现有部署值，避免升级即产生未知配置/启动失败；调整语义：它只约束已经越过提交边界后的 admission deadline，不再是 proven busy-wait 的自动取消期限。忙等待达到该值时以限频日志/UI 告警，但不改变状态。旧环境变量若只用于 busy wait，应在发行说明中标注废弃语义并提供迁移窗口；不在 N-1 中重命名或删除。

兼容矩阵：旧 helper + Hub 不变；新 helper 保持现有 ACK 字段并在普通 waiting 期间不发终态 ACK；新 helper 与新 Hub 才启用 queue_full 受限终态；新 helper 遇到不支持拒收 phase 的 Hub 时不可假装拒收成功，应保留可见本地错误/告警并停止该项自动尝试，协议握手/能力协商由 Hub 协调项决定。不得把 queue_full 当 busy 自动循环重投。

## 6. 测试及删保护清单

所有判据须在真实持久 inbox/lifecycle + fake Codex/Claude writer 上验证；mutation 必须在对应业务断言变红，不接受仅异常、panic、编译失败。

1. Codex 与 Claude 各一格：忙等待超过配置阈值仍 pending、0 次 writer 调用；删“阈值不终止”等保护须红。
2. 边界竞态：等待→submit CAS 与 writer 竞争；只能一个赢家。取消/超时不得让后台旧线程晚到提交。删 CAS/共享锁须红。
3. FIFO：A 等待时 B 不得开始；A 结束后 A→B；更改队列顺序或让后项绕行须红。
4. outbox/progress：重复 ACK/重启不重复投递；progress waiting 不产生 ACK/重放；删 ACK 唯一键或误用 progress 作调度须红。
5. helper 重启：marker 前后各注入 crash。marker 前/已知未提交恢复原项；提交边界之后恢复 unknown 且 writer 调用数不增加。删恢复区分须红。
6. **N-1b blocked**：容量满的可见拒收待 Hub ACK v2 扩展单独实现；N-1a 保持当前行为并验证没有引入新的拒收/回归。
7. 真不确定：越过提交边界后迟到/丢失 ACK，状态 unknown/manual，自动 writer 次数仍 1；删隔离须红。
8. 配置兼容：默认、显式旧值和极小值均可解析；proven wait 不过期，post-boundary deadline 仍按策略工作。删“max 仅后置用途”保护须红。
