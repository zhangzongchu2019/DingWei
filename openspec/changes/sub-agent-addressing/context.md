# 第一期设计上下文

基线：opus50tc2k/r54-integration / 6e2c836491f8ba506833d6f849fbe73530e13f92。
规则/提案来源：opus50tc2k/sub-lane-routing-proposal / b33f9ae6108e1e86778aee5e4040d3847f06dce8。docs/design/SUB-LANE-ROUTING-PROPOSAL.md 保留原文；delta spec 补齐场景、结束态、历史 external 兼容与一期设计约束，AA-001～006 编号不变，新增 AA-007～010 待复核，不冒称原文已含这些细节。
本次派单：2026-11-08T12:10:21Z S-SUBAGENT-ADDRESSING-DESIGN；rulings〔51〕及补1/补2，最新补4冻结r55。设计正文 docs/design/SUBAGENT-ADDRESSING-PHASE1.md；证据 docs/reviews/subagent-addressing-design/REVIEW.md。
前单r55：3dc5cd77f683bd21653955c76f97647ce78034f3，后续实现需先整合其driver，不在本设计分支移植产品代码。

环境：普通用户uid1001；Python锁依赖 /tmp/coder-0232-locked-venv/bin；OpenSpec1.16.0 /tmp/coder56solzzc-openspec-1.16.0/node_modules/.bin/openspec，Node>=22.12。native二进制0.159.2 SHA256=1748767b230ebfc3d4ab7e4e254920d0c0ad9691fd8c11f190e7d44511a4a92e。Go overlay复现独立httptest；仅该overlay测试关闭vet（虚拟测试文件不存在于磁盘），canonical不变。native假SSE仅loopback，无真实推理/凭据/在线进程操作。

复核〔51〕-补5（review/opus50tc2k/rulings ad1a1ee0）通过并冻结bbfbf95b；追加三项修订只改设计/规格与证据。旧输入提案SUB-LANE-ROUTING-PROPOSAL.md保留历史原文，其前8位命名建议已由本裁定及当前AA006替代。

M1 2026-11-08：基线79a60a1e351c207ff0b46bdf9151e846ec83f5d4；唯一写者线程01a1bd1c，锁/tmp/coder56-subagent-m1.lock；工作树/data/ai-dev-data/subagent-p1-m1-hub。代码位置：internal/subagent纯结构与解析；internal/m8/sub_agent.go当前连接协商、控制帧及路由；internal/store/sub_agent.go持久注册和sidecar；migrations/0075_sub_agent_registry.sql加法迁移。实现无新增第三方依赖。验证用隔离HOME/TMPDIR、剥离SH_/DINGWEI_/DW_/DATAD_，沿用锁定Python环境；不读取真实席位数据库、不调用在线helper。新回退开关DW_SUB_AGENTS_V1=0；M2/M3仍另派。报告入口docs/reviews/subagent-p1-m1-hub/REPORT.md。

M2（2026-11-08）：M1获〔51〕-补12，子Agent走r57；基线eae9eca1。helper实现位置tools/sessionhelper/sessionhelper/{app_server_broker,sub_agent_store,sub_agent_runtime,sub_agent_connection}.py；原有CodexDriver/Adapter/inbox加作用域，默认SH_SUB_AGENTS_V1关闭。协议与原生历史主thread限制见docs/design/SUBAGENT-P1-M2-HELPER.md。只用Git交件，本地隔离验证，不部署。

M2收口（2026-11-09）：产品冻结eed6ac54762bb2cf756f78934fbfb2db46f73d64，候选826a7c0ecf3d3b5634f69eed479d6c5abdf3b119；只用cfg.inbox_db本地SQLite加法表/列。实际M1确认不回显lane，由不可变投递作用域侧表定位；源工具调用在发送前持久去重。helper库存92文件，新增29例与24刀；r55九格和21刀继承护栏。所有本地测试使用隔离HOME、短TMPDIR、剥离席位环境，不访问在线数据库；原生仅loopback合成SSE。报告docs/reviews/subagent-p1-m2-helper/REPORT.md。

2026-11-09 〔51〕-补13复核补件，基线d48f3576951a2247233018aab7a091cfc1dfff55，rulings已核ac2bc40e0。沿用同分支与01a1bd1c写者锁。产品仅sub_agent_runtime.py及app_server_broker.py；测试复用test_sub_agent_helper.py，新增ExistingRootToolsTests四例，mutations.py新增ROOT1～ROOT4四刀。新证据单列docs/reviews/subagent-p1-m2-root-tools/，冻结原交件报告原文不改。测试全为临时SQLite/合成原生传输，重开只操作fixture，无真实席位或推理。

M3（2026-11-09）：基线0b47a585，分支coder56solzzc/subagent-p1-m3-view；唯一写者01a1bd1c持/tmp/coder56-subagent-m3.lock。scope实现在internal/m8/sub_agent_view.go，View加法scope/cursor/cost表沿用原投影DB；Hub迁移0076保留回退barrier/逐封隔离。tools/sessionhelper/sub_agent_connection与store支持feature-off只退信，不启动broker。新浏览器专项已登记全量runner；旧Hub/claimer只读源码锚79a60a1e，测试overlay不改旧树。所有测试隔离HOME/TMPDIR并剥离在线环境，不使用真实推理。
