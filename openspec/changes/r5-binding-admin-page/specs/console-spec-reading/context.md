# R5 绑定管理页变更上下文

## 基线与来源

基线 commit：c248506cf120c3af2a06eeaeeae3038f596537b1。需求来源是 review/opus50tc2k/rulings commit cf116475ebc6d40c977269be55d30d237d8b563d 第㉘条；实现范围仅 R5 Console 绑定管理页。

## 代码位置

- cmd/workpulse/console_routes.go：现有 GET/PUT/DELETE /api/console/openspec-bindings，均套 RequireConsolePrincipal 与 RequireConsolePlatformAdmin；现有 GET /console/openspec 是普通登录态页面，不可继承为管理员授权。
- internal/m8/console_openspec_read.go：HandleOpenSpecBinding 从第 102 行起；GET 调用 SpecReadBinding(principal, provider, instance) 读取单条映射；PUT/DELETE 要求同源 Origin、Content-Type application/json、无 query string，并将 JSON 解码为 Binding、拒绝未知字段且要求 Valid()。
- internal/specread/service.go：Binding 字段 principal/provider/instance/account_id/login 与 Valid() 格式校验。
- internal/store/openspec_read_binding.go：upsert 和 delete 的事务及 openspec_read_binding_audit 写入。
- internal/store/migrations/0070_openspec_read_binding.sql：绑定与审计表；无全量读取 API 的必要索引/方法契约。
- internal/m8/console_openspec_read_page.go 与相邻页面文件：页面嵌入/同源 fetch 的实现参考；新页面须自行通过管理员 page route 鉴权。

## 请求与审计语义

GET 使用 principal/provider/instance query，返回一条绑定或 null；没有全量列出全部绑定的 API。PUT 对相同主键 upsert 覆盖并记 operation=set、updated_by、updated_at。DELETE 先复制既有行到 operation=delete 审计，再删绑定；不存在记录时可能无 delete 审计行。GET 不写审计。页面不直接访问数据库。

## 身份、校验与环境

Binding.Valid() 要求 principal 非空且不超过 200 字符；provider 精确为 github；instance 精确为 github.com；account_id 是 1–20 位正整数十进制字符串且无前导零；login 是 1–39 位 GitHub 登录名格式（首位字母/数字，其余字母/数字/连字符）。服务端校验始终权威。

同源 fetch 对 PUT/DELETE 自动携带浏览器生成的 Origin；页面需明确设置 Content-Type: application/json 和 JSON body，避免把该头误称浏览器默认。当前没有自动 GitHub 用户查询或凭据输入；此变更不增加任何服务器/浏览器外部请求。

实现与验收仅在另行批准后进行。本上下文不包含真实 PAT、绑定、用户登录态或线上读数；不据此推断环境已具备。
