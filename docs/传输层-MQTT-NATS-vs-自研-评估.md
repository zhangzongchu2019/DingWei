# 传输层评估：MQTT / NATS 替换自研 WebSocket vs 继续自研

- 评估发起：home（传输层选型任务）
- 组织/执笔：manager（zzc-manager-2642）
- 日期：2026-07-10
- 性质：**选型评估，非实现**。结论出来后由 home 与 manager 定方向。
- 代码依据：`internal/m8/hub.go`(4181 行) + `internal/m8/terminal.go` + `internal/router/*` + `internal/bus/*` + `tools/sessionhelper/`，两路精确盘点 + dev55《sessionhelper-卡死分析》。

---

## 0. 结论先行（TL;DR）

| 维度 | 结论 |
|---|---|
| **推荐选型** | **C（自研补丁）先行**解决需求1/2；**B（NATS，进程内嵌 + JetStream）**作为"规模逼近时再上"的战略预留；**A（MQTT）不推荐**。 |
| **一句话理由** | 现网传输层的坑**不在协议本身**，而在架构：一条 WS 混载控制/数据/终端/镜像/keepalive + 每连接单写锁 + **同步扇出广播**。这套坑的**可替换传输面很小**（Hub ≈500–600 行 / 15%，客户端 ≈110 行），而其余 **85% 是与 broker 无关的业务逻辑**（寻址/owner 鉴权/会话命名/PTY 注入/忙时排队/飞书桥/去重）。换 broker 只能解决那 15%，却要把正在稳定运行的 85% 寻址语义重写到 topic 上——风险与收益不成正比。 |
| **关键排序结论** | **成立且必须先定方向**：若选 B/A，则需求1（连接自愈）/需求2（ACK+排队）由 broker 原生提供，**不要再自研**；若选 C，则需求1/2 **就是**这次自研补丁的内容。两条路二选一，**严禁两头做**。 |
| **纠正一处动机假设** | 动机里"Hub 广播卡 5 分钟疑似发飞书无超时阻塞"——**代码实测：飞书发送不在内联路径**，全部走非阻塞的 `Outbound` DB 队列（`Enqueue` 是 channel/单条 SQLite insert）。真正的全体卡顿源是**在线清单的同步扇出循环**（`hub.go:3015-3028`，一个慢客户端的 `conn.Write` 阻塞后续所有会话）+ ping/write/close **共用 `c.mu`**。"5 分钟"实为 L2 LLM 租约（`LeaseDuration`），与传输无关。 |

---

## 1. 现状：自研传输层的实测边界

### 1.1 Hub 端（Go，`github.com/coder/websocket`）
- **三个独立 WS 端点**：① 会话客户端 WS `HandleSessionWS`（主传输，`hub.go:693`）；② 注册服务 WS `HandleWS`（请求/响应式，`hub.go:660`）；③ 终端 view WS `HandleTerminalViewWS`（`terminal.go:143`）。
- **鉴权**：`key_id`(query) + `secret`(Bearer header) → SHA-256 比对（`hub.go:706`），owner 范围隔离。无 mTLS，依赖 nginx TLS。
- **心跳/死连检测**：30s 应用层 `conn.Ping`（超时 10s，`hub.go:3431`）+ `conn_seq` 单调序号（`hub.go:761`）+ DB reaper（`LastSeenAt`>120s 标离线，`hub.go:219`）。
- **写路径（核心缺陷）**：`sessionClient.write` 直接 `c.mu.Lock(); conn.Write`（`hub.go:3412`）。**无 per-connection 写队列/写泵**；心跳、广播、终端、provision 各 goroutine 直接抢同一把 `c.mu`。ping/close 也抢这把锁 → 一次底层 `conn.Write` 卡住，投递/ping/close 全排队（dev55 卡死根因）。
- **信封协议**：泛化 `model.Envelope{ID,To,From,Body,TS,Meta}`，**消息类型放在 `Meta["type"]`**（无顶层 kind）。约 15 种：`terminal_output/input/resize`、`provision(_ack)`、`online_directory`、`mirror_control`、`feishu_sync`、`agent_network_skill(_ack)`、`collect`、control-task、error。
- **投递语义**：**无传输级逐条 ACK**；靠 `env.ID` 去重（`claimEnvelope`/`envelopeSignature`，`hub.go:3547`）实现 **at-most-once + 去重**（bus.go 注释亦明确"字头 WS 转发为 at-most-once 不重投"）。持久/可靠投递是**另一条路**：DB 队列 `bus.DBQueue`（SQLite），与 WS 解耦。
- **广播（第二处缺陷）**：`broadcastOnlineDirectory` 去抖 2.5s 后，在单 goroutine 里**顺序** `item.Client.write()` 逐个会话（`hub.go:3015-3028`）。慢连接（非报错，仅慢）阻塞后续全体——这正是"全体会话同时收不到清单"的机制。`dispatchSecurityOps` 同型（`hub.go:1519`）。
- **飞书桥**：**不内联**。全部 `h.Outbound.Enqueue`（`AsyncDBQueue` 是有界 channel，`DBQueue` 是单条 insert），另有独立 consumer 出账。**无真正内联飞书 HTTP 调用**。

### 1.2 客户端 sessionHelper（Python，`websockets`）
- **连接**：`app.py:212` `websockets.connect(ws_url, Bearer, ping_interval=20, ping_timeout=20)`，URL `…/ws/session/<name>?key_id=…&tool=…`。
- **单 ws 五协程共享（核心缺陷）**：`asyncio.gather(recv_loop, pending_drain_loop, terminal_loop, outbox_loop, mirror_loop)`（`app.py:238`），**无独立 send_loop**，五个协程各自直接 `ws.send`。
- **卡点**：`recv_loop → process_inbound_message → asyncio.to_thread(adapter.handle)`（`app.py:357`）→ `CLIAdapter.handle` 同步等 CLI 回复（默认 `cli_reply_wait` 45s）并持 `write_lock` 注入 PTY（`cli.py:214,273`）。回复再经**同一条** `ws.send`，一旦 send 卡住则该协程停摆。
- **重连**：`run_forever` 指数退避（`app.py:136`）——**但仅当 `_run_once` 返回/抛异常才触发**；若 `gather` 卡在 `async with connect` 内，外层永不重连。协议层 `ping_timeout` 同事件循环，不保证逃逸。
- **AGENT_ROUTE_RE**（`app.py:57`）：`^(#name|@x#name)\s+(.+)$` 扫 CLI stdout 识别 AI 主动消息。**脆弱**：只在整轮输出**开头**顶格命中；markdown 代码围栏/引用/项目符号/前置散文/加粗任一装饰即漏发。`send.py` 绕过它——**自建全新 socket**手搓 WS 帧发结构化信封（`send_dingwei.py:42`），故可靠。
- **PTY 注入**：`inject_text` 走 `pexpect.child.send` 或 `tmux send-keys`（`cli.py:273`），bracketed-paste 包裹。**DingWei 特有，broker 无法替代**。
- **忙时排队**：`buffer_if_busy`（`app.py:307`）—— CLI 非 idle 则入 `deque(maxlen=100)`，回一次 busy-ack；`is_idle` = PTY 静默 ≥1.2s（`cli.py:195`，输出静默启发式，非真"生成完成"信号）。
- **在线清单**：作为**普通广播信封**收到（非订阅），`is_online_directory_text` 命中即原子写 `~/.dingwei/<name>.DingWeiOnlineSessions.list`（`app.py:440`）。
- **"watchdog"**：仅 `online_list_heartbeat_loop` 每 60s 刷文件时间戳（`app.py:461`）——**只证明协程在调度，WS 半死时仍在刷，反而掩盖假死**。无真正连接级 watchdog（不追踪 last recv/send 成功时刻）。

---

## 2. 维度①：改动量估算

### 2.1 传输面占比（关键前提）
| 组件 | 总量 | 纯传输（broker 可替换） | 业务逻辑（与 broker 无关，留存） |
|---|---|---|---|
| `hub.go` | 4181 行 | **~500–600 行（13–15%）**：连接接入/注册/拆除骨架、`write/ping/close`、心跳、reaper、`RouteEnvelope/routeToSession`、信封去重、地址解析、`forward`、扇出循环 | **~3550 行（85%）**：L1/L2 任务派发树、owner/member/roster 解析渲染、apply-key/sync/mirror/sec-ops、命名派生、飞书桥 glue |
| `terminal.go` | 713 行 | ~40 行（accept/read-loop） | ~670 行（web 终端镜像 + 写锁） |
| `sessionHelper` | 2553 行 | **~110 行**：`_run_once`/`run_forever` 连接+gather（`app.py:136-244`）+ `send_dingwei.py` 手搓 WS | 其余全部：`cli.py`（PTY/注入/收割）、`provision.py`、忙时/命名/飞书过滤/去污/清单落盘 |

**要点：传输缝很窄且隔离良好，但也正因此，换 broker 省不掉那 85%。**

### 2.2 三方案改动量对比
- **C（自研补丁）**：
  - Hub：per-client **writer 泵**（buffered channel + 写超时 + 超时即弃连摘表）；ping/close 脱离 `c.mu`；系统消息优先级绕过忙时；`c.write` 用短 timeout context。**改动集中在那 ~600 行传输面**。
  - 客户端：`send_loop + asyncio.Queue` 替换 5 处直接 `ws.send`（每包 `wait_for` 5–10s）；连接级 watchdog（追踪 last recv/send）；`_run_once` 改 `FIRST_EXCEPTION`。**改动 ~110 行传输面 + 新增 ~80 行**。
  - 广播改异步扇出（每会话独立 goroutine/超时）。
  - **量级：数百行、集中在传输缝、不触碰业务逻辑。约 2–4 人日开发 + 测试。**
- **B（NATS，进程内嵌）**：
  - Hub：内嵌 `nats-server`（library 模式，保持单二进制）；`RouteEnvelope` 的 name→conn 直投改为 **subject 发布**（`dingwei.session.<owner>.<name>`）；owner 鉴权映射到 subject 命名空间/账户；roster 改 subject；`forward` 请求/响应改 NATS req-reply；终端镜像改 subject；**去重语义保留**（JetStream at-least-once 后客户端仍按 env.ID 去重）。
  - 客户端：`websockets` 换 `nats.py`（asyncio 原生，契合），recv 改订阅，outbound 改 publish；**glue 全留**。
  - **量级：触及 85% 中的寻址/路由子系统，需重表达 owner 语义；约 2–4 人周 + 灰度。**
- **A（MQTT）**：
  - 同 B 的客户端/Hub 改造量级，但需**独立 broker 进程**（Mosquitto/NanoMQ）或内嵌 `mochi-mqtt`；owner 鉴权映射到 **topic ACL**（比 subject 账户更粗）；Python 侧 `aiomqtt`（asyncio 尚可但不如 nats.py 干净）。
  - **量级：≈B，且多一层 broker 运维 + ACL 语义适配。**

---

## 3. 维度②：成本对比（一次性迁移 vs 长期维护）

| | C 自研 | B NATS 内嵌 | A MQTT |
|---|---|---|---|
| 一次性迁移 | **低**（数百行/2–4 人日） | 中高（2–4 人周，重写寻址） | 中高（≈B + broker 运维） |
| 新增运维面 | **无**（同一二进制） | 低（内嵌，无独立进程；JetStream 落盘需备份） | 中（独立 broker 进程/ACL/TLS/监控） |
| 长期维护 | 中——writer 泵/watchdog/背压是自研代码，需自己保证正确性 | **低**——连接生命周期/重连/keepalive/持久/ACK 由成熟件兜底 | 低（同 B，但 broker 是第三进程） |
| 收益上限 | 修好当前两个坑；架构仍是"自研传输" | **高**——通道隔离、LWT、req-reply，JetStream 可逐步替代 bus.DBQueue、可跨节点扩展 | 高（同 B），但与本系统语义匹配度低 |
| 主要风险 | 自研并发原语易留边界 bug | 重写正在稳定运行的寻址 85% 的回归风险 | 同 B + 多进程失败面 |

**判断**：本系统当前是**单节点、单 owner（张宗楚）、约 10 会话**。broker 的核心卖点（HA、跨节点、海量 topic、多租户）此刻用不上；而其复杂度税立刻要交。C 的边际成本最低、命中当前痛点最准。

---

## 4. 维度③：边界划分（谁交给 broker，谁必须自留）

| 能力 | 现状位置 | 可交给 broker？ |
|---|---|---|
| 连接生命周期 / 重连 / keepalive | `app.py:136-244`,`hub.go:3431` | ✅ 是（客户端库 + broker） |
| 死连接检测 / 在线状态 | ping+conn_seq+reaper（`hub.go:219,761,3431`） | ✅ 是（MQTT LWT / NATS 连接事件） |
| 逐条 ACK / 至少一次 | 无（at-most-once+去重） | ✅ 是（QoS1/2；JetStream+显式 ACK） |
| 广播扇出（在线清单） | 同步循环 `hub.go:3015` | ✅ 是（broker 异步分发，解 HOL） |
| 通道隔离（控制/数据/终端/镜像分级） | 现全混一条 WS | ✅ 是（多 topic/subject） |
| **会话命名派生 / 强制** | `hub.go:938,3356` | ❌ 自留（业务命名，非 topic） |
| **权限/owner 鉴权 / From 防伪** | `hub.go:706,2276,867` | ⚠️ 半——传输 auth 可用 broker ACL，但 **owner 范围路由语义**仍是自研 |
| **CLI 忙时排队语义** | `buffer_if_busy`/`is_idle`（`app.py:307`,`cli.py:195`） | ❌ 自留。**关键澄清**：MQTT QoS/NATS queue-group 只保证**网络投递**串行/可靠，**不等于**"等 CLI 生成完再注入"——后者是 PTY 静默启发式，永远在 app 层 |
| **CLI PTY 注入** | `inject_text`（`cli.py:273`） | ❌ 自留（broker 只送字节，不会替你 type 进子 TUI） |
| **回复收割（transcript/DB/PTY）** | `cli.py:342-461` | ❌ 自留 |
| **AI 主动消息识别** | AGENT_ROUTE_RE（`app.py:57`） | ⚠️ **不是 broker 直接替代**——脆弱在"扫 stdout 猜意图"；正解是 `send.py` 式**显式 publish 结构化信封**（换 broker 顺带把它换掉，但换掉它本身不需要 broker） |
| **飞书桥接** | `Outbound` 队列（`hub.go:2664`） | ❌ 自留（已解耦，且非阻塞） |
| **web 终端 view 镜像** | `terminal.go` | ⚠️ 可迁独立 subject，但镜像/写锁/unlock 业务自留 |
| **provision / 审计 / 去污** | `provision.py`,`hub.go:895,2184` | ❌ 自留 |
| **持久/可靠投递** | `bus.DBQueue`(SQLite) | ✅ NATS JetStream 可长期替代；MQTT 需外挂持久 |

**一句话边界**：broker 接管**连接层与投递层**（生命周期/心跳/在线/广播/ACK/通道隔离）；DingWei 自留**语义层与执行层**（命名/owner 路由/忙时/PTY/收割/飞书/provision/去污）。**换 broker 不会缩小 85% 业务代码，只会替换 15% 传输代码。**

---

## 5. 维度④：选型 A(MQTT) vs B(NATS) vs C(自研)

**A. MQTT** — 优：LWT/QoS/retained/topic 概念直接命中需求，生态成熟。劣：需独立 broker 进程（内嵌 mochi-mqtt 可缓解但较新）；topic-ACL 对 owner 范围寻址是**粗粒度**匹配；`aiomqtt` 的 asyncio 体验不如 nats.py；QoS 语义与 DingWei 的 req-reply/任务语义**错配**（QoS 解决网络重传，解决不了任务态）。**结论：可用但非最佳，不推荐。**

**B. NATS** — 优：**可进程内嵌**（nats-server as library，**保持单二进制部署**，这是对本系统最大加分项）；req-reply 原生对上 `forward`；queue-group 对上串行消费；**JetStream 可逐步吞掉 `bus.DBQueue` 持久层**；`nats.py` asyncio 原生，契合 sessionHelper。劣：无 MQTT 式 LWT（用连接事件/心跳 consumer 等效）；重写寻址仍触碰 85%。**结论：若上 broker，选 B。**

**C. 自研补丁** — 优：**改动最小最集中**（传输缝 ~600+110 行），**根因已被 dev55 精确定位**，不新增任何基础设施，最快止血；单 owner/单节点场景够用。劣：writer 泵/watchdog/背压是自研并发原语，正确性要自己扛；架构天花板仍是"自研传输"。**结论：当前阶段推荐先做。**

### 推荐（manager 的工程判断）
> **C 先行止血，B 作战略预留，A 不选。**
> 1. **现在**：按 dev55《卡死分析》的"最小可落地补丁"做 C（Hub writer 泵 + 客户端 send_loop + 连接 watchdog + 系统消息分级 + 异步扇出广播）。这以最低风险闭环需求1/2。
> 2. **将来**：当出现{多节点、多 owner/多租户、需要持久重放、会话规模量级上涨}任一信号时，再上 **B（NATS 内嵌 + JetStream）**——届时 JetStream 顺带替换 SQLite 队列。**"规模逼近时再上 broker，别现在交复杂度税。"**
> 3. 无论哪条，**AGENT_ROUTE_RE 都应尽快让位于 `send.py` 式显式发送**（与 broker 无关，可独立先行）。

---

## 6. 维度⑤：部署运维与灰度

- **现网拓扑**：单 Go 二进制 `workpulse`，systemd（`deploy/workpulse.service`），监听 `127.0.0.1:8791`，nginx 反代 `wss://dingwei.wegoab.com/dingwei/ws/`（`proxy_read_timeout 3600s`），单节点 + SQLite 持久化。飞书走**另一条**独立长连（`FEISHU_TRANSPORT=ws`），与会话 WS 无关。
- **C**：无部署变化。发新二进制 + 升级 sessionHelper（已有 provision 下发通道可灰度）。**零新增运维面。**
- **B（NATS 内嵌）**：仍单二进制（nats-server 作为 library 编入 `workpulse`），无独立进程；JetStream 落盘目录需纳入备份（与 `workpulse.db` 同级）。鉴权：NATS 账户/nkey 替代或叠加现 key/secret。**运维面增量小。**
- **A（MQTT 独立 broker）**：Mosquitto/NanoMQ 作为**第二个 systemd 单二进制**（几 MB），nginx 可加一条 WSS location 或客户端直连新端口；鉴权用 broker ACL + TLS 替代 key/secret；需独立监控/日志。**运维面增量中等。**
- **灰度切换路径（若选 B/A）**：
  1. broker 与现 WS **并行运行**，Hub 双写（新旧通道同发），sessionHelper 双连；
  2. 按 owner/会话灰度（现 provision 通道可定点下发新版客户端），先只切在线清单/心跳这类幂等系统消息；
  3. 观察 LWT/ACK 稳定后再切 task/reply 业务消息；
  4. 终端 view、飞书桥最后切或长期保留自研；
  5. 回滚：客户端切回旧 `ws_url` 即可（旧通道未拆）。

---

## 7. 关键排序结论（home 特别关注的第 6 点）

**成立。本评估必须先于需求1/2 的动手实现。**

- 若最终选 **B/A**：需求1（连接自愈=LWT/连接事件）、需求2（ACK+排队=QoS1/JetStream+queue-group）**由 broker 原生提供，不要自研**。此时应把工程量投在"迁移到 broker"，而非先自研一套 writer 泵再推倒。
- 若最终选 **C**：需求1/2 **就是**这次自研补丁的交付物，直接按 dev55 补丁方向做即可。
- **无论如何，二选一，严禁两头做**（先自研 writer 泵/watchdog，再换 broker 把它们扔掉，是纯浪费）。
- manager 本评估的推荐是 **C 先行**——即需求1/2 现在就以自研补丁形式做，同时把 B 记为规模触发的后续项。**请 home 拍板方向后再启动实现。**

---

## 8. 待补 / 待确认

- dev55（代码 owner）的显式倾向已征询（其《卡死分析》"最小可落地补丁"方向本身即等价 C）；如其回复带来线上部署细节或不同判断，将增补至此。
- 若 home 倾向 B，manager 可再出一份《NATS 内嵌迁移 PoC 与灰度详案》。
