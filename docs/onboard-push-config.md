# 客户端上线指南配置

Hub 将时间戳要求、会话间发信方法、提交身份和项目简报合并进现有 Agent 网络指南。
同一 `(key_id, session_name)` 默认一小时只推送一次；可用环境变量调整：

```bash
WP_ONBOARD_PUSH_INTERVAL=1h
```

身份和简报保存在数据库 `app_config`，按 owner 隔离读取。示例：

```sql
INSERT INTO app_config(key, value_json, updated_at) VALUES(
  'onboard.git_identities',
  '{"fulei":{"public":{"name":"Fu Lei","email":"public@example.com"},"private":{"name":"符磊","email":"private@example.com"}}}',
  strftime('%Y-%m-%dT%H:%M:%fZ','now')
) ON CONFLICT(key) DO UPDATE SET value_json=excluded.value_json, updated_at=excluded.updated_at;

INSERT INTO app_config(key, value_json, updated_at) VALUES(
  'onboard.project_briefs',
  '{"fulei":"当前项目、职责、关键约束和交接入口。"}',
  strftime('%Y-%m-%dT%H:%M:%fZ','now')
) ON CONFLICT(key) DO UPDATE SET value_json=excluded.value_json, updated_at=excluded.updated_at;
```

公开仓和私有仓身份分别配置。某 owner 缺少配置时，指南只提示先确认身份，不猜测邮箱，
也不会回退到其他 owner 的配置。项目简报同样只展示当前 owner 对应项。
