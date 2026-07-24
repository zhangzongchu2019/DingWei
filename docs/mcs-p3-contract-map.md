# P3 契约条→代码点→负例测试 映射表(criteria aa011339 §3 交付物)

> 基线=P2 契约 18ebdb1;评审标准=dispatcher-P3-review-criteria.md(sha aa011339)。
> 判据:每条安全属性【存在性(代码点+通过负例)+sign-grade(评审方判)】。全测试 `go test -race ./internal/mcs/...` 绿。
> 件链:154cbc3(首件 route/identity,双审 AGREE)→e92d2c7(C6+F修)→047e575(C2.6)→b46fc17(DEA)→6ee34f3(集成)。

## C4 身份(identity.go)

| 契约条 | 代码点 | 负例测试 |
|---|---|---|
| 4.1 fullmatch+拒控制字符(MC-4) | board/identity.go `IdentityPattern`(\A…\z)+`hasControlChar` | identity_test:TestIdentity_WarnDefaultThenEnforce(尾换行/控制字符名拒) |
| 4.2 enforce+identity_rejected+哨兵 actor(dev56③) | board/identity.go `checkIdentity`(actor=system:identity-gate,裸名进 data) | 同上+mcsd/inbound_test:TestInt_ChainShortCircuitAndSentinelActor |
| P2 warn 非权威(shence P2/F3 fail-safe 默认 provisional) | board/identity.go `identityConfig.provisional`;board.go `Open`(默认 true) | identity_test:TestIdentity_ProvisionalWarnNonAuthoritative |

## C5 五闸路由(route.go)+收件面接线(mcsd/inbound.go)

| 契约条 | 代码点 | 负例测试 |
|---|---|---|
| 5.0 principal 非 from(MC-1) | route.go `Envelope.Principal`(结构无 from 字段=构造性) | route_test:TestFiveGates_V15(伪造 from 不进路由) |
| gate-1 拒 system:* 外部哨兵(dev56⑤) | route.go `Route` ① | TestFiveGates_V15+TestInt_ChainShortCircuitAndSentinelActor(A 短路:无 gate-2 副作用) |
| 5.0a gate-2 仅 meta.op∈ControlOps(MC-B) | route.go `controlOps`/`suspectRe`(仅 gate-5 标记) | TestFiveGates_V15(status 正文含"暂停"不误拒)+TestInt_SuspectControlFlaggedNotExecuted |
| gate-3 role×op 矩阵逐判(MC-2/dev56②) | route.go `authorizeState` | TestFiveGates_V15(implementer 自签门 PASS 拒 等) |
| ★shence P1 收口/门必过 gate-3 不绕 gate-4 | route.go `stateOps` 含 gate_result/close | TestFiveGates_V15(执行席收口拒=调度者唯一关闭权) |
| ★dev56 MC-a 门方 per-段 | route.go `GateKeeperOf`;inbound.go `SetGateKeeper/gatekeeperOf` | TestFiveGates_V15+TestInt_GateAdvanceOnlyViaGatekeeperPath(c) |
| gate-4 入账/gate-5 LLM 不提权(A-3) | route.go `Route` ④⑤;inbound.go `Handle`(LLM 分支零写)+`ResolveLLM` | TestInt_LLMJudgeMustReenterGate |
| item① 过渡窗全 mutating fail-closed+只读放行(§3.2/G) | route.go `Trusted` 闸;inbound_test 集成层 | TestTransitionWindow_AllMutatingFailClosed+TestInt_TransitionWindowFailClosedReadOnlyOpen |
| canonical 首行(5.0a,剥传输头/镜像/引号) | inbound.go `CanonicalFirstLine`(传输头正则须含→) | TestInt_CanonicalUnwrapsTransportAndMirror(§3.6-C 花式不绕闸) |
| ★H2/§3.6-B 未知 op 入口拒 | inbound.go `Handle` 入口(route.KnownOp;additive,Route 已签逻辑未动) | TestInt_UnknownOpRejectedNotLLM+route_test:TestDefaultDeny(越表/nil fail-closed) |

## 集成 3 caller 义务(154cbc3 定性"待集成"的闭合点,6ee34f3)

| 义务 | 代码点 | 负例测试 |
|---|---|---|
| ① LLM 判读输出回授权闸,不直落 guarded 写 | inbound.go `Handle`(llm 分支零写)+`ResolveLLM`(唯一回路=重进全闸) | TestInt_LLMJudgeMustReenterGate |
| ② Authz 拒空 taskID | board/audit.go `RoleOfWho`(空 taskID 恒"")+route 侧 `authorizeState` 防御 | TestInt_EmptyTaskIDRejected |
| ③ gate 推进只经门方闸 MetaOp=="" 入账,正文不推断 | inbound.go `Handle` status_ingest 分支(唯一 gate_result 入账点);engine_op 分支不读正文 | TestInt_GateAdvanceOnlyViaGatekeeperPath(a)(result 正文"门 PASS"零入账) |

## C6 应急原语(c6_owner.go,e92d2c7)

| 契约条 | 代码点 | 负例测试 |
|---|---|---|
| C0.1 授权=业主集,非自述 | `c6Ready`/`SetOwnerPrincipals` | TestC6_OwnerTakeover(非业主拒) |
| epoch-CAS 陈旧不跨代不自动重取(dev56④) | `OwnerTakeover`/`OwnerOverride`(读-比-条件 UPDATE+RowsAffected==1,F-C6-1) | TestC6_OwnerTakeover(cas_miss)+TestC6_OwnerOverride(expected_state) |
| §3.6-D 并发恰一 | 同上(SQLite 串行+CAS 自足) | TestC6_ConcurrentTakeoverExactlyOnce(F-C6-2,-count=3) |
| C0.3/MC-b 幂等单键+★E principal 守卫 | `checkOwnerIdem`(owner_op 表,单键+跨 principal 拒) | TestC6_OwnerTakeover(幂等 no-op/跨 principal 拒) |
| C6.3 过渡期硬禁用 | `c6Config`(默认 emergencyEnabled=false) | TestC6_TransitionHardDisabled |
| actor=业主不冒充调度者(dev56③/A-4) | 两原语事件 INSERT(agent_id=OwnerPrincipal) | TestC6_OwnerTakeover/Override(actor 断言) |
| C0.6 directive 白名单 | `overrideDirectives` | TestC6_OwnerOverride(白名单外拒) |
| 操作段手册(criteria §1) | docs/mcs-c6-emergency-runbook.md | (文档件,评审即门) |

## C2.6 2-Agent 守门(c26_seats.go,047e575)

| 契约条 | 代码点 | 负例测试 |
|---|---|---|
| 执行席≤2,轻角色不占席(P1 §4.2a/D4=a) | `execSeatCount`(非豁免 DISTINCT who) | TestC26_LightRolesDontOccupySeats(放行)+TestC26_PromoteThirdSeatRejected(拒) |
| 升格触发校验,不静默突破 | `PromoteExecSeat`(预演>2→rejected 零写入;default-deny 非轻角色) | TestC26_PromoteThirdSeatRejected+TestC26_PromoteInvalidRoleRejected |
| 豁免=guarded 写+role_exemption 事件 | `GrantRoleExemption`(reason 必填+事件+幂等) | TestC26_PromoteWithExemption(含空 reason 拒)+TestC26_GuardNonOrchestrator |
| 建单/变更守门(换人不绕限) | board.go `CreateTask`/`SetTaskRoles` 接线(promoted 不随 who 释放) | TestC26_RoleChangeCannotBreach+TestC26_SwapSeatThenPromote(换席放行) |
| 幂等 | 两原语 no-op 路径 | TestC26_PromoteIdempotent |

## DEA stub(dea.go,b46fc17;真驱动 Q1-Q4 留 P4)

> **权威坐标(shence 卫生②)**:DEA 契约权威=设计冻结件 `multi-cli-scheduler-M0-board-draft.md` **§3.7 @ commit 6f1b071**(两签冻结旁支,dev56 五核#1 语义;非 P3 代码基线 18ebdb1 树内)。
> **verdict 域脚注(shence 裁决 11:24Z,卫生①)**:三态为正;裸 NOT_APPLIED 非终局→归 UNKNOWN;唯一释放安全态=NOT_APPLIED_FENCED(未 fence 的"未执行"仍可能触发=释放即双副作用)。criteria §5③ 4 值写法系缩写歧义,其脚注修正随首件 re-freshen 批。

| 契约条 | 代码点 | 负例测试 |
|---|---|---|
| 三原语签名+三态冻结(criteria §5③ 4值写法按冻结件收敛,已报 shence 裁) | `Authority`/`Verdict` | TestDEA_VerdictDomainFrozen |
| 规则2 释放仅 NOT_APPLIED_FENCED | `TerminalForRelease` | TestDEA_ReleaseOnlyOnFenced+TestDEA_FenceAfterExecuteReportsApplied |
| 规则3 能力缺失 fail-closed(待验默认) | `Capabilities`(零值全 false)+`ErrNoCapability` | TestDEA_NoCapabilityFailClosed+TestDEA_CapabilitiesZeroValueFailClosed |
| execute 幂等/fence 终局 | `Stub.Execute/Fence`(fence 后 ErrFenced 零副作用) | TestDEA_ExecuteIdempotentExactlyOnce+TestDEA_FenceTerminal+TestDEA_QueryThreeStates |

## §3.6 A-G 覆盖索引

A 闸链短路=TestInt_ChainShortCircuitAndSentinelActor · B default-deny=TestDefaultDeny+TestInt_UnknownOpRejectedNotLLM+TestC26_PromoteInvalidRoleRejected · C canonical=TestInt_CanonicalUnwrapsTransportAndMirror · D 并发恰一=TestC6_ConcurrentTakeoverExactlyOnce · E 键不跨 principal=TestC6_OwnerTakeover(E 段) · F 拒必落账=TestInt_ChainShortCircuit…/TestInt_UnknownOp…(audit 断言) · G 过渡窗两例=TestTransitionWindow_AllMutatingFailClosed+TestInt_TransitionWindowFailClosedReadOnlyOpen(只读放行正例)。

## ★Scope 定级(shence F-INT-1,11:29Z——enforcement 触达的如实标注)

P3 交付=**组件级 enforce 就绪+E2E 单测证**(五闸×Board 接线/3 caller 义务/审计,全负例过);
**最外层生产接通不在本轮**,显式定级如下(与 criteria §1 OUT 一致,不默认已接通):
| 未接通面 | 现状 | 归属 |
|---|---|---|
| transport/hub→Receiver.Handle 真消息源 | Receiver 零生产调用者 | P5 部署灰度(真投递冒烟,criteria OUT) |
| C6 OwnerTakeover/Override 业主授权面 | 原语零非测试调用者(过渡期硬禁用=设计态) | P5 应急原语原子共切(criteria OUT) |
| C2.6 Promote/Exemption 调度者控制面 | 原语零非测试调用者 | P4 管理面接线(攒批已记) |
| DEA 真驱动+Q1-Q4 能力实测 | Stub only(能力全待验=fail-closed) | P4(criteria §5③) |

## 独立测试方(zzc-tester-2642)状态

route 15/15 ✅ · C6 7/7 ✅(+并发 D 补)· C2.6 9/9 ✅ · DEA/集成=已派发待回。
