# G1-M1 派发桥·出件映射表(评审用)

件=排期 v0.3(67364768) L1-M1;基线=P3 冻结件(18ebdb1 契约 v1.5+五代码件)+M0 轮换契约 v0.7(e49378b)。
commit 链:dbacc79(增量1 外呼)→360e1b4(增量2 回执桥)→6547add(增量3 daemon 挂载)。
评审:tester(负例)+shencedeepseek(sign-grade),组内直连。

## 范围(M1 边界,排期冻结)
- 做:board 派发面→真 CLI 会话外呼(经 hub send.py 信道)+回执线格式→事件账。
- 不做:真拉起 CLI 进程(M3 池域)/双向 challenge 探活(M4 域)/SA-SB 轮换(L2 域)/面板(并入 M2)。

## 契约条→代码点→负例
| 契约依据 | 代码点 | 负例(测试名) |
|---|---|---|
| M0 3.1 记账先行→外呼 | dispatch 层既有;hubdriver.Worker.Deliver 仅外呼半边 | TestSendFailurePropagates(外呼失败透传,dispatch 按 at-least-once 处置) |
| M0 v0.7 C-5 worker=SS 级不持权威 | hubdriver 包注释锚定;回执只经 guarded 入口入账 | TestReceiptStaleGenerationGuarded(错代回执 board guard 拦) |
| M0 v0.7 C-6 迟到回执板闸内拒 | ReceiptSink/daemon 只透传,裁决在 board 谓词 | 同上+错代不改权威终态断言 |
| P3 集成件·入口闭合(unknown_op 同款) | hubdriver.ParseReceipt=唯一解析权威(kind/phase/必填校验) | TestReceiptUnknownKind/UnknownPhase/MissingRID/BadJSON+TestHubReceiptEntryClosed |
| 健康判据 fail-closed(P3 F3 教训) | OnlineList.alive:清单过期/不在列=拒 | TestStaleListFailClosed/TestDeliverDeadWorkerRefused |
| 池语义:死会话不静默(I4 同款) | Adapter.Spawn 轮转跳死,全死显式错 | TestSpawnAllDeadExplicit/TestSpawnSkipsDead |
| Terminal 归还 worker(daemon 既有) | Daemon.HandleHubReceipt→ReceiptTerminal(不旁路池归还) | TestHubReceiptE2E(线格式全往返收敛 done) |
| 团队规约:结构化正文不过 shell 内联 | SendPy=临时文件+--file | (构造性保证,无负例) |
| 双入口一致性(tester G1) | ParseReceipt 唯一解析权威+board 谓词恰一 | TestReceiptDuplicateDualEntry(双入口重复投递恰一生效) |
| 回执先于派发(tester G2) | 记账先行不变量(M0 3.1)+幽灵 rid 显式拒 | TestReceiptBeforeDispatchRejectedExplicit |
| 健康窗口漂移(tester G3) | at-least-once:窗口内投递丢失→sweep redeliver 收敛 | 既有 TestS6_LostReceiptsRedeliverConverges(daemon 层) |

## 线格式(冻结待审)
- 派发:`mcs_dispatch_v1`{node_id,generation,rid,payload,from}
- 回执:`mcs_receipt_v1`{node_id,generation,rid,phase∈{processing,done},ok,result_ref,from}
- rid=幂等键(执行点拦,board 谓词);未知 kind/phase=显式拒不猜。

## 已知限制(诚实条款)
1. Health=在线清单只读判据(新鲜≤2min+在列),非双向 challenge——M4 补,判死误差窗=清单刷新周期。
2. Spawn=绑定既有活会话,worker 会话侧的信封消费/回执发送为会话内约定(真 CLI 工人接入的 worker 侧半边=M2 主体)。
3. SendPy 依赖本机 send.py+sessionHelper 凭据,无凭据环境 Deliver 必败(fail-closed,可接受)。
4. 两个回执入口(ReceiptSink=纯账/daemon=账+池归还)共用 ParseReceipt,语义差异已注释——评审请核此分叉可接受性。

测试账:hubdriver 13(7+6)+mcsd 新增 2,全 -race 绿;全 mcs 套件绿。
