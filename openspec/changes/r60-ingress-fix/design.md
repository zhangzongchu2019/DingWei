# 有限兼容窗口与诊断

基9818b979；仅新分支r60-ingress-fix，版本40。先保持channel FIFO实现，容量2048，reserve/release在ingressBudgetMu→q.mu固定锁序内计连接/全局items与bytes，包含读取/处理项；全局items65536。增加高水位但不把256MiB raw计数当RSS上界。独立Reader继续读完整帧，不能暂停业务却期待同TCP流后面的Pong自动被读取；不增加第二Reader/无界任务/显示合并。

sessionClient.write将等槽/序列化预算2秒与物理写预算分开；服务端已知控制种类且序列化≤64KiB用5秒，其余数据20秒，更短父ctx继续有效。取得写权前再次检查取消；物理调用失败记录分类，可能部分消息后关闭连接，不重用。①10秒回执等待可能先unknown，保持原身份与既有status恢复；旧39无持久outbox。既有短调用方ctx不在本次无授权扩展。

条件分片：真实旧39＋10Mbps＋8MiB写/3秒停读＋真实Ping竞争的无分片基线，已经在健康Pong饿死/连接关闭业务断言红（初测日志仓外留存，最终证据需按固定源收口）。因此采用库Writer保持同一个JSON消息，32KiB WS片段，逻辑ID/正文不变。控制Ping/Pong可在帧间竞争，应用ACK/receipt不是WS控制帧，仍不能插入未结束的消息。须保留无分片反例与恢复单帧的红刀；如后续证据不成立则拆出，不将估计当必要性。

诊断每连接结束一次，过载和物理失败各一次；before-teardown捕获，注册提前退出由外层defer兜底。正常/取消/拒绝/关闭/溢出/物理错误均有限枚举；seq无值时0，peer code与时刻无值unknown，不记录原始error或输入正文。物理写失败与Reader退出可竞态，所以保留first_observed_reason/physical_write_reason/writer-active，不将第一观测当因果已证。元数据快照锁内复制、锁外写。使用已有平台非阻塞journal出口，单行≤4096；Hub最多200条/秒，超额或sink失败记DiagnosticDrop，每连接责任仍≤3条；该固定诊断限流不影响业务预算或投递结果。

年龄和限流均走h.now()；生产默认time.Now，测试可固定时钟验证年龄与速率边界，不混用两套年龄。session_ingress发布client.ingress一次后才启动Reader，避免诊断并发读到正在写入的指针。原touch关停期返回值仅观察，不顺手修改。

完整新锚门及删除保护遵循派单。Z-3以组长2026-11-17T21:00:33Z重跑令为当前口径（取代01:37:18Z仅两格限制）：固定a24bb163，全新连续串行五轮、不沿用旧四轮，至少三轮整包rc0/0 FAIL。允许名单仅为TestNewSessionNameNotifiesFeishuOnceAndReconnectFlappingStaySilent、TestViewMentionRoute/a_peer_without_delivery_ack_v1_is_not_sent_to、TestSessionNameReconnectKeepsNameAndReleaseReuses三格，且仅同机制interrupted(9)；名单外FAIL、非interrupted FAIL或五轮绿少于三轮均停报。以后新增须凭基线锚同机制复现记录由组长另裁，不能自动扩。预发现有24不代替39；需另令业主Console点升stagingtest到已批准39，再做事件突发/停读/慢viewer/保活/40独立恢复，不自行执行。

规格优先级：本次RIF-001/RIF-002依据18:04裁定替代进行中HKF-002的64项初值及HKF-004的整次2秒初值；原历史变更保留取证，最终当前值以RIF的2048/65536及等槽2s/物理5s或20s为准，不撤销其唯一Reader/ACK提交后确认/有界写/取消责任。
