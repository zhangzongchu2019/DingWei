# Design

## 固定基线与授权

只读提案基main d2cc47c7；现网截图锚329eb9f9与本件Hub核心相关文件逐字对照见证据manifest。不会动S-FLAP封存件或SEND-LOST冻结方案。目标是消除可服务负载下的协议读取饥饿，不承诺网络故障不发生或任意输出量都能实时显示。

## HKF-001～002：唯一Reader、按连接有界worker

每个session连接只保留一个调用Conn.Reader/Read的owner；它负责继续消费协议帧、固定接收时间、有限解码/入队，不同步写viewer、SQL、路由、业务回执或等业务worker完成。不能同时启动第二个Reader来偷读Pong。OnPing/OnPong只做有界内存活动记录/调度，不做DB或阻塞日志。

业务仍由同连接的顺序worker处理，保留capability→业务、每delivery/ACK generation/sequence的既有顺序；不能因为多worker吞吐让processed越过injected或新连接用旧队列副作用。touch改成有界、按连接合并的后台刷新，不在Reader等唯一SQLite连接。

仅将数据送进有界内存queue不会变为持久接受：必须保留原DB提交/身份现核/ACK2确认点与原native时间。不在enqueue时回hub_durable_queued或delivery_ack_confirm。queued/injected/processed含义不变，ACK v2断连重放仍按原账本/幂等推进。

满队列处理不是无限阻塞q<-：可回放的纯显示可按已有gap协议降载；可靠业务/ACK/session_event不可静默丢。不能立刻接管时，必须以单独business_queue_overflow（非keepalive_failed）结束接入/报告未接受及可见缺口，不伪造确认；未获持久确认的恢复责任与SEND-LOST配合。session_event会驱动AMR/终态，不得整类当可丢日志。队列按条数和字节双界、全局预算与连接代际回收，避免goroutine替代队列无界增长。

候选64项/32MiB能容纳现有单帧16MiB上限，另有全Hub256MiB预算；不是默认已定或生产SLO。过载可以被明确关闭，但不能包装成5秒keepalive失败。32MiB不是承诺每连接都同时分配。

## HKF-003～004：viewer与I/O责任隔离

每个真实legacy viewer一个有界发送worker。terminalWrite的取写权和网络write必须共用同一截止；只给conn.Write加timeout、仍无条件<-v.mu不够。viewer超时或积压时只关/移除该viewer并显示已有buffer/gap边界，不关Agent；虚拟viewer conn=nil继续不写WS。现代SSE/seq/mux已有非阻塞fanout，不把legacy修法泛化成全部View通道都同步。

SessionHistory/persist业务保留提交点、游标与失败gap；I/O有整次调用预算和取消（含入队、锁、等result，不仅SQL事务内），不能在Background上下文外层无界等待。暂时写库失败不得改判业务永久撤权/已完成。活动刷新合并不影响内存lastSeen或离线收尾。所有worker受连接/Hub生命周期登记、取消、join；不新建失管后台任务绕过既有15秒关停总预算。

## HKF-005：长输出与控制帧

0.12秒terminal批窗口不是字节上限；同一WS/TCP控制帧也不能超越已经串行化的数据帧。终端分块/发送应有明确字节界与公平让出，必要时把helper帧分块加入发布范围；协议仍按原字符顺序完整拼接，截断/丢弃须可见gap，不能改结构化事件语义。控制帧不能等待业务层大锁；sessionClient写锁/大帧写也要有预算。不可绕过websocket库帧互斥并发裸写。真实网络断开或持续拥塞仍可失败，不把所有ping fail都叫误判。

初测建议32KiB显示数据块、每64KiB/一个块让出与公平调度，仅为候选。helper20/20硬编码不等于Hub5秒容忍；不能只改helper ping_timeout指望Hub等待更久。stderr/stdout外管道可能反压，诊断不得在Reader内同步flush无界日志。

## HKF-006：失败日志与指标解释

Ping错误用errors.Is/类型分类deadline/cancelled/closed/other；不要把库的任意err都写成“Pong超过5秒”。每个实际keepalive失败连接留一条脱敏journal，包含session或稳定AgentID、connSeq、配置interval/timeout、实耗时、最近Reader/Pong活动年龄、worker阶段/耗时与队列条/字节数、error_class。取不到的字段写unknown，不编造write-vs-wait阶段。

只记录有限错误分类，不记录raw error、正文、WS URL/headers/key/凭据。日志路径不得反向阻塞Reader；若日志出口满则计diagnostic_drop并保留现有ping/close计数，而非无界缓存。主动shutdown/cancel与真实保活失败应分开统计，避免关停时ctx取消被误算网络问题。保留既有p95只含成功RTT的语义并显式说明。

## HKF-007：配置是未来改动，不是现有开关

拟新增WP_SESSION_KEEPALIVE_INTERVAL/TIMEOUT正值时长入口，经cmd装配而非测试函数。未配置仍30s/5s；候选合法范围interval10～60s、timeout1～30s，且interval+timeout<现有75s idle预算；具体范围PM待定。非法/负值/溢出启动失败，不悄然禁保活。有效值启动记脱敏摘要。新增配置需要新构建和经批准重启，不能谎称现网已有入口。

即便应用Ping ctx设15s，coder/websocket.writeControl内仍有5s控制写预算；只延长等Pong部分不能保证任意控制写等15s。代理读超时/OS TCP keepalive也不改变应用5s。配置改动是可选诊断措施，根治仍在Reader/I/O隔离。

## HKF-008：回归与独立删保护

1. 慢viewer：真实/受控socket不读或写权被占超过原5秒。Agent仍收Ping、Hub处理Pong；viewer在预算内被移除、其他viewer继续，不触发Agent keepalive_failed。单独删viewer锁等待/写预算须红于viewer回收和资源界，不能仅看Ping（Reader隔离可能掩盖冗余）。
2. 长输出：连续64×8KiB及经审定峰值，注入Touch/历史业务等待≥6秒，Reader仍消费Pong、队列有界，业务ACK不提前。删Reader解耦须红于Pong/读循环；删非阻塞入队策略须红于满队列场景。单纯健康长输出也有无注入对照，不能把注入延迟当实测现网SQL时延。
3. ACK顺序：故意阻塞事务，queued/injected/processed确认只在各自DB提交后，重复ACK/重连同代幂等，保留native时间与return source ID；删除提交后确认保护须红。
4. helper控制：如修改helper分块，删分块/公平发送保护须使受控带宽大帧场景红；未改helper不能据Hub测试宣称覆盖此支。
5. 正常/故障：空闲、半开、对端不读、取消、错误分类、超载和Hub关停各有独立判据；保活失败journal一条/连接，删除日志或误把cancel分类keepalive须红。实现尚未派，不宣称这些修复或变异已执行。

## R60② 实施选择与当前边界

候选采用64项（含正在读取与处理中的项）/32MiB（含流式读取中已收和处理中的正文）/全Hub256MiB，满即business_queue_overflow并CloseNow，不在Reader回业务ACK。Reader按8KiB scratch流式读，增量预留预算后才追加，不先完整分配16MiB帧再判队列；预算约束逻辑payload，另有每连接8KiB scratch、Go切片容量与库协议缓冲，不冒称精确RSS上界。唯一Reader由Hub lifecycle登记，连接/Hub取消均停止；HTTP handler继续充当原顺序业务worker，没有并发业务执行器。接收时记录活动与帧时间，空TS用接收时间，业务payload/native时刻不改。Reader错误会取消未处理工作；已持久裁决依原账本，未确认发信待①同ID恢复，不能把内存队列当持久接受。

真实viewer输出队列64项/1MiB、含在途正文，取槽与写共1秒截止；溢出/超时只CloseNow该viewer，HTTP退出走原授权宽限/回收，终端buffer仍是有限回放边界，不伪造完整历史。控制消息沿用有1秒总截止的terminalWrite；虚拟viewer不写。Agent会话写使用独立槽和2秒总截止，技能状态锁不再兼作网络写锁；不改变共同组/当前连接授权校验。

touch每连接只有一个合并通知与受管worker，单次1秒（仍保留原5秒写节流），没有每Pong新goroutine。历史Append在原2秒调用预算内连结果等待一起可取消；取消回待定错误，不把尚未返回的cursor算成功，提交已发生但确认丢失仍是不确定。旧业务代码中的内存锁、第三方handler和诊断仍有自己的约束，本件不保证任意第三方代码可被强制中断；Reader始终独立。

journal不经标准logger可能被外管道反压的锁：Linux用一个<=PIPE_BUF JSON行，systemd Unix socket用MSG_DONTWAIT|MSG_NOSIGNAL，pipe用新NONBLOCK fd，不改变进程原fd属性。满/不可用/非Linux/普通文件sink均记可见WS diagnostic_drop，不为诊断派无界goroutine或阻塞Reader；正常Linux pipe/socket一失败一行。计划取消不计ping_fail；成功RTT仍只收成功值。实际写控制帧/等Pong不可从库err进一步细分，固定deadline/cancelled/closed/other，不编造阶段。

新增启动入口在cmd调用ConfigureSessionKeepalive，空值30s/5s，interval10～60s、timeout1～30s且和<75s；非法/负数启动拒绝，仅日志记录已解析有效duration。旧构建没有入口，本件也未应用到生产。库内控制写约5秒上限仍在，延长等Pong不能保证任意拥塞网络。

helper仅terminal_output按UTF-8正文32768字节分块，每块前sleep(0)让出；JSON转义后的线长另含元数据与至多6倍正文转义，非“线长32KiB”。0.12秒收集窗口另有32768字符批收集界；单个驱动chunk大于界仍分块，按原字符顺序拼接。结构化session_event不分块，不能据此承诺任意事件帧/网络负载都满足实时SLO。

## qwen独立复核Q-1～Q-7修订（旧167源结论不外推）

Q-1补对端真实收到Ping并禁止Pong后再取消的竞态，不用先cancel或固定sleep假装覆盖。保护删除须红在journal/PingFail/关闭分类。Q-2恢复legacy离线状态和正常Close：先停止接受该viewer的新输出，再由每既有viewer至多一个Hub登记的viewer_offline清理工作写readonly“会话已离线”，状态写仍共用1秒取槽+I/O预算；成功后用库正常关闭码1000与session offline原因。库Close本身写关闭帧最多5秒、等回应最多5秒（还含库自身goroutine退出等待），不冒称总握手1秒；不把这些等待放回Agent清理循环。已有output worker保持连接绑定，清理在结束后cancel；丢失显示内容或状态失败标gap。Hub封口拒绝清理时不建游离goroutine，明确gap并CloseNow。生命周期总关停预算/超时原样报告，不把尚未完成握手说成清理成功；不新增每输出帧goroutine。closeTerminalLocked不再为马上关闭的旧viewer白起output worker。

Q-3诊断queue_items改为预算q.items，含在读/处理中，和字节口径一致。Q-4 reader/Pong/worker/journal的年龄钟统一h.now，UTC与注入时钟一致；elapsed仍用真实Stopwatch，不混为配置预算。Q-5后台组拒绝启动输出时stopped按q.mu保护并可见gap/关闭；Q-7按组长22:00:43Z令只记关停期观察，不改touch后台登记行为。

Q-6必须在正式集成/发版说明保留：sessionClient.write两秒总截止也覆盖outbound receipt与delivery_ack_confirm，其调用点在错误时分别累计有限obs计数。相比旧的长等待，这可能提高“已提交但回执写失败”的频率，不能拿现网旧构建unknown样本估算本次增量。失败不回滚已提交业务、更不许可换新ID盲重发；①持久outbox/原sender+source ID恢复原回执、dw_send_status与delivery_status查询承担兜底。①②联合验证须注入慢读/写背压导致回执超过两秒，观测发送侧unknown或未确认状态、持久接纳仍在，以及故障消失后原ID/原delivery可恢复，不能伪造已收/模型已执行。本②单源未含①，不能把这格冒称已通过或计SKIP；集成前必须实际补联合读数。控制写失败计数见下段，diagnostic_drop只按原语义，不混作unknown统计。

HKF-PM04既有真实WS补证的helper/夹具字节本轮不改，仍只复用有限控制帧结论；不限速大正文吞吐下降是已披露代价，正式发版说明需点明，不上调为生产SLO或解释419次。


组长22:00:43Z处置补正：Q6新增session obs有限计数outbound_receipt_write_fail与delivery_ack_confirm_write_fail，各自在对应c.write返回错误时加一（含两秒写槽/I/O截止或caller取消）；不混入diagnostic_drop、不按错误字符串/ID扩张标签，不回滚已提交队列/ACK，也不把失败计数解释为丢信或模型未执行。Console monitor底层WS快照可观测；进程重启清零，不是持久账本。①dw_send_status/原sender+source幂等恢复仍是兜底。②本源用真实连接写槽占用超过2秒测到计数及原delivery ACK提交/同ID回执恢复；未包含①helper outbox，因此完整①②联合格仍待集成。Q7依令仅观察，撤回eca中的touch后台拒绝退出额外改动，保留关停期已知边界。
