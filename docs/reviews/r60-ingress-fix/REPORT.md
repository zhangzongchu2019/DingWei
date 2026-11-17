# S-R60-INGRESS-FIX：作者门齐，待组长复核（不构成发布GO）

**当前状态（2026-11-17T21:30:50.688962+00:00）：作者门已按组长21:00:33Z修订口径收齐，提交组长复核，不自验收、不部署。** 全新连续五轮m8整包5/5全绿，逐轮见文末；之前四轮第四轮STOP与所有失败原样保留，未沿用、未择优。其它门见20:01Z续跑节。产品/测试仍a24bb163；本次只追加docs/openspec，不改store/产品/测试，不自行新增例外。下文20:07Z与20:01Z交接段是历史状态，以本段及文末新门记录为当前状态。

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

原始四轮完整JSON日志以gzip无损存于stopped/gates，metadata中的raw_sha256校验解压后的原始字节。本段为20:07Z停门快照：当时第五轮、其它Go包树整包、候选focused race、独立逐名联合/双CLI门、浏览器全量、最终全仓strict均未继续执行，不记绿。20:01Z裁定所允许的非Z-3后续读数另节追加；本段四轮原始记录不改。没有挑选最好五轮，也没有对新增失败做定向重跑或产品二分。原两例允许名单保持不变。

## 20:07Z已取得的本源局部证据（历史快照）

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

## 20:01Z历史交接与预发边界

组长20:01:27Z已裁定新增落点交togpt60单流只读定因及9818/a24频率对照（S-Z3-NEW-SITE，11-18T06Z），本席只继续获准的非Z-3门。本席不自行扩名单、不另做二分、不动store/驱动，不做风险接受。T-04/T-05保持未完成。

STAGING-PLAN.md附现有stagingtest先经业主预发Console升39的方案、包期限及负载/回退安全闸；未执行。0077回退约束不变。当前只交代码和停门证据，未请求也未获得集成/构建发布件/预发/现网GO。

## 20:01Z裁定后的非Z-3续跑（当时Z-3仍STOP）

依据组长2026-11-17T20:01:27Z裁定，产品/测试候选仍为a24bb163，不修改产品；以下是获准续跑的独立门，不能替代Z-3五轮门，不构成整体PASS。togpt60负责S-Z3-NEW-SITE，本席未二分、未改store、未扩名单、未跑第五轮。

| 门 | UTC开始～结束 | rc | 读数 |
|---|---|---:|---|
| go-other-trees | 2026-11-17T20:12:59.174140+00:00 ～ 2026-11-17T20:19:38.339044+00:00 | 0 | 顶层631 / 含子项1033 PASS，12 SKIP，0 FAIL，interrupted=0 |
| focused-race | 2026-11-17T20:19:38.423106+00:00 ～ 2026-11-17T20:27:32.093397+00:00 | 0 | 顶层66 / 含子项79 PASS，0 SKIP，0 FAIL，interrupted=0，DATA RACE=0（focused，非整包race） |
| joint-and-dual-cli | 2026-11-17T20:27:32.137098+00:00 ～ 2026-11-17T20:27:56.046722+00:00 | 0 | 顶层2 / 含子项2 PASS，0 SKIP，0 FAIL，interrupted=0；两格逐名执行、0 SKIP |
| browser | 2026-11-17T20:38:38.889196+00:00 ～ 2026-11-17T20:48:53.435260+00:00 | 0 | 45/45 suite rc0，aggregate_rc=0 |
| strict | 2026-11-17T20:48:53.486379+00:00 ～ 2026-11-17T20:48:53.951387+00:00 | 0 | 全仓strict 41 passed / 0 failed |

Go其它树范围：internal/store/...、internal/obs/...、cmd/workpulse/...，普通用户、env白名单、PG strict、-p 1；m8整包仅原四轮，不补第五轮。12条SKIP逐名保存在go-other-trees.json，不混入PASS。focused race筛选式和所有命令/环境在对应metadata，明确不是全包race。

联合格 TestR60OutboundControlBackpressureRecoversWithSendStatus 与 TestHostedBodySendSameRequestIDAcrossCLIProcesses 均在显式DW_DESKTOP_INTEGRATION_PYTHON下逐名PASS/0 SKIP。浏览器PATH含go，NODE_PATH仅指向隔离HOME中的已装Playwright1.56.1；无共享或源码配置修改。

### 浏览器首轮失败与同源完整重跑

首轮完整45套件aggregate_rc=2：44套件rc0，mcp_zero_consent因显式要求NODE_PATH而当前未设置，报ENVIRONMENT_UNAVAILABLE/rc3。不是产品断言红，也不把它改SKIP或抹去；non-z3-browser-first保存全量原日志、metadata及receipt。补显式NODE_PATH=/tmp/rif-home/.node_modules后，同源完整runner重跑45/45、aggregate_rc=0，未以单套件补跑替代完整门。

### 两把集成删保护及还原基线

完整续跑结束后才在独立a24工作树执行，避免与门并发：去OutboundReceiptWriteFailed计数，联合恢复格业务断言红；heartbeat_handler去current包装，真实重连Python格业务断言红。两刀build_rc=0，原日志/断言在non-z3/integration-cuts，各还原后Go联合格与Python重连基线rc0，工作树clean。两刀与已有28+28证据分开，不合并宣称互不重叠的新保护。

VERIFY.py同时核原四轮STOP、候选产品字节、非Z-3五门原日志SHA、首轮浏览器失败、完整重跑回执、两刀及还原基线；当时成功输出为NOT_FULL_PASS，T-04/T-05未完成、等Z-3定因/组长另令；不请求集成、发布构建、预发或现网动作。

## 21:00:33Z新裁定：全新五轮（不沿用旧四轮）

组长据togpt60基线复现记录，明确准许三格同机制interrupted(9)，且须至少三轮整包rc0/0 FAIL；名单外FAIL、非interrupted FAIL或全绿少于三轮即停报。名单只由组长裁定新增，不推广为忽略SQLite错误。固定SOURCE_COMMIT=a24bb163，普通用户、env白名单、PG strict、-p 1、-count=1；显式隔离Python。五轮顺序连续，无定向补跑或其它门穿插。

| 新轮次 | UTC开始～结束 | rc | 顶层PASS | 含子项PASS | SKIP | FAIL | interrupted |
|---|---|---:|---:|---:|---:|---|---|
| 1 | 2026-11-17T21:01:39.402360+00:00 ～ 2026-11-17T21:07:29.328912+00:00 | 0 | 985 | 1654 | 29 | 0 | 0 |
| 2 | 2026-11-17T21:07:29.465247+00:00 ～ 2026-11-17T21:13:15.538258+00:00 | 0 | 985 | 1654 | 29 | 0 | 0 |
| 3 | 2026-11-17T21:13:15.675035+00:00 ～ 2026-11-17T21:19:01.034610+00:00 | 0 | 985 | 1654 | 29 | 0 | 0 |
| 4 | 2026-11-17T21:19:01.166692+00:00 ～ 2026-11-17T21:24:44.820974+00:00 | 0 | 985 | 1654 | 29 | 0 | 0 |
| 5 | 2026-11-17T21:24:44.952521+00:00 ～ 2026-11-17T21:30:28.552085+00:00 | 0 | 985 | 1654 | 29 | 0 | 0 |

全新五轮全绿5/5，达到至少三轮门槛；本段只报作者门满足，不据五个样本推断零故障率或生产概率。旧四轮不纳入分母、不改FAIL为SKIP。各轮逐名SKIP/完整JSON日志/命令/UTC/环境/raw_sha256与逐轮assessment、POLICY、COMPLETE、实际runner均存z3-revised-five/。

### 定因报告与统计边界

独立从Git取得togpt60lunazzc/z3-new-site 43ec0a67f0ef7862139d4831b69c2a511a90b4ee，阅读docs/reviews/z3-new-site/REPORT.md并核SHA256SUMS所列23项原始日志的Git对象SHA全部一致，引用坐标及哈希见causation-reference.json。本席没有重复其基线频率/驱动定因实验，也不把那些轮次混成本席新五轮。

报告记录基线9818的四批11/200及未配对补样13/200，合计24/400；候选15/200。区间重叠、样本没有提供可归因的概率变化证据；不把区间重叠写成已证明等概率。驱动迟到连接级interrupt机制与落点一致，但单凭失败行不能确定哪个短ctx发出interrupt。报告建议原名单保留、等待组长批准；21:00:33Z组长随后明确把3505加入三格，本席按后发裁定执行，未自行改白名单。

### 全件交审边界

本源canonical完整重跑、四树Go范围、候选focused race、完整浏览器重跑、strict及联合/双CLI逐名0 SKIP均在前述证据；首轮canonical目录清理失败、首轮浏览器缺NODE_PATH及历史第四轮STOP原样保留。新增28刀、原②28刀本源重放、两把集成刀各自分列，不混称58种独立保护。源码current/97/provenance及VERSION40不变。

预发计划STAGING-PLAN.md未执行；真实39升级仍需业主Console操作、构建发布件及预发GO仍另令，0077回退安全闸不变。T-04/T-05本席交件工作完成，不代表别席独立复核或组长验收通过。不改r60-integration/main，不发布、不接触现网。
