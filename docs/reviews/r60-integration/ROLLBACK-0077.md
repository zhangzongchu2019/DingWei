# 0077施加后的旧Hub回退核查与预发演练建议

依据组长2026-11-17T03:19:05Z。结论：**库格式向后可启动，但不满足无条件业务安全回退。默认应保留升级后的库，不回退数据；旧exe开放业务前必须停新writer、关闭新流入，对账并排空或可见取消所有未终结新协议投递。**这与①design.md:59一致。无法满足时保持隔离/停服报组长，不能因health200就放开连接。

## 1 旧件能否在0077库上启动

能。本地同源旧源码110e2c1f63b1530d1ca05b92cd723312ef16c0f8（root VERSION0.2.3.39，Hub/命令代码与d2cc47c7无差异）诊断编译；新件用a311固定build.sh产物。不是下载线上exe，不宣称诊断旧件与线上exe字节相同。完整条件/二进制SHA/原始UTC见rollback-0077/METHOD.json与binary-roundtrip.json。

实际进程链：r60初始化空隔离库→施加0077及两索引、账本行→种W/P/E→停→旧件起/health200/TERM退出0→停→r60再起/health200/TERM退出0。旧期间前后与再前滚启动后，0077的version/digest/applied_at、两显式索引DDL、三条fence六字段、delivery状态均未变化。日志无0077未知迁移警告；shutdown时的已关闭listener ERROR原样保留，退出仍0，不误读为迁移错误。

旧110坐标：internal/store/sqlite.go:195把全部账读入map；:200-209只枚举旧binary内嵌迁移目录；:223-239仅迭代这些已知名字，已知digest不符拒启；:752-766读账不检查超出内嵌目录的额外键。**未知更高号0077被静默忽略/保留，不拒启，也无专门报警。**不是忽略所有schema错误：旧已知迁移digest/已有对象不符仍可独立拒启。0077纯加表/索引，未变旧SQL依赖列；R60 internal/store/migrations/0077_outbound_intent.sql:1-12。无真实PG/外部配置验证，不把本项替代整份生产启动GO。

## 2 旧期间的协议/投递，及再前滚

旧110内部不引用outbound_intent/outbound_recovery_v1，因此不维护该fence、不GC、不恢复新协议原裁决。旧internal/m8/outbound_receipt.go:82-95仍可能回durable_agent_queue=true的旧capability，但没有recovery_v1。真实WS两源探针证明：旧false、新true；不能把旧持久收方队列能力冒充sender/source幂等。

新helper a311：outbound_receipt.py:95-97只有显式recovery_v1=true且sender/targets合法才建立恢复能力；outbound_recovery.py:76-93发现旧/无新能力先unsupported_peer并返回，不发正文；outbound_queue.py:230-238未尝试意图rejected，已尝试者unknown且保留正文/原ID，reason=outbound_recovery_unsupported；:203-210不再自动claim这类停发项。**再次前滚不会自动解除这类unknown，也没有本版管理工具可替代人工核验；不能删库、换ID或手改reason逼恢复。**两个既有helper定向单测均通过，范围为受控Broker；实际WS能力另测，不伪称完整真实旧Hub/helper组合。

更危险的是已在Hub的未终结新协议收方投递。旧110 internal/m8/hub.go:4862-4873走普通subAgentDrainCheck，:4944-4966绑定ACK后writeSubAgentDelivery；旧sub_agent.go:415-431无sidecar时按普通root放行，**不读outbound_intent稳定Target**。新a311 sub_agent.go:418-421及:455-457两处调用checkOutboundDelivery；outbound_recovery.go:204-226按fence现核Target ID/owner/address/state。

同一真实WS探针：原Target绑定后撤销，创建同名active替代席并加入合法共同组；旧件把待发正文交给新席，state=sent；新件不交正文，落dead/outbound_permission_denied。两边原fence实际SELECT仍在。探针在私有库/MemStore身份/loopback完成，没有真实席位或模型。这是design:59禁止只换exe继续派发的直接实证，不是新r60产品缺陷。

再前滚的**格式/存量**实测安全：0077账本/索引和fence原字节保留；Migrate本身不GC；新store先查原fence再清理/计容量（outbound_intent.go:91-117）。真实API恢复W与超期P均created=false/原delivery；首次新受理触发GC后W/P/E由1/1/1至2/1/0。:50-56只删超期且原delivery及关联failure_notice都已终结的E；P不清不计窗内配额，W保留并计quota。

**不等于可任意往返业务无损**：旧期间的legacy新业务没有补写fence，旧规则可能已派发旧队列；不自动把旧业务/历史unknown导入新outbox，不能据Hub缺fence推断未送。已被旧helper标unsupported的unknown须人工对账；自动恢复24h/Hub默认7d时钟余量合同不因回滚延长。只有停止流入、未终结责任已清、同一最新库和私有caller/outbox状态保持完整，才可把前滚作为受控方案。

## 3 是否必须开场备份库+旧exe

本例0077不会令旧件拒启，所以**不需要为了0077删除表/索引/账本行，或恢复开场旧库**。默认保留最新完整升级库及fence，按第2项安全前置条件切旧件；不提供DROP/篡改账本脚本。

若另因已知迁移digest/损坏/非0077因素无法启动，先停止并封存当前DB及匹配WAL/SHM、版本/配置与helper私有账本，再报组长决定。恢复开场快照会抹掉快照后新投递、状态推进与幂等裁决，不能称无损回滚。间隙增量须保留在独立封存副本，按原request/source/delivery逐ID区分已处理、明确未提交和unknown；已处理不重放，unknown人工核验，未提交亦只在受审原ID方案下恢复。无现成自动跨快照合并/重放工具，不能在GO里写自动“重新发一遍”。需业主/组长明确数据损失与对账方案后才能恢复，不由本报告授权。

## 4 建议预发演练（计划，尚未执行）

以下由组长另发GO、ops在指定预发实例/合成canary范围执行；真实unit、DB路径、端口、制品SHA与观察窗须填入GO。没有凭本报告执行生产命令。

1. **停/建基线**：隔离预发入口，仅允许合成canary；停canary新helper/writer和所有新流入，再停Hub，等待进程退出/连接0。用停机一致性副本或SQLite backup API备份；不能运行中只cp主.db而漏WAL。记录旧r59 exe真实SHA、配置身份库/密钥引用（不抄值）、DB/ledger/schema指纹和helper私有caller/outbox快照，恢复文件权限。预留“开场快照”和“回退前最新快照”两份，不覆盖。
2. **换/起新件造样本**：换正式R60制品，保持只连canary；health/版本/0077账本和两索引核对。用稳定request ID完成一次真实新协议收发并确认原delivery的可见终态；额外P负例只在隔离复制库验证旧件不安全，不让真实未终结责任暴露旧件。
3. **回退前安全闸/停**：再次关流入、停新writer，待在途结束。核全量新协议未终结责任（不仅窗外P，窗内W也可能未终结）及关联failure_notice=0；helper local_queued/unknown须为0或由组长逐ID明确冻结/对账。没有自动取消/解除unsupported管理工具，不发明开关、不裸SQL删行，不把admission_unknown计终态。未达即停报；可见取消由组长指定已支持路径执行后再核。保存最新一致性库，保留fence不降级DDL。
4. **换旧/起/核**：在WS/入站仍隔离下换回已核SHA的r59旧exe，使用最新升级库启动。核health200、旧版本、0077账本/两索引/fence六字段与终态队列原样、无意外投递；确认本地新helper不会向旧件发恢复正文。达到组长门槛才允许其另定是否恢复旧版业务，不把仅health200当完成回退。
5. **再停/换新/起/核**：停旧Hub，保持同一最新库/私有账本，换回同一R60制品。核0077digest/索引/fence保留，原已完成ID查得原delivery（不新增），新canary ID受理一次；核W/P/E及旧未知无批量自动发。unsupported未知仍须人工核验，不预期自动转绿。确认后按GO逐步开放，异常即停/封存/报组长，原始FAIL不以重跑抹掉。

计数应使用与Hub相同的UTC cutoff/验证后的retention；W=窗内全部fence，P=窗外且原投递或关联通知未终结，E=窗外均终结可清。回退安全闸另数所有时间范围的未终结，不能误拿P=0代替全部清空。观测字段以outbound_intent.go:50-56、:104-117定义，不把自拟SQL改成清理操作。

## 证据/边界

所有命令/UTC/环境白名单/源锚/原SHA见rollback-0077目录与上级SHA256SUMS，未归档二进制/数据库/真实正文。产品仍a311；本次只追加文档与诊断证据，不重跑或改写先前集成五轮/canonical/browser等门，不扩新功能/迁移。正式集成仍待组长终审，不构成预发或现网GO。
