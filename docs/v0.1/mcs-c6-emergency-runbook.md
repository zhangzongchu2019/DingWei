# C6 应急原语操作段(P3·操作员手册)

> 范围=criteria §1"C6 应急原语操作段随 C6 进 P3";完整部署/回滚手册 defer P5。
> 语义源=P2 契约 C0/C6(18ebdb1)+实现 `internal/mcs/board/c6_owner.go`。本手册不新增语义,只教安全使用。

## 0. 你是谁才能用

仅**业主 principal**(hub 鉴权后 ∈ `SetOwnerPrincipals` 业主集)。不是自称——伪造 from 无效(C0.1)。
调度者日常操作**不用**这两个原语(它有自己的 guarded 原语);这是业主对失控调度者的最后手段。

## 1. 前置检查(每次使用前)

1. **应急开关已启用?** 过渡期默认 `emergencyEnabled=false`=两原语硬禁用(返回 `disabled`,C6.3 fail-closed)。
   P5 上线时与 principal 鉴权就绪**原子共切**;没切之前应急路径不存在,别试图绕。
2. **读当前 epoch**:`SELECT epoch, owner FROM orchestrator WHERE board_id=?`(只读,任何时候可查)。
   两原语都要 `expected_epoch=当前值`;**陈旧值=cas_miss,不会自动重取**(dev56④)——这是特性:
   你看到的局面若已变化,你的应急决定可能不再成立,必须重新观察再发。
3. **准备幂等键** `idempotency_id`:一次应急决定=一个新键;**重投用同一个键**(返回原 outcome,
   不会二次执行);**绝不复用别人的键**(跨 principal 复用→直接拒,shence E)。
4. **写 reason**:落审计事件,事后要能读懂为什么动手。

## 2. owner_takeover(夺调度权,epoch e→e+1)

用途:调度者失联/失控,把调度权移交给 `to_owner`(必须是合法 `zzc-*` 身份)。

```
OwnerTakeover(boardID, {OwnerPrincipal, ToOwner, ExpectedEpoch, IdempotencyID, Reason})
```

| outcome | 含义 | 你该做什么 |
|---|---|---|
| applied | 已夺权,epoch+1,owner=to_owner | 通知新调度者 Takeover 后续续期(Renew) |
| cas_miss | expected_epoch 陈旧(或并发抢先) | **停**。重新读 epoch+重新评估,不要机械重试 |
| rejected | 非业主/键被他人用过/to_owner 非法 | 检查授权与参数;别换键硬闯 |
| disabled | 过渡期硬禁用 | 不可用是设计;走人工运维通道 |

后果须知:旧调度者所有 in-flight guarded 写从此 epoch 不符全部失效(这正是目的);
事件 actor=你的业主 principal,**不会**记成调度者身份(不冒充,dev56③)。

## 3. owner_override(单指令急停,不改 epoch)

用途:不夺权,只对单个节点急停/取消纠偏。directive **仅白名单**:`pause` / `steer-cancel`
(→node blocked)。白名单外一律拒——没有自由文本指令。

```
OwnerOverride({OwnerPrincipal, Target, Directive, ExpectedEpoch, ExpectedState, IdempotencyID, Reason})
```

`ExpectedState`=你观察到的目标节点当前态;不符=cas_miss(防陈旧急停打在已变更节点上)。
outcome 表同上。急停后节点 blocked,恢复走调度者正常 reassign 流程,不是再发一个 override。

## 4. 三条铁律

1. **cas_miss 后先看再发**:每次失败都意味着世界变了;机械重试同参数=对着旧世界开枪。
2. **一决定一键,重投同键**:键=你的决定的身份。换键重发=第二次决定,要重新走前置检查。
3. **不越白名单、不借道**:需要白名单外的干预=走升级链(C2.5→调度者→业主决策),不是硬闯应急面。
