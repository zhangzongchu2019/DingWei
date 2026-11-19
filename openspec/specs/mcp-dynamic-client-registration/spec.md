# mcp-dynamic-client-registration Specification

## Purpose
为 CodeBuddy、Cursor、Cline 等需要动态客户端注册的 MCP 客户端提供受限的公共注册：默认关、精确回调名单、按主体绑定、两段有效期与每主体上限。r62（0.2.3.42）随迁移 0115 上线，开关默认关，需业主现场验收后开启。

## Requirements

### Requirement: DCR-001 发现与公共登记
网关 MUST 仅在受限DCR就绪时广告配置域的registration_endpoint，保留CIMD支持；POST登记只接受none、authorization_code/code的公共metadata，MUST NOT 发client_secret或注册管理token。成功201 JSON；非法metadata/redirect分别400 invalid_client_metadata/invalid_redirect_uri，超体413、媒体415、非POST405。输入有界、重复JSON成员拒绝，不取件metadata URL。

#### Scenario: DCR-S001 标准公共登记
- **WHEN** 原生客户端提交合法回调、显式none及code元数据
- **THEN** 返回服务器随机公共client_id及接受的metadata，201/no-store，不含任何secret或管理token字段

#### Scenario: DCR-S002 发现与关闭
- **WHEN** DCR就绪、手动关闭或启用但依赖缺失
- **THEN** 仅就绪状态广告registration_endpoint；关闭登记404，依赖不可用503 temporarily_unavailable，CIMD及固定客户端不因DCR缺依赖关闭

#### Scenario: DCR-S003 机密及越界metadata
- **WHEN** 登记缺显式none、提供basic/secret、refresh/implicit或安全敏感认证字段，或JSON重复/超体/错媒体
- **THEN** 按固定错误拒绝且不建client、不发secret、不抓URI，不能用缺省basic静默降为none

### Requirement: DCR-002 受限回调与逐字绑定
DCR MUST 只接受精确http回环127.0.0.1/::1/localhost或运营完整URL白名单HTTPS；名单默认空，自定义scheme/通配默认拒绝。回环授权仅端口可变，其余scheme/host/path/query不变；token MUST 逐字匹配本次实际redirect。不得扩大CIMD与固定Codex既有回调边界。

#### Scenario: DCR-S004 回环临时端口
- **WHEN** 合法回环登记后在授权请求指定1～65535临时端口
- **THEN** 精确其它组件才接受，token换票仍绑定该次实际URI；错host/path/query/scheme或无效端口拒绝

#### Scenario: DCR-S005 HTTPS与私有scheme
- **WHEN** 提交非回环HTTP、名单外HTTPS、通配、伪localhost、userinfo/fragment或私有scheme
- **THEN** 登记invalid_redirect_uri且无外部取件；名单中精确HTTPS可接受，但不能借同域其它path/子域/跳转器放行

### Requirement: DCR-003 授权边界与首次开户
DCR MUST 复用每次PKCE S256、固定scope/resource、同浏览器五分钟attempt与一次性code、client/实际redirect/verifier绑定及飞书tenant/目标校验。注册不授权；首次有效OAuth由现有issuer创建外部Agent，无需Console预建Agent。MUST NOT 用Console cookie代MCP bearer或给予高于CIMD的权限。

#### Scenario: DCR-S006 无预建Agent的完整授权
- **WHEN** 合法公共DCR客户端完成飞书OAuth且principal符合既有tenant/target规则
- **THEN** 既有issuer建立外部Agent并签相同权限token，带bearer初始化可用；仅登记或仅Console登录不可调用MCP业务

#### Scenario: DCR-S007 PKCE与主体拒绝
- **WHEN** 缺S256/错误verifier、不同browser、五分钟到期、错client/实际redirect/resource、错tenant或已撤销主体
- **THEN** 拒绝授权/兑换/业务，不扩大scope；去相应保护须对应拒绝断言红

### Requirement: DCR-004 独立持久登记与有限保留
DCR MUST 使用独立PGclient表与resource级唯一键，不占mcpgw_oauth短期配额；metadata加密，未绑定绝对24h不续；首次成功token兑换绑定默认30d，之后成功兑换续期（配置7～90d），失败/查阅不续。每稳定外部主体活绑定最多16，超限淘汰最久未兑换；client_id不进Agent身份或开户去重键。存活配额只计未到期行，GC仅有界清过期行；过期ID拒新authorize且不回退CIMD，旧五分钟快照可完成，已签token仍按原权限现核，不延寿或额外吊销。

#### Scenario: DCR-S008 重启与GC
- **WHEN** 重启/第二store解析登记，或TTL前/恰到/后及过期积压发生
- **THEN** 未绑定恰24h拒新授权；已绑定成功兑换续期、失败不续；主体第17绑定淘汰最旧者且不误删他人绑定；清理释放容量，不删除活client，各期限/续期/淘汰保护删去须红

#### Scenario: DCR-S009 在途快照与旧池
- **WHEN** client登记过期/清理时已有合法五分钟attempt/code，或DCR池满
- **THEN** 快照仍按原client/URI/PKCE一次收尾；既有OAuth短期池和已签bearer不因DCR配额被占满或提权

### Requirement: DCR-005 防洪泛与可信取源
DCR MUST 有来源/全局持久令牌桶、存活client/来源桶数量上限和有界处理并发；metadata失败请求不得逃限流。来源只从RemoteAddr或显式可信代理链右侧判定，MUST NOT 无条件信任XFF/Forwarded。超限429及Retry-After，容量/数据库不可用503；回调拒收记固定四枚举，私有scheme只记过滤后≤32字节名称；不存rawIP/完整URI/凭据或高基数client标签。

#### Scenario: DCR-S010 计量原子且跨进程
- **WHEN** 同源/全局额度耗尽、并发争额、重启或第二store请求
- **THEN** 不超发、不因重启清额度；去限流/存活配额分别使对应拒绝格红，过期释放不误删活跃

#### Scenario: DCR-S011 代理伪造与慢体
- **WHEN** 非可信peer伪造XFF，或可信链错误/超长、来源桶满、慢体/DB锁等待
- **THEN** 不按伪源放行，不建无界map/队列/游离读协程；依固定额度/截止明确拒绝，真实代理追加链按第一个不可信源计量

### Requirement: DCR-006 客户端并存与安全关闭
网关 MUST 分型固定客户端、持久DCR命名空间及原CIMD；缺失/到期DCR不走网络兜底，原CIMD/固定Codex策略不放宽。关闭DCR停止新登记与新DCR授权，保留五分钟快照收尾及原bearer权限；加法数据不删，不新增secret或管理接口，实际旧件回退须实现演练。

#### Scenario: DCR-S012 分型不串路
- **WHEN** 三种client同时授权，或未知/到期不透明ID与近似内置ID到达
- **THEN** 只走准确分支；未知DCR拒绝且不访问CIMD，不能用DCR放宽固定Codex或外部CIMD的规则

#### Scenario: DCR-S013 关闭与回退
- **WHEN** DCR关闭或换旧件，且有已登记client与五分钟在途码
- **THEN** 新登记/新DCR授权停止、数据保留，现有码按兼容快照一次收尾，已有CIMD及bearer权限不变；不删迁移或重开已消费code

### Requirement: DCR-007 传输与固定诊断
网关 MUST 保留AUTH-FIRST未认证/mcp各方法及非法体先401+原WWW；认证GET仍405/Allow:POST，不实现旧HTTP+SSE。接入文档只声明Streamable HTTP POST，解释CIMD文档/Console账号/OAuth开户区别与安全JSON排障；MUST NOT 返回原请求片段或因排障改鉴权优先级。

#### Scenario: DCR-S014 GET与非法体
- **WHEN** 无bearer GET/非法JSON或三版本initialize/tools/list/ping到达，或认证后GET/非法JSON到达
- **THEN** 未认证先401且不读body；认证GET405，认证非法JSON保留固定400/-32700诊断，不能绕现有Origin/版本/权限保护

#### Scenario: DCR-S015 排障不误导
- **WHEN** 用户以网站主页作client_id、仅Console cookie访问，或报告shell发送JSON错误
- **THEN** 文档说明真实CIMD/DCR发现与首次OAuth自动开户，建议验证文件再data-binary，不声称curl -d普遍无效或必须管理员发token

### Requirement: DCR-008 实际HTTP证据与删保护
实现交件 MUST 有真实本地HTTP注册→authorize→connect→飞书回调桩→token→bearer initialize链，明确桩/真实客户端边界；PKCE、非回环放行、secret注册、限流四类删保护均须编译通过后业务断言红并还原绿。MUST 交固定源canonical、mcpgw/extseat/m8三遍PG strict、逐名SKIP及strict，不部署、自验收或用模拟冒称实际CodeBuddy。

#### Scenario: DCR-S016 可复核实现交件
- **WHEN** 实现交冻结源给qwen和组长
- **THEN** 附命令/UTC/rc/原日志SHA、真实PG与HTTP证据、各刀红绿；未测实际CodeBuddy桌面/私有scheme如实标缺，不因设计过审提前宣称产品完成
