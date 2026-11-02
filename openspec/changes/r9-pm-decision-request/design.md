# Design

## Context

新能力主规格/context核r46现状：已有飞书私聊发送/可信入站构件、旧Spec澄清和R8 PMResponse输入，缺新Git决策请求服务及真实提供方评论接线。R10原提案已独立交审；本次S-R48-PM-WRITEBACK在同分支对R10/R9各自目录回写PM，仍不实现。

主规格/context保留r46最小源码基线，本轮按范围不改主目录；其中当时PM待定/完整MR权限前置的旧描述，以本变更㉒已定表及首期后置边界为准，不把源码基线当产品已实施。

## Goals / Non-Goals

- 目标：将11月计划第3节R9五条落实为R9-001～009的可测协议，保证双渠道、明确回应、原子去重、内容失效重发和不自动放行。
- 不实现、不发真实私聊、不启群通知、Slack或MR写权限；不把R1风险提醒变成新审批门、不新增七阶段以外的生命周期、不代替R7委员会确认。

## Decisions

[业主裁定rulings第㉒条](https://git.code.tencent.com/wego-ai/dingwei/blob/0659d208ce731b73206b06d587c3814649ae11d2/docs/rulings/S-TEAM-MIGRATE-HOSTED-20261017.md)：业主workpulse对话，Hub收信2026-11-01T23:07:47Z，delivery dw-dlv_f5170d3c5deb6106833dc76e7c36db1e；对“r48那11项是否全部按建议定”答原话“ok”。本单S-R48-PM-WRITEBACK只回写已定值；不是实现、apply/archive、迁移、预发或发布授权。 R9-PM01～03逐项映射见proposal；平台记录回应与MR后置是保存位置选择，不取消既有飞书/Console双渠道请求。

### 1. 固定PM点集合与请求身份（R9-001）

从R5读取授权仓库/变更/完整C的proposal，完整解析「待 PM 决策」节中的固定编号及问题、选项、建议。PM的权威身份不是 `np_user.type`（它只有platform_admin/manager/member），也不是Git作者、proposal自由文本或旧 `console_principal.id`。R9请求创建时须从持久责任映射取唯一 `pm_user_id_lc`，其值是 `np_user.user_id_lc`；该映射逐仓库/变更/point配置，且请求记录对该值和mapping generation做不可变快照。多人事项拆成不同点，每个point仅一人；映射缺失/多值/用户非active就拒绝建请求，不发送。

身份对应链为：Console登录session → `identity.User.UserIDLC()`/`np_user.user_id_lc`；飞书回复则用 `(provider='feishu',issuer=BotChannelID,subject=SenderOpenID)` 解析 `np_identity.user_id_lc`，必须等于请求快照 `pm_user_id_lc` 且用户active。全局PM角色不存在；“PM”角色来自R9责任映射中的每point指派，不是新用户类型。现有 `spec_clarification_policy.pm_user` 是Console Spec澄清策略，不自动成为OpenSpec R9的映射来源。提案解析仍必须完整，代码块/HTML伪点不算；重复编号、缺必填、读取截断或未知集合阻止“全部已答/零项”判断。无点必须是完整读取且明确空集合，不从网络失败推零。

基线没有R9按GitHub仓库/变更/PM点存储的责任映射表。因此0074新增 `r9_pm_responsibility` 为唯一权威来源，初始为空。已定（组长 2026-11-02）：仅active platform_admin可通过新增管理员接口设置或删除映射；复用 `RequireConsolePlatformAdmin`，按主键单条查、改、删，写操作记审计；不从Git作者、Owner、Console Manager或Spec绑定自动导入。建议接口为 `/api/console/openspec-pm-responsibility`，按 `(provider,instance,repository_id,change_directory,point_id)` 单条GET、PUT、DELETE，不提供全量列表；每个请求要求Console登录，写操作要求同源并使用JSON，与既有绑定管理接口模式一致。PUT校验目标 `np_user.user_id_lc` 当前active；DELETE写入未配置墓碑，不删除代际。每次set/delete在一个事务内锁定主键、令 `mapping_generation` 加1、追加 `r9_pm_audit` 的操作者 `set_by_user_id_lc` 与UTC时刻；并将该point所有在途旧代际请求标记superseded。设置新PM后，由正常R9请求生成流程按新映射创建新代际；删除或未配置时不发请求，状态明确显示「未配置 PM」，变更停在SPEC_REVIEW，不自动挑人。管理员API权限格必须覆盖active platform_admin允许、manager/member拒绝、未登录401/跳转，并对移除管理员guard、允许非管理员写入的变异判红。已定（组长 2026-11-02）：首期仅接口，不做管理页面，页面另立项。该接口不改变飞书私聊身份链；在途旧PM回复因request/mapping generation过期只记late。证据：`internal/m8/newplat_view.go:223-244`；`internal/m8/newplat_feishu_inbound.go:12-18`；`internal/identity/pgschema.sql:6-25`；现有逐Spec澄清PM字段 `internal/store/migrations/0061_spec_clarification_thread.sql:3-14`；旧principal独立表 `internal/store/migrations/0033_console_auth.sql:1-18`。

请求逻辑身份为提供方/实例/稳定仓库ID/变更目录/C/point_id/generation，另保留MR稳定ID及固定C来源链接、负责PM、内容指纹（问题、选项、建议和负责PM的规范结构）。平台保存必要决策摘要和回应，不复制整个仓库文档/源码为上下文。请求代际只代表该点/版本的问答轮次，不是R8阶段。ref仅选版与监测，不代替完整C。

### 2. 同一次请求并行发两渠道（R9-002、008）

在同一持久事务创建Console待办和既有飞书私聊outbox，由两路独立投递，不能等PM答完一边再发另一边。健康隔离夹具候选同事务入队、两路首次投递均≤5秒、首投时差≤1秒，真实容量/限速待R9-A02核实；这些是后续测试值，不冒称现网SLO。飞书目标只能是配置的既有BotChannelID及该PM的private open_id，不按展示名猜，不改用群聊；摘要含问题、选项、建议及固定commit的MR链接。

Console和私聊共享同一请求身份，关联消息ID/稳定RequestID、负责PM与代际。若另一渠道尚未发送而有效回应已到，原子关闭其待办并取消未投递请求，避免完成后又补发旧请求。某路失败记录partial/failed，不装成两路成功，也不跳过PM等待；有权PM仍可经已达渠道明确回应并关闭另一待办。

### 3. 明确回应、身份与关联（R9-003）

Console用受认证PM主体和显式枚举“同意/不同意/需修改”，附意见可选；服务端从登录session取 `np_user.user_id_lc`，必须等于request的PM快照并重核active状态及当前仓库read。私聊沿用既有 `clarificationReplyAuthority` 校验：native Feishu WS authenticated provenance、配置BotChannelID对应AppID与tenant、authenticated metadata、个人会话、SenderOpenID→np_identity→active np_user映射。再以Feishu `ParentID` 精确查R9 outbox已发送/已对账的 `provider_message_id`，其request_id和generation必须仍为当前待答代际；只接收原始用户消息ID，转发/群聊/非认证Webhook不接受。持久化事件唯一键后再核当前request generation、责任PM、仓库权限；代际已过期的旧提醒回复只记late，不产生回应事实。客户端、正文、昵称和消息转发均不能自报发送者或request。

证据：Feishu WS事件形成native `IngressFeishuWSAuthenticated`及reply metadata `internal/feishu/feishu.go:444-485`；统一authority检查 `internal/m8/spec_clarification_reply.go:41-59`；持久通知与原消息映射 `internal/store/migrations/0062_spec_clarification_transport.sql:11-23`；处理时重新映射active身份并匹配通知PM/issuer/subject `internal/m8/spec_clarification_reply.go:186-217`。这些是复用基线，不代表R9已接线。

“收到”“看看”“可以研究”“同意吗？”、表情/沉默不算；自由文本有歧义或前后意见矛盾须澄清。私聊明确命令只接受独立“同意”“不同意”“需修改”或该枚举加分隔后的附言；不能用包含词或LLM猜测。若问题含多方案，摘要需明确“同意”对应哪个建议选项；无法明确选项含义时不采纳笼统同意。R9只记录明确意见，不把不同意/需修改写成同意，不自动推进SPEC_APPROVED。

### 4. 最先有效回应与关闭另一渠道（R9-004、008）

两路有效性核验后通过同一持久序列/事务竞争请求代际，只采纳平台首先接受的有效回应；采用服务端接收序列及UTC，不用提供方时间戳/客户端时钟决定胜负。绑定PM、C、point、代际、渠道、意见与依据后，原子关闭两路逻辑待办/后续催办，后到回应仅追加迟到/已处理记录，不能覆盖第一次意见。相同渠道事件/operation重试返回原结果，不增加决策事实。

飞书原私聊历史消息通常仍保留；“关闭另一渠道”是关闭平台所管待办、停止催办、旧关联按钮/回信不再改变决定，不声称撤回已送达消息或依赖未核的飞书任务关闭/编辑API。若需要私聊完成提示，用原通道可靠outbox发一份已处理通知；物理提示失败不复活逻辑待办，Console可见真实投递状态。并发回应与版本变化在提交边界重核，失效/错误PM不得抢到首份有效回应。

### 5. 内容修改使旧回应失效并重发（R9-005）

当显式目标C2下同点问题/选项/建议/负责PM有变化，旧回应追加失效依据并显示针对旧版本，原C/意见永久保留；生成新代际并向两渠道重发。旧消息回信不能回答新代际。完整比较失败标待核并保持等待，不默认内容没变。

不涉及该点的无关变更不伪造内容变化；原回应仍只绑定原C，不能自动复制/改绑C2供R8使用。若当前治理目标显式选为C2，即使点内容相同，仍需为C2建立明确回应，保留其与原C的关系；不能把旧SHA意见当同SHA批准。仅ref移动不静默改变用户正在处理的目标，需显示目标与当前head差异。需求需依意见修改时由执行者经Git写回规格走审阅，本能力不自动编辑proposal或tasks。

### 6. 超时、催办与SPEC_REVIEW守卫（R9-006）

R9-PM02已按rulings㉒定首轮24小时未回应标超时，之后每6小时催办，超时只停住、不自动放行。截止锚定服务端首次请求时刻；不明确回应、投递失败、重启/重试不能重置截止。每点/代际/渠道/催办周期只一份幂等outbox，不补发重启期间每个错过周期造成洪水。任何超时只加超时标记和提醒，不生成同意、默认选项、委员会确认或状态推进。

当前PM点有缺失/未答/含糊/旧版/待核时，供R8消费的事实必须完整、Valid不伪造，变更停留SPEC_REVIEW；R9自己不写SPEC_APPROVED。明确不同意/需修改也不自动推进，保留真实意见交现有MR/规格流程处理，不在本单偷偷改R8已定义守卫。

### 7. 平台与MR写回及权限前置（R9-007、009）

有效回应先在平台持久化，绑定仓库、变更、C、point、内容指纹/代际、可信PM、渠道、服务端时刻、明确意见/附言与关联证据，保留原记录和失效/迟到链。显示平台记录与MR镜像两个独立结果；平台成功不能显示MR已写，评论成功也不能替代平台记录。

完整MR写回需提供方明确授权的最小comment-write权限、受控服务主体/凭据管理位置和受托PM/仓库动作权限，不复用R5 read接口假装能写。对正确稳定MR身份及目标C写一份意见摘要/平台记录链接，含去重operation；核回执comment_id/链接与C才标成功。写前失权或结果未知标failed/unknown，先按operation查询核对，不用新ID盲POST、不把失败改平台意见。

R9-PM01已按rulings㉒选择原A：首期只在平台记录业主回应，标mr_writeback=postponed、MR未完成；MR写回后置，不新申请写权限。原B及comment-write/回执约束保留为后置阶段的边界，不作为当前A阶段的执行项或新权限申请依据；MR功能将来另派、另授权。当前A批准不等于MR已写或双渠道已验收。 缺权限不是索取真实令牌给Agent或向只读令牌加scope的授权；本单不创建或测试实际凭据。可验证平台意见可供后续R8按其既定规则读取，MR补写失败不制造另一个意见或自动放行，阶段就绪结论仍需负责人/业主判断。

### 8. 失败、重启及权限（R9-008、009）

发送、回应、关闭另一待办、催办、失效重发及MR补写均以稳定请求代际/操作键驱动持久outbox。消息回执未知先对账，同一RequestID允许提供方明确支持的幂等重试；不能因超时换ID重复通知。若无去重/查询能力，保留unknown人工核实，不借自动放行处理不确定状态。

每次Console读取/回应/审计、私聊投递与回复处理、MR补写分别核当前主体/仓库/动作；已撤权限不靠缓存或私聊旧链接恢复。审计带真实PM而非Git作者，敏感请求/凭据和整份源码不入消息或日志。重启恢复未答点、周期和关闭状态，不能重开已答待办；所有渠道故障仍不产生默认回应。

## 验收矩阵与反向控制

### 9. 持久模型、幂等键与周期恢复（R9-001～009）

新增 `internal/store/migrations/0074_pm_decision_request.sql`，在0073之后，只新增SQLite表与索引，不改旧 `pilot_question*`、outbox或spec表；迁移顺序最终由集成维护者复核。字段草案如下：

| 表 | 责任/字段及唯一约束 |
|---|---|
| `r9_pm_responsibility` | 权威PM映射：`provider,instance,repository_id,change_directory,point_id` 联合主键；可空 `pm_user_id_lc`、单调 `mapping_generation`、`set_by_user_id_lc`、`updated_at_utc`、`deleted_at_utc`。删除保留未配置墓碑以免代际归零；值只能是Identity PG active `np_user.user_id_lc`。仅active platform_admin经复用 `RequireConsolePlatformAdmin` 的单条管理接口写入。 |
| `r9_pm_request` | `request_id PRIMARY KEY`、固定仓库/变更/C/point、规范内容SHA、PM `pm_user_id_lc`和mapping generation快照、request generation、status (`open/answered/timed_out/superseded/cancelled/unknown`)、首次请求/24h截止/下次提醒/回应时间、版本号。UNIQUE `(provider,instance,repository_id,change_directory,commit_sha,point_id,content_sha256,pm_user_id_lc,generation)`；唯一活动请求由事务检查，不因新建operation绕过。只保存必要决策摘要，不存整个proposal或源码。 |
| `r9_pm_response` | append-only response ID、request/generation、principal、channel、enum意见、附言/证据引用、server received sequence/UTC、Feishu issuer/tenant/message ID或Console request ID、acceptance/late状态；UNIQUE `(issuer,tenant,message_id)`并UNIQUE `(request_id,generation,accepted_slot)`使当前代际只有首个有效回应获权，其余追加late。附言限长并拒绝凭据字段。 |
| `r9_pm_delivery` | 每个Console/Feishu投递/提醒一个outbox行：`operation_id`、request/generation/channel/kind/period_index、recipient identity、state、provider message ID、attempt/receipt times、reason；UNIQUE `(request_id,generation,channel,kind,period_index)`。有效回应关闭尚未发送的另一渠道行并停止后续周期；已送达消息不声称可撤回。 |
| `r9_pm_inbound_key` | Feishu native事件去重键 `(issuer,tenant,key_kind,event_or_message_id)`主键、payload SHA、request/generation、received time/处理状态；同ID异内容冲突隔离。Console command在delivery/request表用稳定 `operation_id` 幂等。 |
| `r9_pm_audit` | append-only身份、请求坐标、mapping/request generation、set/delete动作、操作者 `set_by_user_id_lc`、UTC及结果；不记凭据或完整proposal。 |

责任映射管理接口只提供按复合主键的单条GET/PUT/DELETE，不提供全量列表；每次set/delete在一个SQLite事务内CAS递增mapping_generation、写审计并使旧映射代际的在途请求superseded。初始映射为空；缺映射时可见「未配置 PM」且停在SPEC_REVIEW，不自动挑选。首期不做管理页面。首发创建request、Console待办和私聊outbox须同一SQLite事务；每个发送用持久operation_id，回执未知保留unknown并用同ID查证，不更换ID盲重发。PM回应落库、第一有效回应CAS、关闭对侧待办、停止提醒在一个事务完成。源事件message_id/event_id至少保留24小时的去重tombstone；设计建议保留至请求审计清理期结束，不能因24小时后到期删除而允许旧消息重放。语义级重复请求采用稳定dedupe SHA `(repo/change/C/point/contentSHA/pm_user)`：有open请求时永远返回该请求；终态后24小时内重试返回原结果；24小时后仅由新的明确操作创建下一代。内容/PM变更生成新generation，不复用旧回应。

Hub当前任务周期器每5秒唤醒且重启后从持久outbox继续扫描；R9采用独立持久due字段而非在内存维护deadline。R9 scheduler每60秒扫描 `status IN ('open','timed_out') AND next_reminder_at_utc<=now_utc`，用Hub UTC作为唯一时钟。首次请求时持久写 `due_at=created_at+24h`、`next_reminder=due_at`；首轮到期仍无有效回应时标记 `timed_out`并保持等待状态，生成period 0提醒；此后每6小时更新下次到期并按 `period_index`插入双渠道提醒，唯一约束防重复。重启从due字段续跑；宕机跨过多个周期时只建当前到期slot，不回补多条提醒洪水，保留明确missed_periods统计。成功或待对账回应后不再产生新slot；未知投递状态保留并单路对账，不新造逻辑请求。每个请求、channel、generation、reminder period只一条业务outbox。

**PM02的24小时首轮超时/催办与“24小时去重”是两种时钟语义**：前者从服务端首次请求起算；后者是上述request内容指纹的重复抑制窗，不重置首次请求/提醒截止。实现不得把同一个重复API调用当作新请求而重新获得24小时，亦不得将去重窗当成回应超时。证据：现有Hub循环每5秒 `internal/m8/console_task_flow.go:438-460`；旧任务outbox唯一键/重启扫描 `internal/store/migrations/0055_console_task_flow_r32.sql:18-24`、`internal/store/console_task_flow.go:84-93,376-394`；现有单次提醒唯一索引和outbox `internal/store/migrations/0059_pilot_questions.sql:46-56`；已有请求/事件键 `internal/store/migrations/0059_pilot_questions.sql:36-44`。

当前主key结构按文档提案仍属草案；0073/0074不得假装现有 R5/GitHub、飞书或PM角色能力已实现。

以下均未执行；用固定C/C2假仓库适配、假私聊通道/可信入站夹具、隔离DB和可控UTC，不调用真实飞书/MR或读取真实凭据。每行正反两格，撤保护必须红在对应断言再恢复绿；并发/重启/去重随机50轮。发送时差和周期用虚拟时钟验证，不等真实24小时。

| 固定场景 | 测试层 | 正向格 | 反向格/目标变异 |
| --- | --- | --- | --- |
| R9-001-S01/S02 决策点集合 | Parser单元＋R5假provider集成 | C的proposal完整PM点/选项/建议/责任映射/MR身份可核 | 重复/伪点/缺字段/截断不得当零项；撤完整集合核验红 |
| R9-002-S01/S02 双渠道同时请求 | SQLite outbox集成＋fake Console/Feishu通道 | 一事务双outbox，健康夹具≤5秒均首投、差≤1秒 | 私聊失败显示部分投递，禁群兜底；改为等回应后才发第二路或漏一渠道必须红 |
| R9-003-S01/S02 明确回应 | HTTP权限＋authenticated inbound集成 | Console/私聊正确PM对请求代际提交三种明确意见 | “收到/同意吗/表情”、伪PM、群/跨应用/旧代际不计；撤provenance/PM/CAS核验必须红 |
| R9-004-S01/S02 最先有效 | SQLite并发集成＋`-race`，50轮 | 两路并发只有最先有效一份，另一待办关闭/停催办 | 后到相反意见不覆盖；撤首回应CAS/关闭outbox必须红；不以历史私聊未删除判失败 |
| R9-005-S01/S02 内容改动 | Fake R5 provider＋request generation集成 | 改问题/选项/建议/负责PM后旧回应失效且两路新代际重发 | 旧代际迟到回应/比较失败不能放行；撤指纹/代际核验必须红；无关变更不误报且不改绑SHA |
| R9-006-S01/S02 超时催办 | Fake clock＋scheduler/outbox重启集成 | 24小时无明确答复仍SPEC_REVIEW，后每6小时每渠道至多一催办 | 沉默/含糊/投递失败不默认同意；撤阻塞/slot唯一约束必须红 |
| R9-007-S01/S02 写回权限 | 平台store集成＋隔离只读provider | A只平台且MR后置；不请求或模拟写入（B仍属后置） | 只读/失权/未知不得伪称MR成功，A明确显示未完成；撤阶段标识必须红 |
| R9-008-S01/S02 重启与重复 | SQLite恢复＋inbound/outbox并发race，50轮 | 相同事件/operation/周期重试只一事实，重启已答仍关闭 | 丢回执不换ID重发、已关闭不重开；撤持久去重必须红 |
| R9-009-S01/S02 审计、ACL与责任映射管理 | SQLite审计＋管理员HTTP权限＋Feishu authority集成 | active platform_admin可按主键查/设/删，set/delete递增generation、留审计；指定PM/渠道/C/时刻/意见与失效历史可核 | manager/member及未登录不得读写（401/跳转或403）；撤管理员guard的变异须红；撤仓库read后Console/私聊旧关联拒绝，把作者当PM必须红 |

矩阵中的“A未批”仅是隔离撤除批准守卫的反向格，B评论/回执格只保留后置边界验证；不表示本次A仍待批，也不授权首期申请写权限或真实评论。

## Risks / Trade-offs

- 双渠道并行不等于网络同时到达，故分别记录接收/投递事实，未知不冒成功。
- 私聊历史不能当可撤回待办，平台关闭与提供方可见提示分开，旧回信不能复活请求。
- PM01阶段取舍已由业主㉒批准，首期不因MR只读权限缺口新申请写权限；平台阶段仍须真实双渠道/身份验收，不能冒称MR完成。
- R8现有接口接受三种明确意见，R9不等于同意审批、更不会自行修改R8；若业主另定不同意/需修改的状态处置，须独立写回相应规格再实现。

## Migration Plan

先严格校验并冻结R9独立交审，迁移草案0074仅新建R9表和索引；PM01～03已按㉒定值。责任映射由0074表作为唯一权威来源，首期由active platform_admin经管理员接口维护；页面不在首期。实现后同步context及逐格证据/反向读数、race/50轮和仓库门，交独立验收。真实私聊闭环、评论写入、预发/生产均另授权；本提案不发送实际PM请求、不补scope、不部署、不archive/apply。

## Open Questions

见proposal；PM01～03已定，无编号不匹配；首期MR写回后置，不新申请写权限。R9-B01～03已完成基线事实盘点，细节与文件:行见context.md：Feishu身份与新平台用户有存储映射、每Spec PM绑定及可信私聊澄清回复校验存在，但没有全局PM角色；既有可靠outbox/事件去重仅覆盖已有流程，提醒周期不同且没有R9决策请求的专属闭环。已定（组长 2026-11-02）：R9 responsibility mapping初始为空，由active platform_admin经受保护的单条管理员接口维护；不导入Git作者、Owner、Console Manager或Spec绑定。映射缺失时显示「未配置 PM」、不发请求并停在SPEC_REVIEW；修改递增mapping_generation并使旧在途代际失效。首期仅接口，不做页面。全仓strict只是结构可校验，不能宣称真实双渠道闭环已完成。

## 基线技术前置核实（r48 `1e17255f28ced042784337903914ca8c126ef4cf`）

事实分类与证据见本变更 `context.md` 的B01～B03矩阵。三项均为「部分具备」：用户与Feishu open_id可通过新平台身份表映射，PM是逐Spec配置而非全局用户角色；既有可信Feishu私聊回复核验可验证认证来源、个人会话、应用/租户、PM当前身份与原通知关联，但只用于澄清回复；持久outbox、唯一事件/请求键和5秒轮询器已用于旧任务/澄清流程，R9所需的24小时首催及每6小时周期键和状态机尚未实现。R9-PM01已定只在平台记录、MR评论后置；这不改变本次对既有只读/写回边界的核实，也不授予真实飞书操作。
