# 多CLI调度 组间接口契约 v0.2(修改稿·devo48 起草)

**v0.1→v0.2**:吸收五方一轮汇总意见(prodops/tester/shencedeepseek/dev56/claudek3),回应统一体现在本稿,不单条回。原 7 O点+3 G点+**新增 15 遗漏**全部落地。
**待终稿轮/张宗楚拍的结构点**(显式标注,不偷改):**§重开门** board vNext(三元组+完整 takeover)是对 m0-final 冻结件的扩展,v0.2 将其冻进 Phase-0=在协议阶段重开 board 门。五方共识"现在冻"(dev56/claudek3/shencedeepseek),理由=board 根依赖,后补打穿全下游。**此项请终稿轮确认。**
**流程**:v0.2→再一轮汇总→终稿→annotated tag `interface-contracts-v1`(认 tag^{commit})。走 git 指针(规约24)。

---

## 契约1·board 接口(根依赖·vNext,含重开门扩展)

### 1.1 Identity 与授权上下文(遗漏B:分离)
两种上下文分开,不再混用模糊的 `id`:
```
WriterContext{ owner, epoch, writer_instance }   // writer mutation 校验;writer_instance=不可复用凭据
SubmitContext{ principal, task_role, op }          // SS result 提交;走 role×op 闸,SA 不得改写
```
- 统一字段名=`writer_instance`(禁 fence_token/writer_instance_id 混称)。**writer_instance 敏感,禁入日志/event meta/handoff**。
- 迟到 receipt 走 WriterContext 三元组校验。

### 1.2 Takeover 完整签名(遗漏A + O2 三元组,重开门)
```
TakeoverRequest{ board_id, rotation_id, request_id,
  expected_owner, expected_epoch, expected_state∈{ROTATION_PENDING,SUSPECT}, new_writer_instance }
TakeoverOutcome{ outcome∈{applied,cas_miss,rejected,idempotency_conflict},
  owner, epoch, state, writer_instance, board_revision, event_cursor, reason_code }
```
- CAS+epoch+1+new writer_instance+目标 state+首 outcome+事件 **同一事务**;重放返同一 outcome;CAS loser 不得取 token/不得追新 epoch。
- **成功后目标 state 转移须显式给值**(v0.2 待轮换组补状态机转移表)。
- reason_code(shencedeepseek):失败可观测哪项不匹配(epoch/state/owner)。

### 1.3 节点写接口(具名 auth context + lease)
```
Send(nodeID, gen, rid, agentID, WriterContext) → Outcome
Processing(nodeID, gen, rid, WriterContext) → Outcome
Terminal(nodeID, gen, rid, done, resultRef, WriterContext) → Outcome
Reassign(...) → gen+1   // gen 对 nodeID 单调不复用(含 board 重启后,claudek3⑥防回绕)
running 带 lease_ttl(claudek3⑦):超时→board 自动 observed_stale+可 Reassign(回收挂死 worker)
```
- **O1 幂等冲突(五方共识,拒写+告警)**:持久化 `{rid, op_kind, canonical_payload_digest, first_outcome}`;同 rid+同 op+同 digest→返首 outcome;**同 rid 异 op/digest→返 `idempotency_conflict` 不覆盖+board 内部写 `idempotency_conflict` 事件**。canonical digest 算法+schema_version 须冻(防跨语言 JSON 排序假冲突)。

### 1.4 事件账 schema(遗漏C:权威重放源)
```
event{ schema_version, event_id, board_id, board_revision, event_seq,
  kind, task_id, rid/request_id, rotation_id, writer_snapshot,
  payload_digest, meta{result_ref,outcome_ref,...}, created_at }
```
- **event_seq 或 board_revision 定义单调域**(时间戳不作唯一排序键);事件与状态变更**同事务**提交。

### 1.5 集中类型注册表(遗漏·shencedeepseek3/claudek3⑮/dev56D)
- **error/outcome code 集中枚举**(禁消费者解析字符串):Outcome{applied,cas_miss,rejected,idempotency_conflict};reason_code 表;event kind 表(gate_result/status_note/observed_stale/idempotency_conflict/owner_op/...)。
- **时钟语义(遗漏D)**:TTL/lease/过期点=UTC 墙钟;进程内等待=monotonic;mock 注入 deterministic clock。

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
- SS(m0-final C-5):不持 task authority、不共享 writer_instance、以 SubmitContext 提交走 role×op 闸;SS 探活独立于 SA 信道/超时(claudek3③防相关性致盲),SS 探活不经 board 写。
- **pool Config(Min/Max)=资源参数≠席位参数**(shencedeepseek4/M0 C-5),接口层显式标注此约束;席位≤2 由 board C2.6 闸(见契约4)。

---

## 契约4·轮换接口(轮换组 owns,复用 m0-final)

SA/SS/SB=m0-final(tag,commit e49378b,**冻结锚补全**)C-1~C-10 全条,直接引用。组间接口点:
- 轮换↔board:takeover 五元谓词+writer_instance(契约1.2,重开门);board 成功后**状态机转移表待轮换组补**。
- 轮换↔usage(O7 展开,shencedeepseek/dev56):**M0b 探针冻结锚=3388882**(非仅"已冻")。边界=探针产样/adapter 归一/coordinator 判触发,**probe 不直接触发 rotation**。
  - sample schema 全列(不省略):`session_id/provider/model/source/normalized_used_tokens/max_context/sample_seq/observed_at/confidence/raw_value/raw_kind`(+opaque turn_id 若有)。
  - adapter outcome:accepted/stale/invalid/unknown/rebaseline_model/rebaseline_compaction/rebaseline_max_change。
  - coordinator 持久化去抖:2×≥0.75 / 1×≥0.85 / ≥0.95 紧急;重启不丢连续轮计数/不重复生成 rotation。
  - static max 表:exact model ID+官方源+版本+变更留痕;未知/变体后缀/表缺/native 冲突→fail closed,冲突取更小分母。
  - result.usage 仅取证不入 normalized_used_tokens;分母已知但分子未鉴定仍 UNKNOWN。
  - 供 deterministic fake samples + stale/compaction/model-switch 序列。
- 轮换↔worker:SA/SS 占席 vs C2.6≤2——**board 侧 C2.6 席位查询签名待补锚**(shencedeepseek/契约4)。

---

## 契约0·worker CLI 接口(遗漏L1·tester,调度组供)
wkr.py(真 worker 实现参考):`ack/done` 命令行+env(SH_FULL_NAME 全名自查)+返回码+纯 JSON 出线(禁附时戳,N12 教训)+错投 MISROUTE 拒+短名拒发。

## 契约·DeliverFail DEA 判定(遗漏L2·tester)
`deaTerminal` 布尔权威来源须冻:驱动 DEA 权威 vs board 推断?hardening.go DeliverFail 调用方明确。(DEA 三态 APPLIED/NOT_APPLIED_FENCED/UNKNOWN 复用既有冻结件。)

---

## 全局

### G1 mock/stub 形态(统一约定,非统一巨型 mock)
每契约 owner 随仓供:接口 fake+确定性 clock+fault script+fixture builder+conformance suite;公共约定统一 outcome/error envelope+sequence+time+trace 字段。**消费者只组装 owner fake,不复制实现/不自造不兼容 mock**。mock 件随契约进仓,版本随契约 sha。

### G2 契约版本 registry(仓内)
`{contract,version,owner,consumers,schema/blob SHA,compatibility,status}`。兼容新增=minor;删除/语义变/鉴权变=major+重开门。owner 起草+受影响消费者双审;**coordinator 只核完整性+登记**(claudek3⑫)。运行消息带 schema_version;未知 major 显式拒不静默降级。

### G3 验收锚
终稿 annotated tag `interface-contracts-v1`→不可变 commit,**认 tag^{commit} 不认分支 tip**。每契约+可执行用例记 blob/file SHA-256。规约23 用例与契约**同 commit 冻结**,签名声明契约 SHA+用例 SHA。跑通冻结用例即过;新争议先补用例生成新锚,不口头改判据。

---

## 收口顺序(dev56 建议,采纳)
1. 先闭 board vNext(遗漏A/B/C+O2 三元组)+conformance cases —— **重开门项,终稿轮/张宗楚确认**;
2. 并行闭 O7 usage adapter/trigger fake;
3. O3/O4、O5/O6 由 owner 按 G1 形态出接口+用例;
4. G2/G3 registry+annotated tag+用例 SHA 收口 Phase-0。

**第二轮审阅**:每人对 v0.2 变更+仍存疑,一次性汇总发起草人(走 git 指针);重点=board vNext 重开门项是否认、状态机转移表/C2.6 席位签名/DeliverFail 权威源三处待补是否补齐。
