# DingWei 总控 P2 测试清单（预备）

**测试方**: zzc-tester  
**预备日期**: 2026-07-08  
**被测范围**: feat/control-plane-p1（P2 L2 最小分诊增量）  
**规格依据**: docs/DingWei总控路由架构规格-v0.1.md §四(L2分诊接口) + §五(保交付) + §八(关键决策)  
**测试方法**: LLM mock + 真机 `go test ./...`  
**前置条件**: developer 回执 P2 完成 + manager 通知开跑

---

## 一、L2 Worker 池并发（N=4 + 租约）

### 用例 1.1：Worker 池大小 N=4
- 同时提交 8 个 llm_pending 任务，验证最多 4 个并发处理
- **验收**: 同一时刻 lease_owner 不为空的记录 ≤ 4；其余排队

### 用例 1.2：租约抢任务不重复处理
- 同一 llm_pending 任务同时被多个 worker 尝试抢
- **验收**: 只有 1 个 worker 获得租约（lease_owner 唯一），其他 worker 跳过

### 用例 1.3：租约排他性
- Worker A 持有租约处理中，Worker B 尝试抢同一任务
- **验收**: B 抢不到（WHERE lease_owner IS NULL 条件），不会重复处理

---

## 二、租约到期 / 崩溃恢复

### 用例 2.1：租约到期可被重抢
- 构造 Worker A 持有任务租约但 lease_until 已过期
- Worker B 扫描时抢到该任务
- **验收**: B 成功获得租约并处理；任务不丢

### 用例 2.2：崩溃恢复完整链
- 模拟 Worker A crash（租约未释放）
- Reaper/Scanner 检测到过期租约 → 重置 lease_owner=NULL → 任务重回 queued
- 新 Worker 抢到并完成
- **验收**: 任务最终 done；无永久悬挂

---

## 三、nl_dispatch：无目标 → 选 1 agent 投递

### 用例 3.1：nl_dispatch 正常分诊
- 私聊无前缀，多会话在线 → 进入 L2 nl_dispatch
- LLM mock 返回 `intent=dispatch, targets=[{session: zzc-developer, instruction: "..."}]`
- **验收**: 投递到指定会话；任务 done；发起方收到回复

### 用例 3.2：nl_dispatch 选单 agent 逻辑
- 多会话在线，LLM 根据角色/tool/model 选择最合适的一个
- **验收**: targets 仅 1 个 session（非多投）；选中的会话收到 instruction

### 用例 3.3：nl_dispatch 无可用会话
- 所有候选会话 offline/busy，或无匹配角色
- LLM mock 返回 `intent=reject` 或 `clarify`
- **验收**: 发起方收到"无可用会话"等明确回复

---

## 四、clarify：信息不足 → 回问发起人

### 用例 4.1：clarify 回问
- 输入模糊，LLM 无法判断意图
- LLM mock 返回 `intent=clarify, reply="请问你想让谁处理这个任务？"`
- **验收**: reply 文本发送给发起方；任务状态 done（P2 clarify 即终态）

### 用例 4.2：clarify 不回投到会话
- **验收**: clarify 时 targets 为空或不处理；仅 reply 给 source_addr

---

## 五、confidence 低阈值 → 降级 clarify

### 用例 5.1：confidence 低于阈值降级
- LLM mock 返回 `intent=dispatch, confidence=0.3`（阈值假定 0.6）
- **验收**: 实际行为降级为 clarify；发起方收到反问而非盲投

### 用例 5.2：confidence 高于阈值正常
- LLM mock 返回 `intent=dispatch, confidence=0.8, targets=[...]`
- **验收**: 正常投递

### 用例 5.3：阈值边界值
- confidence 恰好等于阈值（如 0.6）
- **验收**: 行为明确（通过或降级，不模糊）

---

## 六、DeepSeek 调用失败/超时 → 不吞任务

### 用例 6.1：DeepSeek 超时
- LLM mock 模拟超时（> 超时阈值）
- **验收**: attempts+1；<3 重排队列；≥3 → failed + 明确失败回复给发起方

### 用例 6.2：DeepSeek 返回非法 JSON
- LLM mock 返回非结构化文本（解析失败）
- **验收**: attempts+1 重试或 failed；不吞任务、不静默

### 用例 6.3：DeepSeek 网络错误
- LLM mock 模拟连接失败
- **验收**: attempts+1；重试逻辑正确；终态有通知

### 用例 6.4：重试成功
- 第 1、2 次失败，第 3 次成功
- **验收**: attempts 递增到 2 但不 failed；最终状态 done；结果正确

---

## 七、无回归

### 用例 7.1：P1 L1 命中路径不变
- 同 P1 测试 §二 全部 10 条 L1 规则
- **验收**: L1 规则行为不变；ExitQueue=true 的即时出队路径零延迟

### 用例 7.2：secOps 鉴权不变
- `@SYSTEM-V-TASK-INTERNAL#session #系统安全 cmd` 非管理员 → "无权限"
- 管理员 → 正常鉴权通过并投递
- `TestSecurityOpsRejectsUnauthorizedSender` / `TestSecurityOpsAllowsBoundAdminOwner` / `TestSecurityOpsRoutesToAllSystemSessions` 仍绿
- **验收**: P1 安全修复不受 L2 引入影响

### 用例 7.3：普通 L1 路由不受 L2 代码变更影响
- Rule 5 `#session body` 投递
- Rule 6 `@member#session body` 跨成员投递
- Rule 7 私聊单会话默认投递
- **验收**: 全部正常

---

## 八、全链保交付

### 用例 8.1：每条 llm_pending 任务都有终态
- 构造各类 L2 场景（dispatch/clarify/reject/失败/超时），全部应有终态（done/failed）
- **验收**: 无永久悬挂 llm_pending；expire_at 到期 reaper 兜底

### 用例 8.2：L2 处理中 ack 不丢
- 对比 P1 已知 bug（`_ = ack`），确认 L2 路径入队后发起方收到受理 ack
- **验收**: llm_pending 时发起方仍收到 `已受理 #<id>`

### 用例 8.3：全链路追踪
- 从入队到终态（L1 done 或 L2 done/failed/expired），control_task 记录完整
- **验收**: status/attempts/error/layer/target/result 字段齐；source_addr 可追溯回复目标

---

## 执行计划

| 步骤 | 内容 |
|------|------|
| 1 | developer 回执 P2 完成 |
| 2 | manager 通知开跑 |
| 3 | `git fetch && checkout origin/feat/control-plane-p1` |
| 4 | `go test ./internal/m8/ -v -run "L2|Lease|Dispatch|LLM|Clarify|NL"` |
| 5 | `go test ./internal/store/ -v -run "Lease|Control|L2"` |
| 6 | `go test ./...` 全量回归 |
| 7 | 逐项对用例清单填写 通过/失败/复现步骤 |
| 8 | 结果报 zzc-manager；失败项同步 zzc-developer |
