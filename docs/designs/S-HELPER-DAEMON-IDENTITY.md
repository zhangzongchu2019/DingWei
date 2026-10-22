# S-HELPER-DAEMON-IDENTITY（半页设计）

基 r36 `c78c66c2fe1ccf2ce25ecca40de3819ef7d22800`，分支 `review/coder/daemon-identity`；依据 togpt60 诊断 `042f9d7fa962f28423d60db0811965c52f807451`。只开发、本组隔离验证，不改在线席位，不扫描进程列表、不杀未知daemon、不建替代thread。

**连接门：** 仅daemon_ws；Unix socket connect后、任何HTTP Upgrade／initialize／thread／turn字节发送前，对同一FD取Linux SO_PEERCRED，校验UID为本席、PID为有效正整数。读取该PID的 `/proc/PID/exe`（readlink＋设备/inode身份）及stat.starttime，并前后复读starttime，消除采样期间退出／PID重用；失败或不一致即关FD、保持不可用，固定类别可观测错误，不打印原始异常／凭据／线程正文。预期可执行文件用核定的原生Codex文件：默认解析既有SH_CODEX_BIN／PATH；若为npm／脚本包装器则不按basename猜测厂商子路径，可显式设SH_CODEX_DAEMON_EXE到经核官方原生文件。缺失、wrapper与实际exe不符、deleted或/proc不可读均拒连。

**实例绑定：** 可信启动者若直接持有daemon的PID＋starttime，则传给driver并在每次新连接逐项比对；不接受任意PID环境变量，不把daemon-start命令PID或首次看见的peer冒充自启证明。当前 `_start_daemon` 只有命令rc，诊断及锁定0.161.0／0.163.0 start帮助未提供PID输出合同，因此现有生产路径只做UID＋exe门、记录peer证据与检查范围；不能宣称区分同UID同exe的两个daemon。保留可验证绑定接口并覆盖正负格，后续启动方有可信句柄时再接，不猜协议。重连每次从新FD重新校验，不沿用上次通过。

**拒绝与兼容：** 新开关SH_CODEX_DAEMON_IDENTITY_CHECK，候选默认1，仅显式0为兼容关闭并明示未校验，非法值失败关闭；默认开的候选只在隔离验证通过后交审，启用／升级另批。direct/proxy保持原合同，不声称也被本门保护；无SO_PEERCRED或/proc平台在开关开时不可证即拒。start失败不留下健康RPC或初始化状态；投递返回持久session_unavailable与结构化失败事件、不新建thread、不turn/start、不自动重投；投递ACK语义不改。

**验收：** coder本席私有HOME／CODEX_HOME、离线或本地拒绝模型端点，实测锁定官方0.161.0／0.163.0 Unix listener；先正负验证，再默认开全套。真实同UID假Unix服务、错误exe／UID、proc拒读、PID/starttime不符／重用、重连换peer、关闭／非法开关、握手前零发送、FD关闭、无新thread／turn与失败可见，逐门撤修指定红；隔离HOME锁定venv helper全套、canonical及相关Go／浏览器输入未变证明。升级预检须核原生exe实际路径与SHA，避免包装器席位无准备开启。官方说明仅确认Unix WebSocket采用Upgrade流程，不作为锁定版PID合同：[OpenAI App Server](https://learn.chatgpt.com/docs/app-server)。

## 组长返修定稿（2026-10-22）

前述“失败即拒／默认1”仅保留为首审历史，当前实现以此段覆盖：开关三态默认observe，核验失败产生固定类别告警、结构化unverified观测并继续原连接；显式enforce才拒连并执行原持久失败合同；off不核验。先本组observe采样、确认无误报后逐席另批enforce，不自动切换。非法值拒连。本次仍仅私有环境测试，不改在线席位。
