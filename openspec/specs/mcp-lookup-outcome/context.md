# mcp-lookup-outcome 上下文

发布提交 501e5b8a185b20f5c931c8ebb89c16bfd633a01e（r60，VERSION 0.2.3.40），2026-11-18 现网上线后按方案甲归档建立本主规格。结项凭据见 docs/reviews/r60-closeout/RECEIPTS.md。

## 来自 2026-11-17-r60-mcp-helper-tools

# MCP 确定否定结果现状勘察（R60-③）

## 固定来源与读取边界

下列基线行号全部相对 main `d2cc47c7e699eb45efb3a07ee197aa8c20a9f07e`，可用 `git show d2cc47c7e:<文件>` 独立核对。本能力此前没有主规格，本件按授权只放变更目录。勘察只读本仓源码与本机自身收件库，未连现网、未发探针、未读取他席收件正文或历史样本。

## 1. `request_id` 与 `delivery_id` 的关系（先排除误判）

- `tools/sessionhelper/sessionhelper/inbox_lifecycle.py:88`：`UPDATE delivery_inbox SET dispatch_intent_at=?, request_id=delivery_id, updated_at=?` ⇒ **`request_id` 恒等于 `delivery_id`**。
- 因此「按 `request_id` 读不到」不是键名不匹配问题：真实存在的投递用它的 `request_id` 本来就能读到（本席对本机自身收件库实测 `dw_inbox read` 正常，并已写成用例 MLO-S003）。
- 失败只发生在**该行不存在**时，例如 id 抄错、属于别席、或已超出本席收件库保留范围。

## 2. 失败点：确定答案被折叠成传输级未知

| 环节 | 基线行证据 | 行为 |
| --- | --- | --- |
| driver 模式未命中 | `tools/sessionhelper/sessionhelper/inbox.py:486-487` | `if row is None: return {'ok':False,'error':'not_found'}`，位置在任何写入之前 |
| desktop 模式未命中 | `tools/sessionhelper/sessionhelper/desktop.py:105-106` | 同形返回 `{"ok": False, "error": "not_found"}`，同样在写入之前 |
| IPC 回写 | `ipc_listener.py:222-231`（`desktop_inbox_v1` 分支，`:230` 写回） | 处理器结果原样写回，不做语义分类 |
| shim 解码 | `mcp_shim.py:110-125`（`:124` 抛错） | 除 `request_too_large` 一处特例（`:120-123`）外，所有 `ok:false` 一律 `raise _IPCFailure("helper_rejected")` |
| 模型可见结果 | `mcp_shim.py:128-137` `_tool_failure` | `{"error": category, "outcome": "unknown", "retryable": False, "message": "Tool failed; do not automatically resend."}` |

⇒ 后果：一个**已经得到答案、且确定没有副作用**的查询，对模型呈现为「传输级未知、可能已生效、不要重发」。这与 `_tool_failure` 自己的注释前提（「缺少响应不能证明没有发生副作用」）正好相反——此处 helper 明确回了响应。同一折叠路径也掩盖真实故障：`helper_rejected` 既表示「socket 对端拒绝」，又表示「查无此件」，两者无法区分。

## 3. 既有可复用先例（透传确定否定的唯一现存形状）

- `mcp_shim.py:120-123`（基线）：`request_too_large` + `outcome=rejected` 已被显式透传为 `{"ok": False, "result": {...}}`，且剔除 `ok`／`error`／`request_id`。本变更沿用「显式标记＋固定键」的思路，但收紧为白名单错误码＋固定标量键，避免把任意 helper 文本变成回显通道。
- `mcp_shim.py:175`（基线，`_ipc_request` 末尾）：`outcome = response.get("outcome", response)`，即 `{'ok': True, 'outcome': …}` 是既有的「确定结果封装」形状；心跳子类型复用同一封装。

## 4. 本变更新增／改动位置（交付树）

- `tools/sessionhelper/sessionhelper/inbox.py`：driver 未命中返回追加 `'outcome':'none'`（仍在任何写入之前）。
- `tools/sessionhelper/sessionhelper/desktop.py`：desktop 未命中返回追加 `"outcome": "none"`。
- `tools/sessionhelper/sessionhelper/mcp_shim.py`：新增 `_LOOKUP_MISS_ERRORS`（当前只有 `not_found`）、`_LOOKUP_MISS_FIELDS`（`error`／`outcome`／`mode`）与 `_lookup_miss()`；`_decode_ipc_response` 在 `request_too_large` 分支之后、`helper_rejected` 之前插入白名单透传，且只透出固定键中的标量值。
- `dw_inbox` 工具描述补充：`message_id` 取 `delivery_id`（即该件 `request_id`），查无此件返回 `error=not_found`，是确定答案而非传输故障。
- 测试：`tools/sessionhelper/tests/test_inbox_lookup_miss.py`（driver／desktop 未命中、`request_id` 等价、shim 固定键、未标记仍拒绝、既有超界透传不变、真实 IPC 端到端）。

## 5. 边界

- 只改「未命中」这一类返回的呈现，不改收件生命周期状态机、ACK 代次、派发意图、投递证据或任何 Hub 协议。
- 不放宽其它 `ok:false`：白名单外的错误码、缺显式标记、或标记值不是 `none`，仍抛 `helper_rejected`。
- 不回显 helper 的任意文本、路径、SQL 或行数据；非标量值一律丢弃。
- `outcome=none` 只允许出现在「无任何写入」的返回上；有副作用或结果未知的路径 MUST 继续走 `helper_rejected`／`unknown`。
