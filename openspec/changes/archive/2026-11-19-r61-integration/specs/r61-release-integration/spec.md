# R61阶段集成

## Purpose
本变更把冻结单项的独立读数转为固定合并源上的阶段验证，目的是让顺序、冲突处置、异常尝试和环境条件可独立核查。涉及集成分支与测试证据，不影响单项产品合同，也不授予版本抬升、现网或预发操作权限。

## ADDED Requirements

### Requirement: R61I-001 冻结顺序与可追溯合并
集成者 MUST 从批准main基点按已冻结七项顺序各建一个no-ff合并提交，每次合并前 SHALL 核远端tip全值，偏离派单立即停报，不自动选择浮动新锚。

#### Scenario: R61I-S001 七项按序合并
- **WHEN** 七项远端tip均与派单相同
- **THEN** first-parent保留七个顺序明确、各含对应来源父节点的merge，记录核验UTC与冲突处置。

### Requirement: R61I-002 浏览器刀区语义保留
相邻刀区合并 MUST 同时保留unhandled-group-action、legacy-group-deadlock和silent-group-action-feedback，白名单 SHALL 使用429cf1dd批准集合，不因自动合并静默丢刀。

#### Scenario: R61I-S002 邻接hunk自动合并
- **WHEN** 两处复活通过ort自动合并
- **THEN** 仍检查三分支存在、白名单逐字一致，并运行浏览器全量及其变异判据。

### Requirement: R61I-003 固定源门与诚实证据
本段 SHALL 固定SOURCE_COMMIT运行canonical两profile、m8/store三遍、浏览器全量和strict；指定codex guide格 MUST 绿。失败尝试与SKIP MUST 逐名保留，不能以单跑覆盖完整门或将环境失败记绿。

#### Scenario: R61I-S003 磁盘不足后恢复
- **WHEN** canonical因临时目录空间不足而失败
- **THEN** 保存原rc/UTC/SHA与未完成范围，恢复环境后在同源重跑完整门，分开报告。

### Requirement: R61I-004 阶段边界
第一段 MUST 保持VERSION且不提前合入尚未冻结的N-3/N-1b/N-1c；作者交件 SHALL 不代替独立复核、最终集成裁定或发布授权。

#### Scenario: R61I-S004 第一段交件
- **WHEN** 七项作者门已完成
- **THEN** 只推集成分支和证据，不推main、不部署、不制作发布件，后续项另待冻结令。

### Requirement: R61I-005 第二段冻结项追加
第二段 MUST 从第一段0ab43c63上仅合入已冻结N-1b，保持一个no-ff合并提交；合前 SHALL 核远端为b81737d0或其仅文档后继，且ed7259fa至来件无产品差异。N3/N1c仅可在后续独立冻结令下追加，VERSION保持不变。

#### Scenario: R61I-S005 N-1b共享祖先自动合并
- **WHEN** N1a638cb324已在第一段链上且N1b远端符合令中边界
- **THEN** 只新增N1b合并父，记录自动合并与冲突处置；不重复覆盖N1a，不提前纳入N3/N1c。

### Requirement: R61I-006 第二段兼容与窄例外证据
第二段 SHALL 在固定合并源跑canonical、PG strict m8/store三遍、focusedrace、浏览器全量与strict，留真实PG版本、逐名SKIP与原日志SHA。真实旧Hub501＋合后helper、合后Hub＋真实39helper MUST 各跑一格；初始m8若仅既定Z-3中断，MUST 保留原失败并按固定五轮至少三轮全绿、其余仅允许三格中断判定，不以单跑替代。

#### Scenario: R61I-S006 新旧queue_full协商
- **WHEN** 隔离的真实Hub/helper进程在容量1已满时接收新投递
- **THEN** 旧Hub＋新helper和新Hub＋39helper均busy且无failure_notice，新新协商failed/queue_full且恰一条通知；夹具暂停模型消费的边界须披露。

#### Scenario: R61I-S007 已知中断保留首轮失败
- **WHEN** 初始三遍仅发生已批准Z-3落点的interrupted(9)
- **THEN** 首轮rc1原样保留，另列固定五轮、基线对照、每轮中断与判定；名单外FAIL或非该机制失败停止，不自行扩名单。

### Requirement: R61I-007 第二段补令按序追加N3
收到N3独立冻结补令后，集成者 MUST 保留已交N1b-only历史，在其后no-ff合入冻结N3；合前 SHALL 核1a2a4160与9313197f至tip仅docs/openspec。新合后源 MUST 重跑指定完整门、48套浏览器及真实旧501/39兼容，不以先前单项或阶段门替代。VERSION不变，应用库迁移不改，N1c不得提前合入。

#### Scenario: R61I-S008 补令晚于N1b-only交件
- **WHEN** N1b-only c430已经交件后才收到包含N3的补充令
- **THEN** 不改写或重复合N1b，只在其后追加N3，第一父链保留九个顺序merge，并记录合前远端及冲突处置。

#### Scenario: R61I-S009 双项合后固定源重新验证
- **WHEN** N1b与N3同在合后固定源4581b34a
- **THEN** 新源canonical、PG strict三遍、ACK/history race、48套浏览器含history provenance、strict及真实501/39协议门均有本次原日志；如适用Z3，初始FAIL与固定五轮、匹配落点main对照分列，不自行扩允许名单。

#### Scenario: R61I-S010 第三段重启历史来源与指定基线
- **WHEN** 第三段晚到令要求在N1b＋N3合后源补Hub重启回放及0ab43c63对照
- **THEN** 已完成的同源全门可按不可变证据复用；另以两个不同Hub OS进程读取相同持久身份/历史库，断言重启前后非空可信来源事件字段一致，并冷页面回放图标一致；同中断落点的0ab对照单列，不拿main对照替代，不把夹具或审计缺项冒称产品通过。
