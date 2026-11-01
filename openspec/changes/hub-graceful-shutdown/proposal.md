# Proposal

## Why

现有 Hub 在计划 SIGTERM 后顺序关闭内网、公网 HTTP server，共用十秒截止且丢弃内网错误；运行上下文取消没有自动解除 HTTP 长流，也没有接入 Hub.Close。现网两次约十秒后以 context deadline exceeded 退出 1，无法从记录识别阻塞类别。需要定义可验证的双监听停止、长流清理、持久队列收尾及失败契约，避免把计划排空、超时和其它故障混为一谈，也避免用成功状态掩盖未完成清理。

## What Changes

- 新增 hub-lifecycle 能力 HLC-001～009：单次停机协调、双 server 同时停止接入、注册屏障、流取消/WS 关闭、生产者/队列/DB 顺序、预算与退出、阶段结果和切换验收。
- 建议正常完成退出 0，超时专用非零值 2，其它关停或持久化失败 1；具体非零值与预算待 PM 决策，绝不忽略 deadline 或放宽 SuccessExitStatus。
- 提供明确的候选预算、慢请求/活跃流/混合/超时矩阵及撤掉主动取消的反向控制；所有产品场景均尚未执行。
- 仅写 r47 候选提案。基于 r46 2af469dd6919d8eb6064aa7da0e646f0851becf8；不改产品代码、r46 集成分支、版本、部署、systemd 或控制器。

## Capabilities

### New Capabilities

- `hub-lifecycle`：将成功启动后的计划停机从隐式进程退出改成可观测、有界、保持持久性边界的协议。现状最小基线和代码上下文放在本变更 specs/hub-lifecycle/context.md；主规格当前无此能力，不把拟议行为写成现行承诺。

### Modified Capabilities

无；不修改 R5 阅读、R11 冻结或 r46 功能契约。

## Impact

后续实现涉及 cmd/workpulse 的 run/execute/main、两个 HTTP server、Hub.Close 及连接注册、View 内网 SSE、Console mux/bridge、会话 WS、后台生产者和队列、SQLite/PG 关闭。客户端既有重连与游标协议继续使用，不增加本文自造的线上事件类型、HTTP 路由或 helper 协议。单实例 stop-then-start 必有空档；本提案不承诺零中断或可用性百分比。独立评审、PM 冻结、prodops 实测和实现派单均是后续前置。

## 来源

需求来源：组长 opus50tc2k.zzc 于 2026-11-01 派单 **S-HUB-GRACEFUL-SHUTDOWN-PROPOSAL**（本次平台派单；精确消息 ID 未提供，不补造）。定位依据：[停机定位与补遗](https://git.code.tencent.com/wego-ai/dingwei/blob/2b4b624c716676067f2a28297a7a0c3054e58f54/docs/reviews/hub-sigterm-exit1/REPORT.md)。现网事实：[SIGTERM journal](https://git.code.tencent.com/wego-ai/dingwei/blob/67f55bbdf563b3f5d0345963eebf5f7122152afb/docs/reviews/prodops-r45-prod-closeout-20261101/SIGTERM-EXIT1-JOURNAL.md) 与同目录 REPORT.md；现网阻塞的具体请求仍未知。来源不是从 Git 作者推导。

补充配置事实：[prodops 预发/现网有效单位实测](https://git.code.tencent.com/wego-ai/dingwei/blob/e43100e3bdf200caaa67a925e51ca5fd82287bff/docs/reviews/prodops-r5-t08-facts-20261101/REPORT.md)，2026-11-01T14:05～14:08Z只读检查：dingwei-dw123-staging-hub.service与dingwei-dw123-hub.service有效TimeoutStopUSec均为30s，两个fragment均写TimeoutStopSec=30s，DropInPaths均为空。仅引用停机配置事实，不引入同报告的凭据交付任务。

## 待确认假设

- HLC-A01（停止超时配置已核）：prodops固定证据e43100e3已核预发/现网两个Hub单元有效TimeoutStopSec均30s且无drop-in，S=30满足S≥T+5（T=15秒候选），名义余量15秒；实际仍计入信号交付差δ，见design。**仍待实测**KillMode、SendSIGKILL、WatchdogSec在停止期的行为、控制器stop轮询期限；r45控制器实际等了约10秒只是一次观测，不是其上限，须核上限不短于S=30秒。30秒配置不是实际停机达标证据。
- HLC-A02：候选容量（100 个有限 HTTP、100 个 SSE、20 个 mux、20 个会话 WS）可在 8 秒并行排空阶段完成。仅为明确可测的首轮压测规模，不冒称生产峰值或已有实测；须和在线连接数核对。
- HLC-A03：现有队列的持久确认点、重投/去重语义可支撑“停止消费后保留未完成项”。逐队列代码盘点与故障重启验证前不宣称无丢失；无法证明持久性应报失败，不能清空队列后报成功。
- HLC-A04：客户端识别 EOF/WS 关闭并按现行重连协议恢复。必须实测 Console、View、helper，各流已收到 200/101 后的断开不会自动变成新 5xx；不承诺 WS 必有正常关闭码。

- HLC-A05（看门狗方案待实测）：本提案选择**排空期间继续喂狗到最终退出**，不依赖“systemd停止期间必然禁用看门狗”。模板deploy/workpulse.service为WatchdogSec=12s、WatchdogSignal=SIGABRT；该模板不是两个现网单元的有效值证明。待验证独立停机喂狗生命周期、真实通知接收间隔及systemd停止期行为；新增HLC-S013覆盖13～15秒收尾与停喂反证。若目标systemd停止期不执行watchdog，实际不触发只能作为环境事实，不能代替选定继续喂狗方案的通知间隔验证。

## 待 PM 决策

| 编号 | 选项 | 建议与理由 | 冻结条件 |
| --- | --- | --- | --- |
| HLC-PM01 | 超时用 2；或其它保留非零值 | 建议 2，正常 0、其它故障 1，便于控制器区分；若已占用则选另一独立非零值，不映射成成功 | 核 CLI/控制器现有码，PM 确认；确认前 2 仅是候选 |
| HLC-PM02 | 候选总预算 15 秒；或按实测负载另定 | 建议 1+8+3+1+2 秒阶段分配；现有 WS 等待最多 5 秒，8 秒阶段留收尾空间；尾部预留 2 秒避免超时后失去记录/退出机会 | 外层S=30秒已核；建议保留T=15秒，名义余量15秒，并核δ+T+5≤30及控制器期限覆盖外层等待；若不满足必须重议，不自动调systemd或用满30秒 |
| HLC-PM03 | 排空期间新请求拒绝并由客户端重试；或扩大到无感滚动部署 | 建议本轮只做单实例有界停止，已接受有限请求尽量完成、长流主动断开；不引入双实例/自动业务重试，避免扩大幂等性风险 | 确认临时不可用、流重连体验及测试目标；写请求只在已有幂等协议下重投 |
| HLC-PM04 | 本地冷启动目标 5 秒/恢复 5 秒；或据实测放宽 | 建议按 design 的 stop→ready≤20 秒、helper≤5秒重连、流≤5秒恢复作为候选验收目标；不能把现网一次约350ms回执差当作SLO | 实测启动/客户端退避，PM 定值；不满足不能删格或声称通过 |

PM 冻结前，本提案的数值用于离线夹具设计与评审，不是已批准的生产参数；实施与部署均须另行派单。
