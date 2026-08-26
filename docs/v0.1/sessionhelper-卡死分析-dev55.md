# sessionHelper 卡死分析（dev55）

## 结论摘要

这次现象不是单纯“Hub 心跳 ping 有 bug”，而是 sessionHelper/Hub 两端都把控制面、数据面、终端镜像和 keepalive 放在同一条 WebSocket、同一个事件循环/写锁链路里。一旦其中一个 `send/write` 或 adapter 调用进入长等待，其他循环看起来仍在运行进程内，但实际没有可观测活动：不收新业务消息、不重连、不再向 view 推终端输出、不写新的日志。Hub 回滚后，已经卡住在旧连接/旧 await 上的 sessionHelper 不会自动退出，所以只能靠手动重启恢复。

## 1. sessionHelper 为什么会卡死

主要卡点在 `tools/sessionhelper/sessionhelper/app.py` 的连接生命周期：

- `_run_once()` 在 238-244 行用 `asyncio.gather()` 同时跑 `recv_loop()`、`pending_drain_loop()`、`terminal_loop()`、`outbox_loop()`、`mirror_loop()`。这些循环共享同一个 `ws`，共享同一个 adapter，且没有任何整体 watchdog。
- `recv_loop()` 在 292-294 行对普通入站消息先走 `buffer_if_busy()`，不忙时再 `process_inbound_message()`。
- `process_inbound_message()` 在 357 行 `await asyncio.to_thread(self.adapter.handle, env)`。对 CLI adapter 来说，这会进入 `tools/sessionhelper/sessionhelper/cli.py` 的 `handle()`。
- `CLIAdapter.handle()` 在 214-267 行可能启动 CLI、注入文本、等待 transcript/opencode DB/PTY 输出；默认同步等待路径最长会等待 `cli_reply_wait`，并且 `inject_text()` 在 273-286 行拿 `write_lock` 写 PTY/tmux。
- `pending_drain_loop()` 在 347-349 行从忙时缓存取消息后也直接 `await self.process_inbound_message(ws, env)`，因此排水循环可以被某条缓存消息长时间占住。
- `terminal_loop()` 在 614-626 行从 adapter 取终端输出后 `await ws.send(...)` 推给 Hub。若这个 `ws.send` 卡住，view 页就没有新的终端镜像。
- `mirror_loop()` 在 569-572 行也会调用 `adapter.start()` / `adapter.next_mirror_event()`，再在 578、585、590、599、603 行写同一条 `ws`。

触发因素是 Hub 部署了主动 session ping。Hub 当前相关代码在 `internal/m8/hub.go`：

- `HandleSessionWS()` 795-796 行为每个 session 启动 `startSessionHeartbeat()`。
- `sessionClient.write()` 3412-3416 行和 `sessionClient.ping()` 3419-3422 行共用同一个 `c.mu`，分别做 `conn.Write()` 和 `conn.Ping()`。
- `startSessionHeartbeat()` 3431-3445 行每 30 秒拿同一个 `c.mu` 做 ping，10 秒超时后尝试 `c.close()`。

这个结构里有两个危险组合：

1. Hub 主动 ping 与普通投递共用 `c.mu`。如果某次 `conn.Write()` 或 `conn.Ping()` 卡在底层网络/库调用里，其他投递、ping、close 都会排队等同一把锁。尤其 3444 行 `c.close()` 也要再拿同一把 `c.mu`，如果前一个持锁调用没有返回，关闭也无法打断。
2. sessionHelper 端所有业务循环都共用同一个 asyncio loop 和同一个 `ws.send`。当 `recv_loop`、`terminal_loop` 或 `mirror_loop` 中任何一个 `await ws.send(...)` 没有返回时，相关循环停住；若同时 adapter 调用正在等待 CLI 输出或排水，外部表现就是“进程活着但没有任何活动”。

所以具体卡住的链路很可能是：

`Hub startSessionHeartbeat -> c.ping()/c.write 共用锁或写阻塞 -> sessionHelper 的 ws 收发状态异常但连接未正常关闭 -> sessionHelper 某个 ws.send/adapter await 长时间不返回 -> gather 内各循环不能产生新日志/新镜像/新投递处理`。

## 2. 客户端 ping keepalive 为什么没能自救

客户端确实在 `app.py` 212-219 行 `websockets.connect(..., ping_interval=20, ping_timeout=20)`。但它只能检测“客户端事件循环还能调度 websockets 协议任务，且底层连接读写能被协议栈正常推进”的情况，不能解决以下问题：

- keepalive 不是独立进程/独立线程 watchdog；它运行在同一个 asyncio 事件循环里。若事件循环被某个协程的长 await、写背压或默认线程池拥塞间接拖住，ping 定时器和 close/reconnect 逻辑也不会及时执行。
- `run_forever()` 只有 `_run_once()` 返回或抛异常才会重连，见 `app.py` 140-153 行。只要 `_run_once()` 仍卡在 `async with websockets.connect` 内部的 `gather()`，外层没有机会进入 except 和 sleep/reconnect。
- `websockets` 的 ping_timeout 只覆盖它自己发出的 ping 等不到 pong 的场景。如果 TCP/库状态表现为“写 await 卡住、读协程不退出、服务端也没正常 close”，并不保证 20 秒后一定让用户层 `_run_once()` 抛异常。
- Hub 端的主动 ping 失败后执行的是 `c.close()`，但 `close()` 也在 `hub.go` 3425-3428 行拿同一个 `c.mu`。如果卡死原因就是有 goroutine 持有 `c.mu` 卡在 `conn.Write/Ping`，这个 close 无法抢占，客户端也就收不到明确 close frame。

因此客户端 ping 是“协议层空闲保活”，不是“业务循环和写路径卡死自愈”。这解释了为什么 Hub 回滚后旧 sessionHelper 仍然不恢复：外层重连循环从未重新获得控制权。

## 3. 根本架构缺陷与修复建议

根本缺陷是缺少隔离和背压边界：

- 一条 WS 同时承载业务消息、系统控制、终端输出、镜像采集、在线清单、主动路由和 keepalive。
- Hub 端所有写和 ping 共用一把 per-client mutex，且 close 也需要同一把锁。
- sessionHelper 端多个永久循环共享同一个 `ws.send`，没有单写队列、发送超时、失败熔断和连接级 watchdog。
- adapter 是同步 CLI 的薄封装，虽然用 `asyncio.to_thread()`，但没有统一超时、取消、隔离线程池和健康探针。忙时缓存解决的是“CLI 忙时不要立刻注入”，没有解决“系统消息/心跳/终端镜像必须永远独立”的问题。

建议按优先级修：

1. Hub 端不要用 WebSocket protocol ping 参与业务写锁竞争。若保留主动 ping，必须保证 `Ping` 和 `Close` 有独立、可抢占的超时路径；`close` 不能被同一把 `c.mu` 永久阻塞。更稳妥的是写泵模型：每个 sessionClient 只有一个 writer goroutine，业务写、ping、close 都投递到带缓冲 channel，写超时则直接废弃该连接对象并从在线表摘除。
2. sessionHelper 端也改成单 writer 队列：`recv_loop`、`terminal_loop`、`mirror_loop` 等只 `put` 到 `asyncio.Queue`，由 `send_loop` 统一 `asyncio.wait_for(ws.send(...), timeout)`。超时即抛出连接级异常，让 `_run_once()` 退出，外层 `run_forever()` 重连。
3. 给 `asyncio.gather()` 外围加连接 watchdog。记录最近一次成功收包、成功发包、终端输出入队、online_list 刷新时间；超过阈值无进展时主动 close/raise，而不是等协议 ping。
4. 系统控制消息必须和普通任务消息分级。`online_directory`、`agent_network_skill`、`terminal_input/resize`、provision、heartbeat 类消息不应进入 `buffer_if_busy()` 或 `adapter.handle()`；Hub ping/在线探测也不应以会被 CLI 注入的普通 envelope 形式进入客户端。
5. adapter 调用加硬超时和隔离线程池。`adapter.handle`、`adapter.start`、`next_mirror_event`、`next_terminal_chunk` 不应无限占用默认 executor；超时后返回“CLI 未响应”或触发 adapter 重启，而不是拖住连接生命周期。
6. 终端 view 最好拆独立通道或至少独立发送优先级。当前 `terminal_loop` 的输出要和业务回复/collect/mirror 共用同一条写路径，一旦业务写阻塞，view 就空白，诊断能力也一起丢失。

最小可落地补丁方向：

- Hub：把 `startSessionHeartbeat()` 临时关掉，或确保 ping/close 不抢 `c.mu` 且不会阻塞普通投递；同时所有 `c.write()` 使用短 timeout context，不允许继承无限 `r.Context()`。
- sessionHelper：新增 `send_loop` + `send_queue`，替换所有直接 `await ws.send(...)`；所有发送包 `wait_for` 5-10 秒，超时 raise `ConnectionError`；`_run_once()` 用 `asyncio.wait(..., FIRST_EXCEPTION)`，任一核心循环异常即取消其余循环并重连。
- 健康检查：online list 心跳不要只证明进程活着，要证明连接读写最近成功；否则文件头仍可能被本地心跳刷新，掩盖“WS 已经半死”的状态。
