# 多CLI调度 组间接口契约 v0.3(终稿候选·devo48 起草)

**v0.2→v0.3**:二轮五方全收(prodops/shencedeepseek/tester/claudek3 四方 AGREE+dev56 收口级 CHANGES)。本轮落地:**dev56 F1~F7 全闭**+tester 3 条(DeliverFail 权威措辞/digest 算法/wkr.py 展开)+claudek3 附注(outcome 落日志脱敏)。零方向变更,全部为消歧/冻实。
**§重开门** board vNext 冻入 Phase-0:五方全数确认(prodops/shence/tester/claudek3/dev56)。**报张宗楚终确认后方可打 tag**。
**流程**:v0.3=终稿候选→五方机械核(字段/转移表/原子席位/DEA 类型/digest 算法/事件序域/完整锚)→张宗楚重开门确认→annotated tag `interface-contracts-v1`(认 tag^{commit})。走 git 指针(规约24)。

---

## 契约1·board 接口(根依赖·vNext,含重开门扩展)

### 1.1 Identity 与授权上下文(遗漏B:分离;F1:ID/凭据拆分)
两种上下文分开,不再混用模糊的 `id`:
```
WriterContext{ owner, epoch, writer_instance_id, fence_token }
SubmitContext{ principal, task_role, op }          // SS result 提交;走 role×op 闸,SA 不得改写
```
- **F1 两类字段冻结,禁一字段兼任公开 ID 与秘密凭据**:
  - `writer_instance_id`=不可复用、可审计的**非秘密**实例标识;可入 handoff/event writer_snapshot(与 m0-final C-7 handoff 含 instance、禁敏感 token 一致)。
  - `fence_token`=**秘密**写凭据;禁入日志/event/handoff,仅安全存储与传递。
- board 校验 owner/epoch/writer_instance_id/fence_token **四者绑定**:同 epoch 异 instance_id 或 token 不匹配均拒。
- 迟到 receipt 走 WriterContext 校验(四元绑定)。
- outcome 持有方(coordinator)对含 token 的 outcome 落日志**自担脱敏**(claudek3 附注)。

### 1.2 Takeover 完整签名+状态转移表(F2,重开门)
```
TakeoverRequest{ board_id, rotation_id, request_id,
  expected_owner, expected_epoch, expected_state∈{ROTATION_PENDING,SUSPECT}, new_writer_instance_id }
TakeoverOutcome{ outcome∈{applied,cas_miss,rejected,idempotency_conflict},
  owner, epoch, state, writer_instance_id, fence_token, board_revision, event_cursor, reason_code }
ActivateWriter{ board_id, rotation_id, request_id, expected_state=ACTIVATING } → Outcome
```
- CAS+epoch+1+new instance_id+目标 state+首 outcome+事件 **同一事务**;重放返同一 outcome;CAS loser 不得取 token/不得追新 epoch。
- fence_token 仅经 TakeoverOutcome 返给唯一 coordinator;**持久化 replay outcome 中的 token 必须加密/密封**;审计事件只留 writer_instance_id。
- reason_code(shencedeepseek):失败可观测哪项不匹配(epoch/state/owner)。
- **状态转移表(F2 冻结;CAS fence commit 与新 writer activation=两个独立崩点)**:

| allowed-from | 原语 | applied→target | 失败/非法 |
|---|---|---|---|
| ROTATION_PENDING / SUSPECT | Takeover | **ACTIVATING**(owner/epoch 已换,旧 writer 已 fenced,新 writer 尚未 active) | cas_miss/rejected,状态不变 |
| ACTIVATING | ActivateWriter(guarded,同一新 WriterContext) | ACTIVE(SA) | 失败/超时→**RECOVERY**,旧 epoch 永不重新放开 |
| STAGING | RevokeStaging(CAS 前撤销) | 回退前态,**owner/epoch 不变** | — |
| 其余任意 | 非表内转移 | — | rejected+reason_code=`illegal_transition` |

- takeover 不得直写 active(否则无法表达 C-3"CAS 后 activate 失败");Takeover、ActivateWriter 与各自首 outcome **各自同事务**落状态+事件;同 request_id 幂等重放返首 outcome。崩点恢复:ACTIVATING 中崩→凭 request_id 重放续 activate;activate 败→RECOVERY 人工/轮换组恢复路径。

### 1.3 节点写接口(具名 auth context + lease)
```
Send(nodeID, gen, rid, agentID, WriterContext) → Outcome
Processing(nodeID, gen, rid, WriterContext) → Outcome
Terminal(nodeID, gen, rid, done, resultRef, WriterContext) → Outcome
Reassign(...) → gen+1   // gen 对 nodeID 单调不复用(含 board 重启后,claudek3⑥防回绕)
running 带 lease_ttl(claudek3⑦):超时→board 自动 observed_stale+可 Reassign(回收挂死 worker)
```
- **O1 幂等冲突(五方共识,拒写+告警)**:持久化 `{rid, op_kind, canonical_payload_digest, first_outcome}`;同 rid+同 op+同 digest→返首 outcome;**同 rid 异 op/digest→返 `idempotency_conflict` 不覆盖+board 内部写 `idempotency_conflict` 事件**。
- **F5 digest 算法冻结**:
```
digest_schema  = mcs-canonical-json-v1
payload_digest = sha256( RFC 8785 (JCS) canonical JSON bytes )
```
  输入**含** op_kind+payload schema_version,**排除** transport-only 字段(from/to 等信封路由字段);Go 实现若不直接采 RFC 8785,须给同等确定的字段序/编码并过**跨语言 golden vectors**(同义 key 顺序/数字表示/Unicode/缺省 vs null,正负例),vectors 与用例同 commit 冻结(G3)。

### 1.4 事件账 schema(遗漏C:权威重放源;F6 排序域冻实)
```
event{ schema_version, event_id, board_id, board_revision, txn_seq,
  kind, task_id, rid/request_id, rotation_id, writer_snapshot,
  payload_digest, meta{result_ref,outcome_ref,...}, created_at }
```
- **F6 单调域**:`board_revision`=**per-board 单调**,每状态事务 +1;同一事务多事件以 `txn_seq`(事务内 0 起稳定序)排;**replay 唯一排序键=断点 cursor=(board_revision, txn_seq)**;时间戳不作排序键。事件与状态变更**同事务**提交。(原"event_seq 或 board_revision"二选一占位废除,event_seq 更名 txn_seq。)
- **writer_snapshot = {owner, epoch, writer_instance_id},绝不含 fence_token**(F1/F6)。

### 1.5 集中类型注册表(遗漏·shencedeepseek3/claudek3⑮/dev56D)
- **error/outcome code 集中枚举**(禁消费者解析字符串):Outcome{applied,cas_miss,rejected,idempotency_conflict,limit_exceeded};reason_code 表(含 illegal_transition);event kind 表(gate_result/status_note/observed_stale/idempotency_conflict/owner_op/...)。
- **时钟语义(遗漏D)**:TTL/lease/过期点=UTC 墙钟;进程内等待=monotonic;mock 注入 deterministic clock。

### 1.6 C2.6 席位原语(F3:原子原语,非裸查询;board owns)
```
PromoteExecSeat{ task_id, principal, request_id, expected_board_revision } → SeatOutcome
GrantRoleExemption{ task_id, principal, request_id, expected_board_revision } → SeatOutcome
SeatOutcome{ outcome∈{applied,limit_exceeded,cas_miss,rejected}, board_revision, reason_code }
SeatSnapshot{ task_id, revision, occupied, limit }   // 只读观测,禁作授权依据
```
- 语义=P3 既有守门语义的版本化引用;席位变化必须**同一事务**"检查 DISTINCT 非豁免执行席≤2 + 预留/变更 + 事件"(消 TOCTOU);request_id 幂等返首 outcome;expected_board_revision 防两并发 SS 同见空席双进。
- **STAGING SB 不占席;SB 替代 SA 的 activation=换席,不得短暂计成第三席;独立 SS 才占席**。

---

## 契约2·信道接口(信道组 owns)

### 2.1 调度↔hub 线格式(已冻+补)
```
mcs_dispatch_v1{ kind,node_id,generation,rid,payload,from,to }  // 收发人必填缺拒
mcs_receipt_v1{ kind,node_id,generation,rid,phase∈{processing,done},ok,result_ref,from,
  error_class∈{retryable,fatal} }   // claudek3⑭:ok=false 带 error_class 供调度重试策略
ParseReceipt:生产首对象容错;**stub/测试态可切严格模式**(claudek3⑧防夹具吞坏帧失真)
```

### 2.2 桥接↔driver 解耦(遗漏·骨架先冻,Type B 根治)
最小四端点(dev56 O3/prodops O3,均接 context/deadline):
```
Connect/Close(→close_reason)  Deliver(delivery/request identity)
Events/Receipts(有序+ack)     Interrupt(目标 turn/delivery→outcome)
```
- 错误分类:offline/timeout/backpressure/protocol_reject/internal。
- **WS 连接与 driver 执行独立:driver 挂死≠WS 关**(prodops O3a);driver 侧 health 端点非仅 WS ping(O3b);75s idle timeout 仅在有 driver health 前提触发(O3c)。

### 2.3 stub hub/mock 飞书 conformance kit(O4,owner 供共享,禁各写一套)
- mock unlock: code→{granted,reason} 不连真飞书;stub hub deliver/receipt ack 链(injected→processed)可断言;/page/resume mock 含 can_write;确定性时钟+脚本化 connect/disconnect+背压/重复/乱序/丢包/故障注入。
- **真实现与 stub 跑同一套契约测试;E门自动化夹具直接复用此 kit 不另起(claudek3⑨/O4)**。

---

## 契约3·worker 接口(调度组 owns)

### 3.1 pool 抽象(补活性+drain+类型化)
```
Worker{ ID; Health(ctx,mode∈{readonly,challenge}); Caps; Deliver(ctx,Envelope); Stop(ctx);
        Drain(ctx,mode∈{graceful,immediate}) }   // claudek3④池级 drain:完成再退vs立即中断
DriverAdapter{ Spawn(ctx) → (Worker, SpawnOutcome) }
```
- **活性判据(契约级,O6/claudek3①,禁 presence)**:Health challenge 模式=交付进度回执/应用层 ping-pong,**禁用在线清单/presence 判活**(Type B 五死实证)。probe 结果 `{worker_id,probe_seq,status,reason,observed_at,expires_at}`,旧 probe_seq 不覆盖新状态。
- **SpawnOutcome 类型化(O5)**:`no_healthy_worker`(快败+告警不排队)/`all_healthy_busy`(有界排队+退避)/`capacity_unknown`(快照过期);带 pool_snapshot_seq+observed_at+retry_after,同一快照决策防 health/capacity 读取竞态。
- SS(m0-final C-5):不持 task authority、不共享 writer 凭据、以 SubmitContext 提交走 role×op 闸;SS 探活独立于 SA 信道/超时(claudek3③防相关性致盲),SS 探活不经 board 写。
- **pool Config(Min/Max)=资源参数≠席位参数**(shencedeepseek4/M0 C-5),接口层显式标注此约束;席位≤2 由 board C2.6 原语闸(§1.6)。

---

## 契约4·轮换接口(轮换组 owns,复用 m0-final)

SA/SS/SB=m0-final C-1~C-10 全条,直接引用(完整锚见 §G3 锚表)。组间接口点:
- 轮换↔board:takeover 五元谓词+writer_instance_id/fence_token(契约1.1/1.2,重开门);**状态转移表已冻于 §1.2(F2),轮换组终稿轮逐行确认**(状态命名如需归一以轮换组域为准,语义不动)。
- 轮换↔usage(O7 展开,shencedeepseek/dev56):**M0b 探针冻结锚=33888822f0f7692839ed16d1a0ff280742fcbf0f**。边界=探针产样/adapter 归一/coordinator 判触发,**probe 不直接触发 rotation**。
  - sample schema 全列(不省略):`session_id/provider/model/source/normalized_used_tokens/max_context/sample_seq/observed_at/confidence/raw_value/raw_kind`(+opaque turn_id 若有)。
  - adapter outcome:accepted/stale/invalid/unknown/rebaseline_model/rebaseline_compaction/rebaseline_max_change。
  - coordinator 持久化去抖:2×≥0.75 / 1×≥0.85 / ≥0.95 紧急;重启不丢连续轮计数/不重复生成 rotation。
  - static max 表:exact model ID+官方源+版本+变更留痕;未知/变体后缀/表缺/native 冲突→fail closed,冲突取更小分母。
  - result.usage 仅取证不入 normalized_used_tokens;分母已知但分子未鉴定仍 UNKNOWN。
  - 供 deterministic fake samples + stale/compaction/model-switch 序列。
- 轮换↔worker:SA/SS 占席 vs C2.6≤2——**席位原语已冻于 §1.6(F3)**。

---

## 契约0·worker CLI 接口(遗漏L1·tester,调度组供;终稿展开)
wkr.py=真 worker 实现参考(/home/devo48/work/heartbeat/wkr.py,后续随客户端改造批进 sessionHelper 装机面)。
```
wkr.py ack  <信封JSON文件|->  --to <调度会话全名>
wkr.py done <信封JSON文件|->  --to <调度会话全名>  --ref <产物指针>  [--fail]
```
- 输入=mcs_dispatch_v1 信封(文件或 stdin `-`);出线=mcs_receipt_v1 **纯 JSON**(禁附人类时戳,N12 教训;时刻语义归 hub meta)。
- 语义:ack→`phase=processing`;done→`phase=done, ok=true`(`--fail`→ok=false);`--ref`→result_ref(路径不存在仅警告仍发,board 验收侧拒);generation 缺省 0 透传。
- from 解析:env `SH_FULL_NAME`→缺则 `SH_SESSION_NAME`(短名)+本机在线清单自查全名;**非 zzc- 全名格式拒发(短名禁令)**。
- 强校验(违者拒发):缺 to/from;kind≠mcs_dispatch_v1(入口闭合);缺 node_id/rid;**错投(信封 to≠本会话全名)=MISROUTE,不伪造回执,告警发件人**。
- **返回码约定**:0=回执已发出;1=校验拒发/ERR;2=用法错;其余=透传 send.py 返回码。
- 幂等义务(工人侧,M2 协议):同 rid 重投只重发回执不重复执行(执行点恰一,M0 3.1)。

## 契约·DeliverFail DEA 判定(遗漏L2·tester;F4 冻实)
**DEA 三态判定权威=驱动 DEA,board 只执行/消费、不推断**(tester 措辞)。弃裸 `deaTerminal bool` 跨组接口,改具名 verdict:
```
DeliverFailVerdict ∈ { APPLIED, NOT_APPLIED_FENCED, UNKNOWN }
DeliverFail{ node_id, gen, rid, verdict, evidence_ref }
```
- `NOT_APPLIED_FENCED`=唯一可安全释放/终局失败的未执行态;缺能力/裸 NOT_APPLIED/证据不足一律归 `UNKNOWN`。
- 状态机:仅 `NOT_APPLIED_FENCED` 且 attempts 耗尽→delivery_exhausted;`APPLIED`→等待/收敛 receipt;`UNKNOWN`→保持 unresolved,**禁止 reassign**。
- 冻结权威设计锚(完整):`6f1b07116dc806ad3e6b6e583913a92a718aa450`。

---

## 全局

### G1 mock/stub 形态(统一约定,非统一巨型 mock)
每契约 owner 随仓供:接口 fake+确定性 clock+fault script+fixture builder+conformance suite;公共约定统一 outcome/error envelope+sequence+time+trace 字段。**消费者只组装 owner fake,不复制实现/不自造不兼容 mock**。mock 件随契约进仓,版本随契约 sha。

### G2 契约版本 registry(仓内)
`{contract,version,owner,consumers,schema/blob SHA,compatibility,status}`。兼容新增=minor;删除/语义变/鉴权变=major+重开门。owner 起草+受影响消费者双审;**coordinator 只核完整性+登记**(claudek3⑫)。运行消息带 schema_version;未知 major 显式拒不静默降级。

### G3 验收锚(F7:终稿与 registry 一律完整 SHA,禁短锚)
| 锚 | 完整值 |
|---|---|
| m0-final annotated tag object | `fc5275cf7bb72c4c37df05749c69df93d7143456` |
| m0-final^{commit}(轮换契约 v0.7) | `e49378b76cad20a457687505468da3189ea964c1` |
| M0b probe | `33888822f0f7692839ed16d1a0ff280742fcbf0f` |
| DEA authority | `6f1b07116dc806ad3e6b6e583913a92a718aa450` |

终稿 annotated tag `interface-contracts-v1`→不可变 commit,**认 tag^{commit} 不认分支 tip**。每契约+可执行用例记 blob/file SHA-256。规约23 用例与契约**同 commit 冻结**(含 F5 golden vectors),签名声明契约 SHA+用例 SHA。跑通冻结用例即过;新争议先补用例生成新锚,不口头改判据。

---

## 收口顺序(dev56 建议,采纳)
1. 先闭 board vNext(§1.1/1.2/1.6+O2 三元组)+conformance cases —— **重开门项五方已确认,报张宗楚终确认**;
2. 并行闭 O7 usage adapter/trigger fake;
3. O3/O4、O5/O6 由 owner 按 G1 形态出接口+用例;
4. G2/G3 registry+annotated tag+用例 SHA 收口 Phase-0。

**终稿轮(机械核)**:五方各自核 F1~F7 落点(字段拆分/转移表/原子席位/DEA 类型/digest 算法/事件序域/完整锚)+本方历史点位未回退,回 AGREE(带 sha 头)或指出未闭点;dev56 承诺闭后机械 AGREE 不再新增方向意见。齐签+张宗楚重开门确认→打 tag。
