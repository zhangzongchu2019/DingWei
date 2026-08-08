# C-P0-1 桥接契约面 v0.1(信道组 devo48 执笔,prodops 组内审)

对应接口契约 v0.3(sha256 头 28f09dae @ commit 107015744b0e56c9645c84eb6ed97bf479c7d918)§2.2/§2.3。
Go 面=internal/bridge/bridge.go(接口先冻零实现)。实现步 C-P0-2~5 待张宗楚确认细案后推进。

## 一、契约条款 → Go 符号 → 实现步 映射

| 契约条款 | Go 符号 | 实现步 | 用例 |
|---|---|---|---|
| §2.2 Connect/Close(→close_reason) | Conn / CloseReason | C-P0-3(hub 独立起停) | BR-01,BR-02 |
| §2.2 Deliver(delivery/request identity) | Deliverer / Delivery / DeliverOutcome | C-P0-2 | BR-03,BR-04 |
| §2.2 Events/Receipts(有序+ack) | EventStream / Event / EventKind | C-P0-2 | BR-05,BR-06 |
| §2.2 Interrupt(目标 turn/delivery→outcome) | Interrupter / InterruptTarget / InterruptOutcome | C-P0-2(管道=hub V2 terminal_interrupt,main a5f7c25 侦察档) | BR-07 |
| §2.2 错误分类五值 | ErrClass / Error.Retryable | 全步 | BR-08 |
| O3a WS≠driver | Conn 注释级不变量+C-P0-2 结构 | C-P0-2 | BR-09 |
| O3b driver health 非 WS ping | DriverHealth / HealthReport | C-P0-2 | BR-10 |
| O3c idle 仅在有 health 前提触发 | CloseIdle 约束 | C-P0-2 | BR-11 |
| §2.3 mock unlock(code→granted,reason) | Unlocker / UnlockOutcome | C-P0-4 | BR-12 |
| §2.3 stub hub ack 链可断言 | StubHub.AckChain(injected→processed) | C-P0-3 | BR-13 |
| §2.3 确定性时钟+故障脚本 | Clock / FaultOp / StubHub.Inject | C-P0-3 | BR-14 |
| §2.1 线格式(已冻) | 复用 hubdriver.WireReceipt/ParseReceipt,桥接不拆 dispatch 包 | — | 既有 M1 测试 |

## 二、边界裁定(拆分的三条硬线)

1. **桥接不拆业务包**:Delivery.Body=mcs_dispatch_v1 原文透传;回执解析(ParseReceipt 严格模式)在消费侧(调度组域)。桥接只管送达与 ack 链。
2. **幂等归 board**:重投=同 RequestID 换 DeliveryID;去重在 board O1 闸(digest),桥接 at-least-once 不去重(Event.Seq 供消费侧去重)。
3. **判活禁 presence**:HealthUnknown≠死(契约 §3.1,Type B 五死教训);在线清单只作 M1 只读判据,不进本面。

## 三、规约23 用例骨架(冻结待 prodops 组内确认;C-P0-5 集成自测逐条实现)

每条=前置/输入/操作/期望/判据,负例★:
- BR-01 正:Connect→Close(shutdown),事件账见 conn 事件含 close_reason。
- BR-02 ★:driver 挂死(health unknown)时 WS 不关(O3a);Close(idle) 在 health unknown 时被拒(O3c)。
- BR-03 正:Deliver accepted=true;判据=stub AckChain 走到 injected→processed。
- BR-04 ★:对端离线→DeliverOutcome{accepted=false,Err.Class=offline,Retryable=true};protocol_reject 不可重试。
- BR-05 正:Events 有序,Seq 单调;Ack(seq) 后重连不重放 ≤seq。
- BR-06 ★:未 ack 断连重连→事件重放(at-least-once);消费侧按 Seq 去重恰一。
- BR-07 正:Interrupt(DeliveryID)→applied;★不存在目标→Err.Class=protocol_reject。
- BR-08 ★:五类 ErrClass 各注入一次,Retryable 判定表全对(仅 offline/timeout/backpressure 可重试)。
- BR-09 ★:WS 断开期间 driver 执行继续,重连后 Events 续流(不判 driver 死)。
- BR-10 正:HealthReport 三态;★探测不到返 unknown,禁返 ok/禁判死。
- BR-11 ★:health 端点不可用时 idle 超时不触发关闭。
- BR-12 正:mock unlock 已登记 code→granted;★未登记→{granted=false,reason},fail-closed(demo MockAuth 同型)。
- BR-13 正:AckChain("")→injected→processed 三段可断言,E门夹具复用此断言面(O4)。
- BR-14 正:FaultOp 五种逐一注入,行为确定可重放(deterministic clock)。

## 四、C-P0 步映射(细案待张宗楚确认,此处只挂锚)

C-P0-1=本件(契约面冻结)✅ / C-P0-2=WS-driver 解耦实现(Type B 修) / C-P0-3=hub 独立起停+StubHub 实现 / C-P0-4=Unlocker 飞书生产件+mock / C-P0-5=BR-01~14 集成自测全绿。
验收=规约23:BR 用例冻锚后跑通即过;E门夹具(A 组)对接面=StubHub,禁另起。
