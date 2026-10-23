# S-HELPER-INBOX-LAG A — R1/R2/R3 rework

本件承接组长对 `b541400596aa7754aaa81ab345e5d6e56400d2d8`（原执行源 `6a297ec4320a2e0224437849ba77206880aeccb3`）的 REWORK，仍为 `git.code.tencent.com:wego-ai/dingwei` 的 `review/coder/helper-inbox-lag-a`，不合并或部署。新执行源 `dd7794d55bdb022a0c7918fafd24c6b0ed846231`，tree `4f13a04800e4ba5426613adcb7350f1075fe3318`，直接父为原候选。差集仅 7 个实现／测试文件及本目录 CHECKPOINT；最终证据子提交只追加本目录，原 129 项审查材料保持不变。

## R1 — 当前 instance 未知数

采用组长允许的“只计本 instance 产生”方案，不把旧 processed 伪推为 model_admitted。helper 本地 delivery_progress 增可空 unknown_instance 列；旧六列表自动补列，原始行不删除。历史 processed／failed／cancelled 回填及上一 instance 已结束的 unknown 仍保持单信 unknown，但不计本次实时未知数。当前 mark(unknown) 打当前 instance 标签；启动时丢失内存队列／dispatch，以及 inbox 仍未处理的未知项，由本次恢复观察打标签。计数仅包含 stage=unknown 且标签=本 instance；实际后续确认推进阶段后移除标签。唯一 delivery 主键保证重复观察不重计。

View 明示“本次启动另有 N 条注入状态未知”。这不是历史所有未决状态的总数，当前零不证明旧信已入模型。三新增回归覆盖：预 A／已全量回填的旧候选六列表各 1500 条 processed，均无实时污染且不改 processed／claim；当前未知标记、去重与确认退出；上一 instance 旧未知不进入本次数字且仍可查 unknown；未处理旧信必须作为当前恢复未知而非默认为零。原混合历史回归从 2 改为 1：只计真实 queued/dispatch 恢复那条，不再把历史 processed 无进度那条加进实时数，两条单信阶段仍是 unknown。

R1 三刀分别撤掉计数 scope、当前 unknown 标签、未处理历史恢复标签，均语义断言红、恢复绿；首刀 1500 行在两个旧库模式都由期望 0 变为 1500，直接重现阻断问题。

## R2 — 发送方字段收敛

只从发信方 query／receipt 的 inbox_lag 中移除目标 connection_generation、sample_instance；内部同 sample 的阶段关联完成后再删字段。身份绑定 View 资源仍保留自身防乱序字段。新断言同时检查发送查询不泄露、View 不丢失；两次撤修各恢复一个字段均红，实际阶段查询仍正确。

## R3 — 旧连接写侧闸

补两格直接读旧连接对象状态：旧连接到达入口时，不查身份、不改其 lastInboxAttempt、不给任何旧／新缓存写入；身份重核时通过测试专用 Store wrapper 原子替换当前连接，旧缓存本身也必须保持 nil，新连接仍能正常上报。无生产 hook、无睡眠竞态。分别撤初次 current 闸、提交前 current 闸，两格各自红，已不再依赖读侧只读新连接而等价通过。

## 明确登记，不扩大本轮

- 旧无 durable ACK 能力目标：新 Hub 已协商回执时，普通 send_message 会写一次、通常耗满两秒等待预算后才回 unknown；desktop 普通 IPC mcp_send 共用此路径，发信会变慢。原桌面强确认入口仍独立。立即回 unknown 需增加可信、可关联的非 durable 控制结果，本轮不改协议、不绕过路由、不为超时重发。
- delivery_progress 保留／压缩与 snapshot 扫描、排序随历史增长的成本登记后续。本 instance 的未知也可能在长寿命进程中累积；本修正限制 live 计数范围，没有实施清理历史或滚动 TTL。
- 只有 helper 本地可空列迁移；无新 Hub 迁移、版本／锁／能力下限变化。默认投递、ACK、FIFO、权限、B urgent／steer／hook／新的 interrupt 均不变。未部署、重启或改生产 Hub/helper 配置，不构成上线 GO。

## 验证与取证

最终门见 VERIFY.json 与 raw JSON，均绑定新执行源及差集 SHA。canonical 无新增文件，仍 Python66／shell2；helper 三新格，980项全通过，隔离 HOME、既有锁定 venv、短 ext4 TMPDIR；全仓 Go 包串行、PG strict/embedded，另跑全仓 vet、Hub 定向 race、真实浏览器两宽度20项。Python27 与 Go 定向先验亦过；浏览器本次实际显示当前启动未知范围。

18刀＝原11刀原有语义复验＋本次7刀，逐刀记录原版绿→语义断言红→恢复绿、原／撤修／恢复 SHA 与命令。源码均复原，原 129 SHA 再验仍全 OK。完整原始 stdout/stderr 保留，既有负例诊断和夹具线程退出信息不等于未通过门，也未删去。

| 精确新执行源最终门 | 结果 | 秒 |
|---|---|---:|
| canonical Python66／shell2 | rc0 | 147.381 |
| helper 980 tests | rc0 | 436.697 |
| 全仓 Go：53 测试包／13 无测试包 | rc0 | 658.123 |
| 全仓 vet | rc0 | 2.024 |
| Hub 定向 race | rc0 | 49.662 |
| 浏览器20项／两宽度 | rc0 | 1.366 |
| 原129 SHA 全 OK | rc0 | 0.008 |

新的七刀逐项为 R1-history-counter-scope、R1-current-unknown-tag、R1-unprocessed-recovery、R2-connection-generation、R2-sample-instance、R3-entry-current-check、R3-prewrite-current-check；全部 0→1→0，且源码恢复 SHA 与新执行源一致。完整记录见 raw/withdrawals。
