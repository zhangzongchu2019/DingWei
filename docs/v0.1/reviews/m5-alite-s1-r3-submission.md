# M5 A-lite S1 R3 终轮候审包(devo48)

- 候审分支:`feat/m5-alite-r3`(新不可变分支,基 R2 被审锚 12dfa894 谱系;禁 force-push,中间锚不请审)
- 契约:v1.7=61e5938e(冻结)+ **v1.8 delta=caa5c3a2**(invalid_request/sub 三子因/A13/首果账语义域;shenceclaude 预判①②已纳,候其会签——若裁定改码名,改动局部于双射表+测试表驱动)
- 评审输入:dev56 R2=CHANGES/7(a13ac9d)+ N-A3 addendum(cd06d98)+ R1(5f3e63f)
- 机械核(本机):`go build ./...` 净;`go vet` 净;`go test -race -count=2 ./internal/datad ./internal/jcs` 全绿(显式退出码=0)
- 真 PG 证据:`docs/evidence/na3-r3/`(PostgreSQL 16.14,本机独立测试库 dingwei_r3;MANIFEST.sha256 封存)

## R2-1..R2-7 逐项闭合映射

### R2-1 JCS(A1 全文)
`internal/jcs/jcs.go`:①`canonicalRequestHash`→`(string,*AssignErr)`,canonical 错=invalid_request fail-closed,不吞错不 hash 空字节(assignment.go);②hash 覆盖完整请求体**含 request_id**(TestHashCoversRequestID:仅 id 异⇒hash 必异);③`decodeStrict`:Decode 后 `dec.Token()` 必须 io.EOF,尾随第二值/标量=ErrTrailingData(RawMessage 内嵌路径同门);④原文 utf8.Valid+Go 字符串/对象键 ValidString(UTF-8 编码代理半区拒)+`validateStringEscapes` 原文层校 `\uXXXX` 孤立/倒置/残缺代理=ErrBadEscape(encoding/json 静默 U+FFFD 正是被拒行为);⑤包文档改口**"RFC 8785 整数子集"**,不再宣称完整实现;浮点机械禁止=AssignRequest 类型化 struct(expected_generation *int64)+jcs fail-loud 双层;⑥Mem/PG/HTTP 单一调用点(canonicalRequestHash)+`testdata/golden.jsonl` 官方向量(含 §3.2.3 键序原例:😀 代理对 D83D<FB33 排 דּ 前=UTF-16 判别点),期望值由**独立 Python 实现**(UTF-16BE 字节序+最小转义)生成交叉字节比对,B-lite 同引本文件。测:TestGoldenVectors/TestTrailingRejected/TestInvalidUTF8Rejected/TestLoneSurrogateEscapeRejected。

### R2-2 请求门/双射(A2 全文)
`validRequestID`(UUID 形冻结,v1.8 D3;malformed=422 不入账 D4)/`validAssignRequest`(负 gen、force 空 reason=invalid_request+sub;half/self 保序)/首派带 expected_generation=412 stale_generation(D5,Mem+PG 同判)。force 空 reason 单一路径入 Store(handler 预检删除,Mem/PG/HTTP 不再分叉)。**真路径逐码表驱动**:TestA8ErrorBijectionRealPath(9 码逐一真实触发,断言机器码+HTTP+首果账三者一致;store_down 映射面+PG 断连可证)+TestR1ErrorBijection(表完备对称)。新码=契约 delta 批件(caa5c3a2),非 wrong_state 顶包。

### R2-3 首果 body=权威体(A3 全文)
handler 业务错输出=`writeJSON(ae.HTTP, ae)`(统一 AssignErr DTO JSON),与首果账 `failFirstBody` 同 struct 同 marshal——TestHTTPAssignDTOMatchesFirstResult **逐字节**断言 HTTP body==账内 body,首次==重放。PG `recordFail`:首果 INSERT/audit INSERT/Commit 任一失败=store_down fail-closed(绝不落账失败还返业务错)。诚实注:PG jsonb 存储会规范化空白/键序(字节≠保真),**HTTP 边界字节一致性由单一 DTO 再 marshal 保证**(重放路径=unmarshal 同 DTO→同 marshal),PG 测试为 DTO 字段级同值断言+HTTP 层逐字节测互补。

### R2-4 audit 事务内原子(A4 全文)
`Assigner.Assign(...actor string)`:handler 传真实 `id.Session`。Mem=同 mu 锁边界 append;PG=`auditInTx` 同事务 INSERT(commit 前)。detail 完整绑定 task/generation/executor/reviewer/request_id/force/reason/outcome/sub(assignAuditDetail)。handler 仅记认证 denied(deny 路径),成功/业务失败 audit 全在 Store 原子边界,无重复记账;重放不追加。测:TestAuditInStoreAtomic/TestA6ForceOverWrongStateAudited/PG 侧 TestPGAssignFull audit 字段级断言+A13③ 失败首果与 audit 同事务两在。

### R2-5 并发测试(A5 全文)
Mem:改回 **TestA2ConcurrentUnique**(不降级),K=8 goroutine 真并发:恰一赢家 gen1;**每个 loser 断言精确码 stale_generation+HTTP 412+首果账 body=DTO 字节同体**;终态 active 恰 2/同 gen=1/角色各一/assignee 正确/owner_session=executor/status=dispatched。PG:TestPGA2ConcurrentUnique 真 PG K=8(advisory 串行+partial-unique),同判据+SQL 行级终态+**partial-unique 直证**(assign 角色直 INSERT 第二 active executor=23505)。两层各自成立,不互代。

### R2-6 深拷贝(A6 全文)
`recordFirst` 写边界深拷贝(传入 body 复制入账);读边界保留。TestFirstResultDeepCopyBothWays:写入后改原 slice 账不污+读返回改动不回渗,双向。PG Scan 天然新副本。

### R2-7 N-A3(addendum 全条款)
- **三角色**:`deploy/datad-grants.sql` v2——datad_user(常规池)对 assignment/assign_first_result **零写权仅 SELECT**;datad_assign=Assign 单事务最小权(assignment S/I/U 无 DELETE;首果 S/I 无 U/D;task S/U;audit INSERT+序列 USAGE);datad_assign_gc=仅首果 SELECT/DELETE+task SELECT,assignment 零权。
- **独立受控连接池**:`OpenPGPools(dsn, assignDSN, gcDSN)` 三池独立登录凭据,**无 SET ROLE**⇒结构上无事务后 role 泄漏面;cmd 接线 DATAD_PG_ASSIGN_DSN/DATAD_PG_GC_DSN(未设=dev 回落主池+启动告警)。assignment_pg.go 全部事务走 assign 池。
- **GC**:`GCFirstResults`(gc 池)仅删 created_at<now()-窗 **且** task∈{done,archived};TestPGGCFirstResults 三态(closed+超期删/open+超期留/closed+新留)。
- **仓根 WalkDir 守卫**:TestAssignSoleWritePath 从 go.mod 根扫全部 .go/.sql,写语句(含 assign_first_result 与 DELETE)按**类别显式 allowlist**(impl/grant/test 三类逐文件),新增命中默认失败+反向哨兵;TestGrantFileCoversThreeRoles 部署件结构自检。
- **真 PG 原证**(docs/evidence/na3-r3/,可复现脚本 scripts/na3-pg-evidence.sh):`acl-dp.txt`=\dp 四表 ACL 封存(datad_user=rxt 无写/datad_assign=arw/gc=rd 等);`go-test-pg.log`=TestPG* 7/7 PASS -race 显式退出码 0(含 assign 正路径成功/app+gc 角色 INSERT/UPDATE 全 42501/assign 无 DELETE 首果/audit 不可改/TestPGRoleNoLeak 三池 current_user 恒定多轮借还);`na3-evidence.log`=全程含 psql 直连负向裸证第二面(permission denied×4)+git anchor(da736b1)。
- **诚实边界两条**:①本证据=本机 PostgreSQL 16.14 独立测试库(生产 dingwei 库的同版 grants.sql 生效证据归部署门,prodops 执行时以同脚本产出——grants.sql 入仓不算生效,addendum 原话,不夸大);②datad_user 为表 owner(生产迁移路径同),REVOKE 后 ACL 拒其 DML(已证),但 owner 理论上可自行 re-GRANT——恶意 SQL 面不在本门威胁模型(addendum 防的是"新增端点悄悄绕过"),如需杜绝须迁移角色与运行角色分离,记为部署门候选项非本轮阻断。

## A1-A13 验收映射(Mem+PG+HTTP)
A1=TestA1AtomicDualRole+TestPGAssignFull|A2=TestA2ConcurrentUnique+TestPGA2ConcurrentUnique|A3=TestA3GenerationMonotonic+PG SQL 行级|A4=TestA4A9A12Idempotent+TestA4ThirdType+PG 三型|A5=TestA5CAS+PG|A6=TestA6ForceOverWrongStateAudited|A7=TestHTTPA7PullGateLinkage(assign→pull→drain→重派 force+audit→旧 executor 拒/新 executor 行)|A8=TestA8ErrorBijectionRealPath+TestR1ErrorBijection|A9=TestA4A9A12+PG|A10=TestA10OwnerConflict(Mem;PG PutTask 同源码路径)|A11=TestA11ForceNotOverRequeue|A12=TestA4A9A12(request_hash 对拍+body 原样)|A13=TestA13InvalidRequestSubCauses+PG 三子因+TestInvalidRequestIDNotAccounted。

## 声明
bb539a1/12dfa894 仅输入。本包=代码+部署 SQL+同锚真 PG 证据一次交齐;无"后续补证"项;v1.8 会签为唯一外部待决(码名若改,双射表+表驱动测试局部改不动语义)。
