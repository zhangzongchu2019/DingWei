# R31 R1–R5 作者候选证据

范围：`review/coder/r31-hosted`，基线 `3d99db2bf2dcff81f8060bf427f48a0790f0f775`。设计锚 `80ecff8a93cedecba3a459fcca2836eebd46f846`，审查席报告 `cd2d79ff` 为设计 PASS。当前生产源码冻结在 `d24e4e6fb1f2e103ca5fd784d38817bd601b12b3`／tree `9f9f459a7a51687848c6dfae007e2aeef8b51d02`；其后只补证据。初轮候选1b7c5d76已撤锚：旧会话表曾在空IP更新时保留历史IP，d24e4e6f显式清除并遮盖历史目录读数。最终受审commit/tree以替代交件信为准。

这是作者本地验证，不是独立审查 PASS，也不证明 43、dw123 或真实托管席位已验收。未操作宿主、未部署、未登记生产节点。R6／R7 不在本分支。

环境：VM-4-12-ubuntu，uid 1001，16 核，Go 1.22.2 linux/amd64，Python 3.12.3。PG 用 pgtest 自建临时实例，严格失败策略；规范跨运行时门报告 PostgreSQL 180003。浏览器用 Playwright／Chrome、本机 HTTP fixture。命令与实际结果见 `CHECKS.json`，源码摘要见 `SOURCE-SHA256.json`，日志与撤修回执按相对路径存放。

## 验收映射

| 验收 | 本地证据／复跑入口 |
|---|---|
| A1 | `cmd/workpulse/TestR31AllActiveHTTPReachesQuotaAndDisableIsLive`：真实 N3 cookie，非名单 member options 200，创建实际进入 quota_exceeded；停用后的原 cookie 403。 |
| A2 | `TestR31HostedAccessModes` 与既有 `TestHostedAccess*`：缺省／allowlist、名单隔离、mixed bearer、过期／改密／注销／角色变化及注册不自动获权。 |
| A3 | `parseHostedAccessMode` 启动装配，无代码切换；未知值拒绝。`OPERATIONS.md` 记录配置与重启生效边界。 |
| A4 | `TestR31AdminNodeAPIAndOneTimeCredential`：真实 N3 会话，member／manager／无 cookie 均 403，管理员即使不在 allowlist 也可管理；拒绝异源和 cookie＋bearer。节点管理浏览器验证 manager 无入口。 |
| A5 | `TestR31NodeRegistrationCredentialAndAtomicAudit`、上述 API 格与浏览器：数据库只存凭据哈希、随机明文仅创建响应、no-store、列表不回密钥或哈希、重复登记 409 且不轮换；完成／离开页清空明文，不进浏览器存储。 |
| A6 | `TestR31NodeDisableSurvivesHeartbeatAndBlocksFinalAdmission`：心跳不撤停用，新落座拒绝；既有席不变、已入队工作可继续。浏览器确认 disabled／draining 独立调度动作。 |
| A7 | 上述 API 格检查 node.register／node.update 各一条审计。注册格注入审计失败，登记与节点编辑都回滚。 |
| A8 | `TestR31NodeGroupAccessAndRevocation`、`TestR31NodeOptionsHideModelsAndQueuedRevocation`：创建者／active 成员 Agent 属主／指定用户与编组并集；关闭添加不撤权，删组／失效引用／停用用户拒绝。隐藏模型不泄露；最终队列发起复核属主。并发格 `TestR31AdmissionAndNodeRevocationCommitOrder` 与 `TestR31StatePatchCannotRestoreConcurrentRevocation` 核两种提交顺序与部分编辑不恢复已撤权限。 |
| A9 | `TestR31HardConcurrentReservationsDoNotOversell`：10 次并发、2 个可用槽，只准 2 席；options 与最终落座共用容量计算，扣除未上报 pending。 |
| A10 | `TestR31HardCapacityAllDimensionsAndLegacyUnknown`：16C／30GiB／560GiB，扣 2C／3GiB／80GiB，资源上限 12 席；另受节点 MAX_SEATS 限制。 |
| A11 | 同格分别令 CPU／内存／磁盘不足和测量未知，自动选点跳过，显式节点拒绝；`TestCapacity*` 验固定原因白名单、模型优先级、不泄露内部错误。 |
| A12 | Hub hard／soft 共用能力检查；旧 hard 缺快照拒新增。`TestR31HardWiringUnknownCapacityAndNoSoftEnforcer` 核未知拒绝、4 席上限、既有席启动与 soft enforcer 不启用；`TestR31HardCapacitySamplerForeignLoadOwnershipAndUnknown` 用真实共用采样器＋假 Engine 核占用、错误与恢复；`TestR31HardInventoryTracksLoginWriters` 核登录 writer 身份。完整 nodeagent 回归同时覆盖 soft 模式。 |
| A13 | `internal/identity/httpapi/TestR31AgentListHostTypeAndBearerPrivacy` 核自有 IP／类型；既有 hosted overlay API 与托管浏览器行断言核类型／node-a。`TestR31SourceRecordedOnlyAfterAdmissionAndRetained` 走真实本机 WS 核连接成功写入、失败不改、断线保留与重连刷新。 |
| A14 | `internal/identity/TestR31ConnectionPrivacyPGAndMemory` 与上述 API 格：本人／active 管理员读取，其他用户／失效身份／Agent bearer 不得见；IP 不进通用 Agent 对象或会话目录；旧u_凭据端点即使尚未重连也在读取时遮盖历史值，重连带服务端私密来源标志清除原始旧值。传统非凭据端点的断线／空更新保留语义由存储回归固定。 |
| A15 | `internal/m8/TestR31AgentSourceIPTrustBoundary`：显式开关、回环 TCP 对端、IPv4／IPv6、非法／重复／列表头、X-Forwarded-For 伪造。生产 nginx 只采用 prodops 02:02Z 的只读报告，P4 前仍须复核，不能拿本地测试替代现场证据。 |

## 迁移与兼容

统一 `schema_migrations` 新增 `0103_agent_last_source_r31.sql`、`0200_hosted_r30_baseline.sql`、`0201_hosted_node_access_r31.sql`；Hub 和 hosted-admin 两处原 ApplySchema 都进入统一 runner。0200 对完整 R30 物理形状做只读校验并入账，已有库不重放 R30 历史回填或重建索引；空库执行冻结 DDL。0201 将存量节点回填 admin_only。

`internal/store/migrate/TestR31HostedEmptyAndR30Adoption` 保留表 OID、既有节点／席位／账号／凭据哈希／加密数据；覆盖一个仍无主的历史账号，证明不重放旧 owner 回填。部分形状拒绝且无成功账。目录形状 fixture、源码常量与迁移双轨比较、不可变旧账闸同时保留。

独立从干净 R30 基线构建旧 workpulse，再对自建 R31 临时库执行 `TestR31OldBinarySchemaReadCompatibility`：旧 identity 启动到达“已有管理用户”拒绝，旧 hosted 启动并读取账号、完成隔离库 account-disable，R31 增量字段与存量数据保留，回到 R31 再启动账本不重放。旧二进制 SHA256 记录在 CHECKS；二进制本身未上传。此格只证启动／读取形状兼容，不证明可在开放托管时直接回退。

## 撤修与复跑

`tools/r31-hosted-checks/mutations.py` 使用 Go overlay，不改工作树，删除外部测试 PG DSN，强制自建临时 PG／严格失败；每格同时要求 rc=1、指定测试失败、指定业务断言见证。编译错误、panic、超时、跳过或无关断言都不算过。18 格包括 active、管理员角色、节点模型过滤、hard 容量、pending、最终准入、哈希、注册审计、active 编组成员、IP 权限、可信对端、统一账本、登录 writer、PATCH 撤权锁、浏览器凭据清除，以及历史IP读遮盖、写清除和真实凭据登记标志。格子明细与源码摘要在 mutations/mutations.json；恢复后的定向正对照另有日志。

```sh
TZ=UTC HUB_TEST_PG_STRICT=1 HUB_TEST_PG_EMBEDDED=1 HUB_TEST_PG_SKIP_OK=0 \
  go test ./internal/hosted ./internal/identity ./internal/identity/httpapi \
  ./internal/nodeagent ./internal/m8 ./internal/store/migrate ./cmd/workpulse -count=1 -timeout=30m
NODE_PATH=/path/to/playwright/node_modules PW_CHANNEL=chrome \
  python3 tools/r31-hosted-checks/mutations.py --out /tmp/r31-review-mutations --jobs 2 --browser
NODE_PATH=/path/to/playwright/node_modules PW_CHANNEL=chrome \
  node view-service/r31-hosted-nodes-browser-regression.js
```

旧二进制兼容格须先独立从精确 R30 基线构建，再显式指定 `R31_R30_BINARY`；缺少时该单格 SKIP，不能算兼容通过。正常 m8 全包有历史环境开关跳过的外部环境格；规范跨运行时 shell 门另外启用真实本机 Hub／PG 场景。

## 隐私后继与验证归属

d24e4e6f只改变内部SessionEndpoint写入意图与SQLite通用会话IP处理，不新增数据库列或迁移、不改变HTTP接口。`PrivateClientIP`是服务端传递且不序列化的标志。读取时识别u_这个由AgentKeyID生成的凭据所有者命名空间仅用于遮盖，绝不据此授予权限。旧原始IP不搬入新的私密IP列；新的可信来源仍须实际认证登记。

`TestR31PrivateSessionSourceClearsHistoricalIPAndPreservesLegacy`直接核历史原始值清除和传统端点保留；真实WS格先放入旧离线端点，再验证目录遮盖及登记意图。修复后重跑store／m8完整包、cmd完整包、全仓构建、规范构建门、18格撤修及八包R31正对照。hosted／identity／nodeagent／迁移生产代码与初轮71d14a60逐字相同，其完整包结果继续归属于那些未变的代码；新源码摘要和精确执行归属见CHECKS。

## 既有基线与测试变更限域

托管浏览器原套件在当前与干净 R30 基线都为 30 过／2 败，失败标题：`left list uses full_name: button shows the short name, title/tab/iframe use full_name`；`a create with a new group shows that group in the group list once online, led by the new agent`。新增类型／主机与节点管理断言均通过；另跑 Console 20/0、入网20/0、编组12/0、生命周期15/0。生命周期状态列定位随五列布局更新，独立r30同套为15/0。两项既有失败不算 R31 新增功能通过，也不在本件扩修。

`go vet` 在 m8 既有 `agent_disk_metrics_test.go:77` 报复制 sessionClient 锁；同命令在干净 R30 独立同红，该文件没有本分支变更。其余改动包 vet 另核。

旧测试的 hard fixture 现在显式给 public 授权、批准的策略 revision 与完整测量；R31 负格直接调用生产入口，未经自动升级 fixture。历史“早期 DDL 自动补齐”用例明确限定冻结 R30 DDL；当前统一账本不自动修补 partial/drift 旧基线。现有 CreateSeat 保留可信存储／导入用途；唯一实际编排创建入口调用 CreateAdmittedSeat，执行通用事务准入。节点管理新事务使用显式平台／操作者上下文，仓储边界棘轮仅随实际还债下降收紧，不增加允许项。
