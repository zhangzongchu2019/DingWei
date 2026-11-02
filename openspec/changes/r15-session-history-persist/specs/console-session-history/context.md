# Context

## 基线

- 仓库基线：`c248506cf120c3af2a06eeaeeae3038f596537b1`。
- 只读事实报告：`review/prodops/history-empty-facts-20261102`，提交 `1c7cc1e29`，正文位于 `docs/reviews/prodops-history-empty-facts-20261102/REPORT.md`。取样范围为 2026-11-02 01:44:06Z–01:49:59Z 的 dw123 只读观察；报告没有输出消息正文、事件正文、凭据或对端地址。
- 该报告观察到 dataconnector 当前 Hub 无 terminal state 可回放；另两席各观察到 1000 帧并带 `overflow`。同一 Hub 启动后的五个新进程 invocation 初始内存 ring 为空。报告明确指出：无法仅凭取样证明某次空历史由 Hub 重启单独造成，也不能排除其他 disconnect。

## 文件与当前行为

| 位置（基线行号） | 当前职责／事实 |
| --- | --- |
| `internal/m8/terminal.go:38-41` | 每 terminal 的内存事件 ring 为 1000 帧；终端 buffer 上限 1 MiB；另有 8 小时 terminal token TTL。 |
| `internal/m8/terminal.go:470-493,512-525` | Hub 收到 helper `session_event` 后剥除客户端 provenance 声明、发布结构化事件、递增进程内序号、写 ring 并扇出订阅者。 |
| `internal/m8/event_ring.go:4-8,45-57,79-92` | ring 有每会话 16 MiB、Hub 128 MiB／128000 条上限；超过预算从最旧端淘汰。 |
| `internal/m8/terminal.go:855-894,912-943,945-1015` | View SSE 复制内存 ring 并注册实时 reader；`epoch-seq` cursor 支持同 epoch 接续，不同 epoch 返回 `epoch_changed`，缺少旧帧时返回 `overflow`。 |
| `internal/m8/console_mux_frames.go:150-180` | Console mux 的 source snapshot 来自相同内存 ring，并同时注册实时 reader。 |
| `internal/m8/console_mux_transport.go:698-717` | mux transport 从 snapshot 中按 requested cursor 选择 replay；无 source 时不能启动该会话流。 |
| `internal/m8/hub.go:1241-1255` 与 `internal/m8/terminal.go:177-202` | helper 当前连接结束时调用 `closeTerminalLocked`，清 ring、关闭 mux reader、删除 terminal state。 |
| `internal/m8/console_mux.go:345-388` 与 `internal/m8/console_mux_broker.js:24` | 当前 cursor 不可继续会产生 `resync_required` 等控制结果，前端明确提示需要重新同步。 |
| `internal/store/migrations/0027_session_delivery.sql`（如经基线核实） | session_delivery 表是投递记录，不是当前事件回放源；报告证实 Hub 回放代码没有读取该表。 |

## 已知差异和约束

- eventRing 的 1000 单位是帧／结构化事件条目上限，不等同用户所说的消息或完整回合；本变更需要通过 PM 决策固定保留单位。
- session_delivery 与 message 目前持久存在，但不能由其行数推导出已回放的完整 Agent 会话历史，也不能假定它们具有同一授权和保留契约。
- 当前 mux 的 process epoch 和客户端显式 resync 逻辑已经被真实调用；持久化历史需定义跨进程稳定 cursor，并保留真实淘汰／删除造成的 gap 告警。
- 未从生产报告推导 TTL、允许查看者、数据库加密状态或撤权后的现行清理行为；这些是本提案的 PM 点／实现前核验项。
