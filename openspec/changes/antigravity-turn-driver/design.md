# Design

AntigravityProtocolDriver 在单个 `agy acp` 进程上保持一个 ACP session，以 JSON-RPC `session/prompt` 发送回合。`session/update` 中的 `agent_message_chunk` 与工具事件映射为现有 `SessionEvent`；`session/prompt` 的 `stopReason` 映射为 `turn_completed`。这两个事件按同一读取线程顺序入队，终结事件入队后才把投递账本改为 done。

SQLite `deliveries` 按信封 ID 保存正文哈希、回合 ID、状态和细节。写入前登记 unknown，防止进程在写管道后死亡时重发。收到归属当前 session 的回答或工具通知后才记 accepted。ACP 没有原生起止时刻，生命周期不伪造时间；adapter 使用直接完成回执结算。

`session/cancel` 只发送给当前信封与回合匹配的 session，等待可观察的 cancelled 终态后才返回成功。进程死亡时保留 unknown 并允许后续新信进入新 session。能力标记如实声明无跨进程事件重放、无人类附着及无重启会话存续。
