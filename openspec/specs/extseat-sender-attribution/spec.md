# extseat-sender-attribution Specification

## Purpose
本能力为外部 MCP 席位投递附加由 Hub 依据已核验身份生成的来源抬头与 meta（origin_kind=extseat 等），使接收方识别来源并按全名回信；发信方可用 expect_reply 请求自动回程，普通会话无法伪造该来源标记。涉及 extseat 投影和 m8 路由，不改变身份生命周期与 View UI。

## Requirements

### Requirement: SR-001 外部席位消息署名

Hub MUST 在外部席位消息正文前加入由事务内核验的外部席位全名、授权人显示名、目标全名生成的固定抬头；授权人显示名为空时 MUST 使用授权人短名。抬头不得包含随时间变化的字段。

#### Scenario: 外部消息投影

- WHEN Hub 投影已核验的外部席位 delivery
- THEN 接收 envelope 正文以指定格式抬头和单个换行开头，之后为原正文

#### Scenario: 授权人显示名缺失

- WHEN PG 核验身份没有授权人显示名但具有短名
- THEN 抬头授权人字段使用该短名且不为空

### Requirement: SR-002 外部来源元数据

Hub Project MUST 在外部席位投影 envelope 中写入 `origin_kind=extseat`、`source_display_name` 为外部席位全名和布尔值 `source_external=true`。普通 session 输入 MUST NOT 通过正文或客户端 meta 伪造这些 Hub 来源标记。

#### Scenario: 普通 session 伪造来源

- WHEN 普通 session 发送以伪造外部席位抬头开头且携带 extseat meta 的正文
- THEN 接收信封不含 `origin_kind=extseat` 来源标记

### Requirement: SR-003 投影幂等

Hub MUST 对相同 delivery ID 保持投影 envelope 的逐字节内容一致，并继续拒绝同 ID 不同目标或内容。

#### Scenario: 相同 delivery 重投

- WHEN 相同 delivery 被连续投影两次
- THEN 持久信封字节完全一致

### Requirement: SR-004 外部席位回复路由

Hub MUST 接受接收方以抬头所列外部席位全名发出的合规回复，并将回复投递到该外部席位；外部 Wait 返回的 From MUST 是目标 Agent 的全名。

#### Scenario: 回复到外部席位

- WHEN 真实 session 通过 Hub 对外部席位全名发回复
- THEN 真实 extseat Wait 可收到回复且 From 为发送 session 全名

### Requirement: SR-005 外部席位自动回信选择

`send_message` MUST 接受可选布尔参数 `expect_reply`，未提供wait_reply_seconds时缺省false，提供时缺省true，显式false优先。A MUST 在 outbox 中持久化该选择，并将其纳入同一 `client_message_id` 的 request_hash；同一 ID 的选择改变时 MUST 返回幂等冲突。Hub MUST 仅在值为 true 时于投影 envelope meta 写入 `reply_policy=expect_reply`。false 时 MUST 不自动回程。

#### Scenario: 明确请求自动回信

- WHEN 外部席位以 `expect_reply=true` 发送并且接收 Agent 回合产生无提及输出
- THEN 生产 reply-origin 路径 MUST 将该输出回投至外部席位，Wait 返回且 From 为接收 Agent 全名

#### Scenario: 缺省不自动回信

- WHEN 外部席位省略 `expect_reply` 且接收 Agent 回合产生无提及输出
- THEN 外部 Wait MUST 不收到该回合的自动回信

#### Scenario: 幂等参数冲突

- WHEN 同一 `client_message_id` 在重试时把 `expect_reply` 从 false 改为 true 或反向改变
- THEN A MUST 返回幂等键冲突，且不得产生第二条投递

### Requirement: SW-001 同步等待回复
send与send_message MUST 接受数值wait_reply_seconds，缺省0；提供该字段时expect_reply缺省true，显式false MUST 保留。大于0时 MUST 在同次调用等待本AG当前目标且created_at不早于受理时间的首条可领取消息，自动received成功后返回Wait同字段的单条reply。超时 MUST 返回受理结果、reply:null、timed_out:true及人类hint，MCP文本同样包含提示。hint MUST 提示稍后recv或先check。同client_message_id重试 MUST 不重发且可重新等待。

#### Scenario: 超时后查收
- WHEN 已受理发送在等待窗口内没有合格来件
- THEN 返回timed_out:true和recv/check提示，已受理消息不重复发送

### Requirement: SW-002 轮询与取消
同步等待 MUST 只在入口鉴权续期；每次空唤醒 MUST 仅做无锁只读重核与查询，不持有PG或SQLite长锁、不续组。领取使用短事务租约。请求context取消 MUST 立即结束。

#### Scenario: 空轮询
- WHEN 2秒等待没有合格消息
- THEN token、队列和租约无轮询产生的写入

### Requirement: SW-003 主工具短名
MCP与REST MUST 提供send/recv/check/list/clear；send_message、wait_for_messages、list_online MUST 保留原行为并说明新名，ack MUST 保持。check_messages MUST 不发布。recv MUST 保持原Wait的25秒缺省和上限及租约确认语义。

#### Scenario: 旧名兼容
- WHEN 相同令牌与参数分别调用send/send_message、recv/wait_for_messages、list/list_online
- THEN 两者产生相同语义与数据

### Requirement: SW-004 只查不取
check MUST 立即返回当前待收count与delivery_id/from全名/created_at/body_bytes摘要，MUST NOT 领取、改变队列或租约或TouchGroup。

#### Scenario: 查询已租约消息
- WHEN 队列同时包含queued和leased消息
- THEN check列出两者且调用前后队列租约记录逐字节相同

### Requirement: SW-005 确认清空待收
clear MUST 要求confirm:true，否则拒绝；确认后 MUST 将本席queued及leased行标received，返回count与delivery_ids，重复返回0，MUST NOT 影响发出方向。

#### Scenario: 确认与幂等
- WHEN 先无confirm调用再有confirm调用并重复
- THEN 依次拒绝、清空本席待收、返回0，outbox保持

### Requirement: SW-006 固定scope映射
服务端 MUST 将send映射send_message、recv/check映射wait_for_messages、list映射list_online、clear映射ack；MVPScopes与PG校验 MUST 保持原固定四项，未映射工具 MUST 拒绝。

#### Scenario: 旧令牌
- WHEN 持有原四scope的已授权令牌调用任意短名
- THEN 按同一scope执行；撤权令牌和无映射名称仍拒绝

### Requirement: SW-007 自动回程提示
外部expect_reply消息抬头 MUST 由服务器生成提示；开启时为「对方请求自动回程：本轮回复将原样发给该外部 Agent」，关闭时为「对方请求自动回程，本席位未开启，请按全名手动回复」。提示 MUST 计入1MiB正文预算。

#### Scenario: 两种提示
- WHEN 同一外部请求分别投向允许与拒绝外部自动回程的席位
- THEN 持久投影分别含对应提示，客户端不能选择提示状态

### Requirement: SW-008 外部回程拒绝
服务端 MUST 通过配置键提供默认关闭名单，缺省包含opus50tc2k.zzc、prodops.zzc；回程生成与外部路由出口 MUST 都按接收席位检查。无法取得完整接收席位身份时 MUST 拒绝自动回程；拒绝时 MUST NOT 自动发出本轮内容，但仍允许接收方显式全名回信。PG/Console持久化 MUST 在方案确认后另行实现。

#### Scenario: 出口防绕过
- WHEN 已拒绝外部自动回程的席位产生自动回复信封并直接进入路由出口
- THEN 外部队列无新增，手动全名发送仍可到达
