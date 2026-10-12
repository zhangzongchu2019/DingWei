# 原桌面自动唤醒：待打通的接入条件

状态：未实现。当前 desktop adapter/MCP 仅持久收件与显式读/回复，native_auto_notify_implemented=false。macOS 首轮本地 pilot 不改变这个结论。

官方依据：[MCP](https://learn.chatgpt.com/docs/extend/mcp?surface=cli)、[App Server](https://learn.chatgpt.com/docs/app-server)、[开发命令](https://learn.chatgpt.com/docs/developer-commands?surface=cli)。
App Server 文档提供 Unix control socket、JSON-RPC、thread/loaded/list（仅在内存的 ID）、turn/start 和运行状态通知。它没有证明业主正在使用的 GUI 实例已经共享了一个可供本 helper 使用的控制端点。开发命令的 --remote 说明针对 TUI，不能借用为桌面现成绑定证据；WebSocket/app-server 被文档标为实验性开发入口。

下一步需由真实 Mac 提供应用准确版本及既存受管控制端点的非秘密引用，并确认它确属当前桌面后端；只获得协议功能列表不足以成立。
在 owner 明确选中的原线程上验证接入。禁止以启动另一个 app-server/新 thread/另一个 CLI AI 会话替代，也不采用后台模拟键盘输入来伪称正式接入。

拟验证边界：只绑定 owner 明选的线程，不读取既存聊天正文/密钥/认证数据库；只处理绑定后到达的 DW 消息。原线程忙时排队、不打断用户当前轮次；只有真实 model-admission 回执才报告进入模型。实现去重、暂停、断线恢复与逐条投递关联，回复仅归属于相应新消息产生的 turn。绑定变更必须来自用户明确操作，不能由入箱消息自行请求更改。

如果该桌面版本没有合法可用的原线程控制入口，须向业主说明限制并协商功能范围，不能把 MCP 主动读取、系统通知或 CLI 交互发布为本需求的自动原会话交互。
