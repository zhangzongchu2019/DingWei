# B 路 MCP／REST／OAuth 接口

基线 `144b215e32e718553d9fef2e79c83ece164f6514`；任务 S-R53-MCPGW-IMPL；包 `internal/mcpgw`。A/C/D 包不改，主装配由集成负责。2026-11-06T16:13:02Z 组长分配唯一 PG 迁移 `0107_mcpgw_oauth.sql`。

## 装配与启动降级

`mcpgw.New(Config)` 必需 PublicOrigin、Resource、Seats、Login、Clients、Store；Now 可注入。Resource 与 A/extwire 使用同一配置，值为 PublicOrigin＋`/mcp`；不从请求 Host／转发头推导。PublicOrigin 必须为 HTTPS 源。`Gateway.Register(*http.ServeMux)` 只登记新路径。

cmd/workpulse 的 `registerOptionalMCPGateway(ctx, mux, pool, access, resource, build)` 是集成入口：先读 `DW_MCPGW_OAUTH_KEY_FILE` 指向的仓外文件，检查是普通文件、32 字节原始随机密钥、权限不宽于 0600，再构造加密 Store。build 接到 Store 后构造 C Handler 和 Config。推荐使用 systemd `LoadCredential=mcpgw-oauth-key:<受控路径>`，环境变量仅填 credential 文件路径。密钥不落仓、不输出、不上传；测试密钥均用 crypto/rand 生成。临时读入数组在装配后清零。

密钥缺失、内容长度／权限错误、迁移缺失、依赖配置不完整时，只打印一行 `mcpgw OAuth disabled: ...`，不登记任何 B 路路径，不 panic、不影响已有 Hub 路由。MVP 不支持轮换，换钥使在途登录失效；已有 A 路令牌与吊销权威不依赖此加密密钥。

生产构造 `NewSQLStore(ctx, pool, access, resource, key)`，pool 为已迁移的身份 PG，通过 internal/repo 访问；构造时只查 schema，不执行 DDL。密钥必须在所有使用同一授权服务器的实例之间一致。使用独立的服务访问上下文，resource 必须和 Config.Resource 相同。build 中可把 `LoginStates{Store: store}` 传给 C 的 Config.States，C 的接口和实现不变；这是可选持久适配，不撤回组长㊹-补14接受单进程 MemoryStateStore 的决定。

A 消费签名由 Seats 接口钉住：Authenticate／ListOnline／Send／Wait／Ack。C 消费签名为 ServeConnectPage／Callback。C 的 TokenIssuer 继续用 `extwire.Issuer{Seats: A, Resource: sharedResource}`；飞书令牌不进本包。C 在回调签发 A 令牌，本包按 C 的既有 INTERFACE 暂存至 code 兑换，兑换前再次 Authenticate。此处没有新增签发权限。

## 路由与四工具

| 路径 | 方法 | 含义 |
|---|---|---|
| /mcp | POST | 2026-07-28 无状态 JSON-RPC：server/discover、tools/list、tools/call |
| /api/external/list_online | POST | 本 AG 在线名单，参数 {} |
| /api/external/send_message | POST | to、body、client_message_id；仅显式全名，由 A 现核 AG |
| /api/external/wait_for_messages | POST | wait_seconds 可省略，默认/硬上限 extshared.WaitMax（25秒） |
| /api/external/ack | POST | delivery_id、generation、phase=received |
| /.well-known/oauth-protected-resource/mcp | GET | resource、授权服务器、固定四scope |
| /.well-known/oauth-authorization-server | GET | issuer、authorize/token、PKCE S256、CIMD；无DCR/refresh |
| /oauth/authorize | GET | 外层 OAuth，验证客户端／redirect／resource／完整scope／PKCE |
| /feishu/connect | GET/POST | 验 B 事务和浏览器 cookie，再交 C 页面／Start |
| /mcp/oauth/feishu/callback | GET | 先核 B 关联，再按 C state 取得签发结果，发一次性 code |
| /oauth/token | POST form | authorization_code＋client_id＋redirect_uri＋resource＋code_verifier |

通讯路径只认 Authorization: Bearer，不接受 query/cookie 令牌。MCP 与 REST 共用四工具分发和 Authenticate；REST 不扩权限。在线响应只带 fullname/last_seen/external，不暴露底层 identity 对象。发送返回 stage=accepted/projected/unknown 的原义，不把 HTTP 返回当送达。收件返回稳定 delivery_id/generation，绝不替客户端调用 ACK。

MCP 每次要求 application/json，请求 Accept 包含 application/json 与 text/event-stream；本期选择单 JSON 响应。每次 params._meta 必有 protocolVersion 与对象 clientCapabilities；MCP-Protocol-Version/Mcp-Method 与正文一致，tools/call 要 Mcp-Name（支持标准 Base64 sentinel）。缺头/不符为 HTTP400/-32020；不支持版本 HTTP400/-32022 并列 supported；未知方法 HTTP404/-32601。正常结果 resultType=complete；不做 initialize、GET流、协议会话、订阅、input_required 或 WSS。客户端旧 session/续传头不创建状态。

四工具输入字段在 tools/list 发布 JSON Schema，未知字段拒绝。send_message 重试使用同一 client_message_id；Wait 不自动 ACK，取消由请求 context 传给 A，租约与重领继续由 A 管。ack 仅 received，不能冒充 processed/driver。

## OAuth、CIMD 与 Origin

授权客户端为公开客户端，使用 HTTPS Client ID Metadata Document。默认 MetadataClients 获取文档时无平台认证、无环境代理、禁止重定向，TLS 最低1.2，限制响应64KiB/8秒；解析DNS后拒绝私有/保留/回环/链路本地目的地，并把已验证IP直接用于连接，TLS仍核原域名。元数据 ID 必须与URL逐字相等，client_name/redirect_uris 必填；只支持 code/authorization_code 和 token_endpoint_auth_method=none。redirect 必须为 HTTPS 或精确登记的 IP loopback HTTP，禁止片段与userinfo。暂不缓存CIMD，不做DCR或机密客户端认证。

scope 按集合精确核固定四项，允许顺序不同，不接受子集/额外/重复项；resource 必须逐字匹配。PKCE 只支持 S256。五分钟事务绑定 client_id、redirect_uri、外层state、challenge、浏览器随机值哈希；C 表单只收到 AttemptID。C Start 返回的飞书state另与 B 事务绑定。回调先核浏览器与期限再允许 C 签发，并拒绝 C 返回的不同 AttemptID／audience／scope。客户端回跳只含 code、原state及iss，不含Bearer。token 端点原子 DELETE RETURNING 消费 code，核PKCE与客户端/redirect/resource，A现核通过才交付令牌；并发只有一个赢家，无 refresh_token。

所有本包路径统一 Origin 检查：缺 Origin 允许非浏览器客户端；存在时必须是合法 HTTPS 源。Origins 接口为空时仅信 PublicOrigin；集成 r52 时注入 TrustedOrigin。null、多值、非法与未信任来源一律403，不读Host放行。所有响应 no-store/nosniff/no-referrer；C页面继续使用自身CSP/CSRF。

## 存储与清理（供 extwire.CheckSchema 集成）

唯一表 `mcpgw_oauth`：resource text、kind text、key_hash text、sealed bytea、expires_ns bigint。主键(resource,kind,key_hash)；过期索引(resource,expires_ns)。kind 仅 attempt/login/code/cstate；state/code键只存SHA256；sealed为带随机nonce的AES-256-GCM，AAD绑定resource/kind/key。expires_ns为UTC Unix纳秒，不是存活秒数。

每次 Put 在同一事务先清本resource的过期行，再检查未清记录数（10000阈值）并插入。没有请求时不启动独立worker；下次写入清理，所有读取/兑换仍即时检查期限，晚清不延长授权。Take 用单条 DELETE…RETURNING 跨连接/进程原子消费，失败/过期不能兑换。此表是短期OAuth状态，不改变A的令牌14天、AG3天或投递/ACK权威。

错误表集中在 gateway.go 的 errorRules，按 errors.Is 映射；业务错误使用JSON-RPC保留区之外的正整数1001/1003/1008/1009/1029，MCP保留区只用于标准定义的头/版本错误；未识别错误统一 internal_error，不返回底层异常。认证失败401并给 protected-resource 元数据挑战；权限403、幂等冲突409、队列满429。OAuth失败只返固定错误名，不带上游正文或令牌。
