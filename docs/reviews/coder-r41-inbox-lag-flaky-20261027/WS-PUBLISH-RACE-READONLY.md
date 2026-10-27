# 第 8 项追加：S-HUB-WS-PUBLISH-RACE 登记建议（只读分析）

分析时间：2026-10-27 UTC。请求方：opus50tc2k.zzc，17:39:22Z。状态：**建议登记，是否并入 r41 由组长决定；不阻断第 8 项测试修复。**

源码锚：工蜂 `wego-ai/dingwei`，第 8 项 `bb4bb09765739f69a3bc0b46eba06d4f2fbf2a47`，tree `79a4347c5f53417a82bfc516454c5d31c7af0893`；其产品文件与基线 `549f64e280f2c5496bffb506c709c1bf0a1c504d` 相同。以下行号均对该锚。这次只读源码、追加本文；没有改产品、测试或复现脚本，没有现网探测、部署、重启或数据库写入。结论是源码可达性，不是已证实现网发生过丢信或估算发生率。

## 1. 101 后、发布前真实 helper 是否可发/可收

**可以发送帧，也可以收到 Hub 直接写入的上线指南；但此连接的业务帧要到自身发布后才被 Hub 读取。真正触发本竞态的是另一个已在线发送方，在目标尚未发布时路由到它。**

| 位置 | 精确证据 | 含义 |
| --- | --- | --- |
| Hub 升级 | `internal/m8/hub.go:1107-1125`，`websocket.Accept` | 101 在发布前完成；依赖固定为 `github.com/coder/websocket v1.8.13`（`go.mod`），该版本 `accept.go:151` 写 101、`:159` Hijack，`:170` 才返回连接 |
| 升级后工作 | `hub.go:1130-1156` | 新端点建立 capability-pending admission，递增数据库 conn_seq，构建 client、启动 watchdog，同步渲染/发送上线指南；均处于 101 后、发布前 |
| Hub 发布 | `hub.go:1166-1185`，赋值在 **1181** | 持 `h.mu` 将 client 放入 `sessionClients`；此前在线检查看不到新 client |
| Hub 读首包 | `hub.go:1269-1286` | 在发布之后才进入 read 循环，并消费 capabilities；同一新连接的提前发帧只会缓冲，不能在自身发布前进入业务路由 |
| 端点解析 | `hub.go:3274-3290`、`:3299-3329`；`hub.go:2277-2295` | 在线快路径依赖 sessionClients；短名解析枚举在线 client，缺失时可返回裸 `errGroupAgentNotFound` |
| helper 首包 | `tools/sessionhelper/sessionhelper/app.py:746-782` | `websockets.connect` 握手返回，读取响应头、绑定连接身份，然后立即 `await ws.send(session_capabilities_envelope(...))`，不等待 Hub 发布确认 |
| helper 后续发送 | `app.py:783-818`、`:1554-1576`、`:406-410`、`:1876-1909` | 首包发送后启动 IPC、收包与业务协程；producer 有输入可继续发；IPC 未协商回执时直接 ws.send；outbox 每秒读取新增行。这些路径没有通用的“发布就绪”ACK屏障 |
| 目标提前收包 | `hub.go:1153-1156`、`:3389-3395`、`:3426-3447` | Hub 有意在发布前同步发送 onboarding，保证它先于目录广播；真实 helper 的接收循环可以接到它。收到指南仍不能作为“已发布”证明 |

首包时序：helper 收到 101 → capabilities 写到 socket → 启动 IPC/接收/其他循环；Hub Accept 返回 → 数据库 conn_seq → 指南发送 → 发布 client → read capabilities → 更新能力/返回 outbound_capabilities。两端可并行，101 与指南都不是发布屏障。

须区分“当前 helper 的 legacy 模式”与“无 ACK 旧客户端”：`app.py:703-705` 即便 processing_mode=legacy 仍声明 `delivery_ack_v1=True`；第 8 项 target 是**不发送 capabilities、没有持久 ACK 能力的旧客户端**。不能据此把全部现网 0.2.3.14 helper 视为该旧 target。

已存在的本地证据（本次不新增运行）：第 8 项自然基线 54/60；既有 `sessionBeforePublishHook` 卡住真实认证 target 后，原方法 10/10 在原接收断言超时，且已观察到原消息的拒绝通知。它证明该调度窗口可达，不代表现网频度。当前实际注册名、地址用法、历史端点 ACK 位和旧客户端数量未经现网查询，不能给出“全部现网 helper 会丢信”的结论。

## 2. 后果和 durable 边界

不能一概说所有 101 后发送都会丢失。下面针对另一条已发布连接发向尚未发布目标、当前无可用旧 client 的情况；原组成员关系仍须通过鉴权。

| 目标/路由条件 | 结果与代码 |
| --- | --- |
| 第 8 项那样的短名、无持久 ACK，且没有命中公开全名分支 | 在线快路径失败、短名在线解析无匹配，`hub.go:3329` 返回裸组错误；**在调用 routeToSession/EnqueueSessionDelivery 之前已拒绝，无目标 durable 行，无自动补投** |
| 同真实 key 的精确地址，端点已持久 ACK=true，且为可靠业务信封 | `hub.go:3283-3290` 可走离线 durable；`routeToSession:4170-4178,4216,4255-4265` 持久化，不因这段发布窗口必然丢失 |
| 有效公开全名 `name.owner` | `newplat_session_name.go:22-35` 从身份权威解析；`hub.go:3306-3311` 可绕过短名在线枚举，继续到 routeToSession，但能否接受仍取决于下面能力条件 |
| 全名已解析、新端点 admission 已建立、可靠信封、仓储支持 pending | `hub.go:1130-1132,4178,4255-4265` 可落 **capability_pending durable**；不是本项短名失败那条路径。后续 ACK=true 才转入可靠投递；ACK=false 会终结并发失败通知；一直不报能力的旧客户端则等待 pending 到期，由清理器处理（`hub.go:4404-4418`、`fresh_target_delivery.go:178-195`） |
| 全名已解析，但目标是历史 no-ACK 重连 | `hub.go:1072-1077` 明确不建 fresh admission；无旧 client 时 `:4206-4214` offline_rejected，不能凭 101 推定可可靠投递 |
| 新端点，极早的 Accept 返回至 beginCapabilityPendingAdmission 之间 | 标记也在 101 **之后**建立，`hub.go:1130-1132`；若另一发送方此时已解析全名，且无持久 ACK/旧 client，`:4178` 不满足，仍可能 offline_rejected。这是源码推导，现有第 8 项受控证据没有单独覆盖这个更早切点 |

前两条中的同真实 key 限定不能省略：helper 本地占位 key 可能是 `enrolled`（Hub `:1311-1318` 只重写 from）；短名+占位目标 key 并不一定命中 `:3283` 的同 key 离线兜底。符合当前命名规则的正式公开全名走身份解析，不能将短名测试的拒绝率直接外推到正式全名流量。

拒绝后的处理：

- 普通/observational 消息：Hub `:1407-1408` 转入 `writeDeliveryFailure`，`:3631-3645` 记错误 dock、按版本决定是否回发送方；这只是失败通知，不是原消息排队或重试。旧版本还可能收不到通知，但 dock 记录先于版本门。
- 当前 helper 的普通 IPC：未协商回执时 `app.py:406-410` 的 sent 只证明写入本地 socket，返回 unknown；协商后 observational 请求在没有 queued receipt 时可超时返回 unknown（`:391-396`），没有自动重发。
- 显式 confirmed outbound：`internal/m8/outbound_receipt.go:33-48` 遇路由错误返回 rejected / durable=false；helper broker `outbound_receipt.py:46-76,109-114` 关联回执，不自动重发。该路径不必经过普通 cross_group_denied 日志，因此不要说所有拒绝都会出现同一日志。
- helper 重连循环恢复连接，不恢复这些未被目标 durable 接受的业务发送。`outbox_loop`（`app.py:1895-1909`）在发送前推进读取位置，也没有失败消息重放机制。上游若自行保存/重试是另一层能力，本分析不能假设它必有或必无。

因此，对第 8 项被拒的那次发送，准确说法是：**目标未收到、未进目标 durable、这条 Hub/helper 路径无自动重试；若上游不另行重发，则本次投递丢失。** 可有日志/失败通知，不等于正文仍可自动补投。

### 原因码应细分

是。`hub.go:3329` 是解析不到在线目标返回的裸 `errGroupAgentNotFound`；`grouping.go:26-31` 对没有 typed reason 的错误默认报 cross_group_denied；`hub.go:3631-3634` 用此值记录失败。真正共享编组不允许则来自 `newplat_grouping.go:43-50` 的 typed cross_group_denied。两种来源同名，确实会误导排障。

建议服务器内部结构化原因分别使用：`target_admission_pending`（仅确认有当前认证 admission）、`target_admission_timeout`、`target_offline_or_unresolved`（无当前标记）、`cross_group_denied`（实际 ACL 裁决）。未知身份/未获授权的调用方继续收到现有泛化外部错误，不因此枚举他人端点、在线状态或私有组信息。不能把现有所有裸组错误简单改成“发布竞态”，它也覆盖真实离线/未知目标；同样不能只改默认值影响所有调用点。

## 3. S-HUB-WS-PUBLISH-RACE 登记与修法风险

建议登记：**已认证 WS 的 transport-ready 与 routing-ready 之间存在竞态；短名/no-ACK 目标可拒绝本应在线直投的消息，且诊断原因误分类。** 建议优先级 P2（建议值，待组长裁定）；若现网采样证明影响关键业务或持续丢信，再升级。当前只有可达性和既有本地故障证据，没有现网发生量。

| 方案 | 评价/风险 |
| --- | --- |
| 直接把 sessionClients 赋值挪到 101 前 | **不建议机械前移。** `Accept` 返回后才有可用 websocket.Conn；预先暴露 nil/半初始化 client 会让在线目录、c.write、capability/metrics、替换/清理路径看到假在线。还会破坏“指南先于目录/业务帧”的现有约束。若改为 pre-admission reservation + ready 状态、失败回滚、并发同名裁决，需要状态机重构，风险高，不宜作第 8 项附带小修 |
| 解析时只对正在握手的认证端点短等（推荐另立小方案） | 在 101 **之前**为规范身份注册独立的 admission/generation + completion 通知，成功发布或失败都结束。路由只有在经过身份/ACL校验、精确指向该标记时才等；不对任意未知/离线/模糊目标普遍 sleep，不持 h.mu/client锁/数据库授权事务等待。发布后重新解析、重核当前连接代际和 ACL，再执行原 durable/direct 决策；超时/取消/认证失效给内部准确原因码，不回退到越权直投 |
| 仅提前发布 ready client 到 Accept 之后、指南之前 | 缩短窗口但仍晚于 101，不能消除竞态，且可能让目录或业务帧抢先指南；不建议声称是完整修复 |
| 只细分日志原因 | 诊断修复风险较低，可以单独评审；不恢复已拒原消息，也不替代 admission 修复 |

短等风险中等：每个发送方有新增尾延迟，连接失败/恶意握手可耗尽等待槽；需有限并发/每来源预算、请求截止时间、成功/失败/超时/取消计数、失败 marker 回收。等待预算应依据 handshake 阶段观测确定并冻结；测试的 10 秒容错上限不是生产等待预算。尤其要审计任务/授权事务里的路由调用，避免等待目标初始化时互相持锁阻塞。

保留现有 ACK 持久队列与 capability_pending admission 的 INSERT/resolve 同锁顺序，不扩大 legacy 客户端的“持久已接受”语义，不把无 ACK 直写称为 model admission。公开 full_name、旧同 key 精确地址、短名/选择器需分别覆盖，不能靠“所有目标都解析成离线身份”扩大可达权限。

若获准实施，验收应覆盖：101 后/marker 建立前后/发布前/能力确认前四个切点；旧 no-ACK、新 ACK、已持久 ACK 重连；短名与正式全名；另连接并发发送、失败或取消的握手、同名替换/代际、撤权与跨组拒绝、首次指南顺序、durable恰好一次；已入 durable 的不重复发送，未入队的失败原因准确。测试用事件屏障而非睡眠制造调度。该验收清单是未来建议，本次没有执行或宣称这些格已通过。

结论：可登记产品问题，保持第 8 项测试修复独立；是否并入 r41、生产预算和具体实现由组长批准后另行冻结。本次没有新增产品实施或发布动作。
