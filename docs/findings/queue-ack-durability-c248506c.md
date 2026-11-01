# 运行期队列确认与持久性缺口（只记录）

基线：工蜂 wego-ai/dingwei，c248506cf120c3af2a06eeaeeae3038f596537b1；本件归属S-HLC-IMPL T-04组长裁定，不以本件授权修运行期语义。本次已证范围仅为「异步提交错误不可传播」与「Flush不证明排尽」，不把源码中的跨动作顺序风险写成已复现的业务消息丢失。

| 位置（均为c248506c） | 事实 |
| --- | --- |
| internal/bus/async_dbqueue.go:86–98 | Enqueue成功只代表写入内存channel，未等待SQLite提交。 |
| internal/bus/async_dbqueue.go:194–205 | 批在提交前移出batch，BatchEnqueueMessages失败仅告警丢批，无上返错误。 |
| internal/bus/async_dbqueue.go:158–169、222–224 | Flush返回无错误完成信号；只冲当前batch，不证明channel此前待写项已全部落库。 |
| internal/worker/schedule_worker.go:41–56 | ProcessOne在handle返回后Ack入站，没有与派生出站跨动作原子事务。 |
| internal/store/sqlite.go:1981–2010、2069–2074、2488–2494 | BatchEnqueueMessages有独立SQLite提交；Ack独立标done；启动RecoverProcessing只恢复processing，不能复活已done入站或丢失的内存派生项。 |

复现条件：队列使用AsyncDBQueue，令批大小1，以测试仓储在BatchEnqueueMessages返回合成提交错误；先Enqueue合成消息，等待确实进入并返回该失败，再调用Flush。原实现Enqueue=nil且Flush=nil，而实际批提交失败。Flush分支只调用当前batch的flush；select可在channel仍有多批缓冲时先处理flushReq，因此该完成信号不证明channel排尽。此为源码可核的控制流事实，不冒称原探针已统计通道排尽。

ProcessOne Ack与派生出站的提交没有跨动作原子事务，是另行评估所需的源码背景。本轮未做业务链路故障注入证明业务丢失。出站必须区分待发送业务消息与已发送后写入的回执；后一类落库失败不能直接推断业务消息未发送，也不能把所有未落库项一概称为消息丢失。不得扩推exactly-once或跨动作事务保证。

处理范围：停机专用SealAndFlush先封入队，排尽channel全部多批和部分batch，传回首个落库错误并输出未提交/失败计数、等待写协程真正退出；3秒DB阶段超时退出2、无超时提交失败退出1。运行期Enqueue、Flush及现有调用点的best-effort确认语义不变。S008增加通道仍有多批、提交失败与撤排尽的目标红变异；不以这些停机测试关闭本运行期发现。运行期持久确认口径、失败保留及跨动作事务由组长另议，本单只记录不修。

## 附录：overlay诊断，非HLC验收

- 探针：本席 `/tmp/hlc-queue-boundary-probe_test.go`，测试名 `TestHLCBaselineQueueBoundaryProbe`；通过 `/tmp/hlc-queue-boundary-overlay.json` 增加一个并不存在于工作树的测试文件，不修改基线产品源。合成提交错误已经发生后再调用Flush。
- 首次正常vet运行rc1：Go vet无法打开overlay新增且磁盘不存在的虚拟源路径，探针没有执行。该失败不是产品失败，也不是有效反证。
- 诊断命令：`go test -vet=off -overlay /tmp/hlc-queue-boundary-overlay.json ./internal/bus -run ^TestHLCBaselineQueueBoundaryProbe$ -count=1 -v`。关闭vet仅用于解决此次新增虚拟路径的诊断工具问题；第二次rc0。
- 实际读数：注入BatchEnqueueMessages失败，`Enqueue=nil`、`Flush=nil`。输出明确标记 `BASELINE OBSERVATION ONLY` 和 `not an HLC acceptance PASS`。
- 本附录是诊断记录，不计为HLC验收。新S008测试使用真实工作树文件、默认vet和race；撤排尽变异只overlay替换已存在的源文件，保持vet开启。
