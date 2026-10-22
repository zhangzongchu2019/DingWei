# S-HELPER-DAEMON-IDENTITY（本组隔离候选，待组长审）

基 r36 `c78c66c2fe1ccf2ce25ecca40de3819ef7d22800`；半页设计 `241fbda0ad45b3ed47ff516b8f2c2720412ada89`；冻结实现 `b58f4e990237e6a53c26ef80518d035afd2bc458`，tree `4fa6b32ba668237c314bddc9d8ff440fe5d5333e`；分支 `review/coder/daemon-identity`。冻结源后只追加 docs/证据，不是集成线或上线授权。

## 实现与安全边界

运行时代码仅 `sessionhelper/daemon_identity.py` 和 `sessionhelper/codexDriver.py`。daemon_ws的同一已连接FD在HTTP Upgrade／initialize／thread／turn任何发送前，用SO_PEERCRED取得PID、UID，严格校验本席UID；读取/proc/PID/stat的starttime、readlink exe及其设备/inode，与核定预期原生可执行文件比对，再复读starttime稳定性。PID、starttime、不可读proc、deleted exe、错UID／exe、可选可信自启绑定不符均固定类别拒绝。采样不扫描进程列表、最新PID或不可信pidfile，不读取cmdline/environ，不终止未知进程。

接入UnixRPC时失败直接关闭原FD（不是向未经核验对端发送WS close），driver RPC与初始化／准入绑定清空；可见异常只含固定daemon_identity类别。driver发session_unavailable错误事件；deliver在身份拒绝时持久failed/session_unavailable、重复envelope只查账，不创建／替换thread、不turn/start、不自动重投。ACK含义不改。首次adapter尚未启动时按既有helper启动失败日志路径显示固定异常，driver事件仍保留；不将本地事件缓存冒称首次失败已送达Hub View。已运行桥按原结构化错误路径转发；本件不改Hub、renderer或ACK状态机。

`SH_CODEX_DAEMON_IDENTITY_CHECK`启动快照：默认1，仅显式0兼容关闭并警告identity_scope=unchecked，非法值拒连；Linux没有SO_PEERCRED或proc不可证时不静默放行。direct/proxy不使用本门且原合同保持，不能宣称其后端也已验明。默认开的候选在本席隔离验证后才交件，未改任何在线开关。既有SH_CODEX_BIN／PATH默认解析，包装器不猜厂商子路径；可用SH_CODEX_DAEMON_EXE钉经核实际原生Codex，错配拒绝，升级前必须核路径与SHA。可执行文件身份是可信安装路径的inode比对，不冒充软件供应链签名／远端认证。

**必须保留的限域：** 当前生产 `_start_daemon` 仍只有daemon-start命令rc，没有本组认可的PID输出合同，故装配实际为uid_exe_only；不能区分同UID同exe两个daemon，也不能证明其账户上下文一致。未把启动命令PID、PPID1或首次peer冒充自启所有权。OwnedDaemonIdentity是可信启动者持有句柄的可选接口，PID与starttime同时硬比；当前driver_factory不传，此分支在本组真实Popen前台listener上验证，不夸称生产daemon-start已获取PID。原HOME控制socket路径及CODEX_HOME差异未改，不靠本件宣称该旧差异消失。

## 冻结源门

最终以CHECKS.json、raw/与从Git复核的VERIFY为准：helper全套826项、无跳过、rc0（隔离HOME／源码PYTHONPATH／既有锁定venv，423.806秒）；canonical BUILD_TESTS_OK python=58 shell=2 rc0；vet rc0。全仓 Go 最终 rc0：66 包，53 测试包通过、13 包无测试、0 失败；另有 31 个条件性单测跳过，逐项原文与名单见 GO-SUMMARY.json 及无损压缩的原始 JSON 日志。严格 embedded PG、skip_ok=false、无外部 DSN，跨 helper 格使用锁定 venv；不把条件跳过宣称为全局 skip=0。版本与运行时锁均不改（锁SHA5ef8d581）；venv来自本席既有 `/tmp/coder-0232-locked-venv`，包版本与主机／UID在ENVIRONMENT.json，未新增pip安装或依赖。

新增16项单测含真实AF_UNIX和kernel peer数据，覆UID／exe／proc故障注入、starttime解析与PID重用、可信绑定硬比、每次重连重新核验、HTTP前零字节、显式FD关闭（保留对象排除GC隐式清理假阳性）、错误事件、投递失败持久化及不重试、默认／关闭／非法开关、direct/proxy保持合同。异UID及proc权限故障属于精确注入，不冒称实际换UID验证。

21项撤修、44子命令：每项必须命中指定AssertionError红（不接受语法／导入异常），恢复后绿，首尾完整16项基线绿。包含UID、exe inode、readlink、deleted、starttime变化、owned PID／starttime、平台／stat解析、漏接身份门、将身份门移到HTTP之后、漏显式close、残留RPC、漏接owned绑定、重连漏核、默认关、非法值放行、错误分类、事件不可见、账本漏写与重复连接。原stdout/stderr/rc与命令／UTC全部保留。

## 锁定版本真实实测

官方0.161.0与0.163.0二进制复用前序Git provenance已核本地包，分别再次核292309256／296865480字节及SHA256（9a820c17…／399ff69c…），不用PATH上的Codex。原供应链记录为官方npm包SHA／integrity核对，不将已有registry签名记录说成独立密码学签名验过；本件不重新下载、更不打包二进制入Git。

每版5格，raw/各存原生stdout/stderr、source＋实际三输入SHA的PROVENANCE、RESULT：可信Popen PID＋starttime正例及同实例重连（2次initialize）、UID＋exe弱绑定正例、错exe、错starttime、同exe新PID替换后旧绑定拒。后3格均failed/session_unavailable，RPC方法零次，不建thread。用的是本脚本持有句柄的前台 `app-server --listen unix://PATH`，不把它冒充生产daemon-start接口。仅initialize、0模型turn；HOME/CODEX_HOME私有、凭据环境不继承、模型provider为本地拒绝端点；两进程均由本脚本Popen句柄终止并wait回收，未扫描或影响现有daemon。

preliminary/保留两版首次开发阶段探测原件：当时Git HEAD只为设计锚、实现工作区尚未冻结，不能将其source字段冒充已冻结实现；不用于最终候选准入。冻结源重新完整运行了两版正式门并记录实际输入SHA。开发中一次readlink故障夹具最初误拦了预期Python符号链接解析，收窄为只拦/proc/PID/exe后16项通过；该次仅工具会话捕获，未另落原始文件，不伪造缺失原件、不计正式门失败。

## 交付／回滚与集成提醒

运行说明 `docs/runbooks/helper-daemon-identity.md`。只在coder56solzzc本席私有环境验证，没有改任何在线配置、helper、Hub或席位，不部署。将来本组升级须先核原生exe、UID/proc可见性、检查scope，再按组长窗口逐席；未知实例不杀、不清thread账本，不以关门冒充验收。组外按第95条保持不动。

本分支自r36只加一个canonical入口，计数58；r37如同时合入MCP-SLOT-PIN须调整为59并同步deploy入口计数断言。VERSION仍0.2.3.9，正式升级版本位由集成／发布令另定。

Source manifest锁全部非docs、Scope核限定差集、Doc manifest核设计／运行说明；证据manifest锁全部原件，Go日志压缩时另记原始大小／SHA并由VERIFY解压重验。真实Hub/View／现场双daemon根因没有由本件确认，不作部署或跨席位验收声明。
