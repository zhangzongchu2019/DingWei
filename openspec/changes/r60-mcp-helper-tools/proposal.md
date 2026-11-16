# Proposal

## Why

新平台上两个 MCP 工具对模型只返回 `helper_rejected`：`heartbeat_report` 仍走 T18 的 helper→datad vsapi 管道（`mcp_shim.py:397-406` → `app.py:482-514` 的 aiohttp 调用），而 helper 运行环境既没有 `SH_DATAD_URL`/`DATAD_TOKEN`，锁文件 `tools/sessionhelper/requirements-runtime.lock` 里也没有 aiohttp，且新平台根本不部署 datad（`docker-compose.yml` 只有 workpulse）；`dw_inbox` 按 `request_id` 读取一个不在本席收件箱的 id 时，helper 明确返回 `not_found`，却被 `mcp_shim.py:110-125` 一律折叠成 `helper_rejected`／outcome unknown／「不要自动重发」。前者是工具彻底不可用，后者是把一个已经得到的确定答案伪装成传输级未知，正是板上记过的缺陷类。业主已明确要求两个工具都修复可用，因此不接受「从工具清单下线＋给错误码」这条退路。

## What Changes

- 新增 `session-heartbeat` 能力（SHB-001～006）：自报心跳改走既有已认证会话 WS 的保留控制子类型 `session_heartbeat_v1`，Hub 就地消费、校验、限流并存为连接级内存样本，回 `session_heartbeat_result_v1`；业主侧读取复用既有 View Service 签名路由，**追加** `session_heartbeat` 字段。能力头缺失时确定性 `unsupported`，绝不把未知子类型发给旧 Hub。
- 新增 `mcp-lookup-outcome` 能力（MLO-001～003）：查无此件是确定答案。helper 在任何写入之前的 `not_found` 返回上附显式标记 `outcome=none`，shim 只对「固定错误白名单＋显式标记」透传固定标量键，其余 `ok=false` 仍 `helper_rejected`。
- 不新增运行时依赖（aiohttp 不进锁文件）、不新增表或迁移、不新增 HTTP 路由；Hub 侧只增连接级内存字段与既有响应体的一个追加键。
- 每条判据附变异读数（删除该保护必须在指定用例转红）；`scripts/run-build-tests.sh` rc=0 才交件。

## Capabilities

### New Capabilities

- `session-heartbeat`：会话自报状态（status/task/eta/blocker）的传输、校验、限流、关联应答、无应答语义与业主侧只读出口。现状最小基线与代码挂点见本变更 `specs/session-heartbeat/context.md`；主规格此前无此能力，不把拟议行为写成现行承诺。
- `mcp-lookup-outcome`：MCP 工具「确定否定结果」与「传输级未知」的分界，以及 shim 透传的固定白名单与固定键。现状最小基线见本变更 `specs/mcp-lookup-outcome/context.md`。

### Modified Capabilities

无。不改 `helper-delivery-evidence`（投递终态证据）、`mcp-auth-diagnostics`、`mcp-cacheable-result`、`hub-lifecycle` 或任何既有能力的现有需求；`inbox_progress_v1`、`delivery_status_query_v1`、`agent_environment_metrics` 与文件能力的既有协议逐字不变。

## Impact

涉及 `tools/sessionhelper`（`mcp_shim.py`、`ipc_listener.py`、`app.py`、`inbox.py`、`desktop.py`，新增 `session_heartbeat.py`）与 `internal/m8`（新增 `session_heartbeat.go`，改 `hub.go` 的接收挂点、`agent_ws.go` 的能力头、`agent_disk_metrics.go` 的业主侧响应体）。部署上 helper 与 Hub 必须同批发布：只升 helper 时能力头缺失，工具返回确定的 `unsupported`（不再是 `helper_rejected`）；只升 Hub 时旧 helper 不发该子类型，无行为变化。心跳只存内存，Hub 重启或换连接即失效，不承诺历史与持久性，也不替代 `session_endpoint` 的连接级判活。`_ipc_vsapi` 在本次之后没有仓内调用方，但 `vsapi_v1` IPC 类型与其受限模式断言保留，清理另立单。

## 来源

需求来源：组长 opus50tc2k.zzc 于 2026-11-16T16:05:24Z 的平台派单 **R60-③**（delivery `dw-dlv_39ae6d690231e41b165d1feda136311d`），依据业主 2026-11-16T16:03:17Z「R60 --- OK，可发」与短消息卡「可以」（rulings ㊿-补123，commit `2f5560b7`，均在派单正文中给出）；同席 16:06:35Z 补正（delivery `dw-dlv_c6d3f8b67fd09329ca98b23714ad5d18`）转述业主 rulings ㊿-补124「两个 MCP --- 修复」，据此排除下线方案并要求：若旧 datad 通路不存在，先只读核 Hub 是否已有心跳／状态上报接口（文件:行），没有则写明所需最小 Hub 接口并一并实现与回归。只读核查结论：Hub 现有代码没有任何会话自报状态接口（证据见 `specs/session-heartbeat/context.md` 第 1 节）。来源不是从 Git 作者推导。

## 待确认假设

- SHB-A01：业主侧只读出口挂在既有 View Service 签名路由（`POST /internal/view-v2/{session}/resources`）上作为追加字段，不新增路由。理由是它已有属主签名、agent_id 双重校验与 4096 字节硬界；若组长要求独立路由或 Console 展示，属另一单。
- SHB-A02：心跳为连接级内存态、Hub 重启即失效应可接受（与 `inbox_lag`、`agent_disk` 同一契约）。若需要跨重启保留或历史序列，须另立变更并新增持久化与保留策略。
- SHB-A03：限流 1 秒、陈旧阈值 30 分钟、在途上限 8、文本 128 码点、正文 4096 字节、监听器 5 秒／shim 8 秒超时均取自同仓既有先例（`agent_disk_metrics.go` 的 25 秒采样与 4096 字节界、`outbound_receipt.py` 的 nonce 关联与 2～3 秒超时），不是业主定值；如需改动按 PM 决策处理。
- MLO-A01：白名单当前只含 `not_found` 一个错误码。其它确定性否定（例如未来可能新增的 `paused_conflict`）须逐个显式加入白名单并附用例，不默认放行。

## 待 PM 决策

- SHB-PM01：是否在 Console／View 上展示自报状态。选项 (a) 本期只出 Hub 只读字段，前端不动（建议，最小面）；(b) 同批加前端展示（需另派前端与浏览器回归）。
- SHB-PM02：是否需要历史序列或跨重启保留。选项 (a) 只保留连接级最新一份（建议，与 inbox_lag 一致）；(b) 落库并定保留期（需新增迁移、保留策略与租户边界评审）。
- MLO-PM01：`_ipc_vsapi` 与 `vsapi_v1` 通道是否本单删除。选项 (a) 保留并另立清理单（建议，本单不动无关协议）；(b) 本单一并删除（须同时改 `tests/test_file_upload.py:567`、`tests/test_hosted_mcp_send.py:81` 的受限模式断言，扩大回归面）。
