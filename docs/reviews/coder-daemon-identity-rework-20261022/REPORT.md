# DAEMON-IDENTITY：组长两点 REWORK 后继（待审，不部署）

分支 `review/coder/daemon-identity`，追加于首审 `bef62da0a4a2076a4d837827fb75470c8a8ce384`，不改写历史。冻结源 `39985004f56f28100510ba79b98f1443d93a6070`，tree `71fc51bd10ea3f12f67c7f496246749c78815279`。运行时代码仅改 codexDriver.py；SO_PEERCRED、exe设备/inode、starttime与可信owned绑定算法原样保留。VERSION、锁、Hub、UI、接口均不改。

## 1. 三态与观察后再强制

`SH_CODEX_DAEMON_IDENTITY_CHECK`在helper启动时读一次：

| 值 | 核验失败时 | 成功与告警可见性 |
| --- | --- | --- |
| `observe`（未配置的默认） | 不因身份不符／不可证拒连，继续原HTTP Upgrade与initialize | 固定类别warning；初始化后结构化daemon_identity_observed，scope=unverified；不能冒称verified |
| `enforce` | 原拒连合同：HTTP前关FD、session_unavailable、账本保留、不新建thread、不投递／自动重试 | 固定类别拒绝事件；核验通过记实际scope |
| `off` | 不调用身份核验，按原传输连接 | 明示identity_scope=unchecked |

只接受这三个精确小写字符串；旧未发布候选的1／0不作为别名，非法配置拒连。direct/proxy不受本Unix门影响。默认observe也每次重连重核、不沿用旧结果；它不是“保护已生效”的证明。先在本组席位经另令observe收集实际exe形态与scope，修正包装器误报，确认后再逐席另批enforce，不自动升级模式。本件只做隔离验证，未修改任何在线环境或席位。

新增默认observe＋真实exe不符仍初始化、固定warning与结构化unverified事件、不冒充verified的正格；proc不可证时observe继续且重连两次核验；显式enforce保持拒连、零HTTP字节、无RPC／thread与失败账本格。off、非法模式、直接／代理路径原合同继续覆盖。18项定向格；25撤修／52子命令，增加默认observe、observe不拒连、warning、事件与enforce拒连的指定断言红。

## 2. 专属控制目录可行性：可以作为下一件，不能当作当前daemon命令已支持

锁定官方0.161.0／0.163.0（与前序Git provenance相同的二进制SHA，再核字节）帮助均列出 `codex app-server --listen unix://PATH`。两版 `app-server daemon start --help` 未列出独立socket路径参数；`proxy --help`也没有该选项。本次未发现受支持的“现有daemon start只改控制目录”接口；没有穷尽未知环境变量，不把帮助未列出夸成任何内部变量都不存在。最初误试 `app-server start --help` 的rc2原件也保留，正确子命令是app-server daemon start，不能把rc2当支持性证据。

**实证可行的路线：** helper持有独立前台 `app-server --listen unix://<helper私有0700目录>/control.sock` 的Popen句柄；原HOME、CODEX_HOME仍保持原值，仅显式socket参数不同，不通过更换HOME／拆分CODEX_HOME来分离控制口。`PROBE_PRIVATE_LISTENERS.py`在每版各启动两个已持有句柄的listener，使用同一个私有HOME和同一个CODEX_HOME，两条不同私有socket同时初始化成功，SO_PEERCRED PID分别与持有句柄相符，0 thread、0模型turn；结束只terminate／wait自己持有的进程。raw/private-listeners保留结果与清理，未读取、复制或测试真实凭据。本组原五格官方实测同样用显式unix路径，在本后继enforce下再次通过。

这证明锁定版**支持显式listener路径且不要求改变CODEX_HOME**，不证明生产账户上下文、两个服务并行写会话数据库、认证或human attach已验收；不应把前台listener的支持误当作现有daemon start有自定义socket／可靠PID返回合同。[官方App Server文档](https://learn.chatgpt.com/docs/app-server)用于协议背景，具体锁定版路径能力以前述help和二进制隔离读数为据。

建议下一件 `S-HELPER-PRIVATE-CONTROL-ENDPOINT`（本件不实现）：新增独立可选启动模式；目录权限／属主与单实例锁；保留原CODEX_HOME；直接持有Popen PID＋starttime并传入已实现的owned门；socket选择、重连、helper退出与重启恢复／清理均只针对自有实例，不扫描／杀未知进程；不可证不接其他daemon、不新建替代thread。另需验证人类CLI接入、会话恢复与并发库写兼容，不能悄悄以此替代现有managed daemon生命周期。与旧socket路径兼容、迁移及回滚须另审，本组隔离验证后再批在线使用。

## 3. 冻结输入、原始读数与限域

所有正式门结果见CHECKS.json和raw/，源清单锁1882项。Go门实际启动源为57a9d956，之后两次test-only追加仅修撤修器Python字符串引号、将observe warning断言改成显式mock检查（避免assertLogs先以无日志异常抢红）；Go、全部运行时helper、Hub、UI、锁与版本blob均相同，GO-INPUT-EQUIVALENCE.json由Git逐项复核，不把Go日志source伪改为后继。helper与canonical从最终39985004完整另跑；25撤修以最终源运行。此前开发读数原样在preliminary，非最终PASS。

首个新格曾把initialize之后的initialized通知漏写进期望，失败原文保留；修正夹具后18项绿。首轮撤修器有引号语法错误，工具会话原文存在但未另落原始文件；追加test-only修正不改产品。第二轮去warning红在assertLogs的“无日志”而未命中指定标记，partial日志保留；改为显式warning断言后25格均指定AssertionError红，恢复绿。不得将中间轮记成最终撤修通过。

PG环境口径：runner白名单误写的DW_TEST_EMBEDDED_PG_STRICT／DW_TEST_PG_SKIP_OK不是本仓识别键；实际HUB_TEST_PG_STRICT／SKIP_OK／EMBEDDED／DSN均未设。当前pgtest源码默认允许embedded、环境故障失败关闭（只有显式HUB_TEST_PG_SKIP_OK开才可降级），所以这里“严格”指默认严格语义，不声称显式设HUB_TEST_PG_STRICT=1。PG-ENVIRONMENT.json锁定该源码，raw保存实际runner；30条件跳过须逐项核原文。

最终门：helper 828项、无跳过、rc0（隔离HOME＋锁定venv）；canonical BUILD_TESTS_OK python=58 shell=2 rc0；vet rc0。Go全仓rc0：53测试包过、13无测试、0失败，30个条件性单测跳过逐项保留；严格embedded PG、无外部DSN，不能声称全局skip=0。25撤修／52子命令全部指定红／恢复绿；0.161.0与0.163.0各5格enforce真实门通过；专属目录每版2listener同时初始化成功，0 thread／turn。VERIFY复核全部源码、证据、输入等价与压缩日志原SHA。

首次adapter桥尚未启动时，可见性仍为既有启动日志／本地事件缓存，不宣称首次错误已送到Hub View；运行中的桥沿用结构化事件路径。当前生产启动路径无可信自启PID，scope仍仅uid_exe_only；同UID同exe双daemon歧义未凭本件消失。专属listener是下一件方案，不是本次上线内容。

## 4. 交付边界

不部署、不改在线配置、不重启helper／daemon、不接43／dw123，不影响dataConnector或oaagent。组长审＋prodops交叉核；r37如合入MCP-SLOT-PIN，canonical入口仍需汇总为59并同步断言。本分支仍58＋2，VERSION仍0.2.3.9。首审报告及其manifest保留在旧锚，不将历史默认1误作当前定稿。

本轮条件跳过30项（前轮31）：唯一少跳过的是cmd/workpulse TestUpgradeRotationKeepsNewCredentialOnSharedWork；前轮原文为python3无websockets，本轮PATH含锁定venv而实际运行通过，未新增跳过。原始Go JSON保留PASS与日志，不把少一项跳过归因成产品变更。
