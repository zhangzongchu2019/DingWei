# Design

## Context

R5 绑定把 Console principal 映射到 GitHub login 与数字用户 ID，供 R5 服务按真实用户重新核仓库 read 权限。绑定本身不授予 GitHub 权限。基线 c248506cf120c3af2a06eeaeeae3038f596537b1 已有管理员 GET/PUT/DELETE API 与数据库审计，但无页面。

## Goals / Non-Goals

- 目标：在 Console 平台管理区提供一个最小、仅平台管理员可见的绑定页面；按 principal 查询一条当前绑定；通过原 API 建立/覆盖和删除。
- 非目标：新增 API 语义、全量列表 API、GitHub 账号自动查询、凭据管理、GitHub 权限授予、R5 读取逻辑变更或真实环境绑定。

## Decisions

1. **页面入口和鉴权。** 页面入口位于 Console 平台管理区，建议路径 GET /console/openspec-bindings。页面请求必须经 RequireConsolePrincipal 与 RequireConsolePlatformAdmin；管理区入口也仅向平台管理员显示。现有 GET /console/openspec 不适合承载本页，因为它允许普通 Console principal。
2. **只复用现有数据接口。** 查询用 GET /api/console/openspec-bindings?principal=…&provider=github&instance=github.com；写入用无查询串的 PUT 或 DELETE /api/console/openspec-bindings。PUT/DELETE 的 JSON 正文为 Binding 五字段。页面不新增全量查询路由、后端 provider 调用或数据库直连。
3. **同源浏览器请求。** 页面通过同源相对 URL 的 fetch 访问 API。浏览器为 PUT/DELETE 自动生成 Origin；页面将 Content-Type 明确设置为 application/json，并发送 JSON.stringify 后的对象。不要手工设置 Origin；不要给 PUT/DELETE 附 query string。GET 可用 query 参数。
4. **校验和重复写入。** 服务端仍是 Binding.Valid() 与现有 decoder 的权威。页面可做等价格式提示，但不能替代服务端判定。相同 principal/provider/instance 再次 PUT 会 upsert 覆盖 account_id/login，并更新 updated_by/updated_at，同时追加 set 审计；页面先显示旧值，管理员确认后才提交覆盖。DELETE 请求仍需发送完整 Binding，因为现有 handler 解码并验证整个对象；成功删除时 store 先追加 delete 审计，再删行。
5. **错误与秘密。** 用 textContent 显示现有 API 的安全错误字段，不把原始 HTML 插入 DOM；成功后 GET 回读结果。表单不含 token、PAT 或其它凭据；不得把 fetch 请求/错误响应、审计页或截图扩展为凭据显示或回显入口。
6. **列表能力边界。** 现有 GET 返回单条或 null，不存在列出所有 principal 的能力。按 PM 定值，首期仅按输入 principal 显示单条记录，不新增全量列表 API。

## Risks / Trade-offs

- 手填 principal/login/ID 可降低后端与页面外连依赖，但管理员仍可能把 ID 与 login 配错；成功写入后回读并让管理员核对是必要步骤。
- PUT 是覆盖语义。页面确认可以降低误操作，但不能代替审计或把覆盖改成冲突。
- GET 页面/API 的管理员 guard 必须独立验证；只隐藏导航会导致直接 URL 可访问。
- 当前 GET 无审计；本提案不增加查询审计。写入/删除审计沿用数据库事务内既有记录。

## 实施记录（S-R49-R5-BINDING-PAGE-IMPL）

- 页面由console_openspec_binding_page.go嵌入HTML/JS，使用脚本SHA的CSP、no-store与同源connect。仅新增GET页面路由，嵌套既有RequireConsolePrincipal/RequireConsolePlatformAdmin；既有三个API的注册行不变。
- 平台管理菜单复用现有agent_list_admin能力位展示链接；该位由既有active platform_admin检查产生，与托管开关无关。既有Console刷新发现撤权时移除链接；授权始终在页面/API服务端执行。
- 保存与删除均先GET最新旧值，再显示目标与旧映射。待确认操作冻结目标及五字段；改表单/重新查询会取消旧确认。DELETE用GET返回的映射满足现有Binding.Valid要求，无映射不发送DELETE、不声称删除了一条记录。重复点击通过busy与一次性pending阻止；后端仍无CAS，本页不引入冲突语义。
- 页面不另造字段校验，非法字段由原API拒绝并原文显示安全error。API响应只选择Binding五字段渲染，使用textContent，不解析HTML。成功写入后GET回读，显示实际当前映射；provider/instance固定，脚本无外站请求。

## Open Questions

本提案的页面范围与交互三项已按业主 rulings ㉙ 定值。后续如需变更，先更新本变更规格，不在实现阶段自行扩大范围。
