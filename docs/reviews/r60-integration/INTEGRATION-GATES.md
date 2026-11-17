# R60 正式集成门

当前依据：组长2026-11-17T01:37:18Z修订Z3集成门，替代00:58Z“任一次interrupt即停”。②产品仍3db78e5b/证据b17593fb，Z1两格四刀不变，Z2只观察。①b48已获qwen独立PASS，四项按③④①②合入正式树，版本0.2.3.40。固定源整包门与变异结果以evidence/及REPORT为准，不用定向工作态绿代替。

输入togpt60lunazzc/sqlite-interrupt@a931eb3fbd533334dc60a548044db043b2391e6a（treeb5e03fc818e713db7fcaed0aa6107437b72f3c00，parentd2cc47c7）：已从Git读报告，报告为单docs/REPORT.md，不是作者复跑。其3db0/3、1671 1/3，合qwen为3db2/6、1671 1/6、main0/2。标准store/database/sql单连接双goroutine探针排除“仅排队超时会interrupt另一活动SQL”这一具体机制，不能外推为排除所有SQLite竞态。低频根因未定，不归因②；后续转r61跟踪，不要求先改3db/store/驱动才能按新门验证r60。

## IG-01 Z1 两格与四刀

只在正式集成提交补测试，不改冻结的3db本身。

1. 封口＋真实viewer连接：已有viewer建立后封闭后台工作接纳，调用离线清理。断言可见gap，以及实际WS关闭而非仅等待超时；不能先调用startViewerOutput的封口路径，让其已有gap/关闭替代被测的closeOfflineTerminalViewers分支。分别删该分支gap与CloseNow，编译成功并在对应业务断言红。
2. 离线时队列非空：受控背压/同步将待显责任保留到离线清理，观察未显示字节被标gap，离线之后输出责任不再获准入/发送。使用明确同步与独立写槽，不能靠sleep撞顺序，也不只断言实现字段。分别删待显字节gap保护、离线q.stopped停写保护，须在对应业务断言红。正常离线状态与1000关闭协议继续保留。夹具须保证失败时有界清理，不能用整包超时、panic、编译失败充刀红。

两格形成后重跑基线/四刀/还原后基线，逐刀记录原锚、before/after、命令、UTC、编译rc、业务断言与原日志SHA；不把qwen四刀存活改写，也不与作者旧28刀合计。

## IG-02 m8 五轮串行，精确两格例外且至少三轮全绿

1. 固定正式SOURCE_COMMIT、起止clean、普通用户/env-i/短HOME/TMPDIR、HUB_TEST_PG_STRICT=1。m8整包串行5个独立-count=1轮次，-p1、足够超时，不与其它Go/PG门并行。每轮分别留命令、UTC、rc、逐测试PASS/FAIL/SKIP、完整原日志及SHA。
2. 每轮逐名记录interrupted(9)行数（允许interrupted与括号间空格）及所在用例，不以单跑替代。只允许以下两格中出现的该类已知现象：
   - TestNewSessionNameNotifiesFeishuOnceAndReconnectFlappingStaySilent，hub_test.go原3741～3746一带；
   - TestViewMentionRoute/a_peer_without_delivery_ack_v1_is_not_sent_to，view_mention_route_test.go原420一带。
   以名称/文件与实际错误记录为准，正式合并后行号漂移不能自动增删允许范围。
3. 以下任一即停、封锁后续集成推进：interrupt出现在上述两格之外；R60改动文件内的用例出现任何FAIL；5轮完成后整包全绿少于3轮。其它原门未获失败豁免，不把别种失败伪称已知interrupt。已在现有四输入独立核上述两测试文件未改；正式提交仍按110到SOURCE_COMMIT的实际diff重建改动文件映射，记录FAIL对应测试定义文件，不能只看出错helper的行号。
4. 5轮中至少3轮整包rc0/0FAIL才达到该门。允许范围内出现interrupt的轮次保持原rc/FAIL/计数，不改SKIP、不记整包绿、不从分母删除，不能“继续跑直到凑够3或5绿”或挑选最好5轮。若触发即停条件提前停止，未运行轮明确记未运行，不作完整5轮通过。
5. go test -json可保留Package/Test/Output归属；无可靠Test/文件归属的interrupt标未知，不能按最近RUN或预期将其塞进允许范围。暂不能证明属于两格时停报，等组长判定，不自动扩白名单。
6. togpt报告1671的单次失败是TestSessionNameReconnectKeepsNameAndReleaseReuses（hub_test.go:3505），不在组长指定的两格名单；正式树若出现，仍按越界即停，不因报告显示既有就自行加入。
7. 既有完整四树Go仍须覆盖store/obs/cmd，不用五轮m8代替其它包；若四树中的m8计入一轮，必须有同条件、独立可核的整包结果与逐名计数。Z2全局obs差值仅在出现非+1时再处理，不预改夹具。

### 已作废的旧IG-02

00:58Z“一次interrupt就停且等定因修法再集成”仅作为历史保留，已被01:37:18Z的新门替换。当前定因转r61，不以旧门额外阻塞r60，也不把新门表述成无条件忽略所有interrupted。Z1/其余R60门、产品冻结及补丁复核规则不变。

## 其它仍有效的门

③④①②固定顺序、110e2c1f基点、正式VERSION0.2.3.40、retainCanonicalTrustedProvenance回归、当前WS代际heartbeat/current与inventory97、同request-id双真实CLI只一条Hub投递、①②完整慢读/背压unknown→dw_send_status同source/delivery恢复联合格及删保护红、build.sh SOURCE_COMMIT/canonical两profile/四树Go/浏览器全量PATH含go/strict均保留。

Z2仅观察：全局obs.Default精确+1差值若在集成门出现不符再处理；当前不先改夹具。上线观测caller/outbox行数与物理存储无硬界的披露继续保留。无部署/预发/main推送授权。

## Z3后续归属与产品边界

01:37:18Z裁定将定因转r61 S-SQLITE-INTERRUPT后续，本席不另做二分或实现store/驱动修法。togpt报告未定位根因，也未证明②增量导致；不再沿用先前“排队取消跨调用误伤已证”的推断。若另有产品补丁，仍按组长新锚/复核与版本范围令执行，不顺手并入r60。旧00:58:41Z先定因修法再继续的相关停门描述以IG-02当前版本为准。①冻结条件已满足，正式合并记录见CONFLICT-RESOLUTION.md。
