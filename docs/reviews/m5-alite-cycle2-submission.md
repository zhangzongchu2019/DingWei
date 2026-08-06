# M5 A-lite cycle2 候审包(devo48)

- 候审分支:`feat/m5-alite-cycle2`(新周期新不可变分支,基 4ab9aae 谱系续,历史零改写;旧线两锚仅输入)
- 输入:**冻结契约 v1.8@6d47812**(shenceclaude APPROVE@23a9c118,B1 前置已消)+ **dev56 CR@aa0b0fa3 B2/B3/B4 强制输入**
- 机械核:build/vet 净;`go test -race -count=2 ./internal/datad ./internal/jcs` 全绿(显式退出码 0)
- 真 PG 证据:`docs/evidence/cycle2/`(PostgreSQL 16.14 独立测试库,TestPG* 9/9 -race exit=0,MANIFEST.sha256 封存;可复现=scripts/na3-pg-evidence.sh)

## B2 — /assign 传输门 strict(server.go handleAssign + assignment.go decodeAssignStrict)
①整体限长:读 `1MiB+1`,len>1MiB=**413 明确拒**(不静默截断——截断体恰为合法前缀会被执行,正是 CR 场景);②`rejectDupKeys`:token 级逐层键集扫描,任一对象层级重复键=400(嵌套同拒,"后键覆盖"歧义体不入解码);③Decode 后 `Token()` 必须 io.EOF(尾随第二对象/标量=400)+DisallowUnknownFields+原文 UTF-8 门。**全部先于业务门=任何传输拒付零首果落账**(测断言)。传输错=HTTP 层 400/413 素文(冻结契约 §3 白名单不涉传输形,不私增业务码)。
测:TestB2TransportStrictGate(413/尾随×3/顶层+嵌套重复键/零落账/合法体+恰界不误伤)。

## B3 — request_id 绑定完整操作身份(assignment_mem.go/assignment_pg.go)
首果命中**先比 FirstResult.TaskID**(再比 request_hash):跨 task 同 request_id(同/异 body 皆然)=**稳定 409 request_conflict**,audit 同事务落,目标 task 零污染(零 assignment/status 原样);原 task 重放不误伤。取 CR 给的第二案(比对存档 task_id)而非改 hash 定义——request_hash=「请求体」canonical 是 v1.8 冻结文本,task_id 在 URL path 非请求体,动 hash=契约 delta;比对案语义等强且零契约面。
测:TestB3CrossTaskMem(成功首果/错误首果/条件变化后稳定+audit)+TestB3CrossTaskRequestID(HTTP 层+TB 零污染断言)+TestPGB3CrossTask(PG 行级+audit 同事务)。

## B4 — PG PutTask 原子化(pgstore.go)
整函数事务化:`BeginTx`→`pg_advisory_xact_lock(hashtext('datad.assign:'||id))`(**与 Assign 同锁**)→active executor 校验→task 写(普通 CAS 与 force/UPSERT 双路径均在事务内)→读回→Commit。CR 的并发序(PUT 读旧 A→Assign 提交 B→PUT 写 A)在共锁串行下不可能;冲突拒=事务回滚零改 task。
测:TestPGB4PutAssignInterleave——K=12 真并发交错(重派翻代×PUT 混打),终态不变量 `owner_session==当代 active executor`(SQL 行级);定向负例:owner≠executor 普通+force 双路径稳定 owner_conflict 且 task 逐字节零改(updated_at 同值断言)。

## 声明
一次交齐,无后续补证项;B2-B4 逐条对 CR 判据闭合;证据链中间曾有一版含测试助手自身 race 的失败批(pgUID 计数器无同步,-race 抓获后改 atomic 重跑,历史留痕如实,主码零涉)。
