# 交办 dev55：可靠投递忙时 **残留竞态** 修复（T9 补丁二）

- 派发：manager（zzc-manager-2642），2026-07-12
- 背景：T9 一版（3617bb3，已上线）修了"忙时挂起不重试到死"的主路径，但**真机复测发现残留竞态仍会丢消息**。这是 T9 的后续补丁。
- 负责实现：dev55；评审+部署：manager（禁碰生产）；提交身份 zhangzongchu2019 <tchangshark@outlook.com>，无 claude 署名。

## 真机复现（铁证）

给正忙的 tester 连发多条：多数走 `busy` 挂起→`processed`（一版修复生效）；但个别消息（如 dlv_1eb7f1、dlv_d24347）**attempts 从 1 一路 cli_injection_failed 爬到 12 → dead → 丢失**。

## 根因（已精确定位，勿重复排查）

问题在 **helper 侧一次注入失败就永久钉死**：

1. `delivery_worker_loop`（app.py:427 一带）注入前查 `is_idle`。若 `is_idle=True`（以为空闲）→ 调 `process_inbound_message`→`adapter.handle` 注入。
2. 但 **is_idle 与实际注入之间有 TOCTOU 窗口**：查的时候空闲、真注入时 CLI 又忙了 → `adapter.handle` 抛异常 → worker 的 except 分支 `delivery_inbox.finish(delivery_id, exc)` 把该行标成 **terminal `failed`** + 回 `cli_injection_failed`。
3. 此后 Hub 每次重投这条 → helper `accept_delivery`→`inbox.accept()` 见已存在的 `failed` 行 → 直接回 `cli_injection_failed`（accept 的 failed 分支，app.py:415-418），**永不再试**。
4. Hub 侧每次收 `cli_injection_failed(retryable)` → attempts++ → 到 12 → `ClaimSessionDelivery` 判 `dead`（sqlite.go:1340）。

**一句话**：`is_idle` 误判导致的一次瞬时注入失败，被 helper inbox 永久标 `failed`，再也不会趁 CLI 空闲重试，于是被 Hub 重投拖到 dead。**忙检查只挡了"第一次注入前"，挡不住"注入过程中变忙"。**

## 修复要求（helper 侧为主）

核心原则（张宗楚定，同 T9）：**忙导致的失败绝不能变永久丢失。** 瞬时注入失败要能在 CLI 空闲后自愈重试，不能一次就 terminal。

选一个稳妥方案实现（你评估，倾向 a+c）：

- **a) 注入失败先判是不是"忙"再决定终态**：`process_inbound_message`/`adapter.handle` 注入失败后，**立即复查 `is_idle`**；若此刻 `not idle`（即失败就是因为忙）→ **按 busy 处理**：`inbox.release` 回 `received`（不是 finish failed）+ 回 `busy` ack（不计 dead），下轮空闲再试。只有 `is_idle=True 且注入仍失败`（真故障）才走 failed。
- **b) 注入失败给本地有限重试**：worker 对同一条在本地退避重试 N 次（如 3 次，每次间隔递增）后仍失败才 finish failed；期间不回 cli_injection_failed 给 Hub（或回 busy）。
- **c) `accept()` 对已 `failed` 的行放行自愈**：Hub 重投一条 helper 侧为 `failed` 的投递时，若当前 `is_idle=True`，**允许把该行从 `failed` 重置为 `received` 再试一次**，而不是无脑回 cli_injection_failed。这样即使被钉死过，CLI 空闲后仍能救回来。

Hub 侧一般不用大改（busy 分支 3617bb3 已有）；若采用"注入失败即 busy"策略，确保 Hub 对这类回执按 busy 处理（不累加 attempts）。

## 边界与不破坏

- **真故障仍要能 dead**：CLI 真的崩了/永久无法注入时，仍应在合理次数后判 dead，不能无限重试（保留兜底，但不能被"忙"误伤）。要能区分"忙导致的失败"与"真故障"。
- 一版已过的行为不回归：干净忙态挂起→processed、FIFO 串行、幂等去重、30 分钟总兜底。
- helper `is_idle` 的判定本身若不可靠（TOCTOU 根源），可评估加锁/注入时原子占用，但别过度设计。

## 验收判据（manager 真机复测，这次要严）

- [ ] 给忙 tester **高频连发 ≥8 条**（模拟本次密集场景）：**全部最终 processed，零 dead、零 cli_injection_failed 拖到 12**。这是本次核心，上次就是这里挂的。
- [ ] 被瞬时注入失败标过的投递，能在 CLI 空闲后**自愈处理**，不永久 failed。
- [ ] 真故障（如 kill 掉 CLI）仍能在有限次后 dead，不无限重试。
- [ ] `go build ./... && go test ./...` 全绿；helper 单测新增"注入失败自愈"用例（覆盖 is_idle 误判→失败→空闲后成功）。

## 交付

独立 commit + 说明 → send.py 回报 manager（用全名 **zzc-manager-2642**，别用短名）→ manager 亲跑 go/helper 测试 + 真机高频复测 → 部署（这次连 helper 一起部署,manager 会重启 tester 等非 manager 会话验证）。分支别 push，禁碰生产。
