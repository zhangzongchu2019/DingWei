# M5 ↔ board vNext conformance 对拍 draft v0.3

对拍锚：

- 契约：`interface-contracts-v1^{commit}=107015744b0e56c9645c84eb6ed97bf479c7d918`
- M5：`c96d08950ccfad101731821d6e43e275f5561469`
- board vNext 初锚：`5654ecc5`
- board vNext 修正锚：`14df8d0c`
- board vNext 三轮锚：`5f0c559a8e2d24d20e999b31318d26c930bbff77`

三轮复核结论：**HELD（仅 1 个 R1 残口）**。`5f0c559a` 已令首次密封发生在
CAS 前，且将 `binding_mismatch` 纳入首 outcome；原两个定向负例、M5 adapter
全链和合并树 race 均已通过。但 CAS 后的 `vnextRecord` 仍再次调用吞错
`sealToken`，因此尚不能宣称“所有密封成功先于状态写”。

### 第三轮唯一残口：CAS 后二次密封仍吞错

当前顺序是：

1. `TakeoverV2` 调 `sealTokenE(token)` 成功；
2. 执行 CAS、写事件；
3. `vnextRecord` 再调 `sealToken(out.FenceToken)`；
4. 第二次密封失败时 wrapper 返回 `"seal-unavailable"`，随后照常写幂等账并 commit。

因此文件密钥在两次读取之间失效、第二次随机源/AEAD 失败等情况下，仍会提交
新 epoch 和不可解封的首果。这正是 R1 所要求消除的写后密封窗口。

最小修正：将步骤 1 生成的 sealed envelope 传入 `vnextRecord`，只密封一次；
或令 `vnextRecord` 使用可返回 error 的密封函数并在失败时回滚整个事务。
删除/禁用吞错 wrapper；补可注入的“第二次密封失败→epoch/state/event/ledger
均零变化”负例。

## R1 密封密钥不可用时仍提交 Takeover（阻断）

`sealTokenE` 已改为 AES-GCM，算法方向正确；但调用方 `sealToken` 吞掉错误并返回
字面量 `"seal-unavailable"`。`TakeoverV2` 随后仍执行 CAS、写事件和幂等账，
向调用方返回 `applied` 与明文 fence token。结果是 DB 中没有可恢复的密封 token，
重启后无法重放/激活，同时状态已不可逆地进入新 epoch。

定向负例（清空 `DINGWEI_VNEXT_SEAL_SECRET` 与
`DINGWEI_VNEXT_SEAL_SECRET_FILE`）实测：

```text
TestDev56TakeoverMissingSealSecretFailsClosed
got Outcome=applied State=ACTIVATING Epoch=6 err=nil
want non-applied error before CAS
```

要求：Takeover 在任何 CAS/事件/幂等写入前调用可返回 error 的密封函数；
密钥缺失、过短、文件不可读、随机源/AEAD 初始化失败均 fail closed，数据库零变更。
`vnextRecord` 不得再次用吞错 wrapper 密封。补“缺密钥零状态变化/零事件/零幂等账”
负例。

## R2 ActivateWriter 的 binding_mismatch 未记录首 outcome（阻断）

`ActivateWriter` 的正常 applied、illegal transition 与 UPDATE loser 已写入幂等账；
但四元绑定不匹配分支直接返回：

```go
return TakeoverOutcome{Outcome: OutcomeCASMiss, ReasonCode: "binding_mismatch"}, nil
```

因此同一 request 第一次用错 token 得 `cas_miss`，第二次改成正确 token 会成功
`ACTIVATING→ACTIVE`，违反“同 request 原样/变参重放只返首 outcome、不得二次执行”。

定向负例实测：

```text
first  = cas_miss(binding_mismatch)
replay = applied(ACTIVE)
```

要求：binding_mismatch 也通过 `vnextRecord` 在同事务记录首 outcome；同 request
同 digest 返回首果，变更 WriterContext 后 digest 不同则返回
`idempotency_conflict`，不得执行 activate callback、UPDATE 或重复事件。

## C1 owner 与 writer_instance_id 被混用（阻断）

冻结 §1.1 明确两者不同：owner 是授权主体，writer_instance_id 是不可复用的非秘密实例 ID。`m0-final` C-6 的成功更新集合也是 `{epoch+1, writer_instance_id, state}`，没有把 owner 改成 instance。

`internal/mcs/board/vnext.go@5654ecc5` 的 Takeover SQL 却执行：

```sql
UPDATE orchestrator SET epoch=epoch+1, owner=?, state=?, writer_instance_id=? ...
```

并把两个 `?` 都填成 `req.NewInstanceID`；返回 outcome 也令 `Owner=req.NewInstanceID`。这使 owner/instance 两字段重新合流，四元绑定退化。

要求：Takeover 后 owner 保持契约 owner（当前请求没有 `new_owner` 字段，故应保持 expected/current owner）；仅 writer_instance_id 更新为 NewInstanceID。补断言 owner=O、instance=W6 且二者可不同。

## C2 Takeover 原样重放丢 fence token（阻断）

冻结 CT-F2-04：同 request 原样重放须返逐字段相同首 outcome；持久化 outcome 中 token 应密封存储。

当前 `vnextRecord` 在持久化前执行 `safe.FenceToken=""`。因此首次调用返回 token，重启/重放从 `vnext_request.payload` 读回时 token 为空。现有测试刻意没有比较 `FenceToken`，未覆盖冻结判据。

要求：持久化 sealed token；重放时在合法 coordinator 边界解封并返回与首果相同的 token。测试必须比较完整 outcome（含 token），并覆盖重启后重放。

## C3 ActivateWriter request_id 未参与幂等（阻断）

冻结 §1.2/CT-F2-02 要求 ActivateWriter 的状态、事件、首 outcome 同事务，原 request 重放返回首果。

当前 `ActivateWriter(... requestID string ...)` 完全未使用 requestID，也未写幂等账。第一次 ACTIVATING→ACTIVE 后，同 request 重放因当前状态 ACTIVE 返回 `illegal_transition`，不是首个 applied outcome。

要求：ActivateWriter 建独立或统一幂等记录，digest 覆盖 board/rotation/request/expected state/WriterContext 非秘密身份字段；同 request 同 payload 返回首果，异 payload 返回 idempotency_conflict，且不重复事件。

## C4 C2.6 的 1→2 正例会被错误拒绝（阻断）

冻结 CT-F3-01：当前占席 1，提升第二 principal 应 applied。

`PromoteExecSeatV2` 调 `execSeatCount(..., role)`；该查询用 `extraRole` 把待提升角色预先计入。若已有 1 个执行席，返回 count=2；随后代码以 `if count >= 2` 返回 limit_exceeded，导致合法第二席被拒。现有测试只覆盖“已有 2 席→第三席拒”和豁免后路径，没有执行 CT-F3-01。

要求：按“提升后总数 > 2 才拒”实现，或先数当前席并明确 candidate 是否已占席；补原样 CT-F3-01。并核 CT-F3-02 的拒绝事件：当前 limit_exceeded 只写 `vnext_seat_request`，未见冻结用例要求的拒绝事件账。

## C5 token 的固定 XOR 不是安全密封（阻断）

`sealKey()` 返回源码硬编码常量，`sealToken` 是固定 XOR。任何能读取二进制/源码/DB 的主体都能恢复 token，不满足“加密/密封持久化”的安全边界。

要求：使用部署 secret 派生的 AEAD（随机 nonce、版本化 envelope、鉴别失败 fail closed），密钥不入源码/DB/日志；提供篡改、错 key、重启解封和明文扫描负例。

## C6 RevokeStaging 的 WriterContext 未校验（中）

函数签名接收 `wc WriterContext`，实现完全未使用。任意能调用该方法的路径都可在 STAGING 修改状态，未兑现四元绑定/唯一 coordinator 的 guarded 语义。

要求：要么按四元绑定校验 wc，要么从公开签名移除虚假参数并在唯一受保护上层证明授权；冻结接口消费侧建议前者。

## M5 侧对拍结果

- UsageState 可序列化恢复：PASS。
- 同 request/rotation 重放首 outcome：PASS（M5 store）。
- 同 request 异 rotation、同 rotation 异 request：PASS，均 fail closed。
- 写失败不假成功与 fsync 顺序：PASS。
- M5 只接受 `sealed_fence_token`：PASS。
- `5f0c559a` 与 `wip/m5-claude@c96d089` 临时合并无冲突；
  `go test -race ./internal/mcs/board ./internal/mcs/rotation`：PASS。
- R1 缺密钥首密封、R2 binding_mismatch 首果定向负例：PASS。
- adapter 全链：board Takeover→取 sealed outcome→M5 durable record→FileStore
  重启重放→board 同 request 重放→ActivateWriter：PASS。

## 重跑门槛

1. 消除 CAS 后二次密封吞错窗口，并固化故障注入负例；
2. 该负例与现有 C1-C6/R1/R2、adapter、全量 board/rotation race 同时全绿，
   即可宣 PASS；其余点位不重开。
