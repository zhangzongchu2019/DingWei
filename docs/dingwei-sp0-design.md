# DingWei SP0 总则与定义层设计 v1.1

| 项 | 值 |
|---|---|
| **Design ID** | `dingwei-sp0-design` |
| **版本** | v1.1 |
| **状态** | draft，待双评审 |
| **冻结施工锚** | `46b79ab0022b0686a23455f328c9316d7f87281f` |
| **合同** | `specs/sp0/spec.md` |
| **范围** | SpecDoc §1–§4、附录 A |
| **目标架构** | `GOV` 域；不修改已通过的架构接口目录 |

> v1.1：覆盖声明改为 `COVERAGE-SP0` 结构化块；checker 只认块内逐项落点并加入正交变异反测，设计语义不变。

输入锚：`46b79ab0022b0686a23455f328c9316d7f87281f` · `docs/dingwei-platform-specdoc.md` ·
`docs/dingwei-platform-architecture.md` · `docs/dingwei-prototype.html`

## D-SP0-01 范围、术语与边界

### 1.1 本件拥有

SP0 设计的是“产品定义怎样成为可复算、可评审、可追溯的治理事实”，拥有：

- spec 元数据、不可变 version、输入锚、属性快照；
- rule set、规则版本、分级复算与 gate-set snapshot；
- reviewer-owned checklist、review round、pending item；
- 附录 A tuning profile 的版本化载体；
- 准入、人审、非语义修订、依赖闭包、认领退审和偏离复核的流程合同。

### 1.2 本件不拥有

| 对象 | owner | SP0 只持什么 |
|---|---|---|
| principal / AccessCtx | SP1 / IDN | immutable principal ref，不复制身份状态 |
| project / task / node / gate verdict | SP2 / WRK | spec version、gate-set ref，不改执行状态 |
| approval / effect grant | SP8 / GOV 治理扩展 | approval ref，不自行批准跨边界 effect |
| 仓储、迁移、RLS | SP7a | repository port 与验收合同，不写 DDL |
| 原型页面与运行 UI | SP6 | view model，不把 UI 状态当权威状态 |

### 1.3 受控术语

| 术语 | 唯一含义 |
|---|---|
| `SpecRecord` | 跨版本稳定身份与 parent/child 关系 |
| `SpecVersion` | 内容、输入锚、属性、规则版本均已冻结的不可变版本 |
| `RuleSetVersion` | 可复算分级的不可变规则表版本 |
| `GateSetSnapshot` | 某一 spec version 在某规则版本下的规则命中与门并集 |
| `ChecklistInstance` | reviewer-owned 的评审输入及勾选证据，不由 implementer 改 |
| `ReviewRound` | 针对精确 spec version 的人审轮次与 verdict |
| `PendingItem` | 待完善或待认领 typed item，不是另一个页面/队列体系 |
| `TuningProfile` | 附录 A 中仅数值可调项的不可变版本 |

## D-SP0-02 领域模型与不变量

### 2.1 聚合与引用

```text
SpecRecord 1 ── * SpecVersion
SpecVersion 1 ── * InputAnchor
SpecVersion 1 ── 1 AttributeSnapshot
AttributeSnapshot + RuleSetVersion ── 1 GateSetSnapshot
SpecVersion 1 ── * ChecklistInstance ── * ChecklistEvidence
SpecVersion 1 ── * ReviewRound
SpecVersion 0..1 ── 1 PendingItem
TuningProfile 1 ── * TuningValue
```

| 对象 | 主键/唯一键 | 不可变部分 | 可变部分 |
|---|---|---|---|
| `SpecRecord` | `spec_id` | 创建者、创建时刻 | active_version_ref（CAS） |
| `SpecVersion` | `(spec_id, version)`；content_digest 唯一约束归本 spec | content/input/attributes/rules_ref/digest | 无；修订创建新版本 |
| `InputAnchor` | `(spec_version_id, anchor_id)` | kind/ref/digest/purpose | 无 |
| `AttributeSnapshot` | `attribute_snapshot_id` | 七项属性与 schema_version | 无 |
| `RuleSetVersion` | `(rule_set_id, version)` | rules/digest/effective range | retired_at 仅终止未来使用 |
| `GateSetSnapshot` | `(spec_version_id, rule_set_version)` | matched rule_ids/gates/digest | 无 |
| `ChecklistInstance` | `(spec_version_id, checklist_id, generation)` | template digest/owner reviewer | checkbox/evidence 仅 reviewer CAS 更新 |
| `ReviewRound` | `(spec_version_id, round_no)` | reviewers/input digest | verdict/evidence 由门方完成一次 |
| `PendingItem` | `pending_id` | type/spec_version/reason | claim/return/stale 按 generation CAS |
| `TuningProfile` | `(profile_id, version)` | key/value/evidence/review point | 无；调值创建新版本 |

### 2.2 跨版本原则

1. `SpecVersion` 一旦进入人审即冻结；正文改动创建下一版本，原 ReviewRound 不迁移。
2. GateSetSnapshot 固定 rule set version；规则升级不回写旧结果。
3. checklist evidence 绑定 spec version、item ID、reviewer principal 与证据锚，不能只存 `[x]`。
4. parent/child 只引用稳定 `spec_id + required_version_range`；解析不到即硬门失败。
5. `TuningProfile` 只承载附录 A 已列 key；出现未知 key 判语义变更，不自动接纳。

## D-SP0-03 生命周期与代际

### 3.1 Spec 生命周期

```text
draft --submit--> hard_check
hard_check --fail--> needs_completion
hard_check --pass--> human_review
human_review --NO/INCONCLUSIVE--> needs_completion
human_review --PASS--> effective
effective --new revision--> draft(new version)
effective --scope end--> expired
```

`hard_check` 是过程态，不与 `effective/expired` 共用字段。权威字段分为：

- `lifecycle_state = draft | effective | expired`；
- `admission_state = unchecked | rejected | ready_for_review`；
- `review_verdict = pending | pass | no | inconclusive`。

任何实现不得把三轴折叠成一个 `status`。

### 3.2 Checklist 与 Review

- Checklist：`open → submitted → sealed`；只允许 reviewer principal 以 generation CAS 勾选。
- ReviewRound：`pending → pass|no|inconclusive`；终态不重开，复审创建新 round。
- implementer 修改 content digest 后，所有未终结 round 标 `superseded`，且 pending claim 退回待完善。

### 3.3 PendingItem

| type | 状态 | 允许转换 |
|---|---|---|
| `spec_fix` | open/claimed/returned/closed/stale | claim、return、close、周期性 mark-stale |
| `spec_claim` | open/claimed/returned/closed/stale | 认领后正文变化强制 returned |

两类共用 typed queue port，不共用相同 reason schema。`claim_generation` 防止迟到认领覆盖当前 owner。

### 3.4 TuningProfile

`proposed → reviewed → active → superseded`。调值只能由附录 A 指定观察量触发；任何 key 集、含义、权限或结构变化
转为普通 SpecVersion 语义修订，不能走 tuning 快路径。

## D-SP0-04 接口设计

### 4.1 `GOV-CMD-SubmitSpec` 细化

```text
request: request_id, expected_spec_generation, spec_id?, parent_refs[],
         content_ref, content_digest, input_anchors[], attributes, rules_version
actor:   verified principal_ref
result:  spec_id, version, admission_report, gate_set_ref?, pending_ref?
```

事务：同一 GOV transaction 写 SpecVersion、anchors、attribute snapshot、admission result；需通知时同事务写 outbox。
同 request_id 重放返回原结果。expected generation 不符返回 `FENCED`；输入锚缺失、属性不全、规则不可取返回
`VALIDATION_FAILED`，且完整列出缺项，不创建半个可评审版本。

### 4.2 `GOV-QRY-ResolveGateSet` 细化

```text
query:  attribute_snapshot_ref, rules_version
result: matched_rule_ids[], required_gates[], decision_digest
```

它是对不可变输入的纯求值。规则缺失/解析失败返回 UNKNOWN/FAIL，不回退“最近规则”，不输出空 gates 冒充低风险。

### 4.3 域内 ports

| ID | effect | 幂等/代际 | 失败 |
|---|---|---|---|
| `SP0-CMD-CreateChecklist` | 从 template digest 创建 reviewer-owned instance | request_id + spec_version | template/owner 缺失拒绝 |
| `SP0-CMD-MarkChecklist` | reviewer 更新 item 与 evidence | item generation | 非 owner、空证据、旧代际拒绝 |
| `SP0-CMD-RecordReview` | 封存 round verdict | round generation | implementer 同 principal、证据空拒绝 |
| `SP0-CMD-ClaimPending` | typed item 认领 | request_id + claim generation | 非 open/旧 version 拒绝 |
| `SP0-CMD-ReturnPending` | 退回并记录结构化 reason | request_id + generation | reason code 非法拒绝 |
| `SP0-CMD-ProposeTuning` | 创建 tuning profile version | request_id + profile generation | 未知 key/无 evidence 拒绝 |
| `SP0-QRY-SpecReviewView` | 返回版本、评审、预授权、生命周期 | 只读 | 缺权威字段返回 incomplete，不猜值 |

### 4.4 事件

`SpecVersionSubmitted`、`AdmissionRejected`、`SpecReadyForReview`、`ReviewCompleted`、`SpecEffective`、
`SpecSuperseded`、`PendingClaimed`、`PendingReturned`、`TuningActivated` 均含 event_id/spec_version/generation/actor/evidence_ref。

## D-SP0-05 数据设计与迁移责任

### 5.1 逻辑表

| 表 | 关键字段 |
|---|---|
| `gov_spec` | spec_id, parent_spec_id, active_version, generation |
| `gov_spec_version` | spec_id, version, content_ref/digest, lifecycle_state, created_by/at |
| `gov_input_anchor` | spec_version_id, anchor_id, kind, ref, digest, purpose |
| `gov_attribute_snapshot` | snapshot_id, schema_version, seven attributes, digest |
| `gov_rule_set_version` | rule_set_id, version, rules_ref/digest, effective_at, retired_at |
| `gov_gate_set_snapshot` | spec_version_id, rules_version, rule_ids, gates, digest |
| `gov_checklist` / `gov_check_item` | owner_principal, template_digest, state, generation, checked/evidence |
| `gov_review_round` | reviewers, verdict, evidence_ref, generation |
| `gov_pending` | type, reason_code, owner, spec_version, state, generation, review_due_at |
| `gov_tuning_profile` / `gov_tuning_value` | version, key, typed value, evidence, review_point |
| `gov_outbox` / `gov_audit` | event/audit canonical fields |

### 5.2 约束

- 所有引用以 ID/ref 表达，正文不复制；digest 进入唯一性与审计，不作为授权凭据。
- checklist item 更新须同时匹配 owner principal 与 generation。
- active_version 只可指向 lifecycle=effective 的版本。
- pending close 与 spec version 生效/退审同事务或经可重放 reconciler 收敛。

### 5.3 迁移责任

本件定义目标 schema 合同；实际 migration 归 SP7b/GOV 实现批次。迁移须从现有 `specs/` 文件与评审台账导入，
不得把“文件存在”推定为“已评审”；无法证明 reviewer/evidence 的 checkbox 导入为 unchecked。

## D-SP0-06 核心流程

### 6.1 准入与人审

1. SubmitSpec 固化输入锚、内容 digest、属性和 rules_version。
2. 硬门一次返回全部失败项；失败创建 `spec_fix`，不创建 review round。
3. 硬门全过后解析 GateSetSnapshot，按结果选 reviewer；含跨边界预授权时 reviewer 必须有真实批准权。
4. reviewer 创建并独立填写 checklist；机器检查结果只作 evidence，不自动打勾。
5. 所有 required reviewer 的 round PASS 后 lifecycle 转 effective；任一 NO 则进入 needs_completion。

### 6.2 非语义修订

受保护对象 = 属性头块、预授权、acceptance、硬性约束行、表格规则行及其依赖闭包。
diff 只要触及闭包、规则目标、枚举、术语、禁用词、immutable 名单或链接目标，即是语义修订。
闭包解析失败必须升级为语义修订；不能因“没找到依赖”判免审。

### 6.3 需求池

- 待完善项必须有 reason_code、missing_fields、owner、due_at；自由文本只作补充。
- 待认领项只接已过人审的精确 version；认领后 digest 变化自动 returned。
- 每两周扫描；连续三次无进展标 stale 并通知提出方。扫描失败不静默跳过。

### 6.4 预计数据偏离

预计值存 `value + unit + basis_anchor + review_point`。实测达到预计两倍或低于一半时创建 spec review request；
不自动改预计值，也不直接使 spec 失效。阈值本身来自 TuningProfile，版本进入触发证据。

## D-SP0-07 权限与审计

| 动作 | author | reviewer | implementer | governance owner |
|---|---:|---:|---:|---:|
| 提交/修订 spec | ✓ | ✓ | ✓ | ✓ |
| 指定 reviewer | — | — | — | ✓ |
| 勾 checklist | — | ✓（仅本人项） | — | reviewer 身份时 ✓ |
| 签 review verdict | — | ✓ | — | reviewer 身份时 ✓ |
| 实现/执行 | ✓ | ✓ | ✓ | ✓ |
| 激活 rule/tuning version | — | — | — | ✓（按审批） |

同一 principal 可有多个角色，但同一 spec/version 上 implementer 与 required reviewer 冲突时不得签门。
最低审计：request_id、actor principal、role_at_time、object/version、before/after generation、effect、evidence、timestamp、outcome。

## D-SP0-08 失败、并发与恢复

| 场景 | 结果 |
|---|---|
| SubmitSpec 重投 | 返回原 version/admission report，不重复 outbox |
| 同 spec 并发修订 | expected generation 仅一个成功，另一方 FENCED |
| reviewer 迟到勾选旧 generation | 拒绝并审计，不迁移到新 checklist |
| rule set 读取失败 | GateSet UNKNOWN/FAIL，不输出空门集 |
| 依赖闭包解析失败 | 按语义修订，不免审 |
| pending 已认领后正文变化 | CAS 退回 returned，原 owner 不再有效 |
| outbox 状态未知 | reconcile by request/event ID，不猜成功 |
| tuning key 不在附录 A | VALIDATION_FAILED，转普通 spec 变更 |

## D-SP0-09 现状复用映射

| 现状 | 五态 | 设计处置 |
|---|---|---|
| `specs/constitution.md` / `specs/platform/spec.md` | retain | 继续作规范载体，不作运行态数据库 |
| reviewer-owned checklist 三件 | adapt | 保留所有权语义，补 version/owner/evidence/generation 持久化 |
| `docs/tools/speclint.py` 与 corpus | adapt | 继续作形状旁证，规则/语义 PASS 不由它签 |
| `scripts/speclint-gate.sh` | adapt | 作为提交侧证据入口；不能替服务端 GOV 准入事务 |
| `rv-ledger.txt` / revision history | adapt | 沿革导入只作历史 evidence，不倒签 review 状态 |
| GOV 运行态模型与 ports | new | 当前无权威持久化模型 |
| 外部流程引擎 | retire/not-adopted | 不引入；调度仍由既有 mcsd 语义承担 |

新增外部依赖：0。目标架构接口 ID 新增：0。

## D-SP0-10 追踪与验收矩阵

<!-- COVERAGE-SP0-BEGIN -->

### 10.1 SpecDoc 章节覆盖

| 来源 | 落点 |
|---|---|
| `§1` | `D-SP0-01`、`D-SP0-09` |
| `§2` | `D-SP0-01`、`D-SP0-09` |
| `§2.1` | `D-SP0-01`、`D-SP0-09` |
| `§2.2` | `D-SP0-01`、`D-SP0-09` |
| `§2.3` | `D-SP0-01`、`D-SP0-09` |
| `§2.4` | `D-SP0-01`、`D-SP0-09` |
| `§3` | `D-SP0-01`、`D-SP0-06` |
| `§4` | `D-SP0-02`、`D-SP0-04` |
| `§4.1` | `D-SP0-02`、`D-SP0-07`、`D-SP0-09` |
| `§4.2` | `D-SP0-01`、`D-SP0-09` |
| `§4.3` | `D-SP0-02`、`D-SP0-07` |
| `§4.4` | `D-SP0-02`、`D-SP0-05` |
| `§4.5` | `D-SP0-01`、`D-SP0-09` |
| `§4.6` | `D-SP0-03`、`D-SP0-04`、`D-SP0-06` |
| `§4.6.1` | `D-SP0-02`、`D-SP0-04` |
| `§4.6.2` | `D-SP0-04`、`D-SP0-06`、`D-SP0-08` |
| `§4.6.3` | `D-SP0-04`、`D-SP0-06` |
| `§4.6.4` | `D-SP0-06`、`D-SP0-07` |
| `§4.6.5` | `D-SP0-06`、`D-SP0-08` |
| `§4.7` | `D-SP0-02`、`D-SP0-06` |
| `§4.7.1` | `D-SP0-02`、`D-SP0-04`、`D-SP0-06` |
| `§4.7.1a` | `D-SP0-06`、`D-SP0-08` |
| `§4.7.1b` | `D-SP0-02`、`D-SP0-06` |
| `§4.7.2` | `D-SP0-02`、`D-SP0-06` |
| `§4.7.3` | `D-SP0-03`、`D-SP0-06` |
| `附录 A` | `D-SP0-02`、`D-SP0-03`、`D-SP0-06`、`D-SP0-08` |

### 10.2 附录 E owner_sp=SP0 覆盖

| 探测来源 | 设计落点 |
|---|---|
| `E.1#75` | `D-SP0-04`、`D-SP0-06` |
| `E.1#76` | `D-SP0-06`、`D-SP0-07` |
| `E.1#77` | `D-SP0-06`、`D-SP0-08` |
| `E.1#101` | `D-SP0-06`、`D-SP0-08` |
| `E.1#55` | `D-SP0-07` |
| `E.1#56` | `D-SP0-02`、`D-SP0-04` |
| `E.1#57` | `D-SP0-07` |
| `E.1#58` | `D-SP0-07` |
| `E.1#140` | `D-SP0-04`、`D-SP0-06` |
| `E.1#141` | `D-SP0-02`、`D-SP0-06` |
| `E.1#142` | `D-SP0-03`、`D-SP0-06` |
| `E.1#143` | `D-SP0-03`、`D-SP0-06` |
| `E.1#144` | `D-SP0-03`、`D-SP0-06` |
| `E.1#145` | `D-SP0-06` |
| `E.1#180` | `D-SP0-02`、`D-SP0-04` |
| `E.1#183` | `D-SP0-01`、`D-SP0-09` |
| `E.1#190` | `D-SP0-06`、`D-SP0-08` |
| `E.1#193` | `D-SP0-08` |
| `E.3#8` | `D-SP0-02`、`D-SP0-05` |
| `E.4#11` | `D-SP0-01`、`D-SP0-09` |
| `E.4#18` | `D-SP0-07` |
| `E.4#31` | `D-SP0-07` |
| `E.4#49` | `D-SP0-06`、`D-SP0-08` |
| `E.4#61` | `D-SP0-02`、`D-SP0-06` |
| `E.4#65` | `D-SP0-09` |
| `E.4#66` | `D-SP0-02`、`D-SP0-07` |
| `E.4#62` | `D-SP0-01` |

机器类由 D-SP0-04/06/08 给探测出口；人工类进入 reviewer checklist，不伪装成计数器。

### 10.3 原型入口

| 入口 | 落点 |
|---|---|
| `#s4` Spec 与设计文档 | D-SP0-02、04、07 |
| `#a5` spec 准入检查 | D-SP0-04、06、07 |
| `#a6` 交付物链路 | D-SP0-02、07、10 |
| `#s1` typed 待决队列中的 spec_fix/spec_claim | D-SP0-03、06 |
| `#b1` 分级对齐 | D-SP0-02、04、06 |
| `#s0` 四层架构总览 | D-SP0-01、09 |

<!-- COVERAGE-SP0-END -->

## D-SP0-11 未决与转交

| 项 | owner | 验收门 |
|---|---|---|
| 现有文件制品导入量与脏数据 | SP0 implementer | 用例评审前给扫描结果 |
| reviewer 身份与批准权来源 | SP0+SP8 | 安全评审 |
| GOV schema migration | SP7b/GOV | delivery.verify |
| 运行性能与队列容量 | SP0 implementer+ops | 交付验证，不在设计 PASS 中升级 |
| UI 是否新增入口 | SP6 | 原型/用例评审 |

## 验收结论边界

本件 PASS 只表示 SP0 设计合同闭合，不表示 GOV 已实现、checklist 已运行或历史制品已迁移。
