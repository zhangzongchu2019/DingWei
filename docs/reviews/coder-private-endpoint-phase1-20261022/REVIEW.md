# S-HELPER-PRIVATE-CONTROL-ENDPOINT 一期：作者候选交审

作者门已过，提交组长审查与 prodops 交叉核；没有部署、席位修改或集成线推送。默认 legacy 不变。任意席位启用 private 前，必须先闭合后续 **S-PCE-UPDATER-RETAIN**（活 owner 运行包保留与自升级器留槽冲突）；本件不声称已改自升级器或做真实席位升级。

冻结源 `7cfcd9927c5d18048aeb30ee0ab8c27bfb1cff45`，tree `8a7a67519a57b777eb2d12a18b9f8fb90207f7d8`；不可变输入标签 `evidence/coder/private-control-endpoint-8a7a6751`，基 r37 `29e63f9963dedcc32010ca89265d78ba12664bf2`（0.2.3.10）。其后交件提交只增加本目录证据与文档。设计 `dbb7cf0e` 与组长一期／原生别名七硬门裁定，落实于 docs/designs/S-HELPER-PRIVATE-CONTROL-ENDPOINT-PHASE1.md。

变更：独立 owner 自持 Popen、flock、私有根与控制 socket；helper 只复用活 owner，以同 FD 核 UID／预期 exe／自持 PID＋starttime 后才发 HTTP。唯一 helper writer、准入／停止互斥、当前绑定提示单独持久化，fresh helper 读提示后仍须 read/resume 原 ID；历史回执不选或改写当前绑定。child 异常由真实 Popen wait 后有界退避重启，未知投递不自动重放；owner 失联或遗留对象则拒接／拒补启／拒按 PID 文件杀进程。显式 stop 仅在操作人确认人工退出且原生加载线程全部 idle 后停止自持 child；停止迟退不补启。

原生别名：Popen 前 app.sock 必须不存在；仅接受随后自启程序生成的确定别名。readlink 一次，严格等于自算 canonical /tmp/codex-daemon-UID/路径字节SHA256；原生共享 UID 目录非软链、0700，目标本 UID 的 socket0600。连接直接走计算目标，同 FD 核真实 child；解析前后根／alias／directory／target 不漂移，owner 持 O_PATH 句柄防 inode 立即复用。只精确清自己的 inode，不扫描或改其他原生端点。根、锁、registry、控制 socket 仍拒软链，registry 只作 locator。

配套最小修护：UnixWS stop 时捕获 socket 或以 OSError 结束读取，避免已清空对象的后台断言；只对 private connector 清除握手验证的短超时，安静的持久流不自断，legacy 自定义超时语义保留。Adapter 冲突按 private 配置与补显开关并集解决；canonical 新增私有端点测试并同步60项合同，业务接口／授权／Hub协议／运行时依赖锁未改。

| 冻结输入上的实际门 | 读数 |
| --- | --- |
| vet、全仓 Go（Go1.22.2、严格内嵌PG、DSN未设） | rc0；53测试包通过、13无测试文件，30个既有条件SKIP逐项在 GO-SUMMARY.json |
| helper 全套（每命令独占HOME、锁定venv） | rc0，891项OK |
| canonical | BUILD_TESTS_OK python=60 shell=2，rc0 |
| 本地42项与指定撤修 | 49刀全红、恢复绿，100次命令；不是原生版本验收的替代 |
| 官方0.161.0与0.163.0 | 两版真实owner、竞争首启一owner一child、fresh helper-driver进程复用、pidfd故障注入后同ID恢复、受控入口真实PTY与正常退出、空闲精确清理均rc0 |
| 相关真实浏览器夹具 | model-admission、mention-reply、backfill均rc0 |

原始命令、环境键名、独立HOME／TMPDIR、UTC、stdout／stderr／rc在 `final-7cfcd99`。原生准备仅向私有配置中不可达的127.0.0.1:9 provider提交一次本地turn以物化rollout；没有真实凭据或模型推理。人为故障只用绑定自建child的pidfd；owner正常清理均无兜底强杀。PTY通过受控CLI核owner／当前ID／app FD后exec原生resume，记录TUI确实存活、同ID且退出0；初始Resuming画面不是成功证据。fresh helper-driver是独立客户端进程，不是真实Hub席位或Console升级。

前序失败原样保留：95becaa入口清单引用中文Git路径错误，尚未跑门；d624首轮canonical的旧0.2秒候选marker未落、helper10格新增mock目标过长；b4轮helper891过，但canonical计数遗漏、Go唯一notify.sock首次bind路径恰108字节。各原因与修正见对应OUTCOME。没有改旧hung-candidate格／guard、产品长路径门或Go测试；最终重新完整跑全部要求门，不用前轮局部绿覆盖红。开发早期空线程缺rollout、等待本地turn及夹具错误也留 development 限域记录。

官方来源与对象类型订正见 OFFICIAL-SOURCES.json：0.163.0的129b8ec7是annotated tag，peeled commit为83086188；0.161.0 peeled为97901140。两版路径函数／目录合同相同，transport全文件的startup lock生命周期有差异，本件不动原生lock文件。所有二进制只在本地，未入Git、未上传。

限域：Linux，private须显式选择并要求身份enforce，启动命令须能绑定实际原生Popen PID（SH_CODEX_BIN可指定已核原生程序）；不猜包装器后代PID。不自动启用、不做透明热切；人工lease心跳、owner失联恢复单属二期。运行owner的包与解释器必须保留，启用阻断S-PCE-UPDATER-RETAIN未闭。本件不改变组外helper，未连43／dw123或修改防火墙、Hub、在线会话。

证据自检：VERIFY.py核1893项冻结输入、Git blob／大小／SHA，以及本目录证据清单；作者测试通过不代替独立验收。生成UTC 2026-10-22T21:35:41Z。
