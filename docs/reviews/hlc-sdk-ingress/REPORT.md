# S-HLC-IMPL SDK裁定落实：实施中途读数

执行者coder56solzzc.zzc，2026-11-02 UTC；起点b0f07f377f8f489af64b52c8c2c329244f53e56c。本件为实施中途记录，**不是完整HLC交件，不申请评审／验收／发布**。最终Git锚见通知；不得将本件局部场景当S001～013整矩阵通过。

## 归属与范围

独立fetch核工蜂wego-ai/dingwei分支review/prodops/fcs-runtime-facts-20261101，commit c15d6d7884f8452bf7717818f329ec192bca5094、tree2ca8a513ee5e7d1255069a235aa419c0eed783d8，证据docs/reviews/prodops-fcs-runtime-facts-20261101/REPORT.md。现网Hub无FCS_RPC_URL、枚举ws、三次飞书连接1均归属Hub单元；无独立fcs单元。预发fake且0连接。位置前置已闭合；未触生产、预发、配置、独立FCS或依赖。

按组长request_id68d69f8ce9c3：SDK适配在8秒drain中Close→Seal回调→等已准入返回；迟到回调在规范化/业务前拒绝并计数，不触DB。SDKStart包装器随ctx取消返回，纯SDK内部Start等待帧单列、不纳业务计数；SDK内部残余由进程回收，**不冒称所有SDK goroutine已join**。依赖/版本/协议不改。

Hub在drain调用可选ReceiverLifecycle；remote FCS receiver继续既有订阅/根回调门路径，未改FCS应用。已准入回调保留context值并受独立workCtx约束，不因接收器t0取消就中断其DB提交；超时Force取消workCtx，DB只在真实使用者结束后关闭。

## 实测

| 格 | 读数／结果 | 证据 |
| --- | --- | --- |
| S006回调扩展（真实dispatcher+临时SQLite） | active0、rejected1、totalDB1、afterSealDB0、Close1、SDKStartNotJoined1；等待在途返回后才完成 | sdk-owned-race-final.log |
| 撤回调封口 | rc1，迟到进入处理，totalDB2、afterSealDB1、rejected0，红在S006_POST_SEAL_DB_ZERO | mutation.json/no-ingress-seal.log |
| 回调扩展随机顺序50轮 | 默认vet/race rc0；只计回调扩展，不充完整混合矩阵50轮 | sdk-s006-repeat50.json/log |
| 真实钉版SDK独立进程 | 本地WebSocket真实被Close，实际Start等待帧1单列，业务回调0；测试进程退出回收SDK残余 | sdk-actual-process-fixed.json/log |
| 回调未返回／Close阻塞／多网关／停止后再Start | 截止错误可见，阻塞Close不伪报完成，迟到拒绝及聚合计数可核，禁止停后建立新连接 | sdk-owned-race-final.log |
| Hub接线8秒截止 | drain预算8000ms、实测8006ms、退出分类2、DB未关闭 | sdk-hub-final.log |
| 最终实际Hub fake空载 | 真实子进程退出0；停机1227ms、双listener差20us | sdk-hub-final.log |
| FCS兼容 | internal/fcs包默认vet/race rc0；不将remote分支当现网验收依据 | sdk-owned-race-final.log |

回调扩展中初始回调在封口前完成一次真实SQLite入库，随后保持在途；封口后再注入迟到回调，因此实测封口后总DB调用0。保证针对迟到回调不进入业务；不声称任意已准入事务的所有后续DB步骤均已在封口前发生，已准入者仍须等实际返回后才关DB。

完整100HTTP/100SSE/20mux/20WS的S006混合流量仍未执行，不将上面的扩展夹具冒充完整S006通过；其余生命周期反证、恢复与全量负载同样仍待实施。

## SDK进程所有权与跳过项

默认Go套件中TestReceiverPinnedSDKProcess明确SKIP：SDK残余必须在独立测试进程回收，不能污染其它goroutine断言。已另在隔离HOME与白名单环境显式执行：

`env HLC_PINNED_SDK_PROCESS=1 go test ./internal/feishu -run ^TestReceiverPinnedSDKProcess$ -count=1 -timeout=15s -v`

该显式进程夹具rc0，有实际连接与栈帧读数；默认skip不算通过。普通go test并不冒称SDK自身全部生命周期race认证；业务适配/计数/关闭边界与FCS兼容另以默认vet/race执行并rc0。实际Start栈帧匹配排除created-by归因行，业务owned计数断言不使用runtime.NumGoroutine减一的伪归零。

## 可复跑与最终检查

- `go test -race ./internal/feishu ./internal/lifecycle ./internal/fcs -count=1 -v`：rc0，明确列出上述默认skip。
- `python3 tests/hlc/s006_mutations.py --output /tmp/hlc-s006-review`：默认vet/race，仅临时overlay替换已有源文件；红必须来自DB断言。
- `go test -race ./cmd/workpulse -run '^Test(Shutdown|HLCActualProcessIdle)' -count=1 -timeout=90s -v`：rc0，含当前SDK接线、S008队列/子进程与实际Hub空载。
- 本轮 `bash scripts/run-build-tests.sh`：00:06:09.412389Z～00:14:57.970021Z，rc0；helper两profile各1097、inventory90、Python27组/shell2组；完整原始日志SHA见build-sdk-final.json，较大原始日志本地保留，Git内为命令receipt及摘要。
- 全仓OpenSpec1.16.0 strict rc0，9条需求/13场景编号保持，HLC-004/S006追加SDK裁定；diff-check通过。

中途夹具失败保留：SDK首轮计数把pingLoop的created-by行也算成Start帧（误报2），改为实际栈帧匹配后通过；Hub接线抽函数首轮多一层指针编译失败，修正后通过。这两件都不是产品故障或有效变异；原日志同目录保留。

T-04～11总项仍不因本件勾选；不自验收、不部署、不apply/archive，不改systemd、控制器或R10/R9。源码仅Git托管，本件不含凭据、env值、源包或二进制。
