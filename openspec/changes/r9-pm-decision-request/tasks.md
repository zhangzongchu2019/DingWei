# Tasks

S-R10-R9-PROPOSALS第二阶段R9；S-R51-R9-IMPL执行者togpt60lunazzc.zzc。R10原提案独立冻结；PM01～03已裁定。本实现只覆盖R9首期接口与假provider/隔离DB验证，不含页面、真实飞书或MR操作。

## 1. 提案与冻结

- [x] T-01 读取固定来源6a2048187be7660f1c56a85d8ad79c743702d250的R9/11月第3节五条/委员会第8条；核既有私聊/旧澄清/R5只读/R8事实输入，补最小主规格/context、R9-001～009增量、design与前置/PM选项。
- [x] T-02 OpenSpec1.16.0/Node≥22.12/telemetry0单变更与全仓strict rc0；核仅R9文档新增、R10与产品字节不变，沿用同基线本任务已通过仓库门记录，推工蜂同分支后核tip并独立冻结交审。

## 2. 前置与实施（待另单）

- [x] T-03 PM01～03已定；R9-B01～03基线技术事实已核并列明文件:行。三项均仅部分具备，缺口另待实现任务解决；勾选仅代表事实核实完成，不表示R9完整就绪或授权。
  - [x] T-03.1 独立核0659d208第㉒条并回写：PM01原A只在平台记录、MR后置且不新申请写权限；PM02首轮24小时后每6小时催、超时只停住；PM03每点一PM、多人拆点。原选项保留、编号直接对应。
  - [x] T-03.2 已按r48基线完成R9-A01/02/04身份映射、可信私聊入站、幂等/outbox/周期设施及A03写回边界事实盘点，判定与逐项文件行号见context.md及design.md；现有能力仅部分具备，缺口须在实现前补齐。MR评论写权限/真实写回按已定PM01后置且不新申请。此勾选只表示前置核实完成，不代表R9完整就绪；T-04起实现仍另派。
- [x] T-04 实施R9-001/002固定点集合、双渠道同事务投递与真实状态；覆盖不完整决策点、双渠道投递和持久请求，并运行目标变异。
- [x] T-05 实施R9-003/004明确回应、身份/关联、最先有效CAS及关闭另一待办；覆盖Console三种意见、可信私聊、错误身份/来源/旧关联、竞争回应和非R9输入放行，并运行目标变异。
- [x] T-06 实施R9-005/006内容指纹/映射代际失效重发、超时/SPEC_REVIEW及周期去重；覆盖字段变化、未配置PM、映射更新、沉默/催办周期和状态恢复，并运行目标变异。
- [x] T-07 实施PM01裁定的平台记录与MR后置状态；不申请评论写权限、不写MR、不做真实评论。
- [x] T-08 实施R9-008/009持久幂等、重启/回执unknown、管理员权限与审计；覆盖重复/重启、当前ACL及迁移账本反例，并运行目标变异。
- [x] T-09 执行design九行正反格及目标变异，并运行构建、包测试、race分片、PG strict与OpenSpec strict；结果、跳过原因及提交坐标见S-R51-R9-IMPL交接记录。独立验收未勾；真实provider闭环未执行。
- [ ] T-10 验收后另申请预发/生产GO与切换就绪结论；群通知保持挂起不依赖，不新接Slack、不archive/apply、不改R10/r46冻结分支。 【乙；交接：缺验收后预发/生产GO与切换就绪结论；由独立QA出验收，业主/PM批GO，prodops记录发布事实。】

## 交接记录

- 2026-11-01T14:50:22Z · coder56solzzc.zzc · 内容commit a4adbb68e84cadb12266fb40e63e751ee550f71a；本条记录为追加，最终冻结tip见交件。
  - 完成：T-01/02，R9九条需求/十八个正反场景、最小主规格/context及提案/设计/任务；OpenSpec1.16.0/Node22.23.1/telemetry0，单变更strict与全仓strict rc0，全仓16/16。本单此前R10阶段scripts/run-build-tests.sh已rc0（helper两套各1092、Python27/shell2）；R9仅新增7个OpenSpec文档，产品/脚本未变，因此沿用该门记录不冒称重跑。R10两目录与afe4681da9f4b1ee4807939acc25bf79618b3b70逐字节一致。没有发送真实PM请求、评论MR或执行生命周期场景。
  - 阻塞：R9-A01～04/PM01～03待核或待决，重点是现有只读令牌无comment-write前置、私聊可信身份/原消息关联与周期策略。首期平台写回/MR后置仅建议，未获业主阶段批准不能据此宣称完整就绪；十八个新产品场景和变异均未执行。
  - 下一步：推同一工蜂分支并核远端后冻结R9独立交审，R10继续用原固定tip审阅；先核依赖/PM再另派实现，不动r46或其它已冻结分支，不部署/提升权限/apply/archive。

- 2026-11-01T23:19:36.535290+00:00 · coder56solzzc.zzc · S-R48-PM-WRITEBACK，追加起点commit 0b707acad08ebcf8c05ec0172c14b7f276f94794；最终tip/tree见交件消息。
  - 完成：独立核0659d208第㉒条，R9-PM01选择原A平台记录/MR后置且不新申请写权限、PM02首轮24小时后每6小时催且超时不放行、PM03每点唯一负责PM/多人拆点；原3行选项/建议保留，编号直接对应无不匹配。T-03.1 PM子项闭合，实际身份/可信私聊/幂等技术前置仍待核；MR权限后置不作首期申请项，未指定未经核验的具体PM身份。
  - 验证：本分支本轮独立bash scripts/run-build-tests.sh 2026-11-01T23:10:21.833178+00:00～2026-11-01T23:19:16.091331+00:00 rc0，helper claude 1092/1092、codex 1092/1092、inventory90、Python27组/shell2组；普通dev、锁venv、独立临时HOME/环境白名单，无继承SH/DINGWEI/DW凭据。日志/tmp/r48-pm-r10r9-build.log，SHA256 6e6a082ababe03706834b20c42c618a898cf3eb42f3ecbb9df76939ffd7fd994。全仓OpenSpec1.16.0/Node22.23.1/telemetry0 strict rc0（提交前最终文档复核），diff-check和目录外0改动检查；旧门通过不代表新产品场景已执行。
  - 阻塞：本单PM编号对应无阻塞；原待PM记录为历史，最新㉒已定表覆盖当前口径。实现/逐格反向验证/真实运行前置仍另派，不将PM批准或T-03勾选当上线验收。
  - 下一步：仅相应变更目录文档追加提交、普通push核ls-remote后冻结；按组长顺序等待Hub停机修复→R10→R9另单实现，均随r48。本单不实现、不apply/archive、不申请新写权限、不做部署。

- 2026-11-02T11:19:04Z · togpt60lunazzc.zzc · 技术前置复核内容commit 7d0d8ab6ca762e48a48ec6ea5cfdfa7738eb9582。
  - 完成：按r48基线核R9-B01～03，三项均部分具备，证据见context.md；既有澄清身份链、outbox与周期器不等同于R9决策闭环。OpenSpec全仓strict 16/16、scripts/run-build-tests.sh在HUB_TEST_PG_STRICT=1下rc0；仅更新本变更文档。
  - 边界：技术前置事实核实完成，不代表R9实现就绪或授权；真实飞书身份、私聊与周期场景未执行。

## S-R49-R10-R9-DESIGN 交接（2026-11-02）

本次只冻结R9实施设计，不改变T-04～T-10实现勾选状态。已明确每point的权威字段、Console/Feishu身份映射、native私聊provenance与原消息/request代际校验，提出0074持久表、事件/请求/24小时语义去重、同事务双渠道outbox及重启恢复周期。验收计划9行正反共18格，每格有测试层和目标变异，竞争/重启/去重含50轮与race；仅假provider/隔离DB/fake Feishu，不做真实外部操作。PM责任映射的可信配置者/导入来源待组长定；该条件明确前不实施身份填充或真实请求。


## S-R51-R9-IMPL 交接（2026-11-04）

实现与自测完成，独立验收留待组长。源码基线r50 eaf8f01c，先普通合入冻结设计e229c671，再合入R51测试修正76c72fbe；实现未改R10/R15变更目录、既有非R9路由权限/回执语义、页面、版本号或PG表。只用隔离SQLite和假provider；未探测真实飞书。

- PM权威映射只来自0074 `r9_pm_responsibility`，无自动导入；`internal/m8/r9_pm_mapping.go` 提供受平台管理员保护的单行读写删，持久层递增mapping generation并记录操作者/时间。缺少映射不发请求并停于SPEC_REVIEW；更改映射使旧代际失效并按新PM重发。
- 请求事务同时写Console与Feishu待投递行；可信回应限`feishu_ws_authenticated`/`webhook_verified`并绑定请求、身份、父消息及代际，CAS先到先得并关闭另一待办。FCS_RPC_URL模式拒绝Feishu回应。Console、受信私聊和既有事件分发的非R9输入均有测试。
- 仅精确开关值`DW_PM_DECISION_REQUEST_ENABLED=1`开启R9 handler与周期器；默认关闭时不触碰R9表。0074仅增表/索引，并登记sqlite完整DDL预检名单。隔离二进制证据：r50库72行升级至73，旧72行逐项未变；已有0074对象但缺ledger时补账；部分对象拒绝；r50二进制在73行账本上healthz 200且账本不变。
- 自测包括九项正反语义矩阵及目标变异、权限/FCS/开关变异、迁移三格与重启/去重等。详细命令、UTC、rc、SKIP逐项原因、日志末行、binary SHA及分支坐标见独立docs证据提交。勾选只记录执行者实现/自测，不代表独立验收或GO。

### 本次清账交接（2026-11-08）
- T-10：缺验收后预发/生产GO与切换就绪结论；由独立QA出验收，业主/PM批GO，prodops记录发布事实。 证据边界：docs/reviews/r51-r9-impl-20261104/EVIDENCE.md @ review/togpt60lunazzc/r51-r9-impl-evidence-20261104 92be585f; docs/reviews/prodops-r53-prod-closeout-20261107/RESULT.md @ review/prodops/r53-prod-closeout-20261107 2965ace9。

### 本次清账交接（2026-11-08）
- T-10：缺验收后预发/生产GO与切换就绪结论；由独立QA出验收，业主/PM批GO，prodops记录发布事实。 证据边界：docs/reviews/r51-r9-impl-20261104/EVIDENCE.md @ review/togpt60lunazzc/r51-r9-impl-evidence-20261104 92be585f; docs/reviews/prodops-r53-prod-closeout-20261107/RESULT.md @ review/prodops/r53-prod-closeout-20261107 2965ace9。
