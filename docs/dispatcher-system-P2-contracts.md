# 调度者/任务清单编排系统 · P2 契约文本(C0-C6,v1.1——dev56 fe49ba5 完整复审 MC-A/MC-B 修(C3.4a/gate-2 和谐/C6.2 新原语诚实/C5.1a 门方与收口权/C6.1 to_owner))

作者 zzc-devo48-2642 · 2026-07-23 · 长程任务 P2 段(冻结对象;两签后 C1-C5 即契约,改动须重两签)
上游=P1 设计稿 v1.1(sha 9798c663);本稿把 §7 骨架落成可签文本。
v0.2 修:shence 审#1(MC-1 身份真实性锚/MC-2 状态 op 授权闸/MC-3 应急幂等 epoch;A-1~A-4)+dev56 审#2(①C2 漏 2-Agent 守门/②应急原语非零新语义须诚实立原语/③合法审计 actor 悖论/④canonical 首行与可信 control op)一次攒批;应急原语升为 **C6 新原语**(纳入本次两签,C1 引用面恢复"纯引用零新语义"的诚实性)。
v0.3 增:shence H1(C0 授权模型合章)+H2(过渡窗敞口)。
v1.1=dev56 fe49ba5 完整复审两新洞:MC-A(C0.4"普通控制 best-effort"与全 fail-closed 自相矛盾→过渡面统一仅身份 warn/盘点/只读,一切 control fail-closed)+MC-B(控制判定仅 meta.op∈ControlOps,自然语言正文绝不重分类,反例 meta.op=status+"需暂停部署"合法不误拒;疑似→LLM/重投不执行不误拒)。
v1.0=dev56 逐字复审 5 处正文矛盾修(非摘要,git show 可核):①C3.4a 普通控制过渡期改 fail-closed(对齐 C5 5.0,不留 best-effort 伪造控制口);②gate-2 自然语言控制=疑似即从严(与 5.0a 和谐);③C6.2 owner_override 诚实为 P3 新原语(非"走引擎既有 guarded 原语"——那签调度者 actor 与"actor 业主"矛盾);④C5.1a 门方=每段指定门方(C2.1)非硬编码 tester,收口=调度者唯一关闭权(P1);⑤C6.1 schema 补 to_owner。
v0.9=+manager 折入两项:②C5 gate-2 应急豁免须正向 owner principal 校验(消"标应急即绕过"后门=业主集∧走 C6,二者皆真)+③C4.3 盘点显式纳临时 *note/裸名发信名(enforce 后自伤面)。(①MC-4 fullmatch 已在 v0.6+。)
v0.8=+dev56 provisional 硬边界(★DF-1 深化,C0.4③a):过渡期 provisional 心跳只进隔离非权威 observation 区,不参与任何状态机/计时器/决策(防伪造心跳持续阻止 failover=权威影响),复核后新 request_id 转正;最简替代=心跳也 fail-closed。
v0.7=dev56 FINAL 6MC 一次全闭合(DF-1∪MC1 合并):①过渡窗【所有 mutating】fail-closed=C5 5.0/C0.4③;②role×op+指定门方矩阵=C5 5.1a;③OwnerOverride 专用原语【禁注入 dispatcher Identity】=C0.2/C6 6.2;④owner 请求 schema+expected epoch/state+陈旧不跨代=C6 6.1/6.2;⑤canonical control gate+内部 sentinel 保留=C5 5.0a;⑥fullmatch+拒控制字符=C4.1。
v0.6=shence 审#1 2/2 收尾:MC-4(C4.1 fullmatch/\A\Z+拒控制字符,尾换行注入闭合)+DF-1(过渡期门/收口/result=fail-closed 不入账[防伪造假 PASS,与 C0.4 应急一致],低危心跳 provisional+敞口③枚举;C0.4/C5 5.0/C3.4a 同步)。
v0.5=dev56 7 点整合清单全闭合对照(manager 定为唯一 checklist):①C2 执行席≤2/升格/豁免=C2.6;②身份真实性 hub principal(from 不作授权)=C0.1/C5 5.0;③状态 op 入账前 principal↔task_role+new 创建权=C5 gate-3;④应急 owner 鉴权/expected_epoch/idempotency/重放 no-op/线性化+事件原子+P3 新语义窗=C0.2-0.6/C6/C1 1.4b;⑤identity_rejected 合法 gate 签名/被拒 name 仅 subject=C4 4.2;⑥canonical 首行/可信 meta/control 语法+三负例=C5 5.0a;⑦三跨边界不变量(P5 原子切/每实体同构授权闸/LLM 只判路由)=C3.4a·C3.3·C5 gate-5;**+全状态 op 幂等(非仅 emergency)=C0.3**。
v0.4 增:dev56 审#2 定稿——①H2 提级为**应急原语过渡期 fail-closed**(共享钥可伪造夺权=破坏安全不变量,风险接受消不掉,C0.4)②竞态线性化(takeover↔override 先提交者决定+同事务+重放返原 outcome,C0.5)③override directive 白名单(C0.6)④C1 两层口径(1.4a 现有全集 / 1.4b P3 新 owner emergency 扩展,勿总称零新语义)。

---

## C0 授权模型(★shence H1:身份/权限单一真相,C4/C5/C6 引用本章,不分处补丁)

三者同属一个模型,一处定义方自洽(override 的业主身份依赖真实性锚、其 replay 安全依赖幂等——分散打补丁留缝):
- **0.1 身份真实性锚**:一切 gate/授权判定的主体=**hub 鉴权后的 per-session principal**,【非】信封自述 from(共享队钥 FB-zzc-devteam-* 下 from 可伪造)。principal 由传输层注入收件面。
- **0.2 授权双主体模型(★A-4 定义,dev56 可实现性线并置)**:
  - 常态写=调度者小组以自身 `Identity{owner,epoch}` 走 guarded 原语;
  - **业主 override 写=双 principal 授权**:业主 principal(0.1 校验)以【专用 owner 授权面】写(★dev56③:**不注入/不冒充调度者 Identity**),guard=owner_principal 合法 ∧ expected_epoch==当前 epoch;actor 签业主 principal(非调度者);不夺权、不改 owner、epoch 变则失效重取。竞态线性化见 C0.5,directive 白名单见 C0.6,请求 schema 见 C6 6.2。
- **0.3 epoch/幂等(★dev56 整合补:全状态 op,非仅 emergency)**:**一切可变更状态的 op** 必带 request_id(幂等键)+作用于当前 epoch——重投/重放=返原 outcome no-op(治 15-40min 可重投信道)。分项:
  - owner_takeover=期望 epoch CAS(e→e+1,陈旧 e no-op);
  - owner_override=idempotency_id(同 id 重投 no-op);
  - 状态入账 op(new/status/result/reject/门/收口)=沿引擎 rid 幂等(request_id∈node_request/task_request,已冻结)+经 C5 授权闸;
  - **不变量:任何状态变更提交前必满足(幂等键去重 ∧ epoch 有效 ∧ 授权闸过),三缺一拒。**
- **0.4 过渡窗——应急原语 fail-closed(★dev56 审#2 提级,严于 best-effort)**:hub canonical per-session principal 未就绪时——
  - **owner_takeover / owner_override = 硬禁用(fail-closed,不可启用)**:共享 FB key 下可伪造的夺权/急停若进过渡运行,攻击者可合法触发 epoch+1 fence 真调度者=直接破坏安全不变量,"风险接受"消不掉此破坏,故不接受、直接禁用;
  - **普通身份**:仅身份格式 warn(C4.2 warn 模式,不拒但记 identity_warned)+收编盘点(C4.3,只读)可在 pre-P5 迁移窗运行;**一切 control op = 无过渡 fail-closed**(★dev56 MC-A:删原"控制路由 best-effort",与下条③"一切 mutating fail-closed"及 C3.4a/C5 5.0 统一,不留可伪造控制口);业主接受仅"功能受限",不接受任何伪造控制;
  - **③过渡期一切 mutating 伪造(★DF-1+dev56①,统一)**:过渡期 principal 可伪造→gate-3 授权闸形同虚设→可伪造任何改状态 op(门 PASS/建单/结果/reassign…)。同一致性理由(可伪造→破坏不变量→风险接受消不掉),**处置=过渡期一切 mutating op 一律 fail-closed 不入账**(不止门/收口);仅放行只读查询。
  - **③a provisional 心跳硬边界(★dev56 补:防伪造心跳的权威影响)**:过渡期若保留心跳/状态观测,只能进**隔离的非权威 observation/audit 区**,且【不参与任何状态机/计时器/决策】——principal 复核前**不得**:刷新 agent/task 租约、抑制"静默超两间隔"失联判断、改 TaskLedger/status/blocker、满足门、驱动任何调度决策。否则伪造心跳可持续阻止 failover=仍是权威影响。复核后才以【新 request_id】转正入账。**最简替代=心跳也全 fail-closed**(过渡期不收心跳)。
- **0.5 竞态线性化(★dev56:takeover↔override 同 epoch 竞争,先提交者决定)**:
  - takeover 先提交→override 的 expected_epoch miss=no-op;override 先提交→其单指令原子生效后,takeover fence 后续写;
  - **状态变更+owner_* 事件同一事务**(原子);idempotency_id 重放=返原 outcome(不重复施加)。
- **0.6 override 白名单(★dev56)**:owner_override 的 (target,directive) 必**唯一映射到既有控制 op 白名单**(如 reassign/steer-cancel/pause 等已定义原语),【不得】自由文本直写。

## C1 调度者↔引擎(引用契约,零新语义)

- **1.1 原语集(全集,引用即冻结实现)**:CreateTask(建单硬约束:四角色三元组必填全)/AddNode/SetTaskRoles/Send/Processing/Terminal(含直达窗+fan-out)/Reassign/SteerReserve/SteerFinalize/InterruptIntent/InterruptFinalize/Redeliver/DeliverFail/EnqueueResult/AckResult/Sweep/Takeover/Renew——语义=两签冻结设计件(f41d686 家族)+引擎实现(资产头 74e5dc0,十门全绿)。
- **1.2 身份**:调度者小组=唯一持 `Identity{owner,epoch}` 的主体;一切写走 guarded 原语并签 actor;外部只读 TaskLedger/RoleStats/board_event。
- **1.3 变更纪律**:C1 引用面的任何语义变更=引擎设计变更,须另开窗重两签(本契约不授予捷径)。
- **1.4 两层口径(★dev56 审#2:勿总称零新语义)**:
  - **1.4a C1 现有原语全集(74e5dc0 冻结接口,纯引用零新语义)**:即 1.1 所列;
  - **1.4b P3 待新增 owner emergency 扩展(=C6,新 C1 语义)**:专用 owner_override + 带 owner-auth/事件/idempotency 的 takeover 包装,均【非】74e5dc0 当前接口(现有 Takeover 无 Identity/无 owner_takeover 事件/不满足均 guarded+actor,不能唯一映射);故诚实立为 P3 新语义,纳入本次两签,与 1.4a 分层不混称。

## C2 调度者↔执行小组(2-Agent 工作协议规格化)

- **2.1 派发信封(调度者→小组,建单即发)**:
  `task_id / title / 验收标准(acceptance) / 角色绑定(四角色三元组 who,cli,llm) / 里程碑分段(每段:名称+门定义+门方) / 基线锚(commit 40hex 或 blob sha256) / 交付格式引用(→2.2)`。
  缺任一字段=无效派发(执行小组拒接+回指调度者)。
- **2.2 里程碑回执(小组→调度者,每段一报)**:
  `件(路径或 commit 40hex) / sha256(最终态当场算,报后不改;改=重报) / 行数+字节(wc -c 口径) / 自测结果(命令+摘要) / 门结果(门方出:PASS|FAIL+失败最小复现)`。
- **2.3 心跳**:长段中途按调度者指定间隔报一句【在轨|阻塞(原因+需谁)】;静默超两个间隔=调度者主动查询;查询无应答=按失联处置(小组重建评估)。
- **2.4 澄清直连**:小组内(实现↔门方)澄清直接沟通,不经调度者转述;涉 scope 变更才升调度者。
- **2.5 升级链**:阻塞/需决策→调度者;调度者判属产品决策→按 C3 升业主。
- **2.6 2-Agent 守门(★dev56①/D4=a)**:派发信封的角色绑定,调度者建单时校验**活跃执行席≤2**(执行席=implementer+tester;designer/qa_owner=轻角色不占席,定义见 P1 §4.2a)。轻角色若持续产出执行中间产物→自动升格执行席→触发≤2 校验(换席或调度者显式豁免)。**豁免=调度者 guarded 写+`role_exemption{task,who,reason}` 事件落账**(可审计,不可匿名突破)。

## C3 调度者↔业主(张宗楚)

- **3.1 关键决策请求格式**:`决策项 / 选项(a,b,c…) / 各选项影响一行 / 调度者建议+理由 / 不拍时的默认行为与时限`。一次一批,禁流式追问。
- **3.2 应急原语=C6 两新原语**(D3=c;定义见 C6,均 guarded+actor+epoch 条件化+幂等)。业主(C0.1 principal 校验,非自述)专用;授权=C0.2 双主体模型;幂等/epoch=C0.3。
- **3.3 知情义务(反向)+ 控制授权闸下沉(★A-2)**:调度者对业主=事后 flag 制;业主直连执行小组的控制指令→小组拒收+回指调度者+落 directive_rejected。**控制授权闸不只在调度者收件面(C5),而在每个可接收控制指令的实体上同构生效**(执行组边界=同一 principal+task_role 判定);不变量:控制授权闸在所有可接收控制指令的实体上生效,无缝隙。
- **3.4a 过渡窗(★dev56 复审:对齐 C5 5.0,一切 mutating fail-closed 不留 best-effort 控制)**:hub principal 就绪前——**一切改状态的 op(应急原语 + 所有控制 op + new/status/result/reject/门/收口 + steer/interrupt/reassign)硬禁用 fail-closed**(可伪造 principal 下任何 mutating 都可被伪造,同 C0.4 一致理由);**仅放行只读查询 + 隔离非权威 observation 心跳(C0.4③a,不参与状态机/计时器/决策)**。**业主显式接受仅针对"过渡期功能受限(应急/控制/入账均不可用,只读+隔离心跳)"这一事实,不接受任何伪造控制/夺权/门 PASS**。**P5 上线硬门=principal 鉴权就绪 ∧ 清单清零 ∧ 两应急原语启用原子共切**(D2=a 一致)。
- **3.5 过渡/切换原子性(★A-1)**:C3.4 补丁废止 / C4.3 enforce 切换 / C3.3 例外唯一性 **同一 P5 时点原子共切**;不变量:任一时刻指令入口恰好一套规则生效——无双路径、无真空。

## C4 身份契约(地基,D2=a:上线前收编完成,上线即 enforce)

- **4.1 合法身份格式(★MC-4:fullmatch,拒控制字符)**:模式 `zzc-[a-z0-9]+-[0-9]+`,以 **fullmatch(\A…\Z,非 ^…$)** 校验——`^…$` 下尾换行 `zzc-x-1\n` 会漏过(负例复现:re.match=True / re.fullmatch=False),身份串=台账主键/actor/RegisterAgent 校验点,带尾换行/控制字符名过闸=别名+日志注入面。**额外拒**:任何控制字符/空白(含 \n\r\t)。此字符串=全体系唯一主键(task_role.who / board_event.agent_id / 信封路由 / 台账)。
- **4.2 enforce 语义(Board 侧,P3 实现)**:RegisterAgent 与事件 actor 入账时正则校验;非法名=写入拒绝+`identity_rejected` 事件。
  - **★dev56③ 合法审计 actor**:identity_rejected 事件本身的 actor【不是】被拒的非法名(否则"全事件强制合法 actor"与"记录非法名"自相矛盾)——用**系统哨兵身份 `system:identity-gate`**(保留主体,非 agent),被拒裸名记入 event.data 而非 actor 字段。不变量:每条事件 actor∈{合法 zzc-* 身份, 系统保留哨兵},裸名永不进 actor。
- **4.3 收编流程**:①盘点——hub 全量活跃会话名 **+ 临时/一次性发信名(★manager③:team 自己 send.py from=裸名 *note/developer-out 类,enforce 后自伤,必须纳盘点,非只在线常驻会话)**,产非合规清单+owner 认领②迁移(改名/重建,逐项销号)③enforce(hub 命名 warn→enforce 切换)。**切换判据**:清单清零 ∧ 迁移 100% ∧ 回滚开关就绪;时点=调度者上线里程碑(P5)前完成。
- **4.4 跨系统依赖**:hub 命名 v1.0 的 enforce 开关(hub 侧配合项,P3 排入协调)。

## C5 路由契约(正则先行,LLM 兜底)

- **5.0 身份真实性锚(★MC-1,=C0.1)**:gate 判定的发件身份=**C0.1 per-session principal**,【不是】信封自述 `from`(现行 send.py `from="{name}#{key}"` 中 name 客户端自选、key=全队共享 secret,from 可被任一持钥者伪造成业主/调度者)。C5 全部闸读此 principal。
  - **P3 硬依赖**:hub 提供 per-session 主体注入(连接层→收件面);列 P3 实现清单+跨系统协调项。
  - **过渡期不变量(hub 未就绪前,诚实降级;★dev56①:所有 mutating fail-closed)**:身份【非】不可伪造——此期间**禁用一切改状态的 op**(C6 应急 + 控制 op + new/status/result/reject/门/收口 + steer/interrupt/reassign 等所有 mutating,一律 fail-closed 不入账,防伪造 principal 写任何权威状态);**仅放行只读查询**;心跳观测(若保留)进隔离非权威 observation 区,【不参与任何状态机/计时器/决策】(不刷租约/不抑制失联判断/不改台账/不驱动调度,C0.4③a),复核后新 request_id 转正。"不可伪造性未就位 ⇒ 一切 mutating+心跳决策影响关闭",不带病放行。principal 就位后本条废止。
- **5.0a canonical 输入 + control gate(★dev56④⑤)**:闸只读**规范化首行 canonical_first_line**(剥传输头 `【sender→recipient】`/镜像标签/引号后的净正文首行)+信封 meta;传输头/body 自称控制词不参与路由。
  - **控制判定=仅结构化 meta.op∈ControlOps(★dev56 MC-B:自然语言正文绝不重分类)**:进入控制授权/执行的唯一条件=`meta.op∈ControlOps`(停/暂停/重派/改单等结构化 op 集);**status/result 的正文自然语言【不得】被重分类为控制**(反例:meta.op=status+首行"阻塞:需暂停部署,等待审批"=合法状态,含"暂停"字样也【不】进 gate-2,正常走状态入账授权闸);无结构化 control op 的疑似自然语言→仅交 gate-5 LLM 判读(或回执要求结构化重投),**既不执行、也不直接误拒合法状态**;
  - **内部 sentinel 命名空间保留(★dev56⑤)**:`system:*` 前缀(如 `system:identity-gate`)=系统保留主体,**外部发件 principal 禁用该前缀**(gate-1 对 `system:*` 来源一律拒,防伪造系统哨兵签名);sentinel 仅由 Board 内部原语在事件 actor 处使用,永不作为收件 principal。
- **5.1 处理次序(信封到达调度者收件面;身份=5.0 principal)**:
  1. **身份闸**:发件身份不合 C4 格式→拒收+identity_rejected,终;
  2. **控制闸(★manager②豁免正向 owner + dev56 MC-B 仅 meta.op 触发)**:**触发=`meta.op∈ControlOps`结构化 op(【非】自然语言正文含控制词——status/result 正文不触发本闸,走 gate-3)**;命中后放行仅当【发件 principal∈调度者小组】∨【发件 principal∈业主集(C0.1 鉴权)∧走 C6 应急路径,二者皆真】;否则拒收+directive_rejected。"应急"非自称标签放行(否则任何人标应急即绕过=后门);终;
  3. **授权闸(★MC-2,状态变更前必过)**:meta `op∈{new,status,result,reject}` 或状态首行——**发件 principal 必须 ∈ 目标 task_id 的 task_role**(new=须调度者小组身份);否则 directive_rejected+事件,终。不变量:任何状态变更(建单/门/收口/结果/reject)前必过授权闸,格式闸不替代它。
  4. **结构/状态入账**:过授权闸后——meta op→引擎既有 op 路由(C1);状态首行 ∧ task_id→状态机入账(门/心跳→TaskLedger+事件);均零 LLM,终。
  5. **兜底(★A-3)**:升调度者 LLM 判读——**LLM 只出路由判读、不直接授权/触发 guarded 写**(判读结论落 `route_llm{verdict}`,再经授权闸方可入账);判读输入隔离(canonical 首行,非原始 body),不得成提权路径。
- **5.1a role×op 授权矩阵(★dev56②)**:授权闸(gate-3)判定按下表,principal 角色 × op → 允许/拒;门方由 task_role.tester 指定,非 tester 发门=拒:
  | op / 动作 | 允许的 principal 角色 |
  |---|---|
  | new(建单) | 调度者小组身份(DispatcherIDs) |
  | 门 PASS/FAIL(门结果) | 该 task **该里程碑段【指定门方】**(C2.1 派发信封每段 gate.门方 字段,非硬编码 task_role.tester;他人发门=directive_rejected) |
  | result(产出结果) | 该 task 的执行席(∈task_role implementer/tester) |
  | **收口(任务关闭)** | **调度者小组(★P1 调度者唯一关闭权;执行席不得自行收口)** |
  | status/心跳 | 该 task 任一 task_role 成员 |
  | reject | 调度者小组 或 该段指定门方 |
  | steer/interrupt/reassign(控制) | 调度者小组(gate-2)或业主应急(C6) |
  不在表内的 (角色,op) 组合=fail-closed 拒。矩阵版本化(改=改契约重两签)。
- **5.2 规则只作用于 meta+首行**(§6 冻结件同构:不信 body 自称);规则表版本化(改规则=改契约,重两签)。
- **5.3 度量**:逐闸命中计数入台账;LLM 兜底占比=调度者健康指标(目标持续下降,阈值 D15 域)。

## C6 应急原语(★新原语,dev56② 诚实立项;纳入本次两签,非 C1 引用面)

诚实声明:现有引擎 Takeover(无 Identity/无 owner_takeover 事件)不满足应急语义,故本节=**P3 待实现的新原语**,与 C1 引用面分离;其语义在此冻结,P3 照此实现、tester 回归门验。

- **6.1 owner_takeover(治整体故障:业主夺调度权)**:
  - 授权=发件 **principal ∈ 业主集**(C5 5.0,非自述);
  - 语义=orchestrator `epoch: e→e+1`,`owner=业主指定新调度者`;原调度者小组一切在途 guarded 写即被 epoch fence 失效;
  - **请求 schema(★dev56④⑤:含 to_owner)**:`owner_takeover{owner_principal, to_owner=指定新调度者身份, expected_epoch=e, idempotency_id, reason}`——字段缺失(含 to_owner)/类型错/to_owner 非合法 zzc-* 身份=拒;事件 to_owner=此字段;
  - **★MC-3 幂等/epoch 条件化+陈旧不跨代**:仅当【expected_epoch==当前 epoch e】时执行 e→e+1;陈旧 e(重投/延迟到达)=no-op(不二次夺权、不 fence 新任);**陈旧请求绝不作用于非期望代际(不跨代)**;
  - 事件=`owner_takeover{from_owner,to_owner,from_epoch,to_epoch,reason}`,actor=业主 principal(合法 zzc-* 身份,满足全事件 actor 不变量);
  - 用途=调度者小组整体故障/失信。
- **6.2 owner_override(治单点急停:业主单指令直写,★A-4 epoch 语义定死)**:
  - 授权=principal ∈ 业主集;
  - 语义=业主以**应急身份**对某 task/node 下达一条控制指令(急停/纠偏);**不改调度权归属(epoch 不变)**;
  - **★A-4/MC-1/dev56③④:override=P3 新原语(C1 1.4b),非引擎既有 guarded 原语**——【不得】冒充/注入调度者 `Identity{owner,epoch}`(那是伪造调度者身份,破坏 actor 问责+MC-1 冲突);也【不是】"走引擎既有 guarded 原语"(那些签 actor=调度者 owner,与"actor=业主"矛盾)。**owner_override 是独立新原语**:①自带 guard 谓词=`owner_principal 合法(C0.1) ∧ expected_epoch==当前 orchestrator.epoch ∧ expected_state 匹配`;②对目标 node 施加白名单内的单条状态变更(其效果=等价某既有控制语义如 pause/steer-cancel,但由本新原语实现、非调用既有原语签名);③事件 actor=业主 principal;④原子(状态变更+事件同事务)。不夺 owner、不改 epoch、不冒充调度者;
  - **请求 schema(★dev56④)**:`owner_override{owner_principal, target, directive∈白名单(C0.6), expected_epoch, expected_state, idempotency_id, reason}`——字段缺失/directive 不在白名单/类型错=拒;
  - **★MC-3 幂等+expected state/epoch**:携 `idempotency_id`(同 id 重投 no-op)+`expected_epoch`(陈旧不跨代)+`expected_state`(目标态不符=拒,防陈旧急停作用于已变更节点);
  - 事件=`owner_override{target,directive,idempotency_id,reason,acting_epoch}`,actor=业主 principal;
  - 用途=单点急停/纠偏,不整体接管。
- **6.3 过渡期 fail-closed(★dev56,=C0.4)**:principal 未就绪→C6 两原语**硬禁用不可启用**(非 best-effort);P5 上线硬门=principal 鉴权就绪 ∧ 清单清零 ∧ 两应急原语启用【原子共切】(与 D2=a 一致)。
- **6.4a 竞态/白名单/原子(=C0.5/C0.6)**:takeover↔override 同 epoch 先提交者决定;状态+owner_* 事件同事务;idempotency 重放返原 outcome;override directive∈既有控制 op 白名单,不自由文本直写。
- **6.5 变更纪律**:C6 与 C0-C5 同签同锚;任何 C6 语义变更须重两签。

---

## 附:P2 冻结与签核

- 冻结件=本文档+P1 设计稿 v1.1(9798c663,评审锚);两签纪律=W1′ 同款(精确 commit 锚+两审各自逐字取证+联署变更)。
- 评审建议分工:审#2(dev56)=契约一致性/可实现性/**C1/C6 闭环+task_role 授权+合法审计 actor+canonical routing 负例**;审#1(shence)=sign-grade 语义(应急边界/身份闸/授权闸负例/LLM 不提权)。tester 不进 P2(契约非代码),P3 起回归执行门。
- **v0.2 findings 闭合对照**:shence MC-1→C5 5.0/MC-2→C5 gate-3 授权闸/MC-3→C6 6.1 6.2 幂等 epoch;A-1→C3.4/A-2→C3.3/A-3→C5 gate-5/A-4→C6 6.2。dev56 ①→C2 2.6/②→C6 立项+C1 1.4/③→C4 4.2 合法 actor/④→C5 5.0a canonical。
