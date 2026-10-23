# S-HELPER-INBOX-LAG A — review candidate

基 r38 `30463142e6b0696763934b3f3d9377194e55922c`，按已审设计 `fec9e63d3c3ec13c490d312dd13edf63e3d6efd1` 实现 A。仓库 `git.code.tencent.com:wego-ai/dingwei`；分支 `review/coder/helper-inbox-lag-a`。执行源 `6a297ec4320a2e0224437849ba77206880aeccb3`，tree `5385779d6f4ec750df083316490755e44c857b06`。证据提交仅追加本目录；全部正式最终门及最终撤修均绑定执行源。

长轮次的旧 processed／injected ACK 仍保持原义，另用持久进度表记录 disk_received、adapter_queued、dispatching、model_admitted、terminal 及 unknown／failed／cancelled。只有现有 driver 协议接收证据推进新阶段；Claude processing/submitted 仍等 isReplay 或实际 result，不修改原调度或 ACK。等待按单调时间计算，墙钟回拨后的新信不能挤掉真正最老项；跨重启估计有界并标不确定。重启保留已落表的确认；旧 processed、丢失的内存队列／dispatch 证据保守 unknown，不主动向 CLI 发恢复 RPC，也不以遥测补投正文。

Hub 只收当前专属认证连接的本人快照，绑定连接 epoch／instance／seq，重复和旧连接不续期。每五秒及阶段变更上报；十秒无新快照显示 stale，离线／旧 helper／观测失败不显示假零。聚合覆盖全部本地待确认消息；单消息详情限 256 项并优先保留当前 pending，未覆盖项为 unknown。Hub 缓存属于当前连接、重连由 helper 全量重建；持久权威仍在 helper，发送归属仍取 Hub 原投递记录。

View 状态条显示「已到盘未注入 N 条，最老等待 X 分钟」。复用现有签名资源查询与已验证 View 主体；初始摘要在 agent_disk.inbox_lag，之后可见 View 用 inbox_lag_only 每五秒单飞查询。隐藏、失活、关闭不查询；通用资源刷新保持原五分钟，专用查询不读本机资源或费用。浏览器本地十秒到期撤掉实时数字，不依赖下一轮请求成功。

send_message 保留 sent，新增有界两秒 Hub 入队回执、对端阶段与 status_ref；dw_delivery_status 按 delivery_id 只读查询。新观察请求不强制目标使用可靠投递，旧目标仍按原路直投一次；原桌面强确认模式保持。每次查询核发送归属及当前编组／leader-only／活跃身份，越权统一拒绝，不泄露其他消息 ID 或正文。超时、丢回执、断连、取消均不重发，结果不明保留原发信 ID；受限 MCP／IPC 不开放新工具。

仅 A；未实现 urgent 参数、插队、steer、hook 或新的 interrupt 行为；默认 FIFO、自动投递、现有权限及能力下限未变。版本与 runtime lock 相对 r38 原样。未部署、未重启、未改在运配置。协议与浏览器验证均为隔离夹具，不宣称生产席已运行此功能，也不宣称已定位今天延迟的全部根因。

## 验证

最终门结果见 VERIFY.json，命令、环境白名单、UTC 起止、rc、执行源及逐文件 SHA 在 raw 各门 JSON；原始 stdout/stderr 保留。canonical 增三文件及计数钉值同步为 Python 66／shell 2；完整 helper 使用独立 HOME、既有锁定 venv（websockets15.0.1／pexpect4.9.0／ptyprocess0.7.0／certifi2026.7.22）、短 ext4 TMPDIR；Go 包串行、PG strict/embedded，无跳过授权。

真实认证双 WS 覆盖入队回执→旧 injected ACK→adapter_queued 查询→model_admitted 查询；另验旧目标仍直投。真实 Claude parser/ledger 验 isReplay 前后；共用适配器实际 settlement 长轮次夹具验 Codex／Claude 第二条等待且 FIFO 不重放。真实私有 IPC＋shim 验正常与受限模式。Chromium 桌面／移动两宽度共 20 项，执行真正五秒定时轮询、定时失效、隐藏／关闭停查，HTTP／SSE 全拦截，无生产连接。

撤修 11 刀逐项「原版绿→撤修语义断言红→恢复绿」：admission 落表、Claude 弱 processing、防重启伪推断、发送归属、当前组权限、Hub 十秒 freshness、View 本地过期、查询连接绑定、受限 IPC、新观察不改变旧直投、View 定时轮询。raw/withdrawals 中逐刀给原／撤修／恢复 SHA、命令与日志；没有用编译失败冒充撤修红。

既有 MCP 工具清单由 24→25：同步 forwarder、question、template 的钉值及全工具传输错误夹具；原负例断言保持。旧连接／弱桌面回执夹具补新 loop／handler／未协商 broker。首轮 canonical 与 helper 的清单／夹具失败、追加 vet 发现的测试复制 mutex，及后续修正前绿门如实保留在 raw/prior；没有把首轮失败算通过。原始 stderr 包含既有负例诊断、资源告警和夹具退出线程信息，不宣称 stderr 空白。

| 最终执行门 | 结果 | 秒 |
|---|---|---:|
| canonical Python66／shell2 | rc0 | 152.247 |
| helper 977 tests | rc0 | 434.576 |
| 全仓 Go：53 测试包、13 无测试包 | rc0 | 649.365 |
| 全仓 vet | rc0 | 2.027 |
| Hub inbox-lag 定向 race | rc0 | 26.221 |
| 真实 Chromium：20 项／两宽度 | rc0 | 1.367 |

全部 11 刀撤修恢复后工作树源码逐文件 SHA 与执行源一致。校验文件 SHA256SUMS 可在本目录用 sha256sum -c SHA256SUMS 复核。
