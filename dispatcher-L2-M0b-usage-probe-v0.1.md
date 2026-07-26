# L2-M0b: CLI usage 探针 v0.1

基线：`m0-final^{commit}=e49378b76cad20a457687505468da3189ea964c1`。本件只冻结只读原生能力与不可得性，不以字数、轮数、聊天指令或模型名常识补值。

## Claude Code 实证

- 实测版本：Claude Code `2.1.215` (Linux)。
- 原生入口：`--input-format stream-json --output-format stream-json --verbose`。
- `system/init` 提供 `session_id` 和实际 `model`。
- `assistant.message.usage` 提供单次 API message 的 `input_tokens` / `cache_creation_input_tokens` / `cache_read_input_tokens` / `output_tokens`。
- `result.usage` 是整轮聚合 usage，含工具循环时不等于当前 context snapshot，禁止用它触发 75% 轮换。
- 当前原生帧未提供可验的 `max_context`；`context_management` 在本次样本为 `null`。
- 新主机 Claude 未登录，本次只取得 authentication-failed 协议帧。该帧足以冻结字段形状，不足以证明生产模型的 max-context 或 compaction 后语义。

### 能力结论

| 项 | 结论 | 权威源 |
|---|---|---|
| session | 可得 | `system.session_id` |
| model | 可得 | `system.model` |
| 单 message token usage | 可得 | `assistant.message.usage` |
| 当前 context used | 待鉴定 | 只有单 message usage，尚未证明与 post-turn context 等价 |
| max context | `UNKNOWN` | 当前原生帧缺失 |
| compaction reset | `UNKNOWN` | 尚无认证后 compaction 帧实证 |
| 75% 自动触发 | 禁用 | `max_context` 未知，按 M0 C-2 fail-safe |

`max_context` 的能力矩阵须显式分层：Claude 原生帧当前不可自证该值。若后续允许外部静态表，它必须是带权威来源和版本的 exact-model-id 表；禁止模型家族/前缀回退。未知 model、空 model、表缺失或条目缺失均保持 `UNKNOWN`。该静态表是外部配置证据，不冒充 CLI native 字段。是否允许它作为自动门分母，由架构口径裁定。

## 运行时样本规则

1. 只接受原生协议帧，不调 `/usage` / `/context` 等聊天命令。
2. `sample_seq` 由 adapter 对同 session 递增；`turn_id` 仅溯源，不参与序比较。
3. 缺 `session_id` / `model` / `max_context` 或 token 字段非非负整数时，产出 `UNKNOWN`，不产出虚假 `<75%`。token schema 采严格策略：四个冻结字段缺一即 invalid，可保留 raw evidence，但禁止部分归一化。
4. `result.usage` 以 `raw_kind=result_usage` 保留取证，不归一化为 context used。
5. session/model 线程传递、sample_seq 和 trigger gate 属 adapter 职责；probe 只产出原生证据、原因与置信度。
6. 在认证后环境取得“两正常轮+一次 compaction”真帧前，Claude usage 触发门保持关闭，只允许人工升级。

## 待补实证

- 认证后正常 turn 的完整 `assistant` / `result` 帧（敏感字段脱敏）。
- 连续两轮中单 message usage 与真实 context 增长的对齐。
- compaction 前后原生帧及重基信号。
- 可验的 model max-context 原生源；若 CLI 始终不提供，能力表终态冻结为 `UNKNOWN`。
