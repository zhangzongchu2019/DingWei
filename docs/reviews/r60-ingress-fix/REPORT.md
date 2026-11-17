# S-R60-INGRESS-FIX：已实现，Z-3门触发STOP（不是完整交件）

**状态：不得集成或发布。** 依据组长2026-11-17T01:37:18Z有效Z-3门，以及2026-11-17T18:04:11Z实现令第4项，第四轮m8整包在允许名单之外出现`interrupted (9)`，已停止第五轮及后续门；没有单跑补绿、二分、扩大白名单或修改store。待组长裁定。本文不出spec/code整体PASS，不构成预发/现网GO。

## 坐标与产品边界

仓库wego-ai/dingwei，分支`coder56solzzc/r60-ingress-fix`。产品/测试候选 `a24bb16335040ae06486f1c76be69ee542958214`，tree `6e1f91be7b7ef084af07746981f52c7cb53f84fe`，parent `1b5034c13c5cce917e3159dfcbf985f2918721ea`；base `9818b979e9e20f0bcfeffa6b78045dda71b1bd79`，VERSION保持0.2.3.40。核心产品代码从0b1c1dfba后未改，后两笔只增强测试；仍以a24新源读数为准，不把0b1检查点门冒充最终门。

实际产品变化只在internal/m8：session_ingress.go、hub.go、agent_ws.go与新增session_diag.go、session_write_policy.go；另有Go/旧helper测试夹具及openspec。cmd、tools、scripts、deploy、view-service、go.mod、go.sum、VERSION与9818逐字未变。没有改r60-integration/main，没有构建/上传发布件、没有操作预发或现网。组长17:29Z转述的现网回退状态只作背景，本席未读取当前现网，也不把本地反例当五次现场断连的逐条根因。

## 修订结果及候选参数

唯一Reader＋有序worker；每连接2048项、全Hub65536项，含正在读取/处理责任；32MiB/256MiB raw字节界保留。任何界耗尽仍明确过载，不静默丢canonical帧、不无限阻塞Reader。未做显示合并。

等槽与marshal检查预算2秒，取得写权后小控制物理5秒、数据20秒；更短父ctx仍优先，不扩大既有heartbeat/status的2秒或其它调用方5秒预算。等槽失败未触库写时不谎报物理关闭；可能部分发出的物理失败不得重用半消息连接，①具备能力的helper保持原source/delivery恢复，旧39不被假定拥有新outbox。

每连接session_end一次，overflow/物理失败各至多一次；固定原因/错误/帧类别、peer code或unknown、队列当前/峰值、等待写者与物理活跃状态，非阻塞单行4096字节、每Hub200条/s、失败/限流计DiagnosticDrop。第一观测不等于唯一因果，记录first_observed_reason/physical_write_reason以便同seq关联；不输出正文、URL、原始异常或凭据。reader_frame字段指最后完成的帧，不编造未读完帧的大小。

条件分片已满足：真实旧39＋10Mbps＋8MiB/3秒停读＋Ping竞争，不分片业务断言红；本源删分片仍红。采用单一JSON消息的32KiB片段，逻辑ID/正文不变。8MiB格实际峰值32768；不宣称所有小消息也被切成32KiB（≤64KiB保留单帧）。保活默认30s/5s未改。

参数未突破批准初值。22个Go WS回环生成器各1000条驻留时约22001项/93,742,151 raw字节，低于批准界；单连接1000条窗口保留余量。一次该进程的os.wait4精确子PID峰值RSS325252KiB（约318MiB），驻留时Go堆约109MiB；raw、堆与RSS分列，不外推RSS硬界或生产SLO。不是22个真实helper/模型。

## 第四轮STOP证据

唯一FAIL：`TestSessionNameReconnectKeepsNameAndReleaseReuses`，`internal/m8/hub_test.go:3505`，`interrupted (9)`，UTC **2026-11-17T19:55:19.995522632Z**。3505调用`waitEndpointInactive`，实际报错来自同文件6551的`db.ListSessionEndpoints(ctx)`、6553的`t.Fatal`。该测试文件相对9818未修改。它不在允许的两例内：

- TestNewSessionNameNotifiesFeishuOnceAndReconnectFlappingStaySilent
- TestViewMentionRoute/a_peer_without_delivery_ack_v1_is_not_sent_to

这是新落点的一次观测，不判已证同因、不称已证明基线flaky，也不推算失败率。相同轮次未见新RIF测试失败，但仍严格STOP，不能用前三轮绿推翻门。

| 轮次 | 整包窗口UTC | rc | 顶层PASS | 含子项PASS | SKIP | FAIL | interrupted行 |
|---|---|---:|---:|---:|---:|---:|---:|
| 1 | 2026-11-17T19:35:38.784310+00:00 ～ 2026-11-17T19:41:23.552785+00:00 | 0 | 985 | 1654 | 29 | 0 | 0 |
| 2 | 2026-11-17T19:41:23.638792+00:00 ～ 2026-11-17T19:47:05.801379+00:00 | 0 | 985 | 1654 | 29 | 0 | 0 |
| 3 | 2026-11-17T19:47:05.887174+00:00 ～ 2026-11-17T19:52:54.576196+00:00 | 0 | 985 | 1654 | 29 | 0 | 0 |
| 4 | 2026-11-17T19:52:54.663495+00:00 ～ 2026-11-17T19:58:38.223390+00:00 | 1 | 984 | 1653 | 29 | 1 | 1 |

原始四轮完整JSON日志以gzip无损存于stopped/gates，metadata中的raw_sha256校验解压后的原始字节。第五轮、其它Go包树整包、候选focused race、独立逐名联合/双CLI门、浏览器全量、最终全仓strict均未继续执行，不记绿。没有挑选最好五轮，也没有对新增失败做定向重跑或产品二分。原两例允许名单保持不变。

## 已取得的本源局部证据（不替代完整门）

- SOURCE_COMMIT=a24的build.sh完整重跑rc0：Claude/Codex各1270，Python27、shell2，inventory97；本机workpulse等仅供门读数，不作发布件。heartbeat_handler仍current、renderer保留retainCanonicalTrustedProvenance，版本三处仍40。
- R-D01：真实Git钉定旧39原session_event_loop，1000×4KiB、全部唯一事件ID/正文/顺序/磁盘历史游标完整；500/501之间ACK事务提交前无确认，正文不越过提交屏障。
- R-D02：通过实际routeToSession，保留调用方5秒预算，真实旧39停读3秒，8MiB与小控制两格保持连接、ID/SHA正确。小控制可先进入内核buffer，只断言恢复后完整收到。
- R-D03/04：原慢viewer隔离、readonly离线与正常关闭、Z-1两格四刀、真不回Pong/停止读取、Ping飞行中取消的格均在局部基线与已完成整包运行。无Pong格是本地可控失活形状，不冒称扫描了所有公网半开机制。
- R-D05：真实旧39在worker长业务被阻塞时，默认30s/5s周期约第30秒成功Pong；原slow_business/slow_viewer仍通过。
- R-D06：四轮整包内①②联合格与同ID双send.py进程均逐名PASS、0SKIP，stopped/named-cli-within-four-whole-rounds.json逐条保存；不将其冒充STOP后未执行的独立逐名门。另两把集成补刀因STOP未启动，不计已做。
- R-D07：连接/全局项数及字节精确界、active/pending取消释放、未触物理写的取消不关健康连接、日志满pipe/速率/长度/固定原因均有用例。
- 新增保护28刀＋原②28刀本源重放，均编译成功并业务断言红；有保护重叠，不合称56个不同保护。恢复同一工作树的Go/Python基线均rc0，状态clean。R-D01恢复64/破坏FIFO/提前ACK、R-D02恢复物理2秒、删除分片、删除资源界/释放、删除三类原因、Z-1四刀均有原日志。
- 检查点0b1的focused race48/0/0及strict41/0留在checkpoint目录，明确不是本源完整门的替代。

## 首轮失败与方法边界

a24首轮canonical rc1：仅Claude profile的BusyAdmissionTest.test_claude_bad_transport_old_handling在TemporaryDirectory.cleanup时报ENOTEMPTY，残留driver.db；Codex profile1270 rc0。tools/sessionhelper相对9818零差异；定向六次通过只能说明这六次，不证明根因或失败率。同源完整重跑rc0（仅证据/产物目录另分），原失败完整日志与SHA保留，不改FAIL为SKIP。

早期字段接入编译错误、PAX/owner夹具错误、pipe测试重复Fd导致阻塞并人工中止、GNU time缺失及一次compile cwd错误、修正汇总脚本和加强实际route覆盖前手动停止的预跑，均不计有效刀或完整门。stopped/attempts存可用原始文本；仓外local-run-artifacts保留其余过程件。调度器曾仅暂停本身等待删保护/还原基线，构建子进程继续；build metadata的utc_end是回收时刻，不能拿含等待的窗口当性能值。

10Mbps格是指定回环限速下行/同消息分片；未扫所有10/50Mbps上行大单帧形状、TLS公网/真实CLI输出分布/长时间22真实helper压力。旧39的大单帧上行仍有原方案第2节已披露的WS有序流限制，不悄悄扩大30/5，也不宣称任意大帧、弱网或无限突发不断。

## 后续与预发边界

组长已收到停门初报；请裁定是否由togpt60单流核新增落点或另定处置。本席不自行扩名单、不另做二分、不动store/驱动，不做风险接受。T-04/T-05保持未完成。

STAGING-PLAN.md附现有stagingtest先经业主预发Console升39的方案、包期限及负载/回退安全闸；未执行。0077回退约束不变。当前只交代码和停门证据，未请求也未获得集成/构建发布件/预发/现网GO。
