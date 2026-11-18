# S-R61-N1 设计与 N-1a 实现交件

基点：R60 集成锚 `9818b979e9e20f0bcfeffa6b78045dda71b1bd79`。

组长于 2026-11-17T18:26:32Z 批准设计并裁定拆分：本分支仅实现 N-1a helper 侧持久 busy wait、FIFO/重启恢复及超时语义；容量满行为保持现状。N-1b 的 `queue_full` ACK v2、Hub 通知与兼容协商留待 R60 最终锚定后另派。未改 `internal/m8`。

## N-1a 实现交件

实现仅限 helper、helper 测试、配置说明和本 OpenSpec change；未改 `internal/m8`、VERSION、main，也未部署或生成发布件。持久 inbox 新增 `busy_wait_started_at` 与 `submission_may_have_started_at`。驱动 gate 在等待前先写 busy marker，在 writer 调用前以同一生命周期账本执行提交边界 CAS；落盘失败时拒绝跨越边界。`dispatch_intent_at` 仍只表示 helper 准备派发，不作为已提交证据。

重启时，只有存在 busy marker、且无提交边界与 admitted 证据的行，才从 processing 恢复为 received 并按持久 received 顺序重新进入 FIFO。提交边界后的 processing 行沿既有 unknown/manual 路径恢复，不自动调用 writer。适配器恢复队列时原子 claim 持久行，避免 websocket claimant 与恢复线程重复入队。无持久 inbox 行的直接／本地 driver 调用保留原有内存 gate 行为。

`SH_BUSY_ADMISSION_MAX_SECONDS` 继续解析并用于跨越已证明忙等之后的 admission 窗口；它不再取消仍在等待的 pre-submit delivery。达到阈值后按间隔发操作告警，队列事件用 `alert_after_seconds` 表示告警阈值。容量超界和发送方可见拒收明确留给 N-1b，本段没有改变其现状。

### 验证

- OpenSpec 1.16.0 strict：`Change 'r61-n1-busy-wait' is valid`，rc=0。
- `scripts/run-build-tests.sh`：`BUILD_TESTS_OK helper_profiles=2 python=27 shell=2`，rc=0。env 为 `env -i`，PATH 前置 `/tmp/lockvenv/bin` 与 `/usr/local/go/bin`；两个 driver profile 各 1275 项，分组 `[1266, 9]`，97 个 helper 文件。此前一次因人为设置 `GOPROXY=off` 且 Go module cache 不完整而失败；去掉该限制后完整门通过，`scripts/test-desktop-hub-roundtrip.sh` 与门内 Go roundtrip 均通过。
- 持久 inbox＋假 writer 的重启/FIFO用例断言：忙等恢复为同一头项，后项不越过；忙等超过较短 admission 与旧忙等阈值仍不调用 writer；释放前提交边界仍为空，释放后原 delivery 恰好写一次并持久化边界；已跨边界的重启保持 admission_unknown 且 writer 调用数为零；边界 marker 持久化失败时不允许 writer。

### 独立删保护变异

五刀均单独应用、运行对应定向 unittest、再还原源文件；五刀均 rc=1，且红在业务断言：

| 变异 | 业务断言 |
| --- | --- |
| 恢复按旧忙等上限终止 | `BA_LONG_WAIT_WARNING_MISSING`（Claude cap 用例） |
| 忙等 hook 不写 durable marker | `BA_WAIT_NOT_DURABLE` |
| 重启不再把有 busy marker、无提交边界的行恢复为 received | 期望 received、实际 admission_unknown |
| writer 前不持久化 submission boundary | 期望 boundary timestamp、实际为空 |
| FIFO 恢复改为 received_at 倒序 | 期望 first delivery 为队头、实际 second delivery 为队头 |

### 范围状态

N-1a 的 helper 实现与门已交件。N-1b 容量拒收、Hub ACK v2 `queue_full`、发信方通知及旧 Hub 能力协商仍 blocked，按组长裁定等待独立派单；本交件没有改变容量满时的现有行为。
