# 基线与上下文

## 依据与读取边界

- 本单父基线：review/togpt60lunazzc/r52-agent-gateway-proposal-20261105，f95fa0b3a4a6e693d21c8de4b052e3d3a7424293；新分支 coder56solzzc/r53-agent-gateway-proposal-rev。
- 代码核对基线：opus50tc2k/r51-integration，f7f31fca1d1f1468285138578c3f6fafacfb5537，tree 150f51fea5e2de620912d1d66d35780935782865。以下代码行号均指该 commit，用 git show／git grep 只读取得，不使用本地旧工作树行号。
- 需求原文：review/opus50tc2k/rulings 的 docs/rulings/S-TEAM-MIGRATE-HOSTED-20261017.md。2577d91a 的 ㊹、补1、补2；4accf167 的补3；5ed6d2ae 的补4；51133ad3 的补5（提交前主动读收件库发现追加后同步）；2a18e3f3 的补6；均已读原文。时间线以这些裁定为本变更依据，不从作者名推断需求。仓库没有找到独立覆盖本能力的平台能力时间线，未据此宣称额外现网事实。
- 规则：openspec/config.yaml，spec-driven、中文正文/英文结构/SHALL或MUST、保留 GW-001～010 十个固定需求编号。本能力尚无独立 openspec/specs/agent-http-mcp-gateway 基线；原提案的 context 与下面 r51 观察是本次最小基线，不增改目录外文件。
- 本单只改提案目录 Markdown；不实现、迁移、部署或探测真实飞书，不读取/提交凭据。后续实现和独立验收未完成。

## 八点裁定及第9点协议追加的已核代码影响

下表“拟新增”均是未来设计，不是 r51 已实现；未验证的接口/迁移/运行事实明确标未核实。

| 改造项 | r51 已核坐标与事实 | 拟新增模块/数据表影响与边界 |
|---|---|---|
| 网页飞书扫码、本团队 tenant_key | internal/feishu/feishu.go:454、463、468：入站 reply 的 app/tenant 比对；internal/identity/onboarding.go:624、671：既有飞书注册/登录码链路 | 新扫码回调与授权服务，服务端取可信用户信息并核配置本租户；原 reply 比较不等于网页登录。扫码 SDK、所需飞书权限与 token 响应字段未核实。np_identity 关联稳定主体，不能复用人类来源豁免授权外部 Agent |
| open_id／union_id 主体与显示名 | internal/identity/pgschema.sql:20：np_identity 的 provider/issuer/subject 联合主键；internal/identity/service.go:289：名称.属主简称 | 拟新增可信 tenant/app 范围的外部主体映射、英文/中文拼音显示归属；外部 owner 不能用拼音显示名当唯一键。是否可完全复用 np_identity/np_user 现有约束未核实；拼音库未核实 |
| 全局 AG 序号与临时类型 | internal/identity/group.go:20：Group 仅有 Code/ShortName/Creator/Leader/时间，无临时类型；internal/identity/pgschema.sql:89：np_group，无序号/expiry | 拟新增持久 sequence、temporary 类型、ag_sequence 唯一值、state/generation/last_used_at/expires_at；事务/回滚留空号而不复用；普通组 AGn 既有/并发冲突须保留旧数据并由系统跳号，不改普通组命名规则 |
| 自动建双成员组 | internal/identity/group.go:139、158：建/入组；internal/identity/pgstore.go:927：CreateGroup；pgschema.sql:101：np_group_member 主键(code,agent_id) | 在可信身份后同事务创建 external 席位、临时组、两成员与令牌；普通组建组权限不全局放宽。np_agent.group_code 为入网选择，成员权威仍为 np_group_member，不能覆盖目标既有入网组 |
| external 席位与组合名 | internal/identity/model.go:131：origin 只有 selfhost/hosted；:204：ValidAgentName 只字母数字2～32；pgschema.sql:46、52、65：np_agent origin CHECK 与属主/名称唯一索引 | 新 external 类型、受控 [a-z0-9]{1,8}_AG序号 校验与存储；更新相应 CHECK/投影；全局 AG 唯一保证不需用户查重，DB唯一与幂等仍保留。不把普通 helper 名称校验放宽 |
| 全名解析与 AMR | internal/identity/service.go:418：ResolveFullName 调普通 ValidAgentName；internal/m8/newplat_session_name.go:22：可见全名解析；internal/identity/group.go:296、336：候选可能用同主短名 | 新 external 全名适配只允许显式全名，稳定 owner 映射避免显示名碰撞；内部目标仍用 np_agent.id；不继承短名模糊/广播候选。实际 AMR 外部类型适配未实现、未核实互操作 |
| 编组授权与 roster | internal/m8/newplat_grouping.go:18、24、35：现有豁免及未识别 Agent 分支；internal/identity/group.go:353：共享编组且active才可达 | external 必须映射真实 Agent 后校验本 AG 的类型、状态、expiry和代际；不得落到“非Agent”放行或飞书真人豁免。只返回两成员，不递归可见目标其他组 |
| 14 天令牌与吊销 | internal/identity/pgschema.sql:36：np_session(token_hash,created_at,expires_at,revoked_at)；internal/identity/onboarding.go:215、229：创建与HTTP续期 | 拟独立 OAuth token / scope / external subject / AG / revocation generation 持久记录；不把 Console cookie/token 直接充 MCP 凭据；表名/迁移编号与授权服务器部署未冻结 |
| 90 秒在线租约 | internal/m8/agent_ws.go:191：AgentOnline 基于现有连接状态 | 新 external last_seen_at/lease_until 与HTTP成功请求触达；np_agent 投影/roster 在类型分支读取租约，不能伪造常驻 WebSocket；未核实现有 runtime 可直接容纳此类型 |
| 续期复用点 | internal/identity/onboarding.go:229、238、251、261：低阈值续期/一分钟触达；internal/identity/pgstore.go:487：TouchSession 检查未吊销/未过期及30天cap | 复用时钟注入/哈希/持久CAS/限频模式；不复制阈值、30天绝对上限和仅内存触达保证。新增 durable 触达/expiry投影（或先逐请求CAS），见design节流解释 |
| 3 天组续期与清理 | internal/identity/pgschema.sql:96：组仅created_at/updated_at；internal/identity/pgstore.go:1089：现有DeleteGroup | 新 last_used_at/expires_at、墓碑和 generation；分钟级分批扫/启动补扫/请求时到期检查；普通组不受影响。仅消息/首次有效ACK续组已定（㊹-补6） |
| 普通消息与24小时队列 | internal/store/migrations/0027_session_delivery.sql:1：SQLite session_delivery；internal/store/sqlite.go:2076、2145、2282：入队/过期/领取 | HTTP adapter复用投递，补稳定外部席位与AG代际关联、24小时绝对消息TTL；不能把旧表当成已有gateway token/AG元数据。跨PG/SQLite使用outbox对账而非分布式原子 |
| ACK与在途处理 | internal/m8/delivery_ack_v2.go:22、24：protocol2且driver；internal/store/session_delivery_ack.go：v2持久确认；internal/store/sqlite.go:2449、2464：取消待投递/确认取消 | 新HTTP处理模式或共享底层校验；解散栅栏、排队失败、已领取结算与通知outbox；不能取消已发生副作用或把unknown伪装完成。具体旧取消函数到新终态映射未核实 |
| Console相关AG侧栏 | internal/m8/console_auth.go:401：Agents侧栏与#a；:628、655：Agent导航；:720：refreshGroups；internal/identity/group.go:194、206：ListVisibleGroups也给创建者可见 | 在Agents下独立相关AG栏目，后端严格“名下Agent是成员”过滤，不直接复用“创建者或成员”并集，不给个人侧栏平台管理员全量豁免；表为np_group/np_group_member/np_agent |
| 属性页与主动解散 | internal/m8/console_auth.go:1658、1663、1665：renderGroups/属性/打开工作区；internal/m8/newplat_groups.go:30、94：视图与接口；internal/identity/group.go:185：旧DeleteGroup | 属性增加创建者归属名/创建/最后使用/expiry、相关owner或平台管理员解散、二次确认/现核/审计。旧建组人权限不可代替新授权；双方通知和在途规则共用自动解散路径 |
| 管理权限与审计 | internal/m8/obs_hooks.go:31：RequireConsolePlatformAdmin（仅active platform_admin，manager不算）；internal/identity/group.go:189：group.delete审计；internal/identity/pgstore.go:532：AppendAudit | 复用平台管理员判断与既有审计基础，但新增生命周期/outbox、失败/unknown、扫码/签发/吊销记录；不记录token或默认消息正文。新HTTP接口与迁移未实现 |

## r51 会话滑动续期的复用与差异

参考 commit 698b719eccceba626b11eaf4d9884a3e89d4abd5，已在 r51 中：np_session.created_at 是持久创建锚。onboarding.go:229 的 ResolveSessionForHTTP 先认现有会话，再在低阈值下触达；pgstore.go:487 原子检查 revoked_at、expiry，只延长，不复活吊销/过期记录，且限制为 created_at＋30天。借用可注入 now、持久CAS、吊销优先和重启验证结构，不声称直接复用该TTL政策。

已核测试坐标（仍为f7f31fca）：internal/identity/onboarding_test.go:30 TestSessionSlidingRenewalClockBoundaries，:58 TestTouchSessionRejectsExpiredRevokedAndNeverShortens，:113 TestSessionNoPollingExpiryAndAbsoluteLimit；internal/identity/httpapi/n3_test.go:218 TestHTTPRenewalReissuesCookieOnlyWhenExpiryIsExtended。外部14天每次交互续期须新测，不能沿用“绝对寿命”判据。np_session迁移/旧Hub兼容参考 cmd/workpulse/np_session_migration_binary_test.go；本单未运行这些产品测试来冒充外部实现通过。

## MCP 规范依据

2026-11-06只读复核官方 MCP 2026-07-28 页面：

- [Streamable HTTP](https://modelcontextprotocol.io/specification/2026-07-28/basic/transports/streamable-http)：单一POST endpoint，JSON或请求级SSE；带非法Origin返回403。不要沿用已移除的GET流或协议级session来实现本提案长期身份。
- [Authorization](https://modelcontextprotocol.io/specification/2026-07-28/basic/authorization)：OAuth资源服务器、标准Bearer、Protected Resource Metadata与授权服务器发现。官方规范允许不启用授权；本产品已定必须启用，不把产品强制要求误写成所有MCP实现普遍强制。

以上只核文档。Hub当前是否已有可直接复用的资源服务器、授权服务器、飞书网页扫码回调、MCP客户端互操作、代理保留Authorization/Origin和长轮询：未核实。网页身份与MCP授权衔接必须在实施阶段验证，不以当前站内飞书绑定码替代扫码。

## 第9点官方协议复核（2026-11-06）

- [官方changelog](https://modelcontextprotocol.io/specification/2026-07-28/changelog)：已读原文，按补5撤掉旧握手/会话/GET流/SSE重放假设。无状态是协议层，Hub的AG、token与投递仍须持久化。
- [版本与兼容](https://modelcontextprotocol.io/specification/2026-07-28/basic/versioning)：server/discover、每请求版本/能力与版本拒绝；一期不承诺双时代自动兼容，旧SDK未升级须明确报不兼容，不静默降到旧协议。
- [传输请求元数据](https://modelcontextprotocol.io/specification/2026-07-28/basic/transports/streamable-http)：MCP-Protocol-Version/Mcp-Method及按方法所需Mcp-Name头与正文一致；断流作为请求取消，重新请求而不是SSE续传。结果resultType按现代协议校验。
- [订阅](https://modelcontextprotocol.io/specification/2026-07-28/basic/patterns/subscriptions)：subscriptions/listen是按过滤的通知流，resourceSubscriptions只通知资源变更，不替代应用投递ACK。评估取舍见design第4a节。
- [客户端注册](https://modelcontextprotocol.io/specification/2026-07-28/basic/authorization/client-registration)：新客户端优先CIMD，DCR是已弃用兼容路径；预注册OAuth客户端与预建Agent席位为两回事。
- 状态证据分开记录：[2026-05-21 RC博客](https://blog.modelcontextprotocol.io/posts/2026-07-28-release-candidate/)标题为Release Candidate；[官方2026-07-28 release](https://github.com/modelcontextprotocol/modelcontextprotocol/releases/tag/2026-07-28)本次已标stable，指向commit 5f5440b。保留组长要求“定稿状态开发前须再核”，不把历史RC标签当作当前只有RC的证明；SDK/目标客户端可用性未实测。

上述均为只读网络规范核对，本单没有实现MCP服务、资源订阅或WSS。无论通知流存在多久，平台14天令牌/90秒租约/3天AG仍按持久平台时钟和明确交互规则计算。

## A路授权资源补充（2026-11-06）

组长已在工蜂 opus50tc2k/r53-integration 的593ed5efdaa51bf671cc41fa5d8dc0b7c20ed78e（tree 7378e8bf41342e5edce35b42ed09aa783c1aa3c0）docs/design/R53-MVP-CONTRACT.md 第6节冻结MVP固定四scope、不做子集、A/B/C共享TenantKey与Resource。该坐标已fetch并读取原文，覆盖先前待确认授权细节。A路实现位置internal/extseat/service.go与PG0105：令牌行持久audience及 `list_online send_message wait_for_messages ack`；签发签名不变；Authenticate、Recheck及业务现核共用load，资源/完整scope不匹配拒绝。Service由main注入TenantKey（DW_FEISHU_TEAM_TENANT_KEY）与Resource（暂定https://dw123.cmjrt.com/mcp），A不单独解析环境或内置默认值。C只交验证后的Principal，不交飞书access_token/refresh_token。主程序装配归组长，本路未修改main或声称跨路装配已完成。


唯一写者合并的测试与证据入口：docs/reviews/r53-extseat-writer-reconcile/（命令UTC/rc/hash、变异失败原文、受测源码摘要、完整包SKIP清单）；身份投影锁改动在internal/extseat/outbox.go与internal/m8/extseat.go，时钟重核在service.go/inbox.go。0105引入后的真实跨runtime claim集成迁移清单已补齐，数据库仍由pgtest隔离，未改生产配置或helper状态机。


A路最终门证据入口改为docs/reviews/r53-extseat/EVIDENCE.md（合同75a40880第7节，交件前已同步）；旧根EVIDENCE保持fe67字节，由组长迁移。实际持锁执行会话01a1af35-98c2-7451-a54a-6ec76c50d4f9依据组长05:14:20Z新裁定接续。产品源fe67不变，最终证据仅追加；Go包race-count3整包store实测需为90m总时限留余量，原90m超时保留，最终按120m完整重跑通过，不缩减用例。
## B路实施坐标（2026-11-06）

父基线144b215e32e718553d9fef2e79c83ece164f6514；分支coder56solzzc/r53-mcpgw；internal/mcpgw/INTERFACE.md列路由、A/C依赖、0107表列与密钥来源。cmd/workpulse/mcpgw.go提供显式注册与密钥失败降级；主装配仍由组长集成。已重新fetch并核rulings的㊹-补12/13/14及㊺-补1；0107来自16:13:02Z定向分配。官方2026-07-28的Streamable HTTP/versioning/server/discover/client-registration本轮重新在线核对，支持无initialize的逐请求头/元数据及CIMD；未把假上游联调称为真实飞书或第三方SDK互操作。生产密钥来源DW_MCPGW_OAUTH_KEY_FILE；本地测试用随机密钥、临时SQLite/临时PG，不使用在线Hub或生产库。

协议自查增补：重新核对 [MCP Base Protocol/Error Codes](https://modelcontextprotocol.io/specification/2026-07-28/basic/index#error-codes) 的保留区规则，业务错误改用1001/1003/1008/1009/1029，标准HeaderMismatch/-32020与UnsupportedProtocolVersion/-32022保持；请求ID只接字符串或整数，Accept的q=0拒绝。对应正反与变异在本路证据中绑定，不改业务权限。

## D 路后端接手（2026-11-07）

父 c5152a213783e63626c161afefb34a7f45cfaea3，线程01a1b33c-11e7-7ec0-8de2-ce60abd9d9f5 持 r53-tempgroup-b.writer.lock；分支coder56solzzc/r53-tempgroup。实际接口/0106全部表列/SEQUENCE/通知在 internal/tempgroup/INTERFACE.md。前端45910532原样合入；新增 internal/tempgroup、internal/m8/tempgroup.go，main已注入真实Groups，Console三路由包既有认人/Origin。测试用本地pgtest临时PG、固定时钟与随机测试凭据，不使用现网库/身份。构建Python使用已有仓外helper-deps-lock-venv，普通系统Python缺websockets.sync导致发现失败的原始日志保留，不改测试门。
