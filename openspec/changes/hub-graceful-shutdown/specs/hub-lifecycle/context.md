# hub-lifecycle 代码上下文与最小现状基线

本能力尚无主规格；本件位于获派变更目录，记录2af469dd6919d8eb6064aa7da0e646f0851becf8的现状而非宣告新增需求已实现。实施前须按冻结变更补齐openspec/specs/hub-lifecycle主规格与context，不在本次越出指定目录改写主规格。

## 最小基线（当前行为）

- cmd/workpulse/main.go：54～59统一错误映射退出1；432～441收到SIGINT/SIGTERM后新建10秒shutCtx、cancelRun、顺序internalSrv.Shutdown（忽略错误）再return srv.Shutdown。两台Server没有绑定停机取消的BaseContext/RegisterOnShutdown。
- internal/m8/hub.go：Hub.Close关闭Console mux/AMR/清理goroutine/会话WS并等待，现有WS等待最多5秒只记日志；调用要求无新连接注册，部分其它等待无截止。run未调用它。
- internal/m8/terminal.go：HandleInternalViewEvents的SSE等待请求context、订阅与ping；runCtx取消不会自动解除请求。
- internal/m8/console_mux_transport.go、console_mux.go、console_mux_frames.go：mux流/reader依赖各自context与Hub清理；view-service/view_service.py通过HUB_INTERNAL访问内网事件流/bridge，现有EOF/重连行为须按实际客户端验证。
- main.go的processor/L2/task-flow/outbound/receiver/reminder/scheduler使用运行context；SQLite/新平台PG在run退出defer关闭。当前没有统一的持久工作收尾证明或分阶段结果。
- Go net/http Shutdown先关该server监听再等活动HTTP；不自动等待或关闭hijacked WS。不能将“一个Shutdown开始”理解为两台server都已关监听。
- deploy/workpulse.service模板WatchdogSec=12s、WatchdogSignal=SIGABRT；cmd/workpulse/hardening.go:startSystemdWatchdog绑定runCtx，cancelRun会使喂狗循环退出。模板值不是两个环境有效watchdog配置或停止期行为证据；拟议独立停机喂狗尚未实现。

## 现场事实与边界

历史证据锚67f55bbdf563b3f5d0345963eebf5f7122152afb记r44/r45两次约10秒、error=context deadline exceeded，具体请求未知；本轮精确计时统一引用下述ed797ae6原件，不混用不同摘录的起止时间戳。定位及追加解释锚2b4b624c716676067f2a28297a7a0c3054e58f54。e43100e3bdf200caaa67a925e51ca5fd82287bff（prodops-r5-t08-facts-20261101/REPORT.md，2026-11-01T14:05～14:08Z）已核预发dingwei-dw123-staging-hub.service与现网dingwei-dw123-hub.service有效TimeoutStopUSec均30s、fragment均为TimeoutStopSec=30s且都无drop-in。30秒是外层配置期限，不是Hub停机完成证据。

### T-03补测已闭合事实与适用边界

- Git锚c90f41c2a3e090b34ad17070f5d8beed589cbd7d，review/prodops/public-timeout-hub-stop-facts-20261101，docs/reviews/prodops-public-timeout-hub-stop-facts-20261101/REPORT.md B节：预发/现网有效KillMode=control-group、KillSignal=SIGTERM、SendSIGKILL=yes、FinalKillSignal=SIGKILL、WatchdogSec=12s/WatchdogSignal=SIGABRT、TimeoutStopSec=30s。r45控制器继承stop_hub，systemctl stop命令期限40秒，返回后才轮询30秒/间隔1秒；无旧PID与扫描错误等条件才通过，失败进入restore_old/safety_hold。40+30秒是顺序两段上限，不是应用新增预算。
- Git锚ed797ae6de1f49cc2ad755946f1f3db2147bd8af（父4bfcca6b899665ec24b6b84033692c83742b4850），review/prodops/hlc-t03-watchdog-facts-20261101，docs/reviews/prodops-hlc-t03-watchdog-facts-20261101/REPORT.md及raw：目标dw123为systemd259（259.5-0ubuntu3），在同一systemd实例临时单元复刻W=12s、TimeoutStopSec=30s、KillMode=control-group；未停止/改写真实Hub或View。A停止期不喂狗25.024786秒、B持续喂狗24.125013秒均正常退出，无SIGABRT/SIGKILL；journal的Deactivated successfully为直接终止证据，stop后LoadState=not-found的show默认值不单独作退出码证据。临时单元已清理，journal保留。
- 目标systemd259停止期不执行看门狗，模板W=12秒与15秒预算的目标环境冲突已由ed797ae6实测解决，不构成冲突。按组长本单裁定，HLC-009作为跨版本可移植、成本低的防御要求保留；不得把停止期watchdog当作兜底，最终强制兜底只有TimeoutStopSec=30s→SIGKILL（control-group）。A/B未触及30秒，不能冒称已实触SIGKILL边界。
- HLC-S013“隔离监督器”格和反向控制原文不变。目标systemd不在停止期检测；S013只验Hub自身通知连续性，不从隔离监督器行为推断systemd行为，也不以A未收SIGABRT宣称Hub喂狗成功。所有Hub生命周期场景及通知交接仍未实现或执行。
- C五次立即退出样本：stop请求→返回0.106086～0.106635秒（均值0.106244秒），TERM trap为请求后0.009721～0.010681秒；均为命令/日志观测，不是内核信号时间。按组长要求保守取整个停用开销δ≈0.11秒，δ+T+5≈0.11+15+5=20.11≤30，余量14.89秒，保留5秒后再余9.89秒。该样本不作为任意负载下δ硬上界，GO前须复核漂移；该计时样本本身不批准参数；T=15秒及其它PM值随后由rulings㉒另行批准。
- ed797ae6/raw/READONLY.txt的2026-11-01T21:08:12Z现网Hub PID1017258快照：本地8793端口ESTABLISHED 4、443端口1、28个临时端口各1。只计套接字，不推断SSE/WS/HTTP类别、不等于请求数或峰值，不把它当100HTTP/100SSE/20mux/20WS候选负载已通过。
- 本轮指定r44与r45切换的Stopping→Main process exited分别为10.009208秒（04:46:47.065074Z→04:46:57.074282Z）、10.011406秒（09:24:20.205647Z→09:24:30.217053Z），均status=1；依据ed797ae6/raw/READONLY.txt及VERSION-HISTORY.txt。此前01:27:53Z一轮不列作这两次切换。约10秒观测不等于控制器等待上限，亦不证明已满足拟议新停机协议。

此前T-03事实子项已闭合；本次S-R48-PM-WRITEBACK按rulings㉒回写HLC-PM01～04：退出0/2/1、15秒总预算及1/8/3/1/2阶段、单实例有界、stop→ready≤20秒/helper及推送流各≤5秒；proposal原选项保留，T-03总项闭合。T-04及以后未授权；不自开实现、apply/archive或部署。实测报告仅Git坐标引用，不向文件服务传源码。

## PM定值来源

[业主裁定rulings第㉒条](https://git.code.tencent.com/wego-ai/dingwei/blob/0659d208ce731b73206b06d587c3814649ae11d2/docs/rulings/S-TEAM-MIGRATE-HOSTED-20261017.md)：业主workpulse对话，Hub收信2026-11-01T23:07:47Z，delivery dw-dlv_f5170d3c5deb6106833dc76e7c36db1e；对“r48那11项是否全部按建议定”答原话“ok”。本单S-R48-PM-WRITEBACK只回写已定值；不是实现、apply/archive、迁移、预发或发布授权。

## 读取与依赖

先读本context、同目录spec、proposal/design/tasks；相关进行中r5/r11只作格式和范围参照。当前基线无已实现的平台能力时间线入口，不虚构时间线证据。本变更来源链接集中proposal。仓库只经Git交换，日志不包含凭据、正文或用户内容；本单没有生产连接、数据库写入或部署授权。

## S-HLC-IMPL 当前实施基线与边界

实现分支 opus50tc2k/hub-graceful-shutdown-impl 直接基于r47 c248506cf120c3af2a06eeaeeae3038f596537b1，通过非快进merge引入冻结规格3a73c794f2b2b321079a109e84adfd281088ab2d（本地引入提交ffaa6b27fb4f02660863e0ecc4e8bed41f4be387）；不是在r46产品上直接实现。T-04～T-11由S-HLC-IMPL另单授权，不部署、不apply/archive；此前未授权字样为历史。

按T-04组长裁定，运行期Enqueue语义与延迟不变，仍是best-effort，入站可能先于派生出站落库被确认；不声称跨动作原子提交。S008只验停机落库失败不被吞、退出1/超时2及计数/记录。既有运行期缺口单列docs/findings/queue-ack-durability-c248506c.md，不在本单修复。停机专用封口Flush须等待channel/批全部提交，失败或未知不能当成功，存储使用者未全部退出不得先关库。

未来切换控制器必须将 expected_sigterm_exit1 改为按正常退出0／超时退出2区分，其它失败1；控制器修改另单交prodops，本单只记录兼容条件，不修改其脚本或有效停止配置。

### T-04排尽实现与诊断边界

internal/bus/async_dbqueue.go的SealAndFlush为停机专用接口：封入队、逐批排尽channel及部分batch，保存首个提交原因；ShutdownCounts独立输出remaining/failed，包含未尝试项。等待准入锁也受调用截止约束。运行期Enqueue/Flush确认语义不变。cmd/workpulse/shutdown.go、main.go在3秒queue阶段调用它，实时队列计数和first_error分类在阶段超时竞争时仍可观测；未结束的写协程阻止DB关闭。

internal/bus/shutdown_test.go涵盖停机前多批仍在channel、首错/未提交计数、超时未尝试项及准入锁等待上限。cmd/workpulse/shutdown_queue_test.go涵盖真实队列接协调器的3秒阶段/关闭顺序和独立子进程实际0/1/2/2退出；只验证队列子范围，不代替完整Hub负载矩阵。撤排尽与覆盖首错用Go overlay替换已有源文件，默认vet/race开启，目标断言必须红。

FINDING附录记录基线overlay新增虚拟测试文件：第一次vet无法打开不存在路径而未执行，第二次仅诊断设vet=off，观察Enqueue=nil/Flush=nil。该诊断不是HLC验收，不能作为业务消息已丢失证明；业务出站与已发送回执区分处理。

## 当前SDK运行归属与适配边界

运行证据c15d6d7884f8452bf7717818f329ec192bca5094（tree2ca8a513ee5e7d1255069a235aa419c0eed783d8）已独立fetch核：现网FCS_RPC_URL不存在／FEISHU_TRANSPORT=ws／三次连接1且属于Hub单元，预发fake／0连接，无独立fcs单元；准确命令与UTC见同commit的docs/reviews/prodops-fcs-runtime-facts-20261101/REPORT.md。不报env原文、地址或凭据。

internal/feishu/lifecycle.go新增可选ReceiverLifecycle与SDK连接所有权、Close→回调Seal→Wait顺序及安全快照；feishu.go在规范化前登记回调，封口后拒绝计数。SDKStart运行帧单列sdk_start_not_joined，不进入Hub业务goroutine归零断言；v3.9.7 Start的select{}及其它SDK内部残余随进程回收，**不冒称所有SDK goroutine已join**。cmd/workpulse/main.go在drain阶段接入；远端FCS订阅类和协议未改，测试另核。

internal/feishu/lifecycle_test.go包含S006回调在途/迟到/真实临时SQLite计数、在途超时、Close阻塞、重复Close、停止后启动及多网关计数；tests/hlc/s006_mutations.py撤掉拒绝保护，须在DB访问断言红。lifecycle_sdk_process_test.go独立进程使用真实钉版SDK与loopback连接，默认skip但显式独立运行有读数；只排除明确SDK所有权的残余，不放宽业务回调归零。完整Hub混合流量与恢复验收仍需独立逐格，当前单测不能替代。

## drain 对端先断修复位置

独立分支 opus50tc2k/hlc-drain-exit1 基于bdac3593；按组长本次产品缺陷裁定实施，不改原实现树。internal/m8/lifecycle.go保存逐连接WSCloseSnapshot，与存活计数分开；cmd/workpulse/main.go将hub_ws_close放入每阶段及最终停机快照。新增internal/m8/lifecycle_close_test.go和tests/hlc/ws_close_mutation.py；process_matrix新增S014/S014-hub-first，S014仅在overlay固定跟踪集合以稳定覆盖注销竞争。证据目录docs/reviews/hlc-drain-exit1；临时HOME/IPC/SQLite/PG与端口均由独立runner记录。未改S006逐席ready屏障，42/8的历史409绝对时间缺项不可伪造。
