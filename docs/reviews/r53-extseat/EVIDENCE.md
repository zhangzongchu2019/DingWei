# S-R53-EXTSEAT-IMPL A路最终自测交件

## 身份、授权与源码坐标

- 实取 CODEX_THREAD_ID：01a1af35-98c2-7451-a54a-6ec76c50d4f9；2026-11-06T05:14:45.690524Z 原子抢锁，owner.txt 写实取全值。遵组长05:14:20Z新裁定，本轮接续，锁保持到推送及交件回报完成。
- 早先owner及回执将派单目标01a19ca0误作实际thread，已向组长如实纠正；此处以本轮实取身份为准。e261aaa5、2e5e0b02与fe67均保留，不改写历史；独立复核由组长集成时执行。
- 仓库 `git@git.code.tencent.com:wego-ai/dingwei.git`，分支 `coder56solzzc/r53-extseat`；受测实现源 `fe67be849cca1498e0c188864e8df476d55aaa28`，tree `8ccf4edbe9cfc37b9b3800ec31ad12a66c66095c`。本次承载提交只追加证据及交接，产品代码不变；正式tip/tree以交件回执为准。
- 交件前已fetch A分支仍为fe67，集成分支最新 `75a4088052ccb1a4e1a66bd8674bf5d6b55bf879`。相对已读593ed5ef，合同仅新增第7节证据目录规则，OpenSpec需求未变；本件遵新目录。旧仓根EVIDENCE.md逐字节保留于fe67状态，按约由组长集成时迁移。

## 两路改动核对表

| 项目 | 保留与改写结果 | 当前实现源上的反向验证 |
|---|---|---|
| 外部路由入口 | 保留SplitAgentName判形状及Hub小写索引归一化；foo_bar、abc_AG0、abc_ag0直接返回false且零身份查询，普通离线路径保持 | 改回Contains，三个子项红在身份查询次数 |
| audience／固定scope | 保留Resource、0105令牌行audience及list_online/send_message/wait_for_messages/ack完整授权；IssueToken签名不变；main统一配置，不接C的飞书令牌 | 去资源核对或完整scope核对，分别红在错资源/子集授权断言 |
| PG投影身份锁 | 保留同事务锁双方身份及Delivery.SourceAgent；改为在锁内读取双方当前快照，Hub适配器不二取PG连接；稳定ID与helper原ACK/重试语义保持 | 分别去源锁、目标锁，真实PG并发更新不再阻塞而转红；恢复适配器身份查询转红 |
| 锁后时钟与重试 | 保留current重采样；令牌/AG消费者/发送目标/收件与ACK锁等待后再核整点到期；保留Run瞬态失败按原ID重试 | 去重采样，真实令牌行锁等待及消费者等待两用例红；后台原ID重试正向通过 |

没有丢弃两路改动。初稿Resource范围、投影快照及锁后时钟按合同和实际锁路径改写；C容器签名差异保留为组长集成薄适配器事项，不擅改A/C公开签名。

## 门读数

PG严格模式 `HUB_TEST_PG_STRICT=1`；测试以dev普通用户执行，剥离继承SH_/DINGWEI_/DW_/FCS_变量；canonical使用短TMPDIR、独立HOME、锁定Python。源码在主工作树和隔离工作树均与受测摘要一致。

| 门 | 读数 | 说明 |
|---|---|---|
| 定向／旧评审锁回归 | rc0 | writer-focused，见../r53-extseat-writer-reconcile/RESULTS.json；产品修改完成后执行 |
| store整包 | rc0，367 PASS / 5 SKIP | writer-full，严格模式 |
| migrate整包 | rc0，27 PASS / 0 SKIP | 同一完整包门 |
| identity整包 | rc0，77 PASS / 0 SKIP | 同一完整包门 |
| m8整包 | rc0，872 PASS / 19顶层SKIP | 同一完整包门；子项SKIP另计 |
| extseat整包 | rc0，8 PASS / 0 SKIP | 同一完整包门 |
| canonical | rc0 | 05:11:03Z—05:21:14Z；两个helper配置各收集/执行1097例，27个Python脚本及2个shell门完成 |
| extseat／migrate／identity race三轮 | 各Go包PASS | 8/27/77顶层用例各3次；旧race-core的这三包已完成，原整体rc1另列，未重做已过包 |
| store race三轮最终重跑 | rc0，1101 PASS / 15顶层SKIP | 372顶层用例各3次；06:42:35Z—08:11:52Z，120m总时限 |
| m8 race片0/1/2/3 | 各rc0 | 223/223/223/222顶层用例，各3次；合891项/2673次，独立清单核无漏无重 |
| OpenSpec全仓strict | rc0，24项全过，见OPENSPEC-STRICT.json | 使用本地钉版1.16.0，禁用telemetry |

五包共1375顶层用例，接受的race读数共4125次，逐个核对均为3次。m8四个External用例各3次，无SKIP。逐命令UTC/rc/hash、包级PASS、各项覆盖计数见FINAL-GATES.json；分片及独立测试清单分别见M8-COVERAGE.json、RACE-INVENTORY.json。

### 原90分钟超时与最终store读数

旧 `race-core` 于06:41:11Z触发 `panic: test timed out after 1h30m0s`，整体rc1，原件保留，不能报为全绿。当时无此前断言失败或数据竞争报告；第三轮在既有 `TestR10LockConcurrent50RoundsRestartAndExpiry` 被中断，该例前两轮分别34.81s、31.04s通过。

仅将store整包按原 `-race -count=3`、不减用例、不改源码重跑，总时限120m，最终rc0。该R10例在重跑中三次分别30.37s、30.42s、30.62s通过。最终store覆盖只采本次完整重跑，绝不拼接旧不完整第三轮充数。旧命令中另三包的PASS和最终store替代关系在FINAL-GATES.json明确保留。

### 42条SKIP中PG相关与新增者

普通整包42条SKIP事件包含24个顶层及18个子项；完整逐项名单/条件仍保留于 `../r53-extseat-writer-reconcile/FULL-SUMMARY.json`。

- PG相关仅 `TestDeviceClaimAcrossActualClientAndPostgres`：普通整包未设 `DW_DESKTOP_INTEGRATION_PYTHON`，要求由跨runtime shell门运行；不是PG缺失或启动失败。writer-desktop与本轮canonical均已实际执行，new/reuse/lost_ack/takeover/cancel_then_new五子项PASS；短原文见CANONICAL-PG-EXCERPT.txt。
- 本单新增12个Go测试函数与42条SKIP及最终race SKIP逐项对照，交集为空。新增名单见FINAL-GATES.json。
- extseat、migrate、identity中的PG用例没有SKIP。其他既有可选用例按原条件如实列出，不把SKIP计作PASS。

## 变异与原始证据

- 七项修订判据变异在fe67对应源片上各rc1，均命中对应断言，不是编译错误/超时；旧位置 `../r53-extseat-writer-reconcile/MUTATION-FAILURES.json` 与RESULTS.json保留。
- 最终核对发现早先四条核心变异覆盖源片早于scope定稿，故只将同一语义刀移植到fe67当前源码补验；续期不持久、吊销复活、同键异参放行、轮询续组，四项均rc1且各命中预期断言。命令UTC/rc/hash及原文见CORE-MUTATIONS.json；旧结果只作历史，不替代新源片。
- 共11项有针对性的变异；正向长门均使用原始产品文件，Go overlay不修改工作树。当前source绑定及owner原文见SOURCE-BINDING.json。
- 本机原始大日志 `/tmp/r53-extseat-post-fe67be849/`；正式共享只走上述Git仓库，不上传源码包。摘要、原文摘录与哈希均随证据提交。实现自测已收口，A路交件后由组长集成独立复核；未部署、未重启。
