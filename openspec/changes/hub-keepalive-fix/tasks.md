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
