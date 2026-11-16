# Design

## 授权与原态

历史提案固定代码2e1e4c9d，分支基d2cc47c7；与S-FLAP的现场证据分开。2026-11-16T16:05:24Z组长依据业主16:03:17Z“R60 --- OK，可发”（㊿-补123，2f5560b7）派单实现①；本分支为coder56solzzc/r60-send-lost，尚未交审或部署。当前发送broker只有内存pending，2秒内未获可核Hub持久回执即unknown；未知后正文没有普通主动outbox承担恢复。HTTP/CLI模型投递未知与WS发往Hub未知是两个边界，不可互换。

## 决策1：先接管责任，再允许发送（SL-001～003）

IPC服务生命周期从WS一代解绑；连接尚未建立时也能接收合法本地意图。caller在第一次IPC写出前固定request_id并保留，用同一request_id查询/续传；helper原子映射成一次生成的source_message_id、目标稳定ID与完整原始正文/语义meta摘要。先提交本地发送账本，后尝试网络；只有本地提交成功才能报告accepted_by=local_helper、phase=local_queued。本地提交失败报告phase=rejected、accepted_by=none、delivery_id缺、reason=local_queue_unavailable或local_queue_full、retryable=true（仅同意图）；校验/授权明确永久错误retryable=false。具体状态、错误码均在SL-001/003/006/007定义。

进程崩溃后恢复pending；不因IPC请求断开取消已提交意图。IPC请求结果不明时caller先按原request_id查询；不新建业务ID。实现前须审计CLI caller侧ID保存位置及IPC认证，同一caller请求ID与不同正文冲突不得共用已接受结果。

## 决策2：必须同时有Hub原子幂等（SL-004～005）

现有claimEnvelope是内存表；普通deliveryID随机。重复同env.ID在进程内被吞掉但不补原回执，Hub重启后还可能新入队，所以现状不能支撑自动重送。

拟议Hub唯一键为认证sender_agent_id＋source_message_id，记录稳定target_agent_id、正文及业务meta的摘要、原delivery_id/处理裁决；nonce、连接号、回执时间等每次传输字段不入语义摘要。幂等记录与首次session_delivery插入在同一SQLite事务完成；成功回执在提交之后。已存在且语义相同：返回原delivery_id/持久状态及本次nonce，不新增收件投递；语义不同返回idempotency_conflict（永久拒绝，不覆写旧意图）。不能靠查JSON后再insert的两步操作防并发。正文AMR在hub.go:1460–1464先于普通RouteEnvelope，可能先向其他目标分发（amr.go:34–89）；因此只保护最终入队并不能覆盖全部副作用。首批推荐只恢复单目标无正文AMR扇出的意图；尚未有全扇出幂等方案时必须在任何业务写出前明确不支持，不可删标记改成literal偷换语义。若PM要求首批覆盖扇出，必须另评审父意图与每个派生目标的持久唯一关系与原裁决恢复，不能声称现有内存去重足够。

每次提交/查状态先验证认证sender及稳定目标当前权限；目标同名替换不能继承旧投递。旧记录的回执/状态不得泄露给无权限的新sender或目标。原目标失效或退组后，可返回受限拒绝并让本地意图可见终结；不改发同名替代。应用内存去重快路径必须接入持久回执恢复，不能先静默return吞掉重复。

持久唯一栅栏至少覆盖全部可能重放窗口；栅栏或原delivery清理后，旧source_message_id不能当新请求再插入。可用受保留的轻量终态栅栏或签发有效期策略拒绝过窗重放，方案另评审。不能让自动重试无限超过保留期；本单不清库。

## 决策3：代际发送与明确恢复边界（SL-005～007）

一个outbox只由一个有效writer持租约消费。每次尝试绑定当前WS代际、等待能力协商；失效代际不能继续产生新业务发送。await ws.send成功只代表本地协议写操作成功，不代表Hub提交。写前失败/连接未就绪保留local_queued；写后无回执保留phase=unknown及同source_message_id，不丢正文，不改判永久失败。新连接协商支持持久幂等后，outbox负责人按同ID重送；这是入队提交恢复，不是对目标CLI发turn/start，也不生成新的业务请求。

新Hub先上线幂等能力，helper再启用队列恢复；新helper对旧Hub不得自动重放未知。旧Hub/不支持路径在写前明确phase=rejected、reason=outbound_recovery_unsupported；已可能提交的存量unknown保留原样，只读人工核验，不因升级批量补送旧未知。无可靠目标能力的direct/legacy路径不假借hub_durable_queued。

候选退避1到30秒加抖动、有限租约、并行度与每席容量均需测试后定；SL-007要求有上界和可观测终局。结果明确区分local_queued、hub_durable_queued、unknown、rejected；不把本地接收算成目标已读/已执行。sent兼容字段若保留，不能再独自驱动打印成功或退出0。

## 决策4：规模核算（SL-008）

只记录sender/target稳定ID、request_id、source_message_id、阶段、UTC、部署指纹、reason、回执delivery_id与序号；不记录正文、env、key、完整信封或turn值。采样指标不承担业务恢复。7天估算以发端已创建/接管的唯一意图为分母，与Hub的结构化source_message_id/原env.ID做关联，正文提及排除。保留确认已入Hub、明确未提交、结果未知/日志不全四类，另列重复提交，不能以Hub没有的行反推缺失总量。

跨机时钟/保存期/身份变更/已清理或归档库缺失时，只能列未匹配候选；不得将它们全记丢失。恢复与发布前先独立复核协议、故障注入和容量，当前提案不声称exactly-once模型执行。


## R60 实现映射与待复核定值

SL-001/002/006：CLI在首次IPC写出前以私有`outbound-caller.db`固定请求号及语义摘要；`--request-id`续用原号，`--status`只查不发。MCP `send_message`增加可选request_id，`dw_send_status`按原号只查本地账本。结构化accepted退出0、unknown退出2、definitive_failure退出1；MCP unknown/rejected为isError。本地local_queued只表示持久责任，绝不表示收件模型已执行。旧版仅sent回执归unknown。

SL-003/007：普通主动账本为私有`inbox_db.outbound.db`（0600，SQLite WAL/FULL），正文与映射一次提交；IPC独立于WS重连循环。单进程flock加逐行30秒租约；每次恢复最多等待10秒回执，退避1～30秒带抖动，最多64次/24小时。候选边界：待确认1024条/正文与meta共32MiB，全量轻量请求栅栏65536条。超期已尝试者保留unknown及正文；明确未写出者rejected。未确认项不作缓存淘汰。调用前存储失败可证回滚为local_queue_unavailable/可用原号重试；COMMIT本身结果不明须unknown后按原号查询。

SL-004：迁移0077新增outbound_intent，以认证sender/source复合唯一；裁决与首次session_delivery同一SQLite事务提交。重复先按原稳定身份现核，再恢复原delivery/本次nonce；最终并发由事务唯一键裁决，绕开内存claim吞回执。Hub候选栅栏边界每sender4096/全局65536；没有自动GC，不删原账本或栅栏；达界停止新受理而不放开历史ID。此为安全收口，存在长期容量耗尽的可用性代价，须独立复核并由组长确定上线定值/后续受控维护，不冒称无限期吞吐。

SL-005：认证capability提供sender稳定ID和最多1024个当前可见目标的ID/全名/别名，helper持久保存供离线受理；未知目标无绑定时target_identity_unavailable，不把裸名猜成ID。原意图固定targetID，重连不换目标；提交与物理发送前再次核身份、名称、用户状态、编组权限。权限检查沿用现有Identity服务，不宣称新增跨SQLite/PG分布式事务或撤权与每个字节写出的原子性。每代capability nonce及socket对象绑定回执，旧Hub不写body。首批为专属凭据普通根Agent单目标可靠队列；飞书/外部/子Agent、实际正文AMR扇出/语法通知明确unsupported，不移除标记改变语义。desktop专用reply路径保持独立。capability目标目录是有界快照，离线可漏新目标、不能凭同名替换恢复旧目标。

SL-008：helper私有outbound_event审计只含source/sender/target稳定ID、attempt、phase、固定reason、delivery及版本，无正文/完整信封/凭据；不在async控制路径同步flush外管道，避免新增事件循环反压；本地账本与Hub裁决可按sender/source对账。caller私有摘要与source映射不自动上传。字段/路径和门证据见context与REPORT；未实跑门不得标绿。


R60查询补充（SL-002/005/006）：request_not_found只表示本地当前快照无映射，不证明另一个IPC提交已经停止，故仍unknown/保留同request_id。发送协议能力与目标目录可用性分开协商；临时目录错误不得伪装成旧Hub。目录不可用时只保留同一认证sender的既有私有绑定；换sender清目录并显式sender_identity_changed，不把旧意图改投新身份。目录每60秒或未知目标请求后只刷新元数据，不触发业务重发；没有绑定的新目标先target_identity_unavailable/可用原号重试。MCP调用方账本本身写失败在IPC写出前明确拒绝，查询传输失败仍返回原查询号。首轮候选canonical的两个profile均因旧工具数量断言26失败（新增dw_send_status后应27），原rc1记录保留；修正数量断言不放松工具权限或语义。


诊断保留（SL-007/008）：新helper审计表最多保留8192个最近恢复阶段事件；`dw_send_status`最多返回该意图最近4条，并同时给审计最早序号/时刻。旧诊断淘汰不删除正文、request/source映射或Hub幂等栅栏；7天统计必须说明这段有限覆盖，不能推定完整日志。attempt计数是恢复领取，不代表实际网络帧或模型执行次数。临时存储错误只暂停writer/让查询返回unknown，不在async循环同步打印外管道；恢复后仍同source ID。v1范围校验前置于terminal/session_event/PM结果等业务分派，非法/false恢复标记不允许落回旧发送路径。


私有账本主体隔离（SL-002/005）：即使同一私有卷更换了Agent凭据，新认证sender也不能用旧request_id取回另一个sender的成功回执/目标元数据；提交与查询都按稳定sender过滤，能力已更新但目录worker尚未运行的窗口同样拒绝，不等待下一轮目录刷新才生效。私有状态卷不得在不同Agent间共享；离线受理仅使用该私有卷此前认证的绑定，网络写前仍以当前凭据和稳定ID现核。

回滚约束：Hub新账本及其未终结投递存在时，不可仅切回缺少稳定目标守卫的旧Hub二进制后继续派发；发布/回滚须先停新writer并由组长核定排空或可见取消待发的新协议投递，保留幂等栅栏。旧helper不会导入历史unknown；本件不自行操作任何现网队列或清库。


SL-005永久失败可见性：实际写前的outbound_permission_denied进入既有持久HUB失败通知，通用原因文案不复制业务正文、reply_policy保持no_reply。通知本身以原裁决中的sender稳定ID现核，不向同名新sender投影；原目标已失效不妨碍健康原sender收到这条HUB状态。未知存储读错仍 queued重试，不转永久拒绝。外部席位PG往返长度夹具的旧IPC桥只返回ok，CLI必须unknown退出2；其SQL实到达仍由原用例独立验证，不伪造新协议成功回执。本轮全包初轮rc1为该旧夹具的退出码预期，已补负面判据，未改外部席位产品路径。

0077账本DDL接入现有exact-DDL预检与补账：丢迁移账本但结构完整只补账，不重放/清空幂等fence；缺索引或定义改变拒启。显式target#owner仅按认证目录发布的完整别名匹配，不能丢弃#owner后按同名重定向。旧接收端未协商持久ACK时拒绝outbound_target_unsupported，禁止退回无持久责任的直接写；暂时存储/身份错误仍unknown。hosted/受限MCP的正常运行入口同样是跨WS的run_forever监听，不用旧socket sent布尔充当新回执。

Hub容量的已知原子拒绝（ErrOutboundCapacity）返回rejected/hub_outbound_capacity而非unknown。4096/65536达到上界时仍先查原fence并恢复旧回执；新caller不插入fence/收方行。没有自动清库，长期达到上界会拒绝新接纳，这是候选可用性取舍，须终审，不以复用ID或删除未知业务规避。

### 当前拒绝不消除此前未知接纳

交件前反例证明：Hub首次已提交/回执丢失，原ID恢复时恰遇权限撤销，后一次rejected仅拒绝当前操作，不能证明历史未接纳。helper在首次明确拒绝时可记rejected；从第二次写出起收到拒绝则保留unknown/outbound_rejected_after_uncertainty、原ID与正文，仍按同ID的有限恢复窗处理，不许可caller换ID盲重发。Hub仍先核当前权限，拒绝时不泄露原回执/正文。SL-S011据此细化，新增SL-S016；不改变通信权限、不代目标CLI重做。私有outbox/调用号账本不得直接删除或恢复到早于已上送意图的快照；硬件损坏/人工回滚丢失账本不在“持久存储完整”保证内，须受审对账。


### 离线期限与能力协商收口（SL-S017/018）
writer每次轮询先检查24小时期限，即使完全离线也落可见recovery_expired及脱敏事件。新WS十秒未协商能力即停止新受理并将未尝试项明确拒绝；该超时只表示当前恢复能力不可用，不证明对端版本老。明确不支持/超时下的既有unknown保留原ID/正文，升级后也不批量自动恢复；本轮不提供解除该人工核验停发状态的管理工具。已接受裁决的同请求本地查询/续查仍保留原结果，不把当前能力拒绝当历史未接管证明。

调用方不再用继承的SH_KEY_ID补接收地址，目标只能按认证目录公布的全名/别名核绑定；显式owner信息不被丢弃。会话名按平台不区分大小写口径校验回执范围，socket/nonce/稳定ID校验不放宽。上述边界在633候选全绿后通过额外反例发现，旧门仅作历史；最终门须重固定产品源。


## R60①组长容量退回修正（2026-11-16T19:52:29Z，覆盖上文永久Hub栅栏候选）

累计4096/65536且无GC会确定耗尽，原“只属候选可用性取舍”处置撤回。上文无自动GC/永存栅栏段落仅为旧ba候选历史；以下是当前合同（SL-007、SL-S014、019～021）。helper本地账本的保留与上限没有在本次顺手改变。

Hub `WP_OUTBOUND_INTENT_RETENTION` 默认168h（7天），允许Go duration格式配置，但必须严格大于helper24h恢复窗+2h余量（26h本身拒绝，26h1ns可用）。非法/负值/溢出拒启，有限错误文案不复述原始值。配置装配在启动打开SQLite后、迁移/服务启动前；错误会关闭已开DB，只记录通过校验的duration。默认未配置的测试/嵌入式store同样7天，不依赖cmd才能有安全默认。

推理前提是合规helper：outbound_queue.WINDOW=24h，step即使完全离线也expire，claim不领取超窗行；重启后先按同一持久created检查，不导入旧unknown批量补送。Hub的保留窗从本机首次接受时刻起算，通常比helper剩余恢复期更长，7天覆盖自动重送窗。最小值另加2h：运维应保持两端时钟各在UTC±15min内，创建到恢复期间的时钟变动合并按1h预留，其余1h覆盖重启调度、30s租约/退避与10s回执在途等余量。默认7天的余量远大于此。超过该时钟/回拨假设必须暂停恢复writer核时钟；不是“任意回拨也安全”的证明，24h常量变更必须协调Hub校验地板。本次Python合同回归固定WINDOW=24*3600。

保证边界：此推理确保允许的自动重放不会越过Hub保留窗；并不提供对任意手工/恶意改客户端越窗直投的全时域永久幂等。没有隐瞒新引入带签名有效期或时间戳source ID协议；如要求越窗任意请求也拒绝，需要另行设计不能随本次GC冒称。原SL-S014的绝对永久拒绝措辞按本次有时限合同收窄，所有窗内原回执、语义冲突与原子裁决保证不变。

新受理事务先查原sender/source裁决；已有行（含尚未清走的超期行）仍先恢复原回执/拒绝冲突。新意图以同一server acceptedAt/cutoff先做有界GC，再按`created_at>=cutoff`计本sender和全局窗内条数，再原子写栅栏+收件delivery。4096/65536现是窗内突发背压界，老流量不再永久占满；窗内确实满时仍可见拒绝。GC至多256行/事务，只删`created_at<cutoff`且不承担未终结投递或failure_notice稳定身份的行；原投递/通知仍活跃的旧行保留但不计窗内容量，不能为省行数放开同名替换保护。异常长期unknown可能额外占历史空间，须先收敛该原任务再清理，不能静默删除权威。删除和新受理同事务，若入队失败则GC也回滚；不删除session_delivery、正文、本地unknown、sample或任意其它账本。

0077尚未上线，经本次派单直接补`created_at,sender_id,source_message_id`索引，并把sender索引扩为sender/created_at；沿用exact-DDL部分结构拒启/丢迁移账本补账。时间戳用UTC固定9位小数文本，保留索引顺序和严格边界，时间取store接受时钟，不使用caller或queue.CreatedAt决定fence年龄。未改已发布DDL/另占迁移号；若试验库用了旧未发布0077，应另建干净测试库或受审处理，不以本说明授权清生产库。

测试口径：按“只计窗内”要求，删GC并不应被伪造为必然拒新；新增断言同时要求过期行确实删掉、每次至多256、新意图受理以及窗内原裁决保留。删清理红在过期行仍在，删仅超期条件红在窗内裁决被清，删任一窗内计数过滤才红在旧满额错误拒新。三者分开归因，不把编译错误算业务红。

### 集成观测：保留期外未终结栅栏（组长2026-11-16T20:50:50Z非阻塞意见）

栅栏总存量按同一采样时刻及当前保留期拆为窗内W、超期仍有未终结原delivery/失败通知引用的P、超期可清但尚未清的E：W受全局65536/单sender4096限制；因delivery_id唯一，P不超过仍有未终结原投递或通知引用的不同原delivery_id数量，随积压增长而没有独立固定容量上界（含admission_unknown，不因超期丢掉身份权威）；E仅在成功新受理事务中每次最多回收256条，无新受理时不会主动下降。因此不能以65536宣称全表或磁盘硬上界。集成/运维观测应分别记录W/P/E、最老超期栅栏年龄及未终结投递积压；至少报告“保留期外存量=P+E”，不得将其当窗内容量或重送/模型执行次数。清理子查询虽走created_at索引，每次新受理仍可能重扫P，256只约束删除条数，不保证扫描量或延迟上界。此处仅定义后续受审只读观测口径，本版不新增指标接口、后台清理或代码，不据此授权查询/修改现网。
