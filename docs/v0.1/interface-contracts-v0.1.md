# 多CLI调度 组间接口契约 v0.1(初稿·devo48 起草)

**目的**:Phase-0 依赖先行——冻结组间接口,让信道组/调度组/轮换组基于契约**独立并行开发+各自 mock 掉对面**,零运行时依赖。
**流程(张宗楚 07-27 定)**:初稿→全员一轮汇总意见(规约21 攒批,不单条不单发)→修改稿→再一轮→终稿;终稿后原则不改(除非张宗楚加一次讨论)。讨论走 git 指针不单发全文(规约24)。
**依赖图(实测)**:board=根(谁都依赖);pool 独立;dispatch/hubdriver→board+pool;route→board;mcsd→全部。故 **board 接口最优先冻**。

---

## 契约1·board 接口(根依赖,最优先)

board=权威账,唯一可信状态源。所有写经 guarded CAS,会话状态不作权威(可失忆重放)。

### 1.1 Identity(写者身份)
```
Identity{ Owner string; Epoch int64 }        // 现行
Identity 扩展(M5 需,C-6):+ WriterInstance string  // 不可复用 fence_token
```
- **扩展点**:writer_instance 是对 P3 冻结件的扩展,须走"重开门"双审(不得各组自改 board)。

### 1.2 CAS 谓词(唯一夺权/写入原语)
```
Takeover(boardID, me, ttlSec) → epoch    // epoch e→e+1,单调 int64
写谓词 = (id, expected_epoch) AND RowsAffected==1 自足   // 现行
M5 扩展 takeover 单事务谓词(m0-final C-6/C-9):
  (id, expected_owner, expected_epoch, expected_state∈{ROTATION_PENDING,SUSPECT}, rotation_id)
  成功原子更新 {epoch+1, writer_instance, state};RowsAffected==1 才成功
```
- observed_stale 由 board 闸内部拒后记录,旧 writer 无写审计权。

### 1.3 节点写接口(guarded,幂等)
```
Send(nodeID, gen, rid, agentID, id) → Outcome{applied|cas_miss|rejected}   // 记账先行
Processing(nodeID, gen, rid, id) → Outcome     // dispatched→running
Terminal(nodeID, gen, rid, done, resultRef, id) → Outcome  // 终结,started_ts COALESCE
Reassign(...) → gen+1 换代重派
priorOutcome 幂等:同 rid 重放返原 outcome(异 payload 须告警,见开放点)
```

### 1.4 事件账 schema(可观测,重放源)
```
event{ kind, task_id, principal, meta json, ts }
kind ∈ {gate_result, status_note, observed_stale, owner_op, ...}
```

### 1.5 open(接口层需各组确认的点)
- O1 同 rid 异 payload:priorOutcome 是否加告警事件?(轮换/调度组关注)
- O2 writer_instance 扩展的重开门时点(轮换组主谈)

---

## 契约2·信道接口(信道组 owns,调度组消费)

### 2.1 调度↔hub 线格式(已在 hubdriver 冻,复用)
```
mcs_dispatch_v1{ kind, node_id, generation, rid, payload, from, to }  // 收发人必填,缺拒
mcs_receipt_v1{ kind, node_id, generation, rid, phase∈{processing,done}, ok, result_ref, from }
rid=幂等键;未知 kind/phase=显式拒(入口闭合);ParseReceipt 首对象容错(尾部杂质不破解析)
```

### 2.2 hub↔飞书桥接(信道组拆分后定义,现耦合于 sessionHelper)
```
拆分目标:①WS 连接层与 driver 执行层解耦(driver 挂死不连坐 WS=Type B 修)
         ②hub 可独立起停+stub(测试用)
         ③飞书 unlock 可 mock(不连真飞书)
接口(待信道组拆分时冻):deliver/回执/事件/interrupt(参 contract.py 现有 Driver 抽象)
```

### 2.3 open
- O3 桥接↔driver 解耦后的接口形(信道组拆分时定,本契约先占位)
- O4 stub hub / mock 飞书的接口契约(调度组+A 组验收要用,信道组供)

---

## 契约3·worker 接口(调度组 owns)

### 3.1 pool 抽象(已冻,复用)
```
Worker{ ID()string; Health(ctx)error; Caps()Caps; Deliver(ctx,Envelope)error; Stop(ctx)error }
DriverAdapter{ Spawn(ctx)→(Worker,error) }
Envelope{ NodeID, Generation, RID, Payload }
```
- Health 现=只读判据(在线清单);双向 challenge 探活=M4 扩展(调度组)。
- SS 语义(m0-final C-5):SS 不持 task authority、不共享 fence_token、以自身 principal 提交 result envelope 走 role×op 闸。

### 3.2 open
- O5 池 spawn 全死/全忙区分(调度组 M3)
- O6 SS 级探活独立超时(调度组 M3/M5 交界)

---

## 契约4·轮换接口(轮换组 owns,复用 m0-final)

SA/SS/SB 会话轮换=m0-final(tag,commit e49378b)C-1~C-10 全条,已三签冻结,**本契约直接引用不重写**。仅标组间接口点:
- 轮换↔board:takeover 五元谓词+writer_instance(见契约1.2 扩展,重开门)
- 轮换↔usage:M0b 探针 sample schema(session_id/sample_seq/normalized_used_tokens/max_context...)已冻
- 轮换↔worker:SA/SS 占席 vs C2.6≤2(见契约3)

### open
- O7 M0b usage→M5 75% 触发接线(轮换组内)

---

## 全局 open(全员一轮汇总提)
- G-1 各契约的 mock/stub 形态是否统一约定(便于跨组复用)?
- G-2 Phase-0 冻结后,契约版本管理(改动走重开门,谁登记)?
- G-3 验收(规约23 用例)锚契约 sha,契约本身进 git 哪个分支/tag?

---
**审阅方式**:每人对 O1-O7+G1-G3+任何遗漏,**一次性汇总**发起草人(devo48),走 git 指针不单发。起草人不单条回,回应体现在修改稿。
