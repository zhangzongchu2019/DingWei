# Design

## Context

现状勘察与全部基线行证据见 `specs/session-heartbeat/context.md` 与 `specs/mcp-lookup-outcome/context.md`。两条结论决定了本设计的形状：其一，Hub 现有代码没有任何会话自报状态接口，datad 在新平台不部署且 helper 运行时没有 aiohttp，所以旧通路不是「配置没配对」而是结构性不可用；其二，`dw_inbox` 未命中本来就有确定答案（`not_found`），问题只在 shim 把它折叠成 `helper_rejected`／unknown。业主已裁定两个工具都要修复可用，因此本设计不含「下线工具＋返回错误码」这一选项。

## Goals / Non-Goals

- 目标：两个工具都返回确定结果；自报心跳有一条真实可达、可回归、可被业主侧只读消费的通路；不新增运行时依赖、不新增表或迁移、不新增 HTTP 路由；每条判据有变异读数。
- 非目标：不做心跳历史序列或跨重启保留；不改前端展示；不改任何既有协议子类型的报文形状；不删除 `vsapi_v1` 通道（另立单）；不承诺 Hub 重启后的自报连续性；不把本单当作发布或部署授权。

## Decisions

### 1. 自报走既有已认证会话 WS 的保留控制子类型（SHB-001、SHB-003）

新增 `meta.type = session_heartbeat_v1`（helper→Hub）与 `session_heartbeat_result_v1`（Hub→helper），形状照抄 `delivery_status_query_v1`／`inbox_progress_v1`：Hub 在 `serveSession` 的接收路径就地消费，即使拒绝也不落入会话环、AMR、收件箱、投递 ACK 或成本管线。理由：这条通路已经过认证、已有限流先例、已有能力头协商机制，且不要求 helper 具备任何新的出网能力（旧通路需要 aiohttp 与可达的 datad）。

上报者身份只取已认证连接，正文与信封里自称的名字一律不读（与 datad `server.go:523-528` 的自报域同理）。写样本前还要过 `metricsBinding`，即凭据准入写入的 `metricsAgentID` 必须解析为 active、selfhost、owner 与名字都匹配的 Agent；未绑定回 `unavailable` 且不写样本。

能力头 `X-DW-Session-Heartbeat: session-heartbeat/v1` 只对 selfhost 凭据准入下发。**没有该头就绝不发送**：旧 Hub 会把未知控制子类型当普通会话消息路由，那等于向对端注入一条伪造消息，比工具不可用更糟。缺头时工具返回确定的 `unsupported`。

### 2. 无应答＝unknown，且永不自动重发（SHB-005）

自报是幂等的观测数据，但「Hub 是否已记录」不可知时不能谎称成功，也没有任何理由自动重试（重发只会放大限流与日志）。因此：超时、发送异常、等待被取消、连接断开一律 `unknown`；`abort(ws)` 在连接丢失时释放该连接上的全部等待方为 `unknown`，且只释放属于该连接的等待方。helper 侧待决映射按 nonce 清理，`MAX_IN_FLIGHT = 8` 超限返回确定的 `busy`。

### 3. 双侧独立校验，Hub 不信任 helper（SHB-002）

shim 先按固定 schema 校验参数（非法即失败，不产生 IPC 请求）；IPC 监听器再拒绝带未知键的 `heartbeat_v1`；Hub 侧 `validSessionHeartbeat` 用 `DisallowUnknownFields`、尾部多余内容检查、schema 精确匹配、status 枚举、128 码点上限与控制字符检查独立复核，正文超 4096 字节在解析之前就静默丢弃。任何一侧被绕过都不会污染样本。满长度 CJK 用例（3×128 汉字）证明校验没有把合法上界误杀，同时证明业主出口仍在 4096 字节硬界内。

### 4. 限流在身份查询之前计费（SHB-004）

沿用 `agent_disk_metrics.go:39-71` 的纪律：先取 `lastHeartbeatAttempt` 判 1 秒窗口，被限流者立刻拿到 `throttled` + `retry_after_ms`（>0 且 ≤1000）并且不覆盖已存样本；通过限流后才计费本次尝试，再做解析与身份查询。这样一条连接无法用非法正文把 Hub 拖进无界的身份解析。IPC 监听器对处理器加 5 秒上限（超时 → `unknown`），shim 侧给 8 秒，留出手工可见的差值；处理器抛 `ValueError`／`TypeError` → `invalid`；未接处理器 → `unsupported`。四种负结果都不是 `helper_rejected`。

### 5. 业主侧读取：追加字段而不是新路由（SHB-006）

读取挂在既有 View Service 签名路由 `POST /internal/view-v2/{session}/resources`（`agent_disk_metrics.go:99-176`），响应体追加 `session_heartbeat` 键。理由：该路由已有属主签名、`ResolveFullName` 与 `req.Agent` 的双重校验、以及 `len(body) > 4096 → 503` 的硬界；新增路由会复制这三件事并扩大评审面。读取路径自身再核 `metricsBinding`、连接未被顶替、样本是否存在：未绑定或身份不符 → `unknown`；无样本 → `unsupported`；有样本 → `ok` + `report` + `observed_at_ms` + `age_ms` + `stale` + `connection_seq`。读取不续期样本，超过 30 分钟只标记 `stale=true`，样本仍可见——「旧但真实」优于「渲染成新鲜的零值」。

样本只存 `sessionClient.heartbeat`（进程内存，`h.mu` 下读写），连接被顶替即随旧连接对象一起失效；不落库、不新增表或迁移。

### 6. 确定否定结果的透传白名单（MLO-001、MLO-002、MLO-003）

helper 在**任何写入之前**的未命中返回上附显式标记 `outcome=none`（driver 与 desktop 同形）。shim 只在「`error` ∈ 固定白名单（当前仅 `not_found`）」**且**「`outcome == "none"`」同时成立时透传，且只透出固定标量键 `error`／`outcome`／`mode`。三个条件是刻意的：白名单避免把任意错误码变成确定答案；显式标记避免「helper 恰好用了同一个错误字但已产生副作用」被误判；固定键＋标量类型避免 helper 文本、路径、SQL 或行数据变成回显通道。其余 `ok:false` 仍抛 `helper_rejected`，既有 `request_too_large` 透传逐字不变。

## 接口／状态／错误码 ↔ Requirement 对照（审阅固定检查项）

| 新出现的东西 | 种类 | 对应 Requirement |
| --- | --- | --- |
| `X-DW-Session-Heartbeat: session-heartbeat/v1` | 握手响应头 | SHB-001 |
| `session_heartbeat_v1` / `session_heartbeat_result_v1` | WS 控制子类型 | SHB-003 |
| `heartbeat_v1` | IPC 请求类型 | SHB-001、SHB-002 |
| `invalid_heartbeat_request` | IPC 错误码 | SHB-002 |
| `schema=session-heartbeat/v1`、`status`、`task`、`eta`、`blocker` | 自报正文字段 | SHB-002 |
| `recorded` | 结果状态 | SHB-002、SHB-003 |
| `invalid` | 结果状态 | SHB-002 |
| `throttled` + `retry_after_ms` | 结果状态与字段 | SHB-004 |
| `busy` | 结果状态（在途上限） | SHB-004 |
| `unavailable` | 结果状态（写侧未绑定） | SHB-003 |
| `unsupported` | 结果状态（无能力头／未接处理器） | SHB-001、SHB-006 |
| `unknown` | 结果状态（无应答／读侧未授权） | SHB-005、SHB-006 |
| `ok`／`status`／`retry_after_ms`／`recorded_at_ms` | 应答固定字段 | SHB-003 |
| `sessionClient.heartbeat`、`sessionClient.lastHeartbeatAttempt` | Hub 连接级状态 | SHB-003、SHB-004、SHB-006 |
| 响应体追加键 `session_heartbeat`（`status`／`report`／`observed_at_ms`／`age_ms`／`stale`／`connection_seq`／`source`） | 业主只读出口字段 | SHB-006 |
| `offline` | 读出口状态（连接为空或被顶替） | SHB-006 |
| 常量 4096 字节／128 码点／1 秒／30 分钟／在途 8／监听器 5 秒／shim 8 秒 | 限值 | SHB-002、SHB-004、SHB-006 |
| `error=not_found` + `outcome=none` | helper 确定否定标记 | MLO-001 |
| `_LOOKUP_MISS_ERRORS`、`_LOOKUP_MISS_FIELDS`、`_lookup_miss()` | shim 白名单与固定键 | MLO-002 |
| 未命中提前返回（写入之前） | 无副作用保证 | MLO-003 |

## 变异证据（删掉保护必须在指定用例转红）

34 刀全部 KILLED，逐刀命令、UTC 与转红用例见 `docs/reviews/r60-mcp-helper-tools/REPORT.md` 第 4 节。判据↔刀号：SHB-001＝G13／P1／P19；SHB-002＝G1／G2／G3／P4／P5／P6／P15／P16；SHB-003＝G5／G6／G7／G14／P8／P9；SHB-004＝G4／P7／P17／P18；SHB-005＝P2／P3／P3b；SHB-006＝G8／G9／G10／G11／G12；MLO-001＝P12／P13；MLO-002＝P10／P11；MLO-003＝P14。

首轮 33 刀中 G9、G11、P3 存活，原因都是**用例强度不足而非产品缺陷**：G9 的连接级复核在 `currentSessionHeartbeat` 已重查当前连接时被冗余掩盖，G11 的调用方 agent_id 复核被读侧 `metricsBinding` 掩盖，P3 的 `abort` 被 5 秒超时掩盖（结果相同、只是慢）。已分别补：直接调用 `sessionHeartbeatForClient` 断言被顶替连接读为 `offline`；连接合法绑定时用错误的调用方 agent_id 断言 `unknown`；`abort` 后以 `wait_for(task, 0.5)` 断言立即释放，并断言别的连接断开不释放本连接等待方（新增 P3b 刀）。补强后 4 刀全部 KILLED。

## 部署与兼容

helper 与 Hub 需同批发布。只升 helper：能力头缺失 ⇒ `unsupported`（确定、可解释，不是 `helper_rejected`）。只升 Hub：旧 helper 不发该子类型 ⇒ 无行为变化。两侧都不需要配置项、迁移或数据回填。心跳为连接级内存态，Hub 重启后为空，读出口呈现 `unsupported` 而不是过期值。

## 已知边界与未覆盖

- `_ipc_vsapi` 在本次之后没有仓内调用方，但 `vsapi_v1` IPC 类型与其受限模式断言（`tests/test_file_upload.py:567`、`tests/test_hosted_mcp_send.py:81`）保留未动；清理另立单（MLO-PM01／proposal 已列）。
- 本单没有前端展示，业主侧只能通过既有签名路由读到该字段；Console／View 是否展示为 SHB-PM01。
- 不覆盖：多 Hub 实例间的自报一致性（单实例假设与 `inbox_lag` 相同）、心跳历史与告警阈值、以及真实现网 Hub 上的端到端自报（未连现网、未发探针）。
