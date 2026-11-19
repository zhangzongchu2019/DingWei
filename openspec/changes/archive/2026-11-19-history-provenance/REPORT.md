# S-HISTORY-PROVENANCE 调研报告

基线：`d2cc47c7e699eb45efb3a07ee197aa8c20a9f07e`；比较 renderer：`329eb9f9`。本分支只修改 `openspec/changes/history-provenance/` 文档，没有改产品代码。

## 1. 保存与回放路径

实时可信 canonical source 由 `internal/m8/terminal.go:823-883` 的 `publishInputObservation` / `inputObservationEvent` 构造；它只发布到实时事件环。helper `session_event` 到达 `terminal.go:570-604` 后，Hub 先剥除客户端 provenance，再在 `SessionHistory.Append` 中写入 sanitized user_input，所以历史缺少 canonical 来源。DB 的原始 JSON 写入点为 `internal/store/session_history.go:267-315,430`；分页读点为 `693-738`；HTTP `/view/{session}/history` 授权并编码返回位于 `internal/m8/terminal.go:287-357`。这些文件在 329eb9f9 与 d2cc47c7 内容一致，以上坐标适用于两个版本。

提案推荐持久化按 owner/agent/session/delivery ID 精确定位的紧凑 canonical provenance sidecar，并在相同目标会话 admitted user_input 进入 Append 前匹配成功才附加 Hub attested provenance。sidecar 不含完整正文；Page/HTTP 原样透传既有事件字段。

## 2. 存量无来源显示规则

不回填旧行，不从 source/display_name、正文、delivery_update 或邻近卡片推断。无 provenance、匹配歧义、过期或 trust/attestation 不合法，刷新后均不显示来源图标。

## 3. 驻留体积与旧 renderer 兼容

sidecar 只增加每个 primary 投递的小型 ID、来源与 attestation 元数据和索引开销，按现有 30 天 retention 清理，并计入每 Agent 64 MB、全局 2 GB 配额；不复制 input_posted 正文，避免接近每事件 1 MB 上限的双份存储。renderer 329 与 d2 的 `normalizeEvent` 都按 trusted 加 Hub attestation 识别标准 `user_input.provenance`，所以 329 的实时事件字段兼容。但 329 历史加载器执行 `JSON.parse(row.event)`，而历史 API 将 `json.RawMessage` 返回为 JSON 对象；它不能解析当前 history 响应，这是已存在的限制。d2 的 `parseHistoryEvent` 可接受对象和 JSON 字符串。本提案保持 API envelope 不变，不宣称为 329 增加刷新回放支持；若需支持，需另行解决 renderer/API 格式协商。旧历史缺字段仍按 fail-closed 隐藏。

## 4. 回归判据

实现后需分别以真实 Hub Agent 与 timer 入站做带 SSE id 的 refresh/history replay，DB 行、history API 与 DOM 关联同一 delivery ID；两卡图标数均为 1。移除 Hub 持久 provenance 字段时“图标数=1”必须失败；移除 renderer trust/attestation 判断并输入未证明来源时“未证明来源图标数=0”必须失败，且红在业务断言。另覆盖历史缺字段、copy/control 与不唯一关联保持 0 图标。

## N-3 实现设计补充（2026-11-17）

历史 provenance 的实现设计已补入 `design.md`，内容包括：独立 history migration `0078_history_provenance.sql` 及 R60 新迁移时的顺延规则；以 owner/agent/session/generation/origin_delivery_id 精确关联；在 `appendBatchTx` 同一 history DB 事务中增强 admitted user_input、写事件、消费 sidecar 并更新字节计数；30 天 TTL、每 Agent 64 MB 与全局 2 GB 共享预算和清理；现有 `/history` envelope；存量无证据 fail-closed；renderer trusted Hub replay；真实 Agent/timer 刷新与字段、信任、关联删保护。

当前结论：只写设计，未改 Hub、renderer、数据库或部署，未运行产品回归。OpenSpec 1.16.0 `validate history-provenance --strict` valid，rc=0。
