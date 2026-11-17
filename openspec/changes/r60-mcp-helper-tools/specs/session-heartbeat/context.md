# 会话自报心跳现状勘察（R60-③）

## 固定来源与读取边界

下列基线行号全部相对 main `d2cc47c7e699eb45efb3a07ee197aa8c20a9f07e`，可用 `git show d2cc47c7e:<文件>` 独立核对；本变更新增代码的位置在第 4 节按「文件＋符号」给出，行号以交付树为准。本能力此前没有主规格，本件按授权只放变更目录。本节不是对现网 Hub／datad 的在线验收：未连现网、未发探针、未读取他席数据；helper 环境事实只取本机自身进程。

## 1. Hub 侧只读核查：现有代码没有任何会话自报状态接口

| 核查点 | 基线行证据 | 结论 |
| --- | --- | --- |
| helper→Hub 控制信封的就地消费点 | `internal/m8/hub.go:1409`（`handleInboxLagControl`）、`:1412`（`handleAgentDiskMetrics`）、`:1415`（`handleFileCapabilityRequest`）、`:1418`（`handleSpecGrantRequest`）、`:1345`／`:1356`（capability、delivery-ack、subagent、terminal output） | 全部是既有子类型；没有任何 status／task／blocker 自报入口 |
| Hub 的在线判活 | `sessionClient.lastSeenAt`、`touchSessionEndpoint`（`internal/m8/hub.go` 会话连接结构） | 连接级判活，不承载自报语义，也不能表达 blocked／busy |
| datad 的原自报域 | `internal/datad/server.go:76` `POST /v1/sessions/{name}/heartbeat`（`:523-528` 只许自报：`name != id.Session` 即拒）、`:70` `GET /v1/heartbeats/latest`（`:425-429` 仅 role coordinator 可读） | 属独立二进制 `cmd/dingwei-datad`；`docker-compose.yml` 只有 workpulse ⇒ 新平台未部署 |

⇒ 结论：必须由 Hub 新增最小接口，本变更即实现该接口，不假设任何既有等价通路。

## 2. 旧通路为什么必然失败（两个独立阻塞点）

- 链路：`tools/sessionhelper/sessionhelper/mcp_shim.py:397-406`（`_heartbeat_report` → `_ipc_vsapi("POST", "/v1/sessions/me/heartbeat")`）→ `ipc_listener.py:316-338`（`_handle_vsapi` 把处理器异常写成 `{'ok': False, 'error': 'vsapi call failed: …'}`）→ `mcp_shim.py:110-125`（`_decode_ipc_response` 对所有 `ok:false` 一律 `raise _IPCFailure("helper_rejected")`）。
- 阻塞点一（配置）：`app.py:482-495` `_make_vsapi_handler`，`datad_url` 为空即返回 `_unconfigured`，在 `app.py:492` 抛 `RuntimeError("vsapi 未配置:datad_url 未配置")`。本机实测运行中的 helper 进程环境无 `SH_DATAD_URL`／`DATAD_URL`／`DATAD_TOKEN`。
- 阻塞点二（依赖）：`app.py:497` `import aiohttp`，而 `tools/sessionhelper/requirements-runtime.lock` 只有 certifi／pexpect／ptyprocess／websockets，**没有 aiohttp**（`requirements.txt` 仅 `--require-hashes -r requirements-runtime.lock`）。仓内已有实证：`docs/reviews/coder-pilot-project-context-impl/withdrawals-fourth/legacy-datad-refusal.log:36-37` 记录 `import aiohttp` → `ModuleNotFoundError: No module named 'aiohttp'`。即使配了 `datad_url`，发布运行时仍然失败。
- 模型可见后果：`mcp_shim.py:127-136` 把 `helper_rejected` 呈现为 outcome unknown／retryable false／「不要自动重发」，即把一个确定的配置／依赖缺陷伪装成传输级未知。

## 3. 复用先例（本变更的形状全部取自同仓既有实现）

| 先例 | 位置（基线） | 借用什么 |
| --- | --- | --- |
| 连接级自报推送 | helper `app.py:2113-2121`（`inbox_progress_v1`）；Hub `internal/m8/inbox_lag.go:58-113` | 自报不落库、绑定连接、控制面不进业务 |
| nonce 关联的请求／应答 | Hub `inbox_lag.go:82-92`（`delivery_status_query_v1` → `delivery_status_result_v1`）；helper `outbound_receipt.py:117-141`（`query_status`） | nonce 关联一次应答、超时＝unknown、绝不自动重发 |
| 连接级缓存＋限流＋尝试计费 | `hub.go:263-266`（`inboxLag`／`lastInboxAttempt`）、`hub.go:268-271`（`diskMetrics`／`lastMetricsAttempt`）、`agent_disk_metrics.go:39-71`（`SampleInterval=25s`，尝试先计费再校验） | 每连接限流、拒绝也计费、不续期 |
| 业主侧只读出口 | `agent_disk_metrics.go:99-176` `HandleInternalAgentDiskResources`；响应体在 `:163`，`:164` 为 `len(body) > 4096 → 503` 硬界；路由注册 `cmd/workpulse/main.go:396`（`POST /internal/view-v2/{session}/resources`，View Service 签名） | 追加字段而非新增路由；沿用签名与体积硬界 |
| 能力头协商 | `agent_ws.go:120-124`（`ag.Origin == identity.OriginSelfhost` 时下发 `X-DW-Agent-Metrics`、`X-DW-Inbox-Progress`）；helper 按头判断：`app.py:2101-2106`、`app.py:2124-2131` | 无头即不发，避免把未知子类型送给旧 Hub |
| 身份绑定 | `agent_disk_metrics.go:22-35` `metricsBinding`（要求 `c.metricsAgentID`；仅凭据准入写入：`agent_ws.go:101` → `hub.go:1207`）〔行号相对基线 `d2cc47c7e`〕 | 读写两侧同一绑定，正文自称一律不看 |
| 路由层调用方复核 | `agent_disk_metrics.go:158-162`（`ResolveFullName` 后 `ag.ID != req.Agent → 403`） | 读出口的调用方 agent_id 复核 |
| 受限 IPC 通道 | `ipc_listener.py:170-178`（`upload_only`／`send_body_only` 先于业务分支拒绝） | 新子类型不得给受限套接字开新通道 |

## 4. 本变更新增／改动位置（交付树）

- Hub 新增 `internal/m8/session_heartbeat.go`：`handleSessionHeartbeat`（就地消费＋校验＋限流＋写样本＋应答）、`replySessionHeartbeat`、`validSessionHeartbeat`、`sessionHeartbeatText`、`sessionHeartbeatForClient`、`currentSessionHeartbeat`、`SessionHeartbeatCapability`；常量 4096 字节／128 码点／1 秒／30 分钟。
- Hub 挂点：`internal/m8/hub.go` 接收路径紧随 `handleAgentDiskMetrics` 之后；`sessionClient` 新增 `heartbeat`、`lastHeartbeatAttempt`（均在 `h.mu` 下读写）。
- 能力头：`internal/m8/agent_ws.go` 的 selfhost 分支新增 `X-DW-Session-Heartbeat`。
- 业主出口：`internal/m8/agent_disk_metrics.go` 响应体追加 `session_heartbeat` 键（`agent_disk`／`inbox_lag` 不变）。
- helper 新增 `tools/sessionhelper/sessionhelper/session_heartbeat.py`：`heartbeat_payload`、`heartbeat_wire_payload`、`SessionHeartbeatReporter`（`capable`／`receive`／`report`／`abort`）。
- helper 接线：`app.py`（`__init__` 建 reporter、`_run_once` 的 finally 里 `abort(ws)`、`recv_loop` 消费应答、`_make_heartbeat_handler`、`serve_ipc(..., heartbeat_handler=...)`）、`ipc_listener.py`（`IPC_HEARTBEAT`、未知键拒绝、5 秒上限、缺处理器 → `unsupported`）、`mcp_shim.py`（`_heartbeat_report` 改走 `_ipc_request`，超时 8 秒；工具描述改为 Hub 自报、只留连接级最新一份）。
- 测试与登记：`internal/m8/session_heartbeat_test.go`、`tools/sessionhelper/tests/test_session_heartbeat.py`、`tools/sessionhelper/tests/test_inbox_lookup_miss.py`；清单 `scripts/helper-test-files.txt` 增两行（按序插入，未重排全表），`deploy/test_build_entrypoints.py` 清单计数 94→96。

## 5. 环境与依赖边界

- 不新增运行时依赖：`requirements-runtime.lock`／`requirements.txt` 未改，aiohttp 不进锁文件。
- 不新增表、迁移或 DDL；不新增 HTTP 路由；不改任何既有 IPC 子类型的报文形状。
- 状态只在 Hub 进程内存并绑定该已认证连接：Hub 重启、连接被顶替或凭据绑定失效后不保留；与 `inbox_lag`、`agent_disk` 同一契约。
- 部署顺序：helper 与 Hub 需同批发布。只升 helper ⇒ 能力头缺失 ⇒ 工具返回确定的 `unsupported`（不再是 `helper_rejected`）；只升 Hub ⇒ 旧 helper 不发该子类型 ⇒ 行为无变化。
- 本变更不承诺历史序列、跨重启保留或可用性百分比；也不替代 `session_endpoint` 的连接级判活。
