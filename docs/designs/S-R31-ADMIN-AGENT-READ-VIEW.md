# R31 A14 后继：管理员全量 Agent 只读视图

依据组长 aa0ffb5dee1a 的 REWORK 处置及 2026-10-19T03:54Z 催办。在 f547f025 后追加；目标是补齐“本人及管理员可见来源 IP”的生产管理员响应路径，兼顾管理员查看全部用户 Agent。仅开发，不部署、不变更43，不接P2/运行时操作。

- **接口**：`GET /api/console/platform/agents?limit=100&after=<last-id>`，由生产 `identity/httpapi.API.Register` 挂载，使用现有 `N3.Actor` 会话解析。显式要求 Console cookie，任意 Authorization 头（包括 bearer＋管理员 cookie）均拒；每次重新读取用户，只有 active platform_admin 通过，manager 不继承。缺会话401、无权限403、参数错误400、存储/审计错误不返回私有页面。
- **分页**：默认100、范围1–200；按 Agent ID 确定性升序，读取 limit＋1 判断下一页，响应 `agents/limit/next_after`。拒绝重复参数、超长或NUL游标。最后一页及空页 next_after 为空。响应 no-store；不接受 owner 参数来扩大普通列表范围。
- **字段**：白名单投影 Agent ID、owner（user_id/short_name/display_name）、full_name、agent_type、host、online、身份 state。自有 host 是 identity 的 last_source_ip，并可带 last_source_at；托管只从已有 HostedItem 取 node_name，独立于创建托管的 allowlist，绝不透传账户/凭据子对象。在线状态复用 Hub.AgentOnline。来源未知显示“未知”；无托管后端不伪造节点名。
- **全集范围**：包含各用户 active/suspended/revoked 的库存记录，普通本人列表仍按原未吊销规则工作。异常孤立 owner 引用也保留 Agent ID/owner_lc，缺简称时不伪造 owner 后缀。用户停用不删除其历史 Agent。
- **存储/审计**：独立可选接口 AdminAgentStore、独立 AdminAgentRecord，来源字段 JSON 忽略，不改通用 Agent、编组或事件模型。PG 同事务用用户行 FOR SHARE 重新核 active/admin，取有界页面，再写一条 `agent.admin_list` 审计（actor、all-agents、limit/returned、UTC）；审计失败回滚并返回空页面。MemStore 同一互斥区间做同义操作。审计不写来源IP、凭据或整页数据。拒绝请求不记为成功读取。
- **页面**：平台管理新增“全部 Agent”只读能力/列表、刷新和下一页。服务器实时下发 agent_list_admin，独立于是否开启托管；manager无菜单。渲染使用 textContent，只有 GET；进入/刷新/下一页各一次读，不后台轮询审计。加载开始、错误、离开或能力撤回均清空旧行；AbortController＋generation 丢弃过期响应。
- **迁移/回退**：没有DDL或新增账本，复用0103来源字段和既有 audit_log。新旧二进制数据库可读性不新增变化；R31整体开放后的回退限制仍按原设计/P4执行单，不能借本只读增量授权回退。
- **交付门**：Mem/PG共享分页与权限契约；真实N3 cookie的HTTP成功/拒权/审计/本人列表隔离；PG审计写入失败不返回页面；平台能力接线；托管关闭情况下的真实页面浏览器、分页/错误清除/manager无入口；权限、来源响应、节点投影、审计、普通列表围栏及页面各做指定撤修。原 f547f025 的完整实现与浏览器修复一并保留，最终精确锚待本后继交件确定。
