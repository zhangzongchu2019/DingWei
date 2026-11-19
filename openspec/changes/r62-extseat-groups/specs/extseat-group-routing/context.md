# 上下文

冻结基线 main 206d41b2。原服务 internal/extseat/service.go 的 Authenticate/authTx 绑定单 AG；inbox/outbox 的 AG 字段也同时承担路由和授权。internal/identity/group.go 已有普通组和 leader_only 策略；tempgroup/service.go 创建 AG 的 np_group 镜像。网关 internal/mcpgw、飞书 state internal/feishulogin、Hub external routing 与 Console 编组都需使用一致的服务端授权。

设计来源 eb194143，切片三任务 2026-11-19T01:21:08Z。0112 保留显示名，0113 本片；TTL 与 N-6 不在本片。原配对地址和无 routing 的旧 MCP 请求保持原语义。
