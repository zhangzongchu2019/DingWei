# Hub Lifecycle 最小基线与代码上下文

本件按 S-HLC-IMPL 的 T-04 授权补建能力主规格最小基线，不执行 apply/archive，不把工作树实现或局部自测当成已发布能力。基线固定为 r47 c248506cf120c3af2a06eeaeeae3038f596537b1；实施变更与判据以 openspec/changes/hub-graceful-shutdown 为准。

## 基线与证据

- cmd/workpulse/main.go 的 run 打开、迁移 SQLite，装配内外两台 HTTP server、Hub、生产/消费工作及飞书网关；r47 收到信号后使用共享10秒上下文串行 Shutdown，不能据此证明全部长流或 WS 退出。
- internal/m8/hub.go、terminal.go、console_mux*.go 承载会话 WS、内网事件 SSE 与 Console mux/bridge。r47 既有 Close 未在 run 停机路径显式完整接入；WS 是 hijacked 连接，不由 http.Server.Shutdown 保证关闭。
- internal/bus/async_dbqueue.go 的运行期 Enqueue/Flush 为 best-effort。队列确认缺口单列 docs/findings/queue-ack-durability-c248506c.md；本单不能把停机专用排尽推导为运行期已获得跨动作原子持久性。
- 飞书SDK v3.9.7 Start 成功后固定等待帧不因ctx取消返回，Close与该帧join是不同动作。运行归属已由工蜂 review/prodops/fcs-runtime-facts-20261101 c15d6d7884f8452bf7717818f329ec192bca5094／tree2ca8a513ee5e7d1255069a235aa419c0eed783d8 的 REPORT.md 核到：现网为Hub内ws，无RPC键、连接1归Hub；预发fake、连接0。没有独立fcs单元。

## 实施挂点与恢复边界

实施工作树的 cmd/workpulse/shutdown.go 是15秒协调器，main.go 接线注册屏障、HTTP并行排空、Hub.CloseContext、回调与队列/存储关闭顺序；internal/lifecycle.Group 负责原子封口与真实完成计数。它们尚未通过全量独立验收或上线。

S011沿用既有游标与gap协议。真实 Console 的 internal/m8/console_mux_broker.js route(gap)/notify 只提示历史需要明确重新同步并停该scope；console_auth.go closeTab 调用 remove(name,true)，随后 openTab 注册新 iframe_instance，经同feed的 reset_scope 控制重新建立当前保留历史。自动恢复红与显式恢复结果必须分列，不能新增自动reset掩盖原设计；同时记录stop起与显式操作起两组时钟。HLC-PM04的mux≤5秒按组长3f698b77d848裁定限定显式resync后恢复，其余helper/SSE/WS仍按原ready后5秒。

测试只用普通dev、白名单环境、随机loopback端口、临时HOME/SQLite/PG、假helper及隔离VS/网关；不得借本件启动生产或预发操作。当前实施进度、精确Git锚、原红证据与下一步以变更tasks.md和docs/reviews/hlc-process-first为准。
