# S-R62-EXTSEAT-GROUPS 实现交件

## 锚与状态

- 分支：`coder56solzzc/r62-extseat-groups`，基 main `206d41b26dab646c0e50a111eed5a45ccf7c72f6`。
- 产品/测试冻结候选：`aaed8aaee6630e2683096c977a058da653fee7e2`，tree `8c8306960d118a172190cedcdd2f07ada79f546b`。后续交件只追加文档与 OpenSpec，VERSION 保持0.2.3.40。
- 批准设计 `eb194143747b131556aa3fa32dc5c83624674124` 原样导入于首提交 `8e10420a0`，A-AMEND-02技术设计也在该提交、早于产品实现 `ec17a8b5392edf71080e01242bb8b8fe94d52b13`。
- 已完成实现和指定作者门的执行，m8原命令rc1及已知N-4例外读数如实保留。等待qwen（VIEW-B之后）与组长独立复核，不自验收、不推main、不部署、不出发布件。

## 实现与入口

`internal/identity/communication.go:25` 提供同一PG事务中的统一授权：双方稳定Agent/属主活跃、外部席撤销与可信租户，再检查共享真实普通组或有效原AG。临时AG的np_group镜像不能独立授权；既有leader_only共同硬拒绝保留。普通组优先、多个普通组按code稳定排序。显式组只核该组；不存在、过期或失权时不借他组。

`internal/extseat/group_routing.go:94` 首次受理持久绑定source/target稳定ID、route_kind/ref/generation，外部↔外部在同一PG事务写目标inbox；内部目标继续既有PG outbox→SQLite幂等投影。重复ID不重新选路；并发首次同ID等待身份锁后再读幂等键。未投影outbox按原绑定重核，失权则dead/group_unavailable。普通组通信不续AG，仅AG路径保留既有受理/首次received续期。

`group_inbox.go:22` 统一list/read/wait/ack/clear/check/receipt，查询正文与写租约前检查原路由。Check/Receipt使用只读repeatable-read事务，不执行FOR SHARE，且不额外借第二条PG连接。MCP通信入口严格解析可选routing；旧请求缺routing仍显式表示原AG，不能隐式扩权；无AG旧请求拒绝。内部helper/AMR通过Identity通信授权与外部投影入口同样复核；legacy与direct的新能力席位共用leader_only守卫，纯旧AG席保持原兼容。

`identity/communication.go:157` 仅可信Console属主或平台管理员可加入外部席，复用关闭添加、组长/审计边界；不新增MCP自拉人工具，普通成员和外部canonical身份均不能冒充属主。加入/退出普通组不删除、不续、不重建原AG。Console成员选择器提供可管理外部席；管理员列表按真实在线状态投影。无AG无组显示“已验证／未编组，配对未创建”，目标为空而非伪造AG。

无AG地址冻结为`短名_EX`加128位稳定AgentID的32位小写hex部分。后缀不是权限，仍要求权威np_agent/np_extseat记录；旧AG地址逐字不变。subagent只识别canonical外部根地址，不因此开放外部子树寻址。

飞书OAuth state显式绑定可选配对选择，回调不信任客户端身份。无AG新开户需开关；已验证主体的ExistingSeats选择明确稳定ID，Reauthenticate按provider/tenant/issuer/subject恢复原身份、只新建令牌，不改地址/成员、不新建或续期AG、不刷新消息expiry。可发现身份不等于有通信权，无组通信仍403/1003/not_permitted；令牌到期仍停止鉴权。

## 迁移、开关与未展开范围

只新增`internal/store/migrate/migrations/0113_extseat_groups.sql`：可选真实AG关联、routing_enabled保留已启用语义、稳定路由绑定列与索引、旧行回填。旧迁移字节未改。新增迁移测试验证旧digest/施加时刻不动、列/约束与二次施加幂等。现迁移流按文件排序及digest账本，不要求编号连续。

开工核main最大0111、显示名远端当时未见；交件前再次核 main仍206d41b2、显示名分支d7910d36的迁移为0112，本片0113，未撞号。显示名未合本片，后续集成必须带齐两片迁移及各自台账预期，不把稀疏编号误判为已包含0112。

`DW_EXTSEAT_GROUPS`缺省关，只接受0/1。关掉后停新外部入普通组及新无AG开户，保留既有成员、原配对、稳定身份和认证恢复入口；现有授权仍按真实成员检查，不以开关撤销已有组、不重开过期AG。回退是停新增入口并保留数据，**不承诺直接换强制AG的旧二进制兼容新无AG状态**。

跨租户审批与tenant label没有冻结技术合同，按派单只留`extshared.TenantAdmission`接口，装配SameTenantAdmission，使用服务端配置TenantKey精确匹配；不接收客户端tenant label作授权，不开放跨租户注册。已向组长报告此界限。

PM01已投影件撤回不做；未投影件仅核原路由，不能用新共享组挽救旧件。TTL冻结片不解冻，MessageTTL/GroupTTL/TokenTTL均未修改；本片基main旧常量，后续与TTL片按常量组合。N-6通知、飞书摘要抄送默认、中文显示名均不顺手实现。

## 最终门读数（固定aaed8aaee）

环境为普通uid1001、Go1.22.2、锁定Python3.12.3、env白名单、数据盘HOME/TMP、真实PG strict，未连接现网或在线席位。PG实际记录`REAL_PG_SERVER_VERSION=18.3`、`server_version_num=180003`。显式设置DW_DESKTOP_INTEGRATION_PYTHON，避免可靠发送/claim格静默SKIP。原命令、UTC、白名单环境与raw gzip日志均在evidence，统计在gate-summary/package-counts。

| 门 | 结果 |
|---|---|
| SOURCE_COMMIT=aaed8aaee build.sh --test-only | rc0，BUILD_TESTS_OK helper_profiles=2 python=27 shell=2，inventory97 |
| extseat -count=3 | rc0，108顶层／201含子项PASS，0 FAIL/0 SKIP，104.146s |
| tempgroup -count=3 | rc0，12／24 PASS，0 FAIL/0 SKIP，20.382s |
| identity -count=3 | rc0，231／645 PASS，0 FAIL/0 SKIP，114.980s |
| m8 -count=3 | **rc1**，2957／4964 PASS，1 FAIL，33顶层／87含子项SKIP，965.591s |
| 独立migration全包 -count=3 | rc0，90／135 PASS，0 FAIL/0 SKIP |
| extseat+m8聚焦race | rc0，15 PASS，0 FAIL/0 SKIP/0 DATA RACE |
| 浏览器全量runner | 46/46 suite rc0，aggregate_rc0，包含新增console_extseat_groups |
| OpenSpec validate --all --strict（1.16.0） | 43 passed／0 failed，rc0 |

四包合计3308顶层／5834含子项PASS、1 FAIL、87含子项SKIP。每个SKIP名称与条件见evidence/SKIPS.md；不将真实CLI接收窗口、性能、专用浏览器开关等未覆盖项冒称执行。Console新套件使用生产HTML＋本地API夹具，不冒称浏览器直接连真实PG；后端授权另由真实PG格覆盖。

### 已知N-4与两锚对照

四包命令唯一失败为`TestSessionNameReconnectKeepsNameAndReleaseReuses`，`hub_test.go:3505 interrupted (9)`，全日志仅这一行中断。测试文件、SQLite驱动版本与SQLite产品码未由本片改动。组长2026-11-19T02:38:37Z明确要求照既定口径保留原日志、两锚对照即可，本报告不把原命令改成rc0。

整包结束后同机、同白名单、隔离HOME连续串行单格200次：main206d41b2为193 PASS/7 FAIL（7行interrupt），aaed8aaee为187 PASS/13 FAIL（13行interrupt）。两边失败均同一3505形态，原日志、开始/结束UTC与命令见n4-comparison。这里只证明基线可复现该已知族，**不证明发生概率相等、不拿单格取代整包、不据小样本推断回归因果**。最终例外是否可接受由组长及独立复核方裁定，不自行风险接受。

## 常驻格与删保护

13个`TestExternalGroups*PG`覆盖普通组优先、同组/跨组双方成员、AG镜像过期、显式组失效不回退、无AG未编组、外部互发、只读Check/原绑定读取、投影原路由、租户/leader_only、真实PG、并发同ID、同身份重认证及token/AG独立期限。另有迁移、OAuth state、canonical外部根地址与Console四格。

最终源五刀，全部编译rc0、业务断言rc1、无panic，逐字节恢复且工作树clean：

| 刀 | 保护与命中 |
|---|---|
| M1 | 去目标共享成员：OwnGroupDoesNotAuthorizeDifferentTarget，越组授权变nil而红 |
| M2 | 显式ref条件短路：OrdinaryAndExplicitIsolation，失效显式组借别组变nil而红 |
| M3 | 去来源成员：NoPairNoGroupAndExternalPeer，无AG无组来源发送变nil而红 |
| M4 | 去owner/admin条件：ControlledMembershipAndRollback，stranger添加外部席被错误放行而红 |
| M5 | 丢调用者身份、错误转用Agent属主：同格首先红“external Agent self-invite granted membership” |

四把主刀baseline与final-baseline均rc0；M5有独立baseline0/build0/业务红。evidence/mutations与self-mutation保存锚串、命令、SHA与断言。ee9dbfd2上的四刀仅作历史附加读数，不合并计数。初次不合格尝试中M1/M3因SQL参数/类型错误转红，整组已排除，保留在mutations-initial-unqualified并显式标记，绝不算业务杀死。

## 资格轮失败与停止记录

- ec17a8b5首次canonical rc1：claim集成夹具仍期待旧迁移列表，修夹具后全门重跑；不删除0113。
- ec17a8b5四包首轮rc1：tempgroup测试服务未给可信tenant配置，按安全fail-closed失败；补测试环境，不削弱产品校验。
- 93c63d26轮rc=-15：因继续完善重认证身份路径使其不再为最终源，只停本线程自己的进程，保留原始未完成日志，不拼接成最终结果。
- ee9dbfd2轮rc1：包含额外migrate包，旧ledger数量与DDL清单预期未同步；其m8三遍绿不外推到aa。aa对迁移预期与新增实际DDL断言修订后迁移三遍绿，完整四包重新执行如上。
- 额外N-4后台脚本曾只在等待整包完成时搬迁baseline工作树，未起跑任何取样；该等待进程停止记录单列，200次取样没有择优或中断。

## 交接

完成：批准设计与A-AMEND-02先冻结、产品实现、0113、作者门与五刀、失败/边界披露。阻塞：无新增产品实现阻塞；m8按已知N-4例外记录，不宣称全包全绿；尚待qwen及组长独立复核。下一步：只按复核新要求追加；R6修后锚一到立即暂停本片转复验。任何合入、发布构建、预发或现网动作均未执行，也不由本件授权。
