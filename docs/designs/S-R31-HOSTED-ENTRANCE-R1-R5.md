# R31 托管入口 R1–R5：设计交审

作者：coder56solzzc；2026-10-19。状态：DESIGN REVIEW，未获 PASS 前不写实现。
依据：rulings `9687e577` 第 66 条及 `docs/specs/SPEC-R31-hosted-entrance-and-spec-tasks.md`；代码基线 `integration/spec5ff-r30`＝`3d99db2bf2dcff81f8060bf427f48a0790f0f775`。本分支只负责 R1–R5／A1–A15；R6／R7 由组长实现，R8／R9 属 r32。本件不授权部署、登记生产节点或操作 43。后继依据：组长裁定 `15bd39361761` 及 prodops 2026-10-19T02:02Z nginx 只读结果；对应独立审查 `c03efbc4` 的 D1–D4。

## 接口与行为

| 需求 | 契约 | 验收 |
|---|---|---|
| R1 | `DW_G4_ACCESS_MODE=allowlist\|all_active_users`，缺省 allowlist、未知值拒启；保留现名单配置。每请求确认 active、只认 Console cookie，拒混入 Agent bearer；all 不查名单，仍查配额／节点范围。页面随服务端模式显示，配置重启生效。管理员节点 API 独立鉴权，不受普通 allowlist 阻断。 | A1–A3 |
| R2 | 平台管理增「托管主机」；`GET/POST /api/console/admin/hosted/nodes`，`PATCH .../nodes/{id}`。仅 active platform_admin，写入严格同源 Origin。登记 id/name/pool/quota_mode/quota_policy_revision/address/scope；重复 id＝409，不能隐式轮换。节点、授权、凭据哈希、审计同一 PG 事务，失败全回滚。随机明文只在创建响应显示一次，no-store，不落日志／URL／浏览器存储，遗失用既有显式轮换 CLI。CLI 保留并共用默认范围／校验。 | A4–A7 |
| R3 | scope＝admin_only（缺省）／public／restricted；restricted 用户 id 或组码授权取并集、空集拒绝。按组长已定口径认该组创建者或拥有该组 active 成员 Agent 的用户；关闭添加不撤成员权限，已删组或失效引用拒绝。`GET /api/console/hosted/nodes` 与 options 共用过滤／容量 DTO；用户只得 id、名称、池、在线、剩余席位及三维余量，管理员另见地址／调度状态／授权。隐藏节点的模型能力也不返回。显式／自动选点、实际队列发起均查范围；落座事务锁节点并经 identity 事务查询复核 active、授权和成员，与撤权排序，不建第二份成员缓存。 | A8–A9 |
| R4 | hard／soft 均复用 seatcapacity：每维 max(套餐,1C／1536MiB／40GiB)，扣预留、外部负载、托管占用及未进快照的 pending；三维席数取最小，再限 MAX_SEATS。登录 writer 扣实际预留。单维不足跳过，全失败＝409 node_no_capacity＋原因，不泄露无权节点；未知／过期／策略不符拒绝。撤 hard 的 Hub 旁路；nodeagent 抽共用采样／占用／启动准入，hard 保留硬限额，不运行 soft enforcer。hard 上报完整快照，Hub 行锁内加 pending，本地再准入。R31 交付包含共用采样／准入的新版 nodeagent，P3 安装该 r31 精确提交／制品版本，不再安装 r30 nodeagent；新版未安装或策略／快照不满足时，旧 hard 拒绝新增，既有席不自动停机。 | A10–A12 |
| R5 | Agent 列表增类型（自有／托管）、主机（最近认证连接 IP／绑定节点名）。只在凭据认证成功后写 IP，重连覆盖、断线保留；失败不造成功读数。显式 DW_TRUST_PROXY=1 且 TCP 对端为可信本机 nginx 时采信有效单值 X-Real-IP，否则 RemoteAddr；忽略 X-Forwarded-For，非法头回退对端。nginx 必须覆盖 X-Real-IP＝remote_addr；prodops 2026-10-19T02:02Z 已只读核 dw123 生效配置的 `location = /console` 与 `location /` 均显式覆盖，并代理到 `127.0.0.1:8791`（本席未独立探测生产）。P4 执行前复核两处覆盖，当前不需改 nginx；若复核缺失，修改列入 P4 执行单待业主批，修复并复核前不启用 DW_TRUST_PROXY。仅本人／active 管理员响应序列化 IP，第三人／组员／Agent bearer 不得见，覆盖列表、详情、事件、投影。 | A13–A15 |

调度状态另存 enabled／disabled／draining，与心跳 online/offline 分离，心跳不得重新启用；后两者禁止新分配，保留既有席生命周期，排空不自动迁移／销毁。地址只展示、不用于命令；管理员填写优先，认证节点只补空值，普通用户不可读。

## 表结构、迁移账本与回滚

统一使用 Hub PG `schema_migrations`（`internal/store/migrate`，`HubDomains()`），不另建 G4 账本、不改已应用迁移。组长已采纳下列编号方案，集成时仍核无冲突：

| 增量 | 迁移与升级顺序 | 旧二进制读取 |
|---|---|---|
| `g4_node` 新增 `address`、`address_source`、`scheduling_state`、`access_scope`；新增 `g4_node_access_grant(node_id,subject_kind,subject_id)` 唯一授权表 | 新增 `0200_hosted_r30_baseline.sql` 冻结 r30 的现有 SchemaDDL，供空库建基线／旧库一次入账；`0201_hosted_node_access_r31.sql` 只做新增列／约束／授权表，存量节点 access_scope 回填 admin_only，调度状态 enabled，不清席位或轮换密钥。现有两处生产调用 `cmd/workpulse/newplat_hosted.go` 与 `cmd/workpulse/hosted_admin.go` 均改走统一 runner；ApplySchema 只保留调用 runner 的兼容入口，Hub／CLI／测试共用，不保留启动直写 DDL 的第二条路径。0200 冻结后不可修改，后续只增条目。 | 旧列、state 枚举和约束不改；r30 现有显式列 SELECT 可读新增形状，旧账本按既有域识别 0200／0201。须用 r30 启动和代表性读取实际回归，不能仅据 additive 宣称验证。 |
| `np_agent` 新增 `last_source_ip`、`last_source_at`（可空） | `0103_agent_last_source_r31.sql`，identity 域；不改 0100 基线或 SQLite 表。Hub 通过 identity store 记录认证连接；Console 服务端从 PG 按读者权限构造 DTO，不把 IP 复制到通用 SQLite 投影。 | r30 显式列查询可读；旧 Hub 不再更新最近 IP，字段读数可能陈旧。 |
| 节点容量与审计 | 容量扩展沿现有 `g4_node.capacity` JSONB，注册／范围／地址／状态操作沿现有 `g4_audit`；R1 为进程配置。 | 不新增 SQLite 迁移，不变更历史审计结构或 credential hash 格式。 |

旧库入账：0200 冻结 r30 幂等 SchemaDDL，不能借入账改写基线；有 g4 表而无0200账的库，先按冻结 r30 的表／列／类型／约束／索引清单验证完整形状，缺项或不符即停，不自修或伪造成功账。匹配的 r30 库经 runner 在锁内验证后只记0200的digest，不重放既有DDL／数据回填，不重建索引或约束，验证与记账同事务提交，保留对象与业务数据；然后0201做新增和admin_only回填。空库正常由0200建hosted基线。0103归identity域，完整Hub迁移按既有字典序施加，不声称0103排在0200之后。现网无g4表系组长依据10-18第1a只读结论，不据此豁免旧库用例。

**可读不等于可安全回滚。** r30 不执行新节点范围和 hard 容量门，直接换回旧二进制会丢失安全语义；不得作为开放后的回滚方案。部署件须选择保留这些门的兼容回滚版本，或先关闭托管入口并停止新建／调度后再回退；旧版仍可读存量席位的范围要用隔离库实测证明。不得为回滚删列／删授权表或回写旧迁移 digest；不在本任务操作任何现网库。

## 审查与实现门

入口：cmd/workpulse 的 hosted_access／hosted_routes／hosted_admin；internal/hosted 的 readapi／nodecred／orchestrator／seat_capacity；nodeagent 容量采样；identity PG store；m8 agent_ws／console_auth。与 R6／R7 共改 Console 时，编组正则／文案归组长。

交 A1–A15 路由／PG／容量／页面证据，覆盖两模式、凭据／审计失败回滚、撤权并发、三种单维不足、16C／30GiB／560GiB 扣预留的明确数例、hard／soft、代理开关与 IPv6。撤 active、节点过滤／最终准入、pending、hard 容量门、一次性凭据、IP 权限／可信头检查，分别必须红。请重点审编组授权语义、存量 admin_only 回填、0200 入账及 hard 采样范围。设计 PASS 后才实现，不部署。

D1–D4 必测矩阵（本后继为 docs-only，以下为实现验收要求，并非已跑结果）：

- **编组授权／A8**：restricted 组授权分别验证创建者与 active 成员 Agent 所有者可见且可选点；关闭添加仍可访问；无独立用户授权等其他允许来源时，移除成员、Agent 不再 active、删组或失效引用后均拒绝且列表／options 不泄露。覆盖撤权与落座并发的两种提交顺序：撤权先完成则落座拒绝；落座先完成可保留该既有席，撤权后不得再新增。
- **迁移空库**：隔离空 Hub PG 经统一 runner 建 identity 基线及0103，并从0200建立 hosted 全表、施0201；核列／约束／索引形状与0103／0200／0201各一条正确digest，重复启动不重放。
- **迁移旧库**：用冻结r30直写DDL建立 hosted 表、准备节点、席位、凭据哈希、账户／授权等存量数据且无0200账；一次入账0200再施0201，核表OID不变、不丢行、不改变既有凭据／席位／账户数据，不轮换密钥，已有节点access_scope正确回填admin_only。重复运行0200／0201账仍各一条；形状漂移拒绝且不写成功账。分别从Hub与CLI两生产装配入口核runner调用和跳过行为，确保无直接DDL旁路。
- **代理／A15**：P4前复核以上两location的覆盖；缺失走待业主批准的执行单。实现测试覆盖显式信任开关、可信回环对端、有效／畸形单值头、伪造X-Forwarded-For与IPv4／IPv6，不把生产配置证据当代码测试PASS。
- **nodeagent／A12**：P3执行单钉r31 nodeagent精确提交与制品摘要；未装新版、缺快照、过期或策略不符的hard节点均拒新增，已有席位不自动停止；新hard／soft同容量格与本地启动准入通过后才能接收新席。
