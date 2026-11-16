## ADDED Requirements

### Requirement: SL-001 主动发信责任与结果分层
helper MUST 分别报告local_queued、hub_durable_queued、unknown、rejected；accepted_by SHALL 区分local_helper、hub、none、unknown。本地持久接管才可local_queued，Hub提交回执才可hub_durable_queued；sent或IPC ok不得单独代表入Hub、收件准入或执行成功。明确未接管的暂时失败MUST rejected、accepted_by=none、retryable=true；已可能提交的unknown不得许可caller新建ID盲重发。

#### Scenario: SL-S001 连接未就绪但本地提交成功
- **WHEN** 合法意图在断线期间原子进入本地持久队列
- **THEN** 返回local_queued及稳定request/source ID，不返回Hub delivery_id或Hub接受声明

#### Scenario: SL-S002 写后回执未知
- **WHEN** WS写入结果不明或Hub回执超时
- **THEN** 返回unknown并保留查询键和持久恢复责任，不丢正文，不把unknown等同永久拒绝

### Requirement: SL-002 调用与业务身份固定
caller MUST 在首次IPC写出前固定request_id，helper SHALL 原子持久绑定一次生成的source_message_id、认证发送Agent、稳定目标Agent及正文/业务meta摘要。同request_id同语义须返回同意图，冲突MUST 明确拒绝；IPC回执丢失后须可按原request_id查询，不以新业务ID替代。

#### Scenario: SL-S003 IPC回执丢失与重复调用
- **WHEN** 本地已提交而IPC断开，caller用同request_id及同内容续查或重试
- **THEN** 只存在一个发送意图、一个source_message_id，不重复创建业务消息

#### Scenario: SL-S004 相同请求号不同业务内容
- **WHEN** request_id被用于不同目标或不同正文/业务meta
- **THEN** 拒绝冲突且不覆盖原意图或返回其成功回执冒充新内容接受

### Requirement: SL-003 发送前持久接管与离线IPC
helper MUST 在网络提交前原子保存完整待发意图；IPC生命周期SHALL不依赖单代WS，崩溃/重启后可恢复未确认项。提交失败不得报告接管；local_queue_unavailable及local_queue_full须表示明确未接管暂时失败。未确认项不得按缓存淘汰，私有数据权限与容量须受限。

#### Scenario: SL-S005 持久提交后崩溃
- **WHEN** helper已落本地账本但未上送即退出并重启
- **THEN** 同意图恢复为待发，旧正文/目标/ID保持不变，且没有本地接管前的网络写出

#### Scenario: SL-S006 本地队列写失败
- **WHEN** 存储不可用或待发容量已满
- **THEN** 明确未接管、可用原request_id重试，不伪称local_queued或静默丢掉旧待发

### Requirement: SL-004 Hub原子持久幂等与回执恢复
Hub MUST 将认证sender_agent_id与source_message_id的唯一裁决、稳定目标/语义摘要、原delivery_id和首次入队原子提交，回执在提交后发送。同意图并发/跨连接/Hub重启重放须返回原delivery_id及本次关联回执，不创建新收件项；语义冲突MUST idempotency_conflict且不覆写。回执nonce/连接代际不得算业务内容差异。

#### Scenario: SL-S007 已入队但回执丢失后重放
- **WHEN** Hub提交后回执丢失，helper重连并以原source_message_id续交
- **THEN** Hub恢复原持久回执，仅一条收件投递，不再次触发业务路由副作用

#### Scenario: SL-S008 同ID并发与Hub重启
- **WHEN** 两连接同时提交同一意图，或Hub重启后又收到原意图
- **THEN** 原子唯一裁决仍只关联同一delivery_id，不依赖进程内map或先查后写

#### Scenario: SL-S009 同source号不同内容
- **WHEN** 相同认证sender/source号携带不同稳定目标或业务摘要
- **THEN** 拒绝idempotency_conflict并保留原裁决，不把新正文送给收件方

### Requirement: SL-005 连接与身份边界
恢复发送 MUST 只使用当前有效WS代际，且仅在协商Hub持久幂等能力后启用；失效代际不得新写业务。每次恢复须现核原sender/target稳定身份及当前权限，不向同名替代目标投递，不因lane/连接号变化新建业务ID。旧Hub或不支持路径（含首批未覆盖的正文AMR扇出）在任何业务写出前须返回outbound_recovery_unsupported，不得移除业务标记改变原路由语义；已可能提交的旧unknown不得升级后批量盲补送。

#### Scenario: SL-S010 重连与不支持Hub
- **WHEN** 原连接关闭且新连接未完成能力协商，或对端不支持持久幂等
- **THEN** 不由旧WS继续写业务，不自动重放未知；未接管请求得到明确不支持结果

#### Scenario: SL-S011 目标被替换或权限撤销
- **WHEN** 待恢复意图的目标同名但ID改变，或任一方失去通信权限
- **THEN** 不改投替代席位、不泄露原裁决或正文，原意图转为受限可见拒绝

### Requirement: SL-006 调用方失败语义不掩盖
send.py与MCP MUST 保留结构化phase、accepted_by、request/source/delivery ID及reason；unknown或rejected不得打印无条件sent成功或仅因IPC ok返回成功语义。临时且可证未接管失败SHALL可用原意图重试；已可能提交/IPC未确认时须先查询，不能换通道或生成新ID自动发送。

#### Scenario: SL-S012 helper返回unknown或拒绝
- **WHEN** IPC传输成功但业务结果为unknown或rejected
- **THEN** CLI/MCP明确显示对应非成功结果和查询键，不能仅打印sent并隐藏回执字段

### Requirement: SL-007 恢复有界且与模型执行隔离
outbox MUST 有单writer/租约、受限退避与容量/期限，可查询每次恢复及可见终局；幂等栅栏须覆盖全部允许重放期，清理后过窗ID须拒绝而非当新业务。自动恢复SHALL只重交Hub发送意图，不调用目标CLI重做已入场请求；超期/容量不能伪造成功或覆盖未知入场证据。

#### Scenario: SL-S013 收件已准入后重复回执恢复
- **WHEN** 原收件项已injected/processed而发送方仍等Hub确认
- **THEN** 只恢复原发送确认，不新建delivery或turn，不改变收件HCD/HRI unknown语义

#### Scenario: SL-S014 重放窗到期或账本归档
- **WHEN** 意图超期或Hub清理原完整delivery正文
- **THEN** 保留足够唯一栅栏/过窗拒绝规则与可见原因，不能删除后把旧source号重新作为新消息接受

### Requirement: SL-008 脱敏发端与Hub对账
系统 MUST 为发送意图、写出边界、接管/拒绝/未知和Hub裁决留下可关联的脱敏元数据及部署/保存期信息；按认证sender/source身份关联，不搜正文当原信。规模报告SHALL分开已确认接受、明确未提交、未匹配未知和证据缺口，Hub单侧不能产生丢失分母，日志缺失不得当零丢失。

#### Scenario: SL-S015 七天窗口对账
- **WHEN** 已取得完整授权的发端意图和Hub结构化记录，或其中一方保存不全
- **THEN** 按唯一意图列接受/未提交/未知及覆盖率，缺证部分明确标缺，不将不确定候选冒称已确认丢失
