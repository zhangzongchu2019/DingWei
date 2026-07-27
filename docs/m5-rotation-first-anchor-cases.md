# M5 轮换首锚可执行用例 v1

冻结依据：`m0-final^{commit}` 与 `interface-contracts-v1^{commit}`。本锚只覆盖本地状态迁移及 M4 challenge 适配，不声称已实现 board takeover、usage 去抖或持久化恢复。

执行命令：`go test ./internal/mcs/rotation`。

| 用例 | 前置/输入 | 操作 | 期望与 PASS/FAIL 判据 |
|---|---|---|---|
| M5-S1 | `IDLE + trigger_usage` | `Next` | `ROTATION_PENDING` |
| M5-S2 | `PENDING + spawn_staging` | `Next` | `STAGING` |
| M5-S3 | `STAGING + revoke_staging` | `Next` | 回 `PENDING`，表示 CAS 前撤销 |
| M5-S4 | `STAGING + takeover_applied` | `Next` | 仅到 `ACTIVATING`，不得直达 ACTIVE |
| M5-S5 | `ACTIVATING + activate_applied` | `Next` | `ACTIVE` |
| M5-S6 | `ACTIVATING + activate_failed` | `Next` | `RECOVERY`，不得重开旧 writer |
| M5-S7 | `STAGING + staging_timeout` | `Next` | `REAPED` |
| M5-N1 | PENDING 直接 takeover、STAGING 直接 activate、RECOVERY 直接 activate | `Next` | 状态不变且 `ErrIllegalTransition` |
| M5-H1 | M4 challenge 返回 fresh `ok` 且 worker 匹配 | `ChallengeGate.Check` | PASS |
| M5-H2 | degraded/unknown/expired/worker mismatch | `ChallengeGate.Check` | 全部 fail closed |

清理：纯内存测试，无子进程、端口或持久文件。
