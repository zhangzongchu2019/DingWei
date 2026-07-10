# DingWei 总控 P3 测试清单（预备）

**测试方**: zzc-tester  
**预备日期**: 2026-07-08  
**被测范围**: feat/control-plane-p3（P3 编排增强增量）  
**规格依据**: docs/DingWei总控路由架构规格-v0.1.md §四(L2分诊接口: decompose/aggregate) + §七(P3分期)  
**测试方法**: LLM mock + 真机 `go test ./...`  
**前置条件**: developer 回执 P3 完成 + manager 通知开跑

---

## 一、decompose → 子任务生成

### 用例 1.1：decompose 生成子任务 + 父转 awaiting_children
- 输入: "让团队复测登录全流程并汇总" → 匹配 Rule 9 keyword "让团队"
- LLM mock 返回 `intent=decompose, subtasks=[{session:zzc-developer,instruction:"自查登录接口"}, {session:zzc-tester,instruction:"端到端复测登录"}]`
- **验收**: 
  - 父任务 status → `awaiting_children`，parent_id=NULL
  - 每个 subtask 生成一条 control_task，parent_id=父任务ID，status=`queued` 或 `llm_pending`
  - subtask 数量 = LLM 返回的 subtasks 数组长度

### 用例 1.2：subtasks 各自投递到目标会话
- 父任务分解后，每个子任务独立走投递链
- **验收**: 每个子任务的目标会话收到对应 instruction 消息

### 用例 1.3：decompose 子任务数为 0
- LLM mock 返回 `intent=decompose, subtasks=[]`
- **验收**: 不应生成子任务；父任务应有明确处理（降级 clarify 或 reject）

### 用例 1.4：decompose 子任务数上限
- 构造大量 subtasks（如 20 个）
- **验收**: 有上限保护（合理上限如 10），超出部分截断或拒绝；不炸队列

---

## 二、aggregate 聚合

### 用例 2.1：子全 done → 触发 aggregate → 父 done
- 构造 2 个子任务，模拟全部完成（status=done，有 result）
- aggregate 触发条件满足
- **验收**: 
  - 父任务从 `awaiting_children` → aggregate(L2) → `done`
  - 父任务 result 包含聚合后的综合回复
  - 父任务 source_addr 收到聚合结果

### 用例 2.2：aggregate 回复发给父 source
- 父任务 source 收到聚合回复，而非每个子任务各自回复
- **验收**: 父 source 收到 1 条聚合回复，包含所有子任务结果摘要

### 用例 2.3：aggregate 内容包含子任务结果
- LLM mock 返回聚合结果引用各子任务的输出
- **验收**: 父 result 中可识别各子任务的结论

---

## 三、并发竞态保护

### 用例 3.1：多个子任务同时完成 → aggregate 只触发一次（不重复聚合）
- 构造 2 个子任务几乎同时完成
- **验收**: aggregate 只执行 1 次；父任务只收到 1 条聚合结果；control_task 只更新 1 次 done

### 用例 3.2：aggregate 进行中，新的子任务完成 → 不重复触发
- **验收**: 已触发的 aggregate 不会被后续子任务重复激活

### 用例 3.3：不丢子任务
- 构造 N 个子任务全部独立完成
- **验收**: 所有子任务有终态（done/failed）；父任务 aggregate 时能看到全部子任务结果

---

## 四、子任务失败/超时处理

### 用例 4.1：部分子任务 failed
- 构造 3 个子任务，1 个 failed，2 个 done
- **验收**: aggregate 仍触发；父任务 result 包含失败子任务的状态说明；父最终 done（非 failed）

### 用例 4.2：全部子任务 failed
- 构造 2 个子任务，全部 failed
- **验收**: aggregate 触发；父 result 说明全部失败；父最终 done 或 failed（需确认设计意图——规格未明确，需与 developer 对齐）

### 用例 4.3：子任务超时 expired
- 构造 1 个子任务 expire_at 到期被 reaper 标记 expired
- **验收**: aggregate 仍触发（或父也 expired，需对齐）；行为明确、有兜底

### 用例 4.4：部分子任务悬挂（永不完成）
- 父任务自身有 expire_at（5min），超时后 reaper 兜底
- **验收**: 父任务到期后 expired → 通知发起方；不永久悬挂

---

## 五、优先级调度

### 用例 5.1：高 priority 任务先被认领
- 入队 3 个 llm_pending 任务，priority 分别为 0、5、10
- **验收**: ClaimNextL2ControlTask 按 priority DESC, created_at ASC 顺序认领

### 用例 5.2：同 priority 按 FIFO
- 同 priority 的两个任务，先入队的先被认领
- **验收**: 按 created_at 升序

### 用例 5.3：decompose 子任务继承父 priority
- 父任务 priority=5，子任务也应为 5（或按规则设定）
- **验收**: priority 传递策略明确且一致

---

## 六、无回归

### 用例 6.1：P2 nl_dispatch + clarify 不变
- `TestControlPlaneP2DispatchAndClarifyWithMockLLM` 仍绿
- **验收**: dispatch/clarify 路径不受 decompose/aggregate 影响

### 用例 6.2：P2 L2 重试链不变
- `TestControlPlaneP2ProviderFailureRetriesAndFails` 仍绿
- `TestControlPlaneP2ProviderFailureRequeuesUntilMaxAttempts` 仍绿
- **验收**: L2 失败→llm_pending→重认领→跑满3次→failed+通知 全部正常

### 用例 6.3：P1 L1 全部 10 条规则不变
- Rule 1-10 行为完全不变
- **验收**: L1 命中路径零延迟、secOps 鉴权（非管理员阻断/管理员通过）不受影响

### 用例 6.4：P2 租约排他/过期重抢不变
- `TestControlPlaneP2LeaseClaimIsExclusiveAndReclaimsExpired` 仍绿
- **验收**: 租约机制隔离，P3 不引入租约泄漏

---

## 七、全链保交付

### 用例 7.1：父任务最终必有回复
- 覆盖 decompose→子任务→aggregate→done 完整链
- 覆盖子任务失败/超时/部分悬挂的兜底路径
- **验收**: 所有父任务有终态（done/failed/expired）；source_addr 收到明确回复

### 用例 7.2：ack 不丢
- P1/P2 已修复的 ack 丢弃问题不复现
- **验收**: 入队即 ack `已受理 #<id>`

### 用例 7.3：完整链路可追踪
- control_task 父子关系可查（parent_id 索引）
- **验收**: 查询父任务 → 可找到所有子任务及其状态；查询子任务 → 可找到父任务

---

## 执行计划

| 步骤 | 内容 |
|------|------|
| 1 | developer 回执 P3 完成 |
| 2 | manager 通知开跑 |
| 3 | `git fetch && checkout origin/feat/control-plane-p3` |
| 4 | `go test ./internal/m8/ -v -run "P3|Decompose|Aggregate|Priority|Subtask|Parent|Child"` |
| 5 | `go test ./internal/store/ -v -run "P3|Parent|Subtask"` |
| 6 | P2 回归: `go test ./internal/m8/ -v -run "P2|Lease|LLM|Clarify"` |
| 7 | P1 回归: `go test ./internal/m8/ -v -run "Security|SecOps|CrossMember"` |
| 8 | `go test ./...` 全量 |
| 9 | 逐项填写 通过/失败/复现步骤 |
| 10 | 结果报 zzc-manager；失败项同步 zzc-developer |
