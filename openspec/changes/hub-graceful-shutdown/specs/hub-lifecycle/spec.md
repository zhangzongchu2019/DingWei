# Hub Lifecycle

## Purpose

本变更把 Hub 的计划停机从串行共享十秒截止且缺少长流清理和完整结果记录，改为两个监听同时停止接入、主动取消长流并有界收尾的协议，目的是区分正常排空、超时和其它故障并保留持久工作。涉及 Hub、HTTP server、队列与存储生命周期模块，不影响 R5 阅读和 R11 冻结规则。

## ADDED Requirements

### Requirement: HLC-001 双端停止接入与单次协调
Hub MUST 在首次SIGTERM/SIGINT后以同一单调起点协调停机，原子关闭新业务/流/WS准入，并发停止内外两台HTTP server监听，不等待一台排空才停另一台。按原design验收值，两端距信号均不超过1秒、关闭时差不超过100ms。已接入但未获准入者MUST 返回503；重复信号不得重置总截止或重复资源关闭。

#### Scenario: HLC-S001 空载停机
- **WHEN** 已就绪的空载Hub收到SIGTERM
- **THEN** 两端监听在上述时限关闭、两个Shutdown错误均为nil、真实进程在2秒内退出0，并产生完整阶段记录

#### Scenario: HLC-S010 重复信号与启动失败隔离
- **WHEN** 混合负载停机后0.1秒再次收到SIGTERM，另格注入启动绑定失败
- **THEN** 重复信号不延长15秒总截止且正常排空退出0；启动失败格2秒内非零退出，不能被计划停止规则改为0

### Requirement: HLC-002 注册屏障与 Hub.Close 接线
run MUST 在双监听关闭及注册屏障完成后显式接入Hub.Close。屏障MUST 与draining原子协调，封闭已经Accept但尚未Upgrade/注册的请求，确保等待开始后没有新的sessionWG.Add或流注册。屏障失败不得绕过前提直接Close并报告成功；所有等待纳入预算且能观察真实收尾结果。

#### Scenario: HLC-S005 活跃WS与注册竞争
- **WHEN** 20个真实会话WS存活，另有请求在Accept/Upgrade/注册之间与停机竞争
- **THEN** 屏障前登记者纳入关闭等待、屏障后注册数为0、WS处理器全部结束、无WaitGroup竞争，10秒内退出0

### Requirement: HLC-003 长流主动结束
Hub MUST 用独立停机信号主动结束View内网SSE、Console mux/bridge及会话WS，并有界等待处理器退出，不能仅依赖runCtx或客户端断开。HTTP Shutdown不覆盖的hijacked连接MUST 单独处理；排空超时必须记录剩余类别/数量并强制关闭，发生超时后不得改判正常成功。

#### Scenario: HLC-S004 活跃SSE
- **WHEN** 100个持续心跳的内网SSE连接保持且客户端不主动断开，Hub收到SIGTERM
- **THEN** 停机信号使所有处理器退出、客户端观察EOF或网络结束、不走超时强关，10秒内退出0

#### Scenario: HLC-S009 撤除主动取消的反向控制
- **WHEN** 在相同SSE/mux夹具撤除全部主动取消路径，另格撤除显式WS关闭，保留连接和心跳
- **THEN** 变异运行超时以专用非零退出，原成功格必须分别红在长流或WS未排空断言，恢复保护后同格通过，不接受无效变异

### Requirement: HLC-004 生产者队列存储顺序
Hub MUST 在停机路径先停止新生产和新准入，保留已接受有限请求提交/回滚所需存储；停止领取并等待在途处理完成后，封住写队列入队，再逐批排尽channel及部分batch，返回首个提交错误与未提交计数；封口后Enqueue MUST 明确拒绝。等待受3秒DB阶段约束，超时2、无超时提交失败1，二者并存保留原因且超时优先。进程内飞书接收器MUST 显式Close长连接后封口回调；迟到回调拒绝并计数、不进处理或访问DB，已准入回调在8秒排空阶段等待实际返回，超时2。结束存储使用者后才关SQLite/PG；MUST NOT 清空持久队列伪造排空或在仍有使用者时关DB并报成功。运行期仍为best-effort，不声称入站处理与派生出站跨动作原子提交；无法证明持久性MUST 失败可见。

#### Scenario: HLC-S002 有限慢请求
- **WHEN** 内外各1个已接受的有限HTTP请求在SIGTERM后2秒完成事务
- **THEN** 两请求能完整提交且重启可核，DB在使用者退出后关闭，5秒内退出0

#### Scenario: HLC-S008 持久化失败
- **WHEN** 停机封口时channel仍缓冲多批，另格注入提交失败、DB阶段超时及失败后超时
- **THEN** 成功格全部提交且剩余0；无超时失败格退出1、保留首错与未提交条数，超时格3秒内结束等待并退出2、未结束的写协程阻止关DB；失败与超时原因同时可核，撤排尽逻辑须红在未全部提交断言，不改运行期确认语义

### Requirement: HLC-005 预算与可区分退出
Hub MUST 使用明确总预算与阶段预算、按时全部收尾退出0，任一阶段超时以独立非零码退出，其它关停失败非零；两者并存保留全部原因并优先超时分类。MUST NOT 吞deadline或用SuccessExitStatus掩盖。按rulings㉒的HLC-PM01/02已定总15秒、阶段1/8/3/1/2秒、超时码2、正常0及其它失败1；外层S=30秒已由prodops实测预发/现网有效TimeoutStopSec且都无drop-in；MUST 以30秒为外层上限，按已核停止参数/控制器期限及计时差样本δ≈0.11秒复核δ+T+5≤30，实施/GO前重核漂移；配置值MUST NOT 代替真实停机计时或允许用满30秒，不得自动改配置。

#### Scenario: HLC-S003 不释放HTTP
- **WHEN** 内外各1个活动请求保持到排空阶段9秒绝对截止
- **THEN** 分别记录Shutdown结果与剩余连接、强制关闭且不洗白timeout，15秒内以已定超时码2退出

#### Scenario: HLC-S007 混合超时
- **WHEN** 混合负载只剩1个不响应取消的内网SSE与1个公网HTTP
- **THEN** 超时快照记录对应类别及数量、强制动作和原错误均可核，15秒内以超时码退出而非0

### Requirement: HLC-006 阶段结果完整保留
Hub MUST 记录每阶段起止UTC、单调耗时、预算、超时、强制关闭及结果，并独立保留内外Shutdown原错误与Hub.Close/长流/队列/存储结果。终态MUST 含退出分类/码、总耗时、所有错误及剩余连接类别/数量、在途及持久待办；未知显式标注，不把总连接与流子类重复相加。MUST NOT 记录请求正文、凭据或敏感查询串；日志自身阻塞不得无限延长退出。

#### Scenario: HLC-S012 内网错误不丢失
- **WHEN** 注入internal Shutdown非deadline错误而public正常完成
- **THEN** 两个原结果同时保留，终态为failure且已定码1，15秒内退出；撤除内网错误采集后用例在结果完整性断言红

### Requirement: HLC-007 切换影响与恢复可测
停机验收MUST 分别记录控制器stop、监听关闭、旧PID退出、新监听可连、新ready及客户端恢复时刻，不能将stop完成到ready或单条502当全部不可用时长。MUST 明示新连接可见502/拒绝或准入503，SSE可见EOF、WS可见关闭/断开，沿用现有重连和gap协议，不自动重投无幂等保障的写请求。按rulings㉒的HLC-PM03/04只做单实例有界停机，stop到新版ready≤20秒，ready后20个helper、SSE及普通会话WS恢复各≤5秒；mux按组长3f698b77d848补充裁定，以用户显式resync后≤5秒恢复，自动恢复红及gap送达分列，并同时记录stop起与操作起时钟；不扩大双实例无感切换。

#### Scenario: HLC-S011 隔离切换恢复
- **WHEN** 隔离控制器停止旧Hub并启动新Hub，20个假helper及SSE/mux/WS按既有协议重连
- **THEN** 分别输出各时间点，helper/SSE/普通WS满足上述已定上限；mux自动格照实记录旧游标gap后停止输出，并证明gap送达，显式resync格按实际客户端同feed reset_scope与新iframe_instance流程在操作后5秒内恢复、缺失及重复均0，另记stop到输出时长；跨纪元gap显式可见，不把流原200/101记为没有中断

### Requirement: HLC-008 验收矩阵与故障反证
后续实现MUST 执行design中HLC-S001～013全部矩阵，记录真实进程退出码、信号至退出时长和逐类清理/持久性证据。负载格MUST 随机顺序重复50次并做注册竞争race检查；外部计时误差上限0.5秒必须单列，不能增加应用预算。撤主动取消/WS关闭、注册屏障、内网错误采集的变异MUST 在对应断言红，未执行或无效变异不得标PASS。

#### Scenario: HLC-S006 混合正常排空
- **WHEN** 100个有限HTTP、100个SSE、20个mux/bridge、20个WS混合运行并收到SIGTERM，另覆盖飞书回调在途及封口后迟到两种情形
- **THEN** 15秒内真实退出0、业务处理器与已准入回调结束且持久待办可恢复，双端监听同时停止满足100ms差值；拒绝计数和封口后DB访问计数可核、后者为0，撤回调封口必须在DB断言红；50轮附逐轮原始证据；SDK固定Start等待帧单列随进程回收并从业务goroutine归零断言排除，不冒称所有SDK goroutine已join


### Requirement: HLC-009 排空期间看门狗连续性
Hub MUST 选择排空期间继续喂狗直至最终退出，使用独立于业务runCtx且受原阶段/总截止约束的单一生命周期，不依赖systemd停止期停用watchdog。启用窗口W时MUST 在停止开始立即通知、发送周期≤W/4且实收最大间隔≤W/3，覆盖DB与终态记录；失败保留结果，正常运行锁健康判据不放宽。MUST NOT 因继续喂狗延长原停机截止，阶段或总截止失守须按失败契约退出。目标systemd259停止期不执行看门狗，W=12秒与15秒预算冲突已由ed797ae6实测解决；本要求作为跨版本防御保留，停止期强制兜底只有TimeoutStopSec=30s→SIGKILL，MUST NOT 依赖停止期watchdog兜底。

#### Scenario: HLC-S013 超过12秒不得被SIGABRT打断
- **WHEN** 隔离监督器按12秒窗口在停止期继续检测，Hub各阶段均未超时且总排空13～15秒；另格仅撤除t0后继续喂狗
- **THEN** 原格真实退出0、无SIGABRT、直到退出的通知实收间隔≤4秒；变异在最后通知后12秒被SIGABRT终止且连续通知/未被打断断言红，恢复喂狗后绿，目标systemd停止期行为另测不冒称已核

### Requirement: HLC-010 对端先断的WS关闭结果
Hub MUST 在停机关闭WS时按错误类型或哨兵识别对端已关闭结果，记录already_closed计数和逐条原错，不计入drain失败；写超时及仍存活连接关闭失败MUST 保留原错并按原契约退1，阶段截止仍退2。禁止靠交换夹具次序或字符串模糊匹配消除错误。

#### Scenario: HLC-S014 对端先断
- **WHEN** 已登记的20个会话WS客户端先关闭，随即SIGTERM，注册注销与停机快照竞争
- **THEN** Hub退0，already_closed为仍在关闭集合中的20条，逐条原错可见；保持客户端存活先停Hub另作退0对照；撤掉分类的变异须在退0断言红且实际退1；注入存活连接写关闭帧超时须退1
