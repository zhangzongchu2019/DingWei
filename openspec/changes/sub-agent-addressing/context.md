# 第一期设计上下文

基线：opus50tc2k/r54-integration / 6e2c836491f8ba506833d6f849fbe73530e13f92。
规则/提案来源：opus50tc2k/sub-lane-routing-proposal / b33f9ae6108e1e86778aee5e4040d3847f06dce8。docs/design/SUB-LANE-ROUTING-PROPOSAL.md 保留原文；delta spec 补齐场景、结束态、历史 external 兼容与一期设计约束，AA-001～006 编号不变，新增 AA-007～010 待复核，不冒称原文已含这些细节。
本次派单：2026-11-08T12:10:21Z S-SUBAGENT-ADDRESSING-DESIGN；rulings〔51〕及补1/补2，最新补4冻结r55。设计正文 docs/design/SUBAGENT-ADDRESSING-PHASE1.md；证据 docs/reviews/subagent-addressing-design/REVIEW.md。
前单r55：3dc5cd77f683bd21653955c76f97647ce78034f3，后续实现需先整合其driver，不在本设计分支移植产品代码。

环境：普通用户uid1001；Python锁依赖 /tmp/coder-0232-locked-venv/bin；OpenSpec1.16.0 /tmp/coder56solzzc-openspec-1.16.0/node_modules/.bin/openspec，Node>=22.12。native二进制0.159.2 SHA256=1748767b230ebfc3d4ab7e4e254920d0c0ad9691fd8c11f190e7d44511a4a92e。Go overlay复现独立httptest；仅该overlay测试关闭vet（虚拟测试文件不存在于磁盘），canonical不变。native假SSE仅loopback，无真实推理/凭据/在线进程操作。

复核〔51〕-补5（review/opus50tc2k/rulings ad1a1ee0）通过并冻结bbfbf95b；追加三项修订只改设计/规格与证据。旧输入提案SUB-LANE-ROUTING-PROPOSAL.md保留历史原文，其前8位命名建议已由本裁定及当前AA006替代。

M1 2026-11-08：基线79a60a1e351c207ff0b46bdf9151e846ec83f5d4；唯一写者线程01a1bd1c，锁/tmp/coder56-subagent-m1.lock；工作树/data/ai-dev-data/subagent-p1-m1-hub。代码位置：internal/subagent纯结构与解析；internal/m8/sub_agent.go当前连接协商、控制帧及路由；internal/store/sub_agent.go持久注册和sidecar；migrations/0075_sub_agent_registry.sql加法迁移。实现无新增第三方依赖。验证用隔离HOME/TMPDIR、剥离SH_/DINGWEI_/DW_/DATAD_，沿用锁定Python环境；不读取真实席位数据库、不调用在线helper。新回退开关DW_SUB_AGENTS_V1=0；M2/M3仍另派。报告入口docs/reviews/subagent-p1-m1-hub/REPORT.md。
