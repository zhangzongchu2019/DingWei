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
