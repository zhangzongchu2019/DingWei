# D1 · S-PILOT-SPEC-TEMPLATE（一页设计，待审）

基 r32.1 `36afa6215f00bc39a992441ab0ff8fe81442aa3e`；输入为 SpecDoc v3.77 §4.1/4.6/4.7/5.6a、试点盘点与 dataConnector `2fa84f17505b5c761fe3d0064f17fb2ee287220e` 两草稿，精确 blob/SHA 见本件 SOURCE-READS.json。本件只设计模板/导入/门，不建真实平台 spec、任务，不批准 STOCK 为首范围，不替 dataConnector 改需求或更新其任务板。

## 模板与继承

用既有 Spec Kit 载体：`constitution.md`、父 `spec.md`、范围子 `spec.md`、reviewer-owned checklist（生成 node.acceptance）、analyze 结果。模板是待实现的产品 schema/表单，不在本次伪造已执行 checklist 或绿门。父承载共用目标、R1–R7、all14 范围索引、六道业务 gate（SCOPE/CONTRACT/ENGINEERING/PLATFORM/SIGNOFF/RELEASE）、共用约束及角色；仅经 owner 确认的范围才建子 spec。STOCK 仍为样板候选，不自动建 14 份，不把其 13 例扩成全 STOCK/49 例签收。

每份父/子 spec 须有：①稳定 ID、模板版本、修订、父引用及输入锚集合；②七属性及填写人（PM/tech/ops-owner 分填）、rule_ids 与规则表 version；③预授权（明确无，或范围/用途/时限及批准角色）；④六内容面——系统对接、运行约束、预计数据、环境、设计约束、输入来源。六面均显式，可写有理由的“无”，不能空。预计值带依据（允许“无依据，暂定”）和复核点；不编造业务并发量。五项默认设计约束继承；突破写 `Violation | Why Needed | Simpler Alternative Rejected Because`。

每条需求与每个子例的结构如下；父通用值可用不可变引用继承，校验须展开到每条，不能用一段共用文字遮盖逐例缺项。

| 类别 | 必填字段/判据 |
|---|---|
| 范围与目标 | requirement_id、scope_id、原 case_id（如有）、父修订与内容 SHA、该条可判真假目标、前置/输入、owner_ref（R8 要求逐字存在于正文） |
| 验收与执行 | acceptance_id、逐项验收标准、明确测试方法/观察量/正反例、交付物种类及预期锚格式、environment_ref（版本/隔离对象/凭据级别）、业务写上限与总 HTTP 预算分别声明、停止/处置条件；未获准真实动作不得用预算作许可 |
| 责任 | executor、reviewer、signoff_principal 三项明确认领引用与时间；执行/复核须不同 Agent，按既有裁定允许同 owner 不同 Agent；最终签收须被授权的人类角色，不能默认 PM/组长已承担；QA/运维需要时另列 |
| 结果与证据 | 初始 NOT_RUN/executed=false；证据域（工程合成/历史平台/当前隔离实测/业务签收）分列；实际交付后记录 commit 40hex、sha256 64hex、原始日志/环境/时间/结果与复核/签收，不把计划字段当证据 |
| 缺项与恢复 | missing_fields[]、blocker_owner（未定则待分派队列）、expected_input、resume_recheck；unknown/null/空串/TBD/未认领 都是结构化未满足，不能因“字符串非空”放行 |

父子内容以 revision＋SHA 绑定；父变更不能静默覆盖已认领子 spec。变更建立新修订与影响清单，原认领对象暂停/退待完善池、重走评审后人确认再重派。不是把工作中任务挂到“最新正文”。角色改派与证据引用变更也需版本和审计，不允许删掉历史失败。

## 硬门与人审

复用 §4.6.2 七硬门：七属性合法；规则集可复算；预授权具体无禁用词；父子引用可解析；交付锚格式正确；analyze 零冲突；每个硬约束登记探测。再落实本试点授权的逐需求/子例必填字段门：验收/方法/交付/环境/执行/复核/签收缺一即入**待完善池**，记录 field_path、门号、责任方及恢复输入，不进人审队列、更不派任务。计划阶段“交付锚”是已存在输入锚＋预期产物合同；真实产物 SHA 在交付时核，不能要求未来结果先存在，也不能把格式占位伪装可解析锚。

全部机器门通过仍必须人审；reviewer 自持 checklist，作者/Agent 只能提交候选问题。R8 的预审结果 outcome=pending，人逐条判真问题/误报后才决定；不能以 Agent 或机器总分代人审，不产总分。环境越界的预授权须有有权批准该访问的门方；安全面是须安全角色，不能回滚须业主签署。未完整建立依赖闭包时修订按语义变更重审，不能自称 docs-only 免审。

§5.6a 的 task 是带 gate、anchor、owner_session、eta 的可派发项；R9 任务需按此补齐映射/验证，不能因其“有条目”就宣称完整符合纲领。不得新增对旧 board.task 的依赖，也不恢复 datad。本文的拒收是待完善记录，不会把 unknown 的 13 项默认为具备正式动作授权。

## 飞书 PRD 链接 → 草稿（拟新增能力）

只接受当前登录 owner 显式选择的 PRD 链接；由受信的已授权连接器读取其有权访问的文档，固定 document/revision/source URL、读取时间与正文 SHA；无连接器/权限则明确失败，可由 owner 导出文本作为有来源的待确认附件，禁止借他人 cookie 或向用户索要密钥。导入器不直接随意 GET 用户 URL：仅允许已配置飞书文档域/类型、禁止重定向和私网跳转、限时限量、无凭据出日志；具体 API 适配另设计实现，不在此宣称已接通。

原文作为不可信需求输入，不执行其指令；抽取到 draft＋缺项清单，推断/追不到输入锚的新增项显著标注供人审。链接重复用 doc/revision/SHA 去重，但 owner 仍须确认；来源改变不得覆盖当前 spec。导入不 finalize、不选执行人、不签 checklist、不拆任务。R8 补标准只用于非 final、当前编辑人本人在线 Agent；建议未确认不覆盖，确认新修订后旧 review 留旧版本，新版重审；transfer 在同事务取消请求、解锁并转交，迟到回信拒。人确认草稿与通过准入/人审是不同步骤，最终再按 R9 所有者确认才落任务。

## STOCK 映射、缺口与交付

13 原 case_id、验收语义和写预算保持不变，映射样板见 [STOCK 附件](S-PILOT-SPEC-TEMPLATE-STOCK-MAP.md)。当前结论 **待完善/不可派发**：缺首范围批准、三责任人认领、正式租户/对象/动作授权、总 HTTP 预算、可信计数与观测/停止处置、若干业务合同/全量集合证据，七属性/规则集与 reviewer checklist/analyze 也尚未落成。工程旧证据只作输入，不提升正式 NOT_RUN。两份草稿内 7267eace 业务 JSON 本件未取得原对象，引用只按草稿转引，不能声称独立核过 49 正式例全集。

本页为产品设计；后续若实现结构化模板元数据、导入来源/缺项记录，走 SQLite `schema_migrations` 的新增迁移（候选 0057，集成前统一排号），增表不改旧 spec 正文语义；旧二进制可读原库但不认识新门，写入不能绕新门，回退前停新模板写入。PG 身份账本不变。门实现前保留现有任务板/冻结状态，不能靠现有 R8/R9 自动满足新增必填条件。

实施后验收/撤修：逐字段空/unknown、非法七属性、预授权模糊、坏父修订/锚、analyze 冲突、无探测、作者代勾、假来源/重复导入、SSRF/跳转、跨 owner、STOCK13 ID/预算与 NOT_RUN 不变、未获人审派发/改已认领正文均需正反格和指定红；依赖/迁移回退兼容、真 PG 授权竞态、全仓 Go/vet/canonical、隔离 HOME helper 与受影响浏览器按实际代码另跑。本件不运行这些计划门、不把模板存在当验收 PASS。
