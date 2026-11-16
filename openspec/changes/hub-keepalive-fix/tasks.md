# Tasks

- [x] T-01 核main/现网源码锚、既有规格、monitor输入及统计口径。
- [x] T-02 逐行追Hub Reader/viewer/SQL与helper事件循环/IPC/输出，列A的预算及B无配置入口证据。
- [x] T-03 完成ADDED提案与D独立实施/联合验证建议，PM预算待定，不改产品。
- [x] T-04 保留离线viewer与长输出探针、首轮overlay-vet错误、无修复基线的目标断言红；跑strict及canonical，不把现状probe通过当修复PASS。
- [x] T-05 推指定分支、核ls-remote，回tip/tree及A～D摘要。
- [ ] T-06 组长/独立评审定设计、负载与预算，另派实现/删保护变异；本单不实施、不接现网。

## 交接记录

- 2026-11-16 coder56solzzc，基commit d2cc47c7e699eb45efb3a07ee197aa8c20a9f07e。完成：只读追码、离线注入与提案；阻塞：无419次逐笔错误类型/当时viewer及DB等待trace，不能说全部同一阻塞点；验证：strict单变更rc0、全仓36/0；canonical首轮rc1（Claude临时目录清理错误）及完整重跑rc0并存；现状探针2顶层/4子格绿、目标契约两注入格红。下一步：推送核远端并等审案，不实现。

- 2026-11-16交接：正文/证据提交6e879acc5457c4d44eea8e18fc522dabeb129cfb已推送，ls-remote同值，main仍d2cc47c7。30项Git对象SHA全中，internal/cmd/view-service/tools/scripts/go.mod/go.sum与d2逐一同值。仅补本交接后回传最终tip/tree/parent；产品与规格不变，等组长审案及另派实现。

## R60实施（2026-11-16T16:05:24Z授权）

- [ ] I-01 完成单Reader/顺序有界worker与全局预算/连接清理。
- [ ] I-02 viewer独立有界输出/总截止、touch合并/存储等待取消、session总写截止。
- [ ] I-03 journal有限分类与drop计数、正值配置、helper文本UTF8分块。
- [ ] I-04 固定候选后完整canonical/Go/PG strict/focused race/OpenSpec与逐保护删改须红，保留每轮原失败。
- [ ] I-05 普通推送核远端，qwen独立只读复核/组长终审；不自集成/不部署。

- 2026-11-16 coder56solzzc，工作基d2cc47c7。完成：Reader/队列按流式8KiB增量预留预算，含处理/读取中的64项、32MiB及全Hub256MiB；慢viewer总截止/独立输出与重连gap、合并touch/历史结果等待取消、journal脱敏非阻塞/drop计数/正值配置、helper UTF8分块。真实WS慢业务6.1s对照/ACK提交前无确认/满队列/关停/慢viewer与日志pipe+socket背压均定向通过；工作态focused race已绿但非固定锚门。曾有测试助手命名/字段编译错，保留原失败；未改既有flaky去涂绿。阻塞：固定锚全包/canonical/变异和独立复核未完成。下一步：固定候选，冻结产品期间跑门/删保护；不自验收/不部署。

- 2026-11-16 coder56solzzc，7be26af20固定源四树Go/PG strict整包rc0（m8/store/obs/cmd，逐名SKIP待报告）、12刀及补3刀均编译成功/业务断言红；非最终交审。追加收口：无效启动配置关闭已建Hub，ACK失败事务亦无确认且同ACK可恢复，session默认2秒也涵盖持槽等待；满队列故障用例可安全取消回收，避免删除保护时仅靠Go超时。上述定向已绿；固定新候选后重跑固定门/变异，不沿用7be完整门作最终结论。

- 2026-11-16T19:21:54Z coder56solzzc，冻结产品源1671a59667c544ff3a3e0270ac2a293e15560d20。完成：同源canonical/build/Go全包/PG strict/focused race/strict及17项编译成功业务断言红，证据与全部原失败见docs/reviews/r60-hub-keepalive。阻塞：独立只读评审和组长终审未完成；下一步普通推送核远端并只请求该最终源复核，不自验收/不合入/不部署。

- 2026-11-16T19:55Z coder56solzzc，承接组长19:33:23Z复核：Hub侧接受167冻结源，helper分块按HKF-PM04补真实WS对照证据。完成：同连接8MiB合成Unicode突发、实际frame/Pong读数、默认与受限缓冲/校准限速/不限速各两轮，正文SHA相等；默认10Mbps baseline6.78/6.77秒、chunked1.99/1.77秒，源/命令/UTC/原SHA与吞吐代价见docs/reviews/r60-hub-keepalive/terminal-wire。本次仅docs/任务记录追加，产品及spec/design不改，不把试验计入原17刀或自签收条件。阻塞：组长确认补证及qwen独立复核；下一步：普通推送核远端，集成四树Go逐条列SQLite首次FAIL对应测试，不能以单跑抵消。

- 2026-11-16 coder56solzzc，qwen对167/d4eae NEEDS_CHANGE已收，报告d747a5145dd8ed608e22b66814211bda3f4fe46f只读fetch。完成：工作态Q1实际Ping取消/Q2真实Agent离线状态和正常close/慢viewer隔离增量绿，Q3/4/5/7口径/封口修订；旧17刀/作者门仅旧源，组长Hub侧接受不抵消新发现。阻塞：新源冻结/完整canonical四树Go/PGstrict/focusedrace/变异/独立复验未齐；Q6文档明确且联合格待集成不冒称已跑。下一步固定新源后完整重跑与新旧保护刀，普通FF交新锚，不沿用167预读数作正式集成。
