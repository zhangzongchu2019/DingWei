## ADDED Requirements

### Requirement: RIF-001 旧helper有界FIFO兼容窗口
Hub MUST 保留唯一Reader与顺序worker，入口每连接2048项、全Hub65536项，包含正在读取与处理责任；每连接32MiB与全Hub256MiB原始字节界继续有效。canonical session_event、ACK与正文不得静默丢弃或越序；接收/内存入队不等于durable。越界SHALL 明确过载，取消/清理归还所有预算，不用无界队列或第二Reader绕过控制帧限制。

#### Scenario: RIF-S001 真实39突发与提交屏障
- **WHEN** 固定0.2.3.39 helper的原session_event_loop发送1000条4KiB事件，并在500/501间穿插ACK及正文屏障
- **THEN** 连接保持、全部事件ID/正文与顺序完整落盘，ACK提交前无确认，正文不得越过已提交ACK；恢复64项或破坏FIFO/ACK提交保护时对应断言转红

#### Scenario: RIF-S002 所有资源界与释放
- **WHEN** 每连接或全局的项数/字节刚好到界、越界、取消或换代
- **THEN** 精确界内可受理、界外明确overload、不谎报持久确认，active责任计入预算，结束后引用及预算均释放，去界/释放保护的测试转红

### Requirement: RIF-002 会话写等待与物理预算分离
helper会话写 SHALL 等槽至多2秒；取得写权后按服务端种类/实际字节分小控制5秒与数据20秒物理预算，更短调用方ctx仍优先。未开始物理写的等槽失败不得冒称物理断链。物理写可能部分发出时不得复用半消息连接或换新逻辑ID盲重发；实际具备①能力的helper按同source/delivery恢复，旧39不被假定具有持久outbox。

#### Scenario: RIF-S003 停读恢复
- **WHEN** 真实旧39 helper停止传输读取3秒后恢复，Hub发送8MiB数据与小控制消息
- **THEN** 连接保持、JSON正文/ID正确，物理预算改回2秒须使大数据断言转红，等槽与真正物理失败各有独立可观测面

#### Scenario: RIF-S004 限速下控制帧竞争与条件分片
- **WHEN** 同一真实WS在限速和大数据写入期间需要正常Ping/Pong
- **THEN** 正常helper不被数据帧写锁饿死，实际单帧峰值/正文SHA/控制结果有记录；只有不分片基线在业务断言红时才引入同一消息的有限分片，应用JSON不能插进另一个未完消息

#### Scenario: RIF-S005 不确定回执保持原身份
- **WHEN** 写槽/控制回执暂时失败而Hub提交已完成，helper具备①恢复能力
- **THEN** 保持unknown与原source/delivery，dw_send_status可恢复唯一原投递，两个控制写失败计数仍独立；显式隔离解释器下联合格和双CLI格不得SKIP

### Requirement: RIF-003 连接退出诊断有界且诚实
Hub MUST 每连接一次session_end，overflow与物理写失败各至多一次说明；原因/错误/帧种类采用固定枚举，未知写unknown，记录seq、生命周期阶段、peer code、预算/高水位及writer阶段，不写正文、凭据、原始异常或URL。journal SHALL 非阻塞、单行≤4096字节并有Hub总速率界，失败/限流可计drop；第一观测顺序不冒充唯一因果，计划取消不伪报保活失败。

#### Scenario: RIF-S006 原因与隐私字段
- **WHEN** 对端正常关闭、过载、物理截止、计划取消或注册/身份拒绝结束连接
- **THEN** 相应固定原因/阶段和可得字段被保留、每连接无重复结束，敏感文本不出现；删session_end/overflow/物理原因保护各在对应字段断言红

#### Scenario: RIF-S007 诊断sink堵塞或重连风暴
- **WHEN** 日志pipe/socket满或诊断速率到界
- **THEN** Reader/写者不等待日志、丢弃可观测、内存/日志责任有界，不借阻塞日志重建保活故障

### Requirement: RIF-004 保留隔离与新锚完整门
修订 MUST 保留慢viewer独立隔离、离线readonly/正常关闭、Z-1 gap与停写防线、健康Pong不被慢业务饿死和真实保活失败检测；保活仍30s/5s。SHALL 在新锚完整门及独立复核，不把旧PASS、允许两格的历史SQLite中断或预发24结果冒称为新兼容结论；不实施生产动作。

#### Scenario: RIF-S008 原门与新源证据
- **WHEN** 修订提交固定为新SOURCE_COMMIT
- **THEN** canonical/四树PGstrict/五轮m8有效Z-3门/focused race/浏览器/strict及原current/inventory/provenance/真实CLI门逐项留证，Z-1各刀红，新测试失败不获旧噪声豁免
