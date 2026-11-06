# 设计

## Context

父基线 f95fa0b3 是原作者冻结提案；本次按业主 ㊹-补1～补4 改写。代码观察固定在 r51 f7f31fca，不能把拟新增能力说成已有。现有 identity 以 np_agent／np_group／np_group_member 组织成员，Hub 普通投递使用 SQLite session_delivery；ACK v2 目前检查 driver。MCP 标准参考与代码坐标见 context.md。

## Goals

- 团队用户经网页飞书扫码自助接入外部 Agent，与指定目标组成可清理的临时 AG。
- REST 与 MCP 四工具同权，只通讯，令牌可吊销、限流、审计，投递不伪成功。
- 将身份、14 天令牌、90 秒在线租约、3 天编组、24 小时消息四种寿命明确分离。
- Console 可见性与解散授权在服务端成立，维持普通编组和 helper 既有行为。

## Non-Goals

不开放远程执行、任意工具、宿主/文件访问、升级或管理命令；不靠管理员预建席位；不改普通 Agent 名称规则或普通编组生命周期。本单无实现、上线、真实飞书/MCP 互操作测试。

## Decisions

### 1. 飞书扫码身份与标准 OAuth（GW-001～004、GW-009）

已定（组长 2026-11-06）：接入者网页飞书扫码登录，不是填写姓名或持有客户端自报 tenant_key。服务端验证扫码授权响应、state/PKCE/回调绑定，并从可信飞书用户信息取得 tenant_key；必须精确等于服务端配置的本团队租户。缺失、不相等、扫码失败均拒绝，不发令牌、不建席位/AG。internal/feishu/feishu.go 的现有 reply 租户比较只是参考，不是可直接复用的完整网页登录校验；不得信任请求体声明或 FCS RPC 透传枚举来完成身份认证。

验证成功并提交目标 Agent 与合法短名后，自动完成席位/AG 创建和令牌授权，不人工审批。令牌绑定不可变外部主体、席位 ID、AG ID、最小 scope（roster:read、message:send、inbox:read、inbox:ack）与吊销代际；MCP 客户端不能覆盖主体或编组。内部身份键采用 provider＋可信 tenant_key＋应用/issuer＋open_id；有 union_id 时只在同一可信 issuer/租户下绑定别名，不跨租户/应用按姓名或字符串直接合并。归属显示名优先飞书英文名；缺则中文名转小写拼音，仅保留 [a-z]，不作为内部 ID、授权或查重依据。转写为空/多音字的确定性策略标为实施前待核实，失败不改用用户自报身份。

Hub MCP 是 OAuth 2.1 资源服务器：RFC 9728 受保护资源元数据、授权服务器发现、标准 Authorization: Bearer、audience/resource 与 scope 校验；授权服务器把上述飞书网页登录嵌入标准授权流程（含 PKCE），将绑定该主体的令牌交给客户端。REST 共用授权中间件，不把静态自定义 token 或扫码 cookie 当作 MCP 标准认证。令牌高熵，持久化只保存不可逆验证值，凭据值仅经受控 OAuth 交换，不放 URL、日志、页面列表或审计；明文显示仅在确需人工领取的受控签发一步，不默认要求复制粘贴。

### 2. AG 创建与命名（GW-002、GW-008）

已定：验证通过即自动创建临时编组，显示名 AG＋全局唯一递增序号，例如 AG3；成员只有新外部 Agent 与所选活跃目标 Agent。同一使用方可为不同目标分别创建 AG，不把目标原有编组的其他成员引入。重试以绑定主体、目标、短名的创建幂等键取回同一结果；一次新的接入意图可分配新 AG，不能把返回丢失误当成允许重复签发明文令牌。

技术建议：identity PG 中增加持久全局 sequence，事务内取值；数据库唯一约束守住 AG 序号和名称，事务回滚可留空号，已用号永不复用，不从进程内计数或 MAX+1 分配。备份恢复时 sequence 须推进至历史高水位以上；序号溢出失败关闭。与普通组共用现有全局组名唯一约束；已有或并发普通组若占 AGn，系统分配器以唯一约束为准取下一号重试，不改旧组或普通组命名规则，兼容策略须经迁移测试冻结。分配、外部主体/席位、两成员、令牌哈希和创建审计在同一 identity 事务提交，失败不暴露半成品。

使用方短名必须精确匹配 [a-z0-9]{1,8}，不自动把大写、空白、Unicode 转成合法值。Agent 名＝短名＋“_”＋AG 名，例如 mybot_AG3，显示保留 AG 大写，比较使用确定的规范形式；稳定内部键仍为 Agent ID。全名适配器必须保留这套名与稳定属主映射，显式全名从服务器清单复制，不让显示归属名重名造成寻址歧义。

不另做用户侧重名查询：全局唯一且不复用的 AGn 加上每 AG 一个外部 Agent，已保证组合名不会冲突。保留数据库唯一约束和幂等核验是并发/损坏防线，不是要求用户另起名。r51 ValidAgentName 不接受下划线，普通组也可自行起 AGn：实施必须给 external 类型新增受控命名/解析分支及共享组名唯一约束的跳号适配，不能把组合名直接塞进普通校验或全局放宽 helper 名称规则。

### 3. 四种寿命、续期与时钟（GW-002、GW-005、GW-006、GW-008）

| 对象 | 已定寿命与触发 | 到期效果 |
|---|---|---|
| 外部身份令牌 | 每次成功鉴权交互时刻＋14 天 | now >= expires_at 拒绝，须重新飞书扫码 |
| 外部在线租约 | 距最后成功请求 90 秒 | now >= last_seen_at＋90 秒报告 offline，令牌未必失效 |
| AG | 初始创建时刻＋3 天；有效发送或 ACK 续到本次＋3 天 | 逻辑上即失效，清理任务最终解散 |
| 离线消息 | 入队时刻＋24 小时 | 到期终态 dead，原因可查询，不因令牌/AG续期延长 |

已定（㊹-补4）：令牌的成功鉴权交互含长轮询收件、发送、ACK、在线清单；即使没有消息，合法空轮询也续令牌。鉴权成功后因业务参数非法不意味着可绕过过期/吊销；续期以服务端鉴权成功时刻为准，业务失败不续 AG。长轮询进入时鉴权续期，返回消息/完成响应前再现核身份和 AG；吊销立即中断等待，不因已进入长轮询就保留权限。90 秒按成功请求/合法轮询响应维护；非法请求不能保活，界面不把在线等同正在推理。

已定（㊹-补6）PM-GW-01：只有组内成功发送（持久受理）或成员对实际投递的首次有效 ACK 才续 AG；纯轮询、清单、鉴权失败、越权、重复消息/重复 ACK 不续。正文由平台转发也是消息往来，两侧走同一续期入口。明确将“3 天无消息自动解散”替换为上述滑动到期规则；不使用 token 续期代替 AG 使用。组未到期时才允许续期，正好 3 天先判失效，不用迟到消息复活。

时钟用服务端可注入 UTC 时间，所有 expiry 持久化；客户端时钟无效。续期先现核有效、未吊销且版本匹配，CAS 只延长不缩短；时钟回拨早于已持久 last_activity 时拒绝本次续期/副作用并审计，恢复到有效时刻或重新认证后再处理，不能用回拨延长授权。

写库节流技术建议：复用 r51 的按 token 串行和触达限频思路，但不能直接照搬“低于阈值才续”的近似语义。每次成功鉴权先把 token_id、鉴权时刻、吊销代际写入可恢复触达记录（可与业务事务一起提交），相同 token 的并发批次取最大时刻；expires_at 投影最多每分钟更新一次，鉴权有效期读取持久 expiry 与已提交触达最大值。收到成功响应前触达必须持久化，写失败不得声称已续期；不能只靠一分钟内存缓存、异步 flush 或提前多加一分钟。此方案节流的是 expiry 投影 UPDATE，不宣称每分钟仅一次总 I/O。实现也可先逐请求持久 CAS、禁用时间节流保证精确语义，待测量后再启用等价聚合；其选择属技术假设，须在实现设计中冻结。

AG 的消息使用时刻同样先持久化，序号/代际保证解散和续期互斥；每次接受发送/ACK 之前现核到期。沿用 r51 698b719e 的可注入时钟、哈希查会话、吊销优先、持久 Touch CAS、重启/边界测试，不沿用它的 30 天绝对上限或旧会话不续期规则：本次没有新增外部令牌绝对寿命裁定。

### 4. 消息、ACK、在线与隔离（GW-001、GW-003～009）

保留 MCP 单一 Streamable HTTP POST endpoint 与 JSON-RPC 四工具：list_online、send_message（to_full_name、text、可选 client_message_id）、wait_for_messages（wait_ms 0～25000）、ack（delivery_id、状态与 v2 元数据）。REST 同能力：GET /api/agent-gateway/v1/online、POST /messages、POST /inbox:wait、POST /inbox/{delivery_id}:ack；均为待实现接口路径，不是现有端点。单次收件上限建议 50，席位/来源限流、并发和队列配额必须存在，数值待冻结。错误覆盖 unauthorized、forbidden、rate_limited、delivery_expired、invalid_ack、origin_rejected、group_expired、group_dissolved；接口与错误均对应上述固定需求。

发往外部席位复用 session_delivery 的队列/租约/重试/过期事实，通过稳定外部席位映射 key_id/session_name；不得用显示名或用户传入 key 取队列。ACK 保留 delivery、席位、ack_protocol、processing_mode、generation 绑定与提交后确认。r51 v2 只认 driver，需独立 HTTP adapter/transport-neutral 验证而不放宽旧 driver 路径。未知受理写 unknown，凭 durable delivery ID 对账，不盲重发或伪成功。消息需带不可变 AG ID 与解散代际，入队与领取都再核组状态。

MVP 只显式全名寻址/提及，不接受短名模糊匹配、广播或跨 AG 权限扩张。外部 Agent 必须作为受控 external Agent 身份进入编组校验，不能借飞书人类或系统来源豁免来发送。目标 Agent 可属于其他正常组，但本 token 只见本 AG 的两名成员，不递归取得目标的其他组。令牌活跃、组到期/解散时也不能继续发信、取信或复活 AG。

MCP 校验 Origin（出现且非法则 403），并独立校验 Bearer audience/resource/scope；R52 trusted origins 仅为来源许可，不授身份。REST 共用令牌授权；网页扫码回调与 Console 解散保持既有网页 CSRF/Origin 安全，MVP 不开放任意 CORS。所有审计不含凭据与默认正文。

### 4a. 第9点：无状态MCP、断流与分期（GW-003、GW-006、GW-007）

已定（组长2026-11-06，㊹-补5）：一期使用2026-07-28 Streamable HTTP，不建立MCP协议层会话，不使用Mcp-Session-Id、initialize/notifications/initialized、GET事件流或resources/subscribe。必须实现server/discover；它和tools/list属于协议发现，不是新增通讯工具。每请求params._meta含io.modelcontextprotocol/protocolVersion与io.modelcontextprotocol/clientCapabilities，可带clientInfo；各POST携带匹配的MCP-Protocol-Version与Mcp-Method，tools/call等有name/uri的规定方法还须Mcp-Name，头与正文不符拒绝。正常结果带resultType=complete；本通讯MVP不启用需要input_required的交互扩展，客户端不得把该状态当最终完成。鉴权与能力按请求重核，不依赖先前initialize或连接黏性；令牌、AG、消息及ACK都是平台持久状态，不是MCP会话。

对话式调用者按需要调用同一四工具；自主客户端以“收件→自身处理→ACK→下次收件”循环，进程可常驻，网络仍为独立HTTP请求。wait_for_messages是最长25秒的长时运行工具调用，可单个JSON结果或请求级SSE响应；不要求WebSocket才可自主循环。OAuth客户端注册优先Client ID Metadata Documents，校验元数据/redirect URI与issuer；DCR已弃用，只是旧服务器兼容备选，不作新实现默认，也不与管理员预建Agent席位混淆。

推送/HTTP返回只表明尝试传输，不算平台送达、接纳或完成；须匹配delivery_id与generation的相应ACK事务提交。响应流中断时不使用Last-Event-ID、SSE event id续传或协议重放；客户端用新JSON-RPC request id发起新收件。该批未ACK的持久投递在旧领取租约释放/到期后下次收件再给，已持久ACK的阶段不回退。至少一次投递需要外部客户端按稳定delivery_id＋generation做接纳/处理去重；重收不再次执行业务副作用，重交ACK幂等。ACK本身返回断流时也以数据库已提交事实为准，客户端可重交同一ACK，不以网络失败推导“服务端没写”。send_message断流重试使用同一client_message_id（按稳定发送席位＋AG代际隔离），相同键/正文返回既有受理结果，冲突正文拒绝；结果不明保留unknown，不以新的JSON-RPC id创建新消息。去重保留期至少覆盖消息24小时及在途结算窗口，具体上限实施前冻结。

一期可选方案评估：增加只可读本人收件箱资源，以subscriptions/listen的resourceSubscriptions订阅该资源更新，仅推“有变化”通知；收到后仍调用wait_for_messages取得持久投递并ACK。优点是降低空轮询延迟/请求量；代价是额外资源权限、代理SSE/断流重订阅、通知丢失与重复测试。初始MVP选择四工具轮询为必需路径，资源/订阅为可选增强，不作为上线前置；若实现，首条订阅ack与subscriptionId按规范，通知不能携他人消息/充投递ACK，断流重订阅并主动收件补漏。无订阅能力的客户端仍以25秒工具循环，REST保持相同通信权限；缓存资源必须private且不以缓存续租。长通知流的保活帧不算成功鉴权交互，不续令牌/90秒租约或AG，需真实成功请求续平台租约；吊销/到期终止流。

二期仅考虑平台私有WSS复用helper WS收发与delivery_ack v2，单独鉴权/限流/回归，不称标准MCP WSS，不新增执行权限；若一期轮询/订阅已满足稳定自主客户端需求，可不做。官方RC博客与stable release状态分别见context；“定稿状态开发前须再核”，并核SDK/客户实际兼容，不把本次文档核对当互操作已通过。

### 5. 清理、两侧通知与在途消息（GW-006～008、GW-010）

技术建议：每分钟分批扫描到期的 temporary AG，以状态＋generation＋expiry CAS 由 active 转 dissolved，持久记录原因（expiry/manual）、操作者、时刻与两侧通知 outbox。同一事务把成员授权作废，保留墓碑/审计/序号不复用；不要直接 DELETE 丢失在途归属。启动后立即补扫，幂等且有批大小/游标；后台晚扫不延长授权，每次请求也检查 now < expiry。

解散与发送/ACK 串行化或以相同代际 CAS 协调：解散先提交则新发送/新领取拒绝；已排队但尚未领取的本 AG 消息转为失败，原因 group_expired/group_dissolved；已领取的在途消息不可撤回，不重新投给别组，不伪记 processed，保留已受理事实和后续真实结果。解散后应用 ACK 不再续组或重开投递；需要结算的已领取 ACK 由现有拟定 ack 工具/REST ACK 内独立、只准提交原 delivery 终态的分支处理（不增加第五个工具/额外权限），凭原身份/代际且仍现核令牌；过期/吊销令牌不能借此恢复通用权限。跨 PG 身份库与 SQLite 投递库不是分布式事务：PG 先关授权＋outbox，桥接事务逐项取消投递并写投递审计；不能宣称跨库原子，崩溃后重试凭相同事件 ID 对账，失败留 pending/unknown。待最终实现核对现有取消函数后冻结精确状态映射。

两侧各一条幂等生命周期通知：目标走既有系统通知路径，外部方走仅属于自身的控制收件记录；即使原 AG 已关闭也可查询自己的解散原因，不能借控制通知继续普通发信。离线时通知持久化，不能依赖已删除的编组送达；令牌已过期/吊销时不向其泄露队列，新扫码后经相同稳定主体授权才展示本人生命周期结果。通知失败不回滚解散，outbox 留待重试，管理员可见失败。主动与自动解散共用上述规则，不改目标的其他编组或暂停目标 Agent。

### 6. Console 与解散授权（GW-010）

已定（㊹-补3）：Agents 列表下方单列“临时编组”，服务端只返回当前登录者名下任一 Agent 是成员的 AG；创建者身份本身不是侧栏可见的额外豁免。平台管理员也不在个人侧栏默认看到无关 AG；其管理权限通过独立受控属性访问实施，不改变“只列相关”。普通编组与 Agents 既有列表保留，AG 不混入常规组树造成误操作。

Agent 编组属性页显示两成员、创建者飞书归属显示名、created_at、last_used_at、expires_at（预计自动解散时刻，以服务端 UTC 和时区格式显示）；不显示 token/open_id 原文。与当前任一成员 Agent 的 owner 匹配，或经现有 RequireConsolePlatformAdmin 同口径现核为 active platform_admin（manager 不算），才可在二次确认后解散；服务端操作时再次现核，不能只靠前端按钮隐藏，不能沿用“只有建组者能删组”。确认文案明确两侧断开、在途处理；取消确认零写入，重复解散返回同一终态和审计关联。到期或别人解散后刷新页面去掉操作并显示终态。

### 7. Hub 改造与交付阶段

context.md 表格逐项列出 r51 已核模块/表和拟新增项。PG 的 np_group 加临时类型/序号/最后使用/到期/状态/代际，np_agent 加 external 类型/受控名，np_identity 外部主体映射；令牌与续期/在线租约、解散 outbox 为拟新增持久记录，表名/迁移编号未冻结。在线清单不能只依赖 WebSocket client registry；ACK 新 transport mode、普通路由编组校验、AMR 精确全名适配均需独立回归。

Phase 1：飞书扫码→标准 OAuth→AG 自动创建→MCP/REST 四工具、生命周期、Console 相关 AG 与解散一并交付；完成边界/权限/重启测试、标准客户端互操作与原 helper 回归。Phase 2：压测、指标、限额与运维清理；平台私有WSS仅按第4a节在有必要时单独立项，订阅足够可不做，不扩大token权限。多目标通过多个双成员 AG 实现，不将“多编组支持”留到 Phase 3 才满足已定要求。上线与回滚另派：回滚关闭外部入口/清理 worker，先吊销令牌并保留状态和序号，不误清普通组。

## Test Strategy

以下为后续实现的验收计划，本单不声称已跑产品或变异：

| 判据/固定场景 | 正反例与边界 | 目标变异（必须红在语义） |
|---|---|---|
| GW-002-S03～05 身份 | 本团队扫码成功；他租户、缺 tenant、伪客户端身份拒绝；英文/拼音与同名稳定 ID | 去掉 tenant 比较；按显示名合并主体 |
| GW-008-S03～04 创建命名 | 并发唯一序号、重启不复用；同短名不同目标 AG；不合法短名拒绝 | sequence 换进程计数；允许大写/第9字符 |
| GW-002-S06～08 14天 | 13天23小时交互续至本次＋14天；14天整无交互拒绝；各四工具/空轮询均续；吊销并发优先 | 去掉续期持久写→重启提前失效；删吊销判定 |
| GW-008-S05～07 3天 | 3天前发送/有效ACK续；3天整拒绝；仅轮询不续（已定，㊹-补6）；清理崩溃恢复 | 只在内存续；轮询错误续组；清理不查代际 |
| GW-010-S03～06 Console | 自己/别人的AG隔离；双方主人/管理员解散、无关人403、取消确认零写；全部属性 | 去掉相关过滤→别人AG出现；去掉服务端权限 |
| GW-006-S03 解散 | 排队失败、已领取不重投、两侧离线通知、outbox重启恢复 | 删除解散栅栏或跨AG领取 |
| GW-003-S03～05/GW-006-S04 第9点 | 无initialize直接discover/工具；头/元数据/resultType；断流重取同delivery只处理一次；可选订阅仅本人通知；平台租约不靠流保活 | 强制initialize；删头一致检查；返回即伪ACK；重发更换delivery ID；通知充ACK |
| GW-003/004/007/009 原边界 | 标准OAuth发现、audience/scope、Origin、REST等价、伪ACK、轮询25秒、90秒offline、24小时dead | 逐项删除保护，原判据须红 |

Console 浏览器套件必须登记到统一 runner，断言含真实请求次数/下限，不以固定成功串代替；相关 AG 列表过滤变异必须使“别人 AG 不出现”用例红。时间用可注入时钟，不真等14天；持久续期必须跨真实进程/存储重启验证。PG/SQLite 故障注入验证 unknown 与对账，不能冒充跨库事务。实现时 run-build-tests、相关包 race、两渠道浏览器、严格 OpenSpec 与独立验收另按派单执行。

## Open Questions

PM-GW-01（已定，㊹-补6）：纯轮询不续编组。其余已定八点不得重新列为等待事项。PM-GW-02 与拼音库、扫码 SDK/权限、OAuth 授权服务器、续期聚合数据模型、代理长轮询互操作、解散后 ACK 具体映射均标技术未核实，实施前核定；本单不对未核实能力作已实现承诺。


## A路实现锁与授权细节（2026-11-06合并核对）

对应GW-002令牌撤销/到期、GW-005在线租约、GW-006持久投递与过期、GW-008名称路由及GW-010原helper兼容：PG0105保存令牌audience与MVP完整四scope，统一配置由main注入；签发签名保持。外部路由只在SplitAgentName认可形状后查询身份，兼容Hub小写索引。跨库投影以PG事务内锁定的双方身份快照传给Hub适配器，适配器不再申请身份PG连接，SQLite成功仍只算projected。所有锁等待后重取时钟再判整点到期，防止请求开始时间复活过期令牌；瞬态outbox投影失败保留原稳定ID后台重试。上述不增加权限或变更原helper ACK/重试语义。

## B路协议与短期OAuth状态实现（2026-11-06）

对应GW-003/GW-004/GW-009：MCP与REST共用四工具分发，逐请求校验版本/能力与镜像头；集中错误映射，默认只信PublicOrigin的Origin接口留待r52集成。C回调仍经extwire签A令牌，B不改变C签名；浏览器只见一次性code，兑换核S256及完整客户端绑定，再现核A。B提供可选C StateStore适配，不改C包或撤回㊹-补14的单进程内存许可。

组长2026-11-06T16:13:02Z分配0107：mcpgw_oauth短期表以resource/kind/key_hash隔离，密文AES-256-GCM绑定同一三元组；每次写入清过期行，读/原子兑换即时拒过期，并发DELETE RETURNING只准一次。密钥由DW_MCPGW_OAUTH_KEY_FILE指向仓外32字节私有文件；缺/错只禁用本路并日志，不影响Hub。MVP不支持轮换，换钥使在途登录失效。schema只由迁移创建；集成extwire.CheckSchema及账本期望串由组长维护。
