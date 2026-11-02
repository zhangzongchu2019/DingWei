# S-HLC-IMPL S006／S011 首轮中途记录

本件不是完整交件、不申请评审或发布；源自 ee7be94dbb3069c5e25c8947266be11e4f1b7798 的真实装配，新增测试夹具，未变更产品实现。执行者 coder56solzzc.zzc，2026-11-02 UTC。

## 隔离与装配

入口 `python3 tests/hlc/process_matrix.py --case S006|S011 --output <隔离证据目录>`；普通 dev UID1001，白名单环境、临时 HOME/SQLite、pgtest 实际临时 PG、三台实际 Python View Service、随机 loopback 双监听、二十个凭据认证的假 helper WS；不连接现网或预发，无真实凭据。测试 overlay 仅在 run 装配后插入负载工厂、在飞书包插入测试传输，**不替换停机协调器、HTTP/SSE/mux/bridge/WS handler、队列或存储关闭路径**；SDK 实际 Close 连接另有既存独立进程夹具，不把本件 fake SDK 传输说成真实飞书连接。

固定混合负载为100个有限HTTP（内外各50，SIGTERM后2秒释放后真实提交）、100个真实内网SSE、5个public mux＋15个签名反向bridge（合计20）、20个会话WS，另有1个viewer WS及1个真实dispatcher在途飞书回调；迟到回调在封口后注入。现有单账号单transport和全局最多8 feed限制均不修改；5个测试属主各1个mux，每个mux跨3个VS目标形成15个bridge。最初夹具两次未到达停机：控制ID不符既有格式（400）、viewer WS缺Origin（403），原日志保留，修夹具不改变产品或验收值。

## S006 首个完整负载结果

`attempt03/S006-receipt.json` rc0，01:42:35.237356Z～01:42:45.267318Z；真实waitpid退出0、信号到退出2131ms。五阶段access/drain/queue/storage/finalize分别0/2119/0/7/0ms；双listener关闭差45.526us，均在信号后1秒内；两个Shutdown原结果均nil。完整报告和各阶段快照见同目录S006-shutdown.json、S006-process.log。

末态Hub业务处理器、所有流/WS/注册、后台计数、HTTP及生产/消费工作均0。100个HTTP均204，退出后重新打开原SQLite核到100条独立提交。飞书business_db_calls=1、late_db_calls=0，回调active0、rejected1；SDKStart等待帧1独立单列，不冒称SDK全部goroutine已join。停机队列剩余0，持久快照message_queued=1保留而非清空；本格不把持久待办1当漏排空。

## S011 首轮为红：保留既有epoch_changed显式重同步协议

首轮s011-attempt01 rc1、旧进程停2129ms退出0；复核s011-attempt02同现象，诊断帧保留。补齐真实NOTIFY_SOCKET READY与逐帧单调读数的s011-timing仍rc1（不是放宽判据后的绿）：

- 控制器stop：01:46:30.368102323Z；旧进程退出01:46:32.531554889Z。
- 新listener可连观测01:46:32.570405023Z；真实READY通知01:46:32.570699305Z；stop→READY=2202ms。
- 20个假helper READY后36ms全回；SSE验证事件46ms，viewer WS验证事件46ms。
- 内网SSE显示replay_gap=1、epoch_changed，测试ID hlc-after-00及hlc-after-verify各1；缺失0、重复消息0、重复游标0。
- mux显示gap=1、cause=epoch_changed/reason=resync_required；hlc-after-01/02/03各1，旧游标对应agent00的hlc-after-00与hlc-after-verify均未输出（期望5条、实得3条、缺失2、重复0），5秒恢复判据为红。
- 未自动执行reset、未重写游标、未新增自动重投。现有console_mux_broker.js明确提示历史需显式重新同步；已向组长报告该协议与S011自动恢复≤5秒的契约疑点，delivery dw-dlv_30213f93175447ed7db62ae303ac584e。建议保留自动恢复红、另记显式重同步格，不自行改协议或验收口径。

s011-timing/S011-recovery.json保留全部原消息ID、游标、gap帧、收到时刻与分类计数，S011-shutdown.json含旧进程五阶段完整读数。最初两次诊断夹具尚未收集全时刻，其缺失不补造。

## 后续与交件估时

首轮之后按组长顺序，先进行S006真实混合负载随机50轮/race（非已完成，不拿旧回调50轮替代），再其它反向控制、PG严格相关整包、run-build-tests。整单暂估2026-11-02T04:30Z，需以S011契约裁定及实际PG整包/其余矩阵结果校准；有红项不冒称整单可验收。当前T04～11总项保持未闭合；不部署、不apply/archive、不提前启动R5/R14/R10/R9预告任务。
