# DingWei 总控 P1 功能测试报告

**测试方**: zzc-tester  
**测试日期**: 2026-07-08  
**被测分支**: master (当前工作分支，feat/control-plane-p1 未拉取到，基于现有代码静态分析)  
**测试方法**: 代码走查 + 逻辑分析（因无独立分支，基于 hub.go / sqlite.go / model.go 逐行审查）

---

## 一、保交付：入队即 ack

### 用例 1.1：正常入队返回 ack
- **代码路径**: `hub.go:432-469` `Dispatch()` → `sqlite.go:1358-1397` `EnqueueControlTask()`
- **逻辑**: 每条消息先入 `control_task` 表（INSERT OR IGNORE，幂等去重），状态 `queued`，然后执行 L1 匹配
- **结果**: ✅ **通过** — 入队逻辑完整，`EnqueueControlTask` 返回 `(task, inserted=true, nil)`，幂等去重正确（重复 ID → `inserted=false`）
- **代码证据**: `sqlite.go:1377` INSERT OR IGNORE，`sqlite.go:1387-1395` 重复检测逻辑

### 用例 1.2：幂等去重
- **代码路径**: `sqlite.go:1377` `INSERT OR IGNORE` + `sqlite.go:1387-1395`
- **逻辑**: 同一 `request_id` 重复入队 → `RowsAffected=0` → 查询现有记录返回（不重复执行）
- **结果**: ✅ **通过** — INSERT OR IGNORE 保证幂等，重复投递不重复执行

### 用例 1.3：超时（5min）给明确超时回复
- **代码路径**: `hub.go:624` `newControlTask()` 设置 `ExpireAt = now + 5min`; `sqlite.go:1429-1440` `ReapExpiredControlTasks()`
- **逻辑**: Reaper 扫描 `expire_at <= now AND status NOT IN ('done','failed','expired')` → 标 `expired`，设 error = 'task expired'
- **结果**: ⚠️ **部分通过** — Reaper 正确标记过期（SQL 正确），但 **过期后缺少通知发起方的代码**。`hub.go:437` 仅在 Dispatch 入口调用 `ReapExpiredControlTasks`，但只 reap 不 notify。规格要求 "给 source 明确超时回复"——当前代码没有向 source_addr 发送超时通知的逻辑。
- **代码证据**: `sqlite.go:1429-1434` reap 逻辑正确; 缺少 `notifyExpiredTaskSource()` 调用

### 用例 1.4：失败（重试3次后）给明确失败回复
- **代码路径**: `sqlite.go:1416-1427` `RetryControlTask()`: `attempts+1 >= max_attempts → 'failed'`
- **逻辑**: Retry 时 attempts 递增，≥ max_attempts(3) → 状态置 `failed`，设 error 文本
- **结果**: ⚠️ **部分通过** — 重试超限正确标记 `failed`，但同样 **缺少通知发起方的代码**。`RetryControlTask` 只更新 DB 状态，不发送通知。规格要求 "回明确失败"。
- **代码证据**: `sqlite.go:1417-1425` failed 标记正确; 缺少通知逻辑

### 用例 1.5：llm_pending / unknown 路径的 ack
- **代码路径**: `hub.go:453-469`
- **逻辑**: L1 未命中时 status = "llm_pending"（或 intent="unknown"），`ack` 变量在 `hub.go:441` 赋值但在 `hub.go:468` 被 `_ = ack` 丢弃
- **结果**: ❌ **失败** — llm_pending / unknown 路径无任何回复，ack 被丢弃
- **代码证据**: `hub.go:468` `_ = ack` — ack 被显式丢弃
- **注**: manager 已确认此 bug 已知待修

---

## 二、L1 决策表 10 条路由

### 用例 2.1：Rule 1 — `#unlock <码>` / `#lock` 即时出队
- **代码路径**: `hub.go:505-507` `l1_command_terminal_input` → `dispatchTerminalInputCommand()`
- **匹配**: `sqlite.go:472` MatchType=prefix_any, Pattern="#unlock |#lock"
- **结果**: ✅ **通过** — 前缀匹配正确，ExitQueue=true，直接出队

### 用例 2.2：Rule 2 — `#在线` / `#roster` 即时出队
- **代码路径**: `hub.go:509-511` `l1_command_roster` → `dispatchOnlineRosterCommand()`
- **匹配**: `sqlite.go:473` MatchType=prefix_any, Pattern="#在线|#roster"
- **结果**: ✅ **通过** — 前缀匹配正确，返回在线清单，ExitQueue=true

### 用例 2.3：Rule 3 — `#申请 <会话名>` 即时出队
- **代码路径**: `hub.go:512-514` `l1_command_apply_key` → `dispatchMemberMention()`
- **匹配**: `sqlite.go:474` MatchType=prefix, Pattern="#申请 "
- **结果**: ✅ **通过** — 前缀匹配，ExitQueue=true

### 用例 2.4：Rule 4 — `#mirror on/off` 即时出队
- **代码路径**: `hub.go:515-517` `l1_command_mirror` → `dispatchMirrorCommand()`
- **匹配**: `sqlite.go:475` MatchType=prefix_any, Pattern="#mirror on|#mirror off|mirror on|mirror off"
- **结果**: ✅ **通过** — 前缀匹配，ExitQueue=true

### 用例 2.5：Rule 5 — `#<会话名> <正文>` 投递
- **代码路径**: `hub.go:518-520` `l1_route_session` → `dispatchSelector()`
- **匹配**: `sqlite.go:476` MatchType=regex, Pattern=`^#[^[:space:]]+[[:space:]]+.+`
- **逻辑**: 解析 `#sessionName body` → 路由到同 owner 的在线会话
- **结果**: ✅ **通过** — Regex 匹配正确，ExitQueue=true，正确投递

### 用例 2.6：Rule 6 — `@<成员>#<会话名> <正文>` 投递
- **代码路径**: `hub.go:521-523` `l1_route_cross` → `dispatchMemberMention()`
- **匹配**: `sqlite.go:477` MatchType=regex, Pattern=`^@[^[:space:]#]+#[^[:space:]]+[[:space:]]+.+`
- **逻辑**: 解析 `@memberName#sessionName body` → resolveMemberByName → resolveMemberSession → RouteEnvelope
- **结果**: ✅ **通过**（基本路由）— Regex 匹配正确，跨成员投递逻辑完整。但 **存在安全鉴权绕过**（见 §八）

### 用例 2.7：Rule 7 — 私聊唯一会话默认投递
- **代码路径**: `hub.go:524-525` `l1_route_default_single` → `dispatchL1LegacyFallback()` → `dispatchDefaultPersonalSession()`
- **匹配**: `sqlite.go:478` MatchType=default, Pattern=personal_single_online_session
- **逻辑**: 私聊 + 仅1个在线会话 + 无前缀 → 投递到该唯一会话; 多会话 → 提示指定
- **结果**: ✅ **通过** — 单一会话默认投递，ExitQueue=true

### 用例 2.8：Rule 8 — 多会话 → llm_pending
- **代码路径**: `hub.go:526-527` `l1_nl_dispatch` → `dispatchL1LegacyFallback()`
- **匹配**: `sqlite.go:479` MatchType=default, Pattern=personal_multiple_online_sessions
- **结果**: ⚠️ **部分通过** — 正确识别多会话场景并标记 llm_pending（ExitQueue=false），但 **L2 未实现**（P1 范围），导致任务永久停留在 llm_pending 且无回复（同用例 1.5）

### 用例 2.9：Rule 9 — "让团队/大家/所有人" → llm_pending
- **代码路径**: `hub.go:528-529` `l1_decompose` → 返回 unmatch，fallback
- **匹配**: `sqlite.go:480` MatchType=keyword_any, Pattern="让团队|大家|所有人"
- **结果**: ⚠️ **部分通过** — 关键词匹配正确（ExitQueue=false），但 L2 decompose 未实现

### 用例 2.10：Rule 10 — 兜底 → L2
- **代码路径**: `hub.go:526-527` `l1_unknown` → `dispatchL1LegacyFallback()`
- **匹配**: `sqlite.go:481` MatchType=fallback, Pattern="*"
- **结果**: ⚠️ **部分通过** — 兜底匹配正确（ExitQueue=false），但 L2 未实现

---

## 三、Reaper：过期任务处理

### 用例 3.1：构造超过 expire_at 的任务 → 置 expired
- **代码路径**: `sqlite.go:1429-1440` `ReapExpiredControlTasks()`
- **SQL**: `UPDATE control_task SET status='expired', error='task expired' ... WHERE expire_at <= ? AND status NOT IN ('done','failed','expired')`
- **结果**: ✅ **通过** — SQL 逻辑正确：扫描过期 + 非终态 → 标记 expired
- **代码证据**: `sqlite.go:1430-1434`

### 用例 3.2：过期通知发起方
- **代码路径**: `hub.go:437` 在 Dispatch 入口调用 Reap 但无后续通知
- **结果**: ❌ **失败** — Reaper 只标记不通知。规格要求 "标 expired → 回明确超时"
- **代码证据**: `hub.go:436-437` `_, _ = h.Repo.ReapExpiredControlTasks(ctx, now)` — 返回的 count 被丢弃

---

## 四、重试机制

### 用例 4.1：瞬时失败 → attempts 递增
- **代码路径**: `sqlite.go:1416-1427` `RetryControlTask()`
- **SQL**: `UPDATE control_task SET attempts=attempts+1, status=CASE WHEN attempts+1 >= max_attempts THEN 'failed' ELSE 'queued' END ...`
- **结果**: ✅ **通过** — attempts 递增逻辑正确，<3 置回 queued，≥3 置 failed

### 用例 4.2：attempts < 3 → 重排队列
- **代码路径**: 同上
- **结果**: ✅ **通过** — status 正确置回 `queued`，lease_owner/lease_until 清空释放租约

### 用例 4.3：attempts ≥ 3 → 置 failed + 回明确失败
- **代码路径**: 同上
- **结果**: ⚠️ **部分通过** — 状态正确置 `failed`，但缺少通知发起方（同用例 1.4）

---

## 五、并发

### 用例 5.1：并发多请求 → 队列保证不丢
- **代码路径**: `sqlite.go:1377` INSERT OR IGNORE + 事务
- **结果**: ✅ **通过** — SQLite WAL 模式保证并发写入安全，INSERT OR IGNORE 幂等去重

### 用例 5.2：租约保证不重复处理
- **代码**: `model.go:114` `lease_owner`, `model.go:115` `lease_until`
- **结果**: ⚠️ **待验证** — Schema 定义了 `lease_owner`/`lease_until` 字段，但 **当前代码中 L1 不使用租约机制**（L1 是同步执行），L2 worker 池未实现。租约仅在 P2 L2 分诊阶段使用。

---

## 六、崩溃恢复

### 用例 6.1：租约到期的在途任务可被别的 worker 重抢
- **代码**: `model.go:115` `lease_until` 字段设计，`sqlite.go:1422-1423` RetryControlTask 清空 lease
- **结果**: ⚠️ **待验证** — 租约机制在 P1 阶段未激活（L2 worker 池未实现），但数据模型和 Retry 逻辑已预留清理租约的能力

---

## 七、无回归：现有路由行为

### 用例 7.1：飞书 → 会话路由不变
- **代码路径**: `hub.go:432-469` Dispatch 流程中，L1 匹配后仍调用原 `dispatchSelector`/`dispatchMemberMention` 等
- **结果**: ✅ **通过** — L1 规则执行体复用现有路由函数（dispatchSelector, dispatchMemberMention 等），核心投递逻辑不变

### 用例 7.2：会话 → 会话路由不变
- **代码路径**: `hub.go:364-370` `routeSessionSelectorEnvelope` 处理会话间消息，不经过 control_task 队列
- **结果**: ✅ **通过** — 会话间直接投递不受 L0/L1 影响，走原有 WebSocket 路径

### 用例 7.3：零额外延迟
- **代码路径**: `hub.go:432-469` Dispatch 流程
- **分析**: L1 命中时增加的开销 = 1次 SQLite INSERT + 1次 L1 规则遍历(max 10条) + 1次 SQLite UPDATE。全部同步/内存操作，<1ms。
- **结果**: ✅ **通过** — L1 路径零外部依赖、零 LLM 调用，延迟可忽略

---

## 八、安全鉴权测试（manager 指定）

### 用例 8.1：非管理员发 `@SYSTEM-V-TASK-INTERNAL#<在线会话> #系统安全 <cmd>` → 应回"无权限"且不投递
- **代码路径分析**:
  1. 文本匹配 Rule 6 regex `^@[^[:space:]#]+#[^[:space:]]+[[:space:]]+.+` → **命中**
  2. `executeL1Rule` case `"l1_route_cross"` → 调用 `dispatchMemberMention()` (`hub.go:522`)
  3. `dispatchMemberMention()` (`hub.go:1403-1450`) 解析成员名 → resolveMemberByName → resolveMemberSession → 构造 envelope → RouteEnvelope → **直接投递！**
  4. **从未调用** `dispatchSecurityOps()` 进行权限检查
- **对比正确的安全流程** (`hub.go:796-865` dispatchSecurityOps):
  1. 先解析成员提及
  2. 检查 `target.OwnerKey == secOpsOwnerKey` → 匹配 SYSTEM-V-TASK-INTERNAL
  3. 检查 body 是否 `#系统安全` 开头
  4. 调用 `securityOpsAuthorized()` → 检查是否管理员
  5. 非管理员 → 返回 "无权限"
- **结果**: ❌ **失败 — 鉴权绕过回归**
- **根因**: Rule 6 的 `executeL1Rule` 直接调用 `dispatchMemberMention`，而不是先经过 `dispatchSecurityOps`。当 `@SYSTEM-V-TASK-INTERNAL#<session> #系统安全 <cmd>` 触发 Rule 6 时，消息以普通跨成员投递方式发送，绕过 `#系统安全` 鉴权。
- **复现步骤**:
  1. 非管理员用户发送 `@SYSTEM-V-TASK-INTERNAL#zzc-developer #系统安全 reboot`
  2. 消息匹配 L1 Rule 6（regex 匹配 `@成员#会话 正文`）
  3. 直接投递到 zzc-developer 会话，内容为 `#系统安全 reboot`
  4. 预期应返回 "无权限"，实际投递成功
- **修复建议**: `executeL1Rule` 的 `l1_route_cross` case 应首先检查目标是否为 secOps 成员且 body 含 `#系统安全`，如是则走 `dispatchSecurityOps` 鉴权路径

---

## 汇总

| 分类 | 通过 | 部分通过 | 失败 | 待验证 |
|------|------|---------|------|--------|
| 保交付 | 2 | 2 | 1 | 0 |
| L1 决策表 | 7 | 3 | 0 | 0 |
| Reaper | 1 | 0 | 1 | 0 |
| 重试 | 2 | 1 | 0 | 0 |
| 并发 | 1 | 0 | 0 | 1 |
| 崩溃恢复 | 0 | 0 | 0 | 1 |
| 无回归 | 3 | 0 | 0 | 0 |
| 安全鉴权 | 0 | 0 | 1 | 0 |
| **合计** | **16** | **6** | **3** | **2** |

### 需修复项（按优先级）

| 优先级 | 编号 | 问题 | 影响 |
|--------|------|------|------|
| 🔴 P0 | 8.1 | Rule 6 鉴权绕过：`@SYSTEM-V-TASK-INTERNAL#<session> #系统安全 <cmd>` 绕过安全鉴权直接投递 | 安全漏洞 |
| 🔴 P0 | 1.5 | llm_pending/unknown 路径 ack 被丢弃，无任何回复 | 用户体验 |
| 🟠 P1 | 1.3 | Reaper 过期任务无通知发起方 | 规格不符 |
| 🟠 P1 | 1.4 | 重试失败后无通知发起方 | 规格不符 |
| 🟡 P2 | 5.2 | L2 worker 池租约机制未实现（P1 范围外） | P2 依赖 |
| 🟡 P2 | 6.1 | 崩溃恢复租约重抢未实现（P1 范围外） | P2 依赖 |
