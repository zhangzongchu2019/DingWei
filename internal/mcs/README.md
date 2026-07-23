# mcs —— 多CLI调度引擎与模拟测试台

多CLI调度(multi-cli-scheduler)的实现(phase-1 M1-M5v2)与端到端模拟测试台(phase-2 E1-E4)。
**语义唯一权威=两签冻结设计件**(`multi-cli-scheduler-{spec-s6-v0.5,s7-consistency-model,M0-board,M1-view,test-spec}.md`,
两签@f41d686 家族);本目录代码照抄实现,注释只标条目号不复述设计。

## 包结构

| 包 | 职责 | 里程碑 |
|---|---|---|
| `pool` | worker 池:起停/伸缩[2,5]/round-robin/reap 判死/有界缓冲/A案链① 亲和 pick | M1 |
| `board` | 权威账:冻结 M0 DDL 全表+guarded 原语(send/processing/terminal/reassign/租约/steer/interrupt/redeliver/deliver_fail/outbox)+fan-out+reconciler+M5v2 治理(角色三元组/台账/RoleStats/全事件 actor) | M2/M4/M5v2 |
| `dispatch` | 粘合:记账先行→外呼;J2 failover 全链;origin 亲和提示 | M2/M3 |
| `mcsd` | 守护:租约循环/调度循环/reconciler 定时/孤儿扫描/回执入口 + 真子进程 loopback 驱动(unix socket) | E1/E2 |
| `cmd/mcsd` | 可执行入口(env `MCS_WORKER_SOCK` 触发 worker 模式) | E1/E2 |

## 一条命令跑全部(验收门)

```
make mcs-sim
```
= vet + 构建 mcsd + 包级套件(-race)+ 模拟台剧本(-race -count=3 -p 1,真子进程,验确定性)。
全绿输出 `mcs-sim: ALL GREEN`。单独手感:`go run ./cmd/mcsd -board /tmp/b.db`。

## 八场景剧本(E3,`mcsd/scenarios_test.go`,全部真子进程栈)

| # | 剧本 | 断言要点 |
|---|---|---|
| ① | 池伸缩 | 压满 1→3 扩,idle 超 TTL 缩回 1(I1/I2/I3) |
| ② | 轮换 | 4 件铺开到 2 worker(J1) |
| ③ | 亲和 | 修订件派回原 worker;原亡→静默降级(V11/A案) |
| ④ | failover | 在飞 SIGKILL→判死→换代 gen+1→新 worker 收口(J2 全链) |
| ⑤ | 守护自崩恢复 | 在途账+守护亡→新任 takeover epoch+1→孤儿扫描自动 reassign→零人工收口+旧 epoch fence |
| ⑥ | 投递故障 | 丢双回执→redeliver 同 rid→执行点幂等→不换代收口(V21 族) |
| ⑦ | steer/interrupt | 栅栏拦 reassign→finalize 放行;interrupt 穿越换代,旧代 finalize 事件记预留代际(O31-O33) |
| ⑧ | 治理 | 缺角色拒单/台账(起止/进度/Dispatcher)/RoleStats/零匿名事件(M5v2) |

## 测试↔冻结 spec 断言对映(抽样版,完整=各测试注释内条目号)

| spec 条目 | 测试 |
|---|---|
| I1-I5 池伸缩/caps | pool:TestI1..TestI5 + 剧本① |
| J1-J3 轮换/failover | pool:TestJ1..J3;dispatch:TestJ2_FullChain;剧本②④ |
| C4/C5/C6/C8 代际与 CAS | board:TestC4/C5/C6/C8 |
| guard 五项(owner/lease/epoch/rid/steer 互斥) | board:TestGuard_ExpiredLease...+TestSteer_* |
| V42ab terminal 直达窗(MC2-1/MC3-1) | board:TestV42a/TestMC1_V42b;mcsd:TestE2_DirectTerminal |
| MC1 UNKNOWN 注记/MC2-3 归零 | board:TestV38_*/TestMC23_* |
| V21 redeliver 预算/计数分离 | board:TestV21_*;剧本⑥ |
| V31 result_outbox 每版恰一次 | board:TestV31_* |
| O31-O33 interrupt B 模型/互斥 | board:TestInterrupt_BModel/TestReservation_*;剧本⑦ |
| #15 双集恒空兜底 | board:TestSweep_TimeoutStray...;reconciler |
| V11/V44 亲和/零会话面(A案) | dispatch:TestV11_*/TestV44_*;剧本③ |
| V13 timeout×真实 receipt | board:TestSweep_TimeoutAnnotate... |
| M5v2 治理(建单必填/CLI-LLM/RoleStats/零匿名) | board:TestM5_*;剧本⑧ |
| 崩溃恢复(预留=恢复凭据/孤儿接管) | board:TestCrashRecovery;mcsd:TestE1_Restart/剧本⑤ |

## 范围边界(诚实标注)

- worker=loopback 模拟(不烧 LLM);**真实 CLI 驱动+DEA Q1-Q4 逐 driver 实测=后续段**,能力表在此之前 fail-closed;
- Planner 建图(K 组)/看板 UI(M 组)/灰度=排期项;
- fenced session handoff(B 案)=未来独立增强窗(条件见 M1-view 非规范段);派发路径零 resume(V44 负测守住)。
