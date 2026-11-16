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

SL-008：helper恢复日志只含source/sender/target稳定ID、attempt、phase、固定reason、delivery及版本，无正文/完整信封/凭据；本地账本与Hub裁决可按sender/source对账。caller私有摘要与source映射不自动上传。字段/路径和门证据见context与REPORT；未实跑门不得标绿。
