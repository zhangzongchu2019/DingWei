# Design

## Context

只读基线与挂点见 specs/hub-lifecycle/context.md。现网两次错误均已证实是十秒 shutCtx 超时，但没有逐组件证据识别阻塞请求。两台 server 当前串行 Shutdown，先关内网并可能等满预算，公网可能仍接入；这解释单条末尾 502 的一种路径，仍是推断。

## Goals / Non-Goals

- 目标：实现可验证的停机时序、双 server 并行排空、长流/WS 主动取消、持久性顺序、失败可区分及阶段记录（HLC-001～009）。
- 本单只写提案，不运行产品生命周期场景，不修改实现、客户端协议、部署或 r46；不把 strict rc0 当作业务验收，不保证单实例无感切换。

## Decisions

[业主裁定rulings第㉒条](https://git.code.tencent.com/wego-ai/dingwei/blob/0659d208ce731b73206b06d587c3814649ae11d2/docs/rulings/S-TEAM-MIGRATE-HOSTED-20261017.md)：业主workpulse对话，Hub收信2026-11-01T23:07:47Z，delivery dw-dlv_f5170d3c5deb6106833dc76e7c36db1e；对“r48那11项是否全部按建议定”答原话“ok”。本单S-R48-PM-WRITEBACK只回写已定值；不是实现、apply/archive、迁移、预发或发布授权。 HLC-PM01～04编号与定值对照见proposal；原阶段/场景/防御性HLC-009保持，PM批准不把尚未执行的场景改成PASS。

### 1. 单协调者、封住所有新入口（HLC-001、002）

第一个 SIGTERM/SIGINT 建立单次停机协调；重复信号不得重置截止、重复 Close 或让结果从失败变成功。逻辑阶段为停止接入、并行排空、持久收尾、关闭存储、记录退出；这些是内部阶段标记，不是新增外部 API。

t0 使用单调时钟计时，先原子设 draining，关闭新的 HTTP 业务/流/WS 注册准入，并发启动两台 server 的 Shutdown，在同一个接入阶段内停止两端 listener。这里“同时”指无串行等待另一台排空，原design实测容差两端关闭差≤100ms、各自距t0≤1秒；持续探测两端 listener，不能只看 goroutine 已启动。

仅 listener 关闭不充分：已经 Accept、尚未完成路由或 Upgrade 的 handler 仍可能注册会话。升级/流注册必须通过与 draining 同步的注册屏障：停机前已登记者纳入等待，停机后不得 Add 新会话/订阅。等待入口封闭及在途注册完成后才允许 run 调用 Hub.Close，避免 sessionWG.Add 与 Wait 竞争；记录 barrier 完成时刻。已接受的普通有限业务允许在截止内完成，不用全局 BaseContext cancel 在 t0 粗暴取消其事务；仍在路由前而未获得准入者返回可重试的 503（HLC-001）。该 503 不承担客户端自动重投写请求授权。

### 2. 主动解除长流并接入 Hub.Close（HLC-002、003）

接入屏障完成后发独立 lifecycle 取消信号给 View 内网 SSE、Console mux/bridge 的长流上下文及管理这些流的 goroutine；禁止只取消后台 runCtx 后继续等 HTTP Shutdown。向所有已登记会话 WS 发协议关闭并有界等待；正常握手关闭与超时后的CloseNow强制回收必须区分记录，现有CloseNow不能自动算正常排空。Hub.Close 由 run 中的协调者显式接入，不等到 Shutdown 返回后才 defer 调用。现有 Close 关闭 mux、AMR 和会话连接，但并未覆盖所有内网 SSE，且部分等待不带 deadline，必须在后续实现任务里逐项补齐。

两台 Shutdown 的等待、长流退出与 Hub.Close 收尾并行进行，分别采集结果；成功必须验证实际处理 goroutine 结束，不是 Close 返回即假定全部结束（现有 WS 5秒等待到期只日志）。到排空阶段截止，对剩余普通 HTTP 调用强制 Close，对 hijacked WS/独立 bridge 单独关闭，记录类别、数量与失败；强制动作不把已发生超时洗成成功。两个 server 的独立 Shutdown 返回（含 nil、deadline、listener error）均保存，不能被后一次结果覆盖。

### 3. 生产者、持久队列与存储（HLC-004）

停止接入阶段先阻止后台调度、外部接收与其它生产者新增工作，发其专用停止信号；不要把仍需提交/确认的工作与生产者一起无差别取消。准入前已接受的有限请求在排空阶段提交或完整回滚；存储保持打开。随后停止领取新队列项，等在途工作完成，或确认未完成项仍在持久存储、恢复时可重投且不会把未提交处理标为已确认。这里的排空是停止内存活动并保存可恢复工作，不要求持久待办计数为零。

只有所有存储使用者退出、提交/回滚及持久确认边界核验完才 Close SQLite/PG。清理协调不依赖可能无限阻塞的 defer 链：Close 必须受阶段与总预算约束；无法及时终止的组件到总截止前记录失败并走非零进程退出，不在仍运行的事务下把 DB 提前关闭然后报告成功。超时情形可能无法证明干净关闭，明确记录未知的持久性状态，不承诺已接受未确认请求必然落盘。

### 4. 已定预算及退出映射（HLC-005、006）

prodops e43100e3已核预发与现网有效TimeoutStopSec均为30秒、两单元均无drop-in；**以30秒为外层上限设计**。以下应用总预算15秒及阶段1/8/3/1/2秒已由rulings㉒的HLC-PM02批准；批准不等于实测性能达标。以 t0 为唯一起点，各阶段可以提前完成，后续阶段不超过其绝对截止，不把每个组件串行叠加预算。

| 阶段 | 最长阶段预算 | 从 t0 起绝对截止 | 依据与超时动作 |
| --- | --- | --- | --- |
| 停止接入/生产者停止通知/注册屏障 | 1秒 | 1秒 | 本地关闭监听应快速完成；差≤100ms待实测；屏障超时不得无防护调用 Hub.Close |
| 两台 HTTP、SSE/mux/WS、Hub.Close 并行排空 | 8秒 | 9秒 | 包含现有 WS 最多5秒及其余清理余量；超时分类采样并强制关闭，失败锁定 |
| 队列在途提交/回滚、持久检查及消费者退出 | 3秒 | 12秒 | 离线故障夹具目标，不是现网吞吐容量；超时保留可恢复项，禁止提前确认 |
| DB/资源关闭 | 1秒 | 13秒 | 已定阶段值，仍须本地负载实测；有活跃使用者不假装安全关闭 |
| 结果汇总/超时强制动作/进程退出预留 | 2秒 | 15秒 | 为故障时日志与有界退出留余量；不能用本阶段延长总截止 |

某阶段超时后立即锁定失败，尽力清理只使用剩余总预算。总预算 T=15秒，**从收到信号到真实进程退出≤15秒**；离线外部测量允许≤0.5秒调度/采样误差（≤15.5秒），误差不是应用额外预算。需验证黑洞日志输出、阻塞 Close 不能让协调者无限等待；终态日志尽力有界落到 stderr/journal，若日志输出也失败不得报成功。

外层S=30秒已按e43100e3的有效TimeoutStopUSec/fragment及空DropInPaths核实（预发、现网各一格）。保留㉒已定应用T=15秒，名义差额S−T=15秒，**不把应用预算增加到30秒**。systemd开始停止计时到应用t0收到信号的差记δ，要求δ+T+M≤30秒且M≥5秒。ed797ae6的C五次请求至systemctl stop返回为0.106086～0.106635秒，TERM trap观测为请求后0.009721～0.010681秒；本轮按组长要求用整个停用开销向上取δ≈0.11秒作保守样本复核：**0.11+15+5=20.11≤30秒**，扣δ后的外层余量14.89秒，保留M=5秒后另余9.89秒。0.11不是精确内核信号延迟或任意负载硬上界；若实施/GO实测δ使条件失效，缩减应用预算或重议流程，不能直接等到SIGKILL。

c90f41c2 B节已核预发/现网KillMode=control-group、KillSignal=SIGTERM、SendSIGKILL=yes、FinalKillSignal=SIGKILL、WatchdogSec=12秒、WatchdogSignal=SIGABRT；控制器继承的systemctl stop子进程超时40秒，其返回后才进入默认timeout=30秒、interval=1秒的wait_stopped轮询，两段顺序而非共用30秒。故40秒命令等待覆盖S=30秒，额外轮询最多30秒；不能把r45一次约10秒实际等待当作上限，也不能把40+30秒控制器上限移作应用预算。ed797ae6的目标systemd259停止期不执行看门狗，最终外层强制兜底按已核配置为**TimeoutStopSec=30s→SIGKILL（control-group）**，不依赖停止期watchdog。A/B均在30秒前退出，未实触30秒SIGKILL边界；配置事实与该边界实触验证分开记录。未来实施/GO前须复核配置、控制器和负载是否漂移。

30秒仅是配置外层约束，**不代表停机能在30秒内完成**，也不能证明15秒应用上限已通过；HLC-S001～013仍须真实计时，实测超出15秒即失败，不能因尚未到30秒而报成功。若不满足预算、余量或控制器条件，退回PM，本文不改单位配置。实现前测量慢事务/连接规模；按时完整收尾=0，任一阶段 timeout=专用非零2（HLC-PM01已定），无 timeout 但清理/持久化失败=其它非零1（HLC-PM01已定）；二者并存以 timeout 码优先，日志保留全部错误和不安全状态。正常启动/迁移/绑定等失败保持非零，不能扩大 SuccessExitStatus 或吞 context deadline exceeded。


### 4.1 排空期间继续喂狗（HLC-009）

HLC-009按组长裁定保留**继续喂狗直到最终退出**，理由是跨systemd版本可移植且成本低的防御性要求。固定2af469dd模板W=12秒；c90f41c2另核预发/现网有效窗口同为12秒；ed797ae6目标systemd259复刻单元A停止期不喂狗25.024786秒正常退出，B持续喂狗24.125013秒正常退出，均无SIGABRT/SIGKILL。故目标停止期不执行看门狗，原“W=12秒与应用已定15秒预算冲突”已由该实测解决，不构成目标环境冲突；不得把停止期watchdog当作兜底，最终强制兜底只有TimeoutStopSec=30s→SIGKILL。现有startSystemdWatchdog绑定runCtx、cancelRun后退出仍是代码事实；保留防御性要求意味着未来仍须独立交接其生命周期，不把本次临时单元实验当作Hub实现已完成。

后续实现须将喂狗所有权从业务runCtx交接到单一停机协调者的独立、受截止约束的生命周期；交接不产生通知空档或两个无协调的喂狗循环。停止接入时立即发一次WATCHDOG=1，此后直到进程实际退出（包含队列/DB和最终结果记录阶段）持续发；不能因cancelRun、关闭监听、Hub.Close或开始写终态日志就停喂。使用受信systemd通知通道和实际WATCHDOG_USEC窗口W，建议发送周期≤W/4、接收间隔≤W/3；W=12秒的隔离格即目标3秒、实收最大间隔4秒，调用/重试同样有界。未启用watchdog时显式记录disabled，不伪造收到通知；通知失败纳入关停失败结果，不更改㉒已定PM01退出码。

停机喂狗表达的是“协调者仍处于其有界阶段、可履行退出契约”，不是宣告已关闭的HTTP服务健康。喂狗判断使用停机阶段及不可延长的总截止，不再依赖已取消的runCtx或已拆除的监听/DB探针；正常运行时已有锁健康判据保持原语义。阶段或总截止失守时不得无限续租掩盖卡死，必须记录失败并按原15秒总上限退出；故障不能靠一直喂狗拖到30秒外层。最终退出前仍保持喂狗，不提前取消最后一段看门狗生命周期。

HLC-A05的目标systemd停止期行为子项由ed797ae6闭合；Hub交接/通知间隔仍待实现验证。HLC-S013保持原“隔离监督器”格及反向控制文字，使用停止期继续检测的独立监督夹具，只验Hub自身WATCHDOG=1通知连续性，不推断目标systemd停止期检测行为。目标systemd259不在停止期检测，A格未收到SIGABRT不能证明Hub继续喂狗成功；仍须S013的真实通知接收轨迹。S013监督器的SIGABRT为隔离测试判据，不能写成现网停机兜底。

### 5. 结构化结果（HLC-006）

每阶段记录 shutdown_id、原因/信号、开始/结束 UTC、单调耗时ms、预算ms、timeout、forced_close、result。两个 server 分别记录 Shutdown error（明确 nil/具体分类）、listener关闭时刻、排空时刻；并行阶段还保留 Hub.Close 与 SSE/mux/WS 各自结果。阶段失败与终态各采一份计数快照：public/internal活动普通HTTP、internal SSE、public mux/bridge、session WS、其它 hijacked连接、生产者、队列在途/持久未完成/状态未知。HTTP总连接与流子类有重叠，日志标明维度，不将它们相加当独立总数。

看门狗另记录实际窗口W、生命周期交接时刻、通知发送/成功接收轨迹（夹具或systemd可用证据）、最后通知时刻、最大间隔、发送错误与supervisor是否发SIGABRT；不得把发送调用成功冒充systemd已消费。最终汇总记录 exit_class=graceful/timeout/failure、exit_code、total_elapsed_ms、各阶段结果与所有错误；故障类别未知时显式 unknown 和可计数范围，不输出请求正文、URL查询串、token、用户内容或凭据。结果表示完成动作，不能以“已发取消”代替“已完成”；deadline后强制关闭仍保留原错误和超时计数。字段为拟议日志契约，不宣称现网已有。

### 6. 切换与客户端体验（HLC-007）

两个 server 改成同时停止接新连接后，公网停止接入可能从原来十秒等待的末尾前移到t0。单实例控制器等旧Hub停完才启动新Hub，成功排空耗时也会计入公网无法建立新连接的区间；本方案可能以更一致的排空协议换来更早的连接拒绝，不把它包装成缩短全部可用性空档。优化冷启动或双实例切换另提案。

计时分别测：控制器发 stop→旧PID退出→新listener可连→健康/鉴权ready；listener close→新listener可连；新ready→各helper连回；SSE断开→事件恢复。按㉒已定预算设计离线夹具旧停≤15秒、新启动到ready≤5秒，因此控制器stop→ready≤20秒（外部测量≤20.5秒）。控制器调度延迟必须计入这20秒，不另加隐形宽限；若实测不满足HLC-PM04则本格失败并回报，不自行放宽已定值。

客户端可见：新HTTP连接可能被nginx映射成502/连接拒绝；已接入但未获业务准入者503；在途HTTP完成或在强制截止被断开；SSE读到EOF/网络断开（先前200保持已发状态）；WS收到关闭帧或传输断开（不保证1000）；helper按现有退避重新注册，Console/View按原游标补缺并处理既有gap机制。不新增shutdown事件或无条件自动重放写请求。按HLC-PM04已定要求，新ready后全部20个固定夹具helper≤5秒连回；三类流各≤5秒恢复首个可验证测试事件，重复呈现与缺失数量均0（跨纪元gap必须显式可见且不能伪造连续），超阈值失败不掩盖。

现场 r45 停止完成到ready约350ms、单条同秒502只能作历史事实；它既不是整个停机不可用时长，也不是本方案新SLO。实际生产流量影响和重连分布需授权窗口独立测量，本单不做。

## 验收矩阵（HLC-008）

全部为未来离线测试计划，当前执行状态均“未执行”。固定上述已定预算、独立进程/随机loopback端口/临时DB/HOME/假网关；关闭继承的WP/DW/SH/DINGWEI/FEISHU凭据与通知配置，不连现网。时长从实际发SIGTERM到waitpid完成，退出码观察真实进程，不只mock Shutdown。每格同时核双端关闭差≤100ms且均≤1秒、阶段日志齐全、DB使用者先退出、无泄漏和持久快照；有负载格重复50次并打乱顺序。

| 固定场景 | 夹具/动作 | 期望退出码（㉒已定） | 最长停机/额外判据 |
| --- | --- | --- | --- |
| HLC-S001 空载 | 已ready，无外部活动请求 | 0 | ≤2秒；双方Shutdown error=nil |
| HLC-S002 有限慢HTTP | 公网/内网各1个活动请求，t0后2秒完成并提交 | 0 | ≤5秒；请求完成、提交可恢复，不被t0取消 |
| HLC-S003 不释放HTTP | 公网/内网各1个部分body或阻塞handler，直到排空截止仍不释放 | 2 | ≤15秒；9秒截止触发forced_close，两个server错误保留；阻塞类别/数量与夹具一致 |
| HLC-S004 活跃SSE | 100个内网View SSE持续心跳，无客户端主动断开 | 0 | ≤10秒；由停机取消退出，无强制关闭；客户端EOF/重连记录齐全 |
| HLC-S005 活跃WS | 20个真实会话WS及注册竞争请求，客户端正常读关闭 | 0 | ≤10秒；Hub.Close收尾完成，无屏障后注册或WG竞争 |
| HLC-S006 混合 | 100个有限HTTP（内外各50，2秒完成）、100个内网SSE、20个Console mux/bridge、20个WS；飞书回调在途和封口后迟到两格 | 0 | ≤15秒；业务处理器/回调归零，迟到拒绝可计数且封口后DB访问0；撤回调封口在DB断言红，SDK等待帧另列排除 |
| HLC-S007 混合故障 | 混合格中留1个不响应取消的内网SSE与1个公网HTTP；其余正常 | 2 | ≤15秒；超时快照精确指出这两类和数量，强制关闭不改成0 |
| HLC-S008 持久化失败 | 封口时channel仍有多批；另格注入提交失败/3秒超时/失败后超时 | 0/1/2/2 | 排尽后剩余0；失败保留首错与未提交数，超时不关仍在使用的DB；撤排尽须红在未全部提交断言 |
| HLC-S009 反向控制 | 在S004/S006夹具仅撤掉SSE/mux主动取消，保持网络连接与ping存活，其余不变 | 2（变异运行） | ≤15秒；原格期望0/无强关必须红在长流未排空断言，不能仅因命令报错判红；恢复取消后同格0 |
| HLC-S010 启动与重复信号 | 独立启动绑定失败；另以混合格t0+0.1秒重复SIGTERM | 启动1；重复信号格0 | 启动失败≤2秒；重复信号格≤15秒，不重置deadline、不重复Close |
| HLC-S011 切换恢复 | 隔离控制器stop旧/start新、20个假helper及SSE/mux/WS重连，保留测试消息ID | 旧0、新保持运行 | stop→ready≤20秒，ready后helper全回≤5秒、各流恢复≤5秒；无静默gap、重复显示0/可验证消息缺失0 |
| HLC-S012 内网错误独立可见 | 注入internal Shutdown非deadline错误，public正常结束 | 1 | ≤15秒；internal原错误与public nil同时在结果中，不因后者成功覆盖 |
| HLC-S013 超过12秒的有界排空与看门狗 | 独立监督夹具W=12秒、到期发真实SIGABRT；令各阶段都在自身截止内但总收尾为13～15秒，收到SIGTERM后不停止监督 | 0 | 13≤实际停机≤15秒；不得被SIGABRT打断，接收通知最大间隔≤4秒直至退出；阶段计时/退出记录均完整 |

S009另做WS关闭撤除变异：保留真实WS、禁用显式WS关闭并让等待持续，S005必须在WS未排空断言红；若只删一行没有移除其它关闭路径导致仍PASS，说明变异不充分，应报告无效变异。另撤注册屏障，竞争格必须检出屏障后注册或race；删内网结果采集，S012必须红。所有变异只在独立测试副本，不改交付源锚。

S013反向控制：在相同夹具中保留t0初次WATCHDOG=1，此后仅撤掉排空期间继续喂狗；夹具持续在deactivating阶段计时，于最后通知后12秒发SIGABRT。变异必须在“未被SIGABRT中断/连续通知”断言红，真实waitpid观察到信号终止（非期望exit0），恢复喂狗后同格绿。禁止把监督器自身异常退出、提前结束排空或缩短12秒窗口算有效红。为保证总时长大于12秒且不故意触发阶段超时，可用各阶段0.9/7.9/2.9/0.9/0.9秒的受控耗时（合计13.5秒），任何一阶段及全局上限仍按原表验证。夹具只作用于隔离子进程，禁用core产物；没有真实systemd的普通测试不能冒称验证了目标systemd行为。另获授权后用一次性隔离systemd单元核停止期检测与通知轨迹，不能停止现网/预发单元做本提案验收。

## Risks / Trade-offs

上述S013原格与反向控制保持不变；原计划末句中的“目标systemd停止期检测”事实子项现已由ed797ae6闭合，不再列作T-03待测。Hub自身通知接收轨迹仍须后续隔离验收，prodops临时单元实验不能替代它。

- 准入屏障改变请求接受时点；同步HTTP事务与队列持久边界是必要审阅点，不能仅靠goroutine Wait判安全。
- 现有Hub.Close部分无限等待/WS等待到期只日志；单独调用现函数不足以满足预算与结果契约。
- 主动关闭流会触发重连风暴；20/100规模只是候选，真实退避和容量待测，失败保持可见。
- 强制退出可能留下已持久未消费任务，也可能无法确认部分工作；日志必须区分，不为退出0清除恢复证据。

## Migration Plan

PM01～04已按rulings㉒定值，T-03回写闭合；e43100e3/c90f41c2/ed797ae6已闭合有效systemd参数、停止期行为、控制器40+30秒及δ/连接规模快照事实。由组长按已定规格另派实现，并在实施/GO前复核漂移；本次PM与事实闭合不等于实施/验收门完成。实现时同步最小主规格/context与代码挂点，逐格及反向控制、race/随机50轮、仓库构建门通过后交独立验收；预发与生产各需GO。回退实现须来自固定Git提交，不以本提案授权部署、变更单位配置或清队列。

## Open Questions

HLC-PM01～04的退出码、预算、单实例范围及恢复目标已按rulings㉒定值，proposal保留原选项并追加逐项已定表。停止配置、控制器期限、目标systemd停止期行为、δ样本与当时连接规模已核；峰值负载/客户端退避、队列持久边界及所有Hub生命周期格仍待实施后验证。技术前置未核/实现未获派时不转成生产行为。

## S-HLC-IMPL T-04 技术裁定与生命周期挂点

组长对队列确认/注册屏障的技术裁定只收紧停机路径，非新PM值。运行期 AsyncDBQueue.Enqueue 仍为 best-effort：入内存即返回，入站可能先于派生出站落库被确认；本单不声称处理与派生出站跨动作原子提交。S008限定为停机期落库失败不被吞、剩余条数/原因可见、退出1；阶段超时退出2并优先，全部原因保留。运行期缺口见 docs/findings/queue-ack-durability-c248506c.md，只记录不修。

停机先封新HTTP/外部回调及消费者领取；已获消费者令牌的在途工作使用独立上下文，在8秒排空阶段完成。HTTP有限请求保留原请求上下文，不在t0取消事务。写队列使用独立上下文，生产者与在途处理全部退出后才封口；停机专用SealAndFlush在3秒阶段冲刷channel和部分batch、等待真实写协程完成，传回失败及剩余计数。已有运行期批丢失无法凭停机补写保证恢复；该未知/失败不得清零洗白。

注册门由Hub实例持有，封口与计数Add共用互斥门。登记计数只覆盖准备/Upgrade/发布，不覆盖WS整条处理器；后者独立计数，CloseContext接入后发送关闭帧并等待实际退出。屏障阶段失败不能无防护进入Hub.Close。Hub的独立长流取消树覆盖内网View SSE、public mux与internal bridge；service/view/session WS有显式登记。node WS处理器纳入同一长流生命周期。Hub派生后台工作与实际执行的定时回调独立计数；停止后不再新启动存储回调。

勘察清单：cmd/workpulse/main.go的processor/outbound/receiver/reminder/cron；newplat_hosted.go的backup/login/quota/hosted扫尾；internal_notify.go的Unix listener；internal/m8的AMR、capability cleanup、L2/task-flow、session保活/skill/delivery/directory/refresh回调、Console mux子工作及三类WS；新平台PG与SQLite。取消信号不等于已退出，任何尚存储使用者都阻止DB关闭并保持非成功。看门狗使用单一独立循环，与业务ctx分离，t0唤醒通知，停止期不再做已经拆除的服务健康探测；实际总截止不延长。

控制器兼容提醒：未来切换控制器必须将 expected_sigterm_exit1 改为按正常0／超时2区分（其它失败1），由prodops另单修改；本分支不修改控制器、systemd单元或SuccessExitStatus。目标systemd259停止期不检测watchdog的边界与S013隔离监督器要求保持不变。

### T-04排尽补充裁定（2026-11-01）

已读组长23:23:58Z原裁定（eb859dcad4b1；收信dw-dlv_65f174857d2674b47ade53e0febb2003）及本次补充（dw-dlv_b9197076fd0dc513f7639f24344526cf）：仅收紧停机路径。SealAndFlush以准入门封口，封口后Enqueue返回明确错误；再逐批取尽channel和部分batch。返回首个提交错误，ShutdownCounts给出未提交remaining与失败failed；成功提交才递减remaining，未尝试项继续计入remaining，已失败项不自动重试。后续失败不得覆盖首错，原始DB原因仅留内部供errors.Is核验，日志输出安全分类。

等待锁、在途写协程及排尽都使用DB阶段的3秒截止；队列完成和deadline同时就绪也不能洗成成功。超时2优先于其它故障1；阶段结果与实时队列快照分别保留deadline与首个提交失败分类，避免超时竞争导致Flush尚未返回就丢掉故障计数。写协程未结束不得关闭DB。运行期Enqueue/Flush仍best-effort；本单不建立跨动作原子提交，也不把已发送回执未落库推断为业务未发送。

S008的确定性多批夹具在writer启动前缓冲23条、每批4条，封口后才启动writer；应形成4/4/4/4/4/3六批并全部提交。分别注入第二/第四批不同错误验证首错与8条未提交，第二批超时验证未尝试15条仍计入剩余。移除停机排尽只留当前批flush的变异必须在S008_ALL_BUFFERED_COMMITTED断言失败；覆盖首错变异须在S008_FIRST_ERROR_AND_REMAINDER断言失败。队列/协调器隔离子进程另记录真实0/1/2/2退出码；该子进程不冒充完整Hub混合流量验收，S006/S011及其余生命周期矩阵仍未完成。

## 飞书SDK生命周期裁定与Hub归属（2026-11-02）

已独立fetch核prodops运行态证据review/prodops/fcs-runtime-facts-20261101，commit c15d6d7884f8452bf7717818f329ec192bca5094，tree 2ca8a513ee5e7d1255069a235aa419c0eed783d8，REPORT路径docs/reviews/prodops-fcs-runtime-facts-20261101/REPORT.md。23:53～23:55Z的现网Hub PID1344160无FCS_RPC_URL、枚举ws，三次飞书域名套接字均1且归属dingwei-dw123-hub.service；无独立fcs单元。预发PID1284661为fake且0连接。该证据证明本次在Hub内实施，不将源码装配分支当运行态；不在独立FCS进程动手，不冒称预发实际Close了真实飞书长连接。

组长原裁定request_id 68d69f8ce9c3（delivery dw-dlv_2741db145ed41de256148b9e78ce8bee）落实到HLC-004/006及S006。LarkGateway/MultiGateway实现可选内部ReceiverLifecycle；StopIngress在8秒drain上下文中显式Close，随后封回调准入，等待所有已准入回调真实返回；封口后的消息事件在规范化/业务处理前拒绝并计数，不访问DB。已准入回调仍可完成其提交／回滚；DB只在全部返回后关闭，不把已接受工作粗暴取消当完成。close阻塞、启动未完成或回调未返回均按阶段截止返回，不能报正常。

回调封口与计数Add复用实例lifecycle.Group，迟到拒绝数用独立原子计数；内部快照含active_callbacks/rejected_after_seal/sealed/close_completed/sdk_start_not_joined，对应HLC-006，不新增外部协议。关闭完成仅指Close调用已经返回，不代表SDK内部协程全部退出。Hub直接路径在drain接StopIngress并等待根入站门；远端FCS receiver不实现此可选接口，维持原订阅取消和根回调门，相关FCS包单测复验。

**不冒称所有SDK goroutine已join。** 钉版v3.9.7的ws/client.go:206～230成功后进入固定select{}，Close不解除该Start帧；SDK内部ping/cache等也不归Hub业务工作组所有。该Start帧记sdk_start_not_joined，独立于业务处理器/回调归零计数；不以runtime.NumGoroutine总数减一冒充所有SDK工作退出。SDK残余随真实进程退出回收；本单不改SDK、依赖版本或协议。若另修依赖，另立任务。

S006回调扩展夹具使用真实事件dispatcher与临时SQLite：初始回调在封口前完成一次入库后保持在途，Close后封口，随后从同一dispatcher注入迟到回调；记录总DB调用及封口后调用（期望0）、拒绝数、活跃数及Close次数。这个受控格证明迟到回调不触DB；它不声称任意已准入事务的后续DB步骤都发生在封口前。撤封口变异在S006_POST_SEAL_DB_ZERO断言应出现after_seal_db=1。完整HTTP/SSE/mux/WS混合矩阵仍须连同此扩展交件，回调扩展单测不替代完整S006。

另以独立进程令钉版SDK真实连接loopback WebSocket，观测Close后对端断开、实际Start等待帧仍1、业务回调0，进程结束回收残余。TestReceiverPinnedSDKProcess默认明确SKIP，原因是SDK残余只由进程回收，须单独运行HLC_PINNED_SDK_PROCESS=1；独立运行的实际结果单列，不把默认skip算通过。业务适配层/门及FCS兼容跑默认vet/race，钉版SDK实际连接夹具的普通go test结果不冒称SDK全生命周期race认证。

已准入Hub根回调的数据库工作使用callbackWorkContext：保留SDK入站context值，脱离接收器t0取消，改由独立workCtx约束；排空超时Force取消workCtx。此只服务已获准入的工作，迟到回调在创建该context前就被拒绝，不因脱离取消而延长8秒阶段或总预算。Hub接线测试覆盖Close先于根门封口、在途根回调等待、8秒超时退出2且不关DB，以及remote保持既有根门。
