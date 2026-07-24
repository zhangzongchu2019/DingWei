# 调度者/任务清单编排系统 · P2 契约文本(C0-C6,v0.3——两审 7 点+shence H1/H2 授权模型合章)

作者 zzc-devo48-2642 · 2026-07-23 · 长程任务 P2 段(冻结对象;两签后 C1-C5 即契约,改动须重两签)
上游=P1 设计稿 v1.1(sha 9798c663);本稿把 §7 骨架落成可签文本。
v0.2 修:shence 审#1(MC-1 身份真实性锚/MC-2 状态 op 授权闸/MC-3 应急幂等 epoch;A-1~A-4)+dev56 审#2(①C2 漏 2-Agent 守门/②应急原语非零新语义须诚实立原语/③合法审计 actor 悖论/④canonical 首行与可信 control op)一次攒批;应急原语升为 **C6 新原语**(纳入本次两签,C1 引用面恢复"纯引用零新语义"的诚实性)。
v0.3 增:shence H1(MC-1+MC-3+A-4 合成单一 **C0 授权模型章**,C4/C5/C6 引用不分处补丁)+H2(过渡窗风险敞口写死+业主显式接受,C0.4/C3.4a)。

---

## C0 授权模型(★shence H1:身份/权限单一真相,C4/C5/C6 引用本章,不分处补丁)

三者同属一个模型,一处定义方自洽(override 的业主身份依赖真实性锚、其 replay 安全依赖幂等——分散打补丁留缝):
- **0.1 身份真实性锚**:一切 gate/授权判定的主体=**hub 鉴权后的 per-session principal**,【非】信封自述 from(共享队钥 FB-zzc-devteam-* 下 from 可伪造)。principal 由传输层注入收件面。
- **0.2 授权双主体模型(★A-4 定义,dev56 可实现性线并置)**:
  - 常态写=调度者小组以自身 `Identity{owner,epoch}` 走 guarded 原语;
  - **业主 override 写=双 principal 授权**:业主 principal(0.1 校验)授权 + 在**调度者当前 epoch** 上执行该单条 guarded 写(不夺权、不改 owner、不裸越 fence);epoch 变则 override 上下文失效(须重取当前 epoch)。
- **0.3 epoch/幂等**:owner_takeover=期望 epoch CAS(e→e+1,陈旧 e 为 no-op);owner_override=带 idempotency_id(同 id 重投 no-op)。状态 op 沿引擎 rid 幂等。
- **0.4 过渡窗(★shence H2:principal 未就绪前,风险敞口写死+业主显式接受)**:hub per-session 身份未就绪的窗内——**控制闸 / owner_takeover / owner_override 为 best-effort,共享队钥下【可被任一持钥者伪造】**;敞口逐项=①控制指令可伪造发件人②应急夺权/急停可伪造业主。**处置=此窗内高危通道整体禁用**(C5 5.0 过渡不变量),且 **C3 记"过渡窗风险敞口=上述①②,业主(张宗楚)知情并显式接受"**(限定词写死进契约,不靠传递)。principal 就位后 0.4 废止。

## C1 调度者↔引擎(引用契约,零新语义)

- **1.1 原语集(全集,引用即冻结实现)**:CreateTask(建单硬约束:四角色三元组必填全)/AddNode/SetTaskRoles/Send/Processing/Terminal(含直达窗+fan-out)/Reassign/SteerReserve/SteerFinalize/InterruptIntent/InterruptFinalize/Redeliver/DeliverFail/EnqueueResult/AckResult/Sweep/Takeover/Renew——语义=两签冻结设计件(f41d686 家族)+引擎实现(资产头 74e5dc0,十门全绿)。
- **1.2 身份**:调度者小组=唯一持 `Identity{owner,epoch}` 的主体;一切写走 guarded 原语并签 actor;外部只读 TaskLedger/RoleStats/board_event。
- **1.3 变更纪律**:C1 引用面的任何语义变更=引擎设计变更,须另开窗重两签(本契约不授予捷径)。
- **1.4 边界(★dev56②)**:应急原语(owner_takeover/owner_override)**不属 C1 引用面**——现有 Takeover 无 Identity、无 owner_takeover 事件、不满足"均 guarded+actor",无法由现有原语组合唯一映射;故它们是 **C6 新原语**(P3 实现,纳入本次两签),C1 保持"纯引用冻结引擎、零新语义"。

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
- **3.4a 过渡窗风险敞口(★shence H2,业主显式接受)**:hub principal 就绪前,控制/应急通道 best-effort 且共享钥下可伪造(敞口=①控制指令伪造发件人②应急夺权/急停伪造业主);**处置=该窗高危通道整体禁用(C0.4/C5 5.0),业主张宗楚知情并显式接受此残余风险**。上线(principal 就绪)即闭合。
- **3.5 过渡/切换原子性(★A-1)**:C3.4 补丁废止 / C4.3 enforce 切换 / C3.3 例外唯一性 **同一 P5 时点原子共切**;不变量:任一时刻指令入口恰好一套规则生效——无双路径、无真空。

## C4 身份契约(地基,D2=a:上线前收编完成,上线即 enforce)

- **4.1 合法身份格式**:`^zzc-[a-z0-9]+-[0-9]+$`(现行 v1.0 规范;全小写;三段式 主体-名-尾号)。此字符串=全体系唯一主键(task_role.who / board_event.agent_id / 信封路由 / 台账)。
- **4.2 enforce 语义(Board 侧,P3 实现)**:RegisterAgent 与事件 actor 入账时正则校验;非法名=写入拒绝+`identity_rejected` 事件。
  - **★dev56③ 合法审计 actor**:identity_rejected 事件本身的 actor【不是】被拒的非法名(否则"全事件强制合法 actor"与"记录非法名"自相矛盾)——用**系统哨兵身份 `system:identity-gate`**(保留主体,非 agent),被拒裸名记入 event.data 而非 actor 字段。不变量:每条事件 actor∈{合法 zzc-* 身份, 系统保留哨兵},裸名永不进 actor。
- **4.3 收编流程**:①盘点(hub 全量活跃会话名,非合规清单+owner 认领)②迁移(改名/重建,逐项销号)③enforce(hub 命名 warn→enforce 切换)。**切换判据**:清单清零 ∧ 迁移 100% ∧ 回滚开关就绪;时点=调度者上线里程碑(P5)前完成。
- **4.4 跨系统依赖**:hub 命名 v1.0 的 enforce 开关(hub 侧配合项,P3 排入协调)。

## C5 路由契约(正则先行,LLM 兜底)

- **5.0 身份真实性锚(★MC-1,=C0.1)**:gate 判定的发件身份=**C0.1 per-session principal**,【不是】信封自述 `from`(现行 send.py `from="{name}#{key}"` 中 name 客户端自选、key=全队共享 secret,from 可被任一持钥者伪造成业主/调度者)。C5 全部闸读此 principal。
  - **P3 硬依赖**:hub 提供 per-session 主体注入(连接层→收件面);列 P3 实现清单+跨系统协调项。
  - **过渡期不变量(hub 未就绪前,诚实降级)**:身份【非】不可伪造——此期间**禁用 C6 应急原语与一切控制 op**(gate-2 控制闸对所有来源一律 directive_rejected,只放行只读/状态入账),即"不可伪造性未就位 ⇒ 高危通道整体关闭",不带病放行。principal 就位后本条废止。
- **5.0a canonical 输入(★dev56④)**:闸只读**规范化首行 canonical_first_line**(剥传输头/镜像标签/引号后的净正文首行)+信封 meta;传输头/body 自称控制词不参与路由(防传输头误路由)。可信 control op=仅 meta.op(结构化字段),不认首行自然语言里的"停/暂停"当 op(自然语言控制词→gate-2 视为疑似,升授权判定不直接执行)。
- **5.1 处理次序(信封到达调度者收件面;身份=5.0 principal)**:
  1. **身份闸**:发件身份不合 C4 格式→拒收+identity_rejected,终;
  2. **控制闸**:含控制语义(停/暂停/重派/改单)且发件身份≠调度者小组∧≠应急原语→拒收+directive_rejected+回指调度者,终;
  3. **授权闸(★MC-2,状态变更前必过)**:meta `op∈{new,status,result,reject}` 或状态首行——**发件 principal 必须 ∈ 目标 task_id 的 task_role**(new=须调度者小组身份);否则 directive_rejected+事件,终。不变量:任何状态变更(建单/门/收口/结果/reject)前必过授权闸,格式闸不替代它。
  4. **结构/状态入账**:过授权闸后——meta op→引擎既有 op 路由(C1);状态首行 ∧ task_id→状态机入账(门/心跳→TaskLedger+事件);均零 LLM,终。
  5. **兜底(★A-3)**:升调度者 LLM 判读——**LLM 只出路由判读、不直接授权/触发 guarded 写**(判读结论落 `route_llm{verdict}`,再经授权闸方可入账);判读输入隔离(canonical 首行,非原始 body),不得成提权路径。
- **5.2 规则只作用于 meta+首行**(§6 冻结件同构:不信 body 自称);规则表版本化(改规则=改契约,重两签)。
- **5.3 度量**:逐闸命中计数入台账;LLM 兜底占比=调度者健康指标(目标持续下降,阈值 D15 域)。

## C6 应急原语(★新原语,dev56② 诚实立项;纳入本次两签,非 C1 引用面)

诚实声明:现有引擎 Takeover(无 Identity/无 owner_takeover 事件)不满足应急语义,故本节=**P3 待实现的新原语**,与 C1 引用面分离;其语义在此冻结,P3 照此实现、tester 回归门验。

- **6.1 owner_takeover(治整体故障:业主夺调度权)**:
  - 授权=发件 **principal ∈ 业主集**(C5 5.0,非自述);
  - 语义=orchestrator `epoch: e→e+1`,`owner=业主指定新调度者`;原调度者小组一切在途 guarded 写即被 epoch fence 失效;
  - **★MC-3 幂等/epoch 条件化**:仅当【期望 epoch=e】时执行 e→e+1;陈旧 e(重投)=no-op(不二次夺权、不 fence 新任);
  - 事件=`owner_takeover{from_owner,to_owner,from_epoch,to_epoch,reason}`,actor=业主 principal(合法 zzc-* 身份,满足全事件 actor 不变量);
  - 用途=调度者小组整体故障/失信。
- **6.2 owner_override(治单点急停:业主单指令直写,★A-4 epoch 语义定死)**:
  - 授权=principal ∈ 业主集;
  - 语义=业主以**应急身份**对某 task/node 下达一条控制指令(急停/纠偏);**不改调度权归属(epoch 不变)**;
  - **★A-4/MC-1 交叉:如何在不夺 epoch 下越过 fence**——override 不裸写绕 fence,而是**以当前调度者 owner+epoch 为授权上下文执行该单条 guarded 写**(业主 principal 经 C5 5.0 校验后,系统以"业主 override 上下文"注入当前有效 (owner,epoch),写本身仍走引擎 guarded 原语、rowcount==1);
  - **★MC-3 幂等**:override 携 `idempotency_id`,同 id 重投=no-op(陈旧急停不二次触发);
  - 事件=`owner_override{target,directive,idempotency_id,reason,acting_epoch}`,actor=业主 principal;
  - 用途=单点急停/纠偏,不整体接管。
- **6.3 过渡期(C5 5.0 principal 未就绪)**:C6 两原语**整体禁用**(身份不可伪造性未就位=应急通道不可信=宁缺勿滥);principal 就位后启用。
- **6.4 变更纪律**:C6 与 C1-C5 同签同锚;任何 C6 语义变更须重两签。

---

## 附:P2 冻结与签核

- 冻结件=本文档+P1 设计稿 v1.1(9798c663,评审锚);两签纪律=W1′ 同款(精确 commit 锚+两审各自逐字取证+联署变更)。
- 评审建议分工:审#2(dev56)=契约一致性/可实现性/**C1/C6 闭环+task_role 授权+合法审计 actor+canonical routing 负例**;审#1(shence)=sign-grade 语义(应急边界/身份闸/授权闸负例/LLM 不提权)。tester 不进 P2(契约非代码),P3 起回归执行门。
- **v0.2 findings 闭合对照**:shence MC-1→C5 5.0/MC-2→C5 gate-3 授权闸/MC-3→C6 6.1 6.2 幂等 epoch;A-1→C3.4/A-2→C3.3/A-3→C5 gate-5/A-4→C6 6.2。dev56 ①→C2 2.6/②→C6 立项+C1 1.4/③→C4 4.2 合法 actor/④→C5 5.0a canonical。
