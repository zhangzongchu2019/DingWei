# T17 fan-out 代理 v2.3 实现 整版提审包

- 设计冻结锚:v2.3@90b5a15(业主直裁 T1/T2 diff 后拍定,08-04);runbook 终稿同仓。
- 实现分支:feat/t17-fanout-proxy,基线=v1 实现@6e612bc(双审 PASS)。
- 实现 commits(段 1-4):015bf7f(出站托管核)→3afff1f(入站 ws+N-sink+waive 三态)→6ec820b(配置热载+管理 API+pre-arm+Hub 三改)→本包 tip(G9 e2e+本文)。
- 取码:`git fetch origin <tip 40 位 SHA>`(⚠工蜂读副本分裂前科,勿信 ls-remote 分支行)。

## 1. 改动面

**代理(internal/fanout+cmd/feishu-fanout)**:spool.go/inspool.go(双向 WAL,synchronous=FULL)、custody.go(v:1 托管出站)、sinks.go(ws 接入装配+N-sink 投递)、configfile.go(声明式配置+原子持久+热载)、prearm.go(六门)、admin.go(管理 API+/resolve)。
**Hub(加性三处,per 设计 §G)**:①proxygateway.go ResolveNameToID 透传+Send v:1 托管(无 RequestID=legacy 零回归)②feishu.OutMessage.RequestID+requestid.go DeriveRequestID(UUIDv5)+schedule_worker.go 填充③workpulse main.go verifyVersionedDelivery 条件严格分支(无 v=legacy 原样)。
**依赖新增**:gopkg.in/yaml.v3(配置);modernc sqlite 已在仓。

## 2. 判据对照(设计 v2.3 → 代码 → 测试)

| 判据 | 代码 | 测试 |
|---|---|---|
| §C1 request_id 稳定来源(UUIDv5/派生/代理禁代生成) | requestid.go;custody.go reUUIDv5+400 | TestDeriveRequestIDDeterministicV5(含拼接歧义)/TestCustodyValidation |
| §C1 幂等(同 hash=同 outbound_id;异=409) | spool.Accept | TestCustodyIdempotentReplayAnd409/G13 两层 |
| §C2 202=accepted 仅 WAL fsync 后 | OpenSpool synchronous=FULL;Accept COMMIT 后 202 | TestCustodyRestartRecovery(进程内)+TestG9(跨进程 kill -9) |
| §C3 状态机+GET status | spool 状态列+handleOutboundStatus | TestCustodyAcceptAndSend/TestCustodyDLQ |
| §C 指定来源(bot 选路/caller 后缀) | senderFor/callerByToken+落盘前定格 | TestCustodyMultiBotRouting/TestCustodyCallerSuffix |
| §A WAL 先于 SDK ack | onWSInbound 返回值契约 | TestWSInboundWALBeforeAck |
| §B ALL-authoritative/全局三态/T2 waive | inspool recomputeGlobal/WaiveSink | TestSinkGlobalTerminalAndWaive |
| §B 毒件不 HOL/besteffort 有界 | MarkSinkFailed poison/dropped | TestSinkPoisonNoHOL/TestSinkBesteffortBoundedAndNoBlock |
| §K1 v:1 投递+token+401 恒重试 | inboundPayloadV1/deliverSink | TestWSInboundPayloadShapeAndDelivery/TestSink401AuthKeepsRetrying |
| §K1 Hub 条件严格(v==1/未知字段/无 v=legacy) | verifyVersionedDelivery | TestVerifyVersionedDelivery(5 例) |
| §E TLS 规则/force 防呆/原子持久/烂配置不接管 | configfile.Validate/SaveFileConfigAtomic | TestFileConfigValidateTLSAndForce/TestFileConfigAtomicSaveLoad/TestAdminPutCASAndApply |
| §E CAS 412/§F 掩码/RBAC/loopback/审计 | admin.go | TestAdminAuthAndMask/TestAdminPutCASAndApply/TestAdminRBACForceAndWaive |
| §B DLQ replay 水位门 | handleAdminDLQReplay/ReplayDLQ | TestAdminDLQReplay |
| B2 pre-arm 六门 | prearm.go | TestPreArmGate(过/token 拒/attest 拒) |
| §G /resolve 闭环 | admin.handleResolve+ProxyGateway | TestProxyGatewayResolvePassthrough |
| G9 翻转/回滚 e2e | g9_e2e_test.go(真二进制) | TestG9CustodyAndFlipE2E |

**G1-G13 覆盖**:G1✓(payload 形)G2✓逻辑层(WAL 先于 ack/崩溃窗恢复/幂等;真飞书分流守恒=阶段 0.5 canary 实证项,非测试可代)G3✓(两层)G4✓ G5✓ G6✓ G7✓ G8✓ G9✓(e2e:托管全链/kill -9 跨进程/SIGHUP 热载代次/掩码/回滚语义)G10✓ G11 部分(SQLite busy=busy_timeout 配置;磁盘满 fail-closed=断连逻辑在 PreArm 拒起+spool 错误不 ack,满盘物理演练=真机 runbook 项)G12✓ G13✓(单测+e2e 两层)。

## 3. 复现命令

```
go build ./... && go test ./internal/fanout/ ./internal/feishu/ ./cmd/workpulse/ ./cmd/feishu-fanout/ -count=1
go test ./cmd/feishu-fanout/ -run TestG9 -v   # 真二进制 e2e
```

## 4. 披露项(reviewer 先读)

1. **legacy 双协议过渡**:/outbound 无 v=v1 立发路、ProxyGateway 无 RequestID=旧协议、Hub webhook 无 v=飞书原样——三处均为升级过渡+回滚态兼容,v:1 路全部严格。生产切换后 legacy 出站路仅测试 Hub 旧版本可能触达,弃用时点=阶段 2 后。
2. **测试专用注入面**:`/_test/inject`(FANOUT_TEST_INJECT=1 才注册,启动响亮告警日志)——G9 e2e 无真飞书连接时驱动 ws 管道用;生产配置不设即不存在。真 ws 分流守恒=阶段 0.5 canary(greyTestRobot)实证,e2e 不能代。
3. **SinkConfig.Suffix 字段**:冻结设计 §B 配置模型字段,入站投递不改字节(后缀语义在出站 caller 策略),字段保留兼容配置形状、当前无读者。
4. **collector 透传延后**(设计 §G 明示);后台管理 UI 不在本件(管理 API 契约已冻结可 CLI 管)。
5. **配置文件**:yaml.v3 解析;JSON⊂YAML,e2e 用 JSON 语法写 fanout.yaml 合法。
6. **磁盘满物理演练**:留真机证据清单项(容器内 loop 设备),单测覆盖 fail-closed 逻辑路径。
7. M5/board/mcs 域零触碰;reply-origin 件(另线)零交集。
