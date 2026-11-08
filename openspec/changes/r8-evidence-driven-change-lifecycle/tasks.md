# Tasks

仓库wego-ai/dingwei；变更r8-evidence-driven-change-lifecycle；执行者coder56solzzc.zzc单写，起点dfc95fd8。本单只写规格，实现排r45且待R6提供方接口确定后另派；分支opus50tc2k/openspec-r1-r8，禁force。

## 1. 规格交审

- [x] T-01 核固定决议/月计划/验收计划与真实代码，建立最小基线/context、R8-001～009、各步守卫表及正反场景。
- [x] T-02 用OpenSpec1.16.0/Node>=22.12/遥测关闭执行validate --all --strict --no-interactive；核仅OpenSpec文档，记录tip/tree与读数后冻结交审。

## 2. 依赖冻结（未开始）

- [ ] T-03 按已决R8-A05核R8-A01～04：GitHub/工蜂当前head/所需审阅集合/批准有效性及显式委员会标记字段、可信推进/结果/QA/发布主体、MR/PM及委员会范围按需R7完整当前事实、任务/场景集合与适用映射、发布关联、事务与幂等；补实际代码位置/接口和secret管理位置，核派单后规格并strict，依赖未知前不开放推进。 【乙；交接：真实 GitHub/工蜂有效审批、PM/委员会字段、可信主体与secret位置未核；prodops/提供方负责人补事实。】

## 3. 后续实现（另行派单）

- [x] T-04 实现R8-001/002/007的合法边、普通MR当前head所需批准+同SHA全部PM回应、显式委员会范围附加同SHA R7、幂等和提交重核；按R8-002-S01～S06及其它S01/S02逐格验证普通无Console可过、任一正向标记均需R7、风险/作者不推断类别、旧批准/撤回/漏人/未知集合/模糊或过期PM不放行、head/类别竞争重核。 【甲；证据：docs/reviews/r1-r8-core/REPORT.md @ 397fafb1c307f624742e4bf477c0ad68d40a2395】
- [x] T-05 实现R8-003/004/006的完整任务/交接、Scenario四态与追加修正；按S01/S02验证全勾缺交接、集合缺页、FAIL、缺证据/理由、错编号/身份/版本不放行，带理由未执行可归档但不显示全PASS。 【甲；证据：docs/reviews/r1-r8-core/REPORT.md @ 397fafb1c307f624742e4bf477c0ad68d40a2395】
- [x] T-06 实现R8-005/008的发布事实与工具动作分离；按S01/S02验证release_id/环境/成功覆盖依据缺一不可，预发不冒充生产，tasks/strict/CLI archive不洗绿FAIL/未执行，也不造发布。 【甲；证据：docs/reviews/r1-r8-core/REPORT.md @ 397fafb1c307f624742e4bf477c0ad68d40a2395】
- [x] T-07 实现R8-009及各守卫的Console/审计/权限；按S01/S02验证全长SHA、证据/环境/阻塞可定位、撤权缓存/跨提供方不泄漏、审计失败状态不分裂，并同步context实际路径。 【甲；证据：docs/reviews/r1-r8-core/REPORT.md @ 397fafb1c307f624742e4bf477c0ad68d40a2395】

## 4. 独立验收

- [ ] T-08 对R8全场景及各格记录PASS/FAIL/未执行/不适用和证据；用真实受控事实和隔离数据逐边验收，QA独立复核，不以stub通过代替R7/R9/提供方可用证明；另获预发授权再验证环境与申请GO。 【乙；交接：缺真实提供方受控事实及独立QA/预发授权；由 QA 与 owner 完成，不能以stub测试代替。】

OpenSpec status 的 planningComplete/isComplete 仅表示规划文件齐备，不表示产品场景执行、业务验收、归档或发布。后续实现必须按新冻结commit读取，并在变更被修改时先同步再继续。

## 交接记录
- 2026-11-01T03:14:18Z · coder56solzzc.zzc · content commit 098b773bd1016d1777de4e4dd1d079198a020b8e
  - 完成：T-01、T-02；依据固定决议 c2d9c9fd、草案/月计划6a204818与验收计划a433fc71，在dfc95fd8之上形成两能力最小基线/context、两变更proposal/design/增量/tasks；新需求17条、增量场景34个标题，逐条有正反格。OpenSpec1.16.0/Node22.23.1/OPENSPEC_TELEMETRY=0，validate --all --strict --no-interactive rc0，12项通过/0失败；全部需求正文<=500字、spec<30KiB，仅14个OpenSpec文件。最终tip/tree以承载本记录的提交为准。
  - 阻塞：组长独立审阅、R1-A01～04/R1-PM01和R8-A01～05的真实提供方/规则/身份/审批映射/证据/发布事实；T-03～08均未开始，产品场景未执行；strict不等于验收或发布。
  - 下一步：普通push新分支opus50tc2k/openspec-r1-r8，核远端后冻结交审；先写回依赖与决策再另派实施，不改产品代码、不实施R9、不迁移托管或部署重启。

- 2026-11-01T03:19:18Z · coder56solzzc.zzc · content commit fb0301548e96d993d3ccda4fcc74c0bb41e6433b
  - 完成：按平台架构组/组长2026-11-01T03:1xZ裁定关闭R8-A05；R8-002改为普通MR当前head所需批准＋同SHA全部PM明确回应，任一人工显式委员会标记再加同SHA R7确认；同步R8-007/009、proposal/design/tasks/context，R8-002保留S01/S02并新增S03～S06。R8仍9条需求、22个场景标题，R1/R8合计17条/38个增量场景标题。strict12/12、rc0、diff --check通过，修改仅5个R8 Markdown，R1-PM01及R1全部文件保持不变。
  - 阻塞：R1-PM01仍待业主定；R8-A01～04及GitHub/工蜂当前head/所需审阅/批准/标签/PM接口事实待核，A05不再待决；T-03～08未开始、产品场景未执行，无实现或上线授权。
  - 下一步：普通push追加提交后核远端并冻结交审；实现排r45，待R6提供方接口确定后另派。最终tip/tree以承载本记录的提交为准，不修改其它产品或已交R1规格。

### 本次清账交接（2026-11-08）
- T-03：真实 GitHub/工蜂有效审批、PM/委员会字段、可信主体与secret位置未核；prodops/提供方负责人补事实。 证据边界：docs/reviews/r1-r8-core/REPORT.md @ 397fafb1c307f624742e4bf477c0ad68d40a2395。
- T-08：缺真实提供方受控事实及独立QA/预发授权；由 QA 与 owner 完成，不能以stub测试代替。 证据边界：docs/reviews/r1-r8-core/REPORT.md @ 397fafb1c307f624742e4bf477c0ad68d40a2395。
