# S-HELPER-STALE-THREAD-FIX（r34 后继，只开发）

基 c61866cd；本件落实诊断第4节1、2、3、5，不补历史正文、不改 ACK、不部署或操作席位。

- 新连接 initialize 成功时增加连接代次并使验证缓存失效。当前 RPC 对象、连接代次、活动 thread 三者共同组成验证键；新投递在重连锁内验证并提交，桥不能在验证与提交之间换连接。
- 已绑定 thread 先 thread/read（includeTurns=false）；若明确 notLoaded，再 thread/resume（excludeTurns=true）。成功响应必须同 ID、同工作目录、idle／active、canAcceptDirectInput=true；未恢复不准入。原本未绑定的新 helper 仍可首次 thread/start；有旧绑定时绝不自动新建替代。
- 方法与错误形态由锁定 Codex 0.159.2 实测：read 缺失为 -32600＋thread not loaded，resume 无历史为 -32600＋no rollout found for thread id，turn/start 缺失为 -32600＋thread not found。严格比方法、整数错误码和准确 ID，不能仅用 -32600 或子串。未知协议形态失败关闭，不猜恢复。
- 测试在私有 HOME／CODEX_HOME 下启动自己的 stdio app-server；无认证文件、无继承凭据，模型提供者只指向127.0.0.1:9。一次本地 turn 用于写出 rollout，不以模型完成为门。重启私有进程后原 ID 恢复成功；临时 thread 与不存在的 ID 恢复失败。还以真实 helper driver 验证两条路径。与官方说明一致，但仅上述锁定版实测作为实现证据：[app-server](https://learn.chatgpt.com/docs/app-server)。
- 明确不可用返回 failed receipt，detail 为 session_unavailable 与受限错误分类。RPC 错误保留 method／code／message 的内部分型，不把原始 message／data 放进事件或日志；超时与 EOF／断连分开。turn/start 派发前先落 unknown 账本，避免受理后 EOF／崩溃导致重投；未知／内部 RPC 错误也不冒称未受理；不确定回执停本轮，使用 model_admission_unknown，后续同 envelope 不重发。无自动恢复操作。
- 历史 envelope 查找、poll、interrupt、状态落库按记录自身 thread 进行，不修改活动 thread 别名或验证键。已受理后失败的 turn 也不再重新提交。账本 schema 与契约不变；旧 helper 不理解 unknown 无 turn 的新细分，回退运行前须冻结投递、对账不确定项，禁止清账本后回放。
- Adapter 将准入失败转换为带 delivery_id 的 turn_completed/outcome=failed 结构化事件，reason 区分 session_unavailable 与 model_admission_unknown。View 只在对应输入卡加模型状态附注，不改已送达阶段、不造回复或空 turn 卡；乱序待配对缓存有界且重置清空，不显示服务端原始错误。结构化出站仍遵循现有 SH_EMIT_SESSION_EVENT=1，不擅改席位开关。
- 回归覆盖恢复／缺失、方法错误分类、连接代次／对象变化、验证与派发并发、准确绑定、历史查询、未知准入防重、ACK不变与失败事件、View乱序和重放。撤修须命中指定断言；正式全仓Go、vet、隔离HOME＋锁定venv helper、canonical、相关View浏览器门。helper运行时锁与版本号不改，本件待集成决定发布版本。

尚未证明 oaagent 的现场根因；本件真实协议测试不连接生产、不验证真实席位历史可恢复性。历史正文补读继续另列。

## 上线前补证：0.161.0／0.163.0 与同机双 app-server（2026-10-22）

官方 npm 包独立下载、元数据 integrity／shasum 比对及包／二进制 SHA 见 `docs/reviews/coder-helper-stale-thread-followup-20261022/`。两版按 0.159.2 同法在私有 stdio app-server 实测：重启后原 ID resume 成功；临时 thread 丢失及不存在 ID 的 read／resume／turn/start 均为同样的 -32600 与准确文本。直接调用已审 JsonRpcError.classify，六种实测输入每版全部符合现有分类；另各跑真实 driver 的恢复／不可恢复两路径。此次不改产品码。

### daemon 身份与线程绑定的边界

- 默认 daemon_ws 连接 `_daemon_socket_path()` 展开 HOME 得到的 `~/.codex/app-server-control/app-server-control.sock`，不是按进程列表选最新 PID；`manage_daemon` 只运行 daemon start 并检查返回码，不把其结果绑定到连接。socket 路径函数未显式使用 CODEX_HOME，而启动子命令继承进程环境，因此不一致的 HOME／CODEX_HOME 也属于须排查的配置边界。
- `UnixWebSocketJsonRpc.start()` 连接 Unix socket 后做 WebSocket 升级，只检查状态行含 101；不核 SO_PEERCRED、服务端 PID、可执行文件摘要、socket owner／inode 或 daemon boot ID，initialize 返回值也未作为身份凭证校验。当前连接代次是 helper 的局部计数，RPC 对象是本地传输对象；二者不能证明服务端进程身份。
- 同机看到两个 app-server PID（报告值 219／485）本身不能判哪一个是当前 daemon：可能是不同 HOME 的 daemon、stdio 子进程或 proxy。Unix socket 的当前 pathname 对应监听者；已连接 FD 不会仅因 pathname 被重建就自动迁到新监听者。现实现没有将该 FD 与两个 PID 做身份比对，不能识别「连到了非预期 PID」。本报告未读取 oaagent 现场进程，不把上述可能性定为其实际根因。
- 当前实际行为：换传输并 initialize 后先检查原 thread ID、cwd、状态与 direct-input 能力；缺失或绑定不符则 session_unavailable、不自动新建、不重投。连接失败／超时／鉴权拒绝分别分类；turn/start 受理情况不明则保留 unknown 账本、发 model_admission_unknown。若另一个 daemon 能成功恢复同 ID、同 cwd，现检查仍可通过，**没有额外的进程身份不明即拒绝门**。这只能证明逻辑线程恢复，不能证明原 daemon PID 一致。
- 运维建议（非本件已实现检查，也不授权现场变更）：上线前只读关联 helper 的 HOME／CODEX_HOME、实际 socket 与监听／已连接 FD、进程 UID／可执行路径，确认唯一预期端点；无法确认时暂停该席升级并报组长，不试连接生产 RPC、不杀进程。若需产品化核验 daemon 身份／epoch，应另立项；SO_PEERCRED 也只能证明内核对端进程身份，不能单独证明业务 thread 历史归属。
- proxy 模式拥有 proxy 子进程句柄，但不能据此证明其后端 daemon；direct 模式拥有 stdio app-server 子进程句柄，本次真实协议实测限该独立 stdio路径，不声称实测了双 daemon 竞争或 oaagent 现场 socket。
