# M5 ↔ board vNext conformance 对拍 draft v0.1

对拍锚：

- 契约：`interface-contracts-v1^{commit}=107015744b0e56c9645c84eb6ed97bf479c7d918`
- M5：`c96d08950ccfad101731821d6e43e275f5561469`
- board vNext：`5654ecc5`

结论：**FAIL / CHANGES REQUIRED**。M5 本地状态与 replay store 可承接冻结接口，但 board vNext 当前实现存在下列契约级不一致；修正并重跑冻结 cases 前不能宣称 conformance 闭合。

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
- M5 只接受 `sealed_fence_token`：PASS；但 board 当前未提供可安全持久化并重放的 sealed outcome，集成 seam 因 C2/C5 未闭。

## 重跑门槛

1. board 修正 C1-C5，C6 给出 guarded 证据；
2. 将 CT-F2-02/04、CT-F3-01/02 原样落成可执行测试，不以弱化断言替代；
3. M5 侧增加一个 adapter conformance test：board 首 Takeover outcome→M5 durable record→进程重启→同 request 重放，四元身份与 token 可继续 ActivateWriter；
4. 两锚合并测试运行 `go test -race ./internal/mcs/board ./internal/mcs/rotation` 全绿后再复核。
