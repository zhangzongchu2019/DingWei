# DingWei 总控 P4 指标看板测试清单（预备）

**测试方**: zzc-tester  
**预备日期**: 2026-07-08  
**被测范围**: feat/control-plane-p4（指标看板 /admin/control-plane）  
**规格依据**: manager 指定（P4 范围缩减为仅看板，其余 backlog）  
**测试方法**: 真机 `go test ./...` + 浏览器/curl 验证页面  
**前置条件**: developer 回执完成 + manager 通知开跑

---

## 一、看板数值一致性

### 用例 1.1：看板数值与 ControlTaskStats API 完全一致
- 预先构造已知数量/状态的 control_task（如 done×3、failed×1、queued×2、llm_pending×1）
- 对比 `/admin/control-plane` 页面展示 vs `ControlTaskStats()` API 返回值
- **验收**: Total、各状态计数、Depth、FailedRate、ExpiredRate 完全一致，不允许另算一套

### 用例 1.2：看板刷新后数据同步
- 新增一条 control_task 后刷新看板
- **验收**: 数值实时反映最新状态（或明确标注缓存延迟）

### 用例 1.3：L2 指标展示
- L2 处理时长 P50/P95、L2 失败率
- 构造已知 L2 任务 → 对比指标
- **验收**: 数值与 ControlTaskStats 一致

---

## 二、空数据不炸

### 用例 2.1：total=0 时页面正常渲染
- 空库（无任何 control_task 记录）
- **验收**: 页面正常加载；各计数显示 0；无 panic/NPE/白屏；无 "NaN" / "Infinity" / 除零错误

### 用例 2.2：无 L2 记录时指标正常
- 有 L1 任务但无 L2 处理记录
- **验收**: P50/P95 显示 "N/A" 或 "-"；L2 失败率显示 0 或 "-"；不炸

### 用例 2.3：仅终态任务（depth=0）
- 所有任务均为 done/failed/expired（无 queued/llm_pending）
- **验收**: Depth=0 正常显示；不出现负值

---

## 三、鉴权

### 用例 3.1：未登录访问被拦截
- `curl -v http://localhost:<port>/admin/control-plane`（无 Cookie/Token）
- **验收**: 返回 302 重定向到登录页，或 401；页面内容不泄露

### 用例 3.2：登录后正常访问
- 使用有效 session 访问
- **验收**: 200 OK，页面正常渲染

### 用例 3.3：鉴权与其它 admin 页一致
- 对比 `/admin`、`/admin/` 的鉴权行为
- **验收**: 使用同一鉴权中间件/逻辑，无绕过路径

---

## 四、自包含无外部依赖

### 用例 4.1：离线 / 断 CDN 能渲染
- 断开外网或 mock CDN 不可达
- **验收**: 页面核心内容（数据表格/指标）正常渲染；不依赖外部 CSS/JS/Font CDN（或内联 fallback）

### 用例 4.2：纯服务端渲染或内联资源
- 检查页面源码
- **验收**: CSS/JS 为内联或同源静态资源；无跨域外部请求（或明确列出允许的外部依赖）

---

## 五、无回归

### 用例 5.1：P3 decompose/aggregate 不变
- `TestControlPlaneP3DecomposeChildrenAggregateAndNotify` 仍绿
- `TestControlPlaneP3PartialChildFailureStillAggregates` 仍绿
- `TestControlPlaneP3AggregateClaimReclaimsExpiredLease` 仍绿
- `TestControlPlaneP3L2ClaimHonorsPriority` 仍绿

### 用例 5.2：P2 L2 分诊/重试链不变
- `TestControlPlaneP2DispatchAndClarifyWithMockLLM` 仍绿
- `TestControlPlaneP2ProviderFailureRetriesAndFails` 仍绿
- `TestControlPlaneP2ProviderFailureRequeuesUntilMaxAttempts` 仍绿
- `TestControlPlaneP2LeaseClaimIsExclusiveAndReclaimsExpired` 仍绿

### 用例 5.3：P1 L1/secOps 不变
- `TestSecurityOpsRejectsUnauthorizedSender` 仍绿
- `TestSecurityOpsAllowsBoundAdminOwner` 仍绿
- `TestSecurityOpsRoutesToAllSystemSessions` 仍绿
- `TestControlPlaneP1DispatchPersistsL1DoneAndLLMPending` 仍绿

### 用例 5.4：纯只读，不写总控逻辑
- 访问看板期间，检查 control_task 表无额外写入
- **验收**: 看板只做 SELECT；无 INSERT/UPDATE/DELETE；不修改任何总控状态

---

## 六、状态计数口径

### 用例 6.1：队列深度 = 非终态之和
- Depth = queued + llm_pending + dispatched + awaiting_result + awaiting_children
- **验收**: 看板展示与公式一致；不包含 done/failed/expired

### 用例 6.2：各状态独立计数正确
- 每类状态各构造 1 条任务，验证计数
- **验收**: queued/done/failed/expired/llm_pending/dispatched/awaiting_result/awaiting_children 全部正确

### 用例 6.3：FailedRate 计算正确
- FailedRate = failed / total（total > 0）
- **验收**: 数值精确（至少 1 位小数）；total=0 时不除零

### 用例 6.4：ExpiredRate 计算正确
- ExpiredRate = expired / total
- **验收**: 同上

### 用例 6.5：L2 处理时长 P50/P95
- P50 = 所有 L2 done 任务处理时长中位数；P95 = 95 分位
- **验收**: 无 L2 记录时不炸；有时数值合理

---

## 执行计划

| 步骤 | 内容 |
|------|------|
| 1 | developer 回执 P4 看板完成 |
| 2 | manager 通知开跑 |
| 3 | `git fetch && checkout origin/feat/control-plane-p4` |
| 4 | 启动服务 → `curl` 验证鉴权 + 页面可达 |
| 5 | 构造 control_task 数据 → 对比页面与 ControlTaskStats API |
| 6 | 验证空数据 / 边界 / 离线渲染 |
| 7 | `go test ./...` 全量回归 |
| 8 | 逐项填写 通过/失败/复现步骤 |
| 9 | 结果报 zzc-manager |
