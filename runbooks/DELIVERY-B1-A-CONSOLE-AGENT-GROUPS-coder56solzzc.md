# B1-a Console Agent 编组交付边界

## 身份与短名

- 加入能力只认完整 32 字符组码。
- 6 字符短名是独立、全局唯一的显示标识，不取组码前缀。这是有意偏离“组码＋6 字符短名”可能被理解为截断组码的字面实现，目的是不泄露组码任何位。
- 历史组 `creator_principal IS NULL` 时，优先由 leader endpoint 唯一对应的 `console_agent.owner_principal` 认领；leader 无映射时，仅 `management` 角色可兜底认领。普通业主不得兜底。

## 删空后的路由与回滚

`agent_group_state.ever_activated=1` 表示该库曾启用显式编组。此后即使删光所有组，也继续按 `default-owner:<ownerKey>` 隔离，不恢复跨 owner 全通。

若编组配置错误且业主明确决定回退到 pre-G 路由，回滚是运维动作，不提供产品 UI。执行前必须取得业主授权，并由生产执行单记录：

```sql
UPDATE agent_group_state SET ever_activated=0 WHERE singleton=1;
```

该 SQL 会恢复表空时的 pre-G 全通语义；执行前须确认 `agent_group` 确为空，并明确记录权限放大的影响。

## 明确未做

- 不做跨业主通知（B1-c）。
- 不搬迁 admin 鉴权或页面（B1-b）。
