# R60② qwen 复核修订交件（当前入口，含组长Q6补正）

冻结产品／测试源 `3db78e5b244c4663fe67f7dcd44b74991057a7f6`，tree `c181cb6fd0df8148c8ebc2544ae6d1930588e70e`，parent `9849eef26ced6a378ecbaa026ad59bcf0dee055f`；基 main `d2cc47c7e699eb45efb3a07ee197aa8c20a9f07e`。分支 `coder56solzzc/r60-hub-keepalive`。本源在984的Q6双计数之上追加Q2状态写失败仍正常关闭，4文件／5插入／4删除；984相对3e5为8文件／136插入／30删除。保留eca的Q1/Q2修订。当前证据入口是 [evidence-q6](evidence-q6/README.md)，原evidence属于167、evidence-qwen属于已撤回中间eca/3e5；984完整门/27刀亦保留为中间历史，均不延用。交件 tip 为后续纯文档提交，以 Git 三值与交审回信为准。本文是作者门，等待 qwen 增量独立复验与组长终审，不是 PASS 签收或部署 GO。

## 评审逐项处置

独立只读报告 `review/qwen38mvb4/r60-hub-keepalive-20261116@d747a5145dd8ed608e22b66814211bda3f4fe46f` 已从 Git 取回；旧 167/d4eae 的结论保留 NEEDS_CHANGE；组长22:00:43Z已撤销旧167冻结并将原Hub侧接受收回为待改，新源仍须终审。评审自选 6 刀／独立跑门与作者 28 刀分开，绝不合并计数。

- Q-1：新增 `TestSessionKeepaliveCancellationDuringActualPing`，真实 WS 对端在 OnPingReceived 明确收到 Ping 且禁止 Pong 后才取消；断言无失败 journal、PingFail 或 keepalive_failed，并回收 Reader。不是先 cancel，也不靠固定 sleep。删除 Ping 返回后 ctx 取消保护，编译成功、业务断言红在 planned in-flight cancellation wrote failure journal；原 planned_cancel 格保留作前置取消对照。
- Q-2：恢复 legacy viewer 的 readonly／“会话已离线”状态与 1000/session offline 关闭协议。每既有 viewer 至多一个 Hub 管理的清理任务，Agent teardown 不串行等待慢 viewer；状态写仍含取槽＋I/O 共 1 秒。状态写失败仅标gap并继续尝试1000正常关闭，新增慢写槽格对该关闭码断言，删继续关闭保护须红。正常 Close 是库自己的最多 5 秒关闭帧＋5 秒等回应及内部退出等待，不能叫总握手 1 秒；生命周期超时如实报告。封口不能接清理工作时 gap＋CloseNow，不建游离 goroutine。删除 closeTerminalLocked 临关停白起 output worker 的代码。真实 Hub Agent＋viewer 回归同时断言状态帧和正常关闭码；双 viewer 格确认慢写槽不拖 Agent／健康 viewer。删除状态、改变关闭码、改同步清理各有编译成功业务红。
- Q-3：journal queue_items 改成预算实际 q.items（含正在读取／处理），与 bytes 同口径。
- Q-4：Reader／Pong／worker／journal 年龄统一 h.now()；实际 Ping 耗时仍用单调真实时钟。注入固定时钟回归钉住 UTC 与三个年龄；独立删除 journal/worker 时钟统一各红。
- Q-5：后台封口拒绝启动 viewer worker 时 stopped 在 q.mu 下写，标可见 gap 并关闭连接；专门封口回归与删 gap 刀已覆盖。
- Q-6：按组长22:00:43Z令改产品。outbound_receipt与delivery_ack_confirm各在c.write返回错误时分别累加session obs的outbound_receipt_write_fail与delivery_ack_confirm_write_fail；有限固定字段、无任意标签/正文/错误串，进程重启清零，不混diagnostic_drop，也不解释为丢信。2秒总写截止可能提高已提交但回执失败的unknown，①dw_send_status/原sender+source幂等负责兜底。新增真实WS写槽占用超2秒两子格：对应计数+1、另一计数与diagnostic_drop不变，持久投递/ACK仍在，释放背压后原source与delivery回执恢复、成功写不增计数；分别删两处计数均编译成功业务红。本源没有①helper outbox，完整①②慢读/背压→helper可见unknown/状态查询恢复联合格仍待正式集成前完成，不计PASS/SKIP，不能用这两格替代整条链路。
- Q-7：依组长令只记关停期观察，不改；撤回eca里touch后台拒绝退出的额外改动。

新增HKF-009/HKF-S015钉住控制回执观测与持久责任；保留 HKF-S011～S014 分别钉住离线协议、Ping 期间取消、预算／时钟、封口 viewer。此次保留 167 的 helper 分块字节与 terminal-wire 夹具、环境、原始数据不变；没有另跑 PM04 测量。原测量只证明合成突发/限速条件的控制延迟收益，不能解释历史419次或生产SLO；不限速正文吞吐下降的代价仍保留，发布说明须点明。

## 本源作者门

| 固定门 | rc | 顶层 PASS | 含各层子项 PASS | SKIP | FAIL |
|---|---:|---:|---:|---:|---:|
| canonical-final-review | 0 | 6 | 21 | 0 | 0 |
| go-final-review | 0 | 1559 | 2583 | 49 | 0 |
| race-final-review | 0 | 39 | 57 | 0 | 0 |
| build-final-review | 0 | 0 | 0 | 0 | 0 |
| strict-final-review | 0 | 0 | 0 | 0 | 0 |

canonical 两 profile：claude 1204/1204 rc0；codex 1204/1204 rc0；BUILD_TESTS_OK helper_profiles=2 python=27 shell=2。focused race 包含 m8/store/obs/cmd 与原较宽筛选，非全包 race。strict 明细见原日志。

SQLite interrupted(9) 首轮对应测试，本源完整四树 Go 内逐条读数：`--- PASS: TestNewSessionNameNotifiesFeishuOnceAndReconnectFlappingStaySilent (0.24s)`。这不是其历史失败根因已定，也不用单跑抵消历史完整 rc1。

28 刀＝前27项在本源完整重跑＋状态写失败仍须正常关闭一刀；每刀编译 rc0、测试 rc非0 且非空业务失败断言，两个还原 baseline rc0。不是把旧源17项读数搬来；也不加上 qwen 自己的6刀。每刀 before/after、UTC、原日志SHA、编译原日志在 evidence-q6。WIP/较窄 race 等历史仍在 gate-history，最终只选 race-final-review。

环境 env-i、普通 uid1001、短 TMPDIR、固定本源且起止 clean，PG strict 走临时库；只复用 PG 工具缓存，没有复用在线 DB。各 gate／mutations 位于不同固定工作树，避免删保护与门相互污染。未连现网、未向在线席位发探针、未取他席正文。完整四树 Go 的逐名 SKIP 有原上下文，不混 PASS。未声称整包 race、identity 整包或浏览器全量通过；正式集成仍须全浏览器 runner 且 PATH 有 go。

## 集成与冻结边界

本次只追加②修订，①仍为已接受 12f851f2；正式集成等各项冻结复核，按③④①②基110e2c1f重做，保留 retainCanonicalTrustedProvenance 及其回归，VERSION只在正式集成抬0.2.3.40。旧④①②预读数与冲突演练含旧167，不外推成本源门。不推 main、不构建发布件、不部署，不自签T-10。

## 历史记录（以下含已撤回eca/3e5中间锚及167更早锚，当前门以前文为准）

# R60② qwen 复核修订交件（当前入口）

冻结产品／测试源 `eca016fb2f0ed272bc063f8170b5588dab37ce17`，tree `8f7490011aa3190622330cbae08023bc11a79ed9`，parent `d4eae2d9b0392dfdd643fe7d9a219a4b7e3c8e84`；基 main `d2cc47c7e699eb45efb3a07ee197aa8c20a9f07e`。分支 `coder56solzzc/r60-hub-keepalive`。本次产品与测试 7 件加 openspec 4 件，共 11 文件／337 插入／21 删除。当前证据入口是 [evidence-qwen](evidence-qwen/README.md)，原 evidence 只属于 167 历史，不延用到本源。交件 tip 为后续纯文档提交，以 Git 三值与交审回信为准。本文是作者门，等待 qwen 增量独立复验与组长终审，不是 PASS 签收或部署 GO。

## 评审逐项处置

独立只读报告 `review/qwen38mvb4/r60-hub-keepalive-20261116@d747a5145dd8ed608e22b66814211bda3f4fe46f` 已从 Git 取回；旧 167/d4eae 的结论保留 NEEDS_CHANGE，不被组长原 Hub 侧接受抵消。评审自选 6 刀／独立跑门与作者 25 刀分开，绝不合并计数。

- Q-1：新增 `TestSessionKeepaliveCancellationDuringActualPing`，真实 WS 对端在 OnPingReceived 明确收到 Ping 且禁止 Pong 后才取消；断言无失败 journal、PingFail 或 keepalive_failed，并回收 Reader。不是先 cancel，也不靠固定 sleep。删除 Ping 返回后 ctx 取消保护，编译成功、业务断言红在 planned in-flight cancellation wrote failure journal；原 planned_cancel 格保留作前置取消对照。
- Q-2：恢复 legacy viewer 的 readonly／“会话已离线”状态与 1000/session offline 关闭协议。每既有 viewer 至多一个 Hub 管理的清理任务，Agent teardown 不串行等待慢 viewer；状态写仍含取槽＋I/O 共 1 秒。正常 Close 是库自己的最多 5 秒关闭帧＋5 秒等回应及内部退出等待，不能叫总握手 1 秒；生命周期超时如实报告。封口不能接清理工作时 gap＋CloseNow，不建游离 goroutine。删除 closeTerminalLocked 临关停白起 output worker 的代码。真实 Hub Agent＋viewer 回归同时断言状态帧和正常关闭码；双 viewer 格确认慢写槽不拖 Agent／健康 viewer。删除状态、改变关闭码、改同步清理各有编译成功业务红。
- Q-3：journal queue_items 改成预算实际 q.items（含正在读取／处理），与 bytes 同口径。
- Q-4：Reader／Pong／worker／journal 年龄统一 h.now()；实际 Ping 耗时仍用单调真实时钟。注入固定时钟回归钉住 UTC 与三个年龄；独立删除 journal/worker 时钟统一各红。
- Q-5：后台封口拒绝启动 viewer worker 时 stopped 在 q.mu 下写，标可见 gap 并关闭连接；专门封口回归与删 gap 刀已覆盖。
- Q-6：**正式集成前必做，当前未通过亦不计 SKIP**。2 秒会话写截止覆盖仍忽略错误的 outbound receipt 与 delivery_ack_confirm；可能提高“已提交但回执写失败”的 unknown，不能用旧现网样本估计新增发生率。未加新计数器，diagnostic_drop 不冒充回执失败计数。①②联合格须实际注入慢读／写背压超 2 秒，证实可观测 unknown／未确认、已提交责任保留、故障消失以原 sender/source ID 与原 delivery 恢复。①的 outbox＋dw_send_status／delivery_status 是兜底，绝非许可换新 ID 重发。本②单源不含①，不能声称已联合验证；该格必须写进正式集成说明和发版限制。
- Q-7：touch worker 后台登记失败时结束 ingress 并关 socket，不再保留没有消费者的通知槽；只影响关停封口期。

新增 HKF-S011～S014 分别钉住离线协议、Ping 期间取消、预算／时钟、封口 viewer。此次保留 167 的 helper 分块字节与 terminal-wire 夹具、环境、原始数据不变；没有另跑 PM04 测量。原测量只证明合成突发/限速条件的控制延迟收益，不能解释历史419次或生产SLO；不限速正文吞吐下降的代价仍保留，发布说明须点明。

## 本源作者门

| 固定门 | rc | 顶层 PASS | 含各层子项 PASS | SKIP | FAIL |
|---|---:|---:|---:|---:|---:|
| canonical-qwen | 0 | 6 | 21 | 0 | 0 |
| go-qwen | 0 | 1558 | 2580 | 49 | 0 |
| race-qwen-complete | 0 | 38 | 54 | 0 | 0 |
| build-qwen | 0 | 0 | 0 | 0 | 0 |
| strict-qwen | 0 | 0 | 0 | 0 | 0 |

canonical 两 profile：claude 1204/1204 rc0；codex 1204/1204 rc0；BUILD_TESTS_OK helper_profiles=2 python=27 shell=2。focused race 包含 m8/store/obs/cmd 与原较宽筛选，非全包 race。strict 明细见原日志。

SQLite interrupted(9) 首轮对应测试，本源完整四树 Go 内逐条读数：`--- PASS: TestNewSessionNameNotifiesFeishuOnceAndReconnectFlappingStaySilent (0.44s)`。这不是其历史失败根因已定，也不用单跑抵消历史完整 rc1。

25 刀＝17 项既有保护在新源重跑＋8 项本轮新增；每刀编译 rc0、测试 rc非0 且非空业务失败断言，两个还原 baseline rc0。不是把旧源17项读数搬来；也不加上 qwen 自己的6刀。每刀 before/after、UTC、原日志SHA、编译原日志在 evidence-qwen。WIP/较窄 race 等历史仍在 gate-history，最终只选 race-qwen-complete。

环境 env-i、普通 uid1001、短 TMPDIR、固定本源且起止 clean，PG strict 走临时库；只复用 PG 工具缓存，没有复用在线 DB。各 gate／mutations 位于不同固定工作树，避免删保护与门相互污染。未连现网、未向在线席位发探针、未取他席正文。完整四树 Go 的逐名 SKIP 有原上下文，不混 PASS。未声称整包 race、identity 整包或浏览器全量通过；正式集成仍须全浏览器 runner 且 PATH 有 go。

## 集成与冻结边界

本次只追加②修订，①仍为已接受 12f851f2；正式集成等各项冻结复核，按③④①②基110e2c1f重做，保留 retainCanonicalTrustedProvenance 及其回归，VERSION只在正式集成抬0.2.3.40。旧④①②预读数与冲突演练含旧167，不外推成本源门。不推 main、不构建发布件、不部署，不自签T-10。

## 历史记录（以下只属于167及更早锚，当前门以前文为准）

# R60② Hub 保活：作者实现与验证交件

本件为作者实现与门证据，不是独立验收或部署 GO。冻结产品与测试源 `1671a59667c544ff3a3e0270ac2a293e15560d20`，tree `b0de1365371deed45c831b5ba8f53514858d1c68`，parent `fd44d8c730d949907abe842aeab8cb6496138d38`；基 main `d2cc47c7e699eb45efb3a07ee197aa8c20a9f07e`。分支 `coder56solzzc/r60-hub-keepalive`。交件证据另成纯文档追加提交，当前 tip 坐标以 Git/交审回信为准。

依据组长2026-11-16T16:05:24Z R60-B、业主16:03:17Z“R60 --- OK，可发”（㊿-补123 `2f5560b7`）与冻结提案 `9eeea527e4fd9372e911ac6f935a23adc7f2659f`。只覆盖 R60②，不带入其它候选项。没有访问/修改现网、预发或真实在线席位，没有发布/部署制品。

## 最终实现

- `internal/m8/session_ingress.go` / `hub.go`：每连接唯一 Reader 持续读协议/控制帧，原HTTP handler保留顺序业务worker，业务不并行重排。Reader不等viewer/SQL/路由/业务ACK；按8KiB scratch流式预留预算，再追加payload。64项（含读取/处理中）、32MiB逻辑payload/连接、256MiB全Hub；超过任一界以单独business_queue_overflow CloseNow，不静默丢可靠帧、不提前确认。接收时间/native时刻保留，touch合并受管，连接/Hub取消回收预算与workers。
- `viewer_output_queue.go` / `terminal.go`：真实legacy viewer有独立64项/1MiB队列，绑定原socket代际；槽等待+网络写共1秒，溢出/超时只断该viewer。重连有明确gap状态/文本标记先于有限buffer；不伪造完整历史。virtual viewer不写WS。Agent teardown不逐viewer串行等待N秒。
- `sessionClient.write`：独立网络写槽，marshal/等槽/网络共2秒，尊重caller更短预算，技能锁不再兼用网络写锁。`SessionHistory.Append`原2秒预算包含result等待，取消不假报cursor已提交。合并touch单worker，保留原5秒写节流。
- `keepalive_journal*.go` / `internal/obs/obs.go`：每次实际保活失败一条有限分类诊断，含UTC/session/connSeq/配置/耗时/read/pong年龄/worker阶段/队列；无raw error/body/key/URL。计划取消不算ping fail；deadline/cancelled/closed/other不编造write-vs-Pong细阶段。journal满/不可用记diagnostic_drop，Reader不被日志反压。
- `session_keepalive_config.go` / cmd：新增正值环境入口 `WP_SESSION_KEEPALIVE_INTERVAL` / `WP_SESSION_KEEPALIVE_TIMEOUT`；默认30s/5s，10～60s与1～30s且和<75s。非法/负数/溢出启动拒绝并关闭已建Hub，不记录原始输入。需要新构建及另行批准重启，旧现网没有此开关。
- helper `terminal_loop`：仅terminal_output正文按UTF8≤32768字节拆块，逐块sleep(0)让控制任务运行；0.12秒收集另有32768字符界。结构化session_event不拆，不改变事件含义或顺序。

## 边界、容量与故障语义

队列是内存传输缓冲，不是持久接受。Reader关闭可能取消尚未处理业务；业务ACK/回执仍在原DB提交后，未确认的发送责任由R60①同ID恢复。ACK2相同重交仍按原generation/sequence账本；事务失败不回成功确认，故障消失同ACK可提交。

32MiB/256MiB约束逻辑payload，另有scratch、切片容量、JSON及库缓冲，不冒称精确RSS/任意吞吐SLO。满队列不能用无限阻塞入队替代，也不冒称业务溢出是keepalive失败。真实网络断开/拥塞仍可失败，成功RTT p95只含成功样本；减少假保活断连不等于根治所有断连。

journal支持Linux pipe/systemd Unix socket：单行≤PIPE_BUF，pipe单独NONBLOCK描述符、socket MSG_DONTWAIT|MSG_NOSIGNAL，不改变进程原fd标志。满、非Linux、普通文件sink记drop；不保证任意被阻塞文件系统可无阻写入，也不为诊断创建无界goroutine。这是明确的日志sink范围，非静默声称所有环境都写到journal。

UTF8正文32KiB不等于wire帧32KiB：JSON转义可至多约6倍正文另加metadata。大结构化事件与任意第三方handler不在强制中断承诺中；Reader与业务分离、资源有界，但不保证任意网络/负载实时。websocket库控制写自身约5秒预算仍在，放大等Pong配置不能保证所有控制写等待同样久。

## 证据与回归强度

见 `evidence/gate-summary.json` 固定源/clean起止/命令/UTC/普通uid/白名单环境/原日志SHA及计数；`gate-history.json` 保留早轮失败与修正，`raw/*.gz`为原始输出确定性压缩；`SKIPS.md`逐名列条件，不混PASS。产品逐文件Git对象SHA在product-manifest，私有环境/队列不入库。

真实生产serveSession路径的长输出健康对照＋业务阻塞6.1秒仍处理Pong；慢viewer写槽/网络截止及其它健康viewer不受影响；队列/全局字节界、关停join/预算清零、正常EOF原因与overload分类、真实pipe/socket背压、默认写槽2秒预算均有回归。ACK2测试含事务阻塞时不确认、事务返回错误时不确认、同ACK重交后确认，不以假成功回执替代SQL责任。

17刀均固定最终源、编译成功、非空业务断言红，删Reader独立/慢viewer总预算/字节与全局预算/历史等待取消/日志及drop/配置/分块公平/EOF原因/gap/ACK提交与失败确认/default总写截止/过载策略各有读数；baseline绿、还原clean。只统计本轮17刀，历史12＋3不叠加，不把编译失败/超时当承重。

环境env-i、独立HOME/短TMPDIR、HUB_TEST_PG_STRICT=1、临时PG库、Go -p1；只复用既有PG工具产物缓存，不复用在线库。无继承SH_/DINGWEI_/DW_/DATAD_传入。没有真实模型推理或线上probe。

## 原失败披露

早期定向编译时测试助手字段/命名错误、fixture问题保留原rc；固定fd候选四树Go/PG strict与focused race/17刀已绿，但完整canonical首轮rc1：新增TerminalBoundedOutputTest在unittest.main之后违反main-guard-last门。产品逻辑不变，测试类移到入口之前形成167锚，完整门重跑；首轮失败不改写为绿、不用单跑覆盖完整rc。旧7be/fd门不外推到未测的新产品。

167第一次完整四树Go rc1：TestNewSessionNameNotifiesFeishuOnceAndReconnectFlappingStaySilent在waitEndpointInactive附近遇SQLite interrupted (9)。新源与未改动Go基线110e2c1f（m8/store相对d2零差异）各30次单跑均rc0；这不足以证明基线flaky或断言统计失败率，原因未定，不据此改产品/测试或把FAIL改SKIP。同源四树完整重跑另列，单跑不替代完整首轮结果。

未声称全包race、浏览器全量或identity整包通过；本轮修改helper终端逻辑已随canonical两个profile全量运行，现代浏览器全量仍由R60集成门执行。本件不改变view-service源码，不把旧UI证据叫本轮浏览器重跑。

## 复核与集成

交qwen只读独立复核，组长终审；不自行签T-10/部署。与①分开提交、联合验证：①保证断连时持久责任与同ID裁决，②保证可服务负载下控制读取不被业务占住，不能量化“丢信少多少”或将两者互相替代。集成必须在③④①②冻结件获审后按顺序合入，基110e2c1f保留retainCanonicalTrustedProvenance热修与回归，VERSION在集成才抬0.2.3.40；本源仍0.2.3.39。


## 固定源最终门读数

| 门 | rc | 顶层 PASS | 含子项 PASS | 含子项 SKIP | FAIL |
|---|---:|---:|---:|---:|---:|
| canonical-reviewed | 0 | 6 | 21 | 0 | 0 |
| go-reviewed-retry | 0 | 1553 | 2575 | 49 | 0 |
| race-reviewed | 0 | 33 | 49 | 0 | 0 |
| build-reviewed | 0 | 0 | 0 | 0 | 0 |
| strict-reviewed | 0 | 0 | 0 | 0 | 0 |

Canonical helper 两 profile：claude 1204/1204 rc0；codex 1204/1204 rc0；末尾 BUILD_TESTS_OK helper_profiles=2 python=27 shell=2。表格的 Go PASS 不与 Python/helper 子项混算。focused race 不是全包 race。strict CLI 明细见原日志。


## 组长复核与HKF-PM04补证（纯docs追加，产品不变）

组长2026-11-16T19:33:23Z已接受167冻结源的Hub侧，且独立删除Reader同步交接保护后slow_business用例转红；helper分块须另补真实WS峰值/控制帧测量，②并入前待齐。本段不把Hub侧接受拔高成整项独立PASS或部署GO，qwen仍按其③优先后复核。

详见[真实WS测量](terminal-wire/README.md)、原始逐Ping/帧数据及SHA。相同8MiB合成Unicode突发、同一真实WS、交替两轮，默认客户端socket：去分块峰值8,388,833字节frame payload，分块32,993；配置10Mbps上限，baseline最长Pong6782.564/6766.208ms，chunked1986.856/1766.046ms；50Mbps上限baseline1403.822/1412.463ms，chunked402.094/400.809ms。正文长度/顺序/SHA一致，未使用fake ws。首轮受限缓冲的146ms不能当默认缓冲结论；实际吞吐、CPU/时钟/采样限制、首轮限速器方法偏差均在补证中披露。

这是作者本地受控协议测量：精确运行冻结terminal_loop原方法、真实Go/Python WS，但不是完整Hub鉴权/DB/CLI/LLM或公网TLS压测，不是现网峰值或419次历史根因统计。该条件存在明确控制延迟影响，作者建议保留helper分块，由组长确认并入条件；未改app.py或其它产品文件。本次只追加docs/reviews内测量夹具源码与报告证据，无二进制/凭据/正文历史上传。

SQLite interrupted(9)首轮FAIL原因仍未定；R60集成必须完整四树Go，并逐条列TestNewSessionNameNotifiesFeishuOnceAndReconnectFlappingStaySilent的结果，不能以单跑或旧源PASS替代。当前仅文档补证，未做集成、未抬版本、未部署。
