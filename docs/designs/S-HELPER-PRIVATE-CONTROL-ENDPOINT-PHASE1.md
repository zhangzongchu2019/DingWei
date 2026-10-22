# 私有控制端点一期实现检查点（开发中）

依据设计 `dbb7cf0e` 与组长一期裁定。当前产品基线仍为 `ce58c1bf`，已带 test-only 竞态修 `ee8025ef`；正式 r37 锚到件后合入并重跑最终门，不向在办集成线写入。

默认 `SH_CODEX_ENDPOINT_MODE=legacy` 不变。显式 private 只接受 daemon_ws、允许自持启动、身份门 enforce；不满足时拒绝，不回落 legacy。CODEX_HOME、凭据、rollout 与 driver 账本沿用原位置。独立 owner 持 flock 和实际 Popen，registry 只作 locator；控制 FD 核 Python exe、owner PID/starttime 和新 challenge，app FD 在 HTTP 前强比实际 child PID/starttime/exe。已存在未知对象或失联 owner 一律拒接、拒补启、拒按 PID 文件接管或杀进程。

同一端点一个 helper writer 持有 flock，helper 停止／崩溃释放，owner 保持；新准入与显式停止使用读／写互斥锁。显式 stop 另要求操作人确认人工接入已退出，并通过原生有界 loaded-list/read 证明全部 idle；active、未知、分页未尽或 RPC 失败均拒停。该确认不声称能识别绕过入口的人类客户端。人工入口只核活 owner、实际 app peer，再执行显式 ID 的 remote resume，不实现人类 lease 心跳。owner 失联恢复单与透明热切不在一期。

Codex 异常退出由实际 Popen poll/wait 确认，精确清理自己的 inode 后最多三次退避重启；helper 连接代次变化沿用已审 thread/read→resume 同 ID，不重放账本、不新建替代线程。正常停止写 tombstone，防止随后 helper 自动重启已明确关闭的 owner。owner 运行包在其退出前须保留；兼容复用要求 owner 协议、实现摘要、配置及实际解释器／Codex 身份相符，改变 owner 实现须安排空闲维护，不能以“helper 升级”绕过身份合同。

**真实协议差异待裁定。** 锁定 0.161.0／0.163.0 实测请求 app.sock 为软链，真实 socket 为 canonical `/tmp/codex-daemon-UID/SHA256(canonical 请求路径字节)`，父 0700、socket 0600。0.163.0 官方 Git tag 对应实现已核；0.161.0 源码 fetch 因网络失败未完成，实际二进制读数不受影响。根／锁／registry／控制 socket 继续拒软链；只对自己新启动 Codex 的确定别名是否作限定解析，已呈组长裁定。当前实现仍严格拒 app 软链，所以尚不能通过双版本产品正例。

当前本地定向测试 23 项通过，覆盖路径、模式、registry、owner 活认证、重放、写入排他、准入／停止互斥、helper 重启复用、错误 native PID 握手前拒绝、子进程失败回收。开发阶段 helper 全套在隔离 HOME 跑中。独立原生 PTY 验证与产品 owner 验收分开记录，未把初始“Resuming”画面当作恢复成功。尚无正式冻结源全门或部署结论。

官方源码：`openai/codex` tag `rust-v0.163.0`，commit `129b8ec7ee6f4511557a4856279f529025c25af4`；`unix_socket.rs` blob `afc69753b292bda40f3111bfae06316965f2e27e`，`daemon_directory.rs` blob `90c3c103bc8d564a2cdfc658e1e80e76769bcebe`。该固定运行目录明确不受 HOME、TMPDIR、CODEX_HOME 配置影响；不得通过修改共享目录掩盖路径合同。
