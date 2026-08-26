# M5 durable state 与首 outcome 用例 v1

执行：`go test -race ./internal/mcs/rotation`。

| ID | 前置/输入 | 操作 | 期望/PASS 判据 |
|---|---|---|---|
| D1 | UsageState 含连续计数与 pending rotation | Save 后新建 store Load | 字段逐一相同，重启不丢状态 |
| D2 | 首次 request/rotation outcome | Record | fsync+rename+目录 fsync 后才返回成功 |
| D3 | 同 request/rotation 但传入不同 outcome | Record | 返回持久化首 outcome，replay=true，不覆盖 |
| D4 | 同 request 异 rotation | Record | ErrRequestConflict，零写入 |
| D5 | 同 rotation 异 request | Record | ErrRotationConflict，禁止二次 CAS/spawn |
| D6 | 注入写/fsync失败 | Save/Record | 返回错误，不返回 applied/replay |
| D7 | 两 goroutine 同 rotation 异 request | 并发 Record | 恰一成功、一 rotation conflict，race 全绿 |
| D8 | 文件损坏/schema不支持 | Load | fail closed，不以空状态覆盖 |
| D9 | outcome 含密封 token | 检查落盘 JSON | 仅 sealed_fence_token；无 fence_token 明文字段 |
