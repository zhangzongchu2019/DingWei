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

## 身份与错误

- `Principal.Provider` 固定为 `feishu`，`Issuer` 为 App ID，`Subject` 为 `open_id`，`TenantKey` 为可信 `user_info.tenant_key`，`UnionID` 仅作别名。唯一键继续由 `Principal.Key` 给出，显示名不参与合并。
- tenant 缺失或与独立配置不精确相等，返回 `extshared.ErrTenantRejected`；缺 `open_id` 返回 `ErrIdentityIncomplete`。
- 显示名优先使用去首尾空白后的 `en_name`。缺省时对 `name` 使用 go-pinyin `Normal` 风格，无分隔连接、转小写并逐字节只留 `a-z`；结果为空返回 `ErrDisplayName`，不签令牌。
- state 不存在、已消费、应用或回调地址不符返回 `ErrInvalidState`；到期返回 `ErrStateExpired`。飞书失败返回 `ErrUpstream`，A 签发失败返回 `ErrTokenIssue`，均不回传上游错误正文。

A 路必须自行保证本地令牌只存哈希、吊销立即生效、过期不复活及从最近有效使用起十四天滑动续期。本包的假签发用例只覆盖跨路接线与失败净化，不构成这些 A 路行为的验收。
