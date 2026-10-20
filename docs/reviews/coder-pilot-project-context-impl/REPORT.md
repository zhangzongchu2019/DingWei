# S-PILOT-PROJECT-CONTEXT 可审候选

业主第 85 条 `4dbd95ed` 批准实现，设计 `5b95f5fb`。仓库 wego-ai/dingwei，分支 `review/coder/pilot-project-context-impl`，基 r32.1 `36afa6215f00bc39a992441ab0ff8fe81442aa3e`。本件由组长审、prodops 交叉核；不是部署或席位升级授权。

运行源冻结 `989ae8de55e8a234f170d4f7ea59448a58af701e`，tree `328d10512ec79b19bdd8652d9f5bc137948337d1`。最终测试/门源 `ca227049f05b97b3b4170db37a7ac3759b5b7764`；其后只追加本报告、原始日志与摘要。完整候选 commit/tree 在交件信中给出，避免自引用。1752 项非 docs Git 输入见 SOURCE-MANIFEST.json。

## 行为与边界

同名 MCP project_context 的 tasks/sessions/config/all 现在由一次只读 Hub GET 提供，专属 Agent bearer 决定实际范围；不读取旧 datad，不允许调用方指定用户、Agent、组或任意 URL。任务只给本人当前开发/审查指派，名册只给实际同组的 active、可见且在线身份，config 只给白名单规则和本人任务确切修订/验收项。没有 spec 全文、组码、来源 IP、邮箱、凭据或共享 capability。

PG 普通事务按组→Agent→用户持共享锁，SQLite 单个读快照内重核角色、修订及所有私有投影；连接/授权/投影/事务收尾异常都整页拒绝。GET 不写任务、业务审计、通知、收件/已读或 processed。游标 AES-GCM 认证加密，绑定身份、scope、limit、授权摘要及五分钟期限；授权变化 409、跨身份 403、重启后旧游标 400。响应每页至多 100 项、256KiB，仓储候选批次至多 101 项，超出按完整条目缩页，单项无法容纳 413；不得裁剪验收正文后称完整。

helper/Hub 功能版本 0.2.3.7。固定本机 project_context_v1 IPC，HTTP/IPC/shim 截止分别 10/15/20 秒，单次请求、不重定向、不自动重试，旧四个 datad 键仅键名提示。旧 Hub/旧 helper 明确 unsupported/升级提示，受限 IPC 不开放本能力。实际 SessionHelper 构造→真实 IPC→实际 MCP stdio→本地 Go Hub HTTP 的集成测试已接 canonical，绝无 live Hub、AI 或现有席位连接。

无数据库 schema/迁移变更，运行时依赖锁原字节不变；旧二进制可读原库，回滚后新能力消失，不得将旧同名工具当成功恢复。0.2.3.6 oaagent 候选包和本次试点 0.2.3.7 是不同发布对象，本件没有重建/上传/登记旧包，也没有变更正在运行的 helper。

## 设计验收映射

| 设计项 | 真实覆盖与指定撤修 |
| --- | --- |
| C1 身份/凭据 | TestProjectContextAuthenticationAndQueryMatrix；TestProjectContextPGAuthorizationLocksAndCurrentState（真实 PG 锁、吊销、轮换、owner 停用）；duplicate-auth、cookie-auth、credential-recheck、pg-agent-lock |
| C2 编组/名册 | NoDirectoryAndCrossOwnerMembership、MovedAgentEmptyAndNoReconciliationWrites；directory-privacy、online-agent-binding、group-pair-authorization |
| C3 当前角色/并发 | SQLiteCandidateMetadataAndRoleRace、SQLiteReadSnapshotKeepsOneAssignment；PGSQLiteReadThenRevokeAndRevokeThenRead 按 pg_locks 观测读先/吊销先；current-role、current-revision、needs-reassignment |
| C4 私有投影 | ScopesAndPrivateProjection、SQLiteProjectionReadOnlyAndExactRevision；linked-criteria-only |
| C5 失败关闭/无写 | CandidateRaceAndSnapshotFailureClosed、FinalAuthorizationRoundtripFailureClosed、AuthorizationReleaseFailureNoPartialResponse；get-no-business-write、pg-final-recheck、authorization-release-failure |
| C6 分页/完整性 | PaginationBindingAndDrift、CursorExpiry、ResponseBudgetAndOversizedItem、ParserLimitAndIdentityInputs；游标身份/授权/期限、上限、整项大小各指定红 |
| C7 全链/兼容 | AcrossRealHTTPHelperIPCMCP；Python test_project_context 11 格真实本地 HTTP/IPC 和 shim 调用；旧 Hub/helper/datad URL 拒绝、禁止转发身份重定向、两类受限模式、生产接线和 capability 撤修 |
| C8 相邻行为 | 全仓、隔离 HOME helper 全套；旧两条任务列表源码未改，现有 R9 权限/收件确认/自动投递测试同跑；版本 pin 撤修 |

并发证据的精确限域：真实 PG 以 active 状态吊销两种先后顺序控制锁窗口，移组/轮换/owner 停用在后续已提交状态核拒；SQLite 另以真实第二连接覆盖改派读快照。未将这些测试夸大成每一种身份变化都有双库并发屏障。

## 验证读数与复现

ENVIRONMENT.json 记录 VM-4-12-ubuntu、UID1001、16核、Go1.22.2、Python3.12.3，无外部 DSN/显式跳过开关。锁定 venv `/data/ai-dev-data/helper-deps-lock-venv`，正式 helper 全套清空环境并使用临时 HOME，未读取真实在线收件库。最终 canonical/全仓另显式复用本机现成 Go 模块/编译缓存和 embedded PG 二进制缓存，GOPROXY=off、GOSUMDB=off，未联网装包。缓存不是私有状态复制。

最终结果：FULL-GO-DELIVERY.json 默认并行全仓 rc0（2026-10-20T15:31:57Z—15:43:27Z），65包中52测试包全过、13无测试文件；30项条件跳过逐项见 GO-DELIVERY-SUMMARY.json，新 context PG 与实际 MCP 闭环均运行通过。CANONICAL-DELIVERY.json rc0，BUILD_TESTS_OK python=52 shell=2。最终结果以这三份文件为准。先前 FULL-GO.json 的默认并行全仓 rc0：65包、52测试包通过、13无测试文件，35条件跳过逐项在 GO-SUMMARY.json，新增 project-context PG 测试无跳过；不得将有条件的全仓说成 skip=0。最终轮启用锁定 Python 实际执行跨运行时集成门。

HELPER-FINAL.json：756 项全过，rc0，406.040秒；VET.json rc0；RACE.json 新 context 的 m8/identity/store 定向 race rc0。运行时989到最终门源的生产 Go/helper 字节相同见 RUNTIME-INPUT-EQUIVALENCE.json。111项静态 HTML/JS/CSS/CJS 输入与基线 blob 相同，本件没有声称重跑六套浏览器。

29 项撤修由两个完整通过的批次（25＋4）组成，见 WITHDRAWAL-UNION.json；每项都有非零 rc＋指定断言，不计编译失败。两批均有 Go 三包与 Python 11 格的正对照及恢复后正对照。前三次导出/夹具开发中断保留，但不计为通过的撤修。复现：`python3 tests/pilot-project-context/withdrawals.py --commit 989ae8de55e8a234f170d4f7ea59448a58af701e --output /tmp/pc-withdrawals-review`；需要 Go、锁定 helper venv 及既有 embedded PG 工具缓存。

canonical：隔离 HOME 下 `bash scripts/run-build-tests.sh`；全仓：同环境 `go test ./... -json -count=1 -timeout=30m`。DW_DESKTOP_INTEGRATION_PYTHON 指向上述锁定 venv，确保真实跨运行时门不跳过。原始 stdout/stderr 在 raw/，各 JSON 记起止 UTC、命令与 rc。

## 首败保留

- 首轮 helper 756 项 rc1、两个 IPC socket 隔离夹具失败：测试 SimpleNamespace 未补新 handler factory，已只修夹具 lambda；两项定向和最终756全过，不改守卫。
- canonical 首轮系统 Python 缺 websockets 包元数据；锁定 venv 后又发现 canonical 契约仍期望51、已同步新模块为52；再一轮隔离 HOME 的冷 Go 模块缓存使既有 embedded-postgres 工具依赖下载超时。原件均保留，最终显式现成缓存并禁止联网完成。
- 撤修前三轮因导出缺 view-service/pgtest、重复头的撤修造成未使用变量而中断；这些日志明确不算安全回归红。修完 harness 后25＋4均命中指定断言并恢复。
- 原始 Go JSON 有一条 URL 查询凭据拒绝用例的固定字符串 must-not-be-in-url，是仓库合成测试名，不是真实 token。未收集或提交真实凭据、env 内容、会话库或包体。

审查请仅从 Git 独取提交与证据；本件未部署、未连接43/dw123/oaagent，也未发飞书业务消息。

原始撤修 http-schema.log 首行保留 unittest 未换行输出的尾空格，整差集 whitespace 检查如实为 rc2；排除原始 .log 的源码/文档检查 rc0。没有为过格式门修剪原始证据，见 QUALITY.json。
