# B1-a Console Agent 编组交付边界

## 身份与短名

- 加入能力只认完整 32 字符组码。
- 6 字符短名是独立、全局唯一的显示标识，不取组码前缀。这是有意偏离“组码＋6 字符短名”可能被理解为截断组码的字面实现，目的是不泄露组码任何位。
- 旧 admin 在 `0.2.2.0` 前仍然并存，且其 `UpsertAgentGroup` 会持续产生 `creator_principal IS NULL` 的组；因此这不是只处理旧库快照的迁移兼容，而是在役写入路径的兼容。此类组优先由 leader endpoint 唯一对应的 `console_agent.owner_principal` 认领；仅当 leader 没有 active `console_agent` 映射时，`management` 才可兜底认领。普通业主不得兜底，`management` 也不得夺走已有 leader owner 可认领的组。

## 删空后的路由与回滚

`agent_group_state.ever_activated=1` 表示该库曾启用显式编组。此后即使删光所有组，也继续按 `default-owner:<ownerKey>` 隔离，不恢复跨 owner 全通。

旧 admin 的 `UpsertAgentGroup` 与 console 新入口一样会把 `ever_activated` 置 1。因此，从任一入口创建第一个组，都会把全局路由从 pre-G 全通切换为按组/owner 隔离。旧 admin 页面目前不提示这一跨门副作用；本版只在交付件明确记录，不擅自增加确认框或改变旧 admin 产品交互。

若编组配置错误且业主明确决定回退到 pre-G 路由，回滚是运维动作，不提供产品 UI。执行前必须取得业主授权，并由生产执行单记录：

```sql
UPDATE agent_group_state SET ever_activated=0 WHERE singleton=1;
```

该 SQL 会恢复表空时的 pre-G 全通语义；执行前须确认 `agent_group` 确为空，并明确记录权限放大的影响。

## 明确未做

- 不做跨业主通知（B1-c）。
- 不搬迁 admin 鉴权或页面（B1-b）。
