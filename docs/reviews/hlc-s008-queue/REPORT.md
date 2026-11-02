# S-HLC-IMPL T-04／S008 队列补充读数

本件为 coder56solzzc.zzc 的局部实现自测记录，2026-11-01 UTC。基线HEAD ffaa6b27fb4f02660863e0ecc4e8bed41f4be387（r47 c248506c与冻结规格3a73c794的非快进merge）。队列4件已独立提交并push：opus50tc2k/hub-graceful-shutdown-impl，b0f07f377f8f489af64b52c8c2c329244f53e56c，tree 9cf65bf29448770c938f36468eb8307d0c2a40c6，ls-remote已核；仅包含AsyncDBQueue、队列测试、变异runner及FINDING。Hub协调器等其它产品与本报告尚为本地草稿，不能把子进程局部读数冒充该commit包含完整Hub实现；不宣告整单完成、独立验收或发布。实际相关文件SHA见source-files.json。

来源：已补读组长23:23:58Z T-04原裁定（request_id eb859dcad4b1、收信dw-dlv_65f174857d2674b47ade53e0febb2003），及排尽补充dw-dlv_b9197076fd0dc513f7639f24344526cf。本次不改运行期best-effort确认口径，不新增落库等待，不修跨动作事务，不改依赖／协议。

## 变化与范围

- 停机SealAndFlush先封入队；封口后Enqueue明确返回错误。逐批排尽channel及部分batch，成功提交才减少remaining；未尝试和失败项均保留在未提交计数里。保存首个提交原因，后续错误不覆盖；日志用安全分类，内部可用errors.Is核验首因。
- 等待准入锁／写协程／提交由DB阶段3秒截止约束。deadline与writer完成同时就绪不能改判成功；未结束的写协程阻止DB关闭。失败与超时共存保留first_error／failed和阶段deadline，退出2优先于1。
- FINDING只证异步错误不可传播与Flush不证明排尽，区分业务出站和已发送回执。基线overlay诊断（含首次vet无法打开新增虚拟路径、第二次vet=off的两个nil）单列附录，非HLC验收。

## 实测矩阵

| 隔离格 | 实际读数 | 判据 |
| --- | --- | --- |
| 封口前channel仍有多批 | 23条，批大小4；封口后才启动writer；4/4/4/4/4/3六批；提交23，remaining=0，failed=0；迟到入队拒绝 | 全部缓冲及尾批均提交 |
| 第二／第四批提交失败 | 两个不同合成错误；提交15，remaining=8，failed=8，channel=0，返回首错 | 后续错误不覆盖首错，不把未提交洗为0 |
| 第二批超时 | 提交4，remaining=19，failed=4，未尝试15 | 停止进一步提交并保留未知／未完成数 |
| 异常生产者持准入锁阻塞 | 50ms调用截止返回DeadlineExceeded；取消并收回夹具生产者 | SealAndFlush自身等待有界 |
| 真实队列接协调器 | 成功0；提交失败1；DB截止2；失败后截止2；DB预算3000ms，超时实测3000～3001ms；超时不关库 | 退出分类与存储顺序 |
| 队列／协调器独立子进程 | 实际退出0/1/2/2，外测1028/27/3031/3031ms；失败后超时保留first_error=operation_failed | 不只检查函数返回码 |
| 队列S008随机顺序50轮 | race/default vet，rc0，详见queue-s008-repeat50.log | 仅队列局部重复，不充作完整Hub负载50轮 |

子进程调用实际队列和coordinateShutdown，未调用完整Hub run装配；本格不替代S006混合连接／SDK回调，也不替代S011切换恢复。所有测试均使用合成消息、隔离HOME及环境白名单，不连接飞书／生产DB，不输出正文或凭据。

## 反向控制

可复跑：`python3 tests/hlc/s008_mutations.py --output /tmp/hlc-s008-mutations-review`。脚本仅在临时目录生成Go overlay替换已有async_dbqueue.go，默认vet／race开启；不修改产品工作树。

| 变异 | 实测 | 目标断言 |
| --- | --- | --- |
| 停机路径只flush当前batch，不排尽channel | rc1；提交0，remaining=23，返回queue shutdown incomplete | S008_ALL_BUFFERED_COMMITTED |
| 后续错误覆盖首错 | rc1；计数仍15/8/8，但返回的原因不是首错 | S008_FIRST_ERROR_AND_REMAINDER |

源SHA／变异SHA、UTC、命令、日志SHA见mutations.json；恢复原源的目标测试通过。变异红不是编译／vet／网络失败。

## 验证与未完成项

- `go test -race ./internal/bus -count=1 -v` rc0（queue-final.json；随后只新增准入锁等待测试，已纳queue-s008-complete）。
- `go test -race ./internal/bus ./cmd/workpulse -run ^TestShutdown -count=1 -v` rc0；queue-s008-complete.json／log包含当前所有队列新增格、协调器及真实退出码。
- `go test -race ./internal/bus -run ^TestShutdownQueueS008 -count=50 -shuffle=on -v` rc0，原始随机seed与各轮见日志。
- 全仓OpenSpec1.16.0 strict rc0，需求编号仍9条、场景编号仍13个；HLC-004明确停机限定与多批S008；diff-check通过。
- 仓库要求的 `bash scripts/run-build-tests.sh` 已完成rc0：2026-11-01T23:47:56Z～23:56:46Z（精确值见build-queue-final.json），helper两profile各1097项、inventory90、Python27组／shell2组。原始日志与SHA见同目录receipt；不以构建门通过冒称完整HLC验收。

T-04总项、T-05～11不据本件勾选。SDK当前运行拓扑待组长提供可核非敏感证据；未改SDK、依赖或独立FCS。完整Hub混合流量、S005等其余反证、恢复矩阵及独立验收尚未完成；队列4件已提交／推送，余下Hub草稿未提交；未部署／apply／archive。

最终补充：运行期边界回归 `TestShutdownQueueRuntimeAcknowledgementRemainsBestEffort` 默认vet/race rc0：实际提交失败后运行期Enqueue=nil、Flush=nil，停机SealAndFlush才返回首错，见queue-runtime-boundary.log／json。新增的是停机诊断／确认边界，不改变运行期落库等待。
