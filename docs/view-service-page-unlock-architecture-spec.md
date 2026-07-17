# View Service 页级解锁架构技术规格

状态：技术设计稿。

## 1. 目标与非目标

### 1.1 目标

- 将新 View 页的“页面码、页级写授权、页级浏览器隔离”从 Hub 的易失 `terminalState` 迁到 View Service（VS）。
- 页面码使用 `<完整会话名>-<8 位大小写字母或数字>`，例如 `example-coordinator-0001-a3Kf9xQ2`。这里的前缀是完整会话名，不只是 owner key；它同时提供确定路由，随机后缀只负责页实例唯一性。
- 页面码不是秘密。唯一的人员授权依据是：发送 `#unlock` 的飞书 open_id 所绑定 owner，必须等于目标会话的权威 owner。
- 同一 owner 可以同时解锁同一会话或不同会话的多个页面；每页写权互不转移、互不撤销。
- helper WebSocket 断开、重连及 `closeTerminalLocked` 不再影响页面码或 VS 页级授权。
- 旧短码/老 xterm/未启用 v2 的会话行为保持不变。

### 1.2 非目标

- 8 位随机后缀不承担密码或 bearer token 的功能。
- 本批不移除老 xterm 的 `writerPage/writerToken`。
- 本批不把浏览器鉴权托付给 Hub，也不允许浏览器直接调用 Hub 内网输入接口。
- 本批不以缩短轮询或延长 Hub viewer TTL 代替架构迁移。

## 2. 当前实现与安全核查

### 2.1 当前短码路径

1. `internal/m8/terminal.go:932` 附近的 `dispatchTerminalInputCommand` 解析 `#unlock`。
2. 未显式指定会话时，它调用 `findTerminalViewerByCode`（`terminal.go:1040` 附近）在 Hub 内存 `h.terminals[*].viewers` 中找短码。
3. 找到后调用 `grantTerminalInput`（`terminal.go:1180` 附近），写入单值 `writerPage/writerToken/writerUntil`，并撤销其他页面。
4. VS 通过 `/internal/view/authorize` 查询该 viewer 的 token，再经 `/internal/view/{session}/input` 输入。

当前虚拟 viewer 由 `registerViewViewer` 创建（`terminal.go:652` 附近），查询写权会在 `viewWriteState`（`terminal.go:701` 附近）刷新 `lastSeen`。TTL 是 `terminalTokenTTL = 8h`（`terminal.go:31`），不是 first-fail 的直接原因。helper 当前连接退出时，`internal/m8/hub.go:888-900` 调用 `closeTerminalLocked`；后者在 `terminal.go:108-121` 删除整个 terminal state，页面码随之丢失。

### 2.2 当前是否已有 owner 校验

有，但它不是一个独立的、可复用的明确门，而是短码候选查找的一部分：

- `dispatchTerminalInputCommand` 用 `sourceAccount(msg)` 取得飞书来源。
- `findTerminalViewerByCode` 先检查 `h.keyAccounts[st.keyID][account]`；不直接命中时，要求 `ownerKeyForAccount(ctx, account) == ownerKeyForKey(ctx, keyID)`，否则该 viewer 不进入可命中候选。
- `ownerKeyForAccount`（`terminal.go:1110` 附近）优先扫描 active member 并用 `accountBelongsToMember` 匹配；其次读取 `GetChatEntity(botChannelID, feishuID).BoundOwner`。
- `ownerKeyForKey`（`internal/m8/hub.go:3518` 附近）从 API key 绑定账号、成员和 chat entity 绑定推导 key owner。
- 群消息的真实发信人由 `sourceAccount`（`hub.go:4920` 附近）使用 `BotChannelID + ":personal:" + SenderOpenID`，不是群 open_id。

结论：现有路径有 owner 过滤，不能说“完全没有”；但新模型不能再依赖“找短码时顺便过滤”。必须新增显式 `authorizeViewUnlockOwner`，并以失败即拒绝为准则。它是新模型中唯一的人身授权门。

## 3. 信任边界与最终写授权落点

### 3.1 三层责任

| 层 | 权威职责 | 不负责 |
|---|---|---|
| Hub | 飞书发信人 owner 校验；验证 VS 服务身份；确认目标会话并转发输入 | 页面码、浏览器页身份、哪一页已解锁 |
| View Service | 页面码与页记录；浏览器页 token；页级 `can_write`、过期和撤销；输入放行 | 判断飞书人员是否是 owner |
| Browser tab | 保存本 tab 的不可猜 page token；展示公开 page code | 自行声明已解锁、把 page code 当密码 |

### 3.2 Hub 为什么信任 VS 转发的输入

新模式使用独立的内部输入接口，例如：

`POST /internal/view-v2/{session}/input`

浏览器不能访问该 mux。VS 请求还必须带服务级 HMAC，不能只依赖 `127.0.0.1`：

- 每个 View 路由 target 配置独立共享密钥；Hub 与对应 VS 通过 secret file/env 读取，密钥不进入路由 JSON、日志或页面。
- 请求头包含 `X-DW-VS-Target`、Unix 时间、随机 nonce、请求 ID、HMAC-SHA256。
- 签名原文固定为 `method + path + timestamp + nonce + sha256(body)`；Hub 接受窗口建议 ±60 秒，并以 `(target, nonce)` 做至少 5 分钟有界去重。
- HMAC 验证、目标 target 与 session 路由一致性、会话在线性全部通过后，Hub 才调用新的 `routeViewServiceInput`。该函数只绕过旧 `writerPage/viewer token`，不绕过 session 在线、消息大小、审计、队列或 helper 路由检查。
- body 包含 `session`、`page_id`、`request_id`、`text`；`page_id`只用于审计和追踪，不作为 Hub 的授权依据。

因此，浏览器提交必须先经过 VS 对 page token 和页级授权的检查；Hub 信任的是签名后的 VS 服务身份，不信任页面码或浏览器字段。VS 被攻破等价于其 target 内会话写能力被攻破，故必须按 target 分密钥以隔离故障域。

### 3.3 旧 `writerPage` 如何退化与兼容

- legacy/短码端点继续使用现有 `writerPage == viewer.id`、`writerToken` 和“新页抢走旧页写权”语义，代码路径不改。
- `view_unlock_v2` 会话不创建 Hub 虚拟 viewer，不查询 `writerPage`，也不调用现有 `/internal/view/{session}/input`。
- 新接口只接受具备 `view_unlock_v2` capability 且 HMAC 合法的 target。
- 不允许同一页面同时尝试 v1 和 v2；VS `/status` 明确返回 `auth_mode: "view_page_v2"` 或 `"hub_viewer_v1"`。
- Hub 中的单值 writer 状态不能成为 v2 页之间的共享锁。多页并行写的最终排队顺序由现有统一队列负责，页级授权只决定是否允许提交。

## 4. 显式 owner 认证

### 4.1 发信人 owner

新增 `authorizeViewUnlockOwner(ctx, msg, sessionName) (ownerKey, senderOpenID, error)`：

1. 从 `msg.SenderOpenID` 取得真实发信人；群聊必须有 `SenderOpenID`。仅在个人消息兼容路径中，才允许从 `sourceAccount(msg)` 解析 open_id。无法得到 open_id 时失败。
2. 用现有 `ownerKeyForAccount(ctx, sourceAccount(msg))` 解析 active member 或 active chat entity 的 `BoundOwner`。
3. 禁止仅凭 display name、群 open_id、页面码前缀或会话名字符串认人。

### 4.2 目标会话 owner

目标 owner 必须来自持久的会话端点/Key 绑定，而不是只从 `<owner>-<short>-<suffix>` 文本猜测：

1. 精确查 `Repository.ListSessionEndpoints` 中 `FullSessionName == sessionName` 的唯一 endpoint；优先使用非空 `OwnerKey`。
2. 若 endpoint 的 `OwnerKey` 为空，则由 endpoint `KeyID` 调用现有 `ownerKeyForKey` 推导。
3. endpoint 不唯一、owner 为空、绑定冲突或 Repo 查询失败均 fail closed。
4. `ownerFromSessionName(sessionName)` 仅作一致性校验和路由提示；若它与权威 owner 不一致则拒绝并审计，不能反过来成为授权依据。

该持久 endpoint 解析允许 helper 正处于短暂重连窗口时仍验证 owner。解锁页记录可以成功，但输入仍须等待 Hub 判断 session 在线。

### 4.3 判定与审计

只有 `senderOwner == targetOwner` 才继续。没有默认管理员绕过；如未来需要代理授权，必须单独设计 capability 和审计，不能复用模糊 owner 推导。

审计至少记录：command/message ID、sender open_id、sender owner、target session、target owner、route target、page code 的不可逆摘要、结果和拒绝原因。日志不得记录 page token、HMAC secret 或完整输入正文。

## 5. 页面码、页身份与生命周期

### 5.1 页身份创建

- renderer 每个 tab 持有 `page_id` 和 256-bit `page_token`；token 是真正的浏览器持有凭据，绝不放 URL、日志或页面可复制区。
- 首次载入由 VS 创建，返回一次 page token；VS 只存 `SHA-256(page_token)`。
- 浏览器将二者放在 `sessionStorage`，刷新沿用；新浏览器没有该 token，因此即使看到同一会话 URL，也会得到新的 locked 页。
- 浏览器从现有 tab 复制/打开新 tab 时部分 Chrome 行为会克隆 `sessionStorage`。renderer 必须用 `BroadcastChannel` 做同源 page-id 占用握手；发现同一 `page_id` 同时活跃时，新 tab 重新向 VS 申请页身份，禁止两个 tab 共用 token。

### 5.2 页面码

- 格式：`<full_session_name>-<suffix>`；suffix 从 62 字符集用 CSPRNG 拒绝采样生成 8 位，约 47.6 bit，仅作为唯一标识。
- VS SQLite 对 `code` 建唯一索引；碰撞重试。匹配大小写敏感，Hub v2 parser 不得像旧短码那样 `ToUpper`。
- Hub 先按严格格式拆出完整 session，再按路由配置找到负责该 session 的 v2 target；真正的 code 是否存在由 VS 决定。

### 5.3 VS 持久状态

建议独立 SQLite 表 `view_pages`：

| 字段 | 说明 |
|---|---|
| `page_id` | 随机稳定 ID，主键 |
| `session_name` | 完整会话名 |
| `code` | 唯一公开码 |
| `page_token_hash` | 浏览器 token 摘要 |
| `state` | `locked/unlocked/revoked/expired` |
| `created_at/last_seen_at` | ISO-8601 UTC |
| `unlocked_at/unlock_expires_at` | 页级授权时间 |
| `last_write_at` | 8 小时无输入回收依据 |
| `disconnected_at` | SSE/页面心跳丢失起点 |
| `grant_owner` | 授权 owner，用于审计 |

所有状态转换使用事务/CAS。例如只允许 `locked → unlocked`、当前 token 匹配的 `unlocked → revoked`，重复 unlock command ID 幂等成功。

### 5.4 关闭、断流与过期

- 页面刷新：同一 `page_id + page_token` 在断连宽限内恢复，码和写权不变。
- helper WS churn：不改变任何 VS 页状态。
- 浏览器 SSE 断开：VS 标记 `disconnected_at`，不立即删除；重连取消标记。建议 5 分钟宽限，覆盖刷新和短网抖。
- `pagehide/sendBeacon` 只作加速提示，不能作为唯一关闭证据。
- locked 且从未解锁的孤儿页建议 30 分钟无心跳后过期。
- unlocked 页按“最后一次成功输入”续 8 小时；仅 `/status`、SSE ping 或后台 tab 存活不得延长写授权。
- 断连超过 5 分钟的页立即撤销写权；记录可再保留 24 小时用于审计后清理。具体时长做环境配置，并有有界清扫测试。

## 6. Hub → VS 解锁信号

采用 Hub 主动 POST，不采用 VS 轮询：

1. Hub v2 parser 识别长码但不修改大小写。
2. 从码解析 session，确认该 session 路由到启用 `view_unlock_v2` 的 target。
3. 执行第 4 节的显式 owner 校验。
4. Hub 以反向 HMAC 调用该 target 的内部控制口：`POST /internal/control/view-page/unlock`。
5. body：`command_id`、`session`、`code`、`owner_key`、`sender_open_id`、`granted_at`。VS 事务内校验 code/session、页未过期后设为 unlocked。
6. VS 返回 200 后 Hub 才向飞书回复成功；404=码不存在，409=页已过期/撤销，503=VS 暂不可达。不得先回成功再异步补写。

Hub→VS 与 VS→Hub 使用方向不同的派生签名 key，或至少在签名原文加入固定 direction，避免请求重放到反向接口。VS 对 `command_id` 建幂等表/唯一索引。

`#lock <长码>` 使用对称 revoke 控制口。owner 的“锁定我名下所有页”需要 Hub 枚举该 owner 的 v2 targets 并逐个调用，属于后续能力；首批至少支持按长码锁定，不得误调用 legacy 全局 writer 清理。

## 7. 输入链路

1. Browser `POST /input`，携带 `page_id`、page token 和一次性 request ID。
2. VS 事务读取对应页，验证 token 摘要、session、`state=unlocked`、未过期；失败返回 401/403/409，不请求 Hub。
3. VS 只去除协议规定的结尾 CR，保留多行正文；构造服务签名请求到 Hub v2 input。
4. Hub 校验 HMAC、防重放、target/session 路由一致性和 session 在线，再进入统一队列。
5. Hub 返回接纳结果后，VS 才把 `last_write_at` 和授权期限续 8 小时。网络结果不确定时以 request ID 幂等查询，禁止盲目双投。

不同已解锁页可以同时提交；它们在 helper 统一 FIFO 中按 `queue_seq` 排序。VS 不用 `writerPage` 在页之间做抢占。

## 8. Hub 变更范围与风险

Hub 属于敏感发布项，相关变更应独立验证：

- 新增长码 parser，保留大小写。
- 新增显式 owner 校验与持久 endpoint owner resolver。
- 新增按 route target 调用 VS unlock/revoke 的 HMAC client。
- 新增 VS v2 input HMAC 验证、防重放和 `routeViewServiceInput`。
- capability gate 和审计。

不得顺手修改 helper 设备 lease、群消息门、统一队列或 legacy writer 语义。所有 Repo/owner 查询在 `h.mu` 外完成；网络 POST VS 也必须在 `h.mu` 外。锁内只取不可变快照或更新有界内存 replay cache，遵守现有“锁内抓引用、锁外 I/O”纪律。

## 10. 回归与 GO 门槛

### 10.1 Owner 安全

- owner 的真实飞书 open_id 解锁自己的页成功。
- 非 owner、未绑定 open_id、inactive member、群 open_id 冒充、display name 相同者均失败，且 VS 页状态不变。
- Repo 超时、owner 不唯一、endpoint owner 与会话前缀冲突均 fail closed。
- 页面码泄露给另一账号不能解锁。

### 10.2 页级隔离与多页

- 同一 owner 打开 A/B 两页，分别得到不同 code/token；依次解锁后 A/B 同时可输入，不发生“输入权转移”。
- 另一个浏览器打开同一会话得到 C 页；即使 A/B 已解锁，C 仍不能输入。
- 复制 tab 导致 sessionStorage 克隆时，新 tab 经 BroadcastChannel 换新身份，不能继承原页写权。
- A revoke/过期不影响 B。

### 10.3 Churn 与持久化

- 页已展示 code 后 helper 断线、Hub 执行当前 `closeTerminalLocked`、同设备重连；原 code 仍能一次 `#unlock` 成功，无 first-fail。
- 页已解锁后 helper churn，VS 的 `can_write` 保持；helper 在线恢复后可继续输入。
- VS 重启从 SQLite 恢复 code/grant；浏览器凭原 page token 恢复。
- Hub 重启后双向服务认证恢复，页码不变；输入在 Hub 离线时明确失败且不重复执行。

### 10.4 生命周期

- 刷新和短 SSE 抖动不换码；断连宽限后撤销。
- status/SSE 心跳不延长 8 小时写授权，成功输入才延长。
- locked 孤儿、expired 记录和 nonce/request-id 表按配置有界回收。

### 10.5 协议与故障

- HMAC 错误、过期 timestamp、nonce 重放、target/session 不一致均拒绝。
- Hub→VS unlock 超时不回“已解锁”；同 command ID 重试幂等。
- VS→Hub input 响应丢失时同 request ID 不双投。
- v1 短码、老 xterm 和未启用 v2 的会话全量回归不变。

## 11. 待决策

以下参数可配置，但在实现前需确定取值：

- 断连宽限（建议 5 分钟）、locked 孤儿 TTL（建议 30 分钟）、审计保留期（建议 24 小时）。
- HMAC secret 的环境配置与轮换机制。
- owner 全页 `#lock` 是否进入首批。
- Hub 离线但持久 endpoint 存在时，是否允许先解锁 VS 页（本稿建议允许；输入仍等待 session 在线）。
