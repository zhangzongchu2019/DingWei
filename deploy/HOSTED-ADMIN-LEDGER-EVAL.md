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
1. 新增 `internal/store/migrate/migrations/0200_hosted_baseline.sql`＝十表建表正文（从 `SchemaDDL` 冻结拷贝，头注写明「冻结副本，不可改；增量走 0201+」——同 0400 的先例与措辞）。
2. `wireHosted` 与 `hosted-admin` 里的 `st.ApplySchema(ctx)` 改为「账本已含 hosted 域条目 ⇒ 由 `Migrate(HubDomains())` 施加」；`ApplySchema` 保留为**幂等核对**或仅测试用（双轨指纹用例继续守「SchemaDDL 建形 ≡ 账本建形」）。
3. 补一格：DropDDL 与 SchemaDDL 表名集合相等（顺带修 `g4_backup` 漏拆）。

**所得**：建表动作进账（何时、何条目、digest 一致核对）；`hosted-admin` 跑一次也留账；与 G2 裁定「建表归账本」一致（`newplat.go` openNewPlatformStore 注释引 ruling-2026-10-01T145840Z）。
**代价**：中。要动 `wireHosted` 的启动顺序语义（账本先于装配），并处理「已用 ApplySchema 建过表的存量库（B12 测试库）如何记账」——账本对已存在表的补记口径要按 0400 的既有先例（指纹核对、不重复施加）。
**🔴 时机约束**：0200 条目一旦在生产记过账，正文即不可变（冻结副本语义，sqlite.go:186-193 同旨）⇒ 进账本前必须确认十表形状已定稿；版本 2 窗口后若还要加列，只能走 0201+ 增量。

## 乙案：显式子命令（schema 施加收敛为一个动作）

**改法（最小形）**：
1. `hosted-admin` 新增子命令 `init-schema`（唯一会跑 `ApplySchema` 的路径）；
2. `register-node`／`seed-account` 改为**先核后做**：`CheckSchema` 类探测（g4_node 存在性）不过即拒，报「先跑 init-schema」；
3. Hub 侧 `wireHosted` 同样改「核而不建」（缺表即拒启报缺哪张）——托管面开关本来就要求运维显式设 `DW_G4_MASTER_KEY_FILE`，建表也显式化，语义一致。

**所得**：「随手 register-node 就在生产建表」消灭；改法小（一个子命令＋两处探测），不动账本格式、不动存量库。
**代价**：小。但**双源仍在**——g4_ 表形仍只有 `SchemaDDL` 一处定义、账本仍无 hosted 条目；建表动作依旧无账可查（只能查 Hub 日志）。

## 建议

**先乙后甲**：版本 2 窗口前取乙（小、可复验、直接消掉草案 §6.3-4 的风险形态）；窗口后、十表形状经生产观察定稿再取甲收口进账本（0200 前置位本就为此留）。两案不冲突，乙的 `init-schema` 到甲案里自然变成「跑账本」的显式触发点。
若组长认为版本 2 就该一次到位，直接取甲也可——那时 B12 测试库的补记账口径是主要工作量，须先在测试库上演一遍。
