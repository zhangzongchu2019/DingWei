# r61 第五段：AUTH-FIRST 合入、串行完整门及独立版本提交

本件为作者交件证据，不代替组长与独立复核、不构成发布 GO。未推 main、未部署、未出或上传发布件；本地编译只用于隔离进程联测。

## 坐标与范围

依据组长 2026-11-19T05:52:15Z 合入令和05:56:04Z串行令。第四段远端5305358fd2e37f2eabe6f5349877eba7b015d143上 --no-ff 合入冻结 AUTH-FIRST 584eb119dd86f60549b8871d51a1811fe6f1019f；合并源ce3c084af7ec2d249b9026e96f2b5fa77e71cc84，tree0ae992728f229511b08ace3eda05bcd75c79b6ee，ort无冲突、未手工修改产品。追加唯一集成测试internal/mcpgw/r61_auth_first_baseline_test.go后固定完整门源8cf2de74cb51fe7eb340541b4db099093eee92f5（VERSION0.2.3.40）。新测试以真实本地TCP HTTP覆盖三版本initialize及空params ping，均无凭据/无版本头，检查401及原始challenge，不调用资源或业务。

AA010同步夹具与d6的AST文本SHA逐字一致；f7b32525 guide修复、415b9bf5时序窗及retainCanonicalTrustedProvenance均在。NAME/GROUPS/r62不在本件。AUTH协商支持返回2025-06-18，不宣称完整实现2025-03-26批处理或2025-11-25任务能力。

## 串行与被停止的旧读数

串行令读到前曾启动自己的canonical及Go并行门，读到后仅按精确PID停自己的runner、waiter及子树；原始部分日志单列partial，不记PASS、不与重跑拼接。停止审计crossed-load-stop.json与old-sessions-recheck.json记录无存活旧进程。N6那道门在读令前已经自然结束，不虚报中止，保留旧读数且此后暂停N6。最终本件所有验收门从06:12:39Z开始按单runner严格串行，前项结束才起下一项；见gate-sequence、命令、UTC及SHA。

## 执行环境

普通uid1001、Go1.22.2、Python3.12.3、OpenSpec1.16.0；显式白名单与独立数据盘HOME/TMP。SOURCE_COMMIT钉完整源；HUB_TEST_PG_STRICT=1、DW_DESKTOP_INTEGRATION_PYTHON为锁定解释器。PG运行时抓取自身Go子树PostgreSQL18.3与PID/exe，不把设置环境变量代替PG实跑。SKIP逐名、条件与原始日志行在SKIPS.json。三向兼容用独立真实Hub/helper进程及loopback/private状态，模型消费者暂停夹具，无真实CLI/provider调用，不能冒称端到端模型消费。

## 门读数

冻结源8cf（0.2.3.40）的真实串行结果：

| 门 | 原始结果 |
| --- | --- |
| build.sh --test-only | rc0，Claude/Codex各1290，inventory97；BUILD_TESTS_OK helper_profiles=2 python=27 shell=2 |
| m8 -count=3 PG strict | rc0；2994顶层/5070含子PASS；30顶层/84含子SKIP；0FAIL、0 interrupted |
| store/... -count=3 PG strict | rc0；store1227/2022 PASS、15 SKIP，migrate87/132 PASS、pgtest45/204 PASS；全部0FAIL、0 interrupted |
| focused race（ACK/queue_full/failure notice/history） | rc0；103顶层/175含子PASS、17含子SKIP；0FAIL、0DATA RACE |
| 浏览器全量 | 48/48 suite rc0，aggregate_rc0；r15_session_history_pagination、r15_session_history_mutations、r15_follow_release_after_replay均rc0 |
| OpenSpec strict | 47 passed／0 failed，rc0 |
| 三向真实进程兼容 | 3/3 rc0：旧501+新helper busy/retryable且0notice；新+新 failed queue_full/retryable=false且1notice；新Hub+真实旧39helper busy/retryable且0notice |
| 真实本地HTTP四基线 | 1顶层/4子格PASS，0FAIL／0SKIP，三版本initialize和空params ping均401、原WWW |

m8/store共4353顶层/7428含子PASS、99含子SKIP。SKIP逐名与条件见evidence/SKIPS.json，不能将祖级+子级相加作独立用例数。初始三遍全绿，Z3无例外命中，不起多余五轮或基线对照。PG实取PostgreSQL18.3、进程PID与exe见serial/pg-runtime.json。

canonical中test_guide_staging每profile25格（共50个测试起始行）均随该profile整体rc0；部分verbose起始行被子进程日志穿插，不以单行尾部是否有ok判断。TestR60OutboundControlBackpressureRecoversWithSendStatus、TestHostedBodySendSameRequestIDAcrossCLIProcesses、TestHostedBodySendAcrossRealHub在三遍中逐次PASS，0SKIP。AA010增强同步夹具仍在，没有退回原竞态断言。

## 变异与版本

完整门后独立一次HTTP鉴权顺序变异：把版本校验放到鉴权之前，编译成功但四基线业务断言红，逐字节还原后绿。只算本件一刀，不与AUTH作者12刀或qwen5刀合计；串行令前的同形附加读数不重复计数。

完整门和该刀完成后，独立版本提交4a349ee59a1d8d7b8b9c562c462bded2f8b0f192（tree7fa866de593b6c382ee9c209eb0ac744b9cb47b4、父8cf2de74）只改VERSION、internal/version/version.go及helper __init__.py为0.2.3.41；页面源码不写版本号。

升版后单列：m8整包rc0，998顶层/1690含子PASS，10顶层/28含子SKIP，0FAIL、0 interrupted；未借用升版前读数。升版canonical rc0，Claude/Codex各1290、inventory97；BUILD_TESTS_OK helper_profiles=2 python=27 shell=2；2026-11-19T07:10:22Z至07:19:50Z。两段所有门严格串行，未在其间启动N6门。

## 交接

完成：冻结AUTH合入、真实HTTP基线、串行完整门、独立版本提交及升版后门；阻塞：无作者门阻塞，待组长/独立复核。下一步：由组长确定构建与发布路线，作者不推main、不部署、不出发布件。N6只在本件VERSION交接后按令恢复。
