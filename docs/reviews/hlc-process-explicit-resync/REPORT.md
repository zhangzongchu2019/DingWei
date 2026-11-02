# S011 两条 missing 去向补证（独立观察，非正式交件）

2026-11-02，coder56solzzc.zzc。原始首轮 bdac3593c8a578eb5002b8df9418dc93b2cb93c9 的 missing2 是新 Hub READY 后发布的 hlc-after-00 与 hlc-after-verify；不是停机前旧历史。

本次真实隔离 Hub／PG／三台 Python VS 重现自动格 gap1、missing2，随后保持同一 feed，按 console_auth.go closeTab/remove(true) 与 console_mux_broker.js resetIntents/sync 的 reset_scope＋新 iframe_instance 协议模拟显式操作。没有修改产品、自动重置或补救历史持久化。此为协议模拟操作计时，不是浏览器点击或完整 UI 验证。

显式恢复：missing2→0、dup0，两条各补回一次。操作→验证输出248ms；stop→输出7462ms，包含先等待自动恢复5秒。自动恢复仍为红，不能用显式恢复绿覆盖。

| delivery_id | session_event 信封 ID | 发送开始 UTC | 距 stop | 新 Hub SSE／mux 游标 | 补回次数 |
|---|---|---|---:|---|---:|
| hlc-after-00 | hlc-after-00-event | 2026-11-02T01:51:55.945432845Z | 停机后 2206ms | 9825396f4ccddae6-1 | 1 |
| hlc-after-verify | hlc-after-verify-event | 2026-11-02T01:51:55.987844152Z | 停机后 2248ms | 9825396f4ccddae6-2 | 1 |

发送开始为测试发送端观测，不冒称 Hub 内部事件生成时钟；SSE接收UTC与mux补回UTC逐条在 S011-explicit-resync.json。二者均在新进程中产生并可回放，不能作为 R15“旧Hub内存环丢失／内容不可恢复”的直接证据；本次没有验证旧历史跨重启持久化。

本轮总 rc1：恢复断言通过后，对新 Hub 做后续清理停机出现 drain operation_failed／exit1，原日志与 shutdown JSON 保留，不能报整轮通过。新进程日志还出现两个 Serve accept closed-network-connection 错误；与该 failure 的因果尚未验证，不在此报告定因或修产品。

执行中发现同一共享工作树被另一路会话并发修改（其新增 S011-resync 与 hlcExplicitResync，并启动 PID846845／846854）。发现后停止本轮共享源码编辑与提交，已向组长报告；需由原单一执行流整合两套显式 resync 逻辑，再复跑完整门后给出 Git 坐标。本目录不是已推送 Git 交件，现有 bdac3593 也不包含这次补证。源码不经消息或文件服务传送。
