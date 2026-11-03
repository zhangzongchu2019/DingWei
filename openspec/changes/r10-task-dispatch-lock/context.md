# R10 技术前置与当前实施

当前：T04～09实施与规定自测已完成，独立验收/部署未完成；最终挂点与证据见文末。以下A01～04为r48实施前基线事实，保留历史，不代替当前状态。

核对对象固定为 `1e17255f28ced042784337903914ca8c126ef4cf`。下列文件:行均指该基线，不是当前文档分支。结论区分可复用的既有能力与R10尚未接线的部分；不据工蜂交件或本机访问能力推断产品适配，也未探测真实 GitHub/飞书。

| 编号 | 判定 | 基线事实与证据（文件:行） |
|---|---|---|
| A01 R5提供方 | 部分具备 | R5默认关闭，必须 `DW_OPENSPEC_READ_ENABLED=1`，启动时还要求有效服务端凭据文件与允许的GitHub repo ID；构造的Provider map虽列出 `github:github.com` 和 `gongfeng:git.code.tencent.com`，但后者 `Repository/Authorize/Resolve/Tree/Blob` 一律返回 `ErrUnavailable`。`internal/m8/console_openspec_read.go:17-40`；`internal/specread/service.go:181-194`。GitHub provider用绝对路径credential file，从受限权限文件每次读取Bearer token；服务端不把token交给调用方。它通过GitHub API读取repo、collaborator permission、ref解析、tree/blob；权限值read/pull及更高接受，单次服务读取最长15秒。`internal/specread/github.go:24-85,127-145,207-268`；`internal/specread/service.go:92-115`。所以GitHub只读能力存在，工蜂产品读取不具备；R5不是Git写能力。 |
| A02 受派Agent身份/read ACL | 部分具备 | 稳定Agent主键是 `np_agent.id`；Agent记录含 `OwnerLC`、`GroupID`、state及凭据hash。`internal/identity/model.go:135-155`；PG schema以 `np_agent.id` 为主键。`internal/identity/pgschema.sql:45-65`。现有Agent任务接口从Bearer凭据认证当前active Agent，使用认证返回的Agent ID作主体；只列出/允许当前分派给该Agent且经当前owner/group授权的本地Console task。`internal/m8/console_task_flow.go:44-59,114-181`；`internal/identity/task_authorization.go:17-69`。但R5 `Service.Read` 的actor是Console principal，并按该principal读取自己的绑定/提供方ACL，没有Agent身份到R5 reader的映射或 delegated-task R5 read路径。`internal/specread/permission.go:8-36`；Console principal取自登录session用户。`internal/m8/newplat_view.go:223-244`。因此稳定ID及本地任务ACL已具备，Agent能否以受派身份通过R5读仓库任务未具备。 |
| A03 权威锁事务与时钟 | 部分具备 | 任务流当前由SQLite实现：DB启用WAL、busy_timeout且连接池最大连接数为1（单写者）；task flow赋值/更新用 `BeginTx(ctx,nil)`，未显式指定SQL隔离级别。`internal/store/sqlite.go:57-66`；`internal/store/console_task_flow.go:98-114,250-261`。并发写有串行基础，但现有任务表不是R10跨版本锁，未见R10的唯一互斥键、租约、代际或续租模型；不能据单连接推称“50并发恰一授权”已成立。时间由应用调用方传入 `at`，Hub的 `h.now()` 默认 `time.Now()`，仅测试时可注入 `NowFunc`，不是数据库权威时钟。`internal/m8/console_auth.go:53-58`；`internal/m8/console_task_flow.go:208-212`。PG事务设施存在于repo pool/identity PG store，`InTx`接收`sql.TxOptions`但默认可为nil；身份PG store用于身份域，不是Console task flow/锁的PG实现。`internal/repo/pool.go:117-140`；`internal/identity/pgstore.go:17-25`。 |
| A04 MR/ref监测 | 部分具备 | GitHub provider可把给定branch/ref经 `/repos/{name}/commits/{ref}` 解析成40位SHA，并读取该固定commit；R5 Service不缓存授权。`internal/specread/github.go:258-269`；`internal/specread/service.go:92-130`。Provider接口只暴露Repository/Authorize/Resolve/Tree/Blob/Source，没有MR/PR状态或变化订阅方法。`internal/specread/service.go:72-79`。另有 changeflow 的 Snapshot/Reader 抽象字段可承载 `CurrentHead`、`MergedAt` 等事实，但 GitHub 与工蜂 Provider 的 `WithAccess` 当前都直接返回 `ErrUnavailable`，不是可用的产品适配或轮询/回调。`internal/changeflow/types.go:85-116,126-145`。在该基线产品里没有R10 ref轮询器或Git托管回调接入；工蜂Provider本身不可用（A01）。因此手动读取ref tip可复用，持续MR状态/ref漂移监测不具备；未据仓库工蜂交件能力推断产品工蜂适配。 |

## 结论与边界（r48历史核实）

A01～A04的源码事实盘点完成，状态均为「部分具备」，T-03.2核实工作可关闭；R10执行前还需补上工蜂provider、Agent到R5授权主体的可信映射、可证明的锁表唯一性/租约事务/权威时间方案，以及明确选择轮询或回调并实现监测。本文不表示R10锁已实现，不证明50并发性质，不批准任何产品或真实托管操作。

## S-R49 实施设计交接（历史：当时未实现）

设计以SQLite新表 `r10_task_lock` 的K联合主键＋generation/token CAS为单任务唯一权威；建议迁移0073只建表/索引，新增审计、operation与通知outbox同事务提交。60分钟租期、每15分钟续租、到期只回收、每60秒监测、变化后5秒提醒、逐仓隔离沿用㉒ PM值。SQLite单连接池只说明当前Hub内单写者，不证明多进程/跨主机互斥。基线证据仍为 `internal/store/sqlite.go:57-66`、`internal/store/console_task_flow.go:98-114,250-261`。

首期设计只启用已具备只读接口的GitHub provider；工蜂provider明确列后续。Agent映射按认证Bearer的 `np_agent.id` → 当前Identity PG `np_agent.owner_lc` → 同值 `np_user.user_id_lc` → R5授权检查设计；该路径是Agent所属用户授权委托，不是Agent独立GitHub身份，需组长确认是否满足“受派者独立read”。provider每60秒轮询ref和新增只读PR元数据；异常记录comparison_unknown并关闭继续/续租的成功证明，不作无变化结论。基线能力锚点：`internal/identity/model.go:135-155`、`internal/identity/pgschema.sql:45-65`、`internal/specread/service.go:72-79,181-194`、`internal/specread/github.go:258-269`。

待组长定：Agent使用owner R5权限是否满足受派者read语义；部署是否保证单Hub/单SQLite锁权威，若不是则另定共享锁协调。此交接是设计，不表示新增表、provider方法或轮询器已实现。

## 当前实施挂点（S-R49-R10-IMPL，2026-11-02）

本轮唯一写者01a19c8c，从r48普通merge冻结设计e229c671，分支coder56solzzc/r49-r10-impl；产品归r50，版本不抬。上方r46/r48技术前置与原“待实现”是历史快照，当前已获T04～09实施授权并完成规定自测，尚未独立验收或部署。

- `internal/taskdispatch/service.go:120`：`func (s *Service) Execute`。
- `internal/specread/dispatch_read.go:55`：`func (s *Service) ReadPullRequest`。
- `internal/specread/dispatch_read.go:86`：`func (s *Service) ReadDispatchSnapshot`。
- `internal/taskdispatch/source_compare.go:41`：`func (s *Service) observe`。
- `internal/store/task_dispatch_lock.go:104`：`func (s *SQLite) ApplyTaskDispatch`。
- `internal/taskdispatch/read.go:20`：`func (s *Service) Inspect`。

现实现：可信Console用户/专属Agent上下文→当前np用户/Agent/owner→预投影R5 principal→WithTaskAuthorization当前编组/状态/owner锁→每个角色分别现核R5固定tasks/read，不能借另一个角色的服务令牌。PG回调不能再调MemStore身份getter；实际PG与SQLite非分布式事务，SQLite失败/PG释放结果未知不报成功，另走失败审计。R5绑定非秘密快照在SQLite事务内再核，unbind/替换不能借之前读取结果授予。

R5现有GitHub GET扩展只读PR元数据，核stable base repo ID，仍由同一服务AuthorizeRead前后现核；没有新凭据或Git写/SSH通道。固定任务SHA/UTF8/size与tasks唯一编号校验，完整Markdown快照拒symlink/module/坏文件，比较只用原R7 ConservativePolicyA。未开PR显式pr_not_opened；提交必须已登记可读PR，登记后未知阻断。

当前锁/operation/event/outbox为0073独立四表；原70个迁移SQL未改。operation重放保留原结果但重核当前身份/绑定和current_lease，旧代际不返回旧令牌充当前许可。事件只存坐标/哈希/主体/状态及指针，不写正文或提供方凭据。阶段三当时仅有开发态相关store/parser/服务/PG反测，故当时T04～09未勾；当前完整门及交接状态见文末。


### 阶段四：运行期接线与恢复（2026-11-02）

- `cmd/workpulse/console_routes.go` 新增独立 Console/Agent 锁和 batch 路由，沿用已有认证中间件；`cmd/workpulse/newplat.go` 在身份/R5装配完成后显式启动。`internal/m8/task_dispatch_lock.go` 将远端轮询和 outbox 投递分为两个受管循环，避免慢轮询阻塞已发现变化的提醒；默认关闭入口无R10依赖访问。
- `internal/taskdispatch/monitor.go` 按 repo/ref/PR 分组并行，一组共享一次可变坐标读取、每个holder重核权限；`batch.go` 逐仓部分结果与重启不确定投递；`notice.go` 在实际投递前再核身份/当前代际；`read.go` 返回当前资格、历史分页和监测落后状态。
- `internal/m8/task_dispatch_batch.go` 接线逐仓结果；新增路由仅消费可信认证上下文、拒未知JSON字段/额外JSON/跨站请求。未带R10标签的原信封在新增检查最前返回，不触碰R10表。可靠提醒只记hub queued，不能宣称CLI打开、开工、勾选或验收。
- `internal/store/task_dispatch_restart_test.go` 用单独测试进程重读持久锁/到期边界并验证不确定outbox；`task_dispatch_compat_test.go` 用真实r48二进制创建70行账库、复制后升0073，原70行哈希不变；旧Hub在升级、手工回滚及失败迁移之后均启动。旧Hub READY/healthz/关闭/副本公共夹具取自本会话R14冻结源码cf6134a6对应兼容测试，改为R10隔离路径与0073断言，没有移入R14产品实现或0072。
- `rollback.sql` 是未授权生产执行的离线运维脚本：保留新表与历史、原子记录活动锁撤销操作/事件、推进代际并失效token、pending/attempting改unknown。重复执行无活动锁不重复撤销，不删旧表或原70行账。现网执行、部署和独立验收仍须另令。
- 当前阶段开发态验证不替代最终冻结门；完整18格、语义变异、最终race三轮与全门结果随证据分支交件，T04～09待全部实际完成再勾。


## 最终实施状态与证据（2026-11-02T21:11:01.453194+00:00）

本单T04～09已完成实现与规定自测，T10独立验收/发布仍未完成。前文各阶段“待完成”是当时记录，本段为当前状态。产品冻结85fefd3e；后续只增强测试和交接文档，分包输入一致性、1356×3覆盖与39个有效变异在仅docs证据分支evidence/coder56solzzc-r49-r10-impl，逐格SCENARIO-MATRIX.json、RACE-COVERAGE-RESULTS.json、SKIP-LEDGER.json、PACKAGE-INPUT-EQUIVALENCE.json及原文侧车；最终坐标见交件，不能把任务勾选当独立验收。

当前挂点：`internal/taskdispatch/service.go`组装可信主体/当前PG编组与R5委托，`transition.go`纯状态规则，`internal/store/task_dispatch_lock.go`同事务CAS/operation/event/outbox，`task_dispatch_lock_read.go`当前与分页历史；`source_compare.go`复用R7策略与R5完整快照，`monitor.go`逐仓并行15+5秒读/落账界限；`internal/m8/task_dispatch_lock.go`显式装配后分别启动轮询/通知循环，`task_dispatch_batch.go`逐仓部分结果。固定源只存K/C/tasks blob/行号与交接定位，不落正文。释放的不可变时刻由该代际release事件at_utc核查；当前行updated_at是当前代际状态时间，不冒充跨代际释放时间。

真实WS验证后修正通知delivery_id：与source_envelope_id分开，取Hub生成的真实dw-dlv并核session_delivery来源/目标，否则unknown，不宣称CLI打开/开工。关闭态既有7入口同夹具/同命令r48对照原JSON相等；不带R10标签的原信封在新守卫最前返回。PG回调SQLite仍不是分布式事务，失败/不确定不伪成功，另有真实PG回调失败审计格。手工rollback.sql只在隔离副本实测，不代表授权现网执行。

旧70个迁移SQL和已有账、PG schema、VERSION、helper/View及R9冻结设计均未改。helper offer CAS属于批准的r48基线缺修复，r50随r49整合；终端等待类保留负载敏感FINDING，空闲未复现不等于证明无缺陷。实际全部门的rc/已批准例外和SKIP逐条留存，不以“全部通过”隐藏既有失败。
