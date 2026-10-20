# 附：r32.1 已含 driver dw_inbox 最小方案的源码确认

精确对象：`36afa6215f00bc39a992441ab0ff8fe81442aa3e`，tree `4b4f92d1486cacc543942cb728a933423f55cb23`，VERSION `0.2.3.6`。这里只读精确 Git blob，**未调用正在运行的 helper 或重跑测试**；不能据此宣称试点当前旧 helper 已获修复。文件摘要/定位见 `docs/reviews/coder-pilot-project-context/SOURCE-READS.json`。

| 最小方案要求 | r32.1 精确文件/符号与已存在测试 |
|---|---|
| 同名 MCP 到本机 IPC | `tools/sessionhelper/sessionhelper/mcp_shim.py:301` `_desktop_inbox`，发送 `desktop_inbox_v1`；`ipc_listener.py:164` 按该类型调用非空 inbox_handler |
| 生产装配不再把 driver handler 置空 | `app.py:1357` `_ipc_server` 实参 `inbox_handler=self._make_desktop_inbox_handler(ws)`；`app.py:1365` `_make_desktop_inbox_handler` 的 `mode != desktop` 分支返回调用 `self.delivery_inbox.request(args)` 的协程。因此 **driver** 会获得 handler；该旧 R9 实现实际上涵盖其他非 desktop，本文不把这一事实误写成仅 driver 分支 |
| 同步读与本地已读独立于投递 | `inbox.py:420` `DeliveryInbox.request` 仅 status/list/read/ack；status/list/read 查询现有交付记录，ack 只写 `delivery_inbox_read`，返回 local_only=true；没有 claim_next/finish 或 Hub send |
| 独立身份维度和持久化 | `inbox.py:58` `_initialize` 建 `delivery_inbox_read`，主键 `(session_name,delivery_id)`；request 的查询、连接、确认均带 session_name；重开保留已读 |
| 有界读和拒绝接管 | list limit 1–100；read 的 body 最长 65536 字符并带 body_truncated；driver pause/resume/wait/reply 均返回 mode_operation_unsupported；status 明示 automatic_delivery_unchanged=true、mcp_wait_supported=false |
| desktop 保留原行为 | `_make_desktop_inbox_handler` 的 desktop 分支仍走 adapter.request/wait_message 与确认回信；能力上报 desktop_inbox_v1 仍仅 desktop，不能为了 driver 只读可用而谎报具备 desktop 接管语义 |

已存在正例：`tools/sessionhelper/tests/test_task_flow_r32.py`，类 `R32TaskFlowTest`，方法 **`test_driver_read_ack_never_consumes_delivery_and_is_session_scoped`**（行 15–28）：共享测试库里 a/b 两个 session 同 delivery_id，核读取正文隔离、ack 只降本 session unread、b 不受影响、ack 后 `claim_next()` 仍取得原交付、重开读标记仍在、pause 拒绝。

已存在反向格：`tests/r32-task-flow/withdrawals.py` 的 **`driver_ack_local_only`**（行 55–56），在 ack 撤修中额外把 delivery_inbox.state 改 processed，再运行上面真实用例，要求命中 **`DRIVER_READ_ACK_CONSUMED_DELIVERY`** 指定断言红。另有 `notification_not_executable` 对应任务通知不被自动当作指令。这里只确认测试源码/撤修接线，未把本次只读检查写成重新运行 PASS。

因此 r32.1 已覆盖本次诊断所建议的 driver handler、本地已读、隔席与不消费投递的**实现和对应已有回归/撤修**，无必要单做回移。源码链确认并不替代升级发布、试点真实 MCP 冒烟与 owner 提前通知；不会为此改 SH_MODE、升级现有席位或清理消息。
