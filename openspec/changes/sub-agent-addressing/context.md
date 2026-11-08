# 第一期设计上下文

基线：opus50tc2k/r54-integration / 6e2c836491f8ba506833d6f849fbe73530e13f92。
规则/提案来源：opus50tc2k/sub-lane-routing-proposal / b33f9ae6108e1e86778aee5e4040d3847f06dce8。docs/design/SUB-LANE-ROUTING-PROPOSAL.md 保留原文；delta spec 补齐场景、结束态、历史 external 兼容与一期设计约束，AA-001～006 编号不变，新增 AA-007～010 待复核，不冒称原文已含这些细节。
本次派单：2026-11-08T12:10:21Z S-SUBAGENT-ADDRESSING-DESIGN；rulings〔51〕及补1/补2，最新补4冻结r55。设计正文 docs/design/SUBAGENT-ADDRESSING-PHASE1.md；证据 docs/reviews/subagent-addressing-design/REVIEW.md。
前单r55：3dc5cd77f683bd21653955c76f97647ce78034f3，后续实现需先整合其driver，不在本设计分支移植产品代码。

环境：普通用户uid1001；Python锁依赖 /tmp/coder-0232-locked-venv/bin；OpenSpec1.16.0 /tmp/coder56solzzc-openspec-1.16.0/node_modules/.bin/openspec，Node>=22.12。native二进制0.159.2 SHA256=1748767b230ebfc3d4ab7e4e254920d0c0ad9691fd8c11f190e7d44511a4a92e。Go overlay复现独立httptest；仅该overlay测试关闭vet（虚拟测试文件不存在于磁盘），canonical不变。native假SSE仅loopback，无真实推理/凭据/在线进程操作。
