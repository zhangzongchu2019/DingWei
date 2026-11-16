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
