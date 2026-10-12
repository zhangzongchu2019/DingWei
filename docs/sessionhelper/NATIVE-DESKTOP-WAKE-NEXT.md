# 原桌面自动唤醒：待打通的接入条件

状态：完整原桌面自动唤醒未实现。当前 desktop adapter/MCP 支持持久收件、显式读/回复与有界等待工具，native_auto_notify_implemented=false。新增 desktop_dispatch.py 为离线调度内核，未接生产/GUI transport，不改变功能状态。macOS 首轮本地 pilot 也不改变这个结论。

官方依据：[MCP](https://learn.chatgpt.com/docs/extend/mcp?surface=cli)、[App Server](https://learn.chatgpt.com/docs/app-server)、[开发命令](https://learn.chatgpt.com/docs/developer-commands?surface=cli)。
App Server 文档提供 Unix control socket、JSON-RPC、thread/loaded/list（仅在内存的 ID）、turn/start 和运行状态通知。它没有证明业主正在使用的 GUI 实例已经共享了一个可供本 helper 使用的控制端点。开发命令的 --remote 说明针对 TUI，不能借用为桌面现成绑定证据；WebSocket/app-server 被文档标为实验性开发入口。

下一步需由真实 Mac 提供应用准确版本及既存受管控制端点的非秘密引用，并确认它确属当前桌面后端；只获得协议功能列表不足以成立。
在 owner 明确选中的原线程上验证接入。禁止以启动另一个 app-server/新 thread/另一个 CLI AI 会话替代，也不采用后台模拟键盘输入来伪称正式接入。

拟验证边界：只绑定 owner 明选的线程，不读取既存聊天正文/密钥/认证数据库；只处理绑定后到达的 DW 消息。原线程忙时排队、不打断用户当前轮次；只有真实 model-admission 回执才报告进入模型。实现去重、暂停、断线恢复与逐条投递关联，回复仅归属于相应新消息产生的 turn。绑定变更必须来自用户明确操作，不能由入箱消息自行请求更改。

如果该桌面版本没有合法可用的原线程控制入口，须向业主说明限制并协商功能范围，不能把 MCP 主动读取、系统通知或 CLI 交互发布为本需求的自动原会话交互。

## 调度内核增量（本地替身核验）

`desktop_dispatch.py` 复用私有 desktop inbox，并持久化逐消息 admission 账本。原桌面绑定、加载且空闲、原子 `start_if_idle` 均须未来真实 provider 实现；当前没有 native provider，也没有后台接线。接口契约不等于官方服务已提供原子空闲启动 API。不能以先查 idle 再无条件 turn/start 实现该契约：其间用户可能开始自己的 turn。

- 默认桌面状态仍为未实现/未验证；MCP 和消息字段均不能配置 native 绑定。
- 开始副作用前提交 starting 意图；确认 turn ID 后才记 admitted。异常/取消标 uncertain，进程崩溃残留 starting。两者均阻止自动重投和后续启动，避免重复进入模型。
- 单条 admitted 也阻塞下一条，直至可信 provider 提交同 message/thread/turn 的终止回执。未实现历史查询恢复，不擅自把 uncertain 清回 unread。
- MCP 已读/ACK 与原生 admission 分开；admission 不声称 AI 执行完成、GUI 回显或回复送达。
- 绑定需非秘密 endpoint 引用及原桌面后端来源证据。loaded/list 只能证明线程已加载，不能证明该端点属于原 GUI；thread/read 的 summary 可能包含预览文本，当前内核不调用它。
- provider 必须限制元数据和本次关联的 turn；不拉用户历史，不新建/恢复/分叉线程，不自动批准请求，不覆写审批与 sandbox 策略。

验证：test_desktop_dispatch.py 8 项替身测试通过；另保留既有 desktop inbox 和 pilot 回归。非真实 Mac、非模型调用、非端到端验收。

## 明确端点的只读探针

新增 `--desktop-endpoint-probe --socket /absolute/private/control.sock --thread-id OWNER_SELECTED_ID`。仅在业主明确提供现有端点引用后使用；不枚举/猜测路径。要求当前用户所属 socket 和直接父目录私有，拒绝路径符号链接；连接前后核 dev/inode。WebSocket 官方握手由 websockets 库完成，使用整体5秒期限、64KiB帧界和通知数量上限。

只发送 initialize、initialized、thread/loaded/list；不启动 daemon、AI、线程、turn，不读历史，不回应审批。输出仅选定线程是否加载和总数，不列其他线程 ID；远端原始错误/通知正文不输出。接通仍标 original_desktop_binding_verified=false，native_auto_notify_implemented=false。私有 socket 的同用户所有权不等于来源或签名证明，尚不能用于执行接入。

本机 codex-cli 0.161.0 在独立空 CODEX_HOME 离线导出 TurnStartParams：有 threadId、input、clientUserMessageId 等，没有条件 idle/expectedTurnId 字段。该证据仅属于本机版本，不借作业主 Mac 版本结论；clientUserMessageId 也未核有幂等重放保证。不能直接实现“先查空闲，再无条件 start”作为原子忙时拒绝。真实 provider 仍待既存桌面端点来源与确切控制契约。

## 目标审计与回复回传增量

最终验收链：Hub 消息 → helper 持久收件 → **原 Codex 桌面线程自动进入模型** → 本轮最终回复 → 原消息发送方。配置向导、MCP 显式收件、端点探针均不能替代粗体环节。当前原桌面 provider 未实现，且后台调度与真实 Hub reply sender 尚未接线；0.2.2.2 仍不具备完整发布资格。

新增 `complete_with_reply`：只接受 admission 账本中同 message/thread/turn 且仍 admitted 的终止输出，输出和完成状态一次事务写入私有 outbox。目标来自已保存的原 reply_to，不由模型传入新的路由；其他聊天或重复终止输出不进入 outbox。输出最多64KiB，空输出拒绝。

新增 `flush_reply`：先持久化 sending，只有未来受管 sender 明确 durable ACK 后才记 sent 和 inbox read；异常/取消/无 ACK 留 uncertain，不重放，进程崩溃留下 sending 同样阻止后续发送。当前只有替身 sender，未向真实 Agent 发消息；普通 WebSocket send 完成或 MCP queued 结果不等于 durable ACK。此策略避免盲目重投，但 UNKNOWN 项仍可能需要受管人工核实，未声称端到端 exactly-once。

排除项：不再把重复打包、系统通知、MCP 手动读取、另起 CLI/线程或一次探针成功算作核心实现进度。保留旧 package/pins 和生产冻结；下一项仍是核确 PM Mac 桌面已有控制入口并接上原生 provider，而非扩大服务器/数据库/下载变更。

## 回传现有能力直核

`SessionHelper._make_send_handler` 明确返回 `{sent:true,to:...}`，含义仅为认证连接写入，Hub 裁决另行异步返回。新增测试调用这个实际函数（仅 synthetic websocket，不联网），确认回传内核拒绝以其返回值记 sent/已读，保留 uncertain 且不重投。不得用 `{ok:true}` 包装既有写入回执绕过 durable ACK 契约。后续需确定回复 envelope ID 与 Hub 接受/拒绝或持久投递结果的安全关联，再接 sender。

Mac 最小信息采集：从新的固定源码包解压目录运行 `PYTHONPATH=tools/sessionhelper python3 -m sessionhelper.desktop_host --desktop-app /Applications/Codex.app`。此入口只用标准库，不要求构建、安装 MCP、Hub 配对或桌面登录；如安装的是 ChatGPT.app，则选其准确路径。要求 Python3.11+。只回传 applications 中 name/version/build/bundle_id，以及 architecture；不要传配置、日志或任何认证数据。该信息本身仍不是 socket 来源证据。

## Hub → helper 显式持久入队回执（开发增量）

`outbound_receipt_v1=true` 明确启用；请求须带独立128位 nonce。Hub 只支持常规可靠 Agent 地址：组权限和属主路由仍走原 RouteEnvelope，目标不具可靠入队能力则拒绝且不降级 direct-write；飞书、selector、系统/控制信封本轮不支持。旧请求不启用此路径。

成功回执仅在 EnqueueSessionDelivery 成功返回后产生，包含 source_message_id、私有 nonce、delivery_id、state=queued、durable=true；无正文，来源为 Hub 系统地址。nonce 和启用标志在目标 queue payload 前删除，不泄漏给收件 Agent。路由返回 nil 本身不产生成功回执，故重复/静默终止不会伪造确认。队列提交后的回执丢失不撤销已提交项；helper 留 uncertain，不自行重发。

新增 helper OutboundReceiptBroker 注册 waiter 后才写认证连接，在同一连接、相同随机nonce/请求ID、Hub来源/本会话地址、version1、规范delivery_id 全符合后返回 `{ok:true,stage:hub_durable_queued}`；超时、拒绝、取消、断线不成功。控制回执不进收件箱或 AI。`_make_confirmed_send_handler` 为未来桌面桥准备，同一 recv_loop 已消费回执；旧 IPC `_make_send_handler` 保留 socket-write 语义。

该通道覆盖 Agent 目标的 Hub 持久接受，并非终端读取/AI完成/飞书平台接受确认。本轮尚未把同步 desktop dispatcher 接到原生后台任务与异步 sender；不得称自动闭环已上线。已增加 Go 真实本地 websocket+SQLite 队列检验，以及 Python nonce/连接/ACK/取消/超时回归；不连接生产或真实 Agent。

补充兼容保护：desktop capabilities 带独立一次性 cap_nonce；新 Hub 响应只表示支持 durable Agent queue 回执。Broker 在同一连接、相同 nonce、Hub 本会话地址和 version1 均核符前，不写任何正文。旧 Hub 不响应就拒绝发送；重连重新协商，不复用旧结果。普通 driver/CLI 不申请此新能力，旧 IPC 无变化。握手 nonce 只在控制面，不转给收件 Agent。

全量首次回归的6项错误来自两个精简测试 fixture 未补新组件，已补 fixture 而不削弱生产接收器；另1项真实 HOME 状态变化来源未核，不删除或恢复真实文件。随后独立 HOME 下542项全通过（在能力协商最后增量之前）；最终协商增量另有靶向回归。既有 ResourceWarning/PTY 提醒保留。`durable` 对应已有 repository 队列提交，未另行声称断电/存储故障实证。


## 原会话中的持续等待工具（开发增量）

新增 `dw_wait_message` 和 `dw_reply_message`，安装后的同一 Codex 会话可由用户要求进入持续监听。MCP 工具请求保持待完成，helper 私有收件箱出现消息后，将它返回到该次工具调用；模型处理后调用回复工具，再调用等待工具。既不需要另起 CLI/线程，也不需要猜测桌面控制 socket。

这一实现只覆盖**已有模型轮次仍在调用等待工具**的模式。用户停止、MCP 取消、应用退出、模型结束本轮或工具超时后没有继续调用，均不会自动建立一个空闲原会话的新模型轮次。不能替代原需求中的空闲自动唤醒，也不能把本地 stdio 请求 ID 关联作为真实桌面 UI 验收。完整 0.2.2.2 发布门禁仍开放。

- 单次等待 1–45 秒，默认45秒；每个 helper 只允许一个待完成监听。等待期间 MCP 仍能接收取消、查询和暂停请求。IPC 断开或服务停止会取消并等待本次 helper 线程结束。
- 等待返回消息本身不标已读，不标 model execution。超时返回 timeout；暂停返回 paused；调用取消不消费下一条消息。
- 回复只接受 message_id/body。目标读取原消息的 reply_to；不接受模型指定新目标，也不把收到的正文当配置、系统指令或批准授权。
- 发送前持久写入 sending；必须经同版 Hub 的能力协商，并收到与该连接/nonce/请求相符的 durable Agent queue 回执，才写 confirmed 与 inbox read。同一正文重复调用仅返回缓存确认，不再发送。
- 拒绝、断线、取消或超时保留 rejected/uncertain/sending；重启后不自动重试，下一次等待返回需要核实的状态。未知结果可能已经入队，不改用普通 send_message 绕过防重。
- 当前确认回复只支持可靠 Agent 目标；不支持飞书接收者或旧 Hub 的普通写入确认。Hub 入队不证明对端模型已经执行。

在实际 Mac 原会话中验收前，仍须核确应用版本/构建、运行同版测试 Hub/helper，并由用户确认同一聊天确实完成等待→接收→处理→回复→再次等待。现网 .221 没有本轮协商协议，不能直接用于这条回传验收。

### 本机 schema 事实补充

独立空 CODEX_HOME 导出的 codex-cli 0.161.0 `ThreadResumeParams` **含 excludeTurns**，可避免在 resume 响应里返回 turn 正文；不能以文档搜索未命中推断它不存在。该字段仍不解决原 GUI 来源绑定或空闲原子启动：本机 `TurnStartParams.turnTrigger` 说明已有 active turn 时可能进入 steering；`clientUserMessageId` 也没有已核的幂等保证。本机 schema 不代表业主 Mac 应用版本。


### 真机待完成的有界试验入口

`--desktop-pilot --message-after-wait` 将合成消息延后至 dw_wait_message 已挂起，复用真实 helper 等待/取消路径，适用于 PM 在原 Mac 桌面聊天观察工具返回。它不使用 Hub，不模拟 durable ACK；dw_reply_message 明确拒绝，固定 send_message 合成回复标 sent_to_hub=false。报告计数与用户看到的原会话 UI 结果分开，不计空闲原桌面唤醒或真实路由完成。具体步骤见 MACOS-CODEX-PILOT.md 第5节。


## 实际 Hub/helper/MCP 跨运行时联调门禁

`DW_DESKTOP_INTEGRATION_PYTHON=/absolute/test-venv/bin/python sh scripts/test-desktop-hub-roundtrip.sh` 在完整仓库中运行；它需要本机 Go 和 Python3.11+/websockets/pexpect。只监听 httptest 随机 loopback，所有身份、SQLite、HOME和IPC为临时私有测试资源，凭据仅通过父子 stdin，不放 argv 或共享日志。普通 Go-only 套件未指定 Python 时会明确 skip 本条跨运行时测试；canonical 脚本明确运行它，依赖缺失即失败。

三场景使用实际 SessionHelper._run_once 的接收/持久收件/worker/能力协商、真实 MCP stdio 子进程及实际 Go Hub RouteEnvelope/SQLite 队列：

- legacy 合成服务 API key：挂起同一等待请求后收到消息，确认回复的 delivery_id 在实际队列存在，目标来自原消息，in_reply_to 保留，nonce不入目标队列；相同正文重调缓存确认，不再发业务回复。
- 新平台专属 Agent `/ws/agent` 凭据：临时内存身份走正常申请/批准/领取业务层，共享 leader_only=true 特殊组；组长/组员两向路由及同一等待/回复路径通过。
- 入箱后退出共享组：发起回复时真实 Hub 按当前编组拒绝，客户端保持 unread、journal=reply_rejected，再调仍拒；观察窗内 peer 无回复帧，禁止改用普通发送绕过策略。

没有真实 Mac GUI、AI/模型调用、飞书认领或生产；专属凭据场景不是飞书开户验收，身份存储是临时 MemStore而非PG，durable队列使用现有SQLite提交语义，不声明断电实证。状态语义仍为 returned_to_mcp 和 hub_durable_queued，不是 model_executed。运行中断时测试父进程要求子进程取消/回收，超时才强制结束本轮owned子进程。

本轮仅新增联调与门禁，产品代码不变；先前6e9bc5e4b Mac源码包保留其原pins与资格，不伪称含这些完整仓库联调脚本。
