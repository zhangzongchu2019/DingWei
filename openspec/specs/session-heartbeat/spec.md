# session-heartbeat Specification

## Purpose
heartbeat_report 自报经已认证会话 WS 的保留控制子类型上报，能力协商、受限通道拒绝与计费前丢弃规则使该 MCP 工具返回确定结果。

## Requirements

### Requirement: SHB-001 能力协商与受限通道拒绝

helper MUST 只在会话握手响应头含 `X-DW-Session-Heartbeat: session-heartbeat/v1` 时发送 `session_heartbeat_v1`；缺该头时 MUST 返回确定的 `unsupported` 且 MUST NOT 发出任何帧，因为旧 Hub 会把未知控制子类型当普通会话消息路由。Hub MUST 只对凭据准入的 selfhost 连接下发该头。受限 IPC 套接字（`upload_only`／`send_body_only`）MUST 拒绝 `heartbeat_v1`，不因新增子类型获得新通道；未接处理器时 MUST 返回 `unsupported` 而不是 `helper_rejected`。

#### Scenario: SHB-S001 旧 Hub 不收到未知子类型
- **WHEN** 握手响应头没有该能力头，模型调用 `heartbeat_report`
- **THEN** 工具返回 `ok=false, status=unsupported`，且该连接上没有任何 `session_heartbeat_v1` 帧

#### Scenario: SHB-S002 selfhost 准入下发能力头
- **WHEN** 凭据准入的 selfhost 会话完成握手
- **THEN** 响应头含 `X-DW-Session-Heartbeat` 且值等于 `session-heartbeat/v1`

#### Scenario: SHB-S003 受限套接字与缺处理器
- **WHEN** 在 `upload_only` 或 `send_body_only` 套接字上请求 `heartbeat_v1`，或套接字未接心跳处理器
- **THEN** 请求失败或返回 `unsupported`，处理器不被调用，不发送任何会话消息

### Requirement: SHB-002 固定 schema 与双侧参数校验

自报正文 MUST 是 schema 精确等于 `session-heartbeat/v1` 的单一 JSON 对象；`status` MUST 属于 active／busy／blocked／idle，`task`／`eta`／`blocker` MUST 为字符串、去空白后不超过 128 码点且不含控制字符。未知键、空正文、非 JSON、尾部多余内容或超过 4096 字节的正文 MUST 拒绝。shim MUST 在产生 IPC 请求之前拒绝非法参数，IPC 监听器 MUST 拒绝带未知键的 `heartbeat_v1` 请求，Hub MUST 以 `DisallowUnknownFields` 与尾部检查独立复核，MUST NOT 信任任一侧。拒绝是确定结果：Hub 回 `ok=false, status=invalid`，处理器抛 `ValueError`／`TypeError` 时监听器回 `invalid`，两者都 MUST NOT 写入样本。

#### Scenario: SHB-S004 非法参数不进入连接
- **WHEN** status 为 paused／空／非字符串，或文本超 128 码点、含换行，或参数带未知键
- **THEN** shim 直接失败，不产生 IPC 请求，连接上无帧

#### Scenario: SHB-S005 Hub 逐类拒绝且不落样本
- **WHEN** Hub 收到 unknown_status／empty_status／wrong_schema／unknown_field／trailing_garbage／oversize_text／control_char／empty_body／not_json 中任一种正文
- **THEN** 回 `ok=false, status=invalid`，且该连接的读出口仍为 `unsupported`

#### Scenario: SHB-S006 满长度文本逐字节保留
- **WHEN** task／eta／blocker 各为 128 个汉字
- **THEN** 回 `recorded`，读出口的每个字段按字节长度等于 384 且内容不变

#### Scenario: SHB-S007 IPC 层拒绝未知键
- **WHEN** 直接向 IPC 套接字写 `type=heartbeat_v1` 且带额外键的请求
- **THEN** 监听器返回失败，处理器不被调用

### Requirement: SHB-003 就地消费、连接身份与关联应答

Hub MUST 在会话接收路径就地消费 `session_heartbeat_v1`，即使拒绝也 MUST NOT 让它进入会话环、AMR、收件箱、投递 ACK、成本管线或任何对端；同组其它连接 MUST 看不到该子类型与其应答。上报者身份 MUST 只取已认证连接，MUST NOT 读正文或信封中自称的名字。写样本前 MUST 校验该连接的凭据绑定身份。应答 MUST 以 32 位十六进制 nonce 关联，helper MUST 只在 nonce、连接对象、发送方为 workpulse、目标为本席、类型为结果子类型且 result 为对象全部匹配时解析；解析结果 MUST 限于 ok／status／retry_after_ms／recorded_at_ms，Hub 附加字段 MUST NOT 透出。无法关联的报告（nonce 非法或长度不足）、被顶替的连接与超界正文 MUST 在计费与身份查询之前静默丢弃：不应答、不写样本、不计入限流尝试。不应答 MUST 由计费点之前的首道连接复核保证；写样本前的第二道复核只是缓存防线，MUST NOT 被当成不应答的保证。

#### Scenario: SHB-S008 应答只回到上报连接
- **WHEN** 一次自报被记录
- **THEN** 只有上报连接收到 `session_heartbeat_result_v1`，nonce 相同、meta 标 `no_mirror` 与 `system`，同组对端在该窗口内收不到任何信封

#### Scenario: SHB-S009 静默丢弃
- **WHEN** nonce 非法或长度不足、正文超过 4096 字节、连接已被顶替或连接对象为空
- **THEN** 子类型被消费、不应答、不落样本，读出口不为 `ok`

#### Scenario: SHB-S010 篡改应答不解析
- **WHEN** 应答的 nonce／连接／发送方／目标／类型／result 形状任一不符
- **THEN** 等待方不被解析，最终以 `unknown` 结束，且连接上只发过一帧

#### Scenario: SHB-S011 固定应答字段
- **WHEN** Hub 应答夹带 session／token／note 等额外字段
- **THEN** 工具结果只含 ok／status／recorded_at_ms

#### Scenario: SHB-S012 绑定校验不通过不能写样本
- **WHEN** 连接的凭据绑定校验任一项不通过时上报（`metricsAgentID` 缺失、Agent 非 active 或非 selfhost、`key_id`／会话名不符、属主非 active）
- **THEN** 回 `ok=false, status=unavailable`，且不写任何样本

#### Scenario: SHB-S022 被顶替连接对三类正文都静默且不计费
- **WHEN** 注册表已指向替代连接、而旧连接的读循环仍在服务时，旧连接分别发送合法正文、语义不合法正文与超过 4096 字节的正文
- **THEN** 三格都不应答（旧连接与替代连接在整个观察窗内都收不到心跳结果或错误信封）、都不写样本、`lastHeartbeatAttempt` 保持零值，读出口不为 `ok`；只靠写样本前的第二道连接复核时，语义不合法那一格会漏出 `invalid` 应答

### Requirement: SHB-004 限流、在途上限与确定负结果

Hub MUST 对每个连接限制自报为每 1 秒最多一次，并 MUST 在语义校验与身份查询之前计费尝试次数；被限流者 MUST 收到确定的 `ok=false, status=throttled` 与 `retry_after_ms`（大于 0 且不超过 1000），且 MUST NOT 覆盖已存样本。计费点之前只有 SHB-003 的三道闸门——超过 4096 字节的正文、非法 nonce、非当前连接；被这三道静默丢弃的帧 MUST NOT 计费，否则一条畸形帧就能把同一连接随后的合法自报挤进限流窗口。「尝试先计费」的准确范围是：通过上述三道之后的语义校验与身份查询之前必须计费，使一条连接无法用非法正文把 Hub 拖进无界的身份解析。helper MUST 限制同时在途的自报数量，超限返回确定的 `busy` 且不发帧。IPC 监听器 MUST 在 5 秒内等待处理器，超时返回 `unknown`。以上负结果 MUST 是确定值，MUST NOT 被折叠成 `helper_rejected`。

#### Scenario: SHB-S013 一秒内第二次被限流
- **WHEN** 记录成功后立即再次上报
- **THEN** 回 `throttled` 且 `retry_after_ms` 在 0 到 1000 之间，读出口仍是第一次的样本

#### Scenario: SHB-S014 在途上限
- **WHEN** 已有 8 个未决自报
- **THEN** 新请求立即返回 `busy`，连接上不发帧

#### Scenario: SHB-S015 慢处理器
- **WHEN** 处理器超过 5 秒未返回
- **THEN** IPC 结果为 `unknown`，不是 `helper_rejected`

#### Scenario: SHB-S023 语义不合法帧先计费
- **WHEN** 当前连接先发语义不合法正文并拿到 `invalid`，随即在 1 秒内发合法正文
- **THEN** 第二次回 `throttled` 且 `retry_after_ms` 在 0 到 1000 之间，两帧都不写样本，且该连接的 `lastHeartbeatAttempt` 已被第一帧置位

#### Scenario: SHB-S024 超界正文不计费
- **WHEN** 当前连接先发超过 4096 字节的正文（静默丢弃），随即在 1 秒内发合法正文
- **THEN** 第一帧无任何应答，第二帧回 `recorded`、nonce 与其对应，并可从读出口取回；超界帧没有占用该连接的 1 秒额度

### Requirement: SHB-005 无应答即 unknown 且绝不自动重发

缺少应答 MUST NOT 被当成成功，也 MUST NOT 被当成确定失败：helper MUST 返回 `ok=false, status=unknown`，MUST NOT 自动重发同一自报，并 MUST 清理 nonce 映射。发送异常、等待被取消与连接断开同样 MUST 以 `unknown` 结束；连接断开时 helper MUST 释放该连接上所有等待方为 `unknown`。工具描述 MUST 说明自报只保留本连接最新一份、不写历史。

#### Scenario: SHB-S016 无应答
- **WHEN** 发出后到超时仍无应答
- **THEN** 返回 `unknown`，连接上只发过一帧，待决映射为空

#### Scenario: SHB-S017 断连释放等待方
- **WHEN** 等待期间连接断开
- **THEN** 等待方立刻得到 `unknown`，不重发，待决映射清空

#### Scenario: SHB-S018 发送失败与取消
- **WHEN** 发送抛连接已关闭，或等待被外部取消
- **THEN** 前者结果为 `unknown`、后者取消向外传播，两者都只发过一帧且不重发

### Requirement: SHB-006 业主侧只读出口与陈旧语义

自报 MUST 只存于 Hub 进程内并绑定该已认证连接，MUST NOT 落库、不新增表或迁移；Hub 重启或换连接后 MUST 不保留，也 MUST NOT 把缺失渲染成新鲜的零值。业主侧读取 MUST 复用既有 View Service 签名路由，作为追加字段 `session_heartbeat` 返回，MUST NOT 挤掉 `agent_disk` 与 `inbox_lag`，响应 MUST 仍满足 4096 字节硬界（超界返回 503）。读取 MUST 校验凭据绑定、full_name 与 agent_id 一致、连接未被顶替与属主 active，任一不满足返回 `unknown`；无自报返回 `unsupported`。读取 MUST NOT 续期样本；超过 30 分钟 MUST 标记 `stale=true` 但样本仍可见。

#### Scenario: SHB-S019 追加字段与体积硬界
- **WHEN** 以满长度 CJK 自报请求业主签名路由
- **THEN** 返回 200、响应不超过 4096 字节，`agent_disk` 与 `inbox_lag` 仍在，`session_heartbeat.report` 逐字节等长

#### Scenario: SHB-S020 缺失与陈旧
- **WHEN** 连接从未自报，或自报已超过 30 分钟
- **THEN** 前者读出口 `status=unsupported`；后者 `status=ok` 且 `stale=true`，`observed_at_ms` 不因读取而改变

#### Scenario: SHB-S021 未授权或换连接不可读
- **WHEN** 凭据绑定为空、agent_id 被改成他人、连接被顶替或属主被停用
- **THEN** 读出口为 `unknown` 或 `unsupported`，不返回他人或旧连接的样本
