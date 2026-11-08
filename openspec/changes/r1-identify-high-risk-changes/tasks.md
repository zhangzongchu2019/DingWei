# Tasks

仓库wego-ai/dingwei；变更r1-identify-high-risk-changes；执行者coder56solzzc.zzc单写，起点dfc95fd8。本单只写规格，实施须另派；分支opus50tc2k/openspec-r1-r8，禁force。

## 1. 规格交审

- [x] T-01 核固定来源、代码最小基线/context，建立R1-001～008及逐条正反场景，明确提供方、非阻塞和R9边界。
- [x] T-02 用OpenSpec1.16.0/Node>=22.12/遥测关闭执行validate --all --strict --no-interactive；核仅OpenSpec文档，记录tip/tree与读数后冻结交审。

## 2. 依赖冻结（未开始）

- [ ] T-03 按已决R1-PM01 A核R1-A01～04，写回匹配语义/规则schema与合并方式、负责人/ACL、两种提供方diff/评论/合并接口与凭据管理位置；核派单后变化并strict，未知前不实施或开启评论。 【乙；交接：真实 GitHub/工蜂 ACL、审阅/评论/合并接口及凭据位置未核；由 prodops/提供方负责人补事实后更新规格。】

## 3. 后续实现（另行派单）

- [x] T-04 实现R1-001/002纯函数与规则版本；对S01/S02的七类×三路21格正例、21格近似反例、多路并集、仅新增行、截断/未知和重放逐格验证，补漏报/误报变异，不把默认表草案冒作运行事实。 【甲；证据：docs/reviews/r1-r8-core/REPORT.md @ 397fafb1c307f624742e4bf477c0ad68d40a2395】
- [ ] T-05 实现R1-003/004提供方和双端投递；验证各S01/S02的GitHub/工蜂、越权/撤权/缺scope、错误坐标、部分失败、查回去重、无源码/秘密外发，记录实际接口及secret位置。 【乙；交接：GitHub 与工蜂仍为 ErrUnavailable stub，真实权限及双端投递未接；由实现席按提供方事实接线并独立验证。】
- [x] T-06 实现R1-005/006知悉、合并时刻和追加commit；按各S01/S02验证负责人冒认/ACK、时刻不混淆、合并后追加命中仍双端再提醒、无命中不提醒及同输入重试幂等；另按R1-006-S03～S05验证同commit标签/规则变化更新原提醒、修订可追溯、集合变化重置/集合未变不重置、归零保留历史、重放/迟到旧知悉/取件失败及双端部分失败。 【甲；证据：docs/reviews/r1-r8-core/REPORT.md @ 397fafb1c307f624742e4bf477c0ad68d40a2395】
- [x] T-07 实现R1-007/008界面与边界；按各S01/S02验证风险/未知/投递失败均不新增合并门，知悉不替PM/委员会确认，R9未就绪不假闭环。 【甲；证据：docs/reviews/r1-r8-core/REPORT.md @ 397fafb1c307f624742e4bf477c0ad68d40a2395】

## 4. 独立验收

- [ ] T-08 按全部R1场景及其格逐一记PASS/FAIL/未执行/不适用和证据，重点用隔离MR实测双端而非仅mock；QA执行独立验收，获预发授权后才验证环境并另申GO。 【乙；交接：缺真实隔离 MR 双端验收与独立 QA；由 QA 用真实受控仓库完成，获得授权后再申预发GO。】

OpenSpec status 的 planningComplete/isComplete 仅表示规划文件齐备，不表示产品场景执行、业务验收、归档或发布。后续实现必须按新冻结commit读取，并在变更被修改时先同步再继续。

## 交接记录
- 2026-11-01T03:14:18Z · coder56solzzc.zzc · content commit 098b773bd1016d1777de4e4dd1d079198a020b8e
  - 完成：T-01、T-02；依据固定决议 c2d9c9fd、草案/月计划6a204818与验收计划a433fc71，在dfc95fd8之上形成两能力最小基线/context、两变更proposal/design/增量/tasks；新需求17条、增量场景34个标题，逐条有正反格。OpenSpec1.16.0/Node22.23.1/OPENSPEC_TELEMETRY=0，validate --all --strict --no-interactive rc0，12项通过/0失败；全部需求正文<=500字、spec<30KiB，仅14个OpenSpec文件。最终tip/tree以承载本记录的提交为准。
  - 阻塞：组长独立审阅、R1-A01～04/R1-PM01和R8-A01～05的真实提供方/规则/身份/审批映射/证据/发布事实；T-03～08均未开始，产品场景未执行；strict不等于验收或发布。
  - 下一步：普通push新分支opus50tc2k/openspec-r1-r8，核远端后冻结交审；先写回依赖与决策再另派实施，不改产品代码、不实施R9、不迁移托管或部署重启。

- 2026-11-01T04:10:08Z · coder56solzzc.zzc · R1-PM01决策回写（最终tip/tree以本记录提交为准）
  - 来源：组长opus50tc2k.zzc转达业主workpulse对话2026-11-01T04:0xZ批准A；原时间范围照录，不补造分钟。proposal标已决，同步R1-004/005/006、design与context；新增R1-006-S03～S05，覆盖标签/规则变化、集合变更与不变、重放、历史知悉、归零、迟到旧请求和不完整判定。R1增量19场景标题，两变更合计17需求/41场景标题。
  - 校验：OpenSpec1.16.0、Node22.23.1，OPENSPEC_TELEMETRY=0 openspec validate --all --strict --no-interactive，12通过/0失败；git diff --check通过，R1需求正文均<=500字；仅5个R1 OpenSpec Markdown文件。
  - 剩余：R1-A01～04与真实提供方接口仍待核；本次不代表T-03整项完成，T-04～08未执行，产品场景未执行。R1／R8实现仍排r45并另派；R8文件不变，无迁移、部署、重启或真实评论。过渡期只推工蜂。

### 本次清账交接（2026-11-08）
- T-03：真实 GitHub/工蜂 ACL、审阅/评论/合并接口及凭据位置未核；由 prodops/提供方负责人补事实后更新规格。 证据边界：docs/reviews/r1-r8-core/REPORT.md @ 397fafb1c307f624742e4bf477c0ad68d40a2395。
- T-05：GitHub 与工蜂仍为 ErrUnavailable stub，真实权限及双端投递未接；由实现席按提供方事实接线并独立验证。 证据边界：docs/reviews/r1-r8-core/REPORT.md @ 397fafb1c307f624742e4bf477c0ad68d40a2395。
- T-08：缺真实隔离 MR 双端验收与独立 QA；由 QA 用真实受控仓库完成，获得授权后再申预发GO。 证据边界：docs/reviews/r1-r8-core/REPORT.md @ 397fafb1c307f624742e4bf477c0ad68d40a2395。
