# MCP 受限动态客户端注册（S-MCP-DCR）设计审阅稿

状态：仅设计，等组长过目；本稿不授权实施、部署或开现网注册口。基点 `4a349ee59a1d8d7b8b9c562c462bded2f8b0f192`（r61 0.2.3.41，含 AUTH-FIRST）。设计截止2026-11-19T12:00Z；实现截止11-20T12:00Z，获设计通过后才开始。需求规范在 openspec/changes/mcp-dcr/specs/mcp-dynamic-client-registration/spec.md，DCR-001～008。

## 1. 问题与目标

组长2026-11-19T08:02:39Z派单及08:04:13Z补件：CodeBuddy IntelliJ/Linux报告POST /oauth/register为404，AS元数据没有registration_endpoint，DCR取不到client_id。实际redirect_uris仍未知。AUTH-FIRST解决未认证先被协议400拒绝的问题；本片补受限RFC7591登记，不能把两件混成一个缺陷。

目标是让使用公共客户端、authorization_code/code、S256和受限回调的DCR客户端走现有飞书授权。登记只是建立公开client metadata，**不创建Agent、不签MCP bearer、不授通信权**；用户仍须飞书认证、tenant/target等检查及同浏览器五分钟事务。典型DCR模拟流程成功不等于已测CodeBuddy/Cursor/Cline实物，也不保证只支持旧SSE或私有scheme的客户端可接。

## 2. 已读代码与复用边界

| 基点位置 | 现状/本片接法 |
|---|---|
| internal/mcpgw/oauth.go:54-59 | AS已广告none/code/S256/CIMD；就绪时增加registration_endpoint=配置PublicOrigin+/oauth/register，不从请求Host推导 |
| gateway.go:65-111 | Config依赖注入及路由；新增可选DynamicClients/DCRPolicy，独立于现有Seats、Login、Clients、Store |
| clients.go:21-27、59-105、121-146 | ClientResolver为外部HTTPS CIMD，含SSRF防护与URL型ID校验；不把不透明DCR ID硬塞入CIMD validateClient |
| oauth.go:84-139 | authorize已强制S256、固定scope/resource、内置ID特殊规则；新增准确DCR命名空间分支，CIMD/内置分支不改策略 |
| clients.go:149起 | 已有RFC8252回环端口例外；DCR使用更窄的回调登记规则，不放宽现有CIMD/固定Codex规则 |
| oauth.go:143-178、296-336、355-405 | 浏览器cookie哈希、5分钟attempt/一次性code、client/实际redirect/verifier/resource绑定继续复用 |
| store.go:19-116；PG0107_mcpgw_oauth.sql | 现有短期加密OAuth记录、总量10000；DCR必须独立表和配额，不能占用该池导致正常授权停摆 |
| cmd/workpulse/mcpgw.go:27-52、r53_extseat.go:129-154 | 同PG与持久OAuth密钥装配；新增DCR存储/策略和诊断，不绕过现有登录/issuer |
| extwire/issuer.go:29-49、extseat/service.go:74-165 | 飞书回调调用现有IssueToken创建外部Agent、绑定principal及组并签token；无需预先创建托管/自有Agent |
| gateway.go:276-303 | AUTH-FIRST已使未认证GET/非法体先401，认证后只收POST；本片保留，不再改为鉴权前400 |

无新依赖、无helper/renderer/Agent权限改造；不改VERSION。新增PG迁移须实施前由组长协调编号，设计不先占0112/0113或任何槽；SQLite应用库与history迁移不动。若随r62集成，保留GROUPS可选AG/普通组和NAME改动，本片不把目标或AG重新变成必选。

## 3. RFC7591受限接口（DCR-001）

新增POST /oauth/register；原生客户端不需先持有bearer。只在DCR开关开启且PG/策略就绪时在AS元数据广告该地址；CIMD广告继续为true，现有code/none/S256声明不变。关闭时无registration_endpoint，登记请求404；启用但依赖/迁移缺失时登记503 temporarily_unavailable、明确DCR未就绪诊断，不装成空map“成功”，不关闭已有CIMD/内置OAuth服务。新路沿既有Origin guard：原生无Origin可用，非法/多Origin拒绝；回调白名单不自动授CORS，首版不新增跨站浏览器CORS。

请求Content-Type application/json，UTF-8单对象、无尾随值、重复成员拒绝；最多16KiB，redirect_uris为1～4条，每条最多2048字节，client_name可省略并以中性标签显示，提供时最多128码点且无控制字符。登记名字是客户端自报，不是经飞书验证的主体或厂商名。未知普通metadata忽略且不抓取URL；client_uri/logo_uri/contacts不取件、不落原文日志。

| 字段 | 允许值 |
|---|---|
| token_endpoint_auth_method | 必须显式none。RFC7591缺省为client_secret_basic，本服务不支持，缺省也拒绝；不静默降级成none |
| grant_types | 省略按authorization_code；提供时必须唯一数组[authorization_code] |
| response_types | 省略按code；提供时必须唯一数组[code] |
| scope | 省略采用现有固定四scope；提供必须与现有集合相同，不增加scope或按注册metadata放宽授权 |
| redirect_uris | 按第4节，不允许空、通配或任意私有scheme |
| client_id/client_secret/secret过期值/registration_access_token | 输入拒绝；ID由服务器生成，绝不发client_secret或管理token |
| jwks/jwks_uri/private_key_jwt/software_statement | 首版不支持这些认证/背书模式，拒绝相应安全敏感metadata；不发网络取件 |

成功201 JSON，no-store/no-cache，返回随机256bit不透明公共ID（dw-dcr-前缀）、client_id_issued_at、实际接受的redirects/none/code字段；可带明确为本服务扩展的client_id_expires_at。无client_secret、client_secret_expires_at、registration_access_token或registration_client_uri；不实现RFC7592管理接口。ID是公开标识，不是凭据。

错误：metadata不符400 invalid_client_metadata，回调不符400 invalid_redirect_uri，超体413，媒体不符415；只回固定短句，不反射URI/请求片段。限流429＋Retry-After；资源容量/存储不可用503 temporarily_unavailable。DCR就绪时入口先取有界槽与登记全局额度，再按可信取源记来源额度，之后才作本路method/Origin/body校验；metadata失败也消耗额度，不为非法源新建无界桶。关闭态404不启动登记存储。此顺序只属新增登记路，不改既有/mcp与其它OAuth的Origin/鉴权顺序。

## 4. 回调边界与三个形式（DCR-002）

A. 回环：只接受http://127.0.0.1、http://[::1]、http://localhost，端口省略或1～65535。不是整个127/8，不接受十六/八进制IP、IPv4映射IPv6、localhost.、后缀伪装、userinfo、fragment、opaque URI、通配及编码主机。注册后authorize仅对相同scheme、精确主机、escaped path、raw query采用端口例外；localhost只作ASCII大小写归一。127/localhost/::1之间不互换，不允许错路径/编码路径或额外query。按RFC8252允许实际授权时临时端口；token兑换仍与本次实际redirect逐字相同。

B. HTTPS：默认允许列表为空。运营确认厂商真实回调后，仅配置**完整精确HTTPS回调URL**（主机+端口+path+query），不是仅放某个网站整个域；不自动包含子域或跳转器，不支持通配/HTTP非回环。限定普通DNS主机，拒绝IP地址、私网/localhost形式及userinfo/fragment；注册不访问callback URL，不做DNS“所有权验证”或自动追跳。是否为可信厂商专用回调由运营证据决定，配置不能来自客户端请求。注册及每次新authorize都核现行列表；已经建立的五分钟事务仍按原快照收尾，此窗口在紧急关闭时须计入回退方案。

C. 自定义scheme：首版默认拒绝（invalid_redirect_uri），不能只因客户端名为CodeBuddy而放开。若实测必须使用，另提单一scheme+authority/path受限策略、厂商所有权与系统handler校验、S256和同设备风险评估交组长裁定。Linux/IDE私有scheme可能被别的程序抢注，单凭scheme白名单不能证明接收App；本稿不将该形式偷换成已支持。HTTPS白名单或回环是先期可交付范围。

待补证：CodeBuddy实际redirect_uris尚未从原记录得到；上线前须从受控客户端捕获脱敏登记metadata确认命中A/B，若为C则另裁，不宣称仅加DCR就必定全客户端可用。

## 5. 持久登记、生命周期与清理（DCR-004/006）

新增独立PG动态client表和受限登记计量表，resource分区。客户端行含(resource,client_id)唯一键、accepted metadata加密sealed、source_hash、created/expires；用现有持久OAuth32字节密钥构造AES-GCM，AAD绑定resource/client ID，不把DCR放进mcpgw_oauth的10000条短事务配额。source_hash为持久密钥域分离HMAC，不存原始IP；备份与该密钥关联，不新增下载凭据。

建议绝对TTL24小时（可配1小时～7天），GET/authorize/工具调用不续期，计量只算未过期行。过期ID新authorize为invalid_client、且不回退CIMD；客户端需重新注册。先前已建立的attempt/code按原五分钟快照完成，不因client GC半途失效；现有已签bearer照原seat/token现核，不因登记到期被额外吊销或延寿。client TTL不是Agent/AG/消息/14天token的期限。 client_id_expires_at不是RFC7591通用到期契约，客户端可能忽略；不能假定CodeBuddy会自动重注册。短TTL与客户端缓存的兼容性须受控实测，未支持自动重注册时接入文档要说明重新连接/登记步骤，不能静默延长服务端TTL。

建议每resource最多4096个存活client、每来源128个。受理时在同一PG受控事务中有界清理≤256条过期client并计数/插入，索引覆盖resource+expiry/source；未物理清完的过期行不计配额，但不删未过期行。每分钟后台有界清理client及过期source bucket，并受现有后台生命周期管理；不能每请求全表删或留下终身累计配额。

登记限流与client分配都用resource级PG事务锁/原子计量，多Hub与重启不重置。 持久TTL与桶计量使用同一PG时钟，不由客户端时间或各Hub漂移时钟决定；测试用隔离存库的受控时间边界，不改现网钟。存储失败不回假201，容量满明确503；DCR容量不影响CIMD、既有bearer与五分钟OAuth池。配额只能限制注册洪泛消耗，不能声称抵御任意分布式DDoS；独立小池满时只拒新DCR，保留恢复/清理与现有OAuth通道。

## 6. 来源、限流和有界处理（DCR-005，建议值待过目）

- 每来源令牌桶5次/分钟、burst5；每resource全局60次/分钟、burst20；另有上节存活上限。来源=IPv4单地址或IPv6 /64，经域分离HMAC后作PG键，不用client_name/redirect/用户可伪造header作身份。
- 默认只信RemoteAddr。仅当直连peer属于配置的DW_MCP_DCR_TRUSTED_PROXIES CIDR时读单个有界X-Forwarded-For（≤1024字节/8跳），从右向左跳过可信代理，取第一个不可信地址；不信客户端填的最左项。非可信peer的Forwarded/X-Real-IP/XFF均忽略；非法链拒绝，缺链退回peer且共享额度。代理配置默认空，部署GO前由运维核真实代理来源与追加行为，不猜现网配置。
- 令牌桶在PG持久化，回拨不补负时间令牌，补充最多到burst；source bucket最多4096个/resource，2小时无请求可清理。新来源超槽只限DCR，不无限创建内存map或PG行。
- 首版处理并发槽32，入场不排无界队列，超限503；body最多16KiB且读取截止3秒，DB事务预算2秒，取消必须结束等待/回滚，不派游离后台goroutine。实现须核ResponseWriter包装的deadline支持；不能设置时明确不可用，不默默失去界限。慢体/占锁/并发边界均需真实HTTP/PG测试。
- 首版仅暴露开启、TTL、HTTPS精确列表、可信代理CIDR配置；上述额度为定值候选，不开无限配置。日志/obs用固定枚举结果（accepted/rejected/rate_limited/capacity/unavailable），不记录raw IP、完整client metadata、callback、code/state/token/secret，不用client_id作高基数标签。

NAT可能使多用户共享来源额度，这是上述值的取舍；通过组长审阅后再定参数，不能为兼容而无条件相信XFF。没有owner身份的公开DCR无法只按飞书用户计费，否则形成先注册/先授权互相依赖。

## 7. 三种client并存与权限（DCR-003/006）

解析顺序：两个现有精确Codex身份保持原策略；dw-dcr-命名空间只查本resource持久登记+期限/受限政策，缺失/过期/未就绪均invalid_client，绝不回退网络CIMD；其它现有URL型ID仍走原MetadataClients与validateClient，不放宽SSRF或默认Codex规则。随机ID不能由注册请求自行指定，不与URL ID冲突。

PKCE是每次授权事务的S256，不是登记一次就豁免。保留固定scope/resource、浏览器Secure/HttpOnly/SameSite cookie、5分钟TTL及一次性Take、code/client ID/实际redirect/verifier绑定；token端仍先现核seat的吊销/到期。登记不得跳飞书、tenant、target/编组校验，不接受Console cookie换MCP token，不能创造比CIMD更高权限。DCR不改现有授权scope完整集合规则。

首次飞书OAuth完成时，现有extseat.IssueToken会按可信principal创建外部Agent/配对并签本地token，不要求用户先有托管或自有Agent；飞书Console登录仅能证明Console会话，不等于这个OAuth流程完成。目标/名称仍按当前基点表单与服务端校验，后续r62普通组/可选配对按其冻结实现集成，本片不另定义开户语义。

## 8. GET/SSE与错误排障（DCR-007）

保留AUTH-FIRST已完成的行为：未认证GET /mcp（包括Accept:text/event-stream）返回401＋原WWW；未认证POST非法JSON也先401，不为诊断在鉴权前读body或改400。认证后GET仍405、Allow:POST；不新增旧HTTP+SSE的endpoint事件/第二POST地址，也不创建长连SSE。Streamable HTTP本身允许认证后的GET不提供流而405，不能把这个405误报成DCR仍坏。接入文档只声明单端点Streamable HTTP POST/JSON；AS OAuth元数据不捏造标准“transport”字段。

用户排障建议先把请求写成文件并用JSON校验器验证，再用curl --data-binary发送；不要求提供bearer/cookie/code/verifier。不将用户报告的zsh -d失败推广为curl -d不能发送JSON。需要区分shell引用、文件内容、Content-Type、双Accept及协商后的版本头。当前基点未认证错误已由401掩护；已认证非法JSON可在保留400/-32700的前提下采用固定无反射诊断“invalid JSON request”，详细操作放文档，不把原解析器错误/请求片段回给用户。本片不借DCR顺手改全部codec错误消息。

FAQ：
1. https://codebuddy.ai之类网站首页不是JSON CIMD文档；根路径/缺字段本来就不合法，不能据此断言CIMD机制坏或必须人工发token。DCR取服务器发出的不透明client_id，CIMD则要真实HTTPS metadata文档。
2. /.well-known/oauth-authorization-server/mcp不是本资源发现所指AS地址。客户端应从401 resource_metadata→authorization_servers→AS metadata找registration_endpoint，不轮猜/register路径。
3. Console账号无Agent不意味着要管理员手工签bearer；完整飞书OAuth可自动开户。Console cookie不能用于/mcp，也不新增token明文下载入口。

## 9. 验证矩阵与删保护（DCR-008）

| 格 | 必须实际验证 | 删保护/负对照 |
|---|---|---|
| V01 真实HTTP全链 | 注册201→authorize→同浏览器connect表单→飞书OAuth回调桩→token→带bearer initialize协商；无预建Agent，使用隔离真实PG及真实登录/issuer/seat服务 | 缺S256/method=plain或错verifier：移除相应强制须该拒绝格业务红 |
| V02 redirect | 三回环动态端口、错端口范围/host/path/query；HTTPS空名单拒绝及精确名单成功；自定义scheme/通配拒绝；token实际URI逐字匹配 | 放行非回环非名单HTTPS或自定义scheme，拒绝格必红；不得用正向HTTPS名单格误杀正确支持 |
| V03 公共注册 | none/code成功，无任何secret/管理token字段；basic、secret_post、refresh/implicit/敏感字段/缺none拒绝 | 放行带secret或机密auth method注册必红 |
| V04 滥用与持久性 | source/global限流、存活上限、并发争额、重启/第二store共享额度；伪造XFF无用，真实代理链右侧取源；桶数量/并发/慢体均有界 | 去限流必红；改信任伪造XFF、删source/global计量各自拒绝格红 |
| V05 到期/容量 | TTL前/恰到/后，过期不计quota；有界GC释放空间、不清活跃；在途五分钟快照不受GC破坏，已签token仍现核；旧池正常 | 改回终身计数、去“仅清过期”、重启丢登记或超期仍resolve，各格红 |
| V06 并存/回退 | 两个Codex、外部CIMD、DCR并存；不透明ID未找到不网络兜底；关闭只停新登记/新DCR authorize，在途code五分钟可收尾，CIMD不受DCR容量影响 | 去分型/混用CIMD校验、关闭时仍新登记、DCR撑满挤掉OAuth池，格红 |
| V07 未认证/旧SSE | 无bearer GET与三版本initialize/tools/list/ping仍401原WWW；认证GET405；错误origin/过期/吊销均拒绝；AUTH-FIRST与旧codec全绿 | 版本校验挪鉴权前、Console cookie代bearer或放宽scope/tenant/浏览器绑定，各相应拒绝红 |

实现门：冻结SOURCE_COMMIT后的canonical；internal/mcpgw、internal/extseat、internal/m8整包-count=3 PG strict（SKIP逐名、已知SQLite interrupted按组长现行Z-3口径）；相关cmd/PG store定向与focused race；OpenSpec --all --strict。全链是真实本地HTTP+飞书服务桩，不是真实飞书登录/真实CodeBuddy桌面实测；记录两者边界，必要时另做受控客户端验收。qwen独立复核后组长终审，未获GO不发布。

## 10. 启停与设计待确认

建议DW_MCP_DCR_ENABLED缺省false；开启需独立PG表/计量可用、策略校验通过。关掉后不广告registration_endpoint，登记404、新DCR authorize拒绝，但旧五分钟attempt/code快照可收尾，已签bearer仍按原权限；CIMD与内置客户端不依赖DCR。回退旧二进制不删表、不回改迁移、不重开已消费code，5分钟状态沿兼容快照，不把public ID当secret。正式旧二进制+加法PG迁移及往返演练是实现门，不以设计推断已可回滚。

待组长过目定：A-DCR-01上述TTL/额度与可信代理取源；A-DCR-02 HTTPS精确URL初始空、自定义scheme首版拒绝；A-DCR-03保留未认证GET/非法体401和认证GET405、不做旧SSE。这些是本席技术建议，不冒称业主逐项定值。实际CodeBuddy callback形式未知要补证；PG迁移号在实现前协调，不先占槽。设计通过前只有docs/openspec，无产品代码。

## 11. 标准与输入来源

下列官方资料于2026-11-19只读核对，只取本片需要的子集；更严格的白名单、额度、期限是本平台建议，不说成标准强制：
- RFC7591 §2/3：https://www.rfc-editor.org/rfc/rfc7591.html （登记metadata/201/错误；none不带secret，缺省认证法是basic）
- RFC8252 §7.3：https://www.rfc-editor.org/rfc/rfc8252.html （回环IP授权时端口例外）
- RFC7636：https://www.rfc-editor.org/rfc/rfc7636.html （授权码PKCE）
- RFC9700 §2.1：https://www.rfc-editor.org/rfc/rfc9700.html （URI精确匹配及PKCE安全基线）
- MCP2025-11-25 Authorization：https://modelcontextprotocol.io/specification/2025-11-25/basic/authorization （CIMD、预登记、DCR并存和发现）
- MCP2025-06-18 Transports：https://modelcontextprotocol.io/specification/2025-06-18/basic/transports （单端点POST，GET可不提供SSE，旧HTTP+SSE不是同一机制）

组长08:04:13Z补件本席收件dw-dlv_84a2f9a5bb9df7eb6ad435f4ce82d6ea；用户排查原件7573字节、SHA256=37d581902083b91a3a6e64e920636af35e6aa20cd70d83fce9c90a1da7f63d9a，原始件只留本地私有证据，不整篇复制PII/命令到本仓。其“CIMD不可用/必须手发token”和“-d必非法”是用户推断，已在第8节纠正，不当实测事实外推。
