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
- **THEN** 不改投替代席位、不泄露原裁决或正文；明确未接管的首笔尝试可落rejected，先前写出/接纳已不确定的意图仍保留unknown与原ID/正文，不能把本次权限拒绝推成历史未接管证明

#### Scenario: SL-S016 原回执丢失后权限拒绝
- **WHEN** 首次Hub可能已提交但回执丢失，原意图随后收到当前权限拒绝
- **THEN** helper保留unknown及原ID/正文，不伪造历史未接受；权限恢复时仍只能恢复同ID原回执，不创建第二业务意图

### Requirement: SL-006 调用方失败语义不掩盖
send.py与MCP MUST 保留结构化phase、accepted_by、request/source/delivery ID及reason；unknown或rejected不得打印无条件sent成功或仅因IPC ok返回成功语义。caller_journal_full MUST rejected、reason=caller_journal_full、retryable=true；idempotency_conflict MUST rejected、同名reason、retryable=false，两者CLI/MCP均带request_id，不得折为JSON-RPC invalid arguments。临时且可证未接管失败SHALL可用原意图重试；已可能提交/IPC未确认时须先查询，不能换通道或生成新ID自动发送。

#### Scenario: SL-S018 协商超时与旧未知保持
- **WHEN** 当前WS十秒内未给出持久恢复能力或明确不支持，随后升级为支持的新代际
- **THEN** 不向未协商连接写业务，未尝试项明确拒绝，先前未知项保留原ID/正文且不自动补送；既有成功裁决仍可本地查询，不因当前能力拒绝改写历史

#### Scenario: SL-S012 helper返回unknown或拒绝
- **WHEN** IPC传输成功但业务结果为unknown或rejected
- **THEN** CLI/MCP明确显示对应非成功结果和查询键，不能仅打印sent并隐藏回执字段

#### Scenario: SL-S022 调用方账本拒绝可观测
- **WHEN** caller账本达到窗内容量，或同request_id出现不同摘要
- **THEN** CLI与MCP均返回含request_id/reason/retryable的rejected；满额可重试、摘要冲突不可重试，MCP不是-32602，且未向IPC提交正文

### Requirement: SL-007 恢复有界且与模型执行隔离
outbox MUST 有单writer/租约、受限退避与容量/期限，可查询每次恢复及可见终局；helper在24小时恢复窗外MUST停止自动重交。Hub栅栏SHALL默认保留7天，可配且启动时须大于24小时加2小时时钟/重连余量；容量只计窗内行。仅超期且不再承担未终结投递/失败通知身份绑定的栅栏可有界清理，窗内裁决不得丢。自动恢复仅重交发送意图，不调用目标CLI重做；超期/容量不得伪造成功或清未知正文。幂等保证限于此自动恢复合同，不宣称任意人为越窗直投仍有永久裁决。

#### Scenario: SL-S013 收件已准入后重复回执恢复
- **WHEN** 原收件项已injected/processed而发送方仍等Hub确认
- **THEN** 只恢复原发送确认，不新建delivery或turn，不改变收件HCD/HRI unknown语义

#### Scenario: SL-S017 全离线期间期限收口
- **WHEN** 已接管意图始终离线且达到24小时恢复期限
- **THEN** 即使没有WS也更新可见原因；明确未写出者rejected，曾尝试者unknown并保留原正文与ID，不无限停留local_queued

#### Scenario: SL-S014 重放窗到期或账本归档
- **WHEN** 意图超过helper自动恢复窗，或Hub清理原完整delivery正文
- **THEN** helper停止自动重交并保留unknown证据；Hub在栅栏保留窗内仍恢复原裁决，窗内不得因清正文重建业务，窗外GC只按本需求的期限与未终结绑定保护执行

#### Scenario: SL-S019 过期栅栏释放与窗内去重
- **WHEN** 历史栅栏达到容量上界但均已超保留期，且有新的合法意图到达
- **THEN** 容量只计算窗内行，新意图可原子受理；一次事务最多清256个可清超期行，边界等于截止时刻及窗内裁决保留且同ID恢复原delivery

#### Scenario: SL-S020 启动校验保留窗与时钟余量
- **WHEN** 启动配置保留期为空、有效，或不大于24小时加2小时余量/超过30天/格式非法
- **THEN** Hub与helper均默认使用168小时，保留期必须大于26小时且不超过720小时，helper以SH_OUTBOUND_CALLER_RETENTION_SECONDS整数秒配置；有效配置生效；不安全或非法值拒绝启动且不输出原始配置值，栅栏年龄由Hub接受时钟确定

#### Scenario: SL-S021 超期但仍在途的身份绑定
- **WHEN** 超期栅栏仍绑定未终结投递或待发HUB失败通知
- **THEN** 先保留该稳定身份权威且不计入窗内容量；相关投递/通知终结后才可按有界GC清理，不因GC改投同名替代身份

#### Scenario: SL-S023 调用方旧库迁移与窗内去重
- **WHEN** 旧request表只有id/digest，或保留窗内再次使用同ID
- **THEN** 原子补created并以迁移当前时刻保留旧行/摘要，窗内同ID原映射不变；未知outbox关联不得当无未终结责任

#### Scenario: SL-S024 caller超期清理保留未终结请求
- **WHEN** 新调用方意图到达且有超期caller记录，其中outbox仍有local_queued/unknown
- **THEN** 只计窗内配额、每次至多清256个已超期且非未终结ID；读取outbox未终结快照和删除之间阻止并发写穿越，outbox缺失/不可读/锁忙则不清理；不删除未终结或窗内行

#### Scenario: SL-S025 helper终态历史释放容量
- **WHEN** helper outbox有大量过期hub_durable_queued/rejected历史且新意图到达
- **THEN** 记录配额只计保留窗内、每次最多清256过期终态，local_queued/unknown和窗内/边界行保留，不因历史永久拒新；未终结正文数量/字节仍受原硬界约束

### Requirement: SL-008 脱敏发端与Hub对账
系统 MUST 为发送意图、写出边界、接管/拒绝/未知和Hub裁决留下可关联的脱敏元数据及部署/保存期信息；按认证sender/source身份关联，不搜正文当原信。规模报告SHALL分开已确认接受、明确未提交、未匹配未知和证据缺口，Hub单侧不能产生丢失分母，日志缺失不得当零丢失。

#### Scenario: SL-S015 七天窗口对账
- **WHEN** 已取得完整授权的发端意图和Hub结构化记录，或其中一方保存不全
- **THEN** 按唯一意图列接受/未提交/未知及覆盖率，缺证部分明确标缺，不将不确定候选冒称已确认丢失
