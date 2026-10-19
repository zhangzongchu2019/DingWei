# S-NODEAGENT-TEST-ENV

基 integration/spec5ff-r31 4fbcf26e。仅改测试，不改变产品检查、生产锁位置或实际宿主配置。

正向 soft 根目录显式 chmod0700；R31 legacy 注册夹具的正向 seatRoot 同样显式 chmod0700。负向输入仍由测试自身构造，新增 soft/hard 组写、他写及符号链接拒绝格。三个真正进入 XFS Setup 锁的单测都注入私有锁，使用真实 flock，结束恢复钩子；本包现有全局 mock 用例不并行运行。

SetupFailure 的空 report 与 lsattr 项目号配对为60000，只在 bhard=16384m 返回唯一失败标记，bhard=0 回滚成功。断言必须到设置限额阶段、错误含指定标记、完整命令顺序 report→project设置→limit设置→limit归零→project清除，且清映射时目录仍在，最后目录消失。原先61001夹具造成早退假绿的路径现由 LIMIT_FAILURE_NOT_REACHED 检出。

另修宽权限凭据负格：创建0644受0077掩成0600，须明确chmod0644才能真正覆盖产品拒绝条件；未更改凭据读取逻辑。

## 验证范围

本机 UID1001 非root，子进程保留各自 umask0000/0002/0022/0077，nodeagent整包均rc0；未改父进程umask或生产锁权限。完整原始日志与逐轮读数见RESULTS.json。撤去soft根chmod、hard正向根chmod分别在protected-directory拒绝处红；撤回项目号修正则在指定 LIMIT_FAILURE_NOT_REACHED 红。生产根保护门没有放宽。

四种常用掩码的结果不外推到会屏蔽owner读写/执行位的极端umask；产品内部以0600创建私有状态和锁，Go testing.TempDir外层也受极端掩码影响。本件依组长正式验收要求，以两处非root umask0002为门；若要求支持屏蔽owner位的掩码，须另明确测试设施与产品权限前提，不能靠全局修改umask伪装覆盖。

## 原始定因回执更正

同席最后校正：dev-env日志里直接报quota lock permission denied的是 TestSetupMalformedLsattrNoPanicFullRollback，不能把该原文归到SetupFailure。后者两版完整首条为 quota_unit_test.go:147: Setup 失败目录必须自拆（无残留）: <nil>，单凭该断言不能断定其内部错误。本席此前报告a18a8158把私有拒写锁的本地复现误表述为peer直接错误，现撤回该归属；本地隔离复现仍有效，锁失败产品件独立处理。本次目录与锁夹具修复不冒称修了产品残留。

dev-env ubuntu UID1000、umask0002已由togpt60独立整包复验rc0（6.941s），Git证据496afab7，精确源8bd4d05d，见PEER-RECEIPT.json。双环境单包条件闭合；全仓Go读数交件时补齐。未部署、未操作43。

两件组合兼容检查：仅用Go overlay把a92f48cd的quota.go接到8bd4d05d测试修复上，非root umask0002的nodeagent整包rc0、6.829s；见COMBINED-NODEAGENT.log/json。不生成集成分支、不部署，也不替代各分支独立全仓门。

## 正式全仓门

源码 8bd4d05db25d4fff36e91fe99a86d15d12e5c496（tree bbfcfb33f6b326585a6e7671ffd987e84f52d8ad）完整 `go test ./... -count=1 -timeout=30m` rc0，共64包，其中52个有测试全过、12个无测试文件。严格本地临时PG与R30兼容二进制，730个Go/module源文件冻结核验未变；本提交只追加报告证据，源码与被测锚相同。原始全日志/rc、包清单、源SHA与验证摘要均随Git留档。
