# reply-origin / loop-termination 实现整版提审包 v1

依据评审纪律(张宗楚/coordinator 08-03 定):整版一次性评审,reviewer 一轮给全量 findings,≤3 轮收敛终止。
本包 = 全量 commits + 三层测试证据 + 判据对照 + 取码法,一次交齐。

## 1. 锚

- **设计冻结锚**(coordinator 已裁): v2.5 @ `5db177e`(canonical);分支 tip `b6d3182`(= 5db177e + L32/L103 澄清仅 mirror/ack 归 B3,语义一致,dev56 对 v2.7 `b3c387f` 机械核 CLEAN)。件 = `reply-origin-loop-termination-design-v1.md` @ 分支 `feat/reply-origin-loop-termination`。
- **实现提审锚**: 分支 `feat/reply-origin-impl` @ **`848f097`**,基线 `origin/main` = `a5f7c25`。
- **取码(防工蜂读副本滞后,勿用 ls-remote 判新旧)**,三法任一:
  1. `git fetch origin 848f097<全40位SHA>`(按 SHA 直取)
  2. `git fetch origin refs/tags/reply-origin-impl-848f097`
  3. bundle: `/home/devo48/work/reply-origin-impl-848f097.bundle`(sha256 前缀 aa8ff0b3)

## 2. 全量 commits(基线→tip)

| 段 | commit | 内容 |
|---|---|---|
| 1 | `96ce589` | 纯逻辑原语 internal/m8/reply_origin.go+单测(B3 classifier/R4 hop/R5 dedup TTL/字段访问器),零碰现码 |
| 2a | `3b8dfcb` | Hub 路由层挂钩:applyReplyOriginTermination 于 RouteEnvelope 顶部(conversation 类过 R4/R5+origin 捕获,control 短路透传,hop 每 transit 递增,drop=静默) |
| 2b | `fc9f5b8` | helper 双闸(§10 第二道):sessionhelper/reply_origin.py+app.py process_inbound_message(非 Feishu 走闸+carry-forward;Feishu 原行为;默认无 reply_policy=不回=向后兼容) |
| 3a | `eec3670` | Hub 集成测试:真 Hub+两 WS 会话,T2 hop 兜底丢弃/T4 dedup 丢弃/origin 捕获/hop 递增 |
| 3b | `ff9e066` | T6-T10 负例+M5 边界独立性(Python 单测 13 绿) |
| — | `3e5c7b2` | **前置合并** `feat/mcp-send@2ec753b`(shence 泳道:mcp_shim/ipc_listener/send_handler+其 a1 评审历史件;发方 wiring 依赖) |
| 4 | `5980b09` | 发方 wiring:shim IPC 顶层四字段(reply_to/in_reply_to/reply_policy/kind)→出站 envelope.meta 同名 key(存在且非 null 才取;§9 分层归 helper/hub 的字段不从 IPC 收);9 单测 |
| 5 | `848f097` | e2e binary smoke(真 Hub+两真 helper+真 IPC)+2 真修(见 §5) |

## 3. 判据对照(冻结不变量 → 代码 → 测试)

| 判据 | 代码落点 | 测试 |
|---|---|---|
| R1 mirror 不回 | helper classify_reply_decision(kind=mirror⇒不回);Go MetaKind | Python 单测 T3;e2e S3 |
| R2 reply_policy 门(仅 expect_reply 产 content-reply,默认不回) | classify_reply_decision | Python 单测 T5;e2e S1 正/默认负 |
| R3 reply 默认 terminal(打断 A↔B) | reply_carryforward_meta(kind=reply+conversation_terminal=True);classify 对 terminal 不回 | Python 2 跳终止单测(T1);e2e S1 A 侧终止 |
| R4 hop≥max 硬兜底 | Hub applyReplyOriginTermination(权威点)+helper 第二道;hop 每 transit 递增+reply carry-forward | Go 单测+Go 集成(T2) |
| R5 dedup(TTL) | Hub replyDedup(REPLY_ORIGIN_DEDUP_TTL 默认 10min,SeenOrMark/Sweep) | Go 单测+Go 集成(T4) |
| B1 ACK 永不创建/消费 reply obligation | control 先行短路;ack 路径零 in_reply_to(grep 判据:ack_handler 无 in_reply_to) | T7(ack 误带 in_reply_to 仍不回);e2e S2 |
| B2 conversation_terminal ≠ M5 三终态 | 仅 conversation 类读写;mcs/board 终态不触 conversation 字段 | T8-T10(两线独立) |
| B3 protocol_class/payload classifier | classifyProtocolClass(payload 识 mcs_dispatch_v1/mcs_receipt_v1/board_control/delivery_ack;显式 conversation 撞 reserved=spoof 归 control) | Go classifier 单测含 spoof(T11/N5) |
| §9 字段分层(shim 仅 4 optional;hop/conversation_id/dedup_key 归 helper/hub) | ipc_listener extract_reply_origin_meta 白名单 | test_mcp_send_wiring(未知 key 不收) |
| §10 双闸(Hub 路由层+helper 生成前) | 段 2a+2b 两处独立 honor | Go 集成+Python 单测+e2e |
| T6 多信道回源 | reply_target fallback=from_addr(session);Feishu 源分支原行为 | Python T6 单测;e2e S1(B 回源 A) |

## 4. 测试证据(可复现命令)

- Go: `go test -count=1 ./internal/m8/` → ok 23.6s(含 reply_origin 单测+集成+全量回归)
- Python 单测: `cd tools/sessionhelper && SH_PROVISION_NO_EXIT=1 python3 -m unittest discover -s tests` → **Ran 126 tests, OK**(基线 117+新 9,零回归)
  - ⚠ 必须带 `SH_PROVISION_NO_EXIT=1`:否则 provision.request_restart 的 os._exit(0) 会把 runner 静默带走(exit=0 无输出=假象)。此为存量行为非本件引入。
- e2e: `python3 tools/sessionhelper/tests/reply_origin_binary_smoke.py` → PASS(两连跑)。真 Hub+两真 helper+真 mcp_send IPC:S1 回源+R3 终止/S2 ack control/S3 mirror/静置无环增长。依赖 websockets、pexpect(pip --break-system-packages)。

## 5. 披露项(reviewer 须知)

1. **前置合并了 shence 泳道** `feat/mcp-send@2ec753b`(含其 a1 评审历史 commits)——发方 wiring 的依赖;该泳道代码本身不在本件评审范围,但其中 2 个 bug 被本件 e2e 暴露并修复(下条)。
2. **2 真修(在段 5)**:①app.py `_make_send_handler` 误 `async def`→serve_ipc 拿 coroutine 不可调(IPC 发送必失败);②config.py `mcp_ipc_sock` 未接 env→补 `SH_MCP_IPC_SOCK`/`DINGWEI_MCP_IPC_SOCK`。**shence 已确认两 bug 在其 24ae6a4 同样存在,以本分支修复为 canonical,shim 四字段无改动**。
3. 实现两保证(冻结裁定随附):①control 先行短路=ack 永不进 conversation_terminal 读取路径(防御冗余 by construction);②双闸不单依赖 Hub 版本(降回滚风险,§10)。
4. M5 交界语义未擅动:dev56 硬边界(B1-B3)落点见 §3;实现期未触 M5 域。

## 6. 改动面(vs a5f7c25,共 14 文件 +1865/-5)

Go: `internal/m8/{reply_origin.go(+272),reply_origin_test.go(+158),reply_origin_integration_test.go(+73),hub.go(+8)}`
Python: `tools/sessionhelper/sessionhelper/{reply_origin.py(+158),app.py(+50),ipc_listener.py(+154),config.py(+3),mcp_shim.py(+241,shence 泳道)}`
测试: `tools/sessionhelper/tests/{test_reply_origin.py(+108),test_mcp_send_wiring.py(+108),reply_origin_binary_smoke.py(+252)}`
文档: `docs/mcp-send-config-v1.md(+99,shence 泳道)`、`review/a1-...(+186,shence 泳道历史件)`
