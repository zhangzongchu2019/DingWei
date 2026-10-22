# 私有控制端点一期实现合同

依据设计 `dbb7cf0e` 与组长一期裁定；实施已合正式 r37 `f10ff1b1` 及其版本位后继 `29e63f99`（0.2.3.10）。只开发／隔离验证，不向集成线写入，不修改任何在线席位。

默认 `SH_CODEX_ENDPOINT_MODE=legacy` 不变。显式 private 只接受 daemon_ws、允许自持启动、身份门 enforce；不满足时拒绝，不回落 legacy。CODEX_HOME、凭据、rollout 与 driver 账本沿用原位置。独立 owner 持 flock 和实际 Popen，registry 只作 locator；控制 FD 核 Python exe、owner PID/starttime 和新 challenge，app FD 在 HTTP 前强比实际 child PID/starttime/exe。已存在未知对象或失联 owner 一律拒接、拒补启、拒按 PID 文件接管或杀进程。

同一端点一个 helper writer 持有 flock，helper 停止／崩溃释放，owner 保持；新准入与显式停止使用读／写互斥锁。显式 stop 另要求操作人确认人工接入已退出，并通过原生有界 loaded-list/read 证明全部 idle；active、未知、分页未尽或 RPC 失败均拒停。该确认不声称能识别绕过入口的人类客户端。人工入口只核活 owner、实际 app peer，再执行显式 ID 的 remote resume，不实现人类 lease 心跳。owner 失联恢复单与透明热切不在一期。

Codex 异常退出由实际 Popen poll/wait 确认，精确清理自己的 inode 后最多三次退避重启；helper 连接代次变化沿用已审 thread/read→resume 同 ID，不重放账本、不新建替代线程。正常停止写 tombstone，防止随后 helper 自动重启已明确关闭的 owner。owner 运行包在其退出前须保留；兼容复用要求 owner 协议、实现摘要、配置及实际解释器／Codex 身份相符，改变 owner 实现须安排空闲维护，不能以“helper 升级”绕过身份合同。

**原生别名例外（组长已裁定）。** 0.161.0／0.163.0 会创建 app.sock 软链，真实 socket 在原生按 UID 共享的目录，不能称 helper 独占。仅 owner 自持 Popen 启动之前已证明 app.sock 不存在、且随后出现的别名可以解析；readlink 仅一次，必须等于 helper 自算的 canonical `/tmp/codex-daemon-UID/SHA256(canonical 请求路径 OS 字节)`。目录非软链、本 UID、0700，目标为本 UID 的 socket0600；连接直接走计算目标，同一 FD 强比自持 PID/starttime/exe，任何 HTTP 前核根、别名、目录和目标 inode 均未漂移。owner 保持 alias／target／directory 的 O_PATH 句柄直到清理，防止对象被删后 inode 立即复用。后续 helper／owner 连接只比较活 owner 描述符及现状，不再次 readlink、不经别名连接；根、锁、registry、控制 socket 仍一律拒软链。只核并精确清自己的单目标，不扫描或改动同 UID 目录内其他对象；旧 owner 失联留证拒接，绝不凭 registry 去清理。

**当前绑定与人工接入。** helper 当前 ID 单独存 binding.json（600、活 owner boot＋配置绑定），只有唯一 writer 可写；fresh helper 读提示后仍须 read/resume 核原 ID，历史回执不选择或改写它。人工入口拒绝与该当前 ID 不同的请求，核活 owner 与真实 app FD 后 exec 原生 remote resume，连接同样走确定的真实目标。停止须人工已退出的显式确认及所有加载线程 idle；连接流在验证后恢复无空闲超时，不把安静误判为断连。停止已发出但 child 迟退／清理失败不会再自动补启。

隔离验收分三层：本地 owner／假 native 完整生命周期与错误路径；全门检查器逐门指定红／恢复绿；锁定两版官方真实 owner、fresh helper 进程、child 异常退出同 ID、受控入口 PTY 和空闲清理。全套 helper、canonical、Go／vet 与相关浏览器另外记录，不用本地惰性结果替代实际版本或上线验收。初始空线程缺 rollout 的 PTY 失败、开发中夹具错误均保留，最终以冻结源报告为准。

官方源码：`openai/codex` tag `rust-v0.163.0`，annotated tag object `129b8ec7ee6f4511557a4856279f529025c25af4`，peeled commit `83086188ae0614bd882818ff1aa23f3194962012`；`unix_socket.rs` blob `afc69753b292bda40f3111bfae06316965f2e27e`，`daemon_directory.rs` blob `90c3c103bc8d564a2cdfc658e1e80e76769bcebe`。该固定运行目录明确不受 HOME、TMPDIR、CODEX_HOME 配置影响；不得通过修改共享目录掩盖路径合同。

0.161.0 官方 tag object `7e21416b38834816c224ea0dfd135c3de94b2f15`，peeled commit `979011409de0a60b52f179721948e65531d26144`；unix_socket.rs blob `3f70bcced464e3ce0592c722d155efd06ea04ae1`，daemon_directory.rs blob 与0.163.0相同。两版路径推算函数及目录合同一致，整个 unix_socket.rs 并非相同（原生 startup lock 生命周期有版本差异），本件不改或清其 lock 文件。前文曾将0.163.0 annotated tag SHA写成commit，现明确订正对象类型；原blob坐标与合同判断不变。
