# R31 R1–R5 接线说明（供 P3／P4 执行单引用）

本件是候选代码的说明，不是执行令。P1 plan／apply、P2、P3、P4、P5、P6 各阶段仍按组长分别下令；本组四席留在现自托管容器，不重建迁移。本分支未部署或操作节点 43。

## Hub 与 Console

- `DW_G4_ACCESS_MODE` 仅允许 `allowlist`／`all_active_users`，省略为 allowlist。原 `DW_G4_ACCESS_ALLOW_USER_IDS` 仍按规范小写 ID 校验。配置由启动读取，切换需重启；P4 不应替 P6 决定全员开放。
- 托管面原有主密钥／组合／镜像配置仍按 r30 接线。业主先取主密钥备份，再执行 P4。没有主密钥配置时不注册托管面。
- active platform_admin 在平台管理内看到“托管主机”；仅有 manager 角色不开放节点 API。R6 提升管理员由组长分支交付，执行仍待另令。
- `GET/POST /api/console/admin/hosted/nodes`、`PATCH /api/console/admin/hosted/nodes/{id}` 使用 Console cookie；写入要求准确 Origin，拒 cookie 混 Agent bearer。管理 API 独立于普通托管 allowlist。
- POST 接收 id、name、pool、quota_mode、quota_policy_revision、address、access。access.scope 省略时 admin_only；public 表示全部 active 用户，restricted 使用用户 ID／编组码并集。组授权包括创建者与 active 成员 Agent 属主；关闭添加不撤已有授权。失效引用不能授予访问。
- PATCH 仅接收 address／access／scheduling_state，省略字段在持锁事务内保留；state 为 enabled／disabled／draining。后两者阻止新选点和新落座，不批量停止或删除既有席位。节点心跳不能恢复管理停用，也不能覆盖管理员填写的地址。
- 凭据仅 POST 成功响应显示一次，不可通过列表取回；遗失走现有显式轮换 CLI。重复登记拒绝，不隐式轮换。不要把凭据写进执行报告、Git、消息或示例。

## P3 nodeagent

安装对象须为 r31 新版 nodeagent，提交与制品摘要由集成锚确定；不得沿用 r30 nodeagent。节点策略的 MAX_SEATS 在 P3 环境中显式设为 4；hard／soft 均需要批准的完整策略及 revision。`nodeagent --print-policy-revision` 只计算非秘密策略摘要，不连接 Hub 或读取凭据，可用于对齐登记值；执行环境必须与实际服务环境一致。

hard 保留现有硬磁盘配额实现，只增加持久容量账、占用采样与写入准入，不启动 soft enforcer；soft 继续原保护。最低容量计费 1C／1536MiB／40GiB，取每维 max(套餐,最低值)，扣主机预留、外部负载、本地占用、未入快照 pending，再限 MAX_SEATS。既有套餐的硬磁盘限额值没有在本件重定义。登录容器按实际临时预算计入，不占完整业务席位槽。

hard 的容量账位于配置的 ledger 路径追加 `.hard-capacity`，必须在席位目录之外，目录私有。未知旧席位／容器只令新增准入不可用，不自行接管、停止或删除。旧 hard 缺完整新快照、策略不匹配或过期将拒绝新增，既有席位不会由容量采样自动停止。P3 原“安装但不注册”阶段保持不注册，不因为此说明提前登记节点。

管理员可填写可选 address，或在 nodeagent 环境指定 `NODEAGENT_ADDRESS` 由认证心跳上报。管理员填写优先；只改状态不改变地址的来源标记。

## 迁移、可信来源与回退

统一 Hub 账本新增 0103、0200、0201；生产两处 hosted ApplySchema 都进入统一 runner。执行前将三编号与最终集成账本核冲突，不重写旧迁移。空库建立基线；完整 R30 直写 hosted 库验证形状并入账，已有节点回填 admin_only。partial／形状漂移的旧基线拒启，失败即停，不通过手工删账或重建表自修。

`DW_TRUST_PROXY=1` 仅在 TCP 对端为回环时采信单个合法 X-Real-IP，其他情况取 RemoteAddr；不采信 X-Forwarded-For。依据 prodops 2026-10-19T02:02Z 只读结果，dw123 的 `location = /console` 与 `location /` 都已覆盖 X-Real-IP 为 remote_addr，代理到 127.0.0.1:8791。P4 前再次只读复核；缺失则列入执行单待业主批，覆盖修复并复核前不启用信任。凭据认证并完成 WS 登记后记录自有 Agent 来源 IP，断线保留；仅本人／active 管理员经人类会话看到，不在 Agent bearer 或通用会话目录公开。已有u_凭据端点的历史IP在目录读取时即遮盖，实际重连时显式清除通用表原始值；不从旧的未验证来源回填新的私密IP列。传统非凭据会话保留原断线IP语义。

新增列／表不删旧结构，本地隔离旧二进制测试证实启动与读取形状兼容；开放后不能直接以 R30 回滚。须先按批准执行单关闭托管入口与调度／作业，再回退，或用独立审过的兼容回滚版；只清空 allowlist 不等于关闭已排队作业。旧 R30 不保留新节点范围／容量与来源 IP 隐私约束，因此兼容读测试不构成安全回退批准。
