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
- **整段候选不能原样当多选投递。** `#a.owner\n#b.owner`今天会把a当目标、`#b.owner`当body（internal/m8/hub.go:6436–6445、3988–4005）。候选本身可以整体复制，但粘贴回Hub后如何处理须PM07冻结；建议纯多行候选块只返回“请选择一行”，不选首项、不广播、不更新粘滞。
- AMR也支持legacy井号selector（internal/m8/amr.go:1548–1564），`# `、`## `等Markdown标题被排除，`#全名`不属于这类标题；输出转发/IPC正文的AMR入口是另一条路径（internal/m8/amr.go:33–46、1420–1467）。因此井号语法确有复用，不能泛称完全无冲突，更不能让系统候选回文当Agent正文进入AMR。R14只改私聊默认选择，不改AMR规则或授予群发。
- 代码侧Feishu回复是纯text：worker保留原BotChannelID与ChatType（internal/worker/schedule_worker.go:390–408），SDK把原字符串编码进`{"text":...}`并以MsgType text发送（internal/feishu/feishu.go:283–291）；回入extractText只做JSON取值和首尾TrimSpace（internal/worker/schedule_worker.go:491–500），不会主动剥掉每行`#`。这支持代码层井号往返，但未实测真实飞书桌面/手机复制、富文本粘贴和换行格式，不把代码勘察当客户端回环已通过。

## 4. 可用存储、现存状态与重启

| 现存设施 | r46 文件:行 | 能否直接代表上次对话对象 |
|---|---|---|
| Hub内存连接/同步/去重map | internal/m8/hub.go:154–171、365–387、1236–1246 | 重启重建；sessionClients用于在线、syncTargets用于流同步，不是按账号+机器人通道的粘滞记录 |
| SQLite session_endpoint | internal/store/migrations/0003_session_endpoint.sql:2–10；internal/store/sqlite.go:3730–3759、3787–3818 | 持久保存端点/last_seen_at/active/mirror_to，键是key_id+session_name；不能区分“谁在哪个机器人最后选择谁”，LastSeenAt是连接活动而非对话时钟。断线会写Active=false（internal/m8/hub.go:1267–1274）；不能拿数据库Active当重启后实时在线权威 |
| 消息/投递/控制任务历史 | internal/store/migrations/0001_init.sql:34–48；internal/m8/hub.go:1579–1595、4274–4281；internal/m8/amr.go:1058–1070 | SQLite有消息及投递信封，AMR据deliveryID恢复原信封；这是单条消息回程关联，不是可靠的owner+channel最近选择索引，也不能从最后一条mirror/系统通知推定上次对话 |
| 通用SQLite app_config/CAS | internal/store/migrations/0001_init.sql:182–186；internal/store/store.go:133–139；internal/store/sqlite.go:3625–3678 | 有持久JSON、CAS及删除能力，可作存储能力参照；没有现成R14键/状态契约。建议专用小记录，不能本单先写真实app_config或借用mirror_to |

所查入站函数/模型/仓库接口未见已接线的“账号+BotChannelID→上次Agent+过期时间”状态；当前多Agent始终提示不使用历史。不能说“现成粘滞会跨重启保留”，因为该能力尚不存在；若后续只加内存map则必随Hub重启丢失。建议PM06批准持久元数据后使用SQLite保存目标稳定ID和原绝对到期时间，恢复先重核身份/在线，不重置30分钟。没有读取或写入生产数据库。

## 新提案与实施边界

业主来源仅引用proposal中的7d376639第⑳条；PM01～08均为本次建议，未获确认。本轮只新增本变更5份Markdown，不修改上述源码/现有主规格/迁移/配置/通道。真实飞书回环、持久化与并发、群聊不受影响、AMR不误触发都列为未来验收；不apply/archive、不自行实施或发布。
