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

现网证据锚67f55bbdf563b3f5d0345963eebf5f7122152afb：两次Stopping→exited分别10.011090、10.008936秒，error均context deadline exceeded；具体请求未知。定位及追加解释锚2b4b624c716676067f2a28297a7a0c3054e58f54。补充配置证据e43100e3bdf200caaa67a925e51ca5fd82287bff（prodops-r5-t08-facts-20261101/REPORT.md，2026-11-01T14:05～14:08Z）：预发dingwei-dw123-staging-hub.service与现网dingwei-dw123-hub.service有效TimeoutStopUSec均30s、fragment均为TimeoutStopSec=30s且都无drop-in。30秒是外层配置期限，不是停机完成证据；其它有效停止参数/信号交付差/控制器期限**待prodops补测**，不从源码默认值或无drop-in推导。

## 读取与依赖

先读本context、同目录spec、proposal/design/tasks；相关进行中r5/r11只作格式和范围参照。当前基线无已实现的平台能力时间线入口，不虚构时间线证据。本变更来源链接集中proposal。仓库只经Git交换，日志不包含凭据、正文或用户内容；本单没有生产连接、数据库写入或部署授权。
