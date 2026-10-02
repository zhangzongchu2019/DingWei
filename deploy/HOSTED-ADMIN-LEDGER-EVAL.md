# 评估：hosted g4_ 表不进迁移账本的收口改法（只评估，不改代码）

派工 R-V2-DEPLOY-ARTIFACTS-20261002161028 ④。基 newplat/main c0a0720b。

## 现状（核到代码）

- g4_ 十表由 `internal/hosted/schema.go` 的 `SchemaDDL` 幂等 apply（CREATE IF NOT EXISTS），**不走**迁移账本（`internal/store/migrate`）。
- 触发点两处，都会对**目标库**（含生产）实际建表：
  - Hub 装配 `wireHosted`：`cmd/workpulse/newplat_hosted.go:38`（设了 `DW_G4_MASTER_KEY_FILE` 才走到）；
  - 运维工具 `hosted-admin`：`cmd/workpulse/hosted_admin.go:43-45`（每次运行都先跑账本 `Migrate` 再 `ApplySchema`，`:15-16` 注释自承「对生产库同样会执行」）。
- 账本侧已给 hosted 留位：号段 **0200-0299＝hosted 域**（`internal/store/migrate/domains.go:28`），`:65` 明写「hosted 与 view 现在还没有条目，列在这里是前置，G4/G5 冻结基线进账本那天用」⇒ 收口是**既定方向**，本评估只是给改法。
- 风险形态（草案 §6.3 第 4 条）：窗口外随手跑一次 `hosted-admin register-node`，就会在生产库建 g4_ 十表——**无账可查**（账本里没有 hosted 条目，建表动作不留痕）。
- 附带已知小缺陷：`DropDDL` 恰漏 `g4_backup`（schema.go 两段 DDL 表名集合差集实核）；DropDDL 仅测试用（`:129`），不挡部署，但补格时应一并修。

## 甲案：纳入迁移账本（0200 段冻结基线）

**改法（最小形）**：
1. 新增 `internal/store/migrate/migrations/0200_hosted_baseline.sql`＝十表建表正文（从 `SchemaDDL` 冻结拷贝，头注写明「冻结副本，不可改；增量走 0201+」——同 0400 的先例与措辞，附源文件 sha256）。
2. **🔴 不动装配代码**：`HubDomains()` 已含 hosted 域（`internal/store/migrate/domains.go:67-69`，注释 `:65-66` 明写这是「前置」——条目进来那天 Hub 侧不必改装配）⇒ `openNewPlatformStore` 的 `Migrate` 会自动施加 0200，Hub 启动与 hosted-admin 两个入口同时收口。
3. 两处 `ApplySchema`（`newplat_hosted.go:38`、`hosted_admin.go:44`）降为幂等核对或删除——双轨指纹用例继续守「SchemaDDL 建形 ≡ 账本建形」。
4. 补一格：DropDDL 与 SchemaDDL 表名集合相等（顺带修 `g4_backup` 漏拆）。

**所得**：建表动作进账（何时、何条目、digest 一致核对）；`hosted-admin` 跑一次也留账；与 G2 裁定「建表归账本」一致（`newplat.go` openNewPlatformStore 注释引 ruling-2026-10-01T145840Z）。
**代价**：中小。装配不用动；主要工作量是「已用 ApplySchema 建过表的存量库（B12 测试库）如何补记账」——按 0400 的既有先例（指纹核对、不重复施加）处理。
**🔴 时机约束（复验方指出的代价，照录）**：0200 一旦记账，`SchemaDDL` 正文即**不可再改**（digest 不可变即拒启，`migrate.go:119-124` tamperErr；0400 头注 `:9-12` 同旨）⇒ 以后改表只能新增 0201+ 增量。进账本前必须确认十表形状已定稿。

**DropDDL 漏 `g4_backup` 与双轨指纹格的关系（复验方问）**：指纹格比的是「空库经 SchemaDDL 建出的形状 ≡ 空库经账本建出的形状」，**不含 DropDDL** ⇒ 漏拆不会被指纹格漏检所掩盖（它根本不在比对面内），也不会因走甲案而自愈——必须另补「DropDDL 表名集合 ≡ SchemaDDL 集合」那一格（本评估第 4 条）。

## 乙案：显式子命令（schema 施加收敛为一个动作）

**改法（最小形）**：
1. `hosted-admin` 新增子命令 `init-schema`（唯一会跑 `ApplySchema` 的路径）；
2. `register-node`／`seed-account` 改为**先核后做**：`CheckSchema` 类探测（g4_node 存在性）不过即拒，报「先跑 init-schema」；
3. Hub 侧 `wireHosted` 同样改「核而不建」（缺表即拒启报缺哪张）——🔴 只做 hosted-admin 侧**不够**：Hub 启动路径 `newplat_hosted.go:38` 也调 `ApplySchema`（复验方正指此点）。托管面开关本来就要求运维显式设 `DW_G4_MASTER_KEY_FILE`，建表也显式化，语义一致。

**所得**：「随手 register-node 就在生产建表」消灭；改法小（一个子命令＋两处探测），不动账本格式、不动存量库。
**代价**：小。但**双源仍在**——g4_ 表形仍只有 `SchemaDDL` 一处定义、账本仍无 hosted 条目；建表动作依旧无账可查（只能查 Hub 日志）。

## 建议

**先乙后甲**：版本 2 窗口前取乙（小、可复验、直接消掉草案 §6.3-4 的风险形态）；窗口后、十表形状经生产观察定稿再取甲收口进账本（0200 前置位本就为此留）。两案不冲突，乙的 `init-schema` 到甲案里自然变成「跑账本」的显式触发点。
若组长认为版本 2 就该一次到位，直接取甲也可——那时 B12 测试库的补记账口径是主要工作量，须先在测试库上演一遍。
