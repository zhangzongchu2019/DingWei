# C 路飞书网页登录接口

本包实现飞书新版 OAuth 网页授权页，不使用旧版内嵌二维码 SDK，也不实现 MCP OAuth 授权服务器。B 路负责外层 MCP OAuth、PKCE、资源 audience 与四项 scope，C 路只负责上游飞书身份登录及调用 A 路签发。

## 配置与装配

- `OAuthConfig`：`AppID`、`AppSecret`、服务端登记的 `RedirectURI` 必填。默认端点为 `accounts.feishu.cn` 的新版授权与 OAuth v3 令牌端点，以及 `open.feishu.cn` 的 `authen/v1/user_info`。`HTTPClient` 可注入；端点覆盖只用于假服务器测试。
- `Config.TeamTenantKey` 是独立必填项，生产装配从 `DW_FEISHU_TEAM_TENANT_KEY` 读取。不得从前端参数、其他飞书功能配置或首个登录用户推断。
- `Config.AppID`、`RedirectURI` 必须与 `OAuthConfig` 中对应值相同。state 同时绑定这两个值及 `IssueRequest`。
- `StateStore.Consume` 必须原子完成。生产使用多实例共享且持久化的实现；`MemoryStateStore` 只用于测试和单进程开发。
- `TokenIssuer` 由组长在集成层实现的 A 路薄适配器提供。C 不导入 `internal/extseat`、不保存本地令牌，也不保存飞书访问令牌或刷新令牌。
- audience 与四项 scope 不在 C 另设默认值；由 A／B／C 的集成装配读取同一配置。MVP audience 暂为 `https://dw123.cmjrt.com/mcp`，scope 固定为 `list_online`、`send_message`、`wait_for_messages`、`ack`，不做子集授权。

## B 路两个入口

### 开始飞书登录

```go
Start(ctx context.Context, req StartRequest) (StartResult, error)
```

`StartRequest` 包含使用方短名 `ShortName`、目标全名 `TargetAgent` 和可选 `AttemptID`。短名必须精确匹配共享包规则，目标去除空白的检查不替代 A 路活跃目标校验。`AttemptID` 是调用方提供的登录尝试关联标识，随 state 绑定并传给集成适配器；当前 A 路中间签名不接收它，因此 C 不宣称它已在 A 侧形成持久幂等。

B 将外层 MCP 授权请求与本次浏览器流程绑定后调用 `Start`，再把浏览器重定向到 `StartResult.AuthorizationURL`。state 为 256 位随机值，只在授权 URL 中返回；存储层只保留 SHA-256 哈希，默认五分钟过期。state 暂存的 `IssueRequest.Principal` 必须为零值，身份只能来自回调中的服务端飞书响应。

### 飞书回调完成

```go
Callback(ctx context.Context, req CallbackRequest) (CallbackResult, error)
```

B 仅把飞书回调 query 中的 `state`、`code` 填入 `CallbackRequest`。该结构没有 tenant、open_id、显示名或目标字段，回调不能覆盖开始时绑定的意图。

C 原子消费 state，核对 App ID 与回调地址，用授权码换用户令牌，再GET `user_info`。只有服务端身份通过校验才调用：

```go
IssueToken(ctx context.Context, req IssueRequest) (IssuedToken, error)
```

C 在 `ResolvePrincipal` 成功后才填 `IssueRequest.Principal`，再调用以上消费者接口。成功的 `CallbackResult` 携带 A 返回的本地 `Token`、`SeatID`、`ExternalAgentFullName`、`Group`、`ExpiresAt`、`Audience`、`Scopes`，交给 B 完成其标准 OAuth 授权响应。字段语义与集成映射见仓根 `EVIDENCE.md`。本地令牌明文只走这个瞬态签发响应，不写日志、审计、state 或错误。B 也不得记录 `CallbackRequest`、飞书访问令牌或 `CallbackResult.Token`。


## 接入页 HTTP 接口

C 提供服务端处理函数，由 B 或集成装配挂到一个同源路径，例如 `/feishu/connect`：

```go
ServeConnectPage(w http.ResponseWriter, r *http.Request)
```

### GET 渲染表单

- 唯一可选 query 为 `attempt_id`，它是 B 生成的外层授权事务关联标识。C 不接收 MCP 客户端 `redirect_uri`、client_id、scope 或目标清单。
- 返回服务端渲染 HTML，无 JavaScript、无第三方资源。短名输入有 `required`、`minlength="1"`、`maxlength="8"`、`pattern="[a-z0-9]{1,8}"`，并提示「外部 Agent 名将为 短名_AG序号」；目标 Agent 全名为自由文本，不列平台 Agent 清单。
- GET 生成 256 位随机 CSRF token，隐藏字段为 `csrf_token`；存储层只存 SHA-256 哈希，`LoginAttempt.Purpose=connect_form_csrf`，默认五分钟过期，并绑定 App ID、C 的飞书回调地址与 AttemptID。
- 所有 GET／POST／错误响应均设置 `Content-Security-Policy: default-src 'none'; style-src 'unsafe-inline'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'`、`X-Frame-Options: DENY`、`Cache-Control: no-store`、`Referrer-Policy: no-referrer` 与 `X-Content-Type-Options: nosniff`。

### POST 校验并进入飞书

表单字段为 `csrf_token`、`attempt_id`、`short_name`、`target_agent`。C 原子消费 CSRF，核对用途、App ID、飞书回调地址及 AttemptID，再调用既有 `Start`。CSRF 缺失、无效、过期、重放或与 OAuth state 交叉使用均拒绝；失败后的合法表单重渲染会签发新 CSRF。

短名前后端都按 `extshared.ValidShortName` 的 1～8 位小写字母数字规则校验。目标仅做 `名称.属主简称` 形状校验：名称当前为 2～32 位字母数字，属主简称当前为 2～16 位字母数字；不查询存在性、不列清单，活跃状态及后续权限仍由 A 路 `IssueToken` 现核。成功返回 303 到 `StartResult.AuthorizationURL`，OAuth state 绑定短名、目标和 AttemptID。

B 的飞书回调适配只允许提取两个 query：

```go
CallbackRequestFromQuery(query url.Values) CallbackRequest // 只取 state、code
```

`short_name`、`target_agent`、身份字段及 `redirect_uri` query 一律忽略；签发意图只能取自消费的服务端 OAuth state。`CallbackResult.AttemptID` 把完成的外层事务标识交给 B。

## B 路 MCP OAuth 衔接约定

本包不实现 B 的 MCP 授权服务器。两路按以下字段和顺序衔接：

1. B 的 MCP OAuth `authorize` 端点先验证客户端登记的 `client_id`、`redirect_uri`、PKCE、audience 与固定四项 scope，并生成不可猜测的 `AttemptID`。B 在自己服务端事务中保存 `AttemptID` 与 `client_id`、已验证的客户端 `redirect_uri`、外层 `state`、`code_challenge`、`code_challenge_method`、audience、scopes 的绑定。
2. B 以 302 将浏览器带到 C 接入页，例如 `/feishu/connect?attempt_id=<url编码值>`。不得把 MCP 客户端 `redirect_uri` 交给 C 表单或作为 C 的开放回跳参数。
3. C POST 成功后带浏览器完成飞书 OAuth。飞书 `Config.RedirectURI` 指向 B 暴露的飞书回调端点，不是 MCP 客户端 `redirect_uri`。
4. B 在该回调端点用 `CallbackRequestFromQuery(r.URL.Query())` 构造输入并调用 `Callback`。成功后按 `CallbackResult.AttemptID` 取回并原子消费 B 的外层事务，同时取得本地 Token、SeatID、ExternalAgentFullName、Group、ExpiresAt、Audience、Scopes。
5. B 生成自己的一次性 MCP authorization code，将本地令牌及签发元数据保存在服务端，再 302 到事务中预先验证的 MCP 客户端 `redirect_uri`，query 只带 B 生成的 `code` 与原外层 `state`。本地 Bearer 明文不得放进浏览器回跳 URL；MCP token 端点再按标准 OAuth 流程交付。

## 身份与错误

- `Principal.Provider` 固定为 `feishu`，`Issuer` 为 App ID，`Subject` 为 `open_id`，`TenantKey` 为可信 `user_info.tenant_key`，`UnionID` 仅作别名。唯一键继续由 `Principal.Key` 给出，显示名不参与合并。
- tenant 缺失或与独立配置不精确相等，返回 `extshared.ErrTenantRejected`；缺 `open_id` 返回 `ErrIdentityIncomplete`。
- 显示名优先使用去首尾空白后的 `en_name`。缺省时对 `name` 使用 go-pinyin `Normal` 风格，无分隔连接、转小写并逐字节只留 `a-z`；结果为空返回 `ErrDisplayName`，不签令牌。
- state 不存在、已消费、应用或回调地址不符返回 `ErrInvalidState`；到期返回 `ErrStateExpired`。飞书失败返回 `ErrUpstream`，A 签发失败返回 `ErrTokenIssue`，均不回传上游错误正文。

A 路必须自行保证本地令牌只存哈希、吊销立即生效、过期不复活及从最近有效使用起十四天滑动续期。本包的假签发用例只覆盖跨路接线与失败净化，不构成这些 A 路行为的验收。

## S-MCP-AUTH-DIAG（2026-11-09）

短名提示说明它是本次接入 Codex 自己的名字，系统会追加 `_AG` 编号；目标提示说明必须填写系统中已存在的另一个 Agent 全名（例如 `opus50tc2k.zzc`），且接入后收发仅限该目标。`CallbackRequest.AttemptID` 只由 B 从已校验的外层事务注入。失败以安全 `CallbackFailure` 暴露固定原因码、attempt 前缀与目标类别；`CallbackFailureDetails` 不返回底层错误正文，避免授权码、令牌及主体标识进入日志或浏览器响应。
