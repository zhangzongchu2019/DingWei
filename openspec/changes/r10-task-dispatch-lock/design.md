# Design

## Context

主规格/context记录r46旧任务流与R5只读现状。旧AssignConsoleTaskFlow存在UUID分配及outbox，但不是仓库T编号互斥租约；不得仅换字段名当R10已实现。

主规格/context保留r46最小源码基线，本轮按范围不改主目录；其中当时PM待定/完整MR权限前置的旧描述，以本变更㉒已定表及首期后置边界为准，不把源码基线当产品已实施。

## Goals / Non-Goals

- 目标：满足R10-001～009，在任务编号和稳定仓库身份上保持单一有效持有者，保证固定版本、逐主体核权、到期/释放/交接和漂移审计。
- 本单不实现产品、Git写入、双向tasks同步、跨仓分布式事务或后续长周期检查点；不将平台锁当Git托管平台写权限锁。

## Decisions

[业主裁定rulings第㉒条](https://git.code.tencent.com/wego-ai/dingwei/blob/0659d208ce731b73206b06d587c3814649ae11d2/docs/rulings/S-TEAM-MIGRATE-HOSTED-20261017.md)：业主workpulse对话，Hub收信2026-11-01T23:07:47Z，delivery dw-dlv_f5170d3c5deb6106833dc76e7c36db1e；对“r48那11项是否全部按建议定”答原话“ok”。本单S-R48-PM-WRITEBACK只回写已定值；不是实现、apply/archive、迁移、预发或发布授权。 R10-PM01～04逐项映射见proposal；首期逐仓独立，不实施跨仓一次性锁定。

### 1. 坐标、任务解析与读取（R10-001、002）

逻辑输入为提供方/实例、稳定仓库ID、规范openspec/changes/<change>目录、完整40位commit、T-xx和可信受派主体；可变ref仅用于监测目标，不代替派单commit。任务实际路径固定为该目录/tasks.md，用R5现有只读Service.Read和同一提供方接口逐主体读。不得另造raw URL、SSH或Agent代抓服务；不得因为Hub部署在工蜂源码上就假装工蜂读取适配已启用。

提取任务采用受限Markdown子集：只接受代码块/缩进代码/HTML外的复选列表任务，开头有唯一T-xx（至少两位数字）、后有非空任务描述；保留原checkbox作为读取事实。重复编号、歧义HTML/未闭合代码块、截断、非法编码或超限必须拒绝整个派单，不能挑第一条。已完成任务默认不新派，重新打开需先改仓库规格，不由平台把勾取消。正文留仓库/既有R5呈现，不复制到锁或通知；平台保留tasks blob/hash、任务位置和适用版本等元数据。

派单人需平台可信派单资格及当前仓库read，受派者需有认证Agent主体和当前仓库read；锁变更、历史读取、通知交付及提交边界重新核当前权限。Agent身份只能由现有Bearer认证得到稳定 `np_agent.id`，再从Identity PG当前active Agent行取 `owner_lc`，映射到同值的 `np_user.user_id_lc` 作为R5 Service.Read的授权principal；不得使用浏览器传入的owner/actor，也不得用历史 `console_principal.id`。每次调用都重读active状态与属主并通过R5现有GitHub绑定/ACL重核，失败关闭。这个映射实际复用Agent所属用户的R5仓库授权，不是独立Agent GitHub身份或独立collaborator权限。是否满足“受派者独立read”而允许所有者授权委托给其Agent，须组长确认；确认前不得实现越权回退。证据：`internal/identity/model.go:135-155`；`internal/identity/pgschema.sql:45-65`；Agent Bearer主体：`internal/m8/console_task_flow.go:44-59,114-181`；R5按principal授权：`internal/specread/permission.go:8-36`；Console principal来源：`internal/m8/newplat_view.go:223-244`。

### 2. 互斥键、事务与代际（R10-003、009）

锁键K=(provider,instance,stable_repository_id,change_directory,task_id)，**不含commit或分支**。因此同任务C与C2不能并持；跨仓同名T编号彼此独立。更名后的仓库稳定ID不变则锁不换；新建同名仓库不同ID绝不能继承。内容不变archive移动按R7比较关系处理成历史，不新派一把锁绕过原任务；目录/编号迁移身份无法证明时阻止自动转移，先显式释放/交接。

授予、续租、过期回收、显式释放和显式交接均以单个SQLite事务完成；锁行状态变更、代际/token CAS、审计事件及对应outbox一并提交或一并回滚。使用唯一operation_id返回既有结果，不能以网络回执未知为由换ID重做。授予事务先查当前权限和固定任务内容，再对K做条件插入/更新；竞争只能一方提交。过期回收只标记旧代际expired并递增generation，不选择新holder。显式交接在同一事务中结束旧token并签发新代际。事务失败、权限未知或回执未知都不回报成功、不自动再抢。SQLite连接池虽限制单写者，仍须以schema唯一约束及CAS作为正确性条件，不能把当前SQLite配置本身当成锁实现。证据：`internal/store/sqlite.go:57-66`；旧任务流使用 `BeginTx(ctx,nil)`，没有R10租约/代际语义：`internal/store/console_task_flow.go:98-114,250-261`。

### 2a. 锁记录、迁移及CAS事务（T-03缺口落地）

在现有Hub SQLite主库新增 `internal/store/migrations/0073_task_dispatch_lock.sql`；0072已由R14预留，集成时由迁移维护者统一检查编号。只 `CREATE TABLE` / `CREATE INDEX`，不改旧任务、Agent、outbox或消息表。建议结构：

| 表 | 字段与约束草案 |
|---|---|
| `r10_task_lock` | K字段 `provider, instance, repository_id, change_directory, task_id` 联合主键（不含ref/branch/commit）；`state` (`active/released/expired/handed_off`)、`generation INTEGER`、随机不可预测 `lease_token`、`holder_agent_id`、`holder_user_id_lc`、`assigned_by_user_id_lc`、固定 `commit_sha`、`tasks_blob_sha`、task定位、`granted_at_utc`、`expires_at_utc`、`last_renewed_at_utc`、`updated_at_utc`。K主键保证同仓同变更同任务只有一条当前锁行；只要条件写操作正确，任一时刻最多一把有效锁。`lease_token` UNIQUE，不能只靠状态扫描后的普通INSERT。 |
| `r10_task_lock_operation` | `operation_id TEXT PRIMARY KEY`、K、操作种类、期望/结果代际、请求主体、结果状态、创建/完成时刻和结果摘要；相同重试可回原结果，operation ID不可复用不同请求。 |
| `r10_task_lock_event` | append-only事件ID、K、generation、事件类型、操作者/holder稳定ID、服务端UTC、必要坐标/引用；不得记录任务正文、凭据或访问令牌。用于审计和争用诊断。 |
| `r10_task_lock_outbox` | `id TEXT PRIMARY KEY`、operation_id、K、generation、recipient Agent ID、事件类型、投递状态/回执ID、创建/结束时刻；UNIQUE `(K,generation,event_type,recipient)` 防重复授予通知。与锁授予同事务创建。 |

授予、续租、过期回收和显式交接都在一个SQLite事务中完成。授予对不存在K用INSERT并由主键仲裁；已有行只允许在状态非active或 `expires_at_utc <= now_utc` 时以K+期望代际+到期条件更新，generation加一并换token。续租需同时匹配K、active状态、holder、generation、旧token及 `expires_at_utc>now`，到期时间设为 `now+60m`；其余行数不是1就冲突。回收用K+代际/token+到期条件把旧行改expired，不授给任何人。显式交接验证旧holder或明确获权负责人、双方主体及固定交接记录后，在同一事务CAS旧行并签发新代际、新token、新holder。所有事务同时写operation/event/outbox；任一步失败整体回滚。50并发正确性不得依赖ticker，唯一主键/CAS为最终仲裁。

权威时刻使用Hub的 `h.now().UTC()`；这是当前进程墙钟和测试注入点，不是DB时钟。事务中一次取样；UTC早于已记录授予/续租时刻视为时钟回拨并拒绝授予/续租/回收成功，记录 `clock_invalid` 供人工处理，不使用容差。重启后按绝对 `expires_at_utc` 判过期，不续期；不把ticker视为权威状态。单实例文件/跨进程协调不是A03已证事实，若生产允许多个Hub写同一锁库，须另定部署与SQLite共享文件可用性；不得据单连接池推出跨主机互斥。

证据：SQLite `WAL`、`busy_timeout`、单连接池为 `internal/store/sqlite.go:57-66`；旧任务flow `BeginTx(ctx,nil)` 为 `internal/store/console_task_flow.go:98-114,250-261`；现有Hub UTC时钟注入为 `internal/m8/console_auth.go:53-58` 与调用方传 `at` 为 `internal/m8/console_task_flow.go:208-212`。

### 3. 租约与主动释放（R10-004、005）

服务端权威时间定义start≤now<expires为有效，now=expires即失效，不依赖后台扫描是否执行；客户端时钟不能延长。R10-PM01已按rulings㉒定租期60分钟，持有者每15分钟显式续租。续租必须原holder＋当前generation、未到期、当前read/有效持有者资格成立且无待同步漂移；到期后续租不能复活旧锁。有效续租记录起因及新到期时刻，generation不倒退。R10-PM02到期只回收旧锁、不自动改派；PM03每60秒检查、检测到变化后5秒内提醒；PM04按稳定仓库ID逐仓独立，任一仓的失败不与其它仓合并成全局锁事务。所有时刻用Hub进程UTC，锁操作事务内取样；定时器只唤醒扫描，不改到期值。证据：`internal/m8/console_auth.go:53-58`，PM/逐仓结论见 `context.md:A03/A04`。

每次受保护平台操作核K/holder/generation/截止；到期或交接后的迟到释放/进度关联不能污染新持有者。主动释放还需可核且与K/执行版本关联的固定Git版本交接记录定位，原持有者能核当前锁，原子记录released_at/原因/交接指针后撤权；不能靠浏览器退出或helper离线直接当释放。释放不是完成任务；任务进度仍只从仓库tasks读，历史追加保留。

### 4. 交接和到期恢复（R10-006）

主动交接由当前持有者或明确获权负责人发起，指定原代际、接手人、受权可读且与K/执行版本关联的固定commit/tasks交接记录定位。两主体核权及交接可读校验完，在同一事务结束旧租约、生成新代际给接手人，保证无双持有窗口；任何失败整次不转移，不能先通知新持有者成功。平台只存指针/hash、交出/接入主体/时刻和原因，不保存上下文正文。

到期只使原代际失效，不自动向另一个Agent开工；显式重派必须引用恢复/交接位置并记录旧执行状态已确认停止或未知。未知不能冒称完成“安全交接”；按㉒已定PM02由负责人核现场并写记录后显式重派。平台不能保证离线旧Agent实际停工，托管Git合并与执行纪律是另一个边界。

### 5. 派单后的规格漂移（R10-007）

为每个授予钉住C与监测ref/MR标识；发现C2时通过R5完整读取、沿用R7已批A-conservative-exemptions-v2策略（tasks勾选/交接节、纯内容不变archive移动豁免；其它文件变化保守判实质）。实质修改标needs_sync并提醒持有者/负责人，指出两端SHA与策略依据，旧C不改绑C2；读取/比较失败标待核并禁用继续/续租的成功证明。无关commit和纯勾选/交接不误报任务定义变动，也不把原授予SHA改成新SHA。

R10-PM03已按㉒定每60秒检查、发现后5秒内提醒；保留既有R5一次读取≤15秒技术界限，健康依赖下按60+15+5推导总≤80秒作为原测试判据，非另增PM项，不承诺提供方故障时有界送达；故障标监测滞后。提交前执行席必须重读目标ref并比较，平台关联提交/交接前也重核，无法核则停。同步后由原有效持有者明确确认新C2和范围并重新核任务/权限，追加适用版本记录；互斥K不变。失效锁不能借同步夺回新代际；不能自动撤别人锁、改tasks或替用户确认新规格。

监测只使用只读provider API，不执行ref写入或MR写操作。首期仅GitHub provider：每个不同 `(provider,instance,repository_id,ref,MR_ID)` 每60秒至多一次轮询，合并仓库范围ACL核验与ref SHA解析；同仓多个锁共享同一次ref/MR读取，再逐锁比较固定C与已批准R7规则。需要补充GitHub只读PR元数据读取（head SHA、state、base/head refs、updated_at）；PR标识或该只读能力缺失时该维度记 `comparison_unknown`，不得当作无变化。正向检测后5秒内持久化needs_sync并写提醒outbox；读取/ACL/限流/超时/格式错误均只写 `comparison_unknown` 和last_attempt/error class，禁用续租/继续/提交成功证明，不发“规格已变”提醒，不重设已锁commit。后续健康轮询恢复后才更新比较结果；监测滞后须显示。可以沿用现有 `changeflow.Snapshot` 作内部读数容器，但该抽象有字段不表示R10已具provider适配。

证据：现有R5 provider只提供Repository/Authorize/Resolve/Tree/Blob/Source、无MR状态接口：`internal/specread/service.go:72-79`；GitHub ref到SHA：`internal/specread/github.go:258-269`；`changeflow.Snapshot`含CurrentHead/MergedAt字段但无provider获取器：`internal/changeflow/types.go:85-145`；工蜂provider方法均返回 `ErrUnavailable`：`internal/specread/service.go:181-194`。首期工蜂列后续，不通过SSH/Git凭据或把工蜂交件能力当产品适配。

### 6. 跨仓与审计（R10-008、009）

跨仓接口提供方所在仓库为权威，消费仓自己的T任务引用提供仓固定commit/tag解析出的完整SHA；每仓读取/派单独立核该主体read。相同change/T编号不全局混锁，也不能用A的read访问B。首期不做多仓原子批派，逐项结果清楚区分成功、冲突、权限失败/未知；局部成功不报整批完成。锁历史、漂移、开始/到期/释放/交接、通知状态均按各仓权限查询；不显示无权仓名/任务摘要。

审计只记录坐标、operation、代际、可信身份、服务端UTC起止/到期/续租/释放、原因与Git交接定位。清理/到期不抹历史。CLI成功/锁释放/tasks勾选都不生成完成、验收、归档或发布事实，R8按独立证据推进。

## 验收矩阵与反向控制

所有格均为未来计划、尚未执行。每行正反两格；变异只在隔离副本撤指定保护，必须红在该行目标断言，恢复后绿。并发/到期边界/重试格随机顺序50轮，使用可控权威时钟和50个并发申请；不写真实Git、飞书或共享数据库。

| 固定场景 | 测试层 | 正向格 | 异常/反向格及目标变异 |
| --- | --- | --- | --- |
| R10-001-S01/S02 固定任务 | parser单元＋R5适配集成 | C下唯一未完成T-01、tasks路径/hash准确 | 重复编号、代码块伪任务、C跨仓/短SHA/截断拒绝；撤唯一编号/完整读取检查必须红 |
| R10-002-S01/S02 逐主体 | Identity/R5集成＋HTTP权限 | 两主体当前read才形成锁且不泄露无权正文 | Agent无映射、撤read、仅服务令牌有权均拒绝；撤Agent主体/owner映射核验红；等待组长确认委托语义 |
| R10-003-S01/S02 重复派单 | SQLite集成＋并发/`-race` | 50并发对同K仅1授予、49冲突；全场景50轮 | C与C2/换operation重复也不能双授予；删除K唯一约束或把commit纳入K的变异必须语义失败 |
| R10-004-S01/S02 锁到期 | Store集成＋fake clock | now=expiry拒旧操作，显式新授予代际递增 | expiry前合法续租；expiry后旧token续租/释放均拒绝；撤截止或代际CAS必须红 |
| R10-005-S01/S02 主动释放 | Hub授权集成＋SQLite事务故障 | holder＋有效代际＋固定交接定位释放，记录时刻 | 非holder/旧代际/无交接不能释放；撤身份或指针校验必须红 |
| R10-006-S01/S02 交接 | Identity/R5集成＋SQLite故障注入 | 当前持有者到有权接手者原子转移，旧代际失效 | 接手者无read或交接不可读保持原锁；半提交故障不能双持有；撤原子事务必须红 |
| R10-007-S01/S02 规格被改 | Fake GitHub provider＋scheduler/outbox集成 | 改任务/需求后≤80秒提醒并needs_sync，提交前再核 | 纯勾选/交接不误报；实质变化或比较未知不能当无变化继续；删除unknown守卫/提醒写入必须红 |
| R10-008-S01/S02 跨仓 | SQLite/权限集成 | A/B同名T各自可持，引用提供仓固定版本 | 只有A权限不能派B，A锁不冒充B锁，部分失败显示清楚；删除repoID维度/逐仓ACL必须红 |
| R10-009-S01/S02 可靠派单 | SQLite重启/outbox集成＋并发race | 同operation重试仅一授予/一outbox，重启后历史可读 | 提交后丢回执不新抢锁、投递失败不报已开工、勾选不报验收；删除幂等/审计持久化必须红 |

## Risks / Trade-offs

- 租约只约束平台，不能杀死离线Agent或撤Git权限；到期重派须对未知旧执行显式处置。
- R5目前无工蜂/Agent完整能力，未核权限不能因关键路径跳过。
- 周期漂移不等于提交原子门；外部Git提交与平台DB非单事务，平台只能拒绝不满足其守卫的关联/推进，不承诺阻止任何外部Git写入。
- 跨仓不做整体事务，减少死锁/授权扩散；业务依赖仍由固定提供方版本声明。

## Migration Plan

先严格校验、冻结本提案并独立审阅；0073只增表和索引，PM01～04已按㉒定值；实现任务另派。实现后逐格证据/目标变异/race与50轮测试并交独立验收；迁移、预发、生产另获GO。本提案不新增真实锁记录、不变更旧任务/派单生产路径。

## Open Questions

见proposal已定表：PM01～04均由㉒批准，无编号不匹配。R10-A01～04已完成基线事实盘点，细节与文件:行见context.md。首期建议仅启用现有GitHub只读provider；工蜂provider后续。待组长定：`np_agent.owner_lc`到R5主体的所有者委托是否满足受派者“独立read”；当前部署是否保证单一Hub/单一SQLite锁权威，或要另行定义跨进程锁服务。GitHub只读PR元数据接口需新增；网络/权限/限流失败只能标comparison_unknown，绝不能当无漂移。

## 基线技术前置核实（r48 `1e17255f28ced042784337903914ca8c126ef4cf`）

事实分类与证据见本变更 `context.md` 的A01～A04矩阵。结论是四项均「部分具备」：GitHub只读适配可用而工蜂适配是不可用占位；Agent稳定ID和本地任务ACL存在但未接入R5仓库读取主体；SQLite可提供单写者事务基础但没有R10租约/代际机制，且应用时钟由调用方传入；GitHub可把ref解析为SHA，但无MR状态或ref漂移监测器。T-03.2因此只关闭“事实核实”子项；实际实现仍需设计并验收缺失的身份桥接、数据库约束/事务、时钟权威和监测调度。
