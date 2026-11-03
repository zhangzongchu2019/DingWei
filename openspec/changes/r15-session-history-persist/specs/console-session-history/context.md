# Context

## 基线

- 仓库代码基线：r48 `1e17255f28ced042784337903914ca8c126ef4cf`，树 `ba202c84649fbdd1aa8e468ef583eed5d4375d37`。
- 提案事实来源：S-HISTORY-EMPTY-FACTS 只读报告提交 `1c7cc1e29`；仅引用其取样结论，不声称报告证明单次空历史必然由重启造成，也不读取或复制事件正文。
- PM-R15-01～09 及项目立项依据已写入 proposal：㉙按页面建议立项，㉚～㉞定值。**本次只补实施设计；保留 proposal 中原有裁定与历史选项原文，不覆盖或改写其记录。**

## r48 文件锚点（均为 1e17255f 的行号）

| 文件:行 | 当前职责/事实 |
| --- | --- |
| `internal/m8/terminal.go:480-503,522-558` | 已校验的结构化 `session_event` 进入事件发布；发布时分配进程 seq、写 ring，然后扇出消费者。同步持久写入切点在 provenance 清理后和 `publishSessionEvent` 前。 |
| `internal/m8/event_ring.go:3-9,45-57,79-92` | 现有 ring 每 terminal 16 MiB、全 Hub 128 MiB／128000 条，超预算删最旧。 |
| `internal/m8/terminal.go:873-904,922-953,955-1003` | View SSE 同一临界区注册订阅、取 H 和 ring 快照；cursor epoch 改变产生 `epoch_changed`，覆盖不足产生 `overflow`。 |
| `internal/m8/console_mux_frames.go:150-184` | mux reader 与 ring snapshot 同一 terminal 临界区建立。 |
| `internal/m8/console_mux_transport.go:689-727` | mux 对快照按所请求 cursor 选择 replay，缺 source 则不能启动流。 |
| `internal/m8/terminal.go:177-203`、`internal/m8/hub.go:1255-1265` | helper 连接关闭会清空进程 ring、关闭 reader 并删除 terminal state。 |
| `internal/store/sqlite.go:57-90,99-110,170-219,634-656` | 主 SQLite 配置 WAL、NORMAL、busy timeout 5000ms、单写连接；迁移内嵌、按名排序和账本记录。r48 当前最高编号 0071。 |
| `internal/store/migrations/0027_session_delivery.sql:1-19` | delivery envelope 和状态表，不是完整会话事件历史。 |
| `view-service/renderer-canary-sse.html:413-425,1099,1769-1792,1849-1875` | timeline 的独立滚动容器及现有贴底跟随行为；没有向上历史分页实现。 |

## 已知差异与边界

## 本次性能优化实现位置（2026-11-03）

- `internal/store/session_history.go`：Append 专用写协程和同步有界组提交；最多 32 条、最多收集 2ms，等待队列容量 32。每个 Append 在包含其持久 cursor 的事务提交后返回；关闭时排空写队列。组事务失败回滚并按单事件事务重试，隔离无关事件写入错误。
- `internal/store/session_history_test.go`：20 个并发 Agent 的组提交 cursor/落库一致性用例。
- `internal/m8/session_history_test.go`：隔离 20 Agent × 25 push 发布路径 paired p99 门；Append 仍在计时的 `handleSessionEventEnvelopeContext` 内发生，事务提交完成后事件才进入 ring/live 发布路径。
- 新增优化未引入表结构、公开接口或配置项；WAL、`synchronous(NORMAL)`、10ms SLO、失败时 live 发布和持久游标语义保持冻结值。

- eventRing 条目是结构化事件帧，不是用户/Agent 往返回合；持久表不得按回合数计量。
- `session_delivery` 与 `message` 不可由行数推导完整 Agent 会话历史；本设计不回填。
- r48 mux/SSE 使用进程 epoch，R15 持久代际和序号需独立定义；保留当前 gap/resync 的可观察原则。
- r48 SQLite 为主库启用 WAL 与单写者，历史 2 GB 上限会影响 WAL/checkpoint、备份与 VACUUM；独立库建议和文件位置仍待组长定。
- 凭据撤销、owner 变更时必须挂接到权威 store/lifecycle 事务；具体代码挂点须由实现承办人对 r48 之后的合入现状复核，设计不虚构具体 handler 名。
- 兼容 r48 在带 0075 schema 的库上启动只能隔离实测，不能根据其迁移遍历行为推断。
