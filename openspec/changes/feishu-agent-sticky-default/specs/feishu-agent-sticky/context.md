# R14 飞书私聊目标选择现状勘察

## 固定来源与读取边界

以下现状全部来自 r46 commit **2af469dd6919d8eb6064aa7da0e646f0851becf8**；`文件:行`均相对该树，可用`git show 2af469dd:<文件>`独立核对。不是对当前生产配置或真实飞书客户端的在线验收。本能力没有现存主规格；本件按授权只放变更目录，主规格基线/context留待另派实施时补齐。先读本context、同目录spec，再读proposal/design/tasks；进行中R5/R11仅作OpenSpec格式参照，未找到本能力已发布的时间线入口，不虚构现行承诺。

## 1. 私聊入站到目标选择

| 路径 | r46 行证据 | 当前行为 |
|---|---|---|
| 入口文本 | internal/worker/schedule_worker.go:77–90、491–500 | 从content JSON取text/content并TrimSpace，交Prefix.Dispatch；Matched回复经sendReply返回，空Reply不另回复 |
| 总调度与重复消息 | internal/m8/hub.go:1569–1595、1609–1648 | 先澄清答复；持久control_task按消息ID处理重复，已有done结果不重新路由；不是粘滞状态 |
| 显式命令/选择优先级 | internal/m8/hub.go:1684–1756、1760–1789；internal/store/migrations/0021_control_plane_p1.sql:44–54 | 安全、登录、sync、申请、系统等先行；L1显式session规则进dispatchSelector；fallback中先成员提及再selector，再自定义prefix路由，最后默认私聊。不能在Dispatch最前新增默认路由把已有命令吞掉 |
| 有前缀 | internal/m8/hub.go:3952–4013 | parseSelector拆名字与正文；新平台ResolveIdentity识别当前账号，再用resolveNewPlatformFeishuSession；唯一目标后按原信封路由，正文剥掉选择前缀 |
| 前缀有歧义 | internal/m8/newplat_feishu_inbound.go:60–78；internal/m8/hub.go:2390–2394、3964–3985 | exact优先于fuzzy；多命中回歧义，不选第一个。原文模板为“存在多个 %s 会话：%s，请用全名指定”，列表用顿号连接；空候选变体无冒号列表 |
| 显式目标不属于本人/离线/停用 | internal/m8/newplat_feishu_inbound.go:44–49；internal/m8/newplat_session_name.go:22–36；internal/m8/hub.go:3968–3975 | 不把外属主全名或失效全名降级为模糊匹配；回复“未找到你名下在线且可用的 Agent：”加原请求，或未绑定/停用账号提示 |
| 无前缀 | internal/m8/hub.go:4016–4045 | 私聊、当前账号active；枚举本人在线active Agent，1个即路由，多个回候选，0个返回未处理；外层可能保留control_task/L2处理，不保证0个时已有固定无Agent提示 |
| 多候选原文 | internal/m8/hub.go:4039–4040、4073–4074 | “你有多个在线会话（%s），请用 #会话名 内容 指定”，%s为按名字排序后顿号连接的短会话名；不是每行井号全名，也没有上次对象置首 |

`l1_nl_dispatch`虽然在迁移种子中写handoffL2，实际executeL1Rule仍调用fallback，可能被上述多会话提示先处理；不能仅凭规则名宣称无前缀多Agent必走LLM（internal/m8/hub.go:1749–1752、1851–1854；internal/store/migrations/0021_control_plane_p1.sql:52）。自定义prefix规则也早于默认私聊，以上默认回文以未被前序路径处理为前提。

## 2. 单 Agent 免前缀与“我名下”的口径

- **已经成立，但数的是本人当前可用在线Agent，不是名下全部登记数。** 新平台以`(Provider=feishu, Issuer=BotChannelID, Subject=SenderOpenID)`解析账号（internal/m8/newplat_feishu_inbound.go:13–18），入口要求UserActive（internal/m8/hub.go:4021–4027）。本人集合从`sessionClients[AgentKeyID(ownerLC)]`的非nil连接取名，再逐次FindAgentByName重核AgentActive，最后sort.Strings（internal/m8/newplat_feishu_inbound.go:23–41）。所以离线、suspended/revoked不算，即便旧socket还未消失，停用也可先排除。
- 该默认集合没有跨属主共享/同组扩展，也没有按Origin、CLI/模型、producer或NoDirectory再筛选（同文件:23–41）。Agent模型有selfhost/hosted两种Origin，active/suspended/revoked三态；Current包括active和suspended但默认集合只取active（internal/identity/model.go:117–155）。因此不能把Console登记Agent总数、公开目录可见数或“同组能通信”当成免前缀计数。
- 同组共享是另一条可见性/通信路径：新平台目录广播另作NoDirectory过滤和AgentsCanCommunicate校验（internal/m8/newplat_directory.go:36–55、63–75）；不是默认私聊枚举。本人唯一Agent且另一个属主在线的源码测试见internal/m8/newplat_feishu_inbound_test.go:137–162，多本人Agent提示测试:118–135；本轮仅阅读，未将这些测试冒称新需求验收。
- 旧平台fallback按session_endpoint.Active及APIKey账号关联枚举、排序，并可能返回多个key下的名字；不等于新平台凭据属主判定（internal/m8/hub.go:4046–4075、6656–6691）。本提案建议首期新平台身份私聊，不自动给旧账号投影粘滞权限；兼容边界见PM08。

## 3. 候选排序、井号与复制解析

- 当前默认/歧义候选沿用排序后的会话名，使用“、”串成一行（internal/m8/newplat_feishu_inbound.go:40、51–78；internal/m8/hub.go:2390–2394、4040）。全名不是拼旧后缀：identity.FullName返回`ag.Name + "." + user.ShortName`，ResolveFullName按最后一个点解析并查当前身份（internal/identity/service.go:285–295、418–435）。候选将来应从身份权威取实际全名。
- `#全名 正文`在Hub解析层可以指定目标：parseSelector要求`#`后紧跟非空白token，首token不能再含`#`，余下文本是body（internal/m8/hub.go:6427–6449）；全名随后须通过当前owner/在线/active检查，点号全名不成功时不尝试猜其它对象（internal/m8/newplat_session_name.go:10–35；internal/m8/newplat_feishu_inbound.go:44–49）。`# 名字`不是合法selector；原parseSelector允许空body，但这不等于已经实现“只选择、不发空消息”的切换指令。
- **整段候选不能原样当多选投递。** `#a.owner\n#b.owner`今天会把a当目标、`#b.owner`当body（internal/m8/hub.go:6436–6445、3988–4005）。候选本身可以整体复制；PM07已定粘贴回Hub后只提示“请选择一行”，不选首项、不广播、不更新粘滞。
- AMR也支持legacy井号selector（internal/m8/amr.go:1548–1564），`# `、`## `等Markdown标题被排除，`#全名`不属于这类标题；输出转发/IPC正文的AMR入口是另一条路径（internal/m8/amr.go:33–46、1420–1467）。因此井号语法确有复用，不能泛称完全无冲突，更不能让系统候选回文当Agent正文进入AMR。R14只改私聊默认选择，不改AMR规则或授予群发。
- 代码侧Feishu回复是纯text：worker保留原BotChannelID与ChatType（internal/worker/schedule_worker.go:390–408），SDK把原字符串编码进`{"text":...}`并以MsgType text发送（internal/feishu/feishu.go:283–291）；回入extractText只做JSON取值和首尾TrimSpace（internal/worker/schedule_worker.go:491–500），不会主动剥掉每行`#`。这支持代码层井号往返，但未实测真实飞书桌面/手机复制、富文本粘贴和换行格式，不把代码勘察当客户端回环已通过。

## 4. 可用存储、现存状态与重启

| 现存设施 | r46 文件:行 | 能否直接代表上次对话对象 |
|---|---|---|
| Hub内存连接/同步/去重map | internal/m8/hub.go:154–171、365–387、1236–1246 | 重启重建；sessionClients用于在线、syncTargets用于流同步，不是按账号+机器人通道的粘滞记录 |
| SQLite session_endpoint | internal/store/migrations/0003_session_endpoint.sql:2–10；internal/store/sqlite.go:3730–3759、3787–3818 | 持久保存端点/last_seen_at/active/mirror_to，键是key_id+session_name；不能区分“谁在哪个机器人最后选择谁”，LastSeenAt是连接活动而非对话时钟。断线会写Active=false（internal/m8/hub.go:1267–1274）；不能拿数据库Active当重启后实时在线权威 |
| 消息/投递/控制任务历史 | internal/store/migrations/0001_init.sql:34–48；internal/m8/hub.go:1579–1595、4274–4281；internal/m8/amr.go:1058–1070 | SQLite有消息及投递信封，AMR据deliveryID恢复原信封；这是单条消息回程关联，不是可靠的owner+channel最近选择索引，也不能从最后一条mirror/系统通知推定上次对话 |
| 通用SQLite app_config/CAS | internal/store/migrations/0001_init.sql:182–186；internal/store/store.go:133–139；internal/store/sqlite.go:3625–3678 | 有持久JSON、CAS及删除能力，可作存储能力参照；没有现成R14键/状态契约。建议专用小记录，不能本单先写真实app_config或借用mirror_to |

所查入站函数/模型/仓库接口未见已接线的“账号+BotChannelID→上次Agent+过期时间”状态；当前多Agent始终提示不使用历史。不能说“现成粘滞会跨重启保留”，因为该能力尚不存在；若后续只加内存map则必随Hub重启丢失。PM06已定使用SQLite保存目标稳定ID和原绝对到期时间，恢复先重核身份/在线，不重置30分钟。没有读取或写入生产数据库。

## 5. Agent 短名、命名入口与旧平台边界（㉗勘察）

- 新平台Identity中的 `Agent.Name` 是短名；`FullName` 由 `Agent.Name + "." + User.ShortName` 组成（internal/identity/service.go:285–295，418–435）。私聊选择候选枚举本人在线Agent短名，`ownerSessionExactMatch`／`ownerSessionFuzzyMatch` 对该集合匹配（internal/m8/newplat_feishu_inbound.go:23–78）；Agent短名与井号短名匹配字段一致，不应把全名后缀或旧平台名混入短名比较。
- 当前新平台全名解析器只接受 `full.ownerLC == ownerLC`（internal/m8/newplat_feishu_inbound.go:44–50）；因此PM05允许全名指向其他用户Agent时，实施必须衔接既有跨成员授权与归属核验路径，不能把本人的全名解析器当成已支持跨属主，也不能绕过授权直接投递。该点列入R14-S022验收。
- 自备Agent入口在 Console 审核表单 `enrollName`（internal/m8/console_auth.go:1367），请求送到 `/api/console/enroll/applications/{id}/approve`（同文件:783、924–928）；Hosted生成入口为 `hostedName`（同文件:978 起），调用托管创建流程。服务端分别经 `Onboarding.ApplyDeviceClaim` 的名称校验（internal/identity/claims.go:90–116）、`Service.Approve`（internal/identity/service.go:331–385）和 `Service.IssueHostedAgent`（internal/identity/hosted.go:47–78）校验格式；目前 UI 表单可直接提交，未见提交前查重和明确重名提示，需实现任务在两入口补齐。
- 身份库已有唯一索引 `np_agent_owner_name_current`，按 `(owner_lc,name_lc)` 对 `active`／`suspended` 唯一，吊销后释放名称（internal/identity/pgschema.sql:63–65；internal/store/migrate/migrations/0100_identity_baseline.sql:74–76）。`insertAgent` 将该索引冲突映射为 `ErrNameTaken`（internal/identity/pgstore.go:616–628）；它是最终并发保护，不替代交互提交前检查和可理解提示。新增实现任务：两入口提交前查重、提示短名已占用，并保留服务端索引兜底。
- 旧平台会话名走 `session_endpoint(key_id,session_name)`（internal/store/migrations/0003_session_endpoint.sql:2–10），注册名由请求名与key/owner组合派生（internal/m8/hub.go:957–998、1540–1553）；这不是 `np_agent` 身份库，既有唯一索引不覆盖旧平台会话名。PM08已定旧平台维持既有行为，R14粘滞和本次短名唯一性要求不扩到旧平台。
- 存量只读证据 `review/prodops/r14-shortname-dup-20261102` commit `f44ca5e6a09b2ea6d4dbb61cf869a7e1670a50ca`：生产与预发同属主大小写不敏感短名重复均为0；生产有一个严格前缀组 `ts3dev55.zzc`／`ts3dev5520.zzc`，预发无该组。该快照用于固定验收输入，不代表未来线上数据状态；对应精确优先和多候选反例见R14-S021。

## 新提案与实施边界

业主原始功能来源为proposal中的7d376639第⑳条；PM01～08按review/opus50tc2k/rulings 2f9ddaf0第㉔～㉖条已定，短名唯一性按㉗勘察。本轮只改本变更目录内Markdown，不修改上述源码/现有主规格/迁移/配置/通道。真实飞书回环、持久化与并发、群聊不受影响、AMR不误触发都列为未来验收；不apply/archive、不自行实施或发布。

## T-04 实施设计前置勘察（r48 基线）

本节实现挂点与当前能力以派单指定的 r48 `1e17255f28ced042784337903914ca8c126ef4cf` 为准；上文 T-01 旧行为记录仍以 r46 `2af469dd` 为准，不将它们冒充 r48 行号。只读代码，无真实飞书探测。

| 核对主题 | r48 文件:行 | 基线事实与设计约束 |
|---|---|---|
| 私聊身份输入 | `internal/model/model.go:16-31,80-96`；`internal/feishu/feishu.go:319-332,444-485`；`internal/m8/newplat_feishu_inbound.go:12-18` | 入站消息分别携带 `BotChannelID`、`SenderOpenID`、`ChatType`、`FeishuMsgID` 与 `IngressProvenance`；Feishu SDK 长连接事件适配标为 `IngressFeishuWSAuthenticated`。新平台 resolver 严格用 `(provider=feishu, issuer=BotChannelID, subject=SenderOpenID)` 查身份，但自身不检查 provenance。R14 实施必须只对 `NormalizeIngressProvenance` 认可的可信来源读写 K；不可信来源失败关闭。仅 `ChatPersonal` 纳入 R14（`internal/m8/hub.go:4048-4053`）。open_id 是身份查找输入，不是已实现的粘滞键。 |
| Console principal 与身份字段 | `internal/m8/newplat_view.go:223-244`；`internal/identity/model.go:33-45,53-58`；`internal/identity/pgschema.sql:6-25`；`internal/store/migrations/0033_console_auth.sql:1-18` | 当前身份解析到 `np_user.user_id_lc`（`UserIDLC()`），它与新平台 Console principal 同源；旧 `console_principal.id` 是另一套历史表，不应混用。`np_identity` 对 `(provider,issuer,subject)` 唯一。 |
| Agent 稳定身份与可变显示字段 | `internal/identity/model.go:135-155`；`internal/identity/pgschema.sql:45-65`；`internal/identity/service.go:286-295,416-435` | `np_agent.id` 是稳定主键；`owner_lc`、`name/name_lc`、state、owner 的 short_name 展示后缀均可能影响可见/可路由性。FullName 是 `name + "." + user.ShortName`，不是稳定键。每次使用须按 ID 核 owner、active 与当前在线连接；仅靠旧名字或 `session_endpoint` 不够。 |
| 当前在线枚举 | `internal/m8/newplat_feishu_inbound.go:21-41`；`internal/m8/hub.go:4048-4076` | 当前 helper 从 Hub 连接表按 owner 下名字枚举，再向身份库复核 Agent active；R14 实施须将粘滞保存的 `agent_id` 解析回当前 owner/name，并核对该 ID 对应连接仍在线，不能直接复用名字映射为 ID 的假设。 |
| 时间来源 | `internal/m8/console_auth.go:53-58`；`internal/m8/hub.go:119-124` | `h.now()` 默认 `time.Now()`，`NowFunc` 是测试注入点。该值可转 UTC 持久化，但生产时是系统墙钟，不是 DB 时钟或跨重启单调钟；现有代码没有粘滞专用时钟高水位/漂移检测。 |
| 消息去重与投递 | `internal/store/migrations/0001_init.sql:34-50`；`internal/model/model.go:80-96`；`internal/m8/hub.go:4201-4219,4267-4346,4357-4373` | `message` 有 `(bot_channel_id,feishu_msg_id)` 唯一约束。可靠信封可先入 `session_delivery` 持久队列再 drain；非可靠路径可直接写 WebSocket。`RouteEnvelope` 成功最多说明接受/排队或 WebSocket 写成功，不证明 helper 已注入或 Agent 已执行。粘滞库操作与现有 route 并非同一个事务。 |
| SQLite 事务与迁移 | `internal/store/sqlite.go:57-66,99-103,180-219,264-269,335-350,636-652,732-747`；`internal/store/migrations/0071_coordinate_review.sql:1-40` | Hub SQLite 启用 WAL、busy_timeout=5000ms、单连接池写者；迁移按文件名排序、内容 digest 记账，常规迁移在事务中执行 DDL 并记账，失败返回 `Migrate` 调用方。现有最新文件为 `0071_coordinate_review.sql`。Identity/Agent 权威表在 PG（`internal/identity/pgschema.sql:6-65`），并非 Hub SQLite 的外键目标。 |
| 两个短名入口 | `internal/m8/console_auth.go:922-930,1367-1378`（自备审核表单）；`internal/m8/console_auth.go:1324-1338,1367-1379`（托管生成表单）；`internal/m8/console_auth.go:1654-1682`（现有 Agent 列表handler） | 自备审核的 `enrollApprove()` 读取 `enrollName` 后直接 POST 批准；托管 `hostedCreate()` 读取 `hostedName` 后直接 POST 创建。两处当前均没有提交前重名查询。现有列表handler调 `Repo.ListConsoleAgents` 并只输出 id/session/online，不能据此假定它能权威覆盖 PG 中 active/suspended 短名。 |
| 数据库唯一兜底 | `internal/identity/pgschema.sql:63-65`；`internal/store/migrate/migrations/0100_identity_baseline.sql:74-76`；`internal/identity/pgstore.go:616-627` | PG 索引 `np_agent_owner_name_current` 按 `(owner_lc,name_lc)` 限制 active/suspended；冲突映射 `ErrNameTaken`。前置 UI 查重只改善提示，服务端该索引仍是并发最终兜底。旧 `session_endpoint(key_id,session_name)` 是另一模型（`internal/store/migrations/0003_session_endpoint.sql:1-10`），旧平台名不在此索引内。 |

以上是设计输入，不代表身份变更/并发/回滚运行格已实测。R14 当前没有稳定键、到期状态、代际或迁移表；本节只为 T-04 设计冻结提供 r48 代码证据。


## T-05 / T-06 / T-10 当前实现与测试入口

固定实现源码 `59e859e1f3bb4099e8f15da246070b549d598263`，基线仍为 r48 `1e17255f28ced042784337903914ca8c126ef4cf`。本节为第 108 条授权的当前实现补充，历史 T-01/T-04 的只读勘察边界不替代本单授权。最终整包/变异/浏览器/strict 结果见证据分支及交接记录；本节不声明独立验收或上线。

| 主题 | 当前文件:行 | 说明 |
|---|---|---|
| 可信入口与稳定身份 | `internal/m8/feishu_sticky.go:55`、`:97`、`:103`、`:154` | 只认可信个人入站；K 为当前 UserIDLC 与 bot；真实连接稳定 ID 核对，不能拿名字或 session_endpoint active 充作权威。 |
| 纯解析/匹配/候选 | `internal/m8/feishu_sticky.go:58`、`:75`、`:123`、`:126` | CRLF/LF 纯候选识别、合法上次排首、实际全名逐行井号、精确与唯一前缀。 |
| 存储事务与代际 | `internal/store/feishu_sticky.go:16`、`:25`、`:95`、`:131`、`:254` | UTC 严格边界，先意图，再既有投递，成功后 CAS；保留 source envelope ID，删除状态后代际仍由保留操作约束。 |
| 对账不改默认 | `internal/store/feishu_sticky.go:203`、`:271`；`internal/m8/feishu_sticky.go:415` | resolved 只写 operation 终态、实际 delivery ID、完成时刻；验证源信封/目标/principal 三者，既不补目标也不续 TTL/代际或重发。 |
| 失效/保留清理 | `internal/m8/feishu_sticky.go:377`、`:392`、`:423`；`internal/store/feishu_sticky.go:210`、`:230`、`:282` | 生命周期 owned worker；分钟 pending sweep、小时轮转清理、每批至多 200，pending/unknown 不按 30 天删。 |
| 队列稳定目标复核 | `internal/m8/feishu_sticky.go:469` | 仅 R14 信封受限制性目标元数据核验，普通原信封语义不变；同名新连接不能取走旧目标消息。 |
| 0072 | `internal/store/migrations/0072_feishu_agent_sticky.sql:1` | 两张 SQLite 表及索引，无旧表修改，无 PG 新表。 |
| T-10 服务端与路由 | `internal/m8/console_agent_name.go:14`；`cmd/workpulse/console_routes.go:51` | GET 窄 boolean；Identity PG；登录态 owner、无 owner 输入；新平台才注册。 |
| T-10 两入口 | `internal/m8/console_auth.go:923`、`:943`、`:1351` | 查重失败或 active/suspended 冲突都不提交；旧平台开关在 `:266`/`:726`。 |
| Go 单元与 Hub 场景 | `internal/m8/feishu_sticky_unit_test.go:10`、`:25`；`internal/m8/feishu_sticky_test.go:176` | S001～S022、错误注入、重启、并发、稳定身份，以及迟到对账晚于新选择的专格。 |
| PG / T-10 授权与索引 | `internal/m8/console_agent_name_test.go:16` | 本人 active/suspended、他人 owner、未登录、拒绝跨 owner 输入、旧平台名排除、并发唯一兜底；真实隔离 PG。 |
| 旧 Hub 与回滚 | `internal/store/feishu_sticky_compat_test.go:31`、`:93` | `R14_OLD_HUB_BINARY` 指精确 r48 的隔离测试产物；缺该外部夹具会明确 Skip，正式本单门须先构建并提供。 |
| 浏览器 / 总入口登记 | `view-service/r14-sticky-browser-regression.js:1`；`tools/run-browser-regressions.py:25` | 实际 Console handler 页面、localhost API mocks、两浏览器渠道与两入口变异；S018 仅 mock 文本，不是 T-08。 |

组长补充裁定（2026-11-02T11:44:34Z）：迟到 durable 对账只结案，不改当前合法绑定、TTL 或代际；无证据保持 unknown。已写入 design 对账一节及上述接口实现，另配迟到 A 写回目标的语义变异。构建使用锁定 build venv、普通 dev 用户、短 HOME/TMPDIR 和环境白名单；不继承 SH/DINGWEI/DW/WP/PG DSN。实际版本号由既有构建注入，本单不改 VERSION 或写页面版本字面量。
