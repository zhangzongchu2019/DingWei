# S-R55-MIXED-PRECISION-GUARD 交审件

01a1bacf-9317-7b91-bf86-0a0c88c834f6；to=opus50tc2k.zzc；2026-11-08 UTC。基线 opus50tc2k/r55-integration / 7b123ca4a51fdde77a417d3ce7f03d01235d23ca；分支coder56solzzc/r55-mixed-precision-guard。依据〔51〕-补7 c2b64423（前条误号已更正）、已复核分析24b4b644。最终commit/tree在回执给出，不自验收。

## 1. 改动及约束

所有版本化改动仅在tools/sessionhelper内。产品只改sessionhelper/inbox_lifecycle.py与driver_adapter.py，测试补在既有test_dispatch_lifecycle.py、test_codex_delivery_reconcile.py；无新测试库存文件。此目录含自足矩阵/证据、任务内OpenSpec交接包；遵从本单只改helper目录，仓根openspec、Hub、View、VERSION、迁移及ACK运输实现inbox_ack.py都未动。

record_native_terminal在同一现有事务中、写入admitted/processed之前核验将发出的processed wire时间是否早于原injected wire时间。时间采用现有utc_stamp编码后的UTC值比较，匹配Hub实际收到的微秒精度，不拿未序列化浮点数替代wire比较，不clamp或改原始数值。

- pending injected ACK：优先读取其payload_json.occurred_at，原文绝不更新。没有持久“已发送”位，因此整个outbox都保守视为可能已发送；不能借“尚未确认”重写。
- 已确认injected ACK：确认会删payload，但原admitted_at保留；在本次可能替换它之前，用相同utc_stamp恢复已发出的wire时刻。已有正确终态仍幂等返回，不回退为pending。
- 候选terminal更早：只写既有error列为 `native_terminal_precedes_acked_injected` 及诊断更新时间，返回False。原state（mixed为admitted）、turn、admitted_at、processed_at/outcome保持；不生成processed ACK，不额外生成unknown ACK。
- Adapter识别False，打印明确pending原因并停止terminal progress观察。原生状态/输出与投递ACK仍分开，不能用渲染或driver done补终态。

原生证据不改写：实际只读reconcile先把原proof存到driver已有detail.reconcile.evidence，护栏不写driver账本、也不改变started_at/completed_at/response摘要。相关单测通过真实reconcile、重复调用及新driver重开，核原proof字节仍一致。本次不新增迁移/配置，也不扩大原始事件的既有保留保证。

三个冻结值保持1s/15s/200；不修改Hub processed>=injected保护。不改ACK原文、协议或代次，不重投任务。guard只针对ACK v2；历史/v1语义不被重标。

## 2. pending如何收口，为什么不无限重发

1. 冲突时不入processed、不创建processed outbox，故没有一条Hub必拒的processed ACK可被每秒重发。原queued/injected若未确认，仍按既有可靠运输重发其原文；网络恢复后正常确认可清空，与新增无效终态循环不同。
2. 错误码跨DeliveryInbox重启保持，admitted不是可重新claim的received，重开不会再次提交turn/start。相同冲突证据重复到达仍pending、原ACK不变；真实driver对已缓存terminal不继续原生查询，同一proof事件在每进程仅按既有稳定键发一次，不改变15s等预算。
3. **可接受后续独立一致证据**：同delivery/turn/context、完整原始时间且其serialized terminal不早于原ACK边界时，既有入口可转processed、清error并只产生原生时刻的processed ACK。单测验证该能力，不承诺当前秒级证据会随等待自动变得一致。
4. 默认收口是人工：已缓存的同一终态不会因等一段时间而改时间，代码不新增自动重查/改时刻。操作方读到该原因码后，在节点本地核原Hub injected、helper outbox/cursor和driver原proof，明确时钟域/精度；仍无一致原证据就继续保留pending，后续修复/历史处理须另派审定。不得用当前时间、固定offset、删除ACK、重建delivery_id或重执行任务“收口”。本护栏不是人工修库授权。

本单修的是新终态提交屏障；不自动清理旧版本已经错误落processed的历史行/已生成ACK。r55尚未部署的前提见〔51〕-补7；若另有历史坏行，需只读核实并单独处理，不能在本单改写不可变回执。

## 3. 本地验证与复现命令

普通用户、锁venv；均为临时SQLite与synthetic数据/本地协议夹具，无真实Hub/helper/native服务、无节点或现网操作。

```sh
PATH=/tmp/coder-0232-locked-venv/bin:$PATH PYTHONPATH=tools/sessionhelper python3 -m unittest -v tools.sessionhelper.tests.test_dispatch_lifecycle tools.sessionhelper.tests.test_codex_delivery_reconcile tools.sessionhelper.tests.test_codex_stale_thread
PATH=/tmp/coder-0232-locked-venv/bin:$PATH python3 -B tools/sessionhelper/reviews/r55-mixed-precision-guard/reproduce_matrix.py --output /tmp/unique-guard-matrix
PATH=/tmp/coder-0232-locked-venv/bin:$PATH python3 -B tools/sessionhelper/reviews/r55-mixed-precision-guard/reproduce_matrix.py --remove-guard --output /tmp/unique-guard-mutant
PATH=/tmp/coder-0232-locked-venv/bin:$PATH sh scripts/run-build-tests.sh
```

每次输出目录必须新建。矩阵使用r54 Git源码与当前候选checkout的tracked Python文件，记录两产品文件SHA；Hub只用原实际Store的Go overlay夹具，不修改Hub文件，-vet=off仅用于虚拟测试文件路径，canonical不变。baseline-matrix.json是24b4b644已复核矩阵原文，使8格比较自足；无需为运行重新获取真实样本。

| 判据 | 结果/证据 |
|---|---|
| mixed | helper=admitted/pending、error精确原因、processed_at=None，无processed ACK；Hub=原injected，无拒收terminal，无false terminal progress |
| 其余8格 | CONTROL-COMPARISON.json逐格比较状态、错误、原证据及稳定wire字段，均同24b4b644；只排除运行期unknown观察时刻 |
| 撤护栏一刀 | 仅在隔离worker令比较函数返回False，rc1且红在MIXED_PRECISION_MUST_REMAIN_PENDING；不改工作树、不改Hub或常量 |
| 三套相关单测 | 86项rc0（原80＋6）；含pending/confirmed、重启/重复无ACK、后续一致收口、wire相等边界、pending原文优先、真实driver proof保留 |
| canonical | rc0，双profile各1127/1127，另27Python/2shell；见RUNS.json及原文 |
| OpenSpec | 此目录为task-local项目，运行openspec1.16.0 validate r55-mixed-precision-guard --strict，兼验现状spec；不在本单修改仓根规格 |

已有一条test_codex_delivery_reconcile用例原先要求同秒native终态替换浮点观察并落processed，正是已复核的不一致行为。本次将其改名并收紧为pending/原因/原admitted不动、无processed ACK、原injected payload逐字不变，没有删用例或降低阈值。新增proof测试首跑少导入_reconcile_metadata导致NameError，已补测试import重跑；未把该基础设施错误当有效变异红。

原文/UTC/返回码在raw，SHA256SUMS核raw，ARTIFACT-SHA256SUMS核产品/测试/矩阵及交接件。单元与矩阵rc0是本席验证，不替代组长独立验收。包分发/版本抬升/部署仍由集成发布另排，本单未生成或上传二进制。
