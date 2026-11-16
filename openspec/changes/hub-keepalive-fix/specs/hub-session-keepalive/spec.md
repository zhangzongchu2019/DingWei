## ADDED Requirements

### Requirement: HKF-001 协议读取不等待业务
Hub MUST 每连接只有一个协议Reader，持续消费Ping/Pong/Close；Reader及其控制回调不得同步等待viewer、存储、业务路由或worker完成。业务SHALL交给该连接有界顺序worker，活动时刻在接收边界记录；不得启动第二个Reader抢读或以无界goroutine代替队列。

#### Scenario: HKF-S001 业务等待期间Pong可读
- **WHEN** helper正常应答而某业务handler被受控阻塞超过原5秒
- **THEN** Hub仍读取Pong，健康连接不因该业务等待进入keepalive_failed

### Requirement: HKF-002 有界入队与确认语义
Hub MUST 对worker队列限制条数、字节与全局预算，满队列不得无限阻塞Reader。可回放显示丢弃须可见gap；可靠业务/ACK/session_event不得静默丢弃，无法接管须明确business_queue_overflow而非伪造持久确认或保活失败。业务顺序、身份、ACK代际及各DB提交点SHALL保留，收到WS帧/入内存队列不等于durable。

#### Scenario: HKF-S002 队列满
- **WHEN** 持续输出或慢业务填满配置预算
- **THEN** 内存不越界、Reader不无期限等待，显示gap或明确过载结束，不谎报可靠业务已接受

#### Scenario: HKF-S003 ACK事务未提交
- **WHEN** queued/injected/processed的DB事务被阻塞或失败
- **THEN** 不提前发送delivery_ack_confirm或伪造native时刻，原代次可按既有账本幂等恢复

### Requirement: HKF-003 慢viewer隔离
真实viewer SHALL 使用独立有界发送责任，取写权与网络写共用单次截止；超时/积压只终结该viewer并报告可见历史缺口，不关闭Agent或阻塞其它viewer。虚拟viewer MUST 保留不写WS行为，结构化事件既有序号/gap语义不得混为可静默丢弃文本。

#### Scenario: HKF-S004 慢viewer写或锁卡住
- **WHEN** 一个viewer不读或持有写槽超过预算
- **THEN** 该viewer在预算内被移除，Agent继续Pong、其它viewer继续，队列和goroutine不无界增长

#### Scenario: HKF-S011 Agent离线保留legacy通知
- **WHEN** Agent会话离线且Hub仍接纳清理工作，另有正常读取与慢写viewer
- **THEN** 健康legacy viewer在有界状态写后收到readonly的“会话已离线”与正常关闭码，慢viewer不阻塞Agent清理或其它viewer；失败通知保留gap，清理工作归属Hub生命周期

### Requirement: HKF-004 存储与会话写有整次预算
活动刷新、历史持久和会话写 MUST 不阻塞Reader；各worker调用须覆盖排队、锁、结果等待与I/O的取消预算，不能只限制SQL事务或只限制取得写锁之后。历史写失败保留既有gap/原ID，暂时错误不得变成永久撤权或已完成，连接换代后旧worker不得继续提交新副作用。

#### Scenario: HKF-S005 慢DB与写锁
- **WHEN** 唯一DB连接被占或session写锁持有超过保活预算
- **THEN** Reader仍可处理协议控制；业务按预算保持可定位待定/失败，不提前确认或永久误判撤权

### Requirement: HKF-005 长输出保留控制帧服务机会
系统 SHALL 对输出批处理设置显式字节界与公平调度，在审定负载/带宽内保证长输出不独占控制帧读取/发送；不得把0.12秒批窗口当字节上界。若修法需helper分块，MUST 单列实现与回归；分块保留原字符顺序及结构化事件语义。真正网络失活仍允许被检测，不能承诺任意洪泛下永不失败。

#### Scenario: HKF-S006 长输出与健康对照
- **WHEN** 注入持续输出并叠加受控慢业务，另有无慢业务的相同输出对照
- **THEN** 修复后健康控制帧在预算内通过，输出顺序/可见gap和队列界可核，不能用缩短输出或取消Ping伪装通过

### Requirement: HKF-006 保活失败可定位且脱敏
Hub MUST 对每次实际保活失败保留一条可关联session/connSeq的journal，含有效interval/timeout、实耗时、error_class及可得的Reader/worker/队列状态，未知字段明示unknown。取消/关闭/写失败/等Pong超时不得一律说成5秒Pong晚到。日志不得含正文、raw异常、headers/key/secret，不得阻塞Reader；日志出口满须有diagnostic_drop计数。成功RTT桶与失败计数语义须保持可区分。

#### Scenario: HKF-S007 超时、关闭与关停取消
- **WHEN** 分别注入等Pong超时、底层关闭及计划取消
- **THEN** 错误类别和关闭原因可区分，每失败连接至多一条对应日志；计划关停不伪报网络超时，日志不泄露正文或凭据

#### Scenario: HKF-S012 Ping在飞时计划取消
- **WHEN** 对端已实际收到Ping且不回Pong，此时触发计划取消
- **THEN** watcher退出且不增加PingFail、不记keepalive_failed关闭原因或失败journal；删除Ping返回后的取消保护时本格必须在相应断言红

#### Scenario: HKF-S013 预算诊断与年龄同口径
- **WHEN** 队列唯一项目正在处理且使用可注入时钟产生诊断
- **THEN** queue_items仍含该项目并与queue_bytes同口径，Reader/Pong/worker年龄使用同一时钟，真实elapsed仍独立按实耗时记录

### Requirement: HKF-007 保活配置明确且受限
若实现配置入口，Hub SHALL 从显式WP_SESSION_KEEPALIVE_INTERVAL/TIMEOUT取正值并启动记录有效值；未配置保留审定默认，非法/负数/溢出MUST 启动拒绝，不静默关闭保活。合法范围须校验与idle预算一致；配置和重启不构成I/O隔离修复，库内控制写上限必须单独披露。

#### Scenario: HKF-S008 配置缺省与越界
- **WHEN** 未配置、配置审定正值、或传入负数/非法值/超过idle预算组合
- **THEN** 分别使用缺省、可核有效值或拒绝启动，不把无效配置自动变成不保活

### Requirement: HKF-008 回归护栏与生命周期
实现 MUST 有慢viewer、长输出＋慢业务、满队列、ACK提交顺序、错误日志及关停回归，并逐条删除对应保护验证在相应断言转红。worker须归属连接/Hub生命周期、取消并有界join，不绕过既有总关停预算；不得为减少断连而禁Ping、虚构Pong、自动重放已入场模型请求或替代SEND-LOST持久责任。

#### Scenario: HKF-S009 删除保护
- **WHEN** 分别移除Reader隔离、viewer总截止、队列界或提交后确认保护
- **THEN** 相应慢业务/慢viewer/满队列/ACK断言红，不能依靠另一冗余护栏掩盖该支未覆盖

#### Scenario: HKF-S010 活跃连接关停
- **WHEN** 有慢viewer和在途业务时触发既有Hub关停
- **THEN** Reader/worker均按生命周期退出且未确认业务仍如实待定，不留失管goroutine、不超既有预算或借关停补造业务成功

#### Scenario: HKF-S014 关停后拒绝新viewer输出工作
- **WHEN** Hub后台组已封口却试图建立真实viewer输出责任
- **THEN** 连接关闭并标记可见gap，不保留假活跃worker或静默丢输出


### Requirement: HKF-009 控制回执写失败可观测
会话出站接受回执与投递ACK确认在网络写失败时 MUST 各自累计有限obs计数，不得撤销已提交投递或ACK，不得将计数等同投递失败；发送端 MUST 以原消息身份查询恢复，不盲重做。

#### Scenario: HKF-S015 两秒背压下回执不确定且可恢复
- **WHEN** 真实会话的写槽被占用超过两秒，已提交投递的出站回执或ACK确认写入失败
- **THEN** 对应outbound_receipt_write_fail或delivery_ack_confirm_write_fail加一，另一计数和diagnostic_drop不变，持久责任仍在；故障消除后原消息/投递身份恢复回执，成功写不增加失败计数
