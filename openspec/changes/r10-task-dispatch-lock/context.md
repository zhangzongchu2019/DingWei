# R10 技术前置事实（基线 r48）

核对对象固定为 `1e17255f28ced042784337903914ca8c126ef4cf`。下列文件:行均指该基线，不是当前文档分支。结论区分可复用的既有能力与R10尚未接线的部分；不据工蜂交件或本机访问能力推断产品适配，也未探测真实 GitHub/飞书。

| 编号 | 判定 | 基线事实与证据（文件:行） |
|---|---|---|
| A01 R5提供方 | 部分具备 | R5默认关闭，必须 `DW_OPENSPEC_READ_ENABLED=1`，启动时还要求有效服务端凭据文件与允许的GitHub repo ID；构造的Provider map虽列出 `github:github.com` 和 `gongfeng:git.code.tencent.com`，但后者 `Repository/Authorize/Resolve/Tree/Blob` 一律返回 `ErrUnavailable`。`internal/m8/console_openspec_read.go:17-40`；`internal/specread/service.go:181-194`。GitHub provider用绝对路径credential file，从受限权限文件每次读取Bearer token；服务端不把token交给调用方。它通过GitHub API读取repo、collaborator permission、ref解析、tree/blob；权限值read/pull及更高接受，单次服务读取最长15秒。`internal/specread/github.go:24-85,127-145,207-268`；`internal/specread/service.go:92-115`。所以GitHub只读能力存在，工蜂产品读取不具备；R5不是Git写能力。 |
| A02 受派Agent身份/read ACL | 部分具备 | 稳定Agent主键是 `np_agent.id`；Agent记录含 `OwnerLC`、`GroupID`、state及凭据hash。`internal/identity/model.go:135-155`；PG schema以 `np_agent.id` 为主键。`internal/identity/pgschema.sql:45-65`。现有Agent任务接口从Bearer凭据认证当前active Agent，使用认证返回的Agent ID作主体；只列出/允许当前分派给该Agent且经当前owner/group授权的本地Console task。`internal/m8/console_task_flow.go:44-59,114-181`；`internal/identity/task_authorization.go:17-69`。但R5 `Service.Read` 的actor是Console principal，并按该principal读取自己的绑定/提供方ACL，没有Agent身份到R5 reader的映射或 delegated-task R5 read路径。`internal/specread/permission.go:8-36`；Console principal取自登录session用户。`internal/m8/newplat_view.go:223-244`。因此稳定ID及本地任务ACL已具备，Agent能否以受派身份通过R5读仓库任务未具备。 |
| A03 权威锁事务与时钟 | 部分具备 | 任务流当前由SQLite实现：DB启用WAL、busy_timeout且连接池最大连接数为1（单写者）；task flow赋值/更新用 `BeginTx(ctx,nil)`，未显式指定SQL隔离级别。`internal/store/sqlite.go:57-66`；`internal/store/console_task_flow.go:98-114,250-261`。并发写有串行基础，但现有任务表不是R10跨版本锁，未见R10的唯一互斥键、租约、代际或续租模型；不能据单连接推称“50并发恰一授权”已成立。时间由应用调用方传入 `at`，Hub的 `h.now()` 默认 `time.Now()`，仅测试时可注入 `NowFunc`，不是数据库权威时钟。`internal/m8/console_auth.go:53-58`；`internal/m8/console_task_flow.go:208-212`。PG事务设施存在于repo pool/identity PG store，`InTx`接收`sql.TxOptions`但默认可为nil；身份PG store用于身份域，不是Console task flow/锁的PG实现。`internal/repo/pool.go:117-140`；`internal/identity/pgstore.go:17-25`。 |
| A04 MR/ref监测 | 部分具备 | GitHub provider可把给定branch/ref经 `/repos/{name}/commits/{ref}` 解析成40位SHA，并读取该固定commit；R5 Service不缓存授权。`internal/specread/github.go:258-269`；`internal/specread/service.go:92-130`。Provider接口只暴露Repository/Authorize/Resolve/Tree/Blob/Source，没有MR/PR状态或变化订阅方法。`internal/specread/service.go:72-79`。另有 changeflow 的 Snapshot/Reader 抽象字段可承载 `CurrentHead`、`MergedAt` 等事实，但 GitHub 与工蜂 Provider 的 `WithAccess` 当前都直接返回 `ErrUnavailable`，不是可用的产品适配或轮询/回调。`internal/changeflow/types.go:85-116,126-145`。在该基线产品里没有R10 ref轮询器或Git托管回调接入；工蜂Provider本身不可用（A01）。因此手动读取ref tip可复用，持续MR状态/ref漂移监测不具备；未据仓库工蜂交件能力推断产品工蜂适配。 |

## 结论与边界

A01～A04的源码事实盘点完成，状态均为「部分具备」，T-03.2核实工作可关闭；R10执行前还需补上工蜂provider、Agent到R5授权主体的可信映射、可证明的锁表唯一性/租约事务/权威时间方案，以及明确选择轮询或回调并实现监测。本文不表示R10锁已实现，不证明50并发性质，不批准任何产品或真实托管操作。

## S-R49 实施设计交接（仍未实现）

设计以SQLite新表 `r10_task_lock` 的K联合主键＋generation/token CAS为单任务唯一权威；建议迁移0073只建表/索引，新增审计、operation与通知outbox同事务提交。60分钟租期、每15分钟续租、到期只回收、每60秒监测、变化后5秒提醒、逐仓隔离沿用㉒ PM值。SQLite单连接池只说明当前Hub内单写者，不证明多进程/跨主机互斥。基线证据仍为 `internal/store/sqlite.go:57-66`、`internal/store/console_task_flow.go:98-114,250-261`。

首期设计只启用已具备只读接口的GitHub provider；工蜂provider明确列后续。Agent映射按认证Bearer的 `np_agent.id` → 当前Identity PG `np_agent.owner_lc` → 同值 `np_user.user_id_lc` → R5授权检查设计；该路径是Agent所属用户授权委托，不是Agent独立GitHub身份，需组长确认是否满足“受派者独立read”。provider每60秒轮询ref和新增只读PR元数据；异常记录comparison_unknown并关闭继续/续租的成功证明，不作无变化结论。基线能力锚点：`internal/identity/model.go:135-155`、`internal/identity/pgschema.sql:45-65`、`internal/specread/service.go:72-79,181-194`、`internal/specread/github.go:258-269`。

待组长定：Agent使用owner R5权限是否满足受派者read语义；部署是否保证单Hub/单SQLite锁权威，若不是则另定共享锁协调。此交接是设计，不表示新增表、provider方法或轮询器已实现。
