# 受限 MCP DCR 接入与运行说明

本件仅说明实现合同；启用、重启、发布、回退均须另获组长/业主GO。不得从本文推导任何现网操作授权。设计见 docs/design/MCP-DCR.md，源码冻结与实跑证据以 docs/reviews/mcp-dcr-impl/REPORT.md 的实际锚为准。

## 配置与发现

| 配置 | 合同 |
|---|---|
| DW_MCP_DCR_ENABLED | 缺省false；显式true才接受新登记并在AS元数据广告registration_endpoint |
| DW_MCP_DCR_BOUND_TTL | 缺省720h，允许168h～2160h；只在成功token兑换后用于绑定/续期；未绑定固定24h |
| DW_MCP_DCR_HTTPS_REDIRECTS_JSON | 缺省空；完整精确HTTPS URL的JSON数组，例如`["https://vendor.example/oauth/callback"]`；不自动信任域名/子域/任意path。JSON避免把URI内合法逗号误当分隔符 |
| DW_MCP_DCR_TRUSTED_PROXIES | 缺省空；逗号分隔的可信代理CIDR。只有直连peer在名单中才解析单个有界XFF链，从右向左找第一个不可信源；不要照抄未核过的代理地址 |
| DW_MCPGW_OAUTH_KEY_FILE | 沿用既有受控32字节密钥文件，权限/备份纪律不变；不把值放进env包、日志或下载件 |

PG0115是新增三表与索引，不改已有迁移；集成前再核0112～0115槽。独立登记池不占mcpgw_oauth的短期事务配额。配置/表缺失只使DCR不可用，不因DCR单独失败关闭已有CIMD/内置OAuth；原有核心OAuth密钥缺失仍按旧合同关闭核心OAuth。

就绪为启动时配置/表校验通过；运行期数据库故障登记503，AS元数据不逐请求探活数据库。关闭时登记404且无registration_endpoint，启用但未就绪登记503。首次启用前由运维只读核真实代理链、可信代理CIDR和实际客户端callback；不要为了接入直接开通配或私有scheme。

## 客户端流程和拒绝

发现次序是未认证`/mcp`的401 challenge→resource metadata→AS metadata→POST /oauth/register。公共登记必须显式`token_endpoint_auth_method=none`；仅authorization_code/code，授权每次S256。无client_secret、管理token、refresh授予。登记不创建Agent、不授通信权限；仍走现有飞书登录、同浏览器绑定、五分钟事务与权限现核。

回调允许HTTP精确127.0.0.1、::1、localhost，授权时仅端口可变；token仍绑定本次实际URI。HTTPS仅完整精确名单。私有scheme、通配、非回环HTTP默认拒绝。实际CodeBuddy IDE尚未实测；WorkBuddy云端Connector文档不等于IntelliJ/Linux插件行为，不能据它的通配/refresh要求放宽本期code-only合同。

登记使用短连接，提前错误不等待排空未读请求体；读体3s/16KiB、数据库事务2s、处理槽32。错误固定且无URI反射：400 invalid_client_metadata/invalid_redirect_uri；413超体，415媒体，405非POST；429带Retry-After；容量/存储503 temporarily_unavailable。metadata失败也消耗登记额度。`/mcp`未认证GET/非法体仍先401，认证GET405/Allow POST；没有旧HTTP+SSE端点。

## 保留期、主体及空间

未绑定绝对24h，查询/失败兑换/工具调用不续；成功token兑换绑定默认30天并续期。每稳定可信飞书主体四元组最多16个活绑定，超限淘汰最久未兑换；同一公开client可被多主体使用，淘汰只移除该主体关联。client_id不进Agent身份/去重键，现有IssueToken重复建seat/AG边界未修。

最多4096个存活client/resource、128个/source。登记来源5/min burst5，全局60/min burst20；来源桶最多4096，闲置2h可清；IPv6按/64。来源与主体均HMAC，metadata加密。绑定行总量按活主体数增长（每主体最多16活关联），不能冒称全库只有4096行。过期记录不计存活配额，GC每批最多256绑定、256无子项client、256来源桶，保留五分钟快照余量；失败/暂停清理期间物理存量可大于存活配额。

建议只读观测存活/过期client数、binding总数/过期数、bucket数、过期积压是否持续（本版无单列GC失败计数）与登记固定错误类别；不输出完整client_id、主体/来源哈希列表、URI、令牌或原始metadata。拒收callback只记loopback_bad/https_unlisted/custom_scheme/malformed，私有scheme另记过滤后最多32字节名。不要用注册client名字当可信厂商身份。

## 关闭与回退

由授权运维停、改配置、起，先关闭新DCR登记和新DCR授权；原五分钟attempt/code按既有快照收尾，已签bearer仍按原seat权限现核。旧二进制不提供DCR、不续DCR绑定；不能宣称回退后新登记仍可用。原加法表/迁移账本保留，不删表、不恢复旧库覆盖新投递、不重开已消费code。前滚重新校验配置/表及既有登记期限。

作者隔离验证将记录旧4a349ee5可执行文件的schema兼容、旧Hub兑换已有code与拒绝重放、前滚账本不变；它不等于生产回退GO或在途消息已排空。现网回退继续遵守发布runbook的安全闸，本文不替代。
