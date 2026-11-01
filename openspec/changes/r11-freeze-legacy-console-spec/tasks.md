# Tasks

本文件仅记录本变更；派单坐标必须含仓库、变更 ID、T 编号与 commit。执行者 coder56solzzc.zzc。实现任务在规格审过、待决项写回并获派单前保持未开始。

## 1. 规格就绪

- [x] T-01 依据固定来源和 a8bb5e58 代码建立最小基线/context、proposal、固定编号增量与设计；核查来源链接、代码路径和场景对应关系。
- [x] T-02 使用 OpenSpec 1.16.0、Node 22、OPENSPEC_TELEMETRY=0 完成 strict 校验并冻结 Git 坐标；以校验 rc、固定 tip/tree 和交接记录验证交付。

## 2. 实施前冻结

- [ ] T-03 等组长审阅并确认 R11-A01～03、R11-PM01～03；写回 proposal/design，补逐路由效果与在途清单及并发生效机制；复跑 strict 并核对派单后的规格是否变化。未决前不得进入 T-04。

## 3. 冻结与查询实现（待授权）

- [ ] T-04 落实 LCS-001/003/005 的统一服务端拒写，覆盖 Console、shared、Agent、草稿、后台重试和迟到结果；同步写路由/存储回归与并发冻结测试，确认无新修订/门写入且拒绝不报成功，并更新 context 中实际代码位置。
- [ ] T-05 落实 LCS-002 的普通入口移除、独立历史查询和只读深链接；同步浏览器/越权/分页与归档记录用例，确认不扩大权限且不展示编辑控件，更新查询说明。
- [ ] T-06 落实 LCS-004/005 的历史保留与在途任务隔离；同步 SQLite/PostgreSQL 前后快照比对、任务进度回归与触发器检查，确认旧正文/历史不变、无批量转换，更新依赖说明。

## 4. 集成与证据

- [ ] T-07 按 LCS-S001～010 逐场景记录 PASS/FAIL/未执行/不适用与证据坐标，复跑受影响测试门和 strict，提交干净锚给独立审阅；不得用勾选或结构通过代替业务验收。
- [ ] T-08 在另获预发授权后验证切换与查询发布集，交付生效点/回退边界/历史快照证据并交审；未获 GO 不执行环境变更。

## Workflow follow-up

- 审阅通过后再按已冻结 commit 另行派实现；archive 与 release/部署分别记录，不以归档推断上线。

OpenSpec status 的 planningComplete/isComplete 只说明规划文件齐备，不代表实现、场景执行、业务验收或发布完成。

## 交接记录

- 2026-11-01T01:19:55Z · coder56solzzc.zzc · commit 518d46f61771de06db5832cce99bfc9a29330579
  - 完成：T-01、T-02；基于 a8bb5e5847884b62ba01f6f48fbce3772e2d8b04 与固定来源 c2d9c9fdd0416828739f7042d2ceb9bc71f603c6 形成规格；OpenSpec 1.16.0 / Node 22.23.1 / OPENSPEC_TELEMETRY=0，`openspec validate --all --strict --no-interactive` rc=0，2 个基线与 2 个变更全部通过、零 issue。首次 R11 校验发现场景标题身份不匹配，保留原完整标题后重验通过；config 仅补引号恢复原规格规则。
  - 阻塞：R11-A01～03、R11-PM01～03 与组长独立审阅；场景仍未执行，T-03～08 均未开始；不得从 CLI planningComplete/isComplete 推断业务完成。
  - 下一步：按交件 tip 冻结审阅；先完成 T-03 并回写未决项，再由组长另派实施。当前段只新增本交接记录，未改上述内容 commit 的规格正文；最终冻结 SHA/tree 以承载本记录的提交为准。
