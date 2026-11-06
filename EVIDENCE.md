# S-R53-EXTSEAT-IMPL A 路证据（唯一写者合并，长门待续）

当前状态以末尾「两轮改动核对表」为准；前文保留分轮历史读数，不冒认适用于后续源码。

执行者 coder56solzzc.zzc / 01a19ca0-0622-7743-a995-767c05e49c78；直接起点262f4747b3d7def371b44d2feb07e4683f8f87e0；分支coder56solzzc/r53-extseat。仅实现A路，不是最终交件或独立验收；长门仍运行，完成后追加精确结果。

## 实现范围

- internal/extseat：PG权威主体/席位/token；token只存SHA-256哈希、恒时比较；每成功Authenticate持久CAS续14天，Recheck不续；吊销/代际/到期/回拨拒绝。
- PG0105唯一迁移文件：np_external_principal、np_extseat、np_extseat_token、np_extseat_inbox、np_extseat_send_key、np_extseat_outbox六表；np_agent新增受控external origin。无SQLite迁移，没有改其重试/ACK状态机。
- PG事务内受理、TouchGroup、入队/outbox；received是外部唯一确认阶段，首次确认触发续组，重复幂等；未确认入队24小时到期dead并可查原因；5秒收件租约到期原ID/代际重取。
- 外部→helper的outbox以稳定ID幂等投影；SQLite成功入队仅为projected，不当received。helper旧30分钟/12次策略自SQLite入队开始，未修改旧函数。
- Hub的routeToSession只在external名称分支进入PG适配；普通helper名称提前返回避免原身份锁重入。目录合并external presence后继续既有编组过滤；不伪造WS。HubHelperPresence和HubExtseatProjector供组长装配，Service.Run提供有界outbox/过期后台循环。
- D消费者接口与事务回调见internal/extseat/types.go。C IssueToken与B四通讯方法说明见INTERFACE.md；未实现扫码、MCP/REST端点、Console或main装配。

## 当前已完成的真实验证

- 初轮HUB_TEST_PG_STRICT=1临时PG正反用例通过；涵盖持久续期后新实例读取、吊销不复活、到期整点、时钟回拨、同键异参、事务回滚、空轮询/清单不续组、90秒presence、租约重取、首次received/重复ACK、错席位/代际、24小时过期、吊销中断等待。
- Hub定向用例通过：无WS的helper→external路由、正文/链接保持、external控制信封拒绝、Console presence、稳定ID投影重试与冲突拒绝；后续目录合并和新安全判据另行补完读数。
- 四个Go overlay变异已在最终源片上杀死（2026-11-06T04:50:41.585465+00:00 至 2026-11-06T04:51:00.314836+00:00），均rc1且命中对应断言：续期不写库→persisted renewal lost across restart；吊销续期复活→revoked token revived；同键异参放行→same key different request accepted；轮询续组→poll/list unexpectedly touched group。原始变异日志在本机/tmp/r53-extseat-evidence/mutations/，只通过Git交付摘要与源码。
- 初轮迁移门红在新增0105后的旧条数/六表清单断言，已逐项同步。初轮整包暴露普通helper路由新增身份查询导致旧评审锁重入；加external名称快速分支后，TestR8HTTPRealReliableReviewAndHumanOnlyFinalize独跑rc0。首次未隔离整包已中止，保留原件/tmp/r53-extseat-full-first.jsonl；不冒报通过。

## 尚在运行

隔离继承SH_/DINGWEI_/DW_/FCS_变量，PG均HUB_TEST_PG_STRICT=1；canonical使用短TMPDIR、独立HOME、锁定Python。store/migrate/identity/m8/extseat整包、相关包race -count=3以及m8四分片（每片-timeout 90m，覆盖清单本机/tmp/r53-extseat-evidence/race-m8-shards.json）。所有SKIP在最终记录列名和原因。最终tip/tree/ls-remote在追加交件回执提供。

## 组长 2026-11-06T04:54:43Z 路由入口增量

- 基线：2e5e0b02b5d77db0428c5d357152ce7e418f54bf；本项随下一个tip交付，不单独发DingWei回信。
- routeExternalSeat 仅在 extshared.SplitAgentName 返回 ok 后查询身份，其余返回 false,nil。Hub 使用小写会话索引，校验前仅将 `_ag` 恢复为 `_AG`，保留合法 bot_ag1 的实际路由；不改变共享名称解析器的语法。
- 新增 TestExternalRouteNonExternalUnderscoreSkipsIdentity：foo_bar、abc_AG0、abc_ag0 的身份查询次数必须为0、handled=false、err=nil；继续走 routeToSession 时保留普通离线错误，身份查询仍为0。既有无WS外部路由、目录、投影及旧评审锁回归同时通过。
- 普通定向回归：`go test ./internal/m8 -run ^(TestExternal.*|TestR8HTTPRealReliableReviewAndHumanOnlyFinalize)$ -count=1 -timeout 5m`；2026-11-06T04:57:36.757418+00:00 至 2026-11-06T04:57:50.381710+00:00；rc=0；日志SHA256 d4ac3aae974973ca5e9f4413b0d179390677827de0ad2df0156fe25f617ea533。
- race定向回归：`go test -race ./internal/m8 -run ^(TestExternal.*|TestR8HTTPRealReliableReviewAndHumanOnlyFinalize)$ -count=1 -timeout 5m`；2026-11-06T04:56:54.112300+00:00 至 2026-11-06T04:57:57.968149+00:00；rc=0；日志SHA256 74ca062310a71ca237dde818da047a7f51b661ab66c67ce99fad3188184d948c。
- Contains变异：`go test -overlay /tmp/r53-extseat-evidence/route-shape-overlay.json ./internal/m8 -run ^TestExternalRouteNonExternalUnderscoreSkipsIdentity$ -count=1 -timeout 5m`；2026-11-06T04:57:17.061076+00:00 至 2026-11-06T04:57:30.435575+00:00；rc=1；日志SHA256 18f94dad4e786db12fdf48b971bdaba9be794aec3fa027ca181f48c8330793c1。
- 变异仅使用Go overlay，将新语法分支改回 strings.Contains；三个子用例均红在 `non-external name triggered 1 identity queries`，不是编译失败或超时。变异不修改工作树。
- 原始日志及overlay：本机 /tmp/r53-extseat-evidence/route-shape-*；源码只经Git交付。
- 本次仅完成上述增量自测；此前canonical/整包/race长门仍由原流程收口，不据本项定向PASS声称最终交件、独立验收或上线。canonical尚未结束且当前日志出现既有claim schema断言失败，最终总证据须如实列结果。

## 组长 audience／scope 两问答复落实（合同593ed5ef第6节）

- 已从工蜂fetch opus50tc2k/r53-integration，读取精确commit 593ed5efdaa51bf671cc41fa5d8dc0b7c20ed78e，tree 7378e8bf41342e5edce35b42ed09aa783c1aa3c0 的 docs/design/R53-MVP-CONTRACT.md 第6节；已同步派单后的新增冻结，不单独回信。
- 0105令牌行持久audience与固定完整scope `list_online send_message wait_for_messages ack`，不支持子集；Service.Resource必须配置，Authenticate与所有快照现核共用load检查资源和完整scope。IssueToken签名不变。接口/context已注明main由组长统一注入TenantKey（DW_FEISHU_TEAM_TENANT_KEY）与Resource（暂定https://dw123.cmjrt.com/mcp），A/B/C共用配置；A不接收飞书access_token/refresh_token，不替代C回调或组长装配。
- TestTokenAudiencePG补齐持久化、错资源、空资源签发/鉴权、DB拒绝子集scope、移除隔离fixture内DB约束后应用层仍拒绝子集（Authenticate及Recheck）；其余令牌/队列PG整包与race三轮一并通过。
- contract6-core：`go test -json ./internal/extseat -count=1 -timeout 10m`；2026-11-06T04:59:28.457946+00:00 至 2026-11-06T04:59:55.465342+00:00；rc=0；日志SHA256 b24b718ace19345d7d960fb86d851253814fbc23e46f3af101d8fe391b48290e。
- contract6-race：`go test -json -race ./internal/extseat -count=3 -timeout 10m`；2026-11-06T04:59:41.034851+00:00 至 2026-11-06T05:00:52.155998+00:00；rc=0；日志SHA256 f7f856825a1de4fd6529b22f14eb6c6280d0c86ff9dab6f045e120818f19e2d0。
- contract6-audience-mutant：`go test -overlay /tmp/r53-extseat-evidence/contract6-audience-overlay.json ./internal/extseat -run ^TestTokenAudiencePG$ -count=1 -timeout 5m`；2026-11-06T04:59:39.916658+00:00 至 2026-11-06T04:59:45.686420+00:00；rc=1；日志SHA256 8dfa38fc60513995e5388cef2e7aa6e965a33ed223f34a5b950291c76be08738。
- contract6-scope-mutant：`go test -overlay /tmp/r53-extseat-evidence/contract6-scope-overlay.json ./internal/extseat -run ^TestTokenAudiencePG$ -count=1 -timeout 5m`；2026-11-06T04:59:39.916615+00:00 至 2026-11-06T04:59:45.670394+00:00；rc=1；日志SHA256 bc783d39bca79f6db6d76bf1358016ace0e1bb0a52bfb5320b03a01011e8b3d7。
- audience变异删除资源匹配判断，红在 `wrong audience accepted <nil>`；scope变异删除完整scope匹配判断，红在 `subset scopes authenticated <nil>`。均Go overlay，未修改工作树，不是编译失败或超时。原始日志本机/tmp/r53-extseat-evidence/contract6-*，源码仍只经Git交付。
- 原canonical门已于2026-11-06T04:58:14.174060+00:00结束rc1（日志SHA256 7584f5a9068bebb0d8105e8a372d41f37711b9921eb0125d2e111122bfd10141），失败为TestDeviceClaimAcrossActualClientAndPostgres五子项的旧迁移清单尚未包含0105；本项正反/race通过不消除此最终交件门失败。

## C路对接读数登记

2026-11-06T05:01:24Z登记k31t2075.zzc来信：对方报告已独立fetch并核验2e5e0b02b5d77db0428c5d357152ce7e418f54bf／tree 2b099a6150b6db951453a7c033c019025c77177d及远端分支一致，Groups相对e261aaa5无差异。C现有IssueRequest／IssuedToken容器与A签名不直接满足，拟由组长集成层薄适配器映射Seat.ID、group；本席认可无需A/C改包，已写入INTERFACE.md。此为C报告，未当成本席独立核验C实现、真实跨路测试或A路独立验收；集成适配仍待组长实施验证。

## 两轮改动核对表（唯一写者合并）

2026-11-06T05:03:34.655123Z 原子mkdir取得 /data/ai-dev-data/r53-extseat-01a19ca0.writer.lock，owner.txt已记录；保留两轮全部修改，不回滚。下表为本次合并提交的当前结论，覆盖前文旧阶段“仍运行/失败待修”状态；长门仍另行更新。

| 核对项 | 合并结果 | 正向／反向证据 |
|---|---|---|
| 外部路由入口 | SplitAgentName判形状；仅还原Hub小写_ag前缀，非法下划线名直接false且不查身份 | foo_bar、abc_AG0、abc_ag0零查询并保留普通离线路径；Contains变异三子项均红 |
| audience／四项scope | 0105持久资源与list_online/send_message/wait_for_messages/ack完整授权；签发签名不变，main同配注入 | 跨资源/空资源/子集拒绝；删audience或scope检查分别红 |
| 投影同事务身份锁 | ProjectOne同一PG事务锁定双方Agent及属主/席位，读取当前身份快照；Hub投影不另开身份连接；稳定ID和原helper ACK/重试保持 | 真实PG中两侧身份更新均被锁阻止；分别去源/目标锁红；重新加入Hub身份查询红；幂等/异参冲突回归通过 |
| 锁等待后时钟重核 | token行锁、AG消费者、发送目标锁及收件/ACK消息锁后重新采样，核token/AG/消息整点到期；原ID后台投影失败重试保留 | 真实token行锁等待后整点拒绝、AG消费者等待后拒绝；删除后采样保护两用例均红；Run瞬态失败同ID重试通过 |

### 当前门读数

- writer-focused rc0；writer-full rc0（store/store-migrate/identity/m8/extseat整包），2026-11-06T05:05:15.137109Z至05:09:13.844381Z，日志SHA256 15c9aa606f0fde763bcb6d888914343fe3bead4fa95b2eacb6bb484c9e00d9f8。
- writer-desktop rc0，真实本地Hub/PG/桌面helper跨runtime六组；已同步0105后的claim迁移清单。前文canonical失败定位的五子项此次实际通过；writer-claim-canonical未设置Python而SKIP，不当作修复通过证据。
- writer-strict-sealed rc0；PATH里没有全局openspec的初试writer-strict rc127，已使用本地钉版1.16.0绝对路径重跑通过，没有安装/升级工具。
- 本轮七项overlay变异均rc1且红在对应判据，不是编译错误或超时。原始失败输出在docs/reviews/r53-extseat-writer-reconcile/MUTATION-FAILURES.json；原始命令/UTC/rc/hash在RESULTS.json；完整包逐项SKIP/原因及包级统计在FULL-SUMMARY.json；源码摘要在SOURCE-SHA256.json，提交前已逐字节核对未变。
- 原始大日志本机/tmp/r53-extseat-evidence/writer-*；Git内留可复跑命令、回执、哈希与失败原文，不经文件服务传源码。所有PG严格模式HUB_TEST_PG_STRICT=1，测试普通用户，继承SH_/DINGWEI_/DW_/FCS_剥离，隔离临时路径；未触碰现网。
- 按组长05:03:21Z本轮特许，合并提交推送后从精确提交的隔离工作树重跑canonical/race；本回执是合并tip交付，不宣称长门或跨路独立验收完成。无部署、重启或生产配置改动。
