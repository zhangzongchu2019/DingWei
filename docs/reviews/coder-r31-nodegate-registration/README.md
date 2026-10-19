# R31 nodegate 全仓回归修复

基准15c79dacd1d9c90eb64e5391f57ceb126eaf8b03。组长全仓回归发现internal/nodegate五格登记均失败；作者在该基准独立重现before.log的同五格，均为register: hosted: 参数不合要求。

根因是mkGate旧夹具没有QuotaPolicyRevision。R31 R2/R4将新登记hard节点和soft节点一样要求64位小写策略revision，RegisterNode委托RegisterManagedNode的首个门即校验此值。访问范围空值仍默认admin_only、地址仍允许空、调度状态空值仍默认enabled，并不是这些字段导致失败。新登记规则不同于存量旧hard缺快照时保留既有席位的兼容口径。

本提交仅给nodegate的公共mkGate夹具显式hard与64位测试revision；没有修改任何生产校验、登记、网关或调度代码，也没有把测试revision当成真实节点配置。五格继续验证真实PG与WebSocket鉴权/容量/回调/截止及重连对账，没有跳过或弱化断言。

## 实际生产参数核对

- hosted-admin生产register-node解析--quota-mode（默认hard）和--quota-policy-revision（hard/soft均须传），随后调用同一RegisterNode→RegisterManagedNode校验/落库。显式有效revision的CLI登记与规范化契约通过cli-contract.log；注意既有测试辅助器对旧hard夹具补测试revision，不能由其default-hard用例误称生产允许缺revision。
- 真实nodeagent启动不执行节点登记：运维先登记取得一次性凭据，nodeagent使用NODEAGENT_NODE_ID与NODEAGENT_SECRET_FILE连接；容量帧从同一NodePolicy给出QuotaMode和Revision。实际执行go run ./cmd/nodeagent --print-policy-revision，在隔离的非秘密策略环境（hard、MAX_SEATS4）获得64位小写摘要并rc0；该模式在凭据/Engine/网络初始化前返回，未访问宿主或Hub。
- 实际登记应先用与P3部署相同的策略配置取得nodeagent --print-policy-revision结果，再显式传给hosted-admin register-node --quota-mode hard --quota-policy-revision；不能用本夹具的a重复值登记真实节点。未改该安全要求来兼容过时测试。

## 验证证据

nodegate.log为五格全部通过；WITHDRAWAL.json用Go overlay恢复父提交夹具，必须在TestGateAuthCommandCapacityLifecycle的登记行以同一参数错误红，不能算编译失败。工作树夹具前后SHA相同。SOURCE-SNAPSHOT.json记录全仓测试期间冻结的3738个已跟踪文件SHA；产品源无改动，只有gate_test.go的夹具参数增量。

最终全仓rc0，覆盖64包（52测试包通过、12包无测试文件），PACKAGES.json逐项与go list ./...核齐。hosted全包726.701s、nodegate25.271s、cmd/workpulse229.047s；3738已跟踪文件快照前后核对一致。

FULL-REPO.log与VALIDATION.json记录最终完整go test ./... -count=1 -timeout=30m、严格本地embedded PG、旧R30兼容二进制的rc；不能以定向包读数替代全仓。既有hosted全包曾耗时805秒，采用历史30m超时；最初默认10m轮主动中止，不算完整回归证据。今后交件均须附全仓go test ./...读数，已记恢复件。

MANIFEST.json覆盖本增量夹具与原始证据，从提交blob核验。仅本地测试/临时PG/HTTP与WS，不访问43、不部署，独立结论仍归togpt60。
