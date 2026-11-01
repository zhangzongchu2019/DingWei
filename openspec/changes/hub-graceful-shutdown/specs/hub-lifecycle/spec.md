# Hub Lifecycle

## Purpose

本变更把 Hub 的计划停机从串行共享十秒截止且缺少长流清理和完整结果记录，改为两个监听同时停止接入、主动取消长流并有界收尾的协议，目的是区分正常排空、超时和其它故障并保留持久工作。涉及 Hub、HTTP server、队列与存储生命周期模块，不影响 R5 阅读和 R11 冻结规则。

## ADDED Requirements

### Requirement: HLC-001 双端停止接入与单次协调
Hub MUST 在首次SIGTERM/SIGINT后以同一单调起点协调停机，原子关闭新业务/流/WS准入，并发停止内外两台HTTP server监听，不等待一台排空才停另一台。按待批候选值，两端距信号均不超过1秒、关闭时差不超过100ms。已接入但未获准入者MUST 返回503；重复信号不得重置总截止或重复资源关闭。

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
Hub MUST 先停止新生产和新准入，保留已接受有限请求提交/回滚所需存储；随后停止领取队列项并完成在途确认或保留可恢复持久项，再结束存储使用者，最后关SQLite/PG。MUST NOT 提前确认未提交工作、清空持久队列伪造排空或在仍有使用者时关DB并报成功；无法证明持久性MUST 失败可见。

#### Scenario: HLC-S002 有限慢请求
- **WHEN** 内外各1个已接受的有限HTTP请求在SIGTERM后2秒完成事务
- **THEN** 两请求能完整提交且重启可核，DB在使用者退出后关闭，5秒内退出0

#### Scenario: HLC-S008 持久化失败
- **WHEN** 无超时但队列确认或事务提交被注入失败
- **THEN** 15秒内以其它故障非零码退出，未提交项没有成功确认记录，日志保留故障与持久状态，DB关闭顺序可核

### Requirement: HLC-005 预算与可区分退出
Hub MUST 使用明确总预算与阶段预算、按时全部收尾退出0，任一阶段超时以独立非零码退出，其它关停失败非零；两者并存保留全部原因并优先超时分类。MUST NOT 吞deadline或用SuccessExitStatus掩盖。候选总15秒、阶段1/8/3/1/2秒、超时码2及其它失败1须PM冻结；外层S=30秒已由prodops实测预发/现网有效TimeoutStopSec且都无drop-in；MUST 以30秒为外层上限，补测systemd停止计时到信号接收差δ及其它停止参数/控制器期限，核δ+T+5≤30；配置值MUST NOT 代替真实停机计时或允许用满30秒，不得自动改配置。

#### Scenario: HLC-S003 不释放HTTP
- **WHEN** 内外各1个活动请求保持到排空阶段9秒绝对截止
- **THEN** 分别记录Shutdown结果与剩余连接、强制关闭且不洗白timeout，15秒内以候选超时码2退出

#### Scenario: HLC-S007 混合超时
- **WHEN** 混合负载只剩1个不响应取消的内网SSE与1个公网HTTP
- **THEN** 超时快照记录对应类别及数量、强制动作和原错误均可核，15秒内以超时码退出而非0

### Requirement: HLC-006 阶段结果完整保留
Hub MUST 记录每阶段起止UTC、单调耗时、预算、超时、强制关闭及结果，并独立保留内外Shutdown原错误与Hub.Close/长流/队列/存储结果。终态MUST 含退出分类/码、总耗时、所有错误及剩余连接类别/数量、在途及持久待办；未知显式标注，不把总连接与流子类重复相加。MUST NOT 记录请求正文、凭据或敏感查询串；日志自身阻塞不得无限延长退出。

#### Scenario: HLC-S012 内网错误不丢失
- **WHEN** 注入internal Shutdown非deadline错误而public正常完成
- **THEN** 两个原结果同时保留，终态为failure且候选码1，15秒内退出；撤除内网错误采集后用例在结果完整性断言红

### Requirement: HLC-007 切换影响与恢复可测
停机验收MUST 分别记录控制器stop、监听关闭、旧PID退出、新监听可连、新ready及客户端恢复时刻，不能将stop完成到ready或单条502当全部不可用时长。MUST 明示新连接可见502/拒绝或准入503，SSE可见EOF、WS可见关闭/断开，沿用现有重连和gap协议，不自动重投无幂等保障的写请求。候选stop到ready≤20秒，ready后20个helper及各类流恢复各≤5秒，须PM冻结。

#### Scenario: HLC-S011 隔离切换恢复
- **WHEN** 隔离控制器停止旧Hub并启动新Hub，20个假helper及SSE/mux/WS按既有协议重连
- **THEN** 分别输出各时间点，满足上述候选上限，可核测试消息缺失及重复显示均0，跨纪元gap显式可见，不把流原200/101记为没有中断

### Requirement: HLC-008 验收矩阵与故障反证
后续实现MUST 执行design中HLC-S001～012全部矩阵，记录真实进程退出码、信号至退出时长和逐类清理/持久性证据。负载格MUST 随机顺序重复50次并做注册竞争race检查；外部计时误差上限0.5秒必须单列，不能增加应用预算。撤主动取消/WS关闭、注册屏障、内网错误采集的变异MUST 在对应断言红，未执行或无效变异不得标PASS。

#### Scenario: HLC-S006 混合正常排空
- **WHEN** 100个有限HTTP、100个SSE、20个mux/bridge、20个WS混合运行并收到SIGTERM
- **THEN** 15秒内真实退出0、各处理器结束且持久待办可恢复，双端监听同时停止满足100ms差值；50轮均通过并附逐轮原始证据，变异反证另有对应红读数
