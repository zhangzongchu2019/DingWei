# DingWei SP2 执行层与门禁设计 v1.1

| 项 | 值 |
|---|---|
| **Design ID** | `dingwei-sp2-design` |
| **版本** | v1.1 |
| **状态** | draft，待三方全文评审 |
| **冻结施工锚** | `53d91aedd158c0a8618f0196f013cfee5cfd44aa` |
| **合同锚** | `fa60efa1977188ca2061cd1ec4929a363cdd6a99` |
| **合同** | `specs/sp2/spec.md` v1.7 |
| **范围** | SpecDoc §5–§9；附录 E 中 owner_sp=SP2 的行；F.1.2、F.1.3、F.2 #4 |
| **主责域** | `WRK` |
| **评审** | `zzc-opus50tchangshark-2642`、`zzc-deepseekv4pro08130ca16c13-2642`、`zzc-opus50tczc2000-2642` |

> 本件只定义 SP2 目标设计与 SP7b 的 WRK 迁移合同，不声称实现、迁移、部署或运行验证已经完成。
> 冻结 SP0/SP1 设计件均为 draft 待评审输入；若其 verdict 改变接口，本件按冲突流程重审，不倒签依赖 PASS。

> v1.1：按正文首轮三方评审一次性闭合 WRK 事件、condition 轴、状态动作接口、附录 E 语义映射、
> §6.2 四项与治理成本、多 required gate 全称不变量；未扩 SP2 产品范围或架构接口 ID。

## D-SP2-01 范围、边界与权威事实

### 1.1 WRK 拥有

WRK 拥有 project、`board_task`、node DAG、node role binding、dispatch lease、execution outcome、独立 gate verdict、
revision lineage、release record、retro record，以及上述事实的事务性 outbox。WRK 是以下事实的唯一写入者：

- node 的执行代际、owner epoch、execution outcome 与 result/evidence ref；
- gate verdict、verdict evidence version 与 reviewer principal；
- DAG eligibility 的结构化派生结果；
- revision 新旧 node 的不可变谱系；
- release/rollback effect 的请求、未知态、核对结果与最终记录。

### 1.2 WRK 不拥有

- principal、enterprise、channel assurance、Agent 绑定：归 IDN/SP1；
- risk rule、GateSet、批准权和 effect grant：归 GOV/SP0；
- 帖子正文、通知正文、知识内容、Agent runtime：分别归 COL/MSG/KNO/AGT；
- 企业库路由、RLS、受控 transaction 与 migration runner：归 PLT/SP7a；
- deploy/rollback 的外部执行器：WRK 只持 capability contract 与 effect record。

任何调用方自述的 enterprise、principal、reviewer、agent 或 role 都不是权威事实。WRK command 只接收
`AccessCtxRef` 与已验证的上游 snapshot/grant 引用，在企业库 transaction 内解析并使用。

### 1.3 上下游与 fail-closed

```text
IDN AccessCtx ─┐
GOV GateSet/Grant ─┼→ WRK command + enterprise transaction → WRK facts + outbox
PLT repository ───┘                                      ├→ AGT/MSG dispatch
                                                         └→ KNO/COL projections
```

AccessCtx、GateSetSnapshot、effect grant 或 enterprise transaction 任一不可取时，mutating command 拒绝；
不得降级为调用方字段、默认企业、session 名或 Agent 名。

## D-SP2-02 领域模型与不变量

### 2.1 聚合与键

| 聚合/实体 | 主键与版本 | 必要关联 | 不变量 |
|---|---|---|---|
| `project` | `project_id`, `generation` | enterprise、owner principal | enterprise 服务端派生；产品线仅属性 |
| `board_task` | `task_id`, `generation` | project、spec ref、版本属性 | 与 datad `task` 分名；不得跨 enterprise |
| `node` | `node_id`, `generation` | task、template/version | 终态后不可原地返工 |
| `node_edge` | `(from_node,to_node)` | 同 task | DAG 无环；引用存在 |
| `node_role_binding` | `(node,role,principal)` | role snapshot | Agent/session 不进入授权键 |
| `dispatch_lease` | `(node,owner_epoch)` | principal、agent audit ref | epoch/CAS 单调；旧 owner fenced |
| `execution_outcome` | `(node,generation)` | result/evidence ref | request_id 幂等；一次执行点结果唯一 |
| `gate_verdict` | `(node,gate_id,evidence_version)` | reviewer principal | execution principal 不得同任一要求独立的 reviewer |
| `revision_link` | `(old_node,new_node)` | reason/evidence | append-only；旧 node 不改 |
| `release_record` | `release_id,generation` | manifest/grant/rollback ref | effect 未知不得写 success |
| `retro_record` | `(task,version)` | 四段结构、evidence refs | 只写复盘，不回写历史 outcome |

### 2.2 两轴状态

`execution_state` 仅允许 `planned/ready/dispatched/running/done/failed/blocked/cancelled`；
`gate_verdict` 仅允许 `not_required/pending/pass/fail`。两轴不得合成一个 status。
`retry_exhausted/dep_failed/starved/delivery_unresolved/awaiting_input/awaiting_gate` 不是 execution 枚举值，
而是独立 `node_condition(kind,state=open|resolved,generation,evidence_ref)` 轴，可同时存在多个 condition：

- `retry_exhausted/dep_failed/starved/awaiting_input` 打开时 execution 转 `blocked`；解消后按 guard 回 `ready`；
- `delivery_unresolved` 打开时 execution 保持 `dispatched`，由 redeliver/reconcile 关闭；
- `awaiting_gate` 打开时 execution 保持 `done`，资源租约释放，gate evidence 与恢复游标持久化；全部 required gate 终结后关闭。

```text
DAG eligible = execution_state == done
               AND FOR ALL required gates: verdict IN {not_required, pass}
```

任一 required gate 为 `pending` 时 node 不 eligible 且不封存；任一 required gate 为 `fail` 时 node 终结并封存、
但不放行下游；全部 required gate 均为 `not_required/pass` 才 eligible。返工只能 `AppendRevision` 新建 node。
execution `failed` 不等于 gate `fail`，前者是执行结果，后者是评审结论。

### 2.3 时间与不可变性

数据库生成 UTC `created_at/recorded_at`；调用方时间只作 observation。execution/gate/release/retro 证据写入后不更新正文，
更正通过新版本和 supersedes ref。封存顺序是先落 outcome/verdict 和完整 evidence，再计算 digest。

## D-SP2-03 状态机、DAG eligibility 与封存

### 3.1 execution 转换

| from | command | to | guard | 拒绝 |
|---|---|---|---|---|
| planned | plan accepted | ready | deps 合法且所需输入齐 | DAG 无环/引用不存在 |
| ready | DispatchNode | dispatched | expected generation、owner epoch CAS | 旧代际/无 principal |
| dispatched | receiver ACK | running | lease 未过期、同 owner epoch | late/fenced |
| running | RecordExecution(success) | done | evidence/result ref 齐 | 重复不同 outcome |
| running | RecordExecution(failure) | failed | failure evidence 齐 | 空失败理由 |
| ready/running | RecordExecution(action=block) | blocked | blocker ref 可解析 | 自述 blocker |
| blocked | RecordExecution(action=resolve_blocker) | ready | blocker generation CAS、resolution evidence | stale/无权解阻 |
| 非终态 | RecordExecution(action=cancel) | cancelled | 有权 principal + reason | 已终态 |

同一 `(node,generation,request_id)` 重投返回原 outcome；相同 request_id 载荷不同返回 `IDEMPOTENCY_CONFLICT`。

### 3.2 gate 与封存

gate evaluation 只在 execution evidence 已落账后开始。每个 required gate 的 `pending→pass|fail` 必须携带 gate id、
GateSet version、reviewer principal、evidence version。任一 required gate pending 时不封；任一 gate fail 时先落该 gate
result 与失败最小复现，再封存 node 并生成 digest；无 fail 时须全部 gate 为 `not_required/pass` 才封存并放行。

### 3.3 F.1.2 delivery.verify

`delivery.verify` 验收不可依赖长活 worker 的记忆。输入固定为不可变 dispatch snapshot：task/node/generation、
spec/result/evidence refs、GateSet version、AccessCtxRef。用空上下文 worker 重灌后应能重建该 node 所需上下文；
缺任一字段即拒接。`OriginHint` 只影响 pick 权重，不承担正确性。本判据承接 F.1.2；其「架构 A§21」引用悬空，
本件不臆造该章节内容。

## D-SP2-04 分级、GateSet 与门方独立

### 4.1 GateSet 消费

WRK 不重算风险等级。`CreateTaskPlan` 固定 `GateSetSnapshotRef(rule_version, gates, required_roles,
subsumption_version)`；后续命令必须引用同一 snapshot。规则版本缺失、已撤销或互斥时 fail-closed 并交 GOV 裁定。
snapshot 同时保留七项属性的声明值、填写 principal 与来源锚；WRK 只执行 GOV 产出的门并集，不以级别标签反推门。
交付 diff 与属性声明不符时生成升级请求，执行者无权单方下调风险档。
snapshot 必须声明 `hit_policy=COLLECT/UNION`、基础必过模板与命中 rule id/reason；属性值只能使用 §5.2 的三档
枚举（节点数除外）。属性定义的可重复性、基础模板是否最小、subsumption 是否专业正确均由门方人审，WRK 只验结构。

### 4.2 gate catalog 与 subsumption

| gate | 结构化输入 | verdict | 独立性 |
|---|---|---|---|
| architecture | design ref + checklist | pass/fail | reviewer principal ≠ implementer principal |
| use-case | use-case set + negative cases | pass/fail | qa owner 具批准权 |
| code/security | commit + evidence | pass/fail | L3 安全门由安全角色 |
| delivery.verify | artifact + execution evidence | pass/fail | 不以 worker 记忆作证据 |
| execution-result | run report + environment ref | pass/fail | 与代码评审分轴 |

subsumption 只允许 GOV snapshot 明列的高门覆盖低门；无表不推断覆盖。批次 verdict 取最高固有风险，不能捆绑稀释。
每个 task 固定 `gate_assignment(task,node,gate_key,responsibility_domain,reviewer_principal,generation)` 台账；
这是门方映射的权威台账；内存映射不构成门方指派。变更指派须受权 command、CAS 与审计，历史 verdict 保留原 reviewer。

### 4.3 防绕过矩阵

- `principal` 相同：即使 Agent/session/display name 不同，也视为同一人并拒绝门方独立性；
- `principal` 不同：仍须核 reviewer role 与真实批准权，角色字符串不自动授予权力；
- Agent 只入 audit provenance，永不进入授权或门判定链；
- template/node 数据不能改变批准权或产生外部 effect，不能以「只是数据」免审。
- reviewer role 匹配仍不足以证明专业能力；门方须独立确认其对责任域具有真实判断力，并将依据落账。

### 4.4 变长环节

task plan 的环节列由 GateSet 生成，不固定列数。未被规则要求的环节记 `N/A`，与 `pending` 分开；前端不得把
N/A 渲染成未完成。门方是每个 gate 实例指定的 principal；「评审角色」本身不是具体门方。

## D-SP2-05 WRK 接口详细合同

### 5.1 公共 envelope

所有 mutating command 统一包含：`request_id`、`AccessCtxRef`、`expected_generation`、`input_refs[]`、
`observed_at?`。服务端补 enterprise/principal/recorded_at。成功与业务拒绝都返回稳定 `outcome_id`；数据库错误不伪装业务拒绝。

### 5.2 八个接口

| ID | 输入/输出 | effect 与 transaction | auth/idempotency/generation | error/audit |
|---|---|---|---|---|
| `WRK-CMD-CreateProject` | metadata→project snapshot | 企业库 tx 写 project+audit | principal；request_id | invalid metadata/forbidden |
| `WRK-CMD-CreateTaskPlan` | project/spec/nodes/edges→task snapshot | 校 DAG，写 task/node/edge/outbox | planner grant；plan version | cycle/missing ref/conflict |
| `WRK-CMD-DispatchNode` | node/assignment/context ref→lease | 记账先行，同 tx 写 lease+outbox | scheduler+owner epoch；request_id+generation | fenced/not ready |
| `WRK-CMD-RecordExecution` | node/outcome/result/evidence→record | outcome+audit+outbox 同 tx | AccessCtx principal；request_id+generation | stale/duplicate conflict |
| `WRK-CMD-RecordGateVerdict` | node/gate/verdict/evidence→record | verdict+audit+outbox 同 tx | reviewer grant；独立性；request_id+generation | self-review/stale gate |
| `WRK-CMD-AppendRevision` | old node/revised ref→new node | 新 node+lineage+deps，同 tx | 有权 principal；request_id+generation | old not terminal/invalid deps |
| `WRK-CAP-DeployRelease` | manifest/grant/rollback ref→effect record | 外部 effect；先 intent 后 adapter，result 回账 | ops principal+grant；request_id+release generation | unknown/reconcile/denied |
| `WRK-QRY-TaskSnapshot` | task/version→snapshot | 只读企业库 projection | AccessCtx；跨人按 policy | not found 与 forbidden 不泄漏 |

### 5.3 错误词表

`INVALID_ARGUMENT`、`FORBIDDEN`、`NOT_FOUND_OR_HIDDEN`、`STALE_GENERATION`、`FENCED_OWNER`、
`IDEMPOTENCY_CONFLICT`、`INVALID_TRANSITION`、`DEPENDENCY_UNAVAILABLE`、`EFFECT_UNKNOWN`。
错误不得以成功空集掩盖 deny；跨企业对象在 query 前拒绝并最小审计。

### 5.4 `RecordExecution` 的状态动作变体

不新增架构接口 ID；`WRK-CMD-RecordExecution` 用受控 `action` 判别执行结果与状态动作：

| action | 必填 schema | 授权 | 幂等/代际 | 错误与审计 |
|---|---|---|---|---|
| `finish` | outcome、result_ref（done 时非空）、evidence_ref | 当前 lease principal | request_id+node generation+owner epoch | 空 result、fenced、重复冲突 |
| `block` | blocker_code、blocker_ref、reason | 当前 owner 或 coordinator | request_id+node generation | invalid blocker/forbidden |
| `resolve_blocker` | blocker generation、resolution evidence | coordinator 或 blocker owner | request_id+blocker generation CAS | stale blocker/forbidden |
| `cancel` | reason、effect disposition | coordinator/owner grant | request_id+node generation | terminal/effect unknown |

每种动作同 tx 写 node、condition/outcome（若有）、audit 与 outbox；done 且 `result_ref=''` 由 schema/guard 双重拒绝。

### 5.5 读模型

`WRK-QRY-TaskSnapshot` 的读模型由同企业库事实投影，至少含 task generation、node execution/conditions、
required gate 全集及逐 gate verdict、DAG eligibility、revision lineage、release/rollback、outbox unresolved 摘要。
投影携带 `projection_version/source_high_water`；落后时明确返回 stale，不把缺行当不存在。看板、ingress、driver、
reconciler、KNO/COL 投影均消费该 typed 读模型，不直读内部表或自行重算 eligibility。

## D-SP2-06 调度并发与 agent 择人

### 6.1 mcsd/board seam

mcsd 保留调度语义，WRK repository 取代 board 裸 DB 访问。scheduler 只经 `TaskSnapshot/DispatchNode`；
不得持有或导出数据库句柄。dispatch 事务先冻结 snapshot 与 owner epoch，再写 outbox，提交后异步投递。

### 6.2 并发与迟到结果

- 同 node 仅一个有效 owner epoch；接管用 CAS 提升 epoch；旧 epoch 的 ACK/result 返回 FENCED；
- delivery_id/outbox/ACK 管投递账，reader cursor 不替代投递账；
- at-least-once delivery + consumer dedupe，不宣称消息恰好一次；
- effect 已发但结果未知进入 reconciler，禁止猜 success；
- PG 并发下重新证明线性化点，不沿用 SQLite `SetMaxOpenConns(1)` 的隐含串行假设。

### 6.3 F.2 #4 agent 择人

择人是 WRK 调度算法，不授予身份或权限。候选集先由结构化约束过滤：principal 已绑定、所需 role/capability、
企业/项目范围、availability、冲突隔离；再按 owner affinity、负载与最近失败作确定性排序。`OriginHint` 只作权重。
每次选择持久化 candidate set version、过滤原因、排序输入和 winner，允许重算。无候选进入 starved，不默选任意 Agent。

### 6.4 WRK 域事件

所有事件公共头为 `event_id`、`occurred_at`、`producer=WRK`、`schema_version`、`enterprise_id`、
`principal_id?`、`correlation_id`、`causation_id`。事件与产生它的 WRK 事实、outbox 同事务写入；消费者按
`event_id` 幂等，未知 major schema version 拒绝并报警，新增 optional 字段只升 minor version。

| 事件 | 触发与 payload | 消费者 | 幂等/兼容 |
|---|---|---|---|
| `EV-WRK-NodeDispatched` | lease+snapshot 冻结后；task/node/generation/owner_epoch/context_ref | AGT、MSG、KNO、COL | event_id 去重；旧 epoch fenced |
| `EV-WRK-ExecutionFinished` | outcome 持久化后；node/generation/outcome/result/evidence refs | COL、GOV | 同 outcome 只发一逻辑事件；重投同 event_id |
| `EV-WRK-GateVerdictRecorded` | 单 gate verdict 持久化后；gate_key/verdict/evidence_version/reviewer | COL、GOV | `(node,gate,generation)` 可重算；未知 verdict version 拒绝 |

事件表示事实已落账，不表示下游消费成功，也不代替 command response。新增消费者不得改变 producer transaction。

## D-SP2-07 失败、返工与短程小任务

### 7.1 分流

| 事件 | 当前 node | 后续 |
|---|---|---|
| execution failed | 封存失败 evidence | 可 AppendRevision 新 node；不得改 outcome |
| gate FAIL | 封存 verdict+最小复现 | 下游不放行；revision 新 node |
| blocked | 保留 blocker ref | 解阻须受权 command+新 generation |
| timeout | 先判 lease/effect 状态 | 未知 effect 进 reconcile，不直接重发 |
| cancel | 终结并审计 | 已发生 effect 仍须 reconcile/rollback |

其余非终态处理固定为：重试耗尽→`retry_exhausted/blocked`；依赖失败→下游 `dep_failed/blocked`；无人可派→
`starved/blocked`；投递悬空→`delivery_unresolved` 后由 redeliver 收敛；等人输入→`awaiting_input/blocked`；需产品决策→
调度者再升业主。`owner_override/takeover` 只映射白名单 command，不接受自由文本直写状态。

### 7.2 F.1.3 短程小任务判据

不以时长/token/「改动小」判短程。模板只有同时满足以下结构化条件才可标 `short_path`：

1. 预期不发生 gate FAIL 回流，且 GateSet 未要求独立中间门；
2. 单 node、无外部不可逆 effect、无多 owner 交接；
3. delivery.verify 所需 snapshot 字段齐；
4. 任一条件不满足时回退默认长活会话承接，不静默简化。

交付验证用模板声明与实际 node/effect/gate 记录对账；出现 revision、外部 effect 或 gate FAIL 即反例失败。
这是可复核资格判据，不是性能承诺。

## D-SP2-08 发版、观察期与 rollback

### 8.1 八格模板

提请→预案就绪→审批→preprod 执行→观察期→观察期门→prod 执行→收口。实际 deploy/rollback 仅由
`WRK-CAP-DeployRelease` 调受控 adapter；node 只编排，不执行副作用。

### 8.2 分支与记录

受控分支仅 preprod/prod，ops 为唯一合入者。prod 只能由 preprod 合入；preprod 原则上运行满 2 周，早期可缩短至
3 天。进入 preprod 前记录必须齐：分支名、全部文件、CI 所需文件、SpecDoc+评审记录、设计+评审记录、
测试用例+评审记录、测试报告+校验记录。第 4–7 项逐项成对；省略必须引用具体允许条款。

### 8.3 rollback

rollback 是独立 effect request，须独立 request_id、grant、generation、evidence 与结果。触发判据固定指标、窗口、
decision principal；效果未知时先 reconcile。reject/stop/resume 不天然等于 rollback 已执行。

### 8.4 收口格

第八格只由调度者 principal 关闭，并在同一 WRK 事务登记四项引用：版本摘要的晋升归属、spec 预授权失效、
部署锚 `(component,environment,commit,artifact_sha256)`、本阶段复盘。缺复盘或部署锚不得收口。

## D-SP2-09 复盘与定义回写边界

### 9.1 四段复盘

输入固定为该阶段帖子流、交付物清单、全部 gate 记录（含 FAIL 与整改）、blocker 记录；可由模型生成草稿，但须由
执行者/评审者定稿，禁止从聊天记忆补事实。每个 task/version 输出恰四段：沉淀事实、可复用操作、应固化能力、
本阶段判断失误。任一段为空必须写理由；不得用「目标与结果」等另一套四段替换权威结构。
第 4 段进入知识库逐级晋升前必须去标识化，并由门方检查是否仍可反推出具体人、会话或事故。

### 9.2 回写 SP0

复盘只能提交 `rule_candidate`，包含来源锚、适用范围、反例和建议；不能直接改 SP0 rule/GateSet。
GOV 评审并发布新规则版本后，后续 task 才消费；历史 task 保留原 snapshot。

### 9.3 周报投影

周报查询只读 WRK projection，带数据截止时刻、生成时刻、内容 sha256 与部分数据标记。Markdown 导出走 typed capability，
不开放通用 export。WRK 提供 task/node/gate/release 事实，COL 负责视图与产物落账。

## D-SP2-10 WRK schema 与 SP7b 迁移合同

### 10.1 目标 schema 约束

所有 WRK 表位于企业库，含 enterprise-local key；transaction 由 SP7a `RunInAccessTx` 提供。外键覆盖 project→task→node、
edge 两端、outcome/verdict/revision/release 引用；CHECK 固定状态词表；唯一键覆盖 request_id 与 generation。

### 10.2 迁移与 cutover

| 被测对象 | 输入 | 期望 | 证据形态 | 验收门 |
|---|---|---|---|---|
| WRK DDL | 空企业库 + migration version | 表/约束/index/RLS policy 齐 | schema dump + negative SQL | SP7a conformance + SP2 delivery |
| board_task 改名 | board task rows | datad task 不变；board_task 映射唯一 | row counts + key map | migration review |
| board 状态迁移 | 旧 node/result/outbox | execution/gate 分轴，无丢失 | before/after manifest | SP2 delivery.verify |
| 双读比对 | 旧 board + WRK repo | 结构化事实等价；差异可解释 | keyed diff | cutover gate |
| 写切换 | generation fence | 新写只入 WRK；旧写拒绝 | mutation negative test | release approval |
| rollback | 未越不可逆点/备份 ref | 恢复旧读写或前滚修复 | rehearsal report | ops+SP2 gate |

迁移脚本不得自建连接或绕受控 repository。实际执行前必须有备份/恢复方案、观察窗口和 rollback 判据。

### 10.3 状态词汇

迁移项仅用 `new/retain/adapt/rewrite/retire`：mcsd 调度语义 retain；board API adapt；board storage/state rewrite；
WRK repository/schema new；裸 DB accessor retire。不得以 transition/consolidate 等自由标签绕开五态验收。

## D-SP2-11 现状复用与 conformance suite

| 现状单元 | 状态 | 复用/改造边界 | 必钉 conformance |
|---|---|---|---|
| mcsd scheduler | retain | 保留 pick/dispatch 语义，经 WRK port | 候选为空、owner 接管、迟到结果 |
| board public API | adapt | 调用方迁 typed WRK 接口 | 现有主干行为 + 错误语义 |
| board storage/state | rewrite | PG 企业库、两轴状态 | CAS/generation、事务、封存 |
| result_outbox 模式 | adapt | 冻结区间、ACK、重投 | at-least-once + dedupe |
| `Board.DB()` | retire | 不导出/返回裸句柄 | 仓储包外 escape 恒零 |
| WRK schema/repository | new | 受控 transaction/RLS | 跨企业/跨人负测 |

conformance 断言业务事件按 kind 与语义，不要求所有事件总数不变；新实现可合法增加簿记事件。每项至少一正一反，
变异必须改变被测出口并使对应测试失败；只看到 rc 非零不证明因正确理由失败。
回归矩阵必须覆盖 ingress、driver、scheduler、Receiver、所有读取 node 状态的消费者和三类 WRK 事件投影；
不能只测 repository。`awaiting_gate` 下 worker lease 已释放、门方台账和恢复游标可重建也须有崩溃恢复用例。

## D-SP2-12 追踪、验收与证据出口

### 12.1 证据出口

- design checker：来源集合、实检集合、未知集合；
- interface contract cases：八接口 schema/auth/idempotency/generation/error；
- state-machine negative cases：非法转换、late/fenced、self-review；
- migration manifest：DDL、row/key counts、diff、cutover/rollback；
- delivery.verify：空上下文重灌、缺字段拒接；
- F.1.3：short_path 正反例；F.2 #4：候选集与排序重算。

每次交付回执还必须带：可取件路径或 40 位 commit、最终 sha256、行数与 `wc -c` 字节、自测命令+摘要、
门结果+失败最小复现。前四项机器可复算，门结果由评审方判断；机器全绿不能代签语义 PASS。
本设计引入/改写 GateResult、condition、封存和回流等 C1 原语语义，进入实现前须按 §24 攒批完成重两签；
该治理成本登记在 SP2 实现开工门，不得因设计评审 PASS 自动消失。

### 12.2 结构化覆盖

<!-- COVERAGE-SP2-BEGIN -->
| 来源 | 设计落点 |
|---|---|
| `SRC:§5` | `D-SP2-04` |
| `SRC:§5.1` | `D-SP2-04` |
| `SRC:§5.2` | `D-SP2-04` |
| `SRC:§5.3` | `D-SP2-04` |
| `SRC:§5.4` | `D-SP2-04` |
| `SRC:§5.4a` | `D-SP2-04` |
| `SRC:§5.5` | `D-SP2-04` |
| `SRC:§5.6` | `D-SP2-04` |
| `SRC:§5.6a` | `D-SP2-04` |
| `SRC:§5.7` | `D-SP2-04` |
| `SRC:§5.8` | `D-SP2-04` |
| `SRC:§5.8a` | `D-SP2-04` |
| `SRC:§5.9` | `D-SP2-04` |
| `SRC:§6` | `D-SP2-05` |
| `SRC:§6.1` | `D-SP2-05` |
| `SRC:§6.2` | `D-SP2-03`、`D-SP2-04`、`D-SP2-05`、`D-SP2-11`、`D-SP2-12` |
| `SRC:§7` | `D-SP2-04`、`D-SP2-07` |
| `SRC:§7.1` | `D-SP2-01`、`D-SP2-05` |
| `SRC:§7.2` | `D-SP2-04`、`D-SP2-08` |
| `SRC:§7.3` | `D-SP2-07` |
| `SRC:§7.4` | `D-SP2-03`、`D-SP2-07` |
| `SRC:§8` | `D-SP2-08` |
| `SRC:§8.1` | `D-SP2-08` |
| `SRC:§8.1a` | `D-SP2-08` |
| `SRC:§8.1b` | `D-SP2-08` |
| `SRC:§8.2` | `D-SP2-08` |
| `SRC:§9` | `D-SP2-09` |
| `SRC:§9.1` | `D-SP2-09` |
| `SRC:§9.2` | `D-SP2-09` |
| `E:E.1#1` | `D-SP2-04` |
| `E:E.1#2` | `D-SP2-04` |
| `E:E.1#3` | `D-SP2-04` |
| `E:E.1#4` | `D-SP2-04` |
| `E:E.1#5` | `D-SP2-08` |
| `E:E.1#6` | `D-SP2-04` |
| `E:E.1#7` | `D-SP2-03`、`D-SP2-05` |
| `E:E.1#8` | `D-SP2-08` |
| `E:E.1#9` | `D-SP2-09` |
| `E:E.1#62` | `D-SP2-03` |
| `E:E.1#63` | `D-SP2-04` |
| `E:E.1#66` | `D-SP2-09` |
| `E:E.1#70` | `D-SP2-04` |
| `E:E.1#73` | `D-SP2-09` |
| `E:E.1#74` | `D-SP2-12` |
| `E:E.1#186` | `D-SP2-04` |
| `E:E.3#11` | `D-SP2-04` |
| `E:E.3#13` | `D-SP2-04` |
| `E:E.3#14` | `D-SP2-04` |
| `E:E.4#1` | `D-SP2-04` |
| `E:E.4#2` | `D-SP2-04` |
| `E:E.4#6` | `D-SP2-09` |
| `E:E.4#7` | `D-SP2-09` |
| `E:E.4#12` | `D-SP2-04` |
| `E:E.4#18` | `D-SP2-04` |
| `E:E.4#20` | `D-SP2-12` |
| `E:E.4#21` | `D-SP2-04` |
| `E:E.4#23` | `D-SP2-04` |
| `E:E.4#28` | `D-SP2-08` |
| `E:E.4#32` | `D-SP2-04` |
| `IF:WRK-CMD-CreateProject` | `D-SP2-05` |
| `IF:WRK-CMD-CreateTaskPlan` | `D-SP2-05` |
| `IF:WRK-CMD-DispatchNode` | `D-SP2-05` |
| `IF:WRK-CMD-RecordExecution` | `D-SP2-05` |
| `IF:WRK-CMD-RecordGateVerdict` | `D-SP2-05` |
| `IF:WRK-CMD-AppendRevision` | `D-SP2-05` |
| `IF:WRK-CAP-DeployRelease` | `D-SP2-05` |
| `IF:WRK-QRY-TaskSnapshot` | `D-SP2-05` |
| `EVENT:EV-WRK-NodeDispatched` | `D-SP2-06` |
| `EVENT:EV-WRK-ExecutionFinished` | `D-SP2-06` |
| `EVENT:EV-WRK-GateVerdictRecorded` | `D-SP2-06` |
| `UI:#s2` | `D-SP2-03` |
| `UI:#s4` | `D-SP2-04` |
| `UI:#a5` | `D-SP2-04` |
| `UI:#a6` | `D-SP2-12` |
| `UI:#b2` | `D-SP2-09` |
| `UI:#a2` | `D-SP2-03` |
| `F:§F.1.2` | `D-SP2-03` |
| `F:§F.1.3` | `D-SP2-07` |
| `F:F.2#4` | `D-SP2-06` |
<!-- COVERAGE-SP2-END -->

### 12.3 验收判据

<!-- ACCEPTANCE-SP2-BEGIN -->
| ID | 受检范围 | 判据 | 判不了什么 |
|---|---|---|---|
| `AC-SP2-01` | checker 派生的 §5–§9 heading | COVERAGE 逐项 D 落点，未知 0 | 落点语义正确性 |
| `AC-SP2-02` | owner_sp=SP2 的附录 E 行 | 逐项落点，未知 0 | 探测手段充分性 |
| `AC-SP2-03` | 架构 §13 WRK 行派生并在原型存在的入口 | 逐项落点，未知 0 | UI 可用性与实现状态 |
| `AC-SP2-04` | 架构 §6 WRK 稳定接口与 §4.2 WRK 事件 | 8 接口+3 事件逐项落点，无 RT/非接口 EV 污染 | 字段与事件语义充分性 |
| `AC-SP2-05` | execution/gate/eligibility/seal | 两轴独立，FAIL 与封存无矛盾 | 代码实现 |
| `AC-SP2-06` | 八个接口中的 mutating command | request_id/generation/principal/tx/audit 齐 | handler 漏传 |
| `AC-SP2-07` | 门方冲突与身份反例 | verified principal 比对；Agent/session/display name 拒作身份 | reviewer 专业能力 |
| `AC-SP2-08` | failed/FAIL/blocked/timeout/cancel | 返工、终结、reconcile 分流 | 未知故障穷尽性 |
| `AC-SP2-09` | deploy/rollback 全链 | node/template 不绕 capability/grant/idempotency/audit | adapter 可靠性 |
| `AC-SP2-10` | late/retry/effect unknown/partial failure | fencing+reconcile，不猜成功 | 生产故障覆盖率 |
| `AC-SP2-11` | 复用映射与 conformance | 每项正反例；未覆盖不宣称完成 | 隐藏旧语义 |
| `AC-SP2-12` | SP7b 五列合同与覆盖证据 | 对象/输入/期望/证据/门齐，checker 未知 0 | 实际迁移结果 |
<!-- ACCEPTANCE-SP2-END -->

### 12.4 冲突与挂账

1. SpecDoc F.1.2 引用「架构 A§21」，冻结架构只到 §19：已登记上交；本件只承接明确的 `delivery.verify` 文义。
2. SP0/SP1 冻结设计件为 draft：本件以待确认 seam 编写；其最终 verdict 若改变接口，必须重审相关 D 块。
3. SP7a 未实现：本件给出 SP7b 合同，不以 mock 测试升级受控 transaction/RLS/migration runner 为已就绪。
4. 真实 PG 迁移、备份恢复、并发和 rollback 演练均属于实现/交付验证，不在本设计 PASS 内。
