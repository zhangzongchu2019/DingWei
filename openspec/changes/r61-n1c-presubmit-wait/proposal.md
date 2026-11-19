# 提案

## Why

Helper 当前会把部分暂时无法完成的 Codex 只读预提交校验记为 `session_unavailable` 并终结投递。若 CLI 进程仍在、但 thread/read、thread/resume 或受支持的校验 RPC 暂时失败，原信不会在当前回合结束后再尝试。

## Scope

只覆盖 turn/start 之前、可由固定分类识别的暂时传输／服务端错误。把投递保留为持久 FIFO 头，有限退避后重试只读校验；只有验证通过并持久化提交边界后，才允许一次 turn/start。Hub 仅接收现有 ACK v2 busy 信号：若 `DeliveredAt` 尚未写入，走 busy→queued 重排；若已写入，则保持 `delivered`，由 helper 持久 FIFO 继续负责该队首，不重复投递。

身份、thread ID、cwd、direct-input、daemon 所有权／身份不匹配继续 fail-closed，不换线程；持久 busy marker 保留原信为 FIFO 头并显式告警，等待人工处理及安全重验，不发送 terminal failed ACK。可能已经提交的投递继续 unknown/manual，不自动重投。本 change 不修改 Hub、ACK v2 failure 约束或迁移。
