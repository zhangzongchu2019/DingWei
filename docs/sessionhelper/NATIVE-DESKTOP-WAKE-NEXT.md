# 原桌面自动唤醒：待打通的接入条件

状态：完整原桌面自动唤醒未实现。当前 desktop adapter/MCP 仅持久收件与显式读/回复，native_auto_notify_implemented=false。新增 desktop_dispatch.py 为离线调度内核，未接生产/GUI transport，不改变功能状态。macOS 首轮本地 pilot 也不改变这个结论。

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
