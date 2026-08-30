# DingWei SP3 上下文与知识库设计 v1.9

| 项 | 值 |
|---|---|
| **Design ID** | `dingwei-sp3-design` |
| **版本** | v1.9 |
| **状态** | draft，待新协议轮换全文评审 |
| **冻结施工锚** | 组合冻结：SpecDoc `fcfec9a`；其余输入 `53e4a2aed1569eef91e4048e7da2a1cd32db3ec2` |
| **合同锚** | 本提交同批 `specs/sp3/spec.md` v1.9 |
| **合同** | `specs/sp3/spec.md` v1.9 |
| **范围** | SpecDoc §10–§13；owner_sp=SP3 的附录 E 行 |
| **主责域** | `KNO` |
| **唯一 owner / 执笔** | `zzc-chatgpt56solzhangzongchu-2642` |
| **轮换主评审** | `zzc-opus50tchangshark-2642`、`zzc-kimi30kqtuxerh-2642` |

> 本件定义 KNO 目标设计和 KNO 的 SP7b 迁移合同，不声称数据库、RLS、provider、迁移或真实上下文装载已实现。
> SpecDoc v3.72 §11.6 派发快照字段表是字段全集唯一权威源；§11.6a/§11.6b 分别定义真失败与终结/未知证据。
> v1.1：按业主裁定承认 AccessCtxRef/task/node/generation/spec_ref，并补 FAIL evidence 持久化判据；仍不接真实派发。
> v1.2：补 memory_type 持久化、坑清单晋升去标识化 evidence 门、双现网参数化抽取及 §11.4 墓碑标记。
> v1.3：重锚 SpecDoc v3.71；FAIL repro 按机器可复跑类与 gate_fail 人审门两类判定。
> v1.4：重锚 SpecDoc v3.72；FAIL 只留真失败类，cancelled/timeout/effect_unknown 迁入独立终结/未知证据合同。
> v1.5：补齐 §11.6b 前序态的派发装载：不伪造 FAIL repro，经既有 envelope_ref 类型化携带不可变终结/未知证据。
> v1.6：登记 WRK 信封新增消费侧要求与 v3.73 repro 收紧项；两项未确认/吸收前均不冒充已交付。
> v1.7：round3/4 收准 prior_state fixture 消费边界、挂账坐标与 SP2 协作行唯一性。
> v1.8：新组织由本席唯一接管；按 SP3 固定席 opus/kimi 单主轮换，最多 8 轮，第 9 轮及以上禁止；opus 对 AC-SP3-08 永久 recused，窄票替代池 qwen→sonnet 且当轮只认一张无涉票；旧票归档不转移。
>
> v1.9：闭合 N1 全量意见：评审名单单源派生、owner 禁投窄票、票据锚绑定、逐判据反测与格数棘轮；AC-SP3-08 验收行 canonical 对撞；补验收面来源映射及无涉声明的人审边界。

## D-SP3-01 范围、四维模型与域边界

### 1.1 KNO 拥有与不拥有

KNO 拥有 knowledge item/revision、四维 metadata、anchor 与生命周期、visibility grant/ref、retrieval audit、
immutable context snapshot，以及上述事实的 repository/outbox/audit 写入边界。

KNO 不拥有：

- enterprise/principal/AccessCtx/DataScopeGrant：归 IDN/SP1；
- project/task/node/gate/dispatch：归 WRK/SP2；
- skill/MCP asset 与晋升审批：归 CAP/GOV；
- post/digest/周报产物：归 COL；
- Agent runtime、会话、运行态查询：归 AGT/MSG/SP4；
- 企业库路由、RLS 与 migration runner：归 PLT/SP7a。

### 1.2 四维正交

| 维度 | 字段/取值 | 只决定 | 不得替代 |
|---|---|---|---|
| 归属域 | `domain_level=D0/D1/D2/D3` | 谁拥有、辖域多大 | visibility、生命周期、风险 L、出站 G |
| 可见性 | `visibility=public/internal/restricted` | 该域内谁能看 | domain 晋升 |
| 生命周期 | task→version→domain；active/suspect/archived | 暂存、晋升、新鲜度 | ownership/visibility |
| 记忆类型 | working/procedural/factual | 怎么存、取、清理 | domain 与密级 |

三套符号固定：风险 `L1–L3`、归属 `D0–D3`、出站 `G1–G3`。KNO schema/API/UI 不接受裸 `K0–K3`、
不以 domain 推 visibility，也不因 visibility 变更修改 domain。

### 1.3 权威事实与 fail-closed

所有 KNO command/query 接收服务端签发的 `AccessCtxRef`/`DataScopeGrant`，不信 body/query/header 自述 enterprise、
principal、owner 或 visibility。缺 AccessCtx、grant、企业库映射或授权时拒绝；不得回退全员 query、默认企业或 SystemCtx。

## D-SP3-02 KBDoc 领域模型

### 2.1 聚合

| 对象 | 主键/版本 | 核心字段 | 不变量 |
|---|---|---|---|
| `knowledge_item` | `item_id,version` | canonical content/ref、meta、source revision | append revision，不原地改历史 |
| `knowledge_revision` | `(item_id,version)` | supersedes、reason、curator、created_at | version 单调；旧版可追 |
| `knowledge_anchor` | `(item_id,version,ordinal)` | anchor_type、value、observed ref | anchors 为数组；至少一条 |
| `visibility_grant_ref` | `grant_ref,generation` | item/scope/principal/expiry/approval | KNO 不自造批准权 |
| `promotion_record` | `promotion_id,generation` | source/target domain、approval、deidentification_evidence_ref | 晋升不靠改 Meta 原地完成 |
| `retrieval_audit` | `query_id` | principal、scope、filter/result refs、reason | 不存越权正文；可复算顺序 |
| `context_snapshot` | `snapshot_id,generation` | §11.6 字段引用、resolved item versions、digest | 不可变；任务间不复用 |

### 2.2 `KBDoc.Meta`

```text
KBDoc.Meta {
  domain_level: D0|D1|D2|D3
  owner_key: stable owner identifier
  visibility: public|internal|restricted   // 输入缺失时服务端物化 restricted，存储不得为 NULL
  memory_type: working|procedural|factual  // 输入缺失时物化 factual；不得由其它维度推导
  type: SpecDoc §12.4 controlled type
  anchors[]: [{anchor_type: code|deploy|artifact|external, value, observed_ref?}]
  version: immutable item version
  updated_at: database UTC
  source_kind: design|code_extract|retro|closeout|external
  ingest_pipeline: project|curation|correction
}
```

`anchor_type` 单值字段禁止；`anchors[]` 非空且 ordinal 唯一。`owner_key` 形状随 domain 校验，D0 不允许企业内容晋升生成。
DB 自动写 UTC；调用方时间只作 observation metadata。

`memory_type` 是第四维的持久化字段：缺失时服务端物化 `factual` 并落 `memory_type_defaulted=true`；procedural 必须带
CAP asset/ref，working 只允许任务上下文/快照且不得晋升为长期 domain item。domain_level、visibility、type 均不得推出
memory_type，反向亦然。负测对任意单维变更断言其余三维不变，并拒绝「D0⇒factual」「type=skill⇒procedural」等暗推。

### 2.3 内容与引用

正文可内联有界文本；附件/大产物只存 enterprise-scoped ref+sha256+classification。运行态内容（pod、日志、实时指标、
当前物理拓扑）不允许成为 KBDoc content；需要时只保留 typed MCP query 定义的知识说明，不存查询结果。

## D-SP3-03 归属、生命周期与三条写入管道

### 3.1 domain 与晋升

| domain | owner | 分发/装载 | 晋升边界 |
|---|---|---|---|
| D0 平台 | 平台 ops | 仅 public 可 push 全体 | 只能平台自产，企业内容不得晋升 D0 |
| D1 企业 | 企业 ops | push 其辖下人仍受 visibility | D2→D1 需该域准入 |
| D2 人 | 本人，流通须 ops 核准 | 不 push，主动流通 | D3→D2 需准入 |
| D3 项目 | 项目负责人 | 项目内按需 | 起始沉淀域 |

domain 变更是晋升 command，不是 visibility 降级。平台只有一个企业时呈现可合并，存储标识仍分开。

§12.4 #7 坑清单/判断失误从 D3 晋升 D2 或 D1 前必须带 `deidentification_evidence_ref`，其证据至少记录扫描手段、
规则/依据版本、结论、执行 principal 与产物 sha256。缺证据、证据不可取或扫描仍命中人名/会话名/事故稳定标识时拒绝。
正例是去标识化后证据可复算且门方批准；负例覆盖原文人名、session 全名、事故 ID 和仅声称「已脱敏」无证据。

### 3.2 三条 ingest 管道

| pipeline | 触发/授权 | 写入 | 防旁路 |
|---|---|---|---|
| project | WRK 版本收口 ref | version summary→目标 domain proposal | 仅受权 closeout 原语；无复盘/锚拒绝 |
| curation | ops 提案+权限+评审+锚 | 外部导入/策展 item revision | 受控 curator principal；敏感内容闸 |
| correction | owner/ops 发现错漏 | 新 revision supersedes 旧版 | 禁 UPDATE 历史 content/meta |

`KNO-CMD-IngestItem` 必须带 `ingest_pipeline` 与对应 trigger/approval ref；值不属三者或 ref 不匹配即拒绝。
E.1 #98 的机器判据是所有成功 ingest 均能追到其中一条管道。

### 3.3 生命周期

任务上下文实时使用，不直接成为长期知识；版本收口生成 version summary proposal；通过 domain 准入后形成 KBDoc revision。
订正/晋升都产生新 revision 与 audit，不重写历史。KNO 不承担 CAP skill/MCP 资产审批，但 skill 与知识条目共用
同一套 domain 晋升、同一套上下文装载、同一套企业过滤/visibility 判定；CAP 只持 asset/version 与 KNO ref，不另造旁路。

## D-SP3-04 visibility、授权与继承

### 4.1 默认与放行

未显式 visibility 的 ingest 由服务端物化为 `restricted` 并记录 `visibility_defaulted=true`；不得静默 public。
装载许可为：

```text
allow = domain_scope(AccessCtx, owner_key)
        AND visibility_policy(item.visibility, principal, role, grant_ref)
```

D0 仅 public 可全量；D0 internal/restricted 与 D1/D2/D3 全部按需检索。每次 internal/restricted 放行写
`retrieval_audit(principal,item,visibility,grant/policy,query_id,recorded_at)`。

### 4.2 降级、升级与继承

- `restricted→internal→public` 每一跳须 owner/ops 批准并落账；D0 降 public 或平台基线变更走 §24；
- `public→internal→restricted` 为收紧，可直接新 revision，但仍落账；
- visibility 变更不得改变 domain；跨 domain 只能走晋升；
- 引用 restricted 的派生条目必须 restricted；引用 internal 的派生条目不得 public；多引用取最严；
- grant 过期/撤销后新查询拒绝，历史 audit 不改。

### 4.3 负测

覆盖：缺 visibility、伪造 owner、跨 enterprise、跨 principal、过期 grant、restricted→public 无批准、internal 派生 public、
visibility 变更带 domain 变更、放行不落账。deny 必须在 selector/query 前发生，query spy=0；不存在与无权不可区分。

## D-SP3-05 固化判型、权威源与企业公共库

### 5.0 §11.4 墓碑

<!-- SOURCE-SP3-§11.4: NOT_APPLICABLE-TOMBSTONE -->

`K0-sec` 已撤销，不形成目标字段、表、接口或迁移项。其原安全目的由 visibility 维度与「运行态不入知识库」承接；
正文/代码新增 `K0-sec/K0-pub` 应判旧模型回退，而不是将墓碑当待实现需求。

### 5.1 固化还是运行时取

判据是「变后旧值是否误导」：仍是有效历史则固化；旧值会误导则运行时取。定义/契约与当前实例分别判断：

- 产品、业务/服务架构、逻辑数据/网络、迁移历史、坑清单、运维预案、版本志、流程定义、接口契约、领域不变量固化；
- 当前物理拓扑、流程实例、代码位置、部署版本/动态 schema、对象实例/数量、物理表结构运行时取；
- 实现细节若影响正确性，只上浮「开发应遵守什么」，不写当前 IP/副本/代理链。

每条 ingest 记录判型依据；机器可拦运行态词族/来源，边界判断由门方审，不能以 lint PASS 代替语义判定。

### 5.2 四类权威源

设计文档、代码抽取、复盘、收口是四类来源；与 project/curation/correction 三条写入管道正交。
一条来源可经不同管道写入；source_kind 与 pipeline 都必须持久化，不能用一个字段兼任。

### 5.3 外部导入/链接

默认全文导入；链接须显式指定。两者都必须：

1. 绑定可反映变化的 external 替代锚；无锚拒绝；
2. 经 SP7a/§22.5 敏感内容入站闸；
3. domain 与 visibility 分别判，均不继承外部标注；
4. 标记外部来源，不得直接作施工依据。

外部锚质量由人审；默认导入会增加 suspect 复核量，计入运维容量而不是隐藏成本。

### 5.4 平台现网/企业现网的参数化抽取

抽取/导入使用统一 pipeline 与受控 `ExtractionProfile`，按可信配置注入：enterprise_id（平台态显式 platform scope）、
anchor resolver、permission domain、redaction rule version、deployment source、environment。平台现网与企业现网分别取
自己的部署锚来源、权限域与脱敏规则；不得用平台锚描述企业现网，也不得把企业内容写平台库。

每个企业只配置 profile，不复制一套抽取代码/任务。profile 来自 ops 注册表而非请求自述；缺 profile、锚 resolver 或
脱敏规则时 fail-closed。负测覆盖企业 A profile 读 B、platform scope 携企业正文、不同企业各自 fork 实现绕过统一闸。

## D-SP3-06 上下文装载与不可变 snapshot

### 6.1 权威字段与 fixture

字段全集唯一来自 SpecDoc v3.72 §11.6 表，增删以该表为准；本轮按表逐项 fixture，不接真实 WRK 派发：

| §11.6 字段 | 构造来源 | 缺失判据 | 重灌用途 |
|---|---|---|---|
| `task`/`node`/`generation` | WRK task/node/generation fixture | 任一缺失或 generation 非预期拒绝 | 标识派发对象并 fence 迟到结果 |
| `AccessCtxRef` | SP1 签发的可信主体引用 fixture | 缺失、过期、issuer/audience/generation 不符拒绝 | 固定企业轴+人轴；Agent/session/display name 不得替代 |
| `envelope_ref` | WRK immutable envelope ref fixture；前序为 §11.6b 三态时，消费侧要求信封内有类型化 `prior_state_evidence_ref`（SP2/§11.6 尚未定义，见 §11.1 映射表挂账） | 空/不可解析；三态下引用缺失、类型或 state 不符均拒绝 | 未经 WRK 确认前只用于 fixture；确认后还原派发指令、主体引用与前序不可变终结/未知证据 |
| `spec_ref` | 本次执行所据 spec version ref fixture | 不可取或版本/指纹漂移拒绝 | 固定目标、范围与输入语义 |
| 原交付物锚 | 上轮 `result_ref+sha256` fixture | 任一缺失/sha 不符拒绝 | 取得待修/待审产物 |
| 完整 FAIL evidence+失败最小复现 | 真失败取 §11.6a evidence fixture；前序为 §11.6b 三态时显式写 `N/A:not_failure` | FAIL 场景对象缺失或任一必需子字段不成立；非失败三态留空或伪造 repro 均拒绝 | 真失败空上下文复跑；非失败三态改由 `prior_state_evidence_ref` 复核，不伪装 FAIL |
| acceptance 锚 | 判据 commit/blob sha fixture | 不可取/指纹漂移拒绝 | 固定验收标准 |
| 规则版本（权威名=`GateSet version`） | GOV GateSet version fixture | 缺失/撤销/同物异值拒绝 | 固定门集与规则解释语义 |
| 上下文版本 | 各 domain 装载 version fixture | 任一已选 domain 缺版本拒绝 | 复算装载结果 |

SP2 是本表引用侧；不得另立第二份字段全集或复述条数。fixture 中 `AccessCtxRef` 同时作为构建授权上下文验证，
但 snapshot 只存完整性保护引用，不复制其内部身份字段。

前序结果采用判别联合，不新增 §11.6 顶层字段：`previous_outcome.kind` 属 §11.6a 三类时，FAIL evidence ref 必填且
`prior_state_evidence_ref` 必须不存在；属 §11.6b 三态时，FAIL evidence 槽必须为 `N/A:not_failure`，且
`envelope_ref.prior_state_evidence_ref` 必须指向同态、同 task/node/generation 的不可变证据。两者同时存在、同时缺失或态不匹配均拒绝。

### 6.2 FAIL evidence 权威子字段

| §11.6a 子字段 | 构造/受控形状 | 不成立判据 | 持久化/重算 |
|---|---|---|---|
| `failure_class` | execution_failed/gate_fail/dependency_unavailable | cancelled/timeout/effect_unknown 或自由文本均拒绝；三者转 §11.6b | 先于 digest 写 immutable evidence version |
| `expected`/`actual` | 各一份可复算值、计数或 sha256 | 缺任一，或仅写「不符合预期」拒绝 | 门方按同输入重算对撞 |
| `repro`（execution_failed/dependency_unavailable） | 可复跑命令+不可变输入锚 | 空，或空上下文跑不起来则不成立 | 记录实际重跑 rc/摘要与 evidence |
| `repro`（gate_fail） | 门方判定所据全部不可变锚：commit/blob sha/envelope_ref/acceptance 锚 | 锚集缺项、任一不可解析，或 expected/actual 不可复算则不成立 | 记录锚解析结果与门方 evidence；不要求另一 principal 得同 verdict |
| `artifacts[]` | 日志/产物/诊断 ref+sha256；确无则显式「无」 | 留空、裸路径、无指纹拒绝 | 只存引用，正文不内联敏感日志 |
| `at_generation` | failure generation+evidence_version | 任一缺失或与 node/gate generation 不符拒绝 | fence 迟到结果与 evidence supersedes |

判定失败时先落完整 evidence，再生成 digest；未落齐不得封存或放行。evidence 正文不可改，更正新增 version+supersedes。
本表只判证据形状、机器类复跑性与人审类锚完整性，判不了失败诊断是否正确；后者归门方/执行方人审。

### 6.3 终结与未知的证据合同

`cancelled/timeout/effect_unknown` 不属于 failure_class，不要求 repro；它们各自缺必填同样不成立、不得封存：

| 态 | 必填 | 不成立判据 |
|---|---|---|
| cancelled | cancel command ref、受权 principal、reason、effect disposition（none/rolled_back/in_reconcile+锚） | 任一缺失；effect disposition=未知却无 reconcile 锚 |
| timeout | lease 状态、外部 effect 状态（已发/未发/未知）、下一步（redeliver/reconcile/terminate） | 未判 effect 状态，或无判定直接重发 |
| effect_unknown | reconcile 锚：对账对象、reconciler 句柄/游标、下次对账时点 | 无 reconcile 锚，或以「已发出」记 success |

它们不因缺 repro 而不成立；判据须对准被判物。证据正文同样 immutable，更正新增 version+supersedes。
该证据以类型化 immutable record 持久化，并由下一次派发信封的 `prior_state_evidence_ref` 携带；引用只证明记录完整、同态且同代，
不把取消、超时或 effect_unknown 改写成失败，也不把 reconcile 判断质量冒充机器结论。

### 6.4 构建、清空与不可变性

新 Spawn 是新一轮上下文装载，与会话进程是否长活无关。`BuildContextSnapshot`：

1. 验 AccessCtx/DataScopeGrant、§11.6 fixture，并按 `previous_outcome.kind` 二选一验证 §11.6a FAIL evidence 或 §11.6b `prior_state_evidence_ref`；
2. 装当前版本摘要和本任务信封；
3. 对 KNO 内容先 domain∩visibility 过滤后检索；
4. 固定 item_id/version、source_high_water、规则/上下文版本并计算 digest；
5. 写 immutable snapshot，再返回 ref；后续 UPDATE/DELETE 拒绝。

运行态不灌；D0 只全量 public；其余检索式。修订 node 必须用 snapshot 重灌，OriginHint 只作调度权重。

### 6.5 充分性与负测

字段齐只证明结构齐，不证明信息足。人审须以空上下文 worker 重灌完成任务为判据。负测：删每个 §11.6 字段、
篡改 snapshot、复用上一任务 snapshot、restricted 全量装载、运行态注入、锚漂移、规则版本撤销，均须拒绝或重建。
FAIL evidence 逐项变异 failure_class、expected/actual、repro、artifacts、at_generation。execution/dependency 的 repro 空/命令不可复跑必须失败；
gate_fail 锚集缺项/不可解析或 expected/actual 不可复算必须失败，但不得要求不同 principal 复判出同一 verdict。
§11.6b 另逐态删除 cancel command/effect disposition、timeout lease/effect 判定、effect_unknown reconcile 锚；
并做反向用例证明三者不因缺 repro 被拒，防又塞回 failure_class。另钉四条装载负测：三态证据引用齐且 FAIL 槽为
`N/A:not_failure` 可通过；FAIL 槽留空拒绝；FAIL evidence 与 `prior_state_evidence_ref` 同时存在拒绝；引用 state 或 generation 不符拒绝。

## D-SP3-07 anchors 与新鲜度状态机

### 7.1 四类锚

| anchor_type | value | suspect 判据 | 典型来源 |
|---|---|---|---|
| code | commit40/blob sha | 锚落后且 diff 覆盖所述路径 | 代码抽取/接口实现说明 |
| deploy | component×environment×commit×artifact sha | 对应部署锚变化 | 基于现网的条目 |
| artifact | artifact version+digest | 所锚交付物版本或 digest 变化 | spec/设计/报告 |
| external | version/hash/snapshot id | 替代锚发生变化 | 企业外部源 |

权威源含设计文档的固化类型必须 artifact 锚；外部源必须 external 锚；基于现网必须 deploy 锚。部署锚来自 WRK 收口，
交付物锚来自交付物链路，不另造来源。

### 7.2 多锚与状态

多锚取 OR：任一锚触发即 `suspect`，禁止要求全部同时变化。状态只允许 `active/suspect/archived`：

- active：全部锚按各自判据匹配；
- suspect：任一锚触发，记录 trigger anchor/evidence/version；
- archived：连续两个 item/version 仍 suspect；不按时间过期。

`MarkLifecycle` 只能写新 lifecycle record，不删除 item/revision。reverify 恢复 active 必须带新证据，不能手工清标。

### 7.3 omissions 与外部锚

外部 manifest 至少含 count、source high-water、previous root；裸 root 不能发现生成前少放条目。DB 管理员与锚提交权限
不应同凭据；独立性未落实时只承诺篡改可检测的有限威胁模型，不宣称防恶意管理员。

## D-SP3-08 先过滤后检索与排序

### 8.1 查询流水线

```text
AccessCtx/DataScopeGrant
 → enterprise repository selection
 → domain scope ∩ visibility authorization selector
 → authorized candidate relation
 → text/vector retrieval
 → anchor lifecycle join + suspect penalty
 → stable sort/cursor
 → redacted hits + retrieval audit
```

selector 前拒绝时 retrieval/query spy 必须为 0。deny 返回 `NOT_FOUND_OR_HIDDEN`，不得以 HTTP 200 空 hits 冒充授权拒绝。
真实授权集合内无命中才返回成功空集。

### 8.2 排序与 cursor

`score=w_use*normalized_log_usage+w_rel*relevance+w_imp*domain_importance`，suspect 乘配置中的降权系数；
权重来自版本化 tuning profile，不在代码散落。cursor 绑定 query digest、AccessCtx principal、scope、policy version、
source high-water 与最后稳定 sort key；任一变化 cursor 失效，不能跨 principal 重放。

### 8.3 审计与反推

audit 记录 query_id、principal、scope/policy/grant refs、候选/命中计数、item/version refs、拒绝理由，不记录无权正文。
负测比较存在/不存在的拒绝状态、体积与时延，防通过摘要、计数、排序或缓存键反推 restricted 条目。

## D-SP3-09 四个 KNO 接口合同

### 9.1 公共 envelope

mutating command 带 request_id、AccessCtxRef、DataScopeGrant、expected `version`/snapshot generation、source/approval refs；
服务端派生 enterprise/principal/DB UTC。相同 request_id+同 payload 返回原 outcome；载荷不同报 `IDEMPOTENCY_CONFLICT`。

### 9.2 接口目录

| ID | 输入/输出 | effect/tx | auth/幂等/代际 | error/audit |
|---|---|---|---|---|
| `KNO-CMD-IngestItem` | content/ref+Meta+pipeline refs→item revision | KNO tx 写 item/meta/anchors/audit | curator/closeout principal；request_id+version | 无锚/旁路管道/非法visibility/memory_type拒绝；缺失分别物化restricted/factual |
| `KNO-QRY-Search` | query+scope+cursor→filtered hits | 受控 repository 只读 | AccessCtx+grant；cursor绑定principal/policy | deny非成功空集；记retrieval audit |
| `KNO-CMD-BuildContextSnapshot` | v3.72 §11.6 fixture+§11.6a/§11.6b evidence（适用时）+role→snapshot ref | KNO tx 写immutable snapshot+audit/outbox | service+AccessCtx；request_id+task generation | 字段/对应证据缺失；FAIL repro无效或终结/未知必填不齐拒绝 |
| `KNO-CMD-MarkLifecycle` | item/version+target+evidence→record | 新 lifecycle record，不改历史 | verifier/curator；request_id+version | 非法转换/证据不足/stale version |

### 9.3 错误词表

`INVALID_ARGUMENT`、`FORBIDDEN`、`NOT_FOUND_OR_HIDDEN`、`STALE_VERSION`、`IDEMPOTENCY_CONFLICT`、
`ANCHOR_REQUIRED`、`ANCHOR_SUSPECT`、`INVALID_LIFECYCLE`、`DEPENDENCY_UNAVAILABLE`、`SNAPSHOT_INCOMPLETE`。
坑清单晋升缺少或未通过去标识化证据返回 `DEIDENTIFICATION_REQUIRED`。
数据库/adapter 错误不伪装业务拒绝；拒绝审计不含越权内容。

### 9.4 事件边界

目标架构没有 owner=KNO 事件，本件不新增 `EV-KNO-*`。KNO 消费 `EV-WRK-NodeDispatched` 触发受权 snapshot 构建；
构建结果经 command response/ref 与 repository outbox seam 交付。未来需要 producer event 必须先改目标架构并重审。

## D-SP3-10 KNO schema 与 SP7b

### 10.1 驻留、schema/约束/index

KNO 采用两类受控驻留，不做跨企业全文索引：

- D0 平台自产 catalog 驻平台库，只含平台知识；平台库禁止企业业务正文；
- D1–D3 item/revision 驻对应企业库，平台面只见不含正文的健康/水位；
- context snapshot 驻对应企业库，引用 D0 item 时只存已授权 item/version/ref，不复制 D0 restricted 正文到平台全局表；
- 查询先分别形成已授权 D0 候选与企业候选，再在 KNO application 层合并稳定排序；不得跨企业库拼正文。

KNO schema 包含 item、revision、anchor、visibility grant ref、lifecycle record、retrieval audit、context snapshot、
snapshot item、domain outbox/audit。企业库所有人层表含 owner_principal；应用显式谓词为主守，RLS 为漏谓词兜底。

约束：受控四维枚举（含 memory_type）、anchors 非空/ordinal 唯一、visibility/memory_type 服务端默认后非空、version 单调、snapshot immutable、
request_id 唯一、状态转换合法。index 先服务 domain/owner/visibility selector，再服务检索；不得建跨企业全文索引。

### 10.2 migration/cutover/rollback 五列合同

| 被测对象 | 输入 | 期望 | 证据形态 | 验收门 |
|---|---|---|---|---|
| KNO DDL/RLS | 空企业库+schema version | 表/约束/index/policy 齐 | schema dump+越权负测 | SP7a conformance+SP3 delivery |
| D0 catalog DDL | 空平台库+schema version | 仅平台自产知识表；企业正文列/行0 | schema dump+跨域负测 | 平台治理+SP3 delivery |
| fsgrep inventory | 现有 source/content/meta | 每件分类为 migrate/quarantine/runtime-reject | manifest+理由 | curation review |
| metadata backfill | 可迁 item+owner evidence | 四维（含memory_type）、anchors、source/pipeline 齐 | before/after keyed diff | semantic migration gate |
| dual read | fsgrep+KNO repository | 授权内结果可解释；无越权回归 | query corpus+diff | search conformance |
| write cutover | generation fence | 新写仅 KNO repository；旧 ingest 拒绝 | mutation negative test | release approval |
| rollback/forward fix | backup ref+cutover point | 可恢复旧读或前滚，不丢 revision/audit | rehearsal report | ops+SP3 gate |

不可逆步骤先备份/恢复演练；脚本成功不等于 domain/visibility/anchor 语义正确。迁移 runner 由 SP7a，领域判据由 SP3。

## D-SP3-11 现状复用、差距与 conformance

### 11.1 映射

| 现状单元 | 状态 | 目标处置 | conformance |
|---|---|---|---|
| `KBProvider` port 形状 | adapt | 改 typed Search/Ingest contracts，不暴露 provider | provider替换不改变授权/生命周期 |
| `FSGrepProvider` 文件存储 | retire（目标写面） | 仅作迁移输入/测试 fixture | 路径穿越与原子写测试保留为迁移侧参考 |
| 子串计分 | adapt/可选 fallback | 只在已授权候选集内运行 | 先过滤；suspect 降权；稳定 cursor |
| `KBDoc.Meta json.RawMessage` | rewrite | 结构化 Meta/anchors/四维约束 | 缺字段、错枚举、多锚负测 |
| `handleKBQuery query=全员` | retire | AccessCtx+grant+受控 repository | 跨企业/跨人/visibility 负测 |
| coordinator-only ingest | rewrite | 三管道+真实批准权，不按角色字符串 | 提案者/批准者、旁路管道负测 |
| KNO repository/schema | new | 企业 PG+RLS+outbox/audit | SP7a/SP7b conformance |
| `envelope_ref.prior_state_evidence_ref` | new / 待上游确认 | 本件对 WRK 派发信封的新增消费侧要求；SP2 与 SpecDoc §11.6 均未定义，已登记交 coordinator 转 SP2/WRK 确认；未确认前只用 fixture | SP2/WRK 确认字段形状、同态同代校验与版本兼容后方可接真实派发 |
| SpecDoc v3.73 机器类 repro 收紧句 | adapt / 挂账 | 「不得以重发带外部副作用的操作充当复现」随 SP3 下一批或实现批次吸收；本件冻结锚仍为 v3.72 `fcfec9a` | 实现批次补带副作用命令拒绝负测，未吸收前不得宣称满足 v3.73 |

### 11.2 conformance suite

用例覆盖：四维正交、三管道、引用继承、四锚、多锚 OR、生命周期、先过滤、cursor fencing、snapshot 逐字段、
provider error、崩溃恢复、迁移/rollback。每项至少正反例；变异须落在判据效果出口，不以任意 rc 非零当抓住缺陷。

现有 tests 只证明文件 provider 的 RBAC/路径/排序形状，不证明企业隔离、visibility、anchors 或 context snapshot 已实现。

## D-SP3-12 追踪、验收与证据

### 12.1 证据出口

- coverage checker：预期/实检/未知集合；
- KBDoc schema 与四维/anchors constraint 负测；
- 三管道 ingest 与 revision audit；
- visibility selector/query spy 与继承负测；
- §11.6 逐字段 snapshot fixture、不可变与空上下文重灌；
- 四锚 lifecycle、多锚 OR、suspect 降权；
- 四接口 contract cases；
- SP7b migration manifest、dual-read diff、cutover/rollback rehearsal。

### 12.2 结构化覆盖

<!-- COVERAGE-SP3-BEGIN -->
| 来源 | 设计落点 |
|---|---|
| `SRC:§10` | `D-SP3-01` |
| `SRC:§11` | `D-SP3-03` |
| `SRC:§11.1` | `D-SP3-01` |
| `SRC:§11.2` | `D-SP3-01`、`D-SP3-03` |
| `SRC:§11.3` | `D-SP3-03` |
| `SRC:§11.4` | `D-SP3-05` |
| `SRC:§11.4a` | `D-SP3-04` |
| `SRC:§11.4b` | `D-SP3-04` |
| `SRC:§11.5` | `D-SP3-04`、`D-SP3-06` |
| `SRC:§11.6` | `D-SP3-06` |
| `SRC:§11.6a` | `D-SP3-06` |
| `SRC:§11.6b` | `D-SP3-06` |
| `SRC:§12` | `D-SP3-02` |
| `SRC:§12.1` | `D-SP3-05` |
| `SRC:§12.2` | `D-SP3-05` |
| `SRC:§12.3` | `D-SP3-05` |
| `SRC:§12.3a` | `D-SP3-05` |
| `SRC:§12.4` | `D-SP3-03`、`D-SP3-05` |
| `SRC:§12.5` | `D-SP3-05` |
| `SRC:§12.6` | `D-SP3-05` |
| `SRC:§12.7` | `D-SP3-05` |
| `SRC:§13` | `D-SP3-07` |
| `SRC:§13.1` | `D-SP3-02`、`D-SP3-07` |
| `SRC:§13.2` | `D-SP3-07` |
| `SRC:§13.3` | `D-SP3-07` |
| `SRC:§13.4` | `D-SP3-08` |
| `SRC:§13.5` | `D-SP3-03`、`D-SP3-09` |
| `E:E.1#10` | `D-SP3-08` |
| `E:E.1#11` | `D-SP3-08` |
| `E:E.1#38` | `D-SP3-06` |
| `E:E.1#78` | `D-SP3-05` |
| `E:E.1#79` | `D-SP3-05`、`D-SP3-07` |
| `E:E.1#80` | `D-SP3-07` |
| `E:E.1#81` | `D-SP3-06` |
| `E:E.1#98` | `D-SP3-03` |
| `E:E.1#117` | `D-SP3-04` |
| `E:E.1#118` | `D-SP3-04` |
| `E:E.1#127` | `D-SP3-04` |
| `E:E.1#128` | `D-SP3-04` |
| `E:E.1#129` | `D-SP3-04` |
| `E:E.1#130` | `D-SP3-04` |
| `E:E.1#131` | `D-SP3-07` |
| `E:E.1#136` | `D-SP3-04` |
| `E:E.1#137` | `D-SP3-04` |
| `E:E.1#138` | `D-SP3-04` |
| `E:E.1#139` | `D-SP3-01`、`D-SP3-05` |
| `E:E.1#61` | `D-SP3-06` |
| `E:E.1#62` | `D-SP3-06` |
| `E:E.3#5` | `D-SP3-05`、`D-SP3-06` |
| `E:E.3#10` | `D-SP3-06` |
| `E:E.3#34` | `D-SP3-02` |
| `E:E.3#38` | `D-SP3-01` |
| `E:E.3#44` | `D-SP3-02`、`D-SP3-07` |
| `E:E.3#47` | `D-SP3-01`、`D-SP3-02` |
| `E:E.4#19` | `D-SP3-06` |
| `E:E.4#28` | `D-SP3-07` |
| `E:E.4#41` | `D-SP3-05` |
| `E:E.4#42` | `D-SP3-05` |
| `E:E.4#43` | `D-SP3-05`、`D-SP3-07` |
| `E:E.4#50` | `D-SP3-04` |
| `IF:KNO-CMD-IngestItem` | `D-SP3-09` |
| `IF:KNO-QRY-Search` | `D-SP3-08`、`D-SP3-09` |
| `IF:KNO-CMD-BuildContextSnapshot` | `D-SP3-06`、`D-SP3-09` |
| `IF:KNO-CMD-MarkLifecycle` | `D-SP3-07`、`D-SP3-09` |
| `UI:#s3` | `D-SP3-04`、`D-SP3-08` |
| `UI:#a2` | `D-SP3-06` |
<!-- COVERAGE-SP3-END -->

### 12.3 验收判据

<!-- ACCEPTANCE-SP3-BEGIN -->
| ID | 受检范围 | 判据 | 判不了什么 |
|---|---|---|---|
| `AC-SP3-01` | checker 派生 §10–§13 heading | COVERAGE 逐项 D 落点，未知0 | 落点语义正确性 |
| `AC-SP3-02` | owner_sp含SP3的附录E | 逐项语义落点，未知0 | 人工判据质量 |
| `AC-SP3-03` | 架构§6 KNO接口 | 4接口逐项落点，无跨表污染 | 字段语义充分性 |
| `AC-SP3-04` | 架构§13 KNO原型入口 | 逐项落点且冻结原型存在 | UI实现状态 |
| `AC-SP3-05` | 四维模型与KBDoc.Meta | 四维不互推，字段/枚举/默认/约束齐 | 数据已迁移与否 |
| `AC-SP3-06` | 三条ingest与revision | 成功写入均追管道；订正不改历史 | curator判断质量 |
| `AC-SP3-07` | visibility授权/继承 | 默认restricted、降级审批、继承取严、放行审计齐 | 定级是否恰当 |
| `AC-SP3-08` | v3.72 §11.6/§11.6a/§11.6b逐项 | execution/dependency命令可复跑；gate_fail锚集有效；cancelled/timeout/effect_unknown各自必填齐且 FAIL 槽显式 N/A:not_failure，不误套repro；envelope_ref.prior_state_evidence_ref 仅按 fixture 消费合同判定——该字段冻结 SpecDoc §11.6 未定义，真实装载列为集成前置挂账（见 §11.1 映射表） | 诊断/effect判定正确性归人审；prior_state_evidence_ref 的真实装载、上游字段形状与版本兼容不在本件判定面内 |
| `AC-SP3-09` | anchors/lifecycle | 四锚分判、多锚OR、连续两版本archived | 替代锚质量 |
| `AC-SP3-10` | 检索全链 | 先过滤、suspect降权、deny非空集、cursor隔离 | relevance质量 |
| `AC-SP3-11` | 4接口与复用映射 | auth/idempotency/generation/tx/error/audit及正反例齐 | provider性能 |
| `AC-SP3-12` | SP7b五列合同与证据 | 对象/输入/期望/证据/门齐；设计PASS不升级实现PASS | 实际cutover/rollback |
<!-- ACCEPTANCE-SP3-END -->

### 12.4 挂账与冲突

1. §11.6 与 SP2 §3.3 差异已由业主裁定并在 v3.70 闭合：§11.6 承认 SP2 字段且保持唯一权威；本轮仍只用 fixture，不接真实派发。
   `envelope_ref.prior_state_evidence_ref` 的真实集成挂账落在本文 §11.1 映射表；本节只保留指针，不复制第二份事实。
2. SP1 冻结件头仍 draft，而 coordinator 已告知收口：设计消费 seam，实现门须补可取 verdict/负测证据。
3. SP7a 未交付：先过滤后检索、RLS、migration runner 只写合同，不宣称已具备。
4. KNO event 面为 NOT_APPLICABLE；正文不产出 `EV-KNO-*`，未来新增先改目标架构。
5. 外部锚权限独立、真实 PG migration/rollback、检索质量与容量均须实现期验证。
