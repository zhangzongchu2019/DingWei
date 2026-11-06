# S-R53-EXTSEAT-IMPL A 路证据（中间版）

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
