# 交办 dev55：可靠投递「忙时」改异步挂起，不再重试到死

- 派发：manager（zzc-manager-2642），2026-07-12
- 负责实现：dev55（codex，代码 owner）
- 评审：manager（会亲跑 go test + 写探针 + 真机复测）
- 部署：manager（生产 hub 106.52，本地交叉编译推二进制，dev55 只本地改码 + go build/test，**禁碰生产**）
- 提交身份：public GitHub `zhangzongchu2019 <tchangshark@outlook.com>`，无 claude 署名，推前过 `scripts/check-sensitive.sh`

## 背景：T2 真机验收抓到的 bug

可靠投递（`session_delivery` + ACK 三态）真机一测就露馅。实测证据：
- 给 tester 连发两条 `b0a7`、`52df`。`b0a7` 注入成功、tester CLI 开始处理（要几分钟）。
- 这期间 `52df` 被 claim，但 tester CLI **还在忙**，注入失败 `cli_injection_failed`，每 ~2s 重试。
- CLI 还没忙完，`52df` 的 **12 次重试上限就烧光 → 判 dead → 消息丢失**。
- （`b0a7` 最终 processed，但靠租约过期后重新驱动才收敛，滞后约 4 分钟。）

## 根因（已定位，勿再重复排查）

helper 有两条并行入站路径，忙时缓冲只接在其中一条：

- **普通消息路径**（`app.py: recv_loop → buffer_if_busy`，约 340 行）：忙时机制完整——查 `adapter.is_idle`，忙则缓冲到 `pending_inbound`、回 `send_busy_ack`、空闲后 `pending_drain_loop` 再处理。**这是正确的异步语义。**
- **可靠投递路径**（`app.py: delivery_worker_loop → process_inbound_message`，约 427 行）：**绕过 `buffer_if_busy`**，直接 `adapter.handle` 硬注入 PTY，**不查 `is_idle`**。CLI 忙 → 抛异常 → `ws.send(delivery_ack failed, reason=cli_injection_failed, retryable=True)` → Hub 重试。

Hub 侧凶手规则：`internal/store/sqlite.go:1340` 的 `ClaimSessionDelivery` 里
`UPDATE ... state='dead' ... WHERE ... (attempts>=12 OR created_at<=now-30min)`——忙导致的失败照样累加 attempts，到 12 判死。

## 设计目标（张宗楚 2026-07-12 定，务必遵守语义）

**「对方忙」是异步状态，不是失败。** 收信方 CLI 忙时：消息**不丢、挂起等待**，空闲即处理；发信方收到「对方忙/稍后处理」回应后**可等可走**。**服务端（Hub）把忙当异步——不同步等、不重试到死。**

## 实现要求

### helper 侧（`tools/sessionhelper/`）

1. `delivery_worker_loop` 注入前先查 `is_idle`（复用 `adapter.is_idle`，注意 `hasattr` 兜底，非 cli 模式跳过）。
2. **CLI 忙**（`not idle`）时：
   - **不调 `process_inbound_message`/`adapter.handle`**（不硬注入）。
   - **不 `finish` 该 delivery**（保留在 `delivery_inbox`，状态别落 failed/processed），下一轮循环再试；建议对忙态加小退避（如 0.5~1s）避免空转。
   - 回一个**独立的 `busy` 状态 delivery_ack**（新增 wire 状态 `busy`，`retryable=True`，带个标志让 Hub 知道「这是忙、别计入 dead」，如 `reason="cli_busy"` + 不计数语义）。
   - busy_ack 只回一次即可（参考 `busy_acked_from` 去重思路），避免刷屏。
3. CLI 空闲后正常注入 → `processed` ACK（现有逻辑）。
4. **不要破坏**：`accept_delivery` 首次入库回 `delivered`、幂等去重（`delivery_id` 主键）、`pending_drain_loop` 的普通路径、recv_loop 不阻塞（#4 修复）。

### Hub 侧（`internal/m8/hub.go` + `internal/store/sqlite.go`）

1. `handleSessionDeliveryAck`（hub.go 约 2831）新增处理 `state=="busy"`：
   - **不增 attempts**、**不计入 dead**；把 delivery 置回可再投状态（如 `queued` 或保持，配一个明显更长的 `next_attempt_at` 退避，如 10~15s），并安排稍后再 drain。
   - 不要走 `failed` 的 `attempts++`/dead 分支。
2. `ClaimSessionDelivery`（sqlite.go:1340）的 dead 规则：**忙挂起的 delivery 必须豁免 `attempts>=12`**。两种实现二选一（你评估）：
   - a) 忙态不累加 attempts（busy 分支只更新 next_attempt，不动 attempts）——最简，dead 规则不用改；
   - b) 或给忙态单独状态/标志，dead 扫描排除之。
   - **保留** `created_at<=now-30min` 这个总兜底（防永久挂死），但忙的正常场景不该触发。
3. 语义：Hub 收 busy 后**异步挂起、退避重投**，绝不同步阻塞等待。

### 边界与测试

- 目标 CLI 忙 → 连发 N 条：**全部最终 processed，零 dead、零丢失**（除非真超 30min 兜底）。这是本次核心验收。
- CLI 空闲：happy path 不受影响，仍 `queued→sent→delivered→processed`。
- 真失败（非忙，如注入进程崩）：仍走 `failed→retry→dead`，**不能被 busy 豁免掩盖**（要能区分「忙」与「真失败」）。
- go test 全绿；helper 单测覆盖忙态挂起不丢；建议加探针/表驱动用例。

## 验收判据（manager 复测）

- [ ] 给忙会话连发 3 条：3 条最终都 `processed`，`session_delivery` 无 `dead`。
- [ ] 忙态 delivery 的 `attempts` 不会涨到 12（不被忙烧光）。
- [ ] 发信方收到一次「对方忙」回应，不重复刷屏。
- [ ] 空闲 happy path 与真失败 dead 两条老路径无回归。
- [ ] `go build ./... && go test ./...` 全绿；helper 单测通过。

## 交付

dev55 每模块独立 commit + 交付说明 → 通过 send.py 回报 manager → manager 亲跑 go test + 写探针 + 真机复测 → 编译部署生产 → 真机验收。**dev55 本地分支别 push，别碰生产。**
