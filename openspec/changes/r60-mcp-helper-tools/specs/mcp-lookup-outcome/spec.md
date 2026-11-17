# MCP Lookup Outcome

## Purpose

本变更把 MCP shim 对「helper 明确答复查无此件」的处理从一律 `helper_rejected`／outcome unknown 改为确定结果透传，目的是让模型能区分「已经查过、确定没有」与「传输级未知、可能已生效」。涉及 sessionhelper 的 `inbox.py`／`desktop.py` 未命中返回、`mcp_shim.py` 的响应解码，不影响收件生命周期状态机、投递证据、ACK 代次、发送语义或 Hub 侧任何协议。

## ADDED Requirements

### Requirement: MLO-001 查无此件是确定答案

helper 的收件读取与本地已读（driver 与 desktop 两种模式）在按 `message_id` 查无该行时 MUST 返回 `ok=false, error=not_found` 并附显式标记 `outcome=none`。该标记 MUST 只出现在任何写入发生之前的返回上。`request_id` 恒等于 `delivery_id`，MUST NOT 因调用方使用 `request_id` 这一键名而报未找到。

#### Scenario: MLO-S001 driver 未命中
- **WHEN** 以不存在于本席收件箱的 delivery_id 请求 read 或 ack
- **THEN** 两种操作都返回 `ok=false, error=not_found, outcome=none`

#### Scenario: MLO-S002 desktop 未命中
- **WHEN** 以 desktop 模式的不存在 id 请求 read 或 ack
- **THEN** 返回同一形状的确定否定，含 `outcome=none`

#### Scenario: MLO-S003 用 request_id 读得到原件
- **WHEN** 先领取并开始派发一件，再以其 `request_id` 请求 read
- **THEN** `request_id` 等于 `delivery_id`，读取成功并返回原正文

### Requirement: MLO-002 shim 只透传固定白名单与固定键

shim MUST 仅在响应同时满足「`error` 属于固定白名单（当前只有 `not_found`）」与「helper 显式标记 `outcome=none`」时，把它作为确定结果返回，且 MUST 只透出固定标量键 `error`／`outcome`／`mode`。其余 `ok=false` 响应 MUST 仍抛 `helper_rejected`。helper 的任意文本、路径、SQL、行数据或非标量值 MUST NOT 经该通道回显。既有 `request_too_large` 透传行为 MUST 逐字不变。

#### Scenario: MLO-S004 标记未命中只出固定键
- **WHEN** 响应为 `not_found` + `outcome=none` 且夹带 `detail`／`sql`／`rows`／`request_id`
- **THEN** 工具结果只含 `error`／`outcome`／`mode` 三个键中的实际存在者，夹带值全部被丢弃

#### Scenario: MLO-S005 未标记或未列出的负结果仍拒绝
- **WHEN** `error=not_found` 但缺 `outcome=none`、或 `outcome` 为其它值、或 `error` 不在白名单
- **THEN** 抛 `helper_rejected`，不透出任何 helper 文本

#### Scenario: MLO-S006 既有超界透传不变
- **WHEN** 响应为 `request_too_large` + `outcome=rejected`
- **THEN** 结果与变更前逐字一致

### Requirement: MLO-003 未命中路径无副作用

查无此件 MUST 在任何写入之前返回：MUST NOT 新建收件行、MUST NOT 改动已读标记或未读计数、MUST NOT 触发投递、回复或状态迁移。删除该提前返回会让未命中落入 read／ack 分支，MUST 由用例证明其失败。

#### Scenario: MLO-S007 未命中后收件箱不变
- **WHEN** 对不存在的 id 连续执行 read 与 ack
- **THEN** 未读计数与消息列表与调用前一致，仍只有原有那一条

#### Scenario: MLO-S008 端到端确定结果
- **WHEN** 经真实 IPC 套接字对不存在的 id 调用 read
- **THEN** 工具结果是 `not_found` + `outcome=none` 而不是 `helper_rejected`；对存在的 id 仍返回原正文
