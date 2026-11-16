# Tasks

派单 **R60-③**：组长 opus50tc2k.zzc，2026-11-16T16:05:24Z，delivery `dw-dlv_39ae6d690231e41b165d1feda136311d`；补正 16:06:35Z，delivery `dw-dlv_c6d3f8b67fd09329ca98b23714ad5d18`（转述业主 rulings ㊿-补124「两个 MCP --- 修复」⇒ 两工具必须修复可用，不接受下线）。唯一执行者 qwen38mvb4.zzc。基线 main `d2cc47c7e699eb45efb3a07ee197aa8c20a9f07e`，分支 `qwen38mvb4/r60-mcp-tools`，只许快进推送。复核 togpt60lunazzc，终审组长。期限 2026-11-18T12:00Z 冻结交件。不部署、不并 main、不 apply／archive。

## 1. 只读定因

- [x] T-01 定因 `heartbeat_report`：完整链路 `mcp_shim.py:397-406` → `ipc_listener.py:316-338` → `mcp_shim.py:110-125`；两个独立阻塞点（`app.py:492` datad_url 未配抛错；`app.py:497` `import aiohttp` 而 `requirements-runtime.lock` 无 aiohttp，仓内已有 `ModuleNotFoundError` 实证）。
- [x] T-02 定因 `dw_inbox` 按 `request_id` 读取：`inbox_lifecycle.py:88` 证明 `request_id` 恒等于 `delivery_id`，真实缺陷是 `inbox.py:486-487`／`desktop.py:105-106` 的确定 `not_found` 被 `mcp_shim.py:124` 折叠成 `helper_rejected`（模型看到 `mcp_shim.py:128-137` 的 outcome unknown／不要重发）。
- [x] T-03 按补正要求只读核 Hub 是否已有心跳／状态上报接口：结论为**没有**（逐项行证据见 `specs/session-heartbeat/context.md` 第 1 节），据此设计并实现最小 Hub 接口。

## 2. 实现

- [x] T-04 Hub 侧：新增 `internal/m8/session_heartbeat.go`（就地消费、双侧独立校验、1 秒限流且尝试先计费、连接级内存样本、nonce 关联应答、静默丢弃、业主侧读取与陈旧语义）；`internal/m8/hub.go` 接收挂点与 `sessionClient` 两个新字段；`internal/m8/agent_ws.go` 能力头；`internal/m8/agent_disk_metrics.go` 响应体追加 `session_heartbeat`。不新增表／迁移／路由／依赖。
- [x] T-05 helper 侧：新增 `session_heartbeat.py`（严格参数校验、能力头闸门、nonce 关联、无应答＝unknown、断连释放、绝不重发）；`app.py`／`ipc_listener.py`／`mcp_shim.py` 接线与工具描述更新；受限 IPC 套接字仍拒绝该子类型。
- [x] T-06 收件未命中：`inbox.py`／`desktop.py` 在写入之前的 `not_found` 返回上加 `outcome=none`；`mcp_shim.py` 只对「白名单错误码＋显式标记」透传固定标量键，其余仍 `helper_rejected`，既有 `request_too_large` 透传逐字不变。

## 3. 回归与证据

- [x] T-07 定向用例：Go `internal/m8/session_heartbeat_test.go` 6 个测试（真实认证 socket 往返／限流／对端隔离、9 类非法正文＋满长度 CJK、5 类静默丢弃、绑定校验不通过时写入被拒、读授权与陈旧、业主签名路由 4096 字节硬界）；helper `tests/test_session_heartbeat.py` 20 个、`tests/test_inbox_lookup_miss.py` 7 个。清单登记 `scripts/helper-test-files.txt`（按序插两行）、`deploy/test_build_entrypoints.py` 计数 94→96。
- [x] T-08 变异：34 刀（G1～G14、P1～P19、P3b）**在最终产品树上全量重跑**，34／34 KILLED，逐刀记了删哪处保护／rc／起始 UTC／转红的指定用例，见 `docs/reviews/r60-mcp-helper-tools/REPORT.md` 第 4 节；重跑前后工作树 sha256 同值 `25bd9d810527a6c865963a1a8bc81f1b31b72c2b3576c189138a6031435d54a6` 证明逐字节还原。首轮 G9／G11／P3 存活属用例强度不足（非产品缺陷），补断言并拆出 P3b 后全红；未把存活刀改判为通过，也未删除任何保护、未把失败改成 SKIP。首轮读数另存不删。
- [x] T-09 既有夹具修复：新增的连接清理挂点要求 4 个既有部分构造夹具补 `session_heartbeat`／`_make_heartbeat_handler`（`test_connection_receive_quiescence.py`、`test_file_handler.py`、`test_spec_review_enrolled_r43.py`、`test_ipc_socket_path_isolation.py`），沿用既有 `outbound_receipts` 的做法，未放宽任何断言。
- [x] T-10 全量门 `scripts/run-build-tests.sh` rc=0 与 OpenSpec `validate --all --strict` rc=0，读数写入 REPORT。逐轮 rc≠0 的定性、既有时序抖动的 base-vs-本单对照实验、`internal/m8` 的 37 个 SKIP 逐名逐原因、`go vet` 的 7 条既有告警，全部见 REPORT 第 5 节；未新增 SKIP、未把失败改成 SKIP、未修无关既有用例。
- [x] T-11 交件：快进推送、`git ls-remote` 与本地 tip 一致、只报 Git 坐标。
- [ ] T-12 独立复核（togpt60lunazzc）与组长终审。【乙；不由本席勾选，不以本席门读数代替】

## 待确认假设

见 proposal.md「待确认假设」SHB-A01～A03、MLO-A01；未确认前不扩大实现面。

## 待 PM 决策

见 proposal.md「待 PM 决策」SHB-PM01（是否前端展示）、SHB-PM02（是否落库／历史）、MLO-PM01（是否本单删除 `vsapi_v1`）。三项均按建议选项 (a) 执行，未擅自扩大范围。

## 交接记录

- 2026-11-16T18:2xZ · qwen38mvb4.zzc · 基线 `d2cc47c7e`，分支 `qwen38mvb4/r60-mcp-tools`。
  - 完成：T-01～T-09。只读定因两条链路（含 Hub 无自报接口的逐项行证据）；Hub＋helper 实现；9 条需求增量（SHB-001～006、MLO-001～003）与 21 个 Scenario；两份 context 最小基线；首轮 34 刀变异（G9／G11／P3 存活，判为用例强度不足并补强）；4 个既有夹具按 `outbound_receipts` 先例补齐。
- 2026-11-16T20:5xZ · qwen38mvb4.zzc · 同一分支，产品树相对首轮只多了一处**测试**改动（`internal/m8/session_heartbeat_test.go`），产品文件未再变动。
  - 修一处 `-race` 数据竞争：缺陷在**测试**不在产品——`TestSessionHeartbeatUnboundConnectionGetsDefiniteUnavailable` 原先在 `h.mu` 下改写 `client.metricsAgentID`，与后台 `Hub.resumeHelperUpdateOffer`（`internal/m8/helper_update.go:713` 既有无锁读）竞争；`metricsAgentID` 在产品里只由凭据准入写一次（基线 `hub.go:1207`，最终树 `:1210`）故产品路径无竞争。改为用 `identity.MemStore.SetUserState(..., UserDisabled, ...)` 让 `metricsBinding` 的既有「属主须 active」校验（`internal/m8/agent_disk_metrics.go:30-31`）自然失败，断言强度不变。复验 `-race` 6／6 PASS、0 DATA RACE。
  - 在**最终树**上重跑全 34 刀变异：34／34 KILLED，重跑前后工作树 sha256 同值（逐字节还原）。G7 因该用例造场景机制变更而必须重验，仍 KILLED。
  - Go：`internal/m8` 整包 rc=0（`ok 455.188s`）；带 `-v` 复跑 rc=0（`ok 500.625s`，顶层 927 PASS／19 SKIP／0 FAIL，含子测试 1566 PASS／37 SKIP／0 FAIL），37 个 SKIP 全部为基线既有的环境门控集成用例，已逐名逐原因写入 REPORT §5.3。
  - 全量门共 5 轮 rc≠0，逐轮定性写入 REPORT §5.5：轮 1 是本单引入的夹具缺属性（已修）；轮 4 是本席脚本 `PATH` 漏 helper venv 导致门用了系统 `python3`（无 `websockets`）的**操作错误**（已按需复现留档）；轮 2／3／5 命中三个**互不相同**的既有时序敏感用例，均在本单逐字节未改的文件里，且已用 base 树对照实验证明基线同样失败（REPORT §5.7）。未修这些无关既有用例，也未改成 SKIP。
  - T-10 已达成：全量门 `scripts/run-build-tests.sh` 在 20:32:35Z～20:47:06Z 单独运行取到 **rc=0**（两 profile 各 1230／1230，`BUILD_TESTS_OK helper_profiles=2 python=27 shell=2`）；OpenSpec `validate --all --strict` 在改过 tasks.md 与 SHB-S012 措辞之后重跑，**36 passed／0 failed，rc=0**（20:49:22Z）。
  - 门共尝试 6 次才取到绿轮，逐次台账（含轮 4 的本席 `PATH` 操作错误）写入 REPORT §5.9；三个质量闸与门脚本本单逐字节未改，`scripts/` 下只改了 `helper-test-files.txt` 的清单两行。
  - 下一步：提交并**快进**推送、核 `git ls-remote` 与本地 tip 一致、只报 Git 坐标交 togpt60lunazzc 复核与组长终审。不部署、不并 main、不 apply／archive、不动 r59 冻结锚。T-12 不由本席勾选。
