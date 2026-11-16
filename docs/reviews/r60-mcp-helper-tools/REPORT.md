# R60-③ 两个 MCP 工具在新平台返回 helper_rejected —— 定因、实现与证据

- 派单：组长 opus50tc2k.zzc，2026-11-16T16:05:24Z，delivery `dw-dlv_39ae6d690231e41b165d1feda136311d`；补正 16:06:35Z，delivery `dw-dlv_c6d3f8b67fd09329ca98b23714ad5d18`（业主 rulings ㊿-补124「两个 MCP --- 修复」⇒ 两工具必须修复可用，不接受「下线＋错误码」）。
- 执行：qwen38mvb4.zzc（唯一执行者）。复核：togpt60lunazzc。终审：组长。
- 仓库 `git@git.code.tencent.com:wego-ai/dingwei.git`，分支 `qwen38mvb4/r60-mcp-tools`，基线 main `d2cc47c7e699eb45efb3a07ee197aa8c20a9f07e`。
- 规格：`openspec/changes/r60-mcp-helper-tools/`（9 条需求增量 SHB-001～006、MLO-001～003，24 个 Scenario＝首轮 21＋改单 SHB-S022～S024）。
- 边界：不部署、不并 main、不 apply／archive、不动 r59 冻结锚与镜像。未连现网、未向在线席位发探针、未读取他席数据；helper 环境事实只取本机自身进程。
- 下文基线行号相对 `d2cc47c7e`，可用 `git show d2cc47c7e:<文件>` 独立核对；新增代码按「文件＋符号」给出。

## 1. 只读定因

### 1.1 工具一 `heartbeat_report`

链路：`tools/sessionhelper/sessionhelper/mcp_shim.py:397-406`（`_heartbeat_report` → `_ipc_vsapi("POST", "/v1/sessions/me/heartbeat")`）→ `ipc_listener.py:316-338`（`_handle_vsapi` 把处理器异常写成 `{'ok': False, 'error': 'vsapi call failed: …'}`）→ `mcp_shim.py:110-125`（`_decode_ipc_response` 对所有 `ok:false` 一律 `raise _IPCFailure("helper_rejected")`）→ 模型看到 `mcp_shim.py:128-137` 的 `outcome unknown`／`retryable false`／「do not automatically resend」。

两个**互相独立**的阻塞点，任一都足以让该工具在发布运行时不可用：

| # | 阻塞点 | 证据 |
| --- | --- | --- |
| 1 | 配置缺失：`datad_url` 未配即抛错 | `app.py:482-495` `_make_vsapi_handler`；`app.py:489-492` 返回 `_unconfigured` 并 `raise RuntimeError("vsapi 未配置:datad_url 未配置")`。本机实测运行中的 helper 进程环境无 `SH_DATAD_URL`／`DATAD_URL`／`DATAD_TOKEN` |
| 2 | 依赖缺失：运行时没有 aiohttp | `app.py:497` `import aiohttp`；`tools/sessionhelper/requirements-runtime.lock` 只有 certifi／pexpect／ptyprocess／websockets（`requirements.txt` 仅 `--require-hashes -r requirements-runtime.lock`）。仓内已有实证：`docs/reviews/coder-pilot-project-context-impl/withdrawals-fourth/legacy-datad-refusal.log:36-37` 记 `import aiohttp` → `ModuleNotFoundError: No module named 'aiohttp'` |

补充：datad 本身是独立二进制 `cmd/dingwei-datad`，自报域在 `internal/datad/server.go:76`（`POST /v1/sessions/{name}/heartbeat`，`:523-528` 只许自报）、`:70`（`GET /v1/heartbeats/latest`，`:425-429` 仅 role coordinator 可读）；`docker-compose.yml` 只有 workpulse ⇒ **新平台根本不部署 datad**。所以这不是「把 datad_url 配上就好」的问题。

### 1.2 工具二 `dw_inbox` 按 `request_id` 读取

- 先排除误判：`inbox_lifecycle.py:88` 为 `UPDATE delivery_inbox SET dispatch_intent_at=?, request_id=delivery_id, updated_at=?` ⇒ **`request_id` 恒等于 `delivery_id`**，不存在键名不匹配。真实存在的投递用其 `request_id` 本来就能读到（已写成用例 MLO-S003）。
- 真实缺陷：查无该行时 `inbox.py:486-487`（driver）／`desktop.py:105-106`（desktop）返回确定的 `{'ok': False, 'error': 'not_found'}`，且位置在**任何写入之前**；`ipc_listener.py:222-231` 原样写回；`mcp_shim.py:124` 把它与「socket 对端拒绝」一样折叠成 `helper_rejected`。
- 后果：一个已经得到答案、且确定没有副作用的查询，对模型呈现为「传输级未知、可能已生效、不要重发」，与 `_tool_failure` 自身注释的前提（「缺少响应不能证明没有发生副作用」，`mcp_shim.py:129`）正好相反；同时 `helper_rejected` 失去区分度。

## 2. Hub 只读核查：现有代码没有任何会话自报状态接口

按补正要求先只读核查，逐项行证据：

| 核查点 | 基线证据 | 结论 |
| --- | --- | --- |
| helper→Hub 控制信封的就地消费点 | `internal/m8/hub.go:1409`（`handleInboxLagControl`）、`:1412`（`handleAgentDiskMetrics`）、`:1415`（`handleFileCapabilityRequest`）、`:1418`（`handleSpecGrantRequest`）、`:1345`／`:1356`（capability、delivery-ack、subagent、terminal output） | 全部为既有子类型，无 status／task／blocker 自报入口 |
| Hub 的在线判活 | `sessionClient.lastSeenAt`、`touchSessionEndpoint` | 连接级，不能表达 blocked／busy，也不承载自报正文 |
| datad 自报域 | `internal/datad/server.go:70`、`:76`、`:425-429`、`:523-528` | 属未部署的独立二进制 |

⇒ **需 Hub 新增最小接口**，本单一并实现与回归（下节）。所借形状全部取自同仓既有实现：`inbox_progress_v1`（helper `app.py:2113-2121`；Hub `internal/m8/inbox_lag.go:58-113`）、`delivery_status_query_v1` → `delivery_status_result_v1`（Hub `inbox_lag.go:82-92`；helper `outbound_receipt.py:117-141`）、连接级缓存＋限流＋尝试先计费（`hub.go:263-271`、`agent_disk_metrics.go:39-71`）、业主侧签名只读出口与 4096 字节硬界（`agent_disk_metrics.go:99-176`，响应体 `:163`，硬界 `:164`，路由注册 `cmd/workpulse/main.go:396`）、能力头协商（`agent_ws.go:120-124`）、身份绑定（`agent_disk_metrics.go:22-35`，仅凭据准入写入：`agent_ws.go:101` → `hub.go:1207`；本单加了 3 行字段后同一行在最终树为 `hub.go:1210`）、路由层 `ag.ID != req.Agent → 403`（`agent_disk_metrics.go:158-161`）。

## 3. 实现（最小面：不新增依赖、不新增表／迁移／DDL、不新增 HTTP 路由）

### 3.1 Hub 侧

- 新增 `internal/m8/session_heartbeat.go`：`handleSessionHeartbeat`（就地消费 `meta.type=session_heartbeat_v1`；超界正文与非法 nonce／被顶替连接静默丢弃；1 秒限流且**尝试先计费再校验**；`validSessionHeartbeat` 用 `DisallowUnknownFields`＋尾部多余内容检查＋schema 精确匹配＋status 枚举＋128 码点＋控制字符检查；写样本前过 `metricsBinding`；回 `session_heartbeat_result_v1`）、`replySessionHeartbeat`、`sessionHeartbeatForClient`、`currentSessionHeartbeat`、`SessionHeartbeatCapability`。
- `internal/m8/hub.go`：接收路径紧随 `handleAgentDiskMetrics` 之后新增挂点；`sessionClient` 新增 `heartbeat`、`lastHeartbeatAttempt`（均在 `h.mu` 下读写）。
- `internal/m8/agent_ws.go`：selfhost 凭据准入分支新增能力头 `X-DW-Session-Heartbeat: session-heartbeat/v1`。
- `internal/m8/agent_disk_metrics.go`：业主签名路由响应体**追加** `session_heartbeat` 键（`agent_disk`／`inbox_lag` 逐字不变），仍受 `len(body) > 4096 → 503` 硬界约束。

### 3.2 helper 侧

- 新增 `tools/sessionhelper/sessionhelper/session_heartbeat.py`：`heartbeat_payload`（严格校验，未知键即拒）、`heartbeat_wire_payload`（剥 IPC 传输键 `type`／`request_id`）、`SessionHeartbeatReporter`（`capable` 能力头闸门、`receive` nonce＋连接＋地址＋类型＋result 形状五重匹配、`report` 无应答即 `unknown` 且绝不重发、`abort` 只释放属于该连接的等待方）。固定应答字段 `ok`／`status`／`retry_after_ms`／`recorded_at_ms`，Hub 附加字段不透出。
- `app.py`：`__init__` 建 reporter（紧随 `outbound_receipts`）；`_run_once` 的 finally 里 `abort(ws)`；`recv_loop` 在 `outbound_receipts.receive` 之后、业务路由之前消费应答；`_make_heartbeat_handler` 恒返回确定结果；`serve_ipc(..., heartbeat_handler=...)`。
- `ipc_listener.py`：新增 `IPC_HEARTBEAT = "heartbeat_v1"`，未知键拒绝为 `invalid_heartbeat_request`，处理器 5 秒上限（超时→`unknown`，`ValueError`／`TypeError`→`invalid`，未接处理器→`unsupported`），结果按既有 `{'ok': True, 'outcome': …}` 形状封装。分支位置在 `upload_only`／`send_body_only` 闸门**之后**，受限套接字不获得新通道。
- `mcp_shim.py`：`_heartbeat_report` 改走 `_ipc_request`（超时 8 秒，留出与监听器 5 秒的可见差值）；工具描述改为「向 Hub 自报……只保留本连接最新一份供业主侧只读，不写历史」。

### 3.3 收件未命中

- `inbox.py`／`desktop.py`：写入之前的 `not_found` 返回追加显式标记 `outcome='none'`。
- `mcp_shim.py`：新增 `_LOOKUP_MISS_ERRORS = {"not_found"}`、`_LOOKUP_MISS_FIELDS = {"error","outcome","mode"}`、`_lookup_miss()`；`_decode_ipc_response` 在 `request_too_large` 分支之后、`helper_rejected` 之前插入白名单透传，只透出固定键中的标量值。其余 `ok:false` 仍 `helper_rejected`，既有超界透传逐字不变。
- `dw_inbox` 工具描述补充：`message_id` 取 `delivery_id`（即该件 `request_id`），查无此件返回 `error=not_found`，是确定答案而非传输故障。

### 3.4 既有夹具修复（首轮全量门 rc=1 的原因）

新增的连接清理挂点（`_run_once` finally 里的 `session_heartbeat.abort(ws)`）与 `recv_loop` 的消费分支，要求 4 个「绕过 `__init__` 的部分构造夹具」补上新属性，否则 `AttributeError`。按同仓既有 `outbound_receipts` 的做法补齐，**未放宽任何断言、未改产品语义**：

| 文件 | 补什么 |
| --- | --- |
| `tools/sessionhelper/tests/test_connection_receive_quiescence.py` | `session_heartbeat=SimpleNamespace(receive=lambda _ws, _env: False)`（该文件用 AST 只抽取 `recv_loop`，刻意不 import 运行时模块，故用同形 stub） |
| `tools/sessionhelper/tests/test_file_handler.py` | 真实 `SessionHeartbeatReporter(("agent",))`，与同处的真实 `OutboundReceiptBroker` 一致 |
| `tools/sessionhelper/tests/test_spec_review_enrolled_r43.py` | `session_heartbeat = types.SimpleNamespace(abort=lambda ws: None)` |
| `tools/sessionhelper/tests/test_ipc_socket_path_isolation.py` | `session_heartbeat=SimpleNamespace(abort=…)` 与 `_make_heartbeat_handler=lambda ws: None`（签名须收 `ws`，与 `_make_send_handler` 等一致） |

首轮全量门读数（保留，不隐藏）：`HELPER_GATE_RESULT {"actual": 1228, "expected": 1228, "files": 96, "groups": [1219, 9], "profile": "codex", "rc": 1}`，10 个 error 全部为 `AttributeError: … has no attribute 'session_heartbeat'`／`'_make_heartbeat_handler'`，分布在上述 4 个文件；修复后重跑见第 5 节。

### 3.5 清单登记

`scripts/helper-test-files.txt` 按序插入两行（未重排全表）；`deploy/test_build_entrypoints.py` 清单计数 94→96。helper 源码在部署时按目录挂载（`/opt/dingwei/sessionhelper-src`），无按文件枚举的打包清单需要改；`requirements-runtime.lock`／`requirements.txt` 未动。

## 4. 变异读数（34 刀，全部 KILLED）

方法：逐刀只删除／短路**一处**保护，跑定向用例，记录 rc 与转红用例名，随后按原文逐字节恢复（恢复后基线复跑绿，见第 5 节）。判据：不仅 rc≠0，还必须红在**该判据对应的用例**上；编译错误不算 KILLED（本轮 0 刀编译错误、0 刀 BAD_CUT，即 34 刀的锚点在最终树上都恰好命中 1 处）。

下表是**最终产品树上的完整重跑**（2026-11-16T19:30:48Z～19:38:46Z，34 刀一次跑完），不是首轮读数的拼接。重跑前后各取一次 `git ls-files -s -c -o --exclude-standard | sort | sha256sum`，两值相同＝`25bd9d810527a6c865963a1a8bc81f1b31b72c2b3576c189138a6031435d54a6`，证明逐刀恢复后工作树逐字节还原。之所以重跑：首轮（18:03Z～18:11Z）之后又改过 `internal/m8/session_heartbeat_test.go`（见第 4.2 节的数据竞争处置），旧读数不能直接挂在最终树上。

- Go 刀命令：`go test ./internal/m8/ -run TestSessionHeartbeat -count=1 -v`
- helper 刀命令：`python3 -m unittest -v test_session_heartbeat test_inbox_lookup_miss`（锁定 venv `/opt/dingwei/sessionhelper-venv/bin/python3`，Python 3.12.3）
- 环境：`env -i`，普通用户 is3，独立 `HOME=/tmp/r60home`、短 `TMPDIR=/tmp/r60tmp`，`GOPROXY=off`，`SH_DRIVER=codex`；未连现网、未起真实 Hub／helper 实例、未用 PG。

| 刀 | 判据 | 被删除／短路的保护 | 文件 | 结果 | rc | 起始 UTC | 转红的指定用例 |
| --- | --- | --- | --- | --- | --- | --- | --- |
| G1 | SHB-002 | 正文校验（schema＋status 白名单）短路 | `hub:session_heartbeat.go` | **KILLED** | 1 | 2026-11-16T19:30:48Z | `TestSessionHeartbeatInvalidPayloadsAreRejectedNotStored` |
| G2 | SHB-002 | 删 `decoder.DisallowUnknownFields()` | `hub:session_heartbeat.go` | **KILLED** | 1 | 2026-11-16T19:31:07Z | `TestSessionHeartbeatInvalidPayloadsAreRejectedNotStored` |
| G3 | SHB-002 | 尾部垃圾校验 `Decode(&tail) != io.EOF` 短路 | `hub:session_heartbeat.go` | **KILLED** | 1 | 2026-11-16T19:31:25Z | `TestSessionHeartbeatInvalidPayloadsAreRejectedNotStored` |
| G4 | SHB-004 | 1 秒限流短路 | `hub:session_heartbeat.go` | **KILLED** | 1 | 2026-11-16T19:31:43Z | `TestSessionHeartbeatWireRoundTripThrottleAndPeerIsolation` |
| G5 | SHB-003 | 当前连接复核＋nonce 格式校验短路 | `hub:session_heartbeat.go` | **KILLED** | 1 | 2026-11-16T19:32:01Z | `TestSessionHeartbeatSilentDrops` |
| G6 | SHB-003 | 删 `len(env.Body) > 4096` 的静默丢弃（只留 nil 连接判断） | `hub:session_heartbeat.go` | **KILLED** | 1 | 2026-11-16T19:32:18Z | `TestSessionHeartbeatSilentDrops` |
| G7 | SHB-003 | 删 `metricsBinding` 未绑定即拒（改为恒不拒） | `hub:session_heartbeat.go` | **KILLED** | 1 | 2026-11-16T19:32:35Z | `TestSessionHeartbeatUnboundConnectionGetsDefiniteUnavailable` |
| G8 | SHB-006 | 陈旧标记 `stale` 恒 false | `hub:session_heartbeat.go` | **KILLED** | 1 | 2026-11-16T19:32:53Z | `TestSessionHeartbeatReadAuthorizationAndFreshness` |
| G9 | SHB-006 | 读路径删连接级复核 `sessionClients[..]!=c → 返回` | `hub:session_heartbeat.go` | **KILLED** | 1 | 2026-11-16T19:33:12Z | `TestSessionHeartbeatReadAuthorizationAndFreshness` |
| G10 | SHB-006 | 读路径删「绑定失效即 `unknown`」（`metricsBinding` 判断短路） | `hub:session_heartbeat.go` | **KILLED** | 1 | 2026-11-16T19:33:30Z | `TestSessionHeartbeatReadAuthorizationAndFreshness` |
| G11 | SHB-006 | 读侧删调用方 agent_id 与 ResolveFullName 一致性校验 | `hub:session_heartbeat.go` | **KILLED** | 1 | 2026-11-16T19:33:49Z | `TestSessionHeartbeatReadAuthorizationAndFreshness` |
| G12 | SHB-006 | 业主路由响应体删 `session_heartbeat` 追加键 | `hub:agent_disk_metrics.go` | **KILLED** | 1 | 2026-11-16T19:34:08Z | `TestSessionHeartbeatOwnerRouteStaysWithinBound` |
| G13 | SHB-001 | 删 selfhost 能力头 `X-DW-Session-Heartbeat` | `hub:agent_ws.go` | **KILLED** | 1 | 2026-11-16T19:34:27Z | `TestSessionHeartbeatWireRoundTripThrottleAndPeerIsolation` |
| G14 | SHB-003 | 删 hub.go 接收挂点 `handleSessionHeartbeat` | `hub:hub.go` | **KILLED** | 1 | 2026-11-16T19:34:47Z | `TestSessionHeartbeatWireRoundTripThrottleAndPeerIsolation` |
| P1 | SHB-001 | helper 删能力头闸门（无头也发子类型） | `helper:session_heartbeat.py` | **KILLED** | 1 | 2026-11-16T19:35:16Z | `test_capability_gate_never_sends_to_a_hub_without_the_subtype` |
| P2 | SHB-005 | 无应答时把 `{ok:False,status:unknown}` 改报成 `{ok:True,status:recorded}` | `helper:session_heartbeat.py` | **KILLED** | 1 | 2026-11-16T19:35:25Z | `test_no_reply_is_unknown_and_is_not_resent`、`test_send_failure_and_cancellation_never_resend` |
| P3 | SHB-005 | `abort` 内删等待方释放路径 | `helper:session_heartbeat.py` | **KILLED** | 1 | 2026-11-16T19:35:35Z | `test_closed_connection_releases_waiter_as_unknown` |
| P3b | SHB-005 | `abort` 忽略 `connection is ws` 过滤（任意断连都释放） | `helper:session_heartbeat.py` | **KILLED** | 1 | 2026-11-16T19:35:46Z | `test_closed_connection_releases_waiter_as_unknown` |
| P4 | SHB-002 | 删入参键白名单（`set(args) - {status,*TEXT_FIELDS}` 不再拒绝未知键） | `helper:session_heartbeat.py` | **KILLED** | 1 | 2026-11-16T19:35:56Z | `test_rejects_unknown_status_oversize_and_control_text`、`test_shim_rejects_invalid_arguments_before_ipc` |
| P5 | SHB-002 | 删 128 码点文本上限（`len(text) > TEXT_MAX_RUNES` 短路） | `helper:session_heartbeat.py` | **KILLED** | 1 | 2026-11-16T19:36:06Z | `test_rejects_unknown_status_oversize_and_control_text` |
| P6 | SHB-002 | 删控制字符校验（`ord(char) < 0x20 or == 0x7F`） | `helper:session_heartbeat.py` | **KILLED** | 1 | 2026-11-16T19:36:15Z | `test_rejects_unknown_status_oversize_and_control_text` |
| P7 | SHB-004 | 删在途上限（8） | `helper:session_heartbeat.py` | **KILLED** | 1 | 2026-11-16T19:36:26Z | `test_in_flight_limit_is_definite` |
| P8 | SHB-003 | 删应答来源合法性校验 `_valid_hub_address(env)`（任意来源应答都采信） | `helper:session_heartbeat.py` | **KILLED** | 1 | 2026-11-16T19:36:40Z | `test_uncorrelated_replies_never_resolve_and_never_resend` |
| P9 | SHB-003 | 删固定应答字段白名单 `RESULT_FIELDS`（原样回传 Hub 全部键） | `helper:session_heartbeat.py` | **KILLED** | 1 | 2026-11-16T19:36:50Z | `test_correlated_reply_is_recorded_and_reply_contract_is_fixed` |
| P10 | MLO-002 | shim 删白名单透传分支（回到一律 helper_rejected） | `helper:mcp_shim.py` | **KILLED** | 1 | 2026-11-16T19:36:59Z | `test_marked_miss_is_surfaced_with_only_fixed_scalar_keys`、`test_read_unknown_id_returns_not_found_instead_of_helper_rejected` |
| P11 | MLO-002 | shim 删透传固定键白名单 `_LOOKUP_MISS_FIELDS`（只留标量类型过滤） | `helper:mcp_shim.py` | **KILLED** | 1 | 2026-11-16T19:37:09Z | `test_marked_miss_is_surfaced_with_only_fixed_scalar_keys` |
| P12 | MLO-001 | inbox.py 删 `not_found` 上的 `outcome=none` 标记 | `helper:inbox.py` | **KILLED** | 1 | 2026-11-16T19:37:19Z | `test_read_and_ack_of_an_unknown_id_are_definite_and_write_nothing`、`test_read_unknown_id_returns_not_found_instead_of_helper_rejected` |
| P13 | MLO-001 | desktop.py 删 `not_found` 上的 `outcome=none` 标记 | `helper:desktop.py` | **KILLED** | 1 | 2026-11-16T19:37:29Z | `test_desktop_lookup_miss_is_marked_the_same_way` |
| P14 | MLO-003 | inbox.py 删写入之前的 `not_found` 早退（未知 id 落进读／ack 分支） | `helper:inbox.py` | **KILLED** | 1 | 2026-11-16T19:37:39Z | `test_read_and_ack_of_an_unknown_id_are_definite_and_write_nothing`、`test_read_unknown_id_returns_not_found_instead_of_helper_rejected` |
| P15 | SHB-002 | ipc_listener 删未知键拒绝 | `helper:ipc_listener.py` | **KILLED** | 1 | 2026-11-16T19:37:49Z | `test_unknown_ipc_key_is_refused_before_the_handler` |
| P16 | SHB-002 | ipc_listener 删 `ValueError/TypeError → invalid` 归一 | `helper:ipc_listener.py` | **KILLED** | 1 | 2026-11-16T19:37:59Z | `test_raising_handler_is_a_definite_invalid_not_unknown` |
| P17 | SHB-004 | ipc_listener 删处理器 5 秒上限 | `helper:ipc_listener.py` | **KILLED** | 1 | 2026-11-16T19:38:09Z | `test_missing_handler_and_slow_handler_stay_definite` |
| P18 | SHB-004 | ipc_listener 未接处理器时返回非 `unsupported` 形状 | `helper:ipc_listener.py` | **KILLED** | 1 | 2026-11-16T19:38:22Z | `test_missing_handler_and_slow_handler_stay_definite` |
| P19 | SHB-001 | 把 heartbeat 分支插到 `upload_only`／`send_body_only` 闸门之前（受限套接字获得新通道） | `helper:ipc_listener.py` | **KILLED** | 1 | 2026-11-16T19:38:27Z | `test_restricted_sockets_refuse_the_heartbeat` |

合计 34 刀：**KILLED 34**，其它 无。

### 4.1 首轮 3 刀存活的处置（用例强度不足，非产品缺陷）

首轮 34 刀中 G9／G11／P3 存活（rc=0）。三者都不是产品缺陷，而是**用例强度不足**——保护被删后行为确实变坏，但当时的断言测不出来。处置是补断言、并把 P3 的那一刀拆成 P3／P3b 两刀，随后在最终树上重跑全 34 刀全红（上表）。

| 刀 | 存活原因 | 补强 | 补强后 |
| --- | --- | --- | --- |
| G9（读路径连接级复核） | `currentSessionHeartbeat` 已重查当前连接，冗余掩盖了 `sessionHeartbeatForClient` 内的同一复核 | 新增直接调用 `sessionHeartbeatForClient(ctx, 被顶替的 c)`，断言读为 `offline` 而非 `ok` | KILLED |
| G11（调用方 agent_id 与 full_name 解析一致） | 读侧 `metricsBinding` 先行拦截，掩盖了 `ag.ID != agentID` | 新增：连接保持合法绑定时用错误的调用方 agent_id 读取，断言 `unknown` | KILLED |
| P3（`abort` 释放等待方） | 5 秒超时最终也返回 `unknown`，结果相同只是慢 | 改为 `asyncio.wait_for(task, 0.5)` 断言**立即**释放；并断言别的连接断开不释放本连接等待方 | KILLED |
| P3b（新增刀：`abort` 忽略 `connection is ws` 过滤） | —— | 上一条补强同时钉死该刀 | KILLED |

未把任何存活刀改判为通过，未删除任何保护，未把失败改成 SKIP。

### 4.2 一处 `-race` 数据竞争的定性与处置（缺陷在测试，不在产品）

首轮 `go test ./internal/m8/ -run TestSessionHeartbeat -race -count=1 -v` 报 `WARNING: DATA RACE`，rc=1，红在 `TestSessionHeartbeatUnboundConnectionGetsDefiniteUnavailable`（读数保留于第 5 节「首轮」行，不隐藏）。

- 竞争两侧：测试自身在 `h.mu` 下改写 `client.metricsAgentID`，与后台 goroutine `Hub.resumeHelperUpdateOffer`（`internal/m8/helper_update.go:713`）**无锁读**同一字段。
- 定性：`metricsAgentID` 在产品里只在凭据准入时写一次（`internal/m8/agent_ws.go:101` → 基线 `internal/m8/hub.go:1207`，最终树 `:1210`），此后只读，所以**产品路径无竞争**；竞争是本测试为了造「未绑定连接」而事后篡改该字段造成的。`helper_update.go` 的无锁读属既有代码，不在本单范围，未改。
- 处置：改测试的造场景方式，不再碰 `sessionClient` 字段——改用 `e.svc.Store.(*identity.MemStore).SetUserState(ctx, "zzc", identity.UserDisabled, ...)` 让 `metricsBinding` 的「属主须 active」这一既有校验自然失败（`internal/m8/agent_disk_metrics.go:30-31`）。断言强度不变：仍要求回 `ok=false/status=unavailable` 且 `client.heartbeat` 未被写入。
- 复验：最终树 `-race` 全 6 个 `TestSessionHeartbeat*` PASS、`ok 63.088s`、0 处 DATA RACE（第 5 节）；因该用例的造场景机制变了，G7 刀在最终树上重跑仍 KILLED（第 4 节表内 19:32:35Z 行），且全 34 刀一并重跑。
- 连带修订：规格场景 `SHB-S012` 原写「凭据绑定身份缺失」，只覆盖 `metricsBinding` 的一个分支；已改为「绑定校验任一项不通过（`metricsAgentID` 缺失／Agent 非 active 或非 selfhost／`key_id`·会话名不符／属主非 active）」，与该用例实际走的分支和 G7 刀删除的整处校验对齐，避免规格与测试口径漂移。需求正文未改。

### 4.3 改单三刀（组长 2026-11-16T21:59:28Z）与旧刀重跑

改单要求 a／b／c 三格用例，并各配一刀「删／挪保护必须转红」。三刀都在**最终树**上隔离运行（`go test ./internal/m8/ -run '^<用例名>$' -count=1 -v`），转红全部是业务断言，不是编译失败、也不是 panic；每刀跑完立即以运行前读入的原文整体回写还原。

还原证据（三条独立、都是**内容**级）：`internal/m8/session_heartbeat.go` 的 sha256 = `b78b05291e3a1a824a11d502c93a072353fba0df14ed2c31de0f0b781655a489`，与本单产品锚 `b4c42adb75c7` 里该文件的 blob 逐字节相同；`git diff --stat HEAD -- internal/m8/session_heartbeat.go` 为空；`git status --porcelain` 中该产品文件不出现（只有测试与文档被改）。

🔴 口径更正（本席自查，适用于本轮与此前两轮）：本席此前记录的 `git ls-files -s -c -o --exclude-standard | sort | sha256sum`（本轮 `39ab6711…`、首轮 `25bd9d81…`）反映的是**索引**状态，不是工作树内容——变异只在临时改文件后立即回写、从不 `git add`，所以该值同值只能证明「索引未变」，**不能**单独用作「逐字节还原」的证据。首轮 REPORT §4／tasks T-08 用它作还原证明的表述据此更正：还原的实际证据是产品文件相对 HEAD 无 diff，且最终树上重跑的门与变异读数自洽。本轮不再拿该哈希充当还原证明，只作为「索引未被本席改动」的旁证保留。

| 刀 | 判据 | 产品编辑（`internal/m8/session_heartbeat.go`） | 隔离用例 | rc | 起始 UTC | 转红的断言 |
| --- | --- | --- | --- | --- | --- | --- |
| G15 | SHB-S022（改单 a） | `:94` 的 `if !current \|\| !inboxInstance.MatchString(nonce) {` → `if false && !current \|\| !inboxInstance.MatchString(nonce) {`（等价于去掉 `!current`，保留 nonce 判断且仍可编译） | `TestSessionHeartbeatSupersededConnectionIsSilentForEveryBodyClass` | 1 | 2026-11-16T22:39:30Z | `legal_body` → `session_heartbeat_test.go:566 a dropped frame was billed to the superseded connection`；`invalid_body` → `:556 invalid_body must be dropped silently, but the connection answered: … result:map[ok:false status:invalid]`；`oversize_body` 仍绿（在 `:85` 已被丢弃） |
| G16 | SHB-S023（改单 b） | 把计费三行 `h.mu.Lock()`／`c.lastHeartbeatAttempt = now`／`h.mu.Unlock()` 连同其注释移到 `validSessionHeartbeat` 的 invalid 分支之后 | `TestSessionHeartbeatInvalidAttemptIsBilledBeforeValidation` | 1 | 2026-11-16T22:39:48Z | `:605 the invalid attempt was not billed, so the legal frame escaped the throttle map[ok:true … status:recorded]` |
| G17 | SHB-S024（改单 c） | 删去 `:85` 的 `len(env.Body) > sessionHeartbeatMaxBytes`（只留 `c == nil`）⇒ 4096 上界改由 `validSessionHeartbeat` 在计费之后判定，即「把超长判断挪到计费之后」 | `TestSessionHeartbeatOversizeFrameIsNotBilled` | 1 | 2026-11-16T22:40:05Z | `:637 the oversize frame must be dropped silently, but the connection answered: … result:map[ok:false status:invalid]` |

旧刀在同一最终树上隔离重跑（确认新用例没有把旧刀的红吃掉）：

| 刀 | 产品编辑 | 隔离用例 | rc | 起始 UTC | 转红的断言 |
| --- | --- | --- | --- | --- | --- |
| G4n（＝旧 G4） | 1 秒限流短路 | `TestSessionHeartbeatInvalidAttemptIsBilledBeforeValidation` | 1 | 2026-11-16T22:41:24Z | `:605 … escaped the throttle` |
| G5n（＝旧 G5） | `:94` 两个条件一起短路 | `TestSessionHeartbeatSupersededConnectionIsSilentForEveryBodyClass` | 1 | 2026-11-16T22:41:40Z | `:566` ＋ `:556`（与 G15 同） |

加上首轮 34 刀，本单累计 **37 刀全部 KILLED**（34＋G15／G16／G17），无存活、无改判、无把失败改成 SKIP。

**改单 d：34 刀里为何没有刀钉住这两处**——如实记账，两处都是**刀的形状与用例强度**问题，不是产品缺陷：

- **a**：旧 **G5** 是最接近的一刀，但它把 `:94` 的 `!current` 与 nonce 校验**一起**短路，所以它的红只来自 nonce 分支（`TestSessionHeartbeatSilentDrops/bad_nonce`，第 4 节 19:32:01Z 行），`old_connection` 格当时是绿的。旧用例集的 `old_connection` 格只发**合法**正文，且只断言「不写样本／读出口不为 ok」，从不断言「不应答」；而合法正文即使删掉 `!current`，也会被 `:118` 的第二道连接复核挡在缓存写入之前，行为完全相同。两处叠加，`!current` 单独一刀就存活了——与组长 21:58Z 两刀的读数一致。新 a 用例补的正是这两点：三类正文各一格、在真实套接字上断言零应答、并断言不计费（`lastHeartbeatAttempt` 保持零值）。
- **b**：旧 **G4**（删 1 秒限流）与旧 **G6**（删超界静默丢弃）都不动**计费点的位置**；旧的限流用例序列是「合法→合法」，把计费移到校验之后在该序列里不可分辨（第一帧照样计费、第二帧照样 throttled）。所以「尝试先计费再校验」这半句在旧 34 刀里没有对应的刀。新 b 用例改用「不合法→合法」把它变成可分辨的：计费若在校验之后，第二帧就是 `recorded` 而不是 `throttled`。
- **c**：旧 **G6** 与 G17 是**同一处产品编辑**，但旧 G6 的红来自 `TestSessionHeartbeatSilentDrops` 夹具的 **nil-conn panic**（该夹具的 `sessionClient` 没有 `conn`，一旦走到应答路径就 panic），红得不够干净；而且 panic 会中断整个测试二进制——本轮 package 级重跑 G17 时 `SilentDrops/oversize_body` 先 panic，新 c 用例根本没跑到（`passed=15`、rc=1，该轮判定记为 RED_OTHER 而不是 KILLED，读数保留在 `/tmp/r60/mutation-results-gaidan.json`）。因此 c 刀改在真实套接字上隔离取证，得到干净的业务断言红。

两处取证口径的更正（本席自查，均为**测试侧**问题，产品未改）：

1. `assertNoEnvelope`（`internal/m8/hub_test.go:6533`）读到**第一个可忽略帧就返回**，而新准入连接总会先收到一条 `agent_network_skill` 上线指南推送；于是「静默」断言会被这条推送吃掉，把紧随其后的心跳应答藏起来。首轮 a／c 用例因此只在「不计费」断言上转红，没有抓到应答泄漏。改为后台常驻读取、只转发可能是应答的帧（`session_heartbeat_result_v1` 或 `meta.error=true`），再用定时器断言窗口内静默（`replyFrames`／`assertNoHeartbeatReply`）。
2. 用带超时的 context 直接 `conn.Read` 会**关掉连接**（coder/websocket 语义），首轮 c 用例因此在第二帧写入时报 `use of closed network connection`；上一条的后台读取形状同时解决了这一点。

隔离日志与 sha256：`/tmp/r60/mut-G15i.log`、`mut-G16i.log`、`mut-G17i.log`、`mut-G4n.log`、`mut-G5n.log`，元数据 `/tmp/r60/mutation-gaidan-isolated-meta.json`、`/tmp/r60/mutation-oldcuts-meta.json`（本机，不随 Git；刀文本与命令已在上表逐字给出，可据此重放）。

## 5. 门与读数

### 5.0 环境（所有读数同一套）

`env -i` 全剥环境后只给白名单；普通用户 `is3`（非 root）；独立 `HOME=/tmp/r60home`；短 `TMPDIR=/tmp/r60tmp`（11 字节，满足门的 ≤40 硬界）；`GOPROXY=off`、`GOFLAGS=-mod=mod`；helper 一律用锁定运行时 `/opt/dingwei/sessionhelper-venv/bin/python3`（Python 3.12.3）。未连现网、未向任何在线席位发探针、未起真实 Hub／helper 实例、未用 PostgreSQL、未读取他席数据。所有时间戳为 UTC。

### 5.1 Go（受影响包 `internal/m8`）

| 项 | 命令 | 读数 |
| --- | --- | --- |
| 构建 | `go build ./...` | `BUILD_RC=0` |
| 受影响包整包 | `go test ./internal/m8/ -count=1 -timeout 60m` | `ok … 455.188s`，`TEST_M8_RC=0`（19:21:12Z～19:28:47Z） |
| 同上带 `-v`（取逐名计数） | `go test ./internal/m8/ -count=1 -v -timeout 60m` | `ok … 500.625s`，rc=0（19:38:46Z～19:47:06Z）；顶层 **927 PASS／19 SKIP／0 FAIL**，含子测试 **1566 PASS／37 SKIP／0 FAIL** |
| 并发检测（定向） | `go test ./internal/m8/ -run TestSessionHeartbeat -race -count=1 -v` | `ok … 63.088s`，`RACE_RC=0`，6／6 PASS，**0 处 `WARNING: DATA RACE`**（19:29:06Z～19:30:09Z；首轮 rc=1 见 4.2 与 5.5） |
| vet | `go vet ./internal/m8/...` | rc=1，**7 条全部为既有告警**，本单文件 0 条（见 5.4） |

`internal/m8` 是本单唯一受影响的 Go 包（改动只在 `internal/m8`＋`tools/sessionhelper`）。37 个 SKIP **逐名逐原因**见 5.3，全部为既有的环境门控集成用例，本单**未新增任何 SKIP**，也未把任何失败改成 SKIP。

### 5.2 全量门 `scripts/run-build-tests.sh`

绿的一轮（最终读数）：

```
GATE_START=2026-11-16T20:32:35Z          （门单独运行，机上无本席其它并发作业）
python3 -V  →  Python 3.12.3              （/opt/dingwei/sessionhelper-venv/bin/python3）
GATE repo-boundary-gate      ✓  判据一 仓储包外建库句柄 5 处（基线 5）· 判据二 裸句柄逃逸 1 处（基线 1）
GATE dependency-ratchet      ✓  直接依赖 9（基线 9）
GATE tenant-isolation-gate   ✓  领域租户列 7 处，extseat／store 乙组四项负测均已就位
HELPER_INVENTORY_OK files=96
HELPER_GATE_RESULT {"actual": 1230, "actual_SH_DRIVER": "claude", "expected": 1230, "files": 96, "groups": [1221, 9], "profile": "claude", "rc": 0}
HELPER_GATE_RESULT {"actual": 1230, "actual_SH_DRIVER": "codex",  "expected": 1230, "files": 96, "groups": [1221, 9], "profile": "codex",  "rc": 0}
TEST python × 27   全 OK
TEST shell  × 2    全 OK
BUILD_TESTS_OK helper_profiles=2 python=27 shell=2
GATE_END=2026-11-16T20:47:06Z   rc=0
```

原始完整日志 `/tmp/r60/gate-attempt-1.log`（15701 行，sha256 `c1a88787e5e5f248c0e4635c53bec6d3140ab8d275749c4335f50770f61f6e08`）。两 profile 的整套件耗时分别 568.913s／565.776s，`OK` 无 failure 无 error 无 skip。

helper 用例计数：两 profile 各 **1230**（`groups=[1221, 9]`，`files=96`），与门的独立发现计数 `expected` 逐 profile 相等。基线 `d2cc47c7e` 为 1203（1194＋9）；本单新增 `test_session_heartbeat.py`（20 例）＋`test_inbox_lookup_miss.py`（7 例）＝27 例。首轮读数曾为 1228，原因是 4 个既有部分构造夹具在新增连接清理挂点上抛 `AttributeError`、导致这 4 个文件里有 2 例未能计入，修夹具后回到 1230（见 3.4）。

门内三个质量闸（`repo-boundary-gate`／`dependency-ratchet`／`tenant-isolation-gate`）本单前后读数持平：仓储包外建库句柄 5（基线 5）、裸句柄逃逸 1（基线 1）、直接依赖 9（基线 9）、未租户化调用点 90（基线 90）、显式 `SystemCtx` 17（基线 17）⇒ **未新增依赖、未动棘轮**。

### 5.3 `internal/m8` 的 37 个 SKIP（逐名逐原因，全部既有）

| 用例（含子测试路径） | 跳过原因（用例自报原文） |
| --- | --- |
| `TestDeviceClaimAcrossActualClientAndHub` | run scripts/test-desktop-hub-roundtrip.sh for cross-runtime enrollment |
| `TestDeviceClaimAcrossActualClientAndPostgres` | run scripts/test-desktop-hub-roundtrip.sh for cross-runtime PostgreSQL enrollment |
| `TestChangeEvidenceConsoleBrowser` | set CHANGEFLOW_PLAYWRIGHT to installed Playwright for Chrome integration |
| `TestMuxProductHTTP1EightSeats` | set MUX_PUPPETEER to installed puppeteer-core for actual Chrome product integration |
| `TestConsolePlatformPayloadDump` | 仅在浏览器闸取真载荷时运行(设 DW_CONSOLE_PLATFORM_PAYLOAD_OUT) |
| `TestConsoleTaskBoardStartedTaskRestrictionsPendingCaseRecordModel` | K13 deferred: task case record model does not exist, so started state cannot be derived mechanically |
| `TestDeliveryACKActualCLIQueuedAdmissionAndTerminal/claude` | INCONCLUSIVE: set DW_ACK_V2_E2E_PYTHON for actual CLI/Hub acceptance |
| `TestDeliveryACKActualCLIQueuedAdmissionAndTerminal/codex` | INCONCLUSIVE: set DW_ACK_V2_E2E_PYTHON for actual CLI/Hub acceptance |
| `TestDeliveryACKActualCLIQueuedAdmissionAndTerminal/claude_explicit` | INCONCLUSIVE: set DW_ACK_V2_E2E_PYTHON for actual CLI/Hub acceptance |
| `TestDeliveryACKActualCLIReplaysPredecessorAcrossDisconnect/claude/drop_injected` | INCONCLUSIVE: set DW_ACK_V2_E2E_PYTHON for actual CLI/Hub acceptance |
| `TestDeliveryACKActualCLIReplaysPredecessorAcrossDisconnect/claude/drop_injected_confirm` | INCONCLUSIVE: set DW_ACK_V2_E2E_PYTHON for actual CLI/Hub acceptance |
| `TestDeliveryACKActualCLIReplaysPredecessorAcrossDisconnect/codex/drop_injected` | INCONCLUSIVE: set DW_ACK_V2_E2E_PYTHON for actual CLI/Hub acceptance |
| `TestDeliveryACKActualCLIReplaysPredecessorAcrossDisconnect/codex/drop_injected_confirm` | INCONCLUSIVE: set DW_ACK_V2_E2E_PYTHON for actual CLI/Hub acceptance |
| `TestDeliveryACKActualCLIAdmissionCrashWindows/claude/dispatch_sent_receipt_unread` | INCONCLUSIVE: set DW_ACK_V2_E2E_PYTHON for actual CLI/Hub acceptance |
| `TestDeliveryACKActualCLIAdmissionCrashWindows/claude/receipt_before_admitted_persist` | INCONCLUSIVE: set DW_ACK_V2_E2E_PYTHON for actual CLI/Hub acceptance |
| `TestDeliveryACKActualCLIAdmissionCrashWindows/codex/dispatch_sent_receipt_unread` | INCONCLUSIVE: set DW_ACK_V2_E2E_PYTHON for actual CLI/Hub acceptance |
| `TestDeliveryACKActualCLIAdmissionCrashWindows/codex/receipt_before_admitted_persist` | INCONCLUSIVE: set DW_ACK_V2_E2E_PYTHON for actual CLI/Hub acceptance |
| `TestDeliveryACKActualCLICancelAtomicBoundary/claude` | INCONCLUSIVE: set DW_ACK_V2_E2E_PYTHON for actual CLI/Hub acceptance |
| `TestDeliveryACKActualCLICancelAtomicBoundary/codex` | INCONCLUSIVE: set DW_ACK_V2_E2E_PYTHON for actual CLI/Hub acceptance |
| `TestDeliveryACKActualCLILegacyUpgradeKeepsPerDeliveryProvenance/claude` | INCONCLUSIVE: set DW_ACK_V2_E2E_PYTHON for actual CLI/Hub acceptance |
| `TestDeliveryACKActualCLILegacyUpgradeKeepsPerDeliveryProvenance/codex` | INCONCLUSIVE: set DW_ACK_V2_E2E_PYTHON for actual CLI/Hub acceptance |
| `TestDeliveryACKActualCLILateWireTerminal` | INCONCLUSIVE: set DW_ACK_V2_E2E_PYTHON for actual CLI/Hub acceptance |
| `TestDesktopMCPRoundTripAcrossRealHub` | run scripts/test-desktop-hub-roundtrip.sh for the cross-runtime gate |
| `TestFailureNoticeDriverTurnEndToEnd` | set DW_FAILURE_NOTICE_PYTHON to locked helper runtime |
| `TestGroupReplyMCP` | requires locked helper Python; set DW_SPEC_E2E_PYTHON |
| `TestHelperSelfUpdateEndToEnd/install_restart_confirm` | set DW_HELPER_E2E_PYTHON to locked helper runtime Python |
| `TestHelperSelfUpdateEndToEnd/sha_mismatch_keeps_factory` | set DW_HELPER_E2E_PYTHON to locked helper runtime Python |
| `TestHelperSelfUpdateEndToEnd/new_layout_rejected_keeps_factory_and_projects_reason` | set DW_HELPER_E2E_PYTHON to locked helper runtime Python |
| `TestHelper0232MixedVersionEndToEnd` | explicit immutable release pins required |
| `TestHostedBodySendAcrossRealHub` | set explicit isolated integration interpreter |
| `TestEnvelopeReadTimeoutEnvironmentChild` | subprocess probe only |
| `TestPilotQuestionAcrossRealHTTPHelperIPCMCP` | set locked helper Python; canonical runs this local integration |
| `TestPilotTemplateAcrossRealHTTPHelperIPCMCP` | set locked helper Python; canonical runs loopback integration |
| `TestProjectContextAcrossRealHTTPHelperIPCMCP` | set DW_DESKTOP_INTEGRATION_PYTHON to the locked helper test venv; canonical shell gate runs the cross-runtime set |
| `TestSessionHistoryTwentyAgentPublishP99` | set DW_SESSION_HISTORY_PERF=1 for isolated 20-agent latency gate |
| `TestSpecMCPEndToEnd` | set DW_SPEC_E2E_PYTHON for actual helper/MCP subprocesses |
| `TestBlockerAcrossRealHTTPHelperIPCMCP` | set locked helper Python; canonical runs this local integration |

共 37 项。

以上 37 项**全部为基线既有的环境门控用例**（需显式指定锁定 helper 解释器、真实浏览器、真实 CLI／Hub、PostgreSQL 或性能闸开关），与本单无关；本单未新增、未扩大任何 SKIP。

### 5.4 `go vet` 的 7 条既有告警（未修，不属本单）

`go vet ./internal/m8/...` rc=1，7 条全部是 `internal/store.FeishuStickyKey` 结构体字面量未用字段名：`feishu_sticky_modes_test.go:24`、`feishu_sticky_test.go:158`、`:538`、`:552`、`:566`、`:591`、`:668`。两个文件本单**逐字节未改**（`git diff d2cc47c7e -- internal/m8/feishu_sticky_test.go internal/m8/feishu_sticky_modes_test.go` 为空），按「不修无关既有问题」的口径保留并披露。本单新增／修改的 Go 文件 0 条 vet 告警。

### 5.5 历轮 rc≠0 的逐轮定性（不隐藏、不合并）

| 轮 | UTC | rc | 失败面 | 定性 |
| --- | --- | --- | --- | --- |
| 1 | ～17:39Z | 1 | 两 profile 各 10 个 `AttributeError`，全在 `test_connection_receive_quiescence`／`test_file_handler`／`test_ipc_socket_path_isolation`／`test_spec_review_enrolled_r43` | **本单引入**：新增连接清理挂点后 4 个既有部分构造夹具缺属性 ⇒ 已按 `outbound_receipts` 先例补夹具（3.4），未放宽断言、未改产品语义 |
| 2 | 18:23:12Z～18:34:44Z | 1 | claude profile 2 个 FAIL：`test_dispatch_lifecycle.AdmissionDeadlineTest.test_blocked_deliver_is_aborted_before_next_dispatch`、`…test_missing_accepted_uses_admission_deadline_and_recovers`；codex profile rc=0 | **既有负载敏感**，本单未改这两个文件 |
| 3 | 18:39:07Z～18:49:55Z | 1 | claude rc=0；codex 1 个 FAIL：`test_guide_staging.GuideRealAdapterBoundaryTest.test_claude_stream_json_user_frame_boundary_counts_zero_one_two` | **既有负载敏感**，同上 |
| 4 | 19:47:06Z～19:47:14Z | 1 | 8 秒即失败，两 profile 均 `helper case discovery failed`：`helper files without collected cases: ['test_desktop_endpoint','test_guide_staging','test_ipc_socket_path_isolation','test_sessionhelper']` | **本席操作错误**：脚本 `PATH` 漏了 helper venv，门用了 `/usr/bin/python3`（无 `websockets`）⇒ 5 个测试模块 import 失败、发现阶段收集不到用例。已按需复现并留档（5.6），改用 venv 解释器重跑；不是产品问题 |
| 5 | 19:50:30Z～20:02:09Z | 1 | claude rc=0（1230／1230）；codex 1 个 ERROR：`test_update_release_core.GuardTests.test_actual_hung_candidate_deadline_is_independent`（`FileNotFoundError: …/hung-candidate-executed`） | **既有负载／时序敏感**，该文件与基线逐字节相同（5.7） |

第 2／3／5 轮命中的都是**不同的**用例，且都落在本单未改动的文件里；三轮各自只错 1～2 例、其余 1228～1229 例全绿，符合「间歇性时序抖动」而非「确定性回归」的形状。

### 5.6 第 4 轮（本席操作错误）的可复现证据

同一棵树、同一命令，只把解释器换成系统 `python3`（缺 `websockets`）：

```
$ env -i … /usr/bin/python3 -c "<门内的发现计数代码>"
python= /usr/bin/python3 3.12.3
missing= ['test_desktop_endpoint', 'test_guide_staging', 'test_ipc_socket_path_isolation', 'test_sessionhelper', 'test_supervise_connection_loops']
```

换成锁定运行时 `/opt/dingwei/sessionhelper-venv/bin/python3` 后同一命令 `missing=[]`、`HELPER_EXPECTED_CASES=1230`。⇒ 第 4 轮 100% 是解释器选错，与产品代码无关；两份原始输出留档 `/tmp/r60/gate-wrongpython-{inventory,discovery}.log`。

### 5.7 既有时序抖动的对照实验（base 树 vs 本单树）

**实验一（第一轮，18:56:55Z～19:00:01Z）**：把第 2／3 轮命中的两个文件 `test_guide_staging`＋`test_dispatch_lifecycle` 在**基线纯净工作树** `/tmp/wt-base-r60`（`d2cc47c7e`，本单未改；交件后已 `git worktree remove`，复核方可用 `git worktree add /tmp/wt-base-r60 d2cc47c7e` 原样重建）与**本单树**上，用同样的 4 路并发（BASE／MINE × codex／claude）各跑 6 次、共 3 轮 72 次：

- BASE：36 次中 **27 次 rc=1**
- MINE：36 次中 **28 次 rc=1**
- 两边命中的用例名集合相同（`test_claude_stream_json_user_frame_boundary_counts_zero_one_two`、`AdmissionDeadline*` 系列、`test_blocked_deliver_is_aborted_before_next_dispatch`、`test_missing_accepted_uses_admission_deadline_and_recovers`）

⇒ 基线树在同一并发条件下失败率**不低于**本单树，且失败用例同名。这不是本单引入的回归；本单**不修**这些既有用例，也**不把它们改成 SKIP**（按「不修无关既有缺陷」口径），只如实披露。

**实验二（20:08:23Z～20:08:37Z）**：对第 5 轮命中的单个用例 `test_actual_hung_candidate_deadline_is_independent`，在 160 个 CPU 忙循环制造的饱和负载下，BASE／MINE 交替各跑 10 次：**20／20 全 OK**。⇒ 纯 CPU 饱和不足以复现该用例失败；它的敏感点在**长时间运行的整套件进程内**（该用例 `subprocess.Popen` 派生合成子进程并断言子进程在健康探测窗口内写出标记文件；父进程跑完上千个用例后 RSS 增大、fork＋解释器启动变慢，子进程就可能来不及写标记）。因此补做实验三。

**实验三（gate 级条件对照）**：在**与门完全相同的条件**下（同一锁定解释器、同一 `scripts/test-helper-suite.py --driver codex`、同一套 `HOME`／`XDG_*`／`SH_*`／`TMPDIR` 环境、两树并发），BASE 树与本单树各跑整套件 2 轮：

| 轮 | 树 | rc | 用例数 | 失败用例 | 起讫 UTC |
| --- | --- | --- | --- | --- | --- |
| 1 | BASE（`d2cc47c7e` 纯净工作树 `/tmp/wt-base-r60`） | **1** | `Ran 1194 tests` | `test_guide_staging.GuideRealAdapterBoundaryTest.test_claude_stream_json_user_frame_boundary_counts_zero_one_two` | 20:11:14Z～20:22:37Z |
| 1 | MINE（本单树） | **1** | `Ran 1221 tests` | 同上 ＋ `test_dispatch_lifecycle.AdmissionDeadlineRaceTest.test_slow_poll_accepted_after_deadline_is_not_admission` | 20:11:14Z～20:22:45Z |
| 2 | BASE | **1** | `Ran 1194 tests` | `test_guide_staging…test_claude_stream_json_user_frame_boundary_counts_zero_one_two` | 20:22:45Z～20:31:56Z |
| 2 | MINE | **1** | `Ran 1221 tests` | `test_guide_staging…test_claude_stream_json_user_frame_boundary_counts_zero_one_two`（与 BASE 同名同例） | 20:22:45Z～20:32:07Z |

结论：**4／4 次 rc=1，其中 BASE 2／2 失败、MINE 2／2 失败**；`test_claude_stream_json_user_frame_boundary_counts_zero_one_two` 在 4 次里**全部**失败，包括本单一行未改的基线树。第 2 轮两树命中的是同一个用例。⇒ 这些失败与本单无因果关系，是既有测试在本机（96 核、共享负载，采样时 `/proc/loadavg` 为 `7.74 7.05 10.81`、总线程 16218）上的时序抖动。本单**不修**它们、**不改成 SKIP**、**不放宽断言**，只如实披露；这也说明本机上门的两个 profile 并发本身就有相当概率命中既有抖动用例，需要重试才能取到一次全绿（见 5.2 与 5.9）。

同时该表也印证计数：BASE 1194 ＋ 本单新增 27 ＝ MINE 1221（另有 `test_supervise_connection_loops` 的 9 例单独成组，两树相同）。

### 5.8 OpenSpec 严格校验

命令：`OPENSPEC_TELEMETRY=0 npm exec --yes --package=@fission-ai/openspec@1.16.0 -- openspec validate --all --strict --no-interactive`

```
✓ change/r60-mcp-helper-tools
Totals: 36 passed, 0 failed (36 items)
STRICT_RC=0        2026-11-16T20:52:06Z（全部文档编辑定稿之后重跑；20:49:22Z 那次是中间读数）
```

原始输出 `/tmp/r60/openspec-strict-final2.log`（定稿那一轮）与中间轮 `/tmp/r60/openspec-strict-final.log` **两次逐字节相同**，sha256 均为 `5480ae3f63e25204fffe7be44517799fd72e4a9cc567160e857705b044055eac`（校验输出是确定性的，改 tasks.md 与场景措辞不影响 36 项的通过集合）。18:26Z 那次的 36／36 是在改 tasks.md 与 SHB-S012 措辞之前取的，已作废，不拿来充数。

### 5.9 全量门的逐次尝试台账（含本席操作错误，不隐藏）

| # | 起讫 UTC | rc | 结论 |
| --- | --- | --- | --- |
| 1 | ～17:39:56Z | 1 | 本单引入：4 个既有夹具缺新属性，10 个 `AttributeError`（已修，见 3.4） |
| 2 | 18:23:12Z～18:34:44Z | 1 | 既有时序抖动（claude profile 2 例） |
| 3 | 18:39:07Z～18:49:55Z | 1 | 既有时序抖动（codex profile 1 例） |
| 4 | 19:47:06Z～19:47:14Z | 1 | **本席操作错误**：`PATH` 漏 helper venv ⇒ 门用系统 `python3`（无 `websockets`）⇒ 发现阶段失败（见 5.6，已复现留档） |
| 5 | 19:50:30Z～20:02:09Z | 1 | 既有时序抖动（codex profile 1 例，`test_update_release_core`） |
| 6 | **20:32:35Z～20:47:06Z** | **0** | **绿**：两 profile 各 1230／1230，`BUILD_TESTS_OK helper_profiles=2 python=27 shell=2` |

第 6 次即 5.2 引用的绿轮。第 2／3／5 次命中三个互不相同的既有用例，已由 5.7 的三个对照实验证明基线树同样失败；第 4 次纯为本席脚本环境错误。全程**未修改任何既有测试的断言、未新增 SKIP、未放宽任何门或门槛**，`scripts/run-build-tests.sh`、`scripts/run-helper-build-tests.py`、`scripts/test-helper-suite.py` 与三个质量闸脚本本单逐字节未改（`git diff d2cc47c7e -- scripts/run-build-tests.sh scripts/run-helper-build-tests.py scripts/test-helper-suite.py` 为空；`scripts/` 下本单只改了 `helper-test-files.txt` 的清单两行）。

### 5.10 原始日志清单（本机 `/tmp/r60`，未经文件服务传输，可随 Git 重放）

| 文件 | sha256 |
| --- | --- |
| `gate-attempt-1.log`（绿轮全量门，15701 行） | `c1a88787e5e5f248c0e4635c53bec6d3140ab8d275749c4335f50770f61f6e08` |
| `go-m8-final2.log`（`internal/m8` 整包 rc=0） | `2f74289377a89ceabad4a5b0719b49a08d87d2d51ee13dc1e0b412222391d362` |
| `go-m8-verbose.log`（`-v`，927／19／0 与 1566／37／0 的出处） | `517af9365b20ed89d35fb8357c330cb7b20109bf382a51a6a6d2823fba855264` |
| `go-race-final.log`（`-race` 6／6 PASS、0 DATA RACE） | `7fc1c159116249f6233783a6b51c7205c5c0575f88f99750ab0a276c918d678f` |
| `go-final-round1.log`（首轮 `-race` rc=1 的原始读数，保留不删） | 见 4.2 |
| `mutation-results.json`（最终树 34 刀） | `1916d5f6b8b0e52c08dae512edf4c4399a3569c5466322a62ff7fb3343a78817` |
| `mutation-results-round1.json`／`-round1-rerun.json` | 首轮与补强轮读数，保留 |
| `flake-probe.log`（实验一 72 次） | `daed063cacad219eb152ae21697a8457f96989a7e5384c2933830db5c7c8c50f` |
| `suite-probe.log`＋`sp-{base,mine}-{1,2}.out`（实验三 4 次） | `813369f0deb23f0dbf1dbc46a17c9a49b979719d476f66c43a42ed04634115ae` |
| `flake-probe3.log`（实验二 20 次） | 20／20 OK |
| `gate-wrongpython-{inventory,discovery}.log`（第 4 轮复现） | 见 5.6 |
| `openspec-strict-final.log`（中间轮 20:49:22Z）与 `openspec-strict-final2.log`（定稿轮 20:52:06Z） | 两份逐字节相同，sha256 同为 `5480ae3f63e25204fffe7be44517799fd72e4a9cc567160e857705b044055eac` |

改单轮（T-13）新增：

| 文件 | sha256 |
| --- | --- |
| `build-tests-gaidan.log`（改单后绿轮全量门，16121 行） | `c788fdd3e3a879bd17d4f1b4af13e67faacffde44e7c459868dc19792762205e` |
| `go-m8-gaidan-v.log`（`-v`，930／19／0 与 1572／37／0 的出处） | `f3eeafd229e7bd2c44cc2c10f19685183f25f4adf3e86bd2762b296c09876f6a` |
| `go-race-gaidan.log`（定向 `-race` 27 PASS、0 DATA RACE） | `58dc14d60374f1949b7a909f300704f5078f7a067be220932b2ae12d45c0c976` |
| `new3-focused-v3.log`（定向非 race，27 PASS） | `e2cca56ee3a344d13781a359d0f7c5aafff67d90e77b97ba8f088450504dfd0b` |
| `mut-G15i.log`／`mut-G16i.log`／`mut-G17i.log`（改单三刀隔离红） | `0ca6a64b…1cf04`／`b9646e38…0d219`／`2313d4da…81eb3`（全值见 4.3 与 `/tmp/r60/mutation-gaidan-isolated-meta.json`） |
| `mut-G4n.log`／`mut-G5n.log`（旧刀在新用例上重跑） | `33d86bbb…d6fe`／`8e7f4150…5d11` |
| `mutation-results-gaidan.json`（package 级那轮，含 G17 记为 RED_OTHER 的原因） | `f48e10a6848af1017af15eb8914103579a6ff7584e8154da2f923164c2f6510e` |
| `mutation-gaidan-isolated-meta.json`／`mutation-oldcuts-meta.json` | `a057c4d7…91024`／`56c9ec59…de496` |
| `openspec-strict-gaidan.log`（红轮 35／1）与 `openspec-strict-gaidan2.log`（绿轮 36／0） | `f184f940…3400c`／`e95944fe…d90d9` |
| `PROBE-superseded-reply-path_test.go.txt`（临时探针，定位 4.3 末两条取证口径；跑完已从树内移除，未提交） | `d5034c0f6fa61b278cedc6a67ed49fafece12bfdef8eddfd268ca8f98dd3e7e5` |
| `skip-names-gaidan.txt`（改单轮 37 个 SKIP 逐名，与 5.3 清单 `diff` 为空） | `cb4d4b8aea4268884253138bac72d55ee3d1909b226c0db54807feb9da7872cf` |

### 5.11 改单（T-13）后的门重跑

环境与 5.0 完全相同（`env -i`＋白名单、普通用户 `is3`、`HOME=/tmp/r60home`、`TMPDIR=/tmp/r60tmp`、`GOPROXY=off`、`GOFLAGS=-mod=mod`、helper 一律走锁定 venv `/opt/dingwei/sessionhelper-venv/bin/python3`＝Python 3.12.3；`PATH` 前置该 venv，避免 5.6 那次操作错误）。产品文件相对 `b4c42adb75c7` 逐字节未动（`internal/m8/session_heartbeat.go` sha256 `b78b05291e3a1a824a11d502c93a072353fba0df14ed2c31de0f0b781655a489`，证据见 4.3）；本轮只改 `internal/m8/session_heartbeat_test.go` 与 4 份文档（spec／design／tasks／本报告）。未连现网、未向任何在线席位发探针、未用 PostgreSQL、未读取他席数据。

| 项 | 命令 | 读数 | UTC |
| --- | --- | --- | --- |
| 全量门 | `sh scripts/run-build-tests.sh` | `BUILD_TESTS_OK helper_profiles=2 python=27 shell=2`、`BUILD_TESTS_RC=0`；两 profile 各 **1230／1230**（`groups=[1221,9]`、`files=96`、`rc=0`）；三闸 repo-boundary／dependency-ratchet／tenant-isolation 全过；`HELPER_INVENTORY_OK files=96`（本轮未新增 helper 测试文件，清单与计数未改） | 22:52:47Z～23:06:04Z（**一次绿，无重试**） |
| 受影响包整包 | `go test ./internal/m8/ -count=1 -v -timeout 60m` | rc=0，`ok 445.813s`；顶层 **930 PASS／19 SKIP／0 FAIL**，含子测试 **1572 PASS／37 SKIP／0 FAIL** | 22:43:02Z～22:50:30Z |
| SKIP 集合 | 同上日志逐名提取后与 5.3 清单比对 | **37 名逐字节相同**（`diff` 为空）⇒ 未新增 SKIP、未把失败改成 SKIP | 22:50:30Z |
| 并发检测（定向） | `go test ./internal/m8/ -run TestSessionHeartbeat -race -count=1 -v` | rc=0，`ok 73.537s`，**27 PASS**（9 顶层＋18 子格）／0 FAIL／0 SKIP／**0 处 `WARNING: DATA RACE`** | 22:50:30Z～22:52:15Z |
| 定向（非 race） | `go test ./internal/m8/ -run TestSessionHeartbeat -count=1 -v` | rc=0，`ok 6.215s`，27 PASS／0 FAIL | 22:38Z |
| vet | `go vet ./internal/m8/` | rc=1，7 条全部为 `feishu_sticky*_test.go` composites 既有告警（不在本单 diff 内），本单文件 0 条（见 5.4） | 22:24Z |
| OpenSpec 严格校验 | `OPENSPEC_TELEMETRY=0 npm exec --yes --package=@fission-ai/openspec@1.16.0 -- openspec validate --all --strict --no-interactive` | **36 passed／0 failed，`STRICT_RC=0`** | 22:50:52Z（规格定稿轮）与 **23:09:20Z～23:09:22Z（全部文档含 tasks 交接记录定稿后重跑，同样 36／0、rc=0**，日志 `openspec-strict-gaidan3.log` sha256 `087abb1bd0ce5a56f5462fbff53d466a6d74070a47eeea112beee39f84744385`） |

计数变化对齐：顶层 PASS 927→**930**（新增 3 个测试），含子测试 1566→**1572**（3 个新测试＋a 用例的 3 个子格），SKIP 19／37 两个口径均不变。

**如实记录本轮一次 rc≠0**：22:4xZ 那次 strict 是 **35 passed／1 failed、rc=1**，原因是本席把 SHB-003 需求正文写到 **592 字符**，触发 strict 的 `Requirement text is very long (>500 characters)`。处置：把「三类成本恒定／不查身份」与「只靠第二道复核会漏出 `invalid` 应答」的细节移到 `design.md` 决定 4 与新场景 SHB-S022 的 THEN，需求正文压到 **490 字符**，规范语义不减；重跑 36／36、rc=0。没有通过放宽校验、删场景或改判据的方式转绿。红轮与绿轮日志都留档（见 5.10）。

**未跑＝不覆盖（本轮）**：浏览器全量回归（本单无前端改动，与首轮同口径）、`identity`／`store`／`cmd` 等未受影响包、整包 `-race`、canonical 之外的发布级门。首轮的未覆盖项（7 节）继续有效，本轮没有把它们变成已覆盖。

## 6. 部署依赖与兼容

- helper 与 Hub 需**同批发布**。只升 helper：能力头缺失 ⇒ 工具返回确定的 `unsupported`（不再是 `helper_rejected`，也不会把未知子类型发给旧 Hub）。只升 Hub：旧 helper 不发该子类型 ⇒ 行为无变化。
- 无需配置项、迁移或数据回填；`requirements-runtime.lock` 不变。
- 心跳为连接级内存态：Hub 重启、连接被顶替或凭据绑定失效后不保留，读出口呈现 `unsupported`／`offline`／`unknown`，绝不把缺失渲染成新鲜的零值。

## 7. 已知边界与未覆盖（不冒充已覆盖）

- `_ipc_vsapi` 在本次之后**没有仓内调用方**；`vsapi_v1` IPC 类型与其受限模式断言（`tools/sessionhelper/tests/test_file_upload.py:567`、`tests/test_hosted_mcp_send.py:81`）保留未动。清理属另一单（proposal MLO-PM01 建议 (a)）。
- 无前端展示：业主侧只能经既有签名路由读到 `session_heartbeat` 字段；Console／View 是否展示为 SHB-PM01，本单未做，也因此**没有浏览器回归读数**。
- 未覆盖：多 Hub 实例间的自报一致性（与 `inbox_lag` 同一单实例假设）、心跳历史序列与告警阈值、真实现网 Hub 上的端到端自报（未连现网、未发探针）。
- 不承诺：Hub 重启后的自报连续性、可用性百分比、以及 `heartbeat_report` 之外的其它 datad 遗留工具（`project_context` 等）的通路修复。
- 测试形状观察（未改，如实记）：既有 `TestSessionHeartbeatSilentDrops` 的夹具 `sessionClient` 没有 `conn`，因此「本不该应答却应答了」在该夹具里表现为 **nil-conn panic** 而不是干净的断言红，且 panic 会中断整个测试二进制、掩盖同轮其它用例。改单 a／c 两格改用真实套接字（`replyFrames`＋定时器）取证，得到干净的业务断言红；旧夹具保持原样未动——改它会牵动 5 个既有格，超出改单「只补 a／b／c」的范围，如需统一另立单。

## 8. 坐标索引

| 内容 | 位置 |
| --- | --- |
| 规格增量与最小基线 | `openspec/changes/r60-mcp-helper-tools/{proposal.md,design.md,tasks.md}`、`specs/session-heartbeat/{spec.md,context.md}`、`specs/mcp-lookup-outcome/{spec.md,context.md}` |
| Hub 新增 | `internal/m8/session_heartbeat.go`、`internal/m8/session_heartbeat_test.go` |
| Hub 挂点 | `internal/m8/hub.go`（接收路径、`sessionClient` 两字段）、`internal/m8/agent_ws.go`（能力头）、`internal/m8/agent_disk_metrics.go`（响应体追加键） |
| helper 新增 | `tools/sessionhelper/sessionhelper/session_heartbeat.py`、`tools/sessionhelper/tests/test_session_heartbeat.py`、`tools/sessionhelper/tests/test_inbox_lookup_miss.py` |
| helper 改动 | `app.py`、`ipc_listener.py`、`mcp_shim.py`、`inbox.py`、`desktop.py` |
| 夹具修复 | `tests/test_connection_receive_quiescence.py`、`tests/test_file_handler.py`、`tests/test_spec_review_enrolled_r43.py`、`tests/test_ipc_socket_path_isolation.py` |
| 清单登记 | `scripts/helper-test-files.txt`、`deploy/test_build_entrypoints.py` |
